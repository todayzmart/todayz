// Package idempotency makes unsafe requests safe to retry (02 §2.4 step 14,
// 04 §1.6). A client sends an Idempotency-Key (a ULID it made); the first
// request with that key runs, and every later request with the same key and
// the same body gets the stored response back. The same key with a different
// body is 422 idempotency_mismatch; a retry while the first is still running
// is 409 conflict with a detail saying it is in progress (04 §1.6).
//
// Keys are scoped by principal + route + key and kept for 24 h. A 5xx or a
// panic releases the key, so the client can retry the same key.
//
// Only routes that declare it take part (route.Policy.Idempotency, set where
// the route is registered); on every other route the header is ignored.
//
// "The same request" is the hash of the raw bytes: SHA-256 over the raw query
// string, a zero byte, and the request body exactly as sent. Two bodies that
// mean the same JSON but differ in whitespace or key order are different
// requests (422 idempotency_mismatch): a client retries by resending the same
// bytes, never by re-serialising. A multipart/form-data body is the one
// exception: a browser picks a new boundary on every fetch, so it is hashed in
// a canonical form instead (its parts sorted by field name, each part's name,
// file name, Content-Type and bytes, without the boundary), and a retry that
// rebuilds the same FormData is the same request. A multipart body that does
// not parse, or has more than 64 parts, is hashed as raw bytes.
//
// Every claim carries a random claim id. Complete and Release match on it, so
// a request whose lease ran out (and whose key another request then took) can
// neither store its response over the new holder's nor delete its claim.
//
// A response with Cache-Control: no-store from the handler is never stored:
// the claim is released and the response sent as usual. Stored bodies are
// encrypted (AES-GCM under PII_ENC_KEY, PGStore).
package idempotency

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"sync"
	"time"
)

// TTL is how long a key and its response are kept (02 §2.4: 24 h).
const TTL = 24 * time.Hour

// DefaultLease is how long an in-flight key is held before another request
// may take it over (an instance that died mid-request). It is longer than the
// API server's WriteTimeout (60 s), so a request still running is never taken
// over; TestLeaseOutlivesWriteTimeout holds the two together.
const DefaultLease = 3 * time.Minute

// ClaimID identifies one claim of a key (fencing token).
type ClaimID [16]byte

// NewClaimID returns a random claim id.
func NewClaimID() ClaimID {
	var c ClaimID
	_, _ = rand.Read(c[:]) // crypto/rand.Read never fails (Go 1.24+)
	return c
}

// Scope identifies one key.
type Scope struct {
	Principal string // "user:<ulid>" or "ip:<client ip>"
	Route     string // "POST /v1/me/orders"
	Key       string // the client's ULID
}

// Response is a stored response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Outcome of Begin.
type Outcome int

// Outcomes.
const (
	// Proceed: this request holds the key; run the handler, then Complete or Release.
	Proceed Outcome = iota + 1
	// Replay: the key is done; send Response.
	Replay
	// InProgress: another request holds the key.
	InProgress
	// Mismatch: the key was used with a different request body.
	Mismatch
)

func (o Outcome) String() string {
	switch o {
	case Proceed:
		return "proceed"
	case Replay:
		return "replay"
	case InProgress:
		return "in_progress"
	case Mismatch:
		return "mismatch"
	}
	return "unknown"
}

// Begin is the result of Store.Begin.
type Begin struct {
	Outcome  Outcome
	Response *Response // set for Replay
	Claim    ClaimID   // set for Proceed: pass it to Complete or Release
}

// Store keeps keys. Every method takes now from the caller's clock.
type Store interface {
	// Begin claims s for a request whose body hashes to hash. A missing or
	// expired key, or an in-flight key whose lease has run out (same hash),
	// is claimed with a new ClaimID and returns Proceed.
	Begin(ctx context.Context, s Scope, hash []byte, now time.Time, lease time.Duration) (Begin, error)
	// Complete stores the response for the key while claim still holds it;
	// it is kept until now+TTL. ErrNotClaimed when claim no longer holds it.
	Complete(ctx context.Context, s Scope, claim ClaimID, resp Response, now time.Time) error
	// Release forgets the key while claim still holds it, so the client can
	// retry. A stale claim releases nothing.
	Release(ctx context.Context, s Scope, claim ClaimID) error
	// Purge deletes keys that expired at or before now and returns how many.
	Purge(ctx context.Context, now time.Time) (int64, error)
}

// ErrNotClaimed is returned by Complete when the key is no longer held by
// this request (its lease ran out and another request took it).
var ErrNotClaimed = errors.New("idempotency: key not held")

// MemoryStore is an in-process Store for unit tests and tools.
type MemoryStore struct {
	mu   sync.Mutex
	rows map[Scope]*memRow
}

type memRow struct {
	hash        []byte
	claim       ClaimID
	done        bool
	lockedUntil time.Time
	expiresAt   time.Time
	resp        Response
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{rows: map[Scope]*memRow{}} }

// Begin implements Store.
func (m *MemoryStore) Begin(_ context.Context, s Scope, hash []byte, now time.Time, lease time.Duration) (Begin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[s]
	switch {
	case !ok, !r.expiresAt.After(now):
		// new or expired
	case !bytes.Equal(r.hash, hash):
		return Begin{Outcome: Mismatch}, nil
	case r.done:
		resp := cloneResponse(r.resp)
		return Begin{Outcome: Replay, Response: &resp}, nil
	case r.lockedUntil.After(now):
		return Begin{Outcome: InProgress}, nil
	}
	c := NewClaimID()
	m.rows[s] = &memRow{hash: bytes.Clone(hash), claim: c, lockedUntil: now.Add(lease), expiresAt: now.Add(TTL)}
	return Begin{Outcome: Proceed, Claim: c}, nil
}

// Complete implements Store.
func (m *MemoryStore) Complete(_ context.Context, s Scope, claim ClaimID, resp Response, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[s]
	if !ok || r.done || r.claim != claim {
		return ErrNotClaimed
	}
	r.done, r.resp, r.expiresAt = true, cloneResponse(resp), now.Add(TTL)
	return nil
}

// Release implements Store.
func (m *MemoryStore) Release(_ context.Context, s Scope, claim ClaimID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rows[s]; ok && !r.done && r.claim == claim {
		delete(m.rows, s)
	}
	return nil
}

// Purge implements Store.
func (m *MemoryStore) Purge(_ context.Context, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for s, r := range m.rows {
		if !r.expiresAt.After(now) {
			delete(m.rows, s)
			n++
		}
	}
	return n, nil
}

// Len is the number of stored keys (tests).
func (m *MemoryStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}

func cloneResponse(r Response) Response {
	return Response{Status: r.Status, Header: r.Header.Clone(), Body: bytes.Clone(r.Body)}
}

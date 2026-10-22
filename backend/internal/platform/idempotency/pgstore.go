package idempotency

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore keeps keys in idempotency_keys. It always uses the pool, never the
// request's transaction: the claim must be visible to a concurrent retry at
// once, and must survive the handler's rollback.
//
// Response bodies are sealed with AES-256-GCM under PII_ENC_KEY before they
// are stored (a body can hold an address or an order code). The stored form
// is a version byte (0x01), the 12-byte nonce and the ciphertext; the
// additional data binds it to its principal, route and key, so a body cannot
// be moved to another row. Headers are an allow-list (storedHeaders) and are
// stored as JSON.
type PGStore struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
}

const sealVersion = 0x01

// NewPGStore returns a store on pool that seals bodies with key (32 bytes,
// PII_ENC_KEY).
func NewPGStore(pool *pgxpool.Pool, key []byte) (*PGStore, error) {
	if len(key) != 32 {
		return nil, errors.New("idempotency: the body key must be 32 bytes (PII_ENC_KEY; run make setup)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("idempotency: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("idempotency: gcm: %w", err)
	}
	return &PGStore{pool: pool, aead: aead}, nil
}

// aad binds a sealed body to its row: length-prefixed principal, route, key.
func aad(s Scope) []byte {
	var b []byte
	for _, f := range []string{s.Principal, s.Route, s.Key} {
		b = binary.BigEndian.AppendUint32(b, uint32(len(f))) // #nosec G115 -- header-sized strings
		b = append(b, f...)
	}
	return b
}

func (p *PGStore) seal(s Scope, body []byte) []byte {
	nonce := make([]byte, p.aead.NonceSize())
	_, _ = rand.Read(nonce) // crypto/rand.Read never fails (Go 1.24+)
	out := make([]byte, 0, 1+len(nonce)+len(body)+p.aead.Overhead())
	out = append(out, sealVersion)
	out = append(out, nonce...)
	return p.aead.Seal(out, nonce, body, aad(s))
}

func (p *PGStore) open(s Scope, sealed []byte) ([]byte, error) {
	n := p.aead.NonceSize()
	if len(sealed) < 1+n || sealed[0] != sealVersion {
		return nil, errors.New("idempotency: stored body has an unknown format")
	}
	body, err := p.aead.Open(nil, sealed[1:1+n], sealed[1+n:], aad(s))
	if err != nil {
		return nil, errors.New("idempotency: stored body does not open")
	}
	return body, nil
}

func claimUUID(c ClaimID) pgtype.UUID { return pgtype.UUID{Bytes: c, Valid: true} }

const claimSQL = `
INSERT INTO idempotency_keys AS k (principal, route, key, request_hash, state, claim_id, locked_until, expires_at, created_at)
VALUES ($1, $2, $3, $4, 'in_flight', $8, $5, $6, $7)
ON CONFLICT (principal, route, key) DO UPDATE
   SET request_hash = EXCLUDED.request_hash, state = 'in_flight', claim_id = EXCLUDED.claim_id,
       locked_until = EXCLUDED.locked_until, expires_at = EXCLUDED.expires_at, created_at = EXCLUDED.created_at,
       status_code = NULL, response_headers = NULL, response_body = NULL
 WHERE k.expires_at <= $7
    OR (k.state = 'in_flight' AND k.locked_until <= $7 AND k.request_hash = EXCLUDED.request_hash)
RETURNING true`

// Begin implements Store.
func (p *PGStore) Begin(ctx context.Context, s Scope, hash []byte, now time.Time, lease time.Duration) (Begin, error) {
	// A key released between the claim and the read is simply claimed on the
	// next pass; three passes are plenty.
	for range 3 {
		var claimed bool
		c := NewClaimID()
		err := p.pool.QueryRow(ctx, claimSQL, s.Principal, s.Route, s.Key, hash, now.Add(lease), now.Add(TTL), now, claimUUID(c)).Scan(&claimed)
		if err == nil {
			return Begin{Outcome: Proceed, Claim: c}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Begin{}, fmt.Errorf("idempotency: claim: %w", err)
		}
		var (
			rowHash []byte
			state   string
			status  *int
			hdr     []byte
			body    []byte
		)
		err = p.pool.QueryRow(ctx, `SELECT request_hash, state, status_code, response_headers, response_body
			FROM idempotency_keys WHERE principal = $1 AND route = $2 AND key = $3`,
			s.Principal, s.Route, s.Key).Scan(&rowHash, &state, &status, &hdr, &body)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return Begin{}, fmt.Errorf("idempotency: read: %w", err)
		}
		switch {
		case string(rowHash) != string(hash):
			return Begin{Outcome: Mismatch}, nil
		case state == "done" && status != nil:
			resp := Response{Status: *status, Header: http.Header{}}
			if len(hdr) > 0 {
				if err := json.Unmarshal(hdr, &resp.Header); err != nil {
					return Begin{}, fmt.Errorf("idempotency: headers: %w", err)
				}
			}
			if len(body) > 0 {
				if resp.Body, err = p.open(s, body); err != nil {
					return Begin{}, err
				}
			}
			return Begin{Outcome: Replay, Response: &resp}, nil
		default:
			return Begin{Outcome: InProgress}, nil
		}
	}
	return Begin{Outcome: InProgress}, nil
}

// Complete implements Store.
func (p *PGStore) Complete(ctx context.Context, s Scope, claim ClaimID, resp Response, now time.Time) error {
	hdr, err := json.Marshal(resp.Header)
	if err != nil {
		return err
	}
	var body []byte
	if len(resp.Body) > 0 {
		body = p.seal(s, resp.Body)
	}
	tag, err := p.pool.Exec(ctx, `UPDATE idempotency_keys
		SET state = 'done', status_code = $5, response_headers = $6, response_body = $7, locked_until = NULL,
		    claim_id = NULL, expires_at = $8
		WHERE principal = $1 AND route = $2 AND key = $3 AND claim_id = $4 AND state = 'in_flight'`,
		s.Principal, s.Route, s.Key, claimUUID(claim), resp.Status, hdr, body, now.Add(TTL))
	if err != nil {
		return fmt.Errorf("idempotency: complete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotClaimed
	}
	return nil
}

// Release implements Store.
func (p *PGStore) Release(ctx context.Context, s Scope, claim ClaimID) error {
	_, err := p.release(ctx, s, claim)
	return err
}

func (p *PGStore) release(ctx context.Context, s Scope, claim ClaimID) (int64, error) {
	tag, err := p.pool.Exec(ctx, `DELETE FROM idempotency_keys
		WHERE principal = $1 AND route = $2 AND key = $3 AND claim_id = $4 AND state = 'in_flight'`,
		s.Principal, s.Route, s.Key, claimUUID(claim))
	if err != nil {
		return 0, fmt.Errorf("idempotency: release: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Purge implements Store.
func (p *PGStore) Purge(ctx context.Context, now time.Time) (int64, error) {
	return Purger{pool: p.pool}.Purge(ctx, now)
}

// Purger deletes expired keys; the worker's maintenance job uses it (it needs
// no body key).
type Purger struct{ pool *pgxpool.Pool }

// NewPurger returns a Purger on pool.
func NewPurger(pool *pgxpool.Pool) Purger { return Purger{pool: pool} }

// Purge deletes keys that expired at or before now, in batches so a large
// backlog never holds a long lock.
func (p Purger) Purge(ctx context.Context, now time.Time) (int64, error) {
	var total int64
	for {
		tag, err := p.pool.Exec(ctx, `DELETE FROM idempotency_keys WHERE ctid IN (
			SELECT ctid FROM idempotency_keys WHERE expires_at <= $1 LIMIT 5000)`, now)
		if err != nil {
			return total, fmt.Errorf("idempotency: purge: %w", err)
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < 5000 {
			return total, nil
		}
	}
}

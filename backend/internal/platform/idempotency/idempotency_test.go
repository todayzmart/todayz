package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx/route"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

var t0 = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func sum(s string) []byte {
	h := make([]byte, 32)
	copy(h, s)
	return h
}

// storeContract runs the same cases against every Store.
func storeContract(t *testing.T, s Store) {
	ctx := context.Background()
	sc := Scope{Principal: "user:01J0000000000000000000000A", Route: "POST /v1/me/orders", Key: ids.New(t0).String()}
	a, b := sum("a"), sum("b")

	got, err := s.Begin(ctx, sc, a, t0, time.Minute)
	require.NoError(t, err)
	require.Equal(t, Proceed, got.Outcome)
	first := got.Claim
	require.NotEqual(t, ClaimID{}, first)

	got, err = s.Begin(ctx, sc, a, t0.Add(time.Second), time.Minute)
	require.NoError(t, err)
	require.Equal(t, InProgress, got.Outcome, "same key while running")

	got, err = s.Begin(ctx, sc, b, t0.Add(time.Second), time.Minute)
	require.NoError(t, err)
	require.Equal(t, Mismatch, got.Outcome, "same key, different body")

	resp := Response{Status: 201, Header: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"id":"x"}`)}
	require.ErrorIs(t, s.Complete(ctx, sc, NewClaimID(), resp, t0.Add(2*time.Second)), ErrNotClaimed, "another claim id")
	require.NoError(t, s.Complete(ctx, sc, first, resp, t0.Add(2*time.Second)))
	require.ErrorIs(t, s.Complete(ctx, sc, first, resp, t0.Add(2*time.Second)), ErrNotClaimed)

	got, err = s.Begin(ctx, sc, a, t0.Add(time.Hour), time.Minute)
	require.NoError(t, err)
	require.Equal(t, Replay, got.Outcome)
	require.Equal(t, 201, got.Response.Status)
	require.Equal(t, "application/json", got.Response.Header.Get("Content-Type"))
	require.JSONEq(t, `{"id":"x"}`, string(got.Response.Body))

	got, err = s.Begin(ctx, sc, b, t0.Add(time.Hour), time.Minute)
	require.NoError(t, err)
	require.Equal(t, Mismatch, got.Outcome, "a done key with another body")

	// Scoped by principal and route: the same key elsewhere is independent.
	other := sc
	other.Principal = "user:01J0000000000000000000000B"
	got, err = s.Begin(ctx, other, b, t0, time.Minute)
	require.NoError(t, err)
	require.Equal(t, Proceed, got.Outcome)
	other2 := sc
	other2.Route = "POST /v1/me/orders/x/cancel"
	got, err = s.Begin(ctx, other2, b, t0, time.Minute)
	require.NoError(t, err)
	require.Equal(t, Proceed, got.Outcome)

	// After 24 h the key is gone: a new request with it runs.
	got, err = s.Begin(ctx, sc, b, t0.Add(2*time.Second+TTL), time.Minute)
	require.NoError(t, err)
	require.Equal(t, Proceed, got.Outcome)

	// Release lets the client retry the same key at once; a stale claim
	// releases nothing.
	rel := Scope{Principal: "ip:127.0.0.1", Route: "POST /v1/x", Key: ids.New(t0).String()}
	got, err = s.Begin(ctx, rel, a, t0, time.Minute)
	require.NoError(t, err)
	require.NoError(t, s.Release(ctx, rel, NewClaimID()))
	again, err := s.Begin(ctx, rel, a, t0, time.Minute)
	require.NoError(t, err)
	require.Equal(t, InProgress, again.Outcome, "a release with another claim id is ignored")
	require.NoError(t, s.Release(ctx, rel, got.Claim))
	got, err = s.Begin(ctx, rel, a, t0, time.Minute)
	require.NoError(t, err)
	require.Equal(t, Proceed, got.Outcome)

	// An abandoned in-flight key is taken over once its lease runs out.
	got, err = s.Begin(ctx, rel, a, t0.Add(30*time.Second), time.Minute)
	require.NoError(t, err)
	require.Equal(t, InProgress, got.Outcome)
	got, err = s.Begin(ctx, rel, a, t0.Add(61*time.Second), time.Minute)
	require.NoError(t, err)
	require.Equal(t, Proceed, got.Outcome)

	// Purge removes what expired.
	n, err := s.Purge(ctx, t0.Add(48*time.Hour+time.Minute))
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(4))
	got, err = s.Begin(ctx, rel, a, t0.Add(49*time.Hour), time.Minute)
	require.NoError(t, err)
	require.Equal(t, Proceed, got.Outcome)
}

// fencing: A's lease runs out, B takes the key over, and A's late Complete
// and Release change nothing.
func fencing(t *testing.T, s Store) (a, b Begin, sc Scope) {
	t.Helper()
	ctx := context.Background()
	sc = Scope{Principal: "user:01J0000000000000000000000F", Route: "POST /v1/me/orders", Key: ids.New(t0).String()}
	a, err := s.Begin(ctx, sc, sum("a"), t0, DefaultLease)
	require.NoError(t, err)
	require.Equal(t, Proceed, a.Outcome)
	b, err = s.Begin(ctx, sc, sum("a"), t0.Add(DefaultLease+time.Second), DefaultLease)
	require.NoError(t, err)
	require.Equal(t, Proceed, b.Outcome, "B takes the key over after A's lease")
	require.NotEqual(t, a.Claim, b.Claim)

	stale := Response{Status: 200, Body: []byte(`{"from":"A"}`)}
	require.ErrorIs(t, s.Complete(ctx, sc, a.Claim, stale, t0.Add(DefaultLease+2*time.Second)), ErrNotClaimed)
	require.NoError(t, s.Release(ctx, sc, a.Claim))
	got, err := s.Begin(ctx, sc, sum("a"), t0.Add(DefaultLease+3*time.Second), DefaultLease)
	require.NoError(t, err)
	require.Equal(t, InProgress, got.Outcome, "B still holds the key")

	require.NoError(t, s.Complete(ctx, sc, b.Claim, Response{Status: 201, Body: []byte(`{"from":"B"}`)}, t0.Add(DefaultLease+4*time.Second)))
	got, err = s.Begin(ctx, sc, sum("a"), t0.Add(DefaultLease+5*time.Second), DefaultLease)
	require.NoError(t, err)
	require.Equal(t, Replay, got.Outcome)
	require.JSONEq(t, `{"from":"B"}`, string(got.Response.Body))
	return a, b, sc
}

func TestMemoryStore(t *testing.T) { storeContract(t, NewMemoryStore()) }

func TestMemoryStoreFencing(t *testing.T) { fencing(t, NewMemoryStore()) }

var testBodyKey = []byte("0123456789abcdef0123456789abcdef")

func newPGStore(t *testing.T, db *testkit.DB) *PGStore {
	t.Helper()
	s, err := NewPGStore(db.Pool, testBodyKey)
	require.NoError(t, err)
	return s
}

func TestPGStore(t *testing.T) {
	db := testkit.NewDB(t) // todayz_app: the role the API uses
	storeContract(t, newPGStore(t, db))
}

func TestNewPGStoreNeedsKey(t *testing.T) {
	_, err := NewPGStore(nil, nil)
	require.ErrorContains(t, err, "PII_ENC_KEY")
	_, err = NewPGStore(nil, []byte("short"))
	require.Error(t, err)
}

// TestPGStoreFencing: A's lease expires, B takes over; A's stale Complete and
// Release each affect 0 rows.
func TestPGStoreFencing(t *testing.T) {
	db := testkit.NewDB(t)
	s := newPGStore(t, db)
	a, b, sc := fencing(t, s)

	// Straight at the table: B's done row is untouched by A's claim id.
	ctx := context.Background()
	n, err := s.release(ctx, sc, a.Claim)
	require.NoError(t, err)
	require.Zero(t, n, "A's stale Release affects 0 rows")
	n, err = s.release(ctx, sc, b.Claim)
	require.NoError(t, err)
	require.Zero(t, n, "a done key is never released")
	var state string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT state FROM idempotency_keys WHERE key = $1`, sc.Key).Scan(&state))
	require.Equal(t, "done", state)
}

// TestPGStoreEncryptsBodies: headers round-trip, the body replays, and the
// stored column holds no plaintext.
func TestPGStoreEncryptsBodies(t *testing.T) {
	db := testkit.NewDB(t)
	s := newPGStore(t, db)
	ctx := context.Background()
	sc := Scope{Principal: "user:01J0000000000000000000000E", Route: "POST /v1/me/addresses", Key: ids.New(t0).String()}
	const secret = `{"line1":"42 Gandhi Road","phone":"9876543210"}`
	got, err := s.Begin(ctx, sc, sum("e"), t0, time.Minute)
	require.NoError(t, err)
	hdr := http.Header{"Content-Type": {"application/json"}, "Location": {"/v1/me/addresses/1"}, "Etag": {`"v1"`}}
	require.NoError(t, s.Complete(ctx, sc, got.Claim, Response{Status: 201, Header: hdr, Body: []byte(secret)}, t0))

	var raw []byte
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT response_body FROM idempotency_keys WHERE key = $1`, sc.Key).Scan(&raw))
	require.NotEmpty(t, raw)
	require.Equal(t, byte(sealVersion), raw[0])
	for _, plain := range []string{"Gandhi", "9876543210", "line1", secret} {
		require.NotContains(t, string(raw), plain)
	}

	rep, err := s.Begin(ctx, sc, sum("e"), t0.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	require.Equal(t, Replay, rep.Outcome)
	require.Equal(t, secret, string(rep.Response.Body))
	require.Equal(t, hdr, rep.Response.Header)

	// A body moved to another row does not open (the AAD binds it).
	other := Scope{Principal: "user:01J0000000000000000000000X", Route: sc.Route, Key: sc.Key}
	_, err = s.open(other, raw)
	require.Error(t, err)
	// Another key cannot open it.
	s2, err := NewPGStore(db.Pool, []byte("fedcba9876543210fedcba9876543210"))
	require.NoError(t, err)
	_, err = s2.Begin(ctx, sc, sum("e"), t0.Add(time.Minute), time.Minute)
	require.ErrorContains(t, err, "does not open")
}

// TestPGStoreConcurrentClaim: of many simultaneous first requests, one runs.
func TestPGStoreConcurrentClaim(t *testing.T) {
	db := testkit.NewDB(t)
	s := newPGStore(t, db)
	sc := Scope{Principal: "ip:10.0.0.1", Route: "POST /v1/x", Key: ids.New(t0).String()}
	var proceed atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.Begin(context.Background(), sc, sum("a"), t0, time.Minute)
			if err == nil && got.Outcome == Proceed {
				proceed.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), proceed.Load())
}

// --- middleware ---

type harness struct {
	store Store
	mem   *MemoryStore
	clk   *clock.Fake
	calls atomic.Int32
	h     http.Handler
	// status the handler answers with; block makes it wait.
	status int
	block  chan struct{}
	// cacheControl the handler sets, when not empty.
	cacheControl string
}

func newHarness(idem route.Idempotency) *harness { return newHarnessStore(idem, nil) }

// newHarnessStore wraps store (default a MemoryStore) in the middleware, with
// the route policy idem.
func newHarnessStore(idem route.Idempotency, store Store) *harness {
	hs := &harness{mem: NewMemoryStore(), clk: clock.NewFake(t0), status: 201}
	hs.store = hs.mem
	if store != nil {
		hs.store = store
	}
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hs.calls.Add(1)
		if hs.block != nil {
			<-hs.block
		}
		if hs.status == -1 {
			panic("boom")
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Not-Stored", "1")
		if hs.cacheControl != "" {
			w.Header().Set("Cache-Control", hs.cacheControl)
		}
		w.WriteHeader(hs.status)
		_ = json.NewEncoder(w).Encode(map[string]any{"call": n, "echo": string(body)})
	})
	mw := Middleware(Options{Store: hs.store, Clock: hs.clk})(handler)
	hs.h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := reqctx.WithClientIP(r.Context(), "198.51.100.9")
		ctx = route.WithPolicy(ctx, route.Policy{Idempotency: idem})
		defer func() { _ = recover() }() // stands in for the recover middleware
		mw.ServeHTTP(w, r.WithContext(ctx))
	})
	return hs
}

func (hs *harness) do(method, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/v1/me/orders", strings.NewReader(body))
	if key != "" {
		r.Header.Set(HeaderKey, key)
	}
	w := httptest.NewRecorder()
	hs.h.ServeHTTP(w, r)
	return w
}

func problemCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var p struct{ Code string }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p), w.Body.String())
	return p.Code
}

func TestMiddlewareReplay(t *testing.T) {
	hs := newHarness(route.IdempotencyOptional)
	key := ids.New(t0).String()
	first := hs.do("POST", key, `{"qty":1}`)
	require.Equal(t, 201, first.Code)
	require.Empty(t, first.Header().Get(HeaderReplayed))

	hs.clk.Advance(23 * time.Hour)
	again := hs.do("POST", key, `{"qty":1}`)
	require.Equal(t, 201, again.Code)
	require.Equal(t, "true", again.Header().Get(HeaderReplayed))
	require.Equal(t, first.Body.String(), again.Body.String(), "same key + same body = same response")
	require.Equal(t, "application/json", again.Header().Get("Content-Type"))
	require.Empty(t, again.Header().Get("X-Not-Stored"), "only allow-listed headers are replayed")
	require.Equal(t, int32(1), hs.calls.Load(), "the handler ran once")

	// After 24 h the key is forgotten.
	hs.clk.Advance(2 * time.Hour)
	_, _ = hs.mem.Purge(context.Background(), hs.clk.Now())
	late := hs.do("POST", key, `{"qty":1}`)
	require.Empty(t, late.Header().Get(HeaderReplayed))
	require.Equal(t, int32(2), hs.calls.Load())
}

func TestMiddlewareMismatch(t *testing.T) {
	hs := newHarness(route.IdempotencyOptional)
	key := ids.New(t0).String()
	require.Equal(t, 201, hs.do("POST", key, `{"qty":1}`).Code)
	w := hs.do("POST", key, `{"qty":2}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	require.Equal(t, "idempotency_mismatch", problemCode(t, w))
	require.Equal(t, int32(1), hs.calls.Load())
}

func TestMiddlewareInFlight(t *testing.T) {
	hs := newHarness(route.IdempotencyOptional)
	hs.block = make(chan struct{})
	key := ids.New(t0).String()
	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- hs.do("POST", key, `{}`) }()
	require.Eventually(t, func() bool { return hs.calls.Load() == 1 }, 5*time.Second, time.Millisecond)

	w := hs.do("POST", key, `{}`)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Equal(t, "conflict", problemCode(t, w))
	require.Contains(t, w.Body.String(), "in progress")
	require.Equal(t, "1", w.Header().Get("Retry-After"))

	close(hs.block)
	require.Equal(t, 201, (<-done).Code)
	require.Equal(t, "true", hs.do("POST", key, `{}`).Header().Get(HeaderReplayed))
}

func TestMiddleware5xxAndPanicRelease(t *testing.T) {
	hs := newHarness(route.IdempotencyOptional)
	key := ids.New(t0).String()
	hs.status = 503
	require.Equal(t, 503, hs.do("POST", key, `{}`).Code)
	require.Zero(t, hs.mem.Len(), "a 5xx is not stored")

	hs.status = -1
	_ = hs.do("POST", key, `{}`)
	require.Zero(t, hs.mem.Len(), "a panic releases the key")

	hs.status = 201
	w := hs.do("POST", key, `{}`)
	require.Equal(t, 201, w.Code, "the same key can be retried")
	require.Empty(t, w.Header().Get(HeaderReplayed))

	// A 4xx is a real answer and is replayed.
	hs.status = 409
	key2 := ids.New(t0).String()
	require.Equal(t, 409, hs.do("POST", key2, `{}`).Code)
	require.Equal(t, "true", hs.do("POST", key2, `{}`).Header().Get(HeaderReplayed))
}

func TestMiddlewareHeaderRules(t *testing.T) {
	hs := newHarness(route.IdempotencyOptional)
	// No header: passes through, nothing stored.
	require.Equal(t, 201, hs.do("POST", "", `{}`).Code)
	require.Zero(t, hs.mem.Len())
	// Safe methods ignore the header.
	require.Equal(t, 201, hs.do("GET", ids.New(t0).String(), ``).Code)
	require.Zero(t, hs.mem.Len())
	// Not a ULID.
	w := hs.do("POST", "not-a-ulid", `{}`)
	require.Equal(t, 400, w.Code)
	require.Equal(t, "idempotency_key_invalid", problemCode(t, w))

	// IdempotencyRequired: a route that must have the key.
	req := newHarness(route.IdempotencyRequired)
	w = req.do("POST", "", `{}`)
	require.Equal(t, 400, w.Code)
	require.Equal(t, "idempotency_key_required", problemCode(t, w))
	require.Equal(t, 201, req.do("POST", ids.New(t0).String(), `{}`).Code)
}

// TestMiddlewareOnlyOnDeclaredRoutes: a route without a policy ignores the
// header, so a repeat runs again and nothing is stored.
func TestMiddlewareOnlyOnDeclaredRoutes(t *testing.T) {
	hs := newHarness(route.IdempotencyNone)
	key := ids.New(t0).String()
	require.Equal(t, 201, hs.do("POST", key, `{}`).Code)
	w := hs.do("POST", key, `{}`)
	require.Equal(t, 201, w.Code)
	require.Empty(t, w.Header().Get(HeaderReplayed))
	require.Equal(t, int32(2), hs.calls.Load())
	require.Zero(t, hs.mem.Len())
	// Not even a malformed key is looked at.
	require.Equal(t, 201, hs.do("POST", "not-a-ulid", `{}`).Code)
	// Optional without the header runs.
	opt := newHarness(route.IdempotencyOptional)
	require.Equal(t, 201, opt.do("POST", "", `{}`).Code)
	require.Zero(t, opt.mem.Len())
}

// TestMiddlewareNoStore: a handler's Cache-Control: no-store response is
// sent as usual but never stored; the claim is released.
func TestMiddlewareNoStore(t *testing.T) {
	hs := newHarness(route.IdempotencyRequired)
	hs.cacheControl = "private, No-Store"
	key := ids.New(t0).String()
	w := hs.do("POST", key, `{"otp":"482913"}`)
	require.Equal(t, 201, w.Code)
	require.Equal(t, "private, No-Store", w.Header().Get("Cache-Control"))
	require.Contains(t, w.Body.String(), "482913", "the response itself is sent")
	require.Zero(t, hs.mem.Len(), "never stored")
	w = hs.do("POST", key, `{"otp":"482913"}`)
	require.Empty(t, w.Header().Get(HeaderReplayed), "a retry runs again")
	require.Equal(t, int32(2), hs.calls.Load())

	// Another Cache-Control is stored and replayed.
	hs.cacheControl = "private, max-age=0"
	key2 := ids.New(t0).String()
	require.Equal(t, 201, hs.do("POST", key2, `{}`).Code)
	w = hs.do("POST", key2, `{}`)
	require.Equal(t, "true", w.Header().Get(HeaderReplayed))
	require.Equal(t, "private, max-age=0", w.Header().Get("Cache-Control"))
}

// failingComplete is a store whose Complete fails after the handler ran.
type failingComplete struct {
	*MemoryStore
	releases atomic.Int32
}

func (f *failingComplete) Complete(context.Context, Scope, ClaimID, Response, time.Time) error {
	return errors.New("db gone")
}

func (f *failingComplete) Release(ctx context.Context, s Scope, c ClaimID) error {
	f.releases.Add(1)
	return f.MemoryStore.Release(ctx, s, c)
}

// TestMiddlewareCompleteFailureHoldsKey: when the response cannot be stored
// the key is not released: a retry within the lease gets 409, so the request
// never runs twice inside the lease; the failure is counted.
func TestMiddlewareCompleteFailureHoldsKey(t *testing.T) {
	st := &failingComplete{MemoryStore: NewMemoryStore()}
	hs := newHarnessStore(route.IdempotencyRequired, st)
	key := ids.New(t0).String()
	before := CompleteFailures()
	require.Equal(t, 201, hs.do("POST", key, `{}`).Code, "the response is still sent")
	require.Equal(t, before+1, CompleteFailures())
	require.Zero(t, st.releases.Load(), "not released")

	w := hs.do("POST", key, `{}`)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Equal(t, int32(1), hs.calls.Load())

	hs.clk.Advance(DefaultLease + time.Second)
	require.Equal(t, 201, hs.do("POST", key, `{}`).Code, "after the lease the key is free")
	require.Equal(t, int32(2), hs.calls.Load())
}

func TestScopeByPrincipal(t *testing.T) {
	ctx := reqctx.WithClientIP(context.Background(), "203.0.113.1")
	require.Equal(t, "ip:203.0.113.1", principal(ctx))
	ctx = reqctx.WithPrincipal(ctx, reqctx.Principal{Kind: "customer", UserID: "01J0000000000000000000000C"})
	require.Equal(t, "user:01J0000000000000000000000C", principal(ctx))
}

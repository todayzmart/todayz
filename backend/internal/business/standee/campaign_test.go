package standee

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

var testNow = time.Date(2026, 10, 25, 6, 30, 0, 0, time.UTC)

type env struct {
	t     *testing.T
	tdb   *testkit.DB
	svc   *Service
	clk   *clock.Fake
	ctx   context.Context
	staff authn.Principal
	codes int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	e := &env{t: t, tdb: tdb, clk: clock.NewFake(testNow), ctx: context.Background()}
	e.staff = authn.Principal{UserID: e.user(), App: authn.AppBusiness}
	var err error
	e.svc, err = New(Deps{DB: db.New(tdb.Pool), Clock: e.clk, PublicURL: "https://api.todayz.localhost/"})
	require.NoError(t, err)
	return e
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.tdb.OwnerPool.Exec(e.ctx, sql, args...)
	require.NoError(e.t, err, sql)
}

func (e *env) code() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	e.codes++
	n, b := e.codes, []byte("S00000")
	for i := 5; i >= 1; i-- {
		b[i] = alphabet[n%len(alphabet)]
		n /= len(alphabet)
	}
	return string(b)
}

func (e *env) user() ids.ID {
	id := ids.New(testNow)
	e.exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(id))
	return id
}

// store is a live store of a new business; it returns the store and its
// short code.
func (e *env) store() (ids.ID, string) {
	biz, id, code := ids.New(testNow), ids.New(testNow), e.code()
	e.exec(`INSERT INTO businesses (id, brand_name, referral_code, status) VALUES ($1, 'Shop', $2, 'verified')`, ids.UUID(biz), e.code())
	e.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, status, live_at) VALUES ($1, $2, $3, $4, 'Shop', 'live', $5)`,
		ids.UUID(id), ids.UUID(biz), "shop-"+strings.ToLower(code), code, testNow)
	return id, code
}

// scan is a click on the standee's link, as referrals/click.go records it.
func (e *env) scan(store ids.ID, code string, standee ids.ID, at time.Time) ids.ID {
	id := ids.New(at)
	e.exec(`INSERT INTO referral_clicks (id, code, referrer_type, referrer_id, standee_id, landing, created_at)
	  VALUES ($1, $2, 'store', $3, $4, 'store', $5)`, ids.UUID(id), code, ids.UUID(store), ids.UUID(standee), at)
	return id
}

// signUp is a referral claimed from a scan, in the given status.
func (e *env) signUp(store ids.ID, code string, standee, click ids.ID, status string) {
	var reason, qualified, rewarded any
	switch status {
	case "rejected", "rejected_same_device":
		reason = "test"
	case "qualified":
		qualified = testNow
	case "rewarded":
		qualified, rewarded = testNow, testNow
	}
	e.exec(`INSERT INTO referrals (id, kind, referrer_type, referrer_id, referee_type, referee_id, code, source, click_id, standee_id,
	    status, reject_reason, referrer_credits, referee_credits, qualified_at, referrer_rewarded_at, referee_rewarded_at, created_at, updated_at)
	  VALUES ($1, 'retailer_customer', 'store', $2, 'customer', $3, $4, 'qr_standee', $5, $6, $7, $8, 0, 0, $9, $10, $10, $11, $11)`,
		ids.UUID(ids.New(testNow)), ids.UUID(store), ids.UUID(e.user()), code, ids.UUID(click), ids.UUID(standee),
		status, reason, qualified, rewarded, testNow)
}

func problemCode(err error) string {
	var p *problem.Error
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

func (e *env) audits(action, subject string) int {
	var n int
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(e.ctx,
		`SELECT count(*) FROM audit_log WHERE action = $1 AND subject_type = 'qr_standee' AND subject_id = $2 AND actor_kind = 'staff'`,
		action, subject).Scan(&n))
	return n
}

func TestCreateAndList(t *testing.T) {
	e := newEnv(t)
	store, code := e.store()

	l, err := e.svc.List(e.ctx, store)
	require.NoError(t, err)
	require.Empty(t, l.Items)
	require.Equal(t, MaxPerStore, l.Limit)

	a, err := e.svc.Create(e.ctx, e.staff, store, "  Counter ")
	require.NoError(t, err)
	require.Equal(t, "Counter", a.Campaign)
	require.Equal(t, store.String(), a.StoreId)
	require.Equal(t, "https://api.todayz.localhost/v1/public/ref/"+code+"?s="+a.Id, a.Link)
	require.Zero(t, a.Counts.Scans)
	require.Nil(t, a.LastScanAt)
	require.Nil(t, a.ReorderRequestedAt)
	require.Equal(t, 1, a.Version)
	require.Equal(t, 1, e.audits("standee.create", a.Id))

	e.clk.Advance(time.Minute)
	b, err := e.svc.Create(e.ctx, e.staff, store, "Diwali gate")
	require.NoError(t, err)
	l, err = e.svc.List(e.ctx, store)
	require.NoError(t, err)
	require.Len(t, l.Items, 2)
	require.Equal(t, b.Id, l.Items[0].Id, "newest first")

	for _, bad := range []string{"", "   ", "a\x00b", string(make([]rune, 41))} {
		_, err := e.svc.Create(e.ctx, e.staff, store, bad)
		require.Equal(t, genproblems.ValidationFailed, problemCode(err), "%q", bad)
	}
	_, err = e.svc.Create(e.ctx, e.staff, ids.New(testNow), "Counter")
	require.Equal(t, genproblems.NotFound, problemCode(err))
	_, err = e.svc.List(e.ctx, ids.New(testNow))
	require.Equal(t, genproblems.NotFound, problemCode(err))
}

// SC-ST-04: scans → sign-ups → qualified per standee, from the clicks and
// referrals, and the store's totals.
func TestCounts(t *testing.T) {
	e := newEnv(t)
	store, code := e.store()
	other, otherCode := e.store()
	a, err := e.svc.Create(e.ctx, e.staff, store, "Counter")
	require.NoError(t, err)
	b, err := e.svc.Create(e.ctx, e.staff, store, "Gate")
	require.NoError(t, err)
	o, err := e.svc.Create(e.ctx, e.staff, other, "Elsewhere")
	require.NoError(t, err)
	aid, bid, oid := mustID(t, a.Id), mustID(t, b.Id), mustID(t, o.Id)

	// a: 5 scans; sign-ups pending, qualified, rewarded, rejected,
	// rejected_same_device.
	var clicks []ids.ID
	for i := range 5 {
		clicks = append(clicks, e.scan(store, code, aid, testNow.Add(time.Duration(i)*time.Minute)))
	}
	for i, st := range []string{"pending", "qualified", "rewarded", "rejected", "rejected_same_device"} {
		e.signUp(store, code, aid, clicks[i], st)
	}
	// b: one scan, nobody signed up. The store's plain link clicks and the
	// other store's standee count for neither.
	e.scan(store, code, bid, testNow)
	e.exec(`INSERT INTO referral_clicks (id, code, referrer_type, referrer_id, landing, created_at) VALUES ($1, $2, 'store', $3, 'store', $4)`,
		ids.UUID(ids.New(testNow)), code, ids.UUID(store), testNow)
	e.signUp(other, otherCode, oid, e.scan(other, otherCode, oid, testNow), "qualified")

	l, err := e.svc.List(e.ctx, store)
	require.NoError(t, err)
	require.Len(t, l.Items, 2)
	byID := map[string]int{}
	for i, it := range l.Items {
		byID[it.Id] = i
	}
	ga, gb := l.Items[byID[a.Id]], l.Items[byID[b.Id]]
	require.Equal(t, 5, ga.Counts.Scans)
	require.Equal(t, 3, ga.Counts.SignUps, "rejected sign-ups are not counted")
	require.Equal(t, 2, ga.Counts.Qualified)
	require.NotNil(t, ga.LastScanAt)
	require.True(t, ga.LastScanAt.Equal(testNow.Add(4*time.Minute)))
	require.Equal(t, 1, gb.Counts.Scans)
	require.Zero(t, gb.Counts.SignUps)
	require.Equal(t, 6, l.Totals.Scans)
	require.Equal(t, 3, l.Totals.SignUps)
	require.Equal(t, 2, l.Totals.Qualified)

	// The business sees only its own standees.
	lo, err := e.svc.List(e.ctx, other)
	require.NoError(t, err)
	require.Len(t, lo.Items, 1)
	require.Equal(t, o.Id, lo.Items[0].Id)
	require.Equal(t, 1, lo.Totals.Qualified)
}

func TestLimit(t *testing.T) {
	e := newEnv(t)
	store, _ := e.store()
	// Concurrent creates around the limit: the store lock counts them in turn.
	for range MaxPerStore - 2 {
		_, err := e.svc.Create(e.ctx, e.staff, store, "Counter")
		require.NoError(t, err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.svc.Create(e.ctx, e.staff, store, "Rush") })
	}
	wg.Wait()
	var ok, full int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case problemCode(err) == genproblems.StandeeLimitReached:
			full++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	require.Equal(t, 2, ok)
	require.Equal(t, 4, full)
	l, err := e.svc.List(e.ctx, store)
	require.NoError(t, err)
	require.Len(t, l.Items, MaxPerStore)
}

func TestReorder(t *testing.T) {
	e := newEnv(t)
	store, _ := e.store()
	other, _ := e.store()
	a, err := e.svc.Create(e.ctx, e.staff, store, "Counter")
	require.NoError(t, err)
	aid := mustID(t, a.Id)

	e.clk.Advance(time.Hour)
	r, err := e.svc.RequestReorder(e.ctx, e.staff, store, aid)
	require.NoError(t, err)
	require.NotNil(t, r.ReorderRequestedAt)
	require.True(t, r.ReorderRequestedAt.Equal(testNow.Add(time.Hour)))
	require.Equal(t, 2, r.Version)
	require.Equal(t, 1, e.audits("standee.reorder_requested", a.Id))

	// Asking again while it waits changes and audits nothing.
	e.clk.Advance(time.Hour)
	r2, err := e.svc.RequestReorder(e.ctx, e.staff, store, aid)
	require.NoError(t, err)
	require.True(t, r2.ReorderRequestedAt.Equal(*r.ReorderRequestedAt))
	require.Equal(t, 2, r2.Version)
	require.Equal(t, 1, e.audits("standee.reorder_requested", a.Id))

	// Another store's path, or an unknown standee, is not found.
	_, err = e.svc.RequestReorder(e.ctx, e.staff, other, aid)
	require.Equal(t, genproblems.NotFound, problemCode(err))
	_, err = e.svc.RequestReorder(e.ctx, e.staff, store, ids.New(testNow))
	require.Equal(t, genproblems.NotFound, problemCode(err))
}

func TestNotConfigured(t *testing.T) {
	s, err := New(Deps{Clock: clock.NewFake(testNow)})
	require.NoError(t, err)
	_, err = s.List(context.Background(), ids.New(testNow))
	require.Equal(t, genproblems.ServiceUnavailable, problemCode(err))
	_, err = New(Deps{})
	require.Error(t, err)
}

func mustID(t *testing.T, s string) ids.ID {
	t.Helper()
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return id
}

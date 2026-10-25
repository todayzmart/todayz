package rollups

import (
	"context"
	"testing"
	"time"

	api "github.com/todayz/backend/gen/api/admin-pulse"
	"github.com/todayz/backend/gen/sqlc/rollupsdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// The seeded day is Tue 20 Oct 2026 (IST); the clock sits two days later so
// the day is final.
var (
	seedDay = Day(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
	testNow = time.Date(2026, 10, 22, 12, 0, 0, 0, clock.IST)
)

// at is h:m on the seeded day, IST.
func at(h, m int) time.Time { return time.Date(2026, 10, 20, h, m, 0, 0, clock.IST) }

type env struct {
	t   *testing.T
	ctx context.Context
	tdb *testkit.DB
	clk *clock.Fake
	svc *Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker")
	}
	tdb := testkit.NewDB(t)
	e := &env{t: t, ctx: context.Background(), tdb: tdb, clk: clock.NewFake(testNow)}
	e.svc = New(Deps{DB: db.New(tdb.Pool), Clock: e.clk,
		Settings: settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})})
	return e
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.tdb.OwnerPool.Exec(e.ctx, sql, args...); err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (e *env) count(sql string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.tdb.OwnerPool.QueryRow(e.ctx, sql, args...).Scan(&n); err != nil {
		e.t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

// fixture is one verified business with two stores (A live with 50 fresh
// items, B paused), three customers and a cashier.
type fixture struct {
	biz, storeA, storeB ids.ID
	c1, c2, c3, cashier ids.ID
}

func (e *env) order(f fixture, store, customer ids.ID, placed time.Time, status, reason string, accepted bool) ids.ID {
	e.t.Helper()
	// placed_at is part of an order's identity (orders_guard), so insert it
	// placed at that time and walk the allowed transitions.
	id := ids.New(placed)
	e.exec(`INSERT INTO orders (id, customer_id, store_id, fulfilment, subtotal_paise,
		  items_after_offers_paise, item_subtotal_paise, total_paise, code_enc, code_hmac, placed_at, created_at, updated_at)
		VALUES ($1, $2, $3, 'pickup', 100, 100, 100, 100, '\x00', sha256(($1::uuid)::text::bytea), $4, $4, $4)`,
		ids.UUID(id), ids.UUID(customer), ids.UUID(store), placed)
	if accepted {
		e.exec(`UPDATE orders SET status = 'accepted', accepted_at = $2 WHERE id = $1`, ids.UUID(id), placed.Add(90*time.Second))
	}
	if status != "placed" && status != "accepted" {
		var rs any
		if reason != "" {
			rs = reason
		}
		e.exec(`UPDATE orders SET status = $2, status_reason = $3 WHERE id = $1`, ids.UUID(id), status, rs)
	}
	return id
}

func (e *env) pos(store ids.ID, cashier ids.ID, sold time.Time, linkedTo ids.ID) {
	e.t.Helper()
	id := e.tdb.InsertPosSale(e.t, ids.Zero, store, cashier)
	e.exec(`UPDATE pos_sales SET sold_at = $2 WHERE id = $1`, ids.UUID(id), sold)
	if linkedTo != ids.Zero {
		e.exec(`UPDATE pos_sales SET link_method = 'bill_qr', link_status = 'linked', customer_id = $2 WHERE id = $1`,
			ids.UUID(id), ids.UUID(linkedTo))
	}
}

func (e *env) event(name, app string, when time.Time, user, store ids.ID) {
	e.t.Helper()
	var u, s any
	if user != ids.Zero {
		u = ids.UUID(user)
	}
	if store != ids.Zero {
		s = ids.UUID(store)
	}
	e.exec(`INSERT INTO events (id, name, occurred_at, app, user_id, store_id) VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)`,
		name, when, app, u, s)
}

func (e *env) search(when time.Time, query string, results int, cell int64) {
	e.t.Helper()
	e.exec(`INSERT INTO search_log (id, created_at, query_norm, results_count, store_count, radius_m, geo_cell)
		VALUES (gen_random_uuid(), $1, $2, $3, $3, 2000, $4)`, when, query, results, cell)
}

func (e *env) seed() fixture {
	e.t.Helper()
	f := fixture{biz: ids.New(testNow), storeA: ids.New(testNow), storeB: ids.New(testNow)}
	f.c1, f.c2, f.c3 = e.tdb.InsertUser(e.t, ids.Zero), e.tdb.InsertUser(e.t, ids.Zero), e.tdb.InsertUser(e.t, ids.Zero)
	f.cashier = e.tdb.InsertUser(e.t, ids.Zero)
	e.exec(`INSERT INTO businesses (id, brand_name, referral_code, status, verified_at) VALUES ($1, 'Lakshmi', 'BZRFR2', 'verified', $2)`,
		ids.UUID(f.biz), at(8, 0).Add(-10*24*time.Hour))
	e.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, city, status, live_at, location) VALUES
		($1, $3, 'lakshmi-a', 'STRAA2', 'Lakshmi A', 'Bengaluru', 'live', $4, 'SRID=4326;POINT(77.64 12.97)'),
		($2, $3, 'lakshmi-b', 'STRBB2', 'Lakshmi B', 'Bengaluru', 'paused', NULL, 'SRID=4326;POINT(77.65 12.98)')`,
		ids.UUID(f.storeA), ids.UUID(f.storeB), ids.UUID(f.biz), at(9, 0))
	// 50 listed items at store A, stock updated on the seeded day (live).
	e.exec(`INSERT INTO categories (id, slug, names, tint, icon) VALUES (gen_random_uuid(), 'rollup-test', '{"en": "Rollup test"}', 'green', 'package')`)
	e.exec(`INSERT INTO products (id, name, category_id, pack_size, unit)
		SELECT gen_random_uuid(), 'Item ' || n, (SELECT id FROM categories WHERE slug = 'rollup-test'), 1, 'pc'
		FROM generate_series(1, 50) n`)
	e.exec(`INSERT INTO store_items (id, store_id, product_id, price_paise, is_listed, stock_updated_at)
		SELECT gen_random_uuid(), $1, id, 100, true, $2 FROM products WHERE name LIKE 'Item %'`, ids.UUID(f.storeA), at(10, 0))

	// Orders on the day: 2 collected, 1 rejected, 1 expired unaccepted,
	// 1 cancelled, 1 still placed; one the day before and one the day after.
	e.order(f, f.storeA, f.c1, at(10, 0), "collected", "", true)
	e.order(f, f.storeA, f.c2, at(11, 0), "collected", "", true)
	e.order(f, f.storeA, f.c1, at(12, 0), "rejected", "out_of_stock", false)
	e.order(f, f.storeB, f.c2, at(13, 0), "expired", "not_accepted", false)
	e.order(f, f.storeB, f.c3, at(14, 0), "cancelled", "changed_mind", false)
	e.order(f, f.storeA, f.c3, at(23, 50), "placed", "", false)
	e.order(f, f.storeA, f.c1, at(0, 0).Add(-time.Minute), "collected", "", true) // 23:59 the day before
	e.order(f, f.storeA, f.c1, at(0, 0).Add(24*time.Hour), "collected", "", true) // 00:00 the next day

	// POS: one linked to c3, one walk-in, one the day after.
	e.pos(f.storeA, f.cashier, at(15, 0), f.c3)
	e.pos(f.storeA, f.cashier, at(16, 0), ids.Zero)
	e.pos(f.storeB, f.cashier, at(0, 0).Add(25*time.Hour), ids.Zero)

	// App opens: c1 twice and c2 on the day, c1 the day before, c2 on the
	// day before's previous day (outside dau, inside wau).
	e.event("app_opened", "customer", at(9, 0), f.c1, ids.Zero)
	e.event("app_opened", "customer", at(18, 0), f.c1, ids.Zero)
	e.event("app_opened", "customer", at(19, 0), f.c2, ids.Zero)
	e.event("app_opened", "customer", at(9, 0).Add(-24*time.Hour), f.c1, ids.Zero)
	e.event("app_opened", "customer", at(9, 0).Add(-48*time.Hour), f.c3, ids.Zero)
	e.event("biz_app_opened", "business", at(8, 30), ids.Zero, f.storeA)
	for i := 0; i < 3; i++ {
		e.event("store_viewed", "customer", at(10+i, 5), f.c1, f.storeA)
	}

	// Searches: 12 for "atta" in one cell (2 with no result), 3 for "kokum
	// syrup" with none, one the day after.
	for i := 0; i < 12; i++ {
		r := 4
		if i < 2 {
			r = 0
		}
		e.search(at(10, i), "atta", r, 0x8861892653fffff)
	}
	for i := 0; i < 3; i++ {
		e.search(at(11, i), "kokum syrup", 0, 0x8861892655fffff)
	}
	e.search(at(0, 0).Add(24*time.Hour+time.Hour), "atta", 4, 0x8861892653fffff)
	return f
}

func (e *env) platformDay(d Day) rollupsdb.RollupDailyPlatform {
	e.t.Helper()
	rows, err := rollupsdb.New(e.tdb.Pool).PlatformDays(e.ctx, rollupsdb.PlatformDaysParams{FromDay: d.pg(), ToDay: d.pg()})
	if err != nil || len(rows) != 1 {
		e.t.Fatalf("platform day %s: %v (%d rows)", d, err, len(rows))
	}
	return rows[0]
}

// SC-OB-11: the rollups of a seeded day match the raw tables' counts.
func TestRollupsMatchRawCountsForASeededDay(t *testing.T) {
	e := newEnv(t)
	f := e.seed()
	res, err := e.svc.ComputeDay(e.ctx, seedDay)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ComputeDemand(e.ctx, seedDay); err != nil {
		t.Fatal(err)
	}
	if res.Stores != 2 {
		t.Errorf("store rows = %d, want 2", res.Stores)
	}
	r := e.platformDay(seedDay)
	start, end := seedDay.Start(), seedDay.End()

	raw := func(sql string) int64 { return e.count(sql, start, end) }
	checks := []struct {
		name      string
		got, want int64
	}{
		{"orders_placed", int64(r.OrdersPlaced), raw(`SELECT count(*) FROM orders WHERE placed_at >= $1 AND placed_at < $2`)},
		{"orders_completed", int64(r.OrdersCompleted), raw(`SELECT count(*) FROM orders WHERE placed_at >= $1 AND placed_at < $2 AND status IN ('collected','delivered')`)},
		{"orders_accepted", int64(r.OrdersAccepted), raw(`SELECT count(*) FROM orders WHERE placed_at >= $1 AND placed_at < $2 AND accepted_at IS NOT NULL`)},
		{"orders_rejected", int64(r.OrdersRejected), raw(`SELECT count(*) FROM orders WHERE placed_at >= $1 AND placed_at < $2 AND status = 'rejected'`)},
		{"orders_expired_unaccepted", int64(r.OrdersExpiredUnaccepted), raw(`SELECT count(*) FROM orders WHERE placed_at >= $1 AND placed_at < $2 AND status = 'expired' AND status_reason = 'not_accepted'`)},
		{"orders_cancelled", int64(r.OrdersCancelled), raw(`SELECT count(*) FROM orders WHERE placed_at >= $1 AND placed_at < $2 AND status = 'cancelled'`)},
		{"orders_open", int64(r.OrdersOpen), raw(`SELECT count(*) FROM orders WHERE placed_at >= $1 AND placed_at < $2 AND status = 'placed'`)},
		{"gmv_paise", r.GmvPaise, raw(`SELECT coalesce(sum(total_paise), 0) FROM orders WHERE placed_at >= $1 AND placed_at < $2 AND status IN ('collected','delivered')`)},
		{"pos_sales", int64(r.PosSales), raw(`SELECT count(*) FROM pos_sales WHERE sold_at >= $1 AND sold_at < $2 AND status = 'completed'`)},
		{"pos_sales_linked", int64(r.PosSalesLinked), raw(`SELECT count(*) FROM pos_sales WHERE sold_at >= $1 AND sold_at < $2 AND link_status = 'linked'`)},
		{"pos_gmv_paise", r.PosGmvPaise, raw(`SELECT coalesce(sum(total_paise), 0) FROM pos_sales WHERE sold_at >= $1 AND sold_at < $2 AND link_status = 'linked'`)},
		{"pos_unlinked_paise", r.PosUnlinkedPaise, raw(`SELECT coalesce(sum(total_paise), 0) FROM pos_sales WHERE sold_at >= $1 AND sold_at < $2 AND link_status IS NULL`)},
		{"dau", int64(r.Dau), raw(`SELECT count(DISTINCT user_id) FROM events WHERE name = 'app_opened' AND occurred_at >= $1 AND occurred_at < $2`)},
		{"transacting_customers", int64(r.TransactingCustomers), raw(`SELECT count(DISTINCT c) FROM (
			SELECT customer_id c FROM orders WHERE placed_at >= $1 AND placed_at < $2 AND status IN ('collected','delivered')
			UNION SELECT customer_id FROM pos_sales WHERE sold_at >= $1 AND sold_at < $2 AND link_status = 'linked') x`)},
		{"searches", int64(r.Searches), raw(`SELECT count(*) FROM search_log WHERE created_at >= $1 AND created_at < $2`)},
		{"searches_zero", int64(r.SearchesZero), raw(`SELECT count(*) FROM search_log WHERE created_at >= $1 AND created_at < $2 AND results_count = 0`)},
		{"store_views", int64(r.StoreViews), raw(`SELECT count(*) FROM events WHERE name = 'store_viewed' AND occurred_at >= $1 AND occurred_at < $2`)},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, raw count %d", c.name, c.got, c.want)
		}
	}
	// And the fixture's own numbers, so a raw query that is wrong in the
	// same way as the rollup cannot hide it.
	want := map[string]int64{"orders_placed": 6, "orders_completed": 2, "gmv_paise": 200, "pos_sales": 2, "pos_sales_linked": 1,
		"dau": 2, "transacting_customers": 3, "searches": 15, "searches_zero": 5, "store_views": 3}
	for _, c := range checks {
		if w, ok := want[c.name]; ok && c.got != w {
			t.Errorf("%s = %d, the fixture has %d", c.name, c.got, w)
		}
	}
	if r.Wau != 3 || r.Mau != 3 {
		t.Errorf("wau, mau = %d, %d; want 3, 3", r.Wau, r.Mau)
	}
	if r.ZeroResultRateBp == nil || *r.ZeroResultRateBp != 3333 {
		t.Errorf("zero_result_rate_bp = %v, want 3333", r.ZeroResultRateBp)
	}
	if r.RetD1Base != 1 || r.RetD1Back != 1 {
		t.Errorf("d1 retention = %d/%d, want 1/1 (c1 first opened the day before)", r.RetD1Back, r.RetD1Base)
	}
	if r.VerifiedStores != 2 || r.ActiveStores != 1 || r.LiveStores != 1 || r.NewStores != 1 {
		t.Errorf("stores verified/active/live/new = %d/%d/%d/%d, want 2/1/1/1", r.VerifiedStores, r.ActiveStores, r.LiveStores, r.NewStores)
	}
	if !r.Final {
		t.Error("a past day must be final")
	}

	// Store A: 4 orders on the day (2 collected, 1 rejected, 1 placed), 2 POS sales (1 linked).
	var orders, completed, posSales, linked, conv, views int32
	var live, active bool
	var p50 *int32
	if err := e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT orders, completed, pos_sales, pos_sales_linked, conversions, views, live, active, p50_accept_seconds
		FROM rollup_daily_store WHERE date = $1 AND store_id = $2`, seedDay.pg(), ids.UUID(f.storeA)).
		Scan(&orders, &completed, &posSales, &linked, &conv, &views, &live, &active, &p50); err != nil {
		t.Fatal(err)
	}
	if orders != 4 || completed != 2 || posSales != 2 || linked != 1 || conv != 3 || views != 3 || !live || !active {
		t.Errorf("store A = orders %d completed %d pos %d linked %d conv %d views %d live %v active %v",
			orders, completed, posSales, linked, conv, views, live, active)
	}
	if p50 == nil || *p50 != 90 {
		t.Errorf("store A p50 accept = %v, want 90 s", p50)
	}

	// Demand: one row per (cell, query); the next day's search is not in it.
	if n := e.count(`SELECT sum(searches) FROM rollup_daily_search_demand WHERE date = $1`, seedDay.pg()); n != 15 {
		t.Errorf("demand searches = %d, want 15", n)
	}
	if n := e.count(`SELECT zero_results FROM rollup_daily_search_demand WHERE date = $1 AND query_norm = 'kokum syrup'`, seedDay.pg()); n != 3 {
		t.Errorf("kokum syrup zero results = %d, want 3", n)
	}
}

// A rerun, or new rows arriving late, rewrites the same day in place.
func TestRecomputeIsIdempotentAndPicksUpLateRows(t *testing.T) {
	e := newEnv(t)
	f := e.seed()
	for i := 0; i < 2; i++ {
		if _, err := e.svc.ComputeDay(e.ctx, seedDay); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.count(`SELECT count(*) FROM rollup_daily_platform`); n != 1 {
		t.Fatalf("platform rows = %d, want 1", n)
	}
	first := e.platformDay(seedDay)
	e.pos(f.storeB, f.cashier, at(20, 0), f.c1) // an offline sale synced late
	if _, err := e.svc.ComputeDay(e.ctx, seedDay); err != nil {
		t.Fatal(err)
	}
	again := e.platformDay(seedDay)
	if again.PosSales != first.PosSales+1 || again.PosSalesLinked != first.PosSalesLinked+1 {
		t.Errorf("late sale: pos %d→%d linked %d→%d", first.PosSales, again.PosSales, first.PosSalesLinked, again.PosSalesLinked)
	}
}

func TestTodayIsPartialAndUsesTheClock(t *testing.T) {
	e := newEnv(t)
	e.seed()
	e.clk.Set(at(12, 30))
	if _, err := e.svc.ComputeDay(e.ctx, seedDay); err != nil {
		t.Fatal(err)
	}
	r := e.platformDay(seedDay)
	if r.Final {
		t.Error("today's row must not be final")
	}
	if !r.ComputedAt.Equal(at(12, 30)) {
		t.Errorf("computed_at = %v, want the clock", r.ComputedAt)
	}
}

func TestPulse(t *testing.T) {
	e := newEnv(t)
	f := e.seed()
	e.clk.Set(at(23, 55)) // today is the seeded day
	if _, err := e.svc.Backfill(e.ctx, seedDay.Add(-7), seedDay); err != nil {
		t.Fatal(err)
	}
	rng := api.N7d
	p, err := e.svc.Pulse(e.ctx, &rng)
	if err != nil {
		t.Fatal(err)
	}
	if p.From.Time != time.Time(seedDay.Add(-6)) || p.To.Time != time.Time(seedDay) {
		t.Errorf("period = %v..%v", p.From, p.To)
	}
	if p.Kpis.Orders.Value != 7 { // 6 on the day and 1 the day before
		t.Errorf("orders = %d, want 7", p.Kpis.Orders.Value)
	}
	if p.Kpis.GmvPaise.Value != 300+100 || p.Kpis.PosGmvPaise.Value != 100 || p.Kpis.OnlineGmvPaise.Value != 300 {
		t.Errorf("gmv = %d (online %d, pos %d)", p.Kpis.GmvPaise.Value, p.Kpis.OnlineGmvPaise.Value, p.Kpis.PosGmvPaise.Value)
	}
	if p.Kpis.Orders.Previous == nil || *p.Kpis.Orders.Previous != 0 {
		t.Errorf("previous orders = %v, want 0 (the backfilled day before the period)", p.Kpis.Orders.Previous)
	}
	if p.Kpis.SuccessRateBp.Value == nil || *p.Kpis.SuccessRateBp.Value != 3*10000/7 {
		t.Errorf("success rate = %v", p.Kpis.SuccessRateBp.Value)
	}
	if p.Kpis.ActiveCustomers.Value != 3 {
		t.Errorf("active customers = %d, want 3", p.Kpis.ActiveCustomers.Value)
	}
	if p.AsOf == nil || !p.AsOf.Equal(at(23, 55)) {
		t.Errorf("asOf = %v", p.AsOf)
	}
	if len(p.Trend) != 7 {
		t.Errorf("trend days = %d, want 7", len(p.Trend))
	}
	// k ≥ 10: "atta" has 2 zero results, "kokum syrup" 3, so neither shows.
	if len(p.ZeroResultSearches) != 0 {
		t.Errorf("zero-result searches = %v, want none under k", p.ZeroResultSearches)
	}
	leaks := map[string]int{}
	for _, l := range p.Leaks {
		leaks[string(l.Status)] += l.Count
	}
	if leaks["rejected"] != 1 || leaks["expired"] != 1 || leaks["cancelled"] != 1 {
		t.Errorf("leaks = %v", leaks)
	}

	m, err := e.svc.Map(e.ctx, &rng)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Stores) != 2 {
		t.Errorf("map stores = %d, want 2", len(m.Stores))
	}
	if len(m.Demand) != 1 || m.Demand[0].Cell != "8861892653fffff" || m.Demand[0].Searches != 12 {
		t.Errorf("demand = %+v, want the one cell with ≥ 10 searches", m.Demand)
	}
	for _, s := range m.Stores {
		if s.StoreId == f.storeA.String() && (!s.Live || s.Status != api.Live) {
			t.Errorf("store A on the map = %+v", s)
		}
	}
}

func TestStoresNeedingAttention(t *testing.T) {
	e := newEnv(t)
	f := e.seed()
	e.clk.Set(at(15, 0))
	for i := 0; i < 3; i++ {
		e.order(f, f.storeB, f.c1, at(14, 10+i), "expired", "not_accepted", false)
	}
	rng := api.Today
	p, err := e.svc.Pulse(e.ctx, &rng)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.StoresNeedingAttention) == 0 || p.StoresNeedingAttention[0].StoreId != f.storeB.String() ||
		p.StoresNeedingAttention[0].Reason != api.NotAccepting || p.StoresNeedingAttention[0].ExpiredUnaccepted != 4 {
		t.Fatalf("attention = %+v, want store B not accepting with 4 expired", p.StoresNeedingAttention)
	}
}

func TestItemPairsAndCohorts(t *testing.T) {
	e := newEnv(t)
	e.seed()
	if _, err := e.svc.ComputeItemPairs(e.ctx, seedDay.Add(1)); err != nil {
		t.Fatal(err) // no POS lines in the fixture: an empty, valid rebuild
	}
	n, err := e.svc.ComputeCohorts(e.ctx, seedDay)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no cohort rows")
	}
	// c1 (19 Oct 23:59), c2 (an order on the 20th) and c3 (a linked POS sale on
	// the 20th) all first transacted in the ISO week of Monday 19 Oct.
	if s := e.count(`SELECT cohort_size FROM rollup_cohort_weekly WHERE kind = 'customer' AND cohort_week = '2026-10-19' AND week_n = 0`); s != 3 {
		t.Errorf("customer cohort size = %d, want 3", s)
	}
}

func TestDayHelpers(t *testing.T) {
	d := DayOf(time.Date(2026, 10, 19, 19, 0, 0, 0, time.UTC)) // 00:30 IST on the 20th
	if d != seedDay {
		t.Fatalf("DayOf = %s, want 2026-10-20", d)
	}
	if !d.Start().Equal(time.Date(2026, 10, 19, 18, 30, 0, 0, time.UTC)) || !d.End().Equal(time.Date(2026, 10, 20, 18, 30, 0, 0, time.UTC)) {
		t.Errorf("start, end = %v, %v", d.Start(), d.End())
	}
	if d.Monday().String() != "2026-10-19" || d.Add(5).Monday().String() != "2026-10-19" || d.Add(6).Monday().String() != "2026-10-26" {
		t.Errorf("Monday: %s %s %s", d.Monday(), d.Add(5).Monday(), d.Add(6).Monday())
	}
	if got := asOf(d, d.End().Add(time.Hour)); !got.Equal(d.End()) {
		t.Errorf("asOf past day = %v", got)
	}
	if _, err := ParseDay("2026-13-01"); err == nil {
		t.Error("ParseDay accepted a bad date")
	}
	for r, n := range map[api.AdminPulseRange]int{api.Today: 1, api.N7d: 7, api.N30d: 30, api.N90d: 90} {
		if got, err := rangeDays(r); err != nil || got != n {
			t.Errorf("rangeDays(%s) = %d, %v", r, got, err)
		}
	}
	if b := bp(1, 3); b == nil || *b != 3333 {
		t.Errorf("bp(1,3) = %v", b)
	}
	if bp(1, 0) != nil {
		t.Error("bp with no denominator must be nil")
	}
}

package search

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	"github.com/todayz/backend/gen/events"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/seed"
)

// recorder keeps the last SQL text and arguments of each sqlc query by name,
// and how long each query name took (total, calls, slowest call).
type recorder struct {
	mu   sync.Mutex
	seen map[string]recorded
	took map[string]*timing
}

type recorded struct {
	sql  string
	args []any
}

type timing struct {
	calls      int
	total, max time.Duration
	slowest    recorded
}

type traceKey struct{}

type traceStart struct {
	name  string
	rec   recorded
	start time.Time
}

func (r *recorder) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if rest, ok := strings.CutPrefix(d.SQL, "-- name: "); ok {
		name, _, _ := strings.Cut(rest, " ")
		rec := recorded{sql: d.SQL, args: d.Args}
		r.mu.Lock()
		r.seen[name] = rec
		r.mu.Unlock()
		return context.WithValue(ctx, traceKey{}, traceStart{name: name, rec: rec, start: time.Now()})
	}
	return ctx
}

func (r *recorder) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	s, ok := ctx.Value(traceKey{}).(traceStart)
	if !ok {
		return
	}
	d := time.Since(s.start)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.took == nil {
		r.took = map[string]*timing{}
	}
	tm := r.took[s.name]
	if tm == nil {
		tm = &timing{}
		r.took[s.name] = tm
	}
	tm.calls++
	tm.total += d
	if d > tm.max {
		tm.max, tm.slowest = d, s.rec
	}
}

// tracedService is a Service on a second pool whose queries rec records.
func tracedService(t *testing.T, f *qa) (*Service, *recorder) {
	t.Helper()
	rec := &recorder{seen: map[string]recorded{}}
	cfg := f.tdb.Pool.Config()
	cfg.ConnConfig.Tracer = rec
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	cat, err := eventcat.Load(events.FS)
	require.NoError(t, err)
	svc, err := New(Deps{DB: db.New(pool), Settings: f.cache, Clock: f.clk, Open: fakeOpen}, cat)
	require.NoError(t, err)
	return svc, rec
}

// explain is the plan of a recorded query with sequential scans priced out,
// so it names an index whenever the query can use one.
func (w *world) explain(rec recorded) string {
	w.t.Helper()
	tx, err := w.tdb.OwnerPool.Begin(w.ctx)
	require.NoError(w.t, err)
	defer func() { _ = tx.Rollback(w.ctx) }()
	_, err = tx.Exec(w.ctx, `SET LOCAL enable_seqscan = off`)
	require.NoError(w.t, err)
	rows, err := tx.Query(w.ctx, "EXPLAIN "+rec.sql, rec.args...)
	require.NoError(w.t, err)
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		require.NoError(w.t, rows.Scan(&line))
		b.WriteString(line)
		b.WriteByte('\n')
	}
	require.NoError(w.t, rows.Err())
	return b.String()
}

// checkIndexes runs every discover query once through a pool that records the
// SQL sqlc sent, then EXPLAINs each with its arguments and checks the index
// that keeps it off a full scan (02 §9: search is an index walk from the
// customer's point).
func checkIndexes(t *testing.T, f *qa) {
	svc, rec := tracedService(t, f)
	ctx := f.ctx
	_, err := svc.Search(ctx, discoverapi.SearchDiscoverParams{Q: "Amul Butter 100 g", Lat: qaLat, Lng: qaLng})
	require.NoError(t, err)
	_, err = svc.Search(ctx, discoverapi.SearchDiscoverParams{Q: "kuch meetha", Lat: qaLat, Lng: qaLng}) // a synonym miss: CatalogueKnows
	require.NoError(t, err)
	_, err = svc.Search(ctx, discoverapi.SearchDiscoverParams{Q: "8901262150019", Lat: qaLat, Lng: qaLng})
	require.NoError(t, err)
	_, err = svc.Summary(ctx, discoverapi.GetDiscoverSummaryParams{Lat: qaLat, Lng: qaLng})
	require.NoError(t, err)
	_, err = svc.ListStores(ctx, discoverapi.ListDiscoverStoresParams{Lat: qaLat, Lng: qaLng})
	require.NoError(t, err)
	_, err = svc.SearchPlaces(ctx, discoverapi.SearchPlacesParams{Q: "indira"})
	require.NoError(t, err)
	_, err = svc.SearchPlaces(ctx, discoverapi.SearchPlacesParams{Q: "5600"})
	require.NoError(t, err)
	_, err = svc.ReversePlace(ctx, discoverapi.ReversePlaceParams{Lat: qaLat, Lng: qaLng})
	require.NoError(t, err)
	_, err = svc.Suggest(ctx, discoverapi.SearchSuggestParams{Q: "amu"})
	require.NoError(t, err)
	_, err = svc.ListStoreItems(ctx, "near", discoverapi.ListDiscoverStoreItemsParams{Q: sp("dahi")})
	require.NoError(t, err)

	want := map[string][]string{
		"SearchItems":           {"ix_stores_location"},
		"CountStoresInRadius":   {"ix_stores_location"},
		"CountListingsInRadius": {"ix_stores_location"},
		"StoresInRadius":        {"ix_stores_location"},
		"NearestPlace":          {"ix_places_location"},
		"PlacesByName":          {"ix_places_name_trgm"},
		"PlacesByPincode":       {"ix_places_pincode"},
		"TypoProducts":          {"ix_products_name_trgm"},
		"SuggestProducts":       {"ix_products_name_trgm"},
		"ProductsByWords":       {"ix_products_search"},
		"CatalogueKnows":        {"ix_products_search"},
		"ProductsByBarcode":     {"ix_products_barcodes"},
		"StoreItemsPage":        {"store_items_store"},
	}
	for name, idx := range want {
		r, ok := rec.seen[name]
		if !assert.True(t, ok, "%s never ran", name) {
			continue
		}
		plan := f.explain(r)
		t.Logf("%s:\n%s", name, plan)
		for _, i := range idx {
			assert.Contains(t, plan, i, "%s:\n%s", name, plan)
		}
	}
}

// TestSearchAtScale builds a city: the dev seed plus 1,000 synthetic live
// stores around the QA point, each of its own verified business, 30-odd items
// each. On it every discover query must walk an index, and p95 of search,
// summary and the store list must stay under 300 ms (01 §11, the SC-SR
// latency line). The index checks need the city: on the tiny QA fixture the
// planner rightly prefers other plans.
func TestSearchAtScale(t *testing.T) {
	f := newQA(t)
	ctx := f.ctx
	require.NoError(t, seed.Run(ctx, config.EnvTest, seed.Dev, seed.Deps{
		DB: db.New(f.tdb.Pool), Clock: clock.NewFake(testNow), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))

	words := []string{"Rice", "Atta", "Milk", "Curd", "Butter", "Ghee", "Sugar", "Salt", "Oil", "Tea",
		"Coffee", "Soap", "Biscuits", "Bread", "Eggs", "Paneer", "Toor Dal", "Poha", "Rava", "Jaggery"}
	f.exec(`INSERT INTO products (id, name, category_id, pack_size, unit)
	  SELECT md5('synthetic-product-' || g)::uuid,
	    ($1::text[])[1 + g % 20] || ' ' || (ARRAY['Classic','Gold','Fresh','Premium','Daily','Pure'])[1 + (g / 20) % 6] || ' ' || g,
	    $2::uuid, 1 + g % 5, 'kg'
	  FROM generate_series(1, 600) g`, words, catStaples)

	const synthetic = 1000
	slugs := make([]string, synthetic)
	codes := make([]string, synthetic)
	refs := make([]string, synthetic)
	lats := make([]float64, synthetic)
	lngs := make([]float64, synthetic)
	for i := range synthetic {
		// A deterministic spiral out to ~12 km: dense near the point, like a city.
		r := 12000 * math.Sqrt(float64(i+1)/synthetic)
		a := float64(i) * 2.399963 // the golden angle
		lats[i], lngs[i] = offset(r*math.Cos(a), r*math.Sin(a))
		slugs[i] = fmt.Sprintf("synthetic-%04d", i)
		codes[i] = shortCode(100000 + i)
		refs[i] = shortCode(200000 + i)
	}
	f.exec(`INSERT INTO businesses (id, brand_name, referral_code, status, category_id)
	  SELECT md5('business-' || v.slug)::uuid, 'Biz ' || v.slug, v.ref, 'verified', $3::uuid
	  FROM unnest($1::text[], $2::text[]) AS v(slug, ref)`, slugs, refs, catGrocery)
	f.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, locality, city, location, hours, status, live_at, accepts_pickup,
	    accepts_delivery, delivery_fee_paise)
	  SELECT md5(v.slug)::uuid, md5('business-' || v.slug)::uuid, v.slug, v.code, 'Shop ' || v.slug, 'Indiranagar', 'Bengaluru',
	    ST_SetSRID(ST_MakePoint(v.lng, v.lat), 4326)::geography, $5::jsonb, 'live', $6::timestamptz, true, v.n % 3 = 0, 2000
	  FROM unnest($1::text[], $2::text[], $3::float8[], $4::float8[]) WITH ORDINALITY AS v(slug, code, lat, lng, n)`,
		slugs, codes, lats, lngs, allDay, testNow.Add(-24*time.Hour))
	f.exec(`INSERT INTO store_items (id, store_id, product_id, price_paise, on_hand, stock_updated_at, search)
	  SELECT md5(s.slug || p.id::text)::uuid, s.id, p.id, 1000 + (abs(hashtext(s.slug || p.name)) % 50000), 1 + abs(hashtext(p.name)) % 20,
	    $1::timestamptz, p.search
	  FROM stores s JOIN products p ON abs(hashtext(s.slug || p.id::text)) % 20 = 0
	  WHERE s.slug LIKE 'synthetic-%'`, testNow.Add(-time.Hour))
	f.exec(`ANALYZE`)

	var n int
	require.NoError(t, f.tdb.OwnerPool.QueryRow(ctx, `SELECT count(*) FROM store_items si JOIN stores s ON s.id = si.store_id WHERE s.slug LIKE 'synthetic-%'`).Scan(&n))
	t.Logf("synthetic: %d stores, %d listings", synthetic, n)
	require.Greater(t, n, synthetic*20)

	t.Run("indexes", func(t *testing.T) { checkIndexes(t, f) })
	t.Run("latency", func(t *testing.T) { checkLatency(t, f) })
}

// checkLatency times 14 queries at two radii twice, plus the summary and the
// store list, and asserts p95 < 300 ms (latencyBudget).
func checkLatency(t *testing.T, f *qa) {
	ctx := f.ctx
	queries := []string{"curd", "amul butter 100 g", "rice", "atta 5 kg", "milk", "ghee", "dahi", "biscuits",
		"amull buttr", "sugar 1 kg", "toor dal", "classic", "8901262150019", "something for breakfast"}
	radii := []discoverapi.SearchDiscoverParamsRadius{
		discoverapi.SearchDiscoverParamsRadiusN2000, discoverapi.SearchDiscoverParamsRadiusN5000,
	}
	// The tracer costs microseconds per query; it names the slow query when
	// the target is missed.
	svc, rec := tracedService(t, f)
	type sample struct {
		label string
		d     time.Duration
	}
	var took []sample
	measure := func(label string, fn func() error) {
		start := time.Now()
		require.NoError(t, fn())
		took = append(took, sample{label, time.Since(start)})
	}
	// Warm the settings cache and the connection pool.
	_, err := svc.Search(ctx, discoverapi.SearchDiscoverParams{Q: "curd", Lat: qaLat, Lng: qaLng})
	require.NoError(t, err)
	rec.mu.Lock()
	rec.took = nil
	rec.mu.Unlock()
	for range 2 {
		for _, q := range queries {
			for _, r := range radii {
				p := discoverapi.SearchDiscoverParams{Q: q, Lat: qaLat, Lng: qaLng, Radius: &r}
				measure(fmt.Sprintf("search %q r=%d", q, r), func() error { _, err := svc.Search(ctx, p); return err })
			}
		}
		measure("summary", func() error {
			_, err := svc.Summary(ctx, discoverapi.GetDiscoverSummaryParams{Lat: qaLat, Lng: qaLng})
			return err
		})
		measure("stores open now", func() error {
			_, err := svc.ListStores(ctx, discoverapi.ListDiscoverStoresParams{Lat: qaLat, Lng: qaLng, OpenNow: bp(true)})
			return err
		})
	}
	sort.Slice(took, func(i, j int) bool { return took[i].d < took[j].d })
	p50 := took[len(took)/2].d
	p95 := took[int(math.Ceil(0.95*float64(len(took))))-1].d
	t.Logf("latency over %d requests: p50 %v, p95 %v, max %v", len(took), p50, p95, took[len(took)-1].d)
	for _, s := range took[len(took)-6:] {
		t.Logf("  slow request %-36s %v", s.label, s.d)
	}
	rec.mu.Lock()
	names := make([]string, 0, len(rec.took))
	for n := range rec.took {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return rec.took[names[i]].total > rec.took[names[j]].total })
	var worst []recorded
	for i, n := range names {
		tm := rec.took[n]
		t.Logf("  query %-26s calls %3d total %9v max %9v", n, tm.calls, tm.total.Round(time.Microsecond), tm.max.Round(time.Microsecond))
		if i < 2 {
			worst = append(worst, tm.slowest)
		}
	}
	rec.mu.Unlock()
	if !assert.Less(t, p95, latencyBudget(t)) {
		for _, w := range worst {
			t.Logf("EXPLAIN (ANALYZE, BUFFERS) of the slowest call\n%s\n%s", w.sql, f.analyze(w))
		}
	}
}

// latencyBudget is the p95 the latency subtest gates on: 300 ms, the W1-06
// "p95 < 300 ms local" line. A shared GitHub-hosted runner (2 vCPU, noisy
// neighbours) measured 387 ms on the same data, so there the gate is a
// regression ceiling of twice that, which still catches a lost index (a
// sequential scan over the 180k listings takes seconds). The 300 ms itself is
// held by `make check` on a laptop and by the k6 browse_peak scenario (D-215).
func latencyBudget(t *testing.T) time.Duration {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") == "true" && os.Getenv("RUNNER_ENVIRONMENT") == "github-hosted" {
		t.Log("GitHub-hosted runner: gating p95 at the 600 ms regression ceiling, not the 300 ms local budget (D-215)")
		return 600 * time.Millisecond
	}
	return 300 * time.Millisecond
}

// analyze is EXPLAIN (ANALYZE, BUFFERS) of a recorded query under the
// planner's normal settings.
func (w *world) analyze(rec recorded) string {
	w.t.Helper()
	rows, err := w.tdb.OwnerPool.Query(w.ctx, "EXPLAIN (ANALYZE, BUFFERS) "+rec.sql, rec.args...)
	require.NoError(w.t, err)
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		require.NoError(w.t, rows.Scan(&line))
		b.WriteString(line)
		b.WriteByte('\n')
	}
	require.NoError(w.t, rows.Err())
	return b.String()
}

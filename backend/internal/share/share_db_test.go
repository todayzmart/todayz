package share

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/require"

	admindemandapi "github.com/todayz/backend/gen/api/admin-demand"
	shareapi "github.com/todayz/backend/gen/api/share"
	"github.com/todayz/backend/gen/events"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/geo/h3"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// 12:00 IST on a Friday.
var testNow = time.Date(2026, 10, 23, 6, 30, 0, 0, time.UTC)

// Seeded by 10501.
const (
	catGrocery   = "0190c47e-0000-7000-8000-000000000100"
	catStaples   = "0190c47e-0000-7000-8000-000000000101"
	catMedicines = "0190c47e-0000-7000-8000-000000000201"
)

const qaLat, qaLng = 12.9719, 77.6412

type memStore struct {
	mu   sync.Mutex
	puts map[string]int
}

func (m *memStore) Put(_ context.Context, key, contentType string, body []byte) error {
	if contentType != "image/png" || len(body) == 0 {
		return errors.New("bad put")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts[key]++
	return nil
}

func (m *memStore) count(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.puts[key]
}

type world struct {
	t      *testing.T
	tdb    *testkit.DB
	svc    *Service
	clk    *clock.Fake
	images *memStore
	admin  ids.ID
	n      int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	tdb := testkit.NewDB(t)
	cat, err := eventcat.Load(events.FS)
	require.NoError(t, err)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	h, err := pii.NewHasher(key)
	require.NoError(t, err)
	w := &world{t: t, tdb: tdb, clk: clock.NewFake(testNow), images: &memStore{puts: map[string]int{}}}
	w.exec(`UPDATE platform_settings SET value = '5' WHERE key = 'share_card_create_per_hour'`)
	w.admin = w.user()
	cache := settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	w.svc, err = New(Deps{
		DB: db.New(tdb.Pool), Clock: w.clk, Settings: cache, Events: cat, Hash: h, IPHashKey: key,
		Images: w.images, AppURL: "https://app.todayz.localhost", ImageURL: "https://img.todayz.localhost",
	})
	require.NoError(t, err)
	return w
}

func (w *world) exec(sql string, args ...any) {
	w.t.Helper()
	_, err := w.tdb.OwnerPool.Exec(context.Background(), sql, args...)
	require.NoError(w.t, err, sql)
}

func (w *world) count(sql string, args ...any) int {
	w.t.Helper()
	var n int
	require.NoError(w.t, w.tdb.OwnerPool.QueryRow(context.Background(), sql, args...).Scan(&n), sql)
	return n
}

func (w *world) id() ids.ID {
	w.n++
	return ids.New(testNow)
}

func (w *world) user() ids.ID {
	id := w.id()
	w.exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(id))
	return id
}

func (w *world) city() ids.ID {
	var u pgtype.UUID
	require.NoError(w.t, w.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT id FROM places WHERE kind = 'city' AND name = 'Bengaluru'`).Scan(&u))
	return idOf(u)
}

func shortCode(n int) string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	for i := 5; i >= 0; i-- {
		b[i] = alphabet[n%len(alphabet)]
		n /= len(alphabet)
	}
	return string(b)
}

func (w *world) store(slug string) ids.ID {
	biz, id := w.id(), w.id()
	w.exec(`INSERT INTO businesses (id, brand_name, referral_code, status, category_id) VALUES ($1, 'Biz', $2, 'verified', $3)`,
		ids.UUID(biz), shortCode(w.n), catGrocery)
	w.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, locality, city, location, hours, status, live_at,
	    accepts_pickup, accepts_delivery, delivery_fee_paise)
	  VALUES ($1, $2, $3, $4, $5, 'Indiranagar', 'Bengaluru', ST_SetSRID(ST_MakePoint($6, $7), 4326)::geography,
	    '{"weekly":{},"holidays":[]}'::jsonb, 'live', $8, true, false, 0)`,
		ids.UUID(id), ids.UUID(biz), slug, shortCode(w.n+1000), "Shop "+slug, qaLng, qaLat, testNow.Add(-24*time.Hour))
	return id
}

func (w *world) product(name, category string) ids.ID {
	id := w.id()
	w.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, barcodes, mrp_paise) VALUES ($1, $2, $3, 1, 'kg', '{}', 9900)`,
		ids.UUID(id), name, category)
	return id
}

func (w *world) item(store, product ids.ID, price int64) ids.ID {
	id := w.id()
	w.exec(`INSERT INTO store_items (id, store_id, product_id, price_paise, on_hand, is_listed, stock_updated_at)
	  VALUES ($1, $2, $3, $4, 7, true, $5)`, ids.UUID(id), ids.UUID(store), ids.UUID(product), price, testNow.Add(-48*time.Hour))
	return id
}

// searches logs n searches of product yesterday from the QA point, by user
// (nil for anonymous).
func (w *world) searches(product ids.ID, user *ids.ID, n int) {
	cell, err := h3.LatLngToCell(qaLat, qaLng, 8)
	require.NoError(w.t, err)
	var u any
	if user != nil {
		u = ids.UUID(*user)
	}
	for range n {
		w.exec(`INSERT INTO search_log (id, created_at, query_norm, results_count, store_count, radius_m, geo_cell, product_id, user_id)
		  VALUES ($1, $2, 'q', 1, 1, 3000, $3, $4, $5)`,
			ids.UUID(w.id()), testNow.Add(-20*time.Hour), cell.Int64(), ids.UUID(product), u)
	}
}

func (w *world) asAdmin() context.Context {
	return authn.WithPrincipal(context.Background(), authn.Principal{UserID: w.admin, App: authn.AppAdmin})
}

func code(t *testing.T, err error) string {
	t.Helper()
	var p *problem.Error
	require.ErrorAs(t, err, &p)
	return p.Code
}

// SC-ASK-2x: the strip counts distinct signed-in searchers only (anonymous
// searches never reach the floor), never below 10, and leaves out the denylist.
func TestMarqueeFloorDenylistAndApproval(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	atta := w.product("Atta", catStaples)
	nine := w.product("Ragi flour", catStaples)
	pills := w.product("Paracetamol", catMedicines)
	for range 10 {
		u := w.user()
		w.searches(atta, &u, 1)
		w.searches(pills, &u, 1)
	}
	for range 9 {
		u := w.user()
		w.searches(nine, &u, 2)
	}
	w.searches(nine, nil, 30)
	w.searches(atta, nil, 4)

	n, err := w.svc.Build(ctx, testNow)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.Equal(t, 1, w.count(`SELECT count(*) FROM site_demand_marquee`))
	require.Equal(t, 1, w.count(`SELECT count(*) FROM site_demand_marquee WHERE product_id = $1 AND searchers = 10 AND searches_total = 14 AND status = 'proposed'`, ids.UUID(atta)))

	// Not on the site until approved.
	m, err := w.svc.siteMarquee(ctx, "bengaluru")
	require.NoError(t, err)
	require.True(t, m.Fallback)
	require.Empty(t, m.Items)

	city := w.city()
	l, err := w.svc.adminMarquee(w.asAdmin(), admindemandapi.ListSiteMarqueeParams{})
	require.NoError(t, err)
	require.Equal(t, 10, l.MinSearchers)
	require.Len(t, l.Cities, 1)
	require.Equal(t, "bengaluru", l.Cities[0].CitySlug)
	require.NotNil(t, l.Cities[0].Latest)
	require.Nil(t, l.Cities[0].Approved)
	require.Equal(t, admindemandapi.Proposed, l.Cities[0].Latest.Status)
	builtOn := l.Cities[0].Latest.BuiltOn
	require.Equal(t, "2026-10-23", builtOn.String())

	approve := func(d admindemandapi.MarqueeDecisionDecision, on openapi_types.Date, pids ...ids.ID) (admindemandapi.MarqueeCity, error) {
		s := make([]string, len(pids))
		for i, p := range pids {
			s[i] = p.String()
		}
		return w.svc.decide(w.asAdmin(), city.String(), &admindemandapi.MarqueeDecision{BuiltOn: on, Decision: d, ProductIds: &s})
	}
	_, err = approve(admindemandapi.Approve, builtOn)
	require.Equal(t, genproblems.ValidationFailed, code(t, err))
	_, err = approve(admindemandapi.Reject, builtOn, atta)
	require.Equal(t, genproblems.ValidationFailed, code(t, err))
	_, err = approve(admindemandapi.Approve, builtOn, pills)
	require.Equal(t, genproblems.ValidationFailed, code(t, err))
	_, err = approve(admindemandapi.Approve, openapi_types.Date{Time: builtOn.AddDate(0, 0, -1)}, atta)
	require.Equal(t, genproblems.InvalidTransition, code(t, err))

	c, err := approve(admindemandapi.Approve, builtOn, atta)
	require.NoError(t, err)
	require.Equal(t, admindemandapi.Approved, c.Latest.Status)
	require.NotNil(t, c.Approved)
	require.Equal(t, 1, w.count(`SELECT count(*) FROM audit_log WHERE action = 'marquee.decide' AND actor_id = $1`, ids.UUID(w.admin)))
	_, err = approve(admindemandapi.Reject, builtOn)
	require.Equal(t, genproblems.InvalidTransition, code(t, err))

	m, err = w.svc.siteMarquee(ctx, "bengaluru")
	require.NoError(t, err)
	require.False(t, m.Fallback)
	require.Equal(t, []shareapi.SiteMarqueeItem{{ProductName: "Atta"}}, m.Items)

	// A rerun of the same night keeps the decided list.
	_, err = w.svc.Build(ctx, testNow)
	require.NoError(t, err)
	require.Equal(t, 1, w.count(`SELECT count(*) FROM site_demand_marquee WHERE status = 'approved'`))

	m, err = w.svc.siteMarquee(ctx, "nowhere")
	require.NoError(t, err)
	require.True(t, m.Fallback)
}

// SC-ASK-2x: gaps are closed asks with no Yes, from at least 10 distinct
// customers of the city; nine is not enough.
func TestGapsFloor(t *testing.T) {
	w := newWorld(t)
	city := w.city()
	ten := w.product("Kodo millet", catStaples)
	nine := w.product("Foxtail millet", catStaples)
	answered := w.product("Little millet", catStaples)
	st := w.store("gap-shop")
	ask := func(p ids.ID, yes bool) {
		id := w.id()
		w.exec(`INSERT INTO asks (id, customer_id, product_id, query_norm, city_place_id, radius_m, recipients_count, status, close_reason, created_at, closed_at)
		  VALUES ($1, $2, $3, 'millet', $4, 3000, 1, 'closed', 'window_ended', $5, $5)`,
			ids.UUID(id), ids.UUID(w.user()), ids.UUID(p), ids.UUID(city), testNow.Add(-30*time.Hour))
		if yes {
			w.exec(`INSERT INTO ask_recipients (id, ask_id, store_id, sent_at, distance_m, distance_band, reply_by, answer, replied_at)
			  VALUES ($1, $2, $3, $4, 100, 'lt_1km', $4, 'yes', $4)`, ids.UUID(w.id()), ids.UUID(id), ids.UUID(st), testNow.Add(-24*time.Hour))
		}
	}
	for i := range 10 {
		ask(ten, false)
		ask(answered, i == 0)
		if i < 9 {
			ask(nine, false)
		}
	}
	l, err := w.svc.gaps(w.asAdmin(), admindemandapi.ListDemandGapsParams{})
	require.NoError(t, err)
	require.Equal(t, 10, l.MinCustomers)
	require.Equal(t, 7, l.Days)
	require.Len(t, l.Items, 1)
	g := l.Items[0]
	require.Equal(t, "Kodo millet", g.ItemName)
	require.Equal(t, 10, g.Customers)
	require.Equal(t, 10, g.ShopsAsked)
	require.Equal(t, "Bengaluru", g.CityName)
	require.NotNil(t, g.Category)

	one := 1
	l, err = w.svc.gaps(w.asAdmin(), admindemandapi.ListDemandGapsParams{Days: &one})
	require.NoError(t, err)
	require.Len(t, l.Items, 0, "asks 30 hours old are outside a one-day window")
}

func TestCards(t *testing.T) {
	w := newWorld(t)
	ctx := reqctx.WithClientIP(context.Background(), "203.0.113.7")
	st := w.store("card-shop")
	it := w.item(st, w.product("Toor dal", catStaples), 3850)

	c, err := w.svc.create(ctx, &shareapi.ShareCardCreate{StoreItemId: it.String()})
	require.NoError(t, err)
	require.Len(t, c.Code, 10)
	require.Equal(t, "https://app.todayz.localhost/x/"+c.Code, c.Url)
	require.Equal(t, "https://img.todayz.localhost/share/"+c.Code+".png", c.ImageUrl)
	require.Equal(t, "Toor dal at Shop card-shop, ₹38.50 · TodayZ", c.Text)
	require.Equal(t, 1, w.images.count("share/"+c.Code+".png"))

	// The same item's card again: same code, not counted, not re-rendered.
	again, err := w.svc.create(ctx, &shareapi.ShareCardCreate{StoreItemId: it.String()})
	require.NoError(t, err)
	require.Equal(t, c.Code, again.Code)
	require.Equal(t, 1, w.images.count("share/"+c.Code+".png"))
	require.Equal(t, 1, w.count(`SELECT count(*) FROM share_card_creates`))

	p, err := w.svc.get(ctx, c.Code)
	require.NoError(t, err)
	require.Equal(t, 3850, p.PricePaise)
	require.Equal(t, 9900, *p.MrpPaise)
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "distance")
	require.Equal(t, 1, w.count(`SELECT count(*) FROM events WHERE name = 'share_opened'`))

	// A price change re-renders on the next read.
	w.exec(`UPDATE store_items SET price_paise = 3600 WHERE id = $1`, ids.UUID(it))
	p, err = w.svc.get(ctx, c.Code)
	require.NoError(t, err)
	require.Equal(t, 3600, p.PricePaise)
	require.Equal(t, 2, w.images.count("share/"+c.Code+".png"))
	require.Equal(t, 1, w.count(`SELECT count(*) FROM share_cards WHERE price_paise_rendered = 3600`))

	_, err = w.svc.get(ctx, "ZZZZZZZZZZ")
	require.Equal(t, genproblems.NotFound, code(t, err))
	_, err = w.svc.create(ctx, &shareapi.ShareCardCreate{StoreItemId: ids.New(testNow).String()})
	require.Equal(t, genproblems.ShareCardUnavailable, code(t, err))

	// A store that is no longer live: 410 for the card and for new ones.
	w.exec(`UPDATE stores SET status = 'paused' WHERE id = $1`, ids.UUID(st))
	_, err = w.svc.get(ctx, c.Code)
	require.Equal(t, genproblems.ShareCardUnavailable, code(t, err))
}

func TestCardCreateLimit(t *testing.T) {
	w := newWorld(t)
	ctx := reqctx.WithClientIP(context.Background(), "203.0.113.9")
	st := w.store("limit-shop")
	items := make([]ids.ID, 6)
	for i := range items {
		items[i] = w.item(st, w.product(fmt.Sprintf("Item %d", i), catStaples), 1000)
	}
	for _, it := range items[:5] {
		_, err := w.svc.create(ctx, &shareapi.ShareCardCreate{StoreItemId: it.String()})
		require.NoError(t, err)
	}
	_, err := w.svc.create(ctx, &shareapi.ShareCardCreate{StoreItemId: items[5].String()})
	require.Equal(t, genproblems.RateLimited, code(t, err))

	// Another IP is counted on its own; an hour later the first may again.
	other := reqctx.WithClientIP(context.Background(), "198.51.100.4")
	_, err = w.svc.create(other, &shareapi.ShareCardCreate{StoreItemId: items[5].String()})
	require.NoError(t, err)
	w.clk.Advance(time.Hour + time.Second)
	it := w.item(st, w.product("Late", catStaples), 1000)
	_, err = w.svc.create(ctx, &shareapi.ShareCardCreate{StoreItemId: it.String()})
	require.NoError(t, err)
}

func TestCardCreateNeedsHashKey(t *testing.T) {
	w := newWorld(t)
	w.svc.hash = nil
	_, err := w.svc.create(context.Background(), &shareapi.ShareCardCreate{StoreItemId: ids.New(testNow).String()})
	require.Equal(t, genproblems.ServiceUnavailable, code(t, err))
}

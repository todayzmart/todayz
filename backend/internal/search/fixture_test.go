package search

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	"github.com/todayz/backend/gen/events"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// The QA point: Indiranagar, Bengaluru (08 ASK-09 "at the QA point").
const qaLat, qaLng = 12.9719, 77.6412

// testNow is 12:00 IST on a Friday.
var testNow = time.Date(2026, 10, 23, 6, 30, 0, 0, time.UTC)

// Category ids seeded by 10501.
const (
	catStaples = "0190c47e-0000-7000-8000-000000000101"
	catDairy   = "0190c47e-0000-7000-8000-000000000102"
	catGrocery = "0190c47e-0000-7000-8000-000000000100"
)

// offset moves the QA point dx metres east and dy metres north.
func offset(dx, dy float64) (lat, lng float64) {
	lat = qaLat + dy/111320
	lng = qaLng + dx/(111320*math.Cos(qaLat*math.Pi/180))
	return lat, lng
}

// fakeOpen: a store is open when it has weekly hours and no future manual
// close (the real rules are W1-04's business.OpenAt, wired in routes).
var fakeOpen = OpenFunc(func(h []byte, closedUntil *time.Time, t time.Time) bool {
	return bytes.Contains(h, []byte(`"open"`)) && (closedUntil == nil || !closedUntil.After(t))
})

const allDay = `{"weekly":{"mon":[{"open":"00:00","close":"24:00"}],"tue":[{"open":"00:00","close":"24:00"}],"wed":[{"open":"00:00","close":"24:00"}],"thu":[{"open":"00:00","close":"24:00"}],"fri":[{"open":"00:00","close":"24:00"}],"sat":[{"open":"00:00","close":"24:00"}],"sun":[{"open":"00:00","close":"24:00"}]},"holidays":[]}`

// world is a database with the fixture catalogue and a service on it.
type world struct {
	t     *testing.T
	tdb   *testkit.DB
	svc   *Service
	cache *settings.Cache
	clk   *clock.Fake
	ctx   context.Context
	user  string
	n     int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	tdb := testkit.NewDB(t)
	cat, err := eventcat.Load(events.FS)
	require.NoError(t, err)
	w := &world{t: t, tdb: tdb, clk: clock.NewFake(testNow), ctx: context.Background()}
	w.cache = settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	w.svc, err = New(Deps{
		DB: db.New(tdb.Pool), Settings: w.cache, Clock: w.clk, Open: fakeOpen,
		UserID: func(context.Context) string { return w.user },
	}, cat)
	require.NoError(t, err)
	return w
}

// exec runs fixture SQL as the owner.
func (w *world) exec(sql string, args ...any) {
	w.t.Helper()
	_, err := w.tdb.OwnerPool.Exec(w.ctx, sql, args...)
	require.NoError(w.t, err, sql)
}

// shortCode is a unique 6-character code in the codes' alphabet
// ([A-HJKMNP-Z2-9]) for fixture number n.
func shortCode(n int) string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	for i := 5; i >= 0; i-- {
		b[i] = alphabet[n%len(alphabet)]
		n /= len(alphabet)
	}
	return string(b)
}

// id returns a fresh fixture uuid (time part fixed, so it is a valid ULID).
func (w *world) id() string {
	w.n++
	return fmt.Sprintf("01900000-0000-7000-8000-%012x", w.n)
}

func (w *world) business(status, category string) string {
	id := w.id()
	w.exec(`INSERT INTO businesses (id, brand_name, referral_code, status, category_id) VALUES ($1, 'Biz', $2, $3, $4)`,
		id, shortCode(w.n), status, category)
	return id
}

type storeOpt struct {
	status           string
	pickup, delivery bool
	fee              int64
	freeAbove        *int64
	hours            string
	rating           *float64
	reliability      *float64
	closedUntil      *time.Time
	locality         string
}

func (w *world) store(biz, slug string, dx, dy float64, o storeOpt) string {
	id := w.id()
	if o.status == "" {
		o.status = "live"
	}
	if o.hours == "" {
		o.hours = allDay
	}
	if o.locality == "" {
		o.locality = "Indiranagar"
	}
	lat, lng := offset(dx, dy)
	code := shortCode(w.n)
	w.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, locality, city, location, hours, status, live_at,
	    accepts_pickup, accepts_delivery, delivery_fee_paise, delivery_free_above_paise, rating_avg, rating_count,
	    reliability_score, closed_until)
	  VALUES ($1, $2, $3, $4, $5, $6, 'Bengaluru', ST_SetSRID(ST_MakePoint($7, $8), 4326)::geography, $9::jsonb, $10,
	    CASE WHEN $10 = 'live' THEN $11::timestamptz END, $12, $13, $14, $15, $16, CASE WHEN $16::numeric IS NULL THEN 0 ELSE 10 END, $17, $18)`,
		id, biz, slug, code, "Shop "+slug, o.locality, lng, lat, o.hours, o.status, testNow.Add(-24*time.Hour),
		o.pickup, o.delivery, o.fee, o.freeAbove, o.rating, o.reliability, o.closedUntil)
	return id
}

func (w *world) brand(name, slug string) string {
	id := w.id()
	w.exec(`INSERT INTO brands (id, name, slug) VALUES ($1, $2, $3)`, id, name, slug)
	return id
}

func (w *world) product(name string, brand *string, category string, size float64, unit string, barcodes ...string) string {
	id := w.id()
	if barcodes == nil {
		barcodes = []string{}
	}
	w.exec(`INSERT INTO products (id, name, brand_id, category_id, pack_size, unit, barcodes) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, name, brand, category, size, unit, barcodes)
	return id
}

// item lists product in store at price with stock on hand, confirmed
// `confirmed` before testNow. The search vector is built the way W1-05's
// InsertStoreItem builds it.
func (w *world) item(store, product string, price int64, onHand int, confirmed time.Duration, localName *string) string {
	id := w.id()
	tag, err := w.tdb.OwnerPool.Exec(w.ctx, `INSERT INTO store_items (id, store_id, product_id, local_name, price_paise, on_hand, stock_updated_at, search)
	  SELECT $1, $2, p.id, $4::text, $5, $6, $7,
	    p.search || setweight(to_tsvector('simple'::regconfig, coalesce(b.name, '')), 'B')
	             || setweight(to_tsvector('simple'::regconfig, coalesce($4::text, '')), 'A')
	  FROM products p LEFT JOIN brands b ON b.id = p.brand_id WHERE p.id = $3::uuid`,
		id, store, product, localName, price, onHand, testNow.Add(-confirmed))
	require.NoError(w.t, err)
	require.EqualValues(w.t, 1, tag.RowsAffected(), "item: no such product")
	return id
}

func sp(s string) *string { return &s }
func bp(v bool) *bool     { return &v }

// search runs GET /v1/discover/search at the QA point.
func (w *world) search(q string, mod ...func(*discoverapi.SearchDiscoverParams)) discoverapi.DiscoverSearchResult {
	w.t.Helper()
	p := discoverapi.SearchDiscoverParams{Q: q, Lat: qaLat, Lng: qaLng}
	for _, f := range mod {
		f(&p)
	}
	out, err := w.svc.Search(w.ctx, p)
	require.NoError(w.t, err, q)
	return out
}

func resultSlugs(r discoverapi.DiscoverSearchResult) []string {
	out := make([]string, len(r.Results))
	for i, s := range r.Results {
		out[i] = s.Store.StoreSlug
	}
	return out
}

// qa is the ASK-09 fixture around the QA point: 4 live stores of verified
// businesses within 2 km (one in-store only), and every kind of store that
// must never count or show.
type qa struct {
	*world
	butter100, butter500, curd, atta, ghee string
	amul                                   string
	nearButter, midCurd                    string
}

func newQA(t *testing.T) *qa {
	w := newWorld(t)
	f := &qa{world: w}
	verified := w.business("verified", catGrocery)
	f.amul = w.brand("Amul", "amul")
	nandini := w.brand("Nandini", "nandini")
	f.butter100 = w.product("Amul Butter", &f.amul, catDairy, 100, "g", "8901262150019")
	f.butter500 = w.product("Amul Butter", &f.amul, catDairy, 500, "g")
	f.curd = w.product("Nandini Curd", &nandini, catDairy, 400, "g")
	f.atta = w.product("Aashirvaad Atta", nil, catStaples, 5, "kg")
	f.ghee = w.product("Ghee", &nandini, catDairy, 1, "l")

	free := int64(30000)
	near := w.store(verified, "near", 300, 0, storeOpt{pickup: true, rating: fp(4.6)})
	mid := w.store(verified, "mid", 0, 900, storeOpt{pickup: true, delivery: true, fee: 2000, freeAbove: &free})
	far := w.store(verified, "far", -1500, 0, storeOpt{pickup: true, reliability: fp(0.4)})
	instore := w.store(verified, "instore", 0, -600, storeOpt{})

	f.nearButter = w.item(near, f.butter100, 5800, 10, 12*time.Minute, nil)
	w.item(near, f.curd, 3500, 5, time.Hour, nil)
	w.item(near, f.atta, 25000, 3, time.Hour, nil)
	w.item(mid, f.butter500, 27500, 4, time.Hour, nil)
	f.midCurd = w.item(mid, f.curd, 3000, 8, 96*time.Hour, sp("Nandini Dahi")) // stale: older than 72 h
	w.item(far, f.atta, 24000, 2, time.Hour, nil)
	w.item(far, f.curd, 3300, 2, time.Hour, nil)
	w.item(instore, f.butter100, 5500, 0, time.Hour, nil) // out of stock: never a result
	w.item(instore, f.ghee, 60000, 1, time.Hour, nil)

	// Never counted, never shown, whatever the price.
	for _, st := range []string{"paused", "draft", "suspended", "pending_review"} {
		s := w.store(verified, "hidden-"+strings.ReplaceAll(st, "_", "-"), 400, 100, storeOpt{status: st, pickup: true})
		w.item(s, f.butter100, 4000, 10, time.Minute, nil)
	}
	unverified := w.business("submitted", catGrocery)
	s := w.store(unverified, "unverified", 350, 0, storeOpt{pickup: true})
	w.item(s, f.butter100, 4000, 10, time.Minute, nil)
	// Outside 2 km, inside 5 km.
	out := w.store(verified, "outside", 3000, 0, storeOpt{pickup: true})
	w.item(out, f.butter100, 4500, 10, time.Minute, nil)
	return f
}

package offers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/gen/events"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// 12:00 IST on a Friday.
var testNow = time.Date(2026, 10, 23, 6, 30, 0, 0, time.UTC)

// Seeded by 10501.
const (
	catStaples = "0190c47e-0000-7000-8000-000000000101"
	catGrocery = "0190c47e-0000-7000-8000-000000000100"
)

const allDay = `{"weekly":{"mon":[{"open":"00:00","close":"24:00"}]},"holidays":[]}`

// world is a database with a store, items and an offers service on it.
type world struct {
	t     *testing.T
	tdb   *testkit.DB
	svc   *Service
	clk   *clock.Fake
	ctx   context.Context
	owner ids.ID
	n     int
}

func newWorld(t *testing.T, banners BannerSource) *world {
	t.Helper()
	tdb := testkit.NewDB(t)
	cat, err := eventcat.Load(events.FS)
	require.NoError(t, err)
	w := &world{t: t, tdb: tdb, clk: clock.NewFake(testNow), owner: ids.New(testNow)}
	w.ctx = authn.WithPrincipal(context.Background(), authn.Principal{UserID: w.owner, App: authn.AppBusiness})
	w.exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(w.owner))
	cache := settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	w.svc, err = New(Deps{DB: db.New(tdb.Pool), Clock: w.clk, Settings: cache, Banners: banners, Events: cat})
	require.NoError(t, err)
	return w
}

func (w *world) exec(sql string, args ...any) {
	w.t.Helper()
	_, err := w.tdb.OwnerPool.Exec(context.Background(), sql, args...)
	require.NoError(w.t, err, sql)
}

func (w *world) id() ids.ID {
	w.n++
	return ids.New(testNow)
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

// store makes a live store dx metres east of the QA point.
func (w *world) store(slug string, dx float64) ids.ID {
	biz, id := w.id(), w.id()
	w.exec(`INSERT INTO businesses (id, brand_name, referral_code, status, category_id) VALUES ($1, 'Biz', $2, 'verified', $3)`,
		ids.UUID(biz), shortCode(w.n), catGrocery)
	lat, lng := 12.9719, 77.6412+dx/108500
	w.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, locality, city, location, hours, status, live_at,
	    accepts_pickup, accepts_delivery, delivery_fee_paise)
	  VALUES ($1, $2, $3, $4, $5, 'Indiranagar', 'Bengaluru', ST_SetSRID(ST_MakePoint($6, $7), 4326)::geography, $8::jsonb,
	    'live', $9, true, true, 0)`,
		ids.UUID(id), ids.UUID(biz), slug, shortCode(w.n+1000), "Shop "+slug, lng, lat, allDay, testNow.Add(-24*time.Hour))
	return id
}

// item lists a fresh product in store at price.
func (w *world) item(store ids.ID, name string, price int64) ids.ID {
	prod, id := w.id(), w.id()
	w.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, barcodes) VALUES ($1, $2, $3, 1, 'kg', '{}')`,
		ids.UUID(prod), name, catStaples)
	w.exec(`INSERT INTO store_items (id, store_id, product_id, price_paise, on_hand, stock_updated_at) VALUES ($1, $2, $3, $4, 100, $5)`,
		ids.UUID(id), ids.UUID(store), ids.UUID(prod), price, testNow)
	return id
}

type offerRow struct {
	kind, scope, channels, status string
	value                         int64
	buy, get, minBill             *int64
	items                         []ids.ID
	category                      string
	perCustomer, maxRedeem        *int
	redemptions                   int
	startsAt                      time.Time
	endsAt                        *time.Time
}

// offer inserts an offer row directly (the engine reads rows, not the API).
func (w *world) offer(store ids.ID, o offerRow) ids.ID {
	id := w.id()
	if o.scope == "" {
		o.scope = "store"
	}
	if o.channels == "" {
		o.channels = "both"
	}
	if o.status == "" {
		o.status = "live"
	}
	if o.startsAt.IsZero() {
		o.startsAt = testNow.Add(-time.Hour)
	}
	var cat any
	if o.category != "" {
		cat = o.category
	}
	w.exec(`INSERT INTO offers (id, store_id, kind, value, buy_qty, get_qty, min_bill_paise, scope, store_item_ids, category_id,
	    channels, per_customer_limit, max_redemptions, redemptions, starts_at, ends_at, status)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::uuid[], $10, $11, $12, $13, $14, $15, $16, $17)`,
		ids.UUID(id), ids.UUID(store), o.kind, o.value, o.buy, o.get, o.minBill, o.scope, uuids(o.items), cat,
		o.channels, o.perCustomer, o.maxRedeem, o.redemptions, o.startsAt, o.endsAt, o.status)
	return id
}

func (w *world) customer() ids.ID {
	id := w.id()
	w.exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(id))
	return id
}

// order is a real order of cust at store: offer_redemptions points at orders.
func (w *world) order(cust, store ids.ID) ids.ID {
	return w.tdb.InsertOrder(w.t, w.id(), cust, store)
}

func ip(n int) *int             { return &n }
func i64(n int64) *int64        { return &n }
func tp(t time.Time) *time.Time { return &t }

// fakeBanners is a BannerSource with fixed cards.
type fakeBanners struct{ cards []BannerCard }

func (f *fakeBanners) LiveBanners(_ context.Context, stores []ids.ID, _ time.Time) ([]BannerCard, error) {
	var out []BannerCard
	for _, c := range f.cards {
		for _, s := range stores {
			if c.StoreID == s {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

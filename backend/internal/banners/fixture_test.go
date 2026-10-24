package banners

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

const catGrocery = "0190c47e-0000-7000-8000-000000000100"

const allDay = `{"weekly":{"mon":[{"open":"00:00","close":"24:00"}]},"holidays":[]}`

type world struct {
	t     *testing.T
	tdb   *testkit.DB
	svc   *Service
	clk   *clock.Fake
	ctx   context.Context
	owner ids.ID
	n     int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	tdb := testkit.NewDB(t)
	cat, err := eventcat.Load(events.FS)
	require.NoError(t, err)
	w := &world{t: t, tdb: tdb, clk: clock.NewFake(testNow), owner: ids.New(testNow)}
	w.ctx = authn.WithPrincipal(context.Background(), authn.Principal{UserID: w.owner, App: authn.AppBusiness})
	w.exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(w.owner))
	cache := settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	w.svc, err = New(Deps{DB: db.New(tdb.Pool), Clock: w.clk, Settings: cache, Events: cat})
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

func (w *world) store(slug string) ids.ID {
	biz, id := w.id(), w.id()
	w.exec(`INSERT INTO businesses (id, brand_name, referral_code, status, category_id) VALUES ($1, 'Biz', $2, 'verified', $3)`,
		ids.UUID(biz), shortCode(w.n), catGrocery)
	w.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, locality, city, location, hours, status, live_at,
	    accepts_pickup, accepts_delivery, delivery_fee_paise)
	  VALUES ($1, $2, $3, $4, $5, 'Indiranagar', 'Bengaluru', ST_SetSRID(ST_MakePoint(77.6412, 12.9719), 4326)::geography, $6::jsonb,
	    'live', $7, true, true, 0)`,
		ids.UUID(id), ids.UUID(biz), slug, shortCode(w.n+1000), "Shop "+slug, allDay, testNow.Add(-24*time.Hour))
	return id
}

func (w *world) customer() ids.ID {
	id := w.id()
	w.exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(id))
	return id
}

func (w *world) as(user ids.ID) context.Context {
	return authn.WithPrincipal(context.Background(), authn.Principal{UserID: user, App: authn.AppCustomer})
}

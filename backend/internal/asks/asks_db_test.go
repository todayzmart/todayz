package asks

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	asksapi "github.com/todayz/backend/gen/api/asks"
	"github.com/todayz/backend/gen/events"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/asksdb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
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

const (
	qaLat, qaLng = 12.9719, 77.6412
	allDay       = `{"weekly":{"mon":[{"open":"00:00","close":"24:00"}]},"holidays":[]}`
)

// priceAuthz grants item.price.edit to everyone but cashiers.
type priceAuthz struct{}

func (priceAuthz) Check(ctx context.Context, _ authn.Principal, action string, s authz.Scope) (authz.Decision, error) {
	if d, ok := authz.DecisionFrom(ctx); ok && d.Role == authz.Cashier {
		return authz.Decision{}, problem.New(genproblems.Forbidden, "")
	}
	return authz.Decision{Action: action, Grant: authz.Allow, Role: authz.Owner, Scope: s}, nil
}

type world struct {
	t        *testing.T
	tdb      *testkit.DB
	svc      *Service
	clk      *clock.Fake
	customer ids.ID
	staff    ids.ID
	n        int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	tdb := testkit.NewDB(t)
	cat, err := eventcat.Load(events.FS)
	require.NoError(t, err)
	w := &world{t: t, tdb: tdb, clk: clock.NewFake(testNow)}
	w.customer, w.staff = w.user(), w.user()
	cache := settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	w.svc, err = New(Deps{DB: db.New(tdb.Pool), Clock: w.clk, Settings: cache, Events: cat, Authz: priceAuthz{}})
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
	lng := qaLng + dx/108500
	w.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, locality, city, location, hours, status, live_at,
	    accepts_pickup, accepts_delivery, delivery_fee_paise)
	  VALUES ($1, $2, $3, $4, $5, 'Indiranagar', 'Bengaluru', ST_SetSRID(ST_MakePoint($6, $7), 4326)::geography, $8::jsonb,
	    'live', $9, true, true, 0)`,
		ids.UUID(id), ids.UUID(biz), slug, shortCode(w.n+1000), "Shop "+slug, lng, qaLat, allDay, testNow.Add(-24*time.Hour))
	return id
}

func (w *world) product(name string, mrp int64) ids.ID {
	id := w.id()
	w.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, barcodes, mrp_paise) VALUES ($1, $2, $3, 1, 'kg', '{}', $4)`,
		ids.UUID(id), name, catStaples, mrp)
	return id
}

func (w *world) item(store, product ids.ID, price int64, listed bool) ids.ID {
	id := w.id()
	w.exec(`INSERT INTO store_items (id, store_id, product_id, price_paise, on_hand, is_listed, stock_updated_at)
	  VALUES ($1, $2, $3, $4, 7, $5, $6)`, ids.UUID(id), ids.UUID(store), ids.UUID(product), price, listed, testNow.Add(-48*time.Hour))
	return id
}

func (w *world) asCustomer() context.Context {
	return authn.WithPrincipal(context.Background(), authn.Principal{UserID: w.customer, App: authn.AppCustomer})
}

func (w *world) asStaff(store ids.ID, role authz.Role) context.Context {
	ctx := authn.WithPrincipal(context.Background(), authn.Principal{UserID: w.staff, App: authn.AppBusiness})
	return authz.WithDecision(ctx, authz.Decision{Action: "asks.reply", Grant: authz.Allow, Role: role, Scope: authz.Scope{StoreID: store}})
}

func (w *world) ask(productID ids.ID, slug string) (asksapi.AskDetail, error) {
	body := &asksapi.AskCreate{Lat: qaLat, Lng: qaLng, Quantity: 2}
	if productID != ids.Zero {
		body.ProductId = ptr(productID.String())
	} else {
		body.Query = ptr("organic jaggery")
	}
	if slug != "" {
		body.StoreSlug = &slug
	}
	return w.svc.create(w.asCustomer(), body)
}

func code(t *testing.T, err error) string {
	t.Helper()
	var p *problem.Error
	require.ErrorAs(t, err, &p)
	return p.Code
}

func TestRoundDistance(t *testing.T) {
	for m, want := range map[int32]int32{0: 500, 249: 500, 600: 500, 749: 500, 750: 1000, 1234: 1000, 1250: 1500, 4999: 5000} {
		require.Equal(t, want, RoundDistance(m), m)
	}
}

func TestBand(t *testing.T) {
	for m, want := range map[int32]string{0: "lt_1km", 999: "lt_1km", 1000: "1_3km", 2999: "1_3km", 3000: "3_5km", 4999: "3_5km", 5000: "gt_5km"} {
		require.Equal(t, want, Band(m), m)
	}
}

// TestSelectRecipients is the recipient-selection table: the order is the
// query's (tier, then distance); closed stores drop out; max caps the list.
func TestSelectRecipients(t *testing.T) {
	open := func(h []byte, _ *time.Time, _ time.Time) bool { return string(h) != "closed" }
	c := func(name string, tier, dist int32, hours string) Recipient {
		return Recipient{Name: name, Tier: tier, DistanceM: dist, Hours: []byte(hours)}
	}
	cands := []Recipient{c("a", 0, 900, ""), c("b", 0, 2500, "closed"), c("c", 1, 300, ""), c("d", 2, 100, ""), c("e", 2, 200, "")}
	names := func(rs []Recipient) []string {
		out := []string{}
		for _, r := range rs {
			out = append(out, r.Name)
		}
		return out
	}
	for _, tc := range []struct {
		max  int
		want []string
	}{
		{10, []string{"a", "c", "d", "e"}},
		{2, []string{"a", "c"}},
		{1, []string{"a"}},
		{0, []string{}},
	} {
		require.Equal(t, tc.want, names(SelectRecipients(cands, testNow, tc.max, open)), tc.max)
	}
}

func TestLowYield(t *testing.T) {
	r := func(yes ...bool) []asksdb.RecentAskYesRow {
		out := make([]asksdb.RecentAskYesRow, len(yes))
		for i, y := range yes {
			out[i].HadYes = y
		}
		return out
	}
	require.False(t, LowYield(r(false, false), 3), "fewer asks than the count")
	require.True(t, LowYield(r(false, false, false), 3))
	require.False(t, LowYield(r(false, true, false), 3), "a Yes resets it")
	require.False(t, LowYield(nil, 0), "0 turns the rule off")
}

func mustID(t *testing.T, s string) ids.ID {
	t.Helper()
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return id
}

func TestReplyPriceRule(t *testing.T) {
	mrp := int64(6000)
	listed := ReplyPriceRule{Listed: true, ListedPaise: 5000, MrpPaise: &mrp}
	p, err := listed.Decide(nil)
	require.NoError(t, err)
	require.Equal(t, int64(5000), *p, "no price sent: the listed price")
	p, err = listed.Decide(ptr(5000))
	require.NoError(t, err)
	require.Equal(t, int64(5000), *p, "the listed price needs no permission")
	_, err = listed.Decide(ptr(4500))
	require.Equal(t, string(genproblems.Forbidden), code(t, err), "cashier lock")
	edit := listed
	edit.CanEdit = true
	p, err = edit.Decide(ptr(4500))
	require.NoError(t, err)
	require.Equal(t, int64(4500), *p)
	_, err = edit.Decide(ptr(6001))
	require.Equal(t, string(genproblems.PriceAboveMrp), code(t, err), "never above MRP")
	p, err = (ReplyPriceRule{}).Decide(nil)
	require.NoError(t, err)
	require.Nil(t, p, "an unlisted Yes with no price")
}

func TestReadyByOK(t *testing.T) {
	tomorrow := "tomorrow"
	require.True(t, ReadyByOK(testNow.Add(time.Hour), testNow, nil))
	require.False(t, ReadyByOK(testNow.Add(-time.Hour), testNow, nil), "in the past")
	require.False(t, ReadyByOK(testNow.Add(13*time.Hour), testNow, nil), "after today's IST midnight")
	require.True(t, ReadyByOK(testNow.Add(13*time.Hour), testNow, &tomorrow))
	require.False(t, ReadyByOK(testNow.Add(37*time.Hour), testNow, &tomorrow))
}

func TestCreateSendsNearestFirstAndSkipsExcluded(t *testing.T) {
	w := newWorld(t)
	prod := w.product("Organic Jaggery 1 kg", 12000)
	near := w.store("near", 300)
	lister := w.store("lister", 2500) // lists the product: tier 0 beats distance
	w.item(lister, prod, 11000, true)
	muted := w.store("muted", 100)
	w.exec(`UPDATE stores SET asks_muted = true WHERE id = $1`, ids.UUID(muted))
	far := w.store("far", 40000)
	blocked := w.store("blocked", 200)
	old := w.id()
	w.exec(`INSERT INTO asks (id, customer_id, query_norm, radius_m, status, close_reason, created_at, closed_at) VALUES ($1, $2, 'x', 1, 'closed', 'window_ended', $3, $3)`,
		ids.UUID(old), ids.UUID(w.customer), testNow.Add(-48*time.Hour))
	w.exec(`INSERT INTO ask_store_blocks (store_id, customer_id, ask_id) VALUES ($1, $2, $3)`, ids.UUID(blocked), ids.UUID(w.customer), ids.UUID(old))

	d, err := w.ask(prod, "")
	require.NoError(t, err)
	require.Equal(t, 2, d.Ask.Progress.Asked)
	require.Equal(t, asksapi.AskStatus("open"), d.Ask.Status)
	var got []ids.ID
	rows, err := w.tdb.OwnerPool.Query(context.Background(),
		`SELECT store_id FROM ask_recipients WHERE ask_id = $1 ORDER BY distance_m DESC`, ids.UUID(mustID(t, d.Ask.AskId)))
	require.NoError(t, err)
	for rows.Next() {
		var u pgtype.UUID
		require.NoError(t, rows.Scan(&u))
		got = append(got, idOf(u))
	}
	require.ElementsMatch(t, []ids.ID{lister, near}, got)
	_ = far
	require.Equal(t, 1, w.count(`SELECT count(*) FROM events WHERE name = 'ask_created' AND user_id IS NULL`))
	require.NotZero(t, w.count(`SELECT count(*) FROM asks WHERE id = $1 AND geo_cell IS NOT NULL`, ids.UUID(mustID(t, d.Ask.AskId))))
}

func TestCreateLimitsAndOneOpenAsk(t *testing.T) {
	w := newWorld(t)
	w.store("near", 300)
	prod := w.product("Ghee 500 ml", 40000)
	_, err := w.ask(prod, "")
	require.NoError(t, err)
	_, err = w.ask(prod, "")
	require.Equal(t, string(genproblems.AskAlreadyOpen), code(t, err))

	limit := w.count(`SELECT (value #>> '{}')::int FROM platform_settings WHERE key = 'ask_daily_limit_customer'`)
	for i := 0; i < limit+1; i++ {
		w.exec(`INSERT INTO asks (id, customer_id, query_norm, radius_m, status, close_reason, created_at, closed_at) VALUES ($1, $2, $3, 1, 'closed', 'window_ended', $4, $4)`,
			ids.UUID(w.id()), ids.UUID(w.customer), "q"+shortCode(i), testNow.Add(-time.Minute))
	}
	_, err = w.ask(ids.Zero, "")
	require.Equal(t, string(genproblems.AskLimitReached), code(t, err))
	var p *problem.Error
	require.ErrorAs(t, err, &p)
	require.EqualValues(t, limit, p.Extensions["limit"], "the setting, for the C-25 copy")
}

// TestNoShopAnswersAreTheSame: "Ask this shop" never says why a store is
// left out (muted, blocked, closed, unknown slug all read the same).
func TestNoShopAnswersAreTheSame(t *testing.T) {
	w := newWorld(t)
	muted := w.store("muted", 300)
	w.exec(`UPDATE stores SET asks_muted = true WHERE id = $1`, ids.UUID(muted))
	_, err := w.ask(ids.Zero, "muted")
	require.Equal(t, string(genproblems.AskStoreUnavailable), code(t, err))
	_, err = w.ask(ids.Zero, "no-such-shop")
	require.Equal(t, string(genproblems.AskStoreUnavailable), code(t, err))
	_, err = w.ask(ids.Zero, "")
	require.Equal(t, string(genproblems.AskNoShops), code(t, err), "muted is the only shop nearby")
}

func TestReplyPricerAndNoStockMovement(t *testing.T) {
	w := newWorld(t)
	prod := w.product("Basmati 5 kg", 90000)
	st := w.store("rice", 400)
	item := w.item(st, prod, 80000, true)
	other := w.store("other", 600)
	d, err := w.ask(prod, "")
	require.NoError(t, err)
	aid := d.Ask.AskId

	// The shop side never carries the customer or the exact distance.
	l, err := w.svc.listStore(w.asStaff(st, authz.Owner), st.String(), asksapi.ListStoreAsksParams{})
	require.NoError(t, err)
	require.Len(t, l.Asks, 1)
	raw, err := json.Marshal(l)
	require.NoError(t, err)
	for _, k := range []string{"customer", "phone", "address", "distanceM", "userId"} {
		require.NotContains(t, string(raw), k)
	}
	require.Equal(t, "lt_1km", string(l.Asks[0].DistanceBand))

	// A cashier cannot change the price; above MRP is refused.
	_, err = w.svc.reply(w.asStaff(st, authz.Cashier), st.String(), aid, &asksapi.AskReplyCreate{Answer: asksapi.AskReplyCreateAnswerYes, PricePaise: ptr(75000)})
	require.Equal(t, string(genproblems.Forbidden), code(t, err))
	_, err = w.svc.reply(w.asStaff(st, authz.Owner), st.String(), aid, &asksapi.AskReplyCreate{Answer: asksapi.AskReplyCreateAnswerYes, PricePaise: ptr(90001)})
	require.Equal(t, string(genproblems.PriceAboveMrp), code(t, err))

	moves := w.count(`SELECT count(*) FROM stock_movements`)
	got, err := w.svc.reply(w.asStaff(st, authz.Owner), st.String(), aid, &asksapi.AskReplyCreate{Answer: asksapi.AskReplyCreateAnswerYes, PricePaise: ptr(76000)})
	require.NoError(t, err)
	require.Equal(t, 76000, *got.PricePaise)
	require.Equal(t, moves, w.count(`SELECT count(*) FROM stock_movements`), "a Yes writes no movement")
	require.Equal(t, 7, w.count(`SELECT on_hand FROM store_items WHERE id = $1`, ids.UUID(item)))
	require.Equal(t, 1, w.count(`SELECT count(*) FROM store_items WHERE id = $1 AND stock_updated_at = $2`, ids.UUID(item), testNow))
	_, err = w.svc.reply(w.asStaff(st, authz.Owner), st.String(), aid, &asksapi.AskReplyCreate{Answer: asksapi.AskReplyCreateAnswerYes})
	require.Equal(t, string(genproblems.InvalidTransition), code(t, err), "one reply per shop")

	// The Pricer: per unit, valid for ask_price_valid_min, capped at the
	// asked quantity, only for this customer and store.
	det, err := w.svc.getMine(w.asCustomer(), aid)
	require.NoError(t, err)
	require.Len(t, det.Replies, 1)
	rep := mustID(t, det.Replies[0].AskReplyId)
	require.Equal(t, 500, det.Replies[0].DistanceM, "400 m is kept as 500 m")
	require.Equal(t, st.String(), det.Replies[0].StoreId)
	require.NotNil(t, det.Replies[0].ListedPricePaise, "the item's price now, for after the reply's price ends")
	p, ok, err := w.svc.AskPrice(context.Background(), w.customer, st, rep, testNow)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, Price{StoreItemID: item, UnitPaise: 76000, ValidUntil: p.ValidUntil, MaxQuantity: 2}, p)
	require.True(t, p.ValidUntil.After(testNow))
	_, ok, err = w.svc.AskPrice(context.Background(), w.staff, st, rep, testNow)
	require.NoError(t, err)
	require.False(t, ok, "another customer")
	_, ok, err = w.svc.AskPrice(context.Background(), w.customer, other, rep, testNow)
	require.NoError(t, err)
	require.False(t, ok, "another store")

	// The other shop says No: every recipient answered, so the ask closes.
	_, err = w.svc.reply(w.asStaff(other, authz.Owner), other.String(), aid, &asksapi.AskReplyCreate{Answer: "no"})
	require.NoError(t, err)
	det, err = w.svc.getMine(w.asCustomer(), aid)
	require.NoError(t, err)
	require.Equal(t, asksapi.AskStatus("closed"), det.Ask.Status)
	require.Equal(t, asksapi.AskCloseReason(ReasonAllReplied), *det.Ask.CloseReason)
	require.Equal(t, 1, det.Ask.YesCount)
}

func TestYesWithNoPriceBooksAtCataloguePrice(t *testing.T) {
	w := newWorld(t)
	prod := w.product("Paneer 200 g", 10000)
	st := w.store("dairy", 400)
	w.item(st, prod, 9000, false) // in the store's stock but not listed
	d, err := w.ask(prod, "")
	require.NoError(t, err)
	_, err = w.svc.reply(w.asStaff(st, authz.Cashier), st.String(), d.Ask.AskId, &asksapi.AskReplyCreate{Answer: asksapi.AskReplyCreateAnswerYes})
	require.NoError(t, err)
	det, err := w.svc.getMine(w.asCustomer(), d.Ask.AskId)
	require.NoError(t, err)
	require.Nil(t, det.Replies[0].PricePaise)
	require.Equal(t, 9000, *det.Replies[0].ListedPricePaise)
	p, ok, err := w.svc.AskPrice(context.Background(), w.customer, st, mustID(t, det.Replies[0].AskReplyId), testNow)
	require.NoError(t, err)
	require.True(t, ok)
	require.Zero(t, p.UnitPaise, "0 books at the catalogue price")
}

func TestCloseWindowRetentionAndDeletion(t *testing.T) {
	w := newWorld(t)
	st := w.store("near", 300)
	d, err := w.ask(ids.Zero, "")
	require.NoError(t, err)
	n, err := w.svc.CloseEnded(context.Background())
	require.NoError(t, err)
	require.Zero(t, n, "the window is still open")
	w.clk.Advance(24 * time.Hour)
	n, err = w.svc.CloseEnded(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	det, err := w.svc.getMine(w.asCustomer(), d.Ask.AskId)
	require.NoError(t, err)
	require.Equal(t, asksapi.AskCloseReason(ReasonWindowEnded), *det.Ask.CloseReason)

	// The demand rollup counts it once for the store and places its cell.
	rows, err := w.svc.RollupDay(context.Background(), testNow)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	dem, err := w.svc.Demand(context.Background(), st, testNow.Add(-24*time.Hour), testNow.Add(24*time.Hour))
	require.NoError(t, err)
	require.Len(t, dem, 1)
	require.Equal(t, int64(1), dem[0].Count)
	require.Equal(t, 1, w.count(`SELECT count(*) FROM insight_geo_cells c JOIN asks a ON a.geo_cell = c.geo_cell`))

	// Report blocks the customer for that store; deletion clears both.
	d2, err := w.ask(w.product("Oats 1 kg", 30000), "")
	require.NoError(t, err)
	require.NoError(t, w.svc.report(w.asStaff(st, authz.Owner), st.String(), d2.Ask.AskId, &asksapi.AskReportCreate{Reason: "spam"}))
	require.Equal(t, 1, w.count(`SELECT count(*) FROM ask_store_blocks`))
	require.Equal(t, 1, w.count(`SELECT count(*) FROM risk_flags WHERE rule = 'ask_reported'`))
	require.NoError(t, w.svc.db.InTx(context.Background(), func(ctx context.Context) error {
		return w.svc.OnAccountDelete(ctx, w.customer)
	}))
	require.NoError(t, w.svc.OnAccountDelete(context.Background(), w.customer), "idempotent")
	require.Zero(t, w.count(`SELECT count(*) FROM asks`))
	require.Zero(t, w.count(`SELECT count(*) FROM ask_recipients`))
	require.Zero(t, w.count(`SELECT count(*) FROM ask_store_blocks`))
	require.Equal(t, 1, w.count(`SELECT count(*) FROM rollup_daily_ask_demand`), "the rollup keeps counts only")

	// Retention deletes asks older than ask_retention_days.
	old := w.id()
	w.exec(`INSERT INTO asks (id, customer_id, query_norm, radius_m, status, close_reason, created_at, closed_at) VALUES ($1, $2, 'x', 1, 'closed', 'window_ended', $3, $3)`,
		ids.UUID(old), ids.UUID(w.staff), testNow.Add(-400*24*time.Hour))
	gone, err := w.svc.Retain(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), gone)
}

// TestOneReplyUnderARace: two staff answering the same ask at once give one
// reply and one InvalidTransition.
func TestOneReplyUnderARace(t *testing.T) {
	w := newWorld(t)
	st := w.store("near", 300)
	w.store("near2", 500)
	d, err := w.ask(ids.Zero, "")
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = w.svc.reply(w.asStaff(st, authz.Owner), st.String(), d.Ask.AskId, &asksapi.AskReplyCreate{Answer: asksapi.AskReplyCreateAnswerYes})
		}()
	}
	wg.Wait()
	ok := 0
	for _, e := range errs {
		if e == nil {
			ok++
		} else {
			require.Equal(t, string(genproblems.InvalidTransition), code(t, e))
		}
	}
	require.Equal(t, 1, ok)
	require.Equal(t, 1, w.count(`SELECT count(*) FROM ask_recipients WHERE answer IS NOT NULL`))
}

// TestNoExactDistanceIsStored: a recipient row keeps the band and a distance
// rounded to 500 m, never the exact one (three exact distances place the
// customer), and the table refuses an exact value.
func TestNoExactDistanceIsStored(t *testing.T) {
	w := newWorld(t)
	w.store("a", 320)
	w.store("b", 1340)
	w.store("c", 2710)
	_, err := w.ask(ids.Zero, "")
	require.NoError(t, err)
	require.Equal(t, 3, w.count(`SELECT count(*) FROM ask_recipients`))
	require.Zero(t, w.count(`SELECT count(*) FROM ask_recipients WHERE distance_m % 500 <> 0 OR distance_m < 500`))
	require.Equal(t, 1, w.count(`SELECT count(*) FROM ask_recipients WHERE distance_m = 1500 AND distance_band = '1_3km'`))
	_, err = w.tdb.OwnerPool.Exec(context.Background(), `UPDATE ask_recipients SET distance_m = 1340`)
	require.ErrorContains(t, err, "ck_ask_recipients_distance")
}

// TestReplyWaitsForAClose: the reply locks the ask row, so a Yes cannot land
// on an ask a customer is closing; it waits for the close and gets ask_closed.
func TestReplyWaitsForAClose(t *testing.T) {
	w := newWorld(t)
	st := w.store("near", 300)
	d, err := w.ask(ids.Zero, "")
	require.NoError(t, err)

	ctx := context.Background()
	closing, err := w.tdb.OwnerPool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = closing.Rollback(ctx) }()
	_, err = closing.Exec(ctx, `UPDATE asks SET status = 'closed', close_reason = 'customer_closed', closed_at = $2 WHERE id = $1`,
		ids.UUID(mustID(t, d.Ask.AskId)), testNow)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, err := w.svc.reply(w.asStaff(st, authz.Owner), st.String(), d.Ask.AskId, &asksapi.AskReplyCreate{Answer: asksapi.AskReplyCreateAnswerYes})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the reply did not wait for the close: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	require.NoError(t, closing.Commit(ctx))
	require.Equal(t, string(genproblems.AskClosed), code(t, <-done))
	require.Zero(t, w.count(`SELECT count(*) FROM ask_recipients WHERE answer IS NOT NULL`))
}

// countSender counts the notices by kind.
type countSender struct {
	mu sync.Mutex
	n  map[string]int
}

func (c *countSender) Send(_ context.Context, _ pgx.Tx, n notice.Notification) (ids.ID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n[n.Kind]++
	return ids.New(testNow), nil
}

// TestFirstYesPushOnceUnderARace: two shops saying Yes at once send the
// customer one push.
func TestFirstYesPushOnceUnderARace(t *testing.T) {
	w := newWorld(t)
	sender := &countSender{n: map[string]int{}}
	w.svc.notices = sender
	a, b := w.store("a", 300), w.store("b", 600)
	w.store("c", 900)
	d, err := w.ask(ids.Zero, "")
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, st := range []ids.ID{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = w.svc.reply(w.asStaff(st, authz.Owner), st.String(), d.Ask.AskId, &asksapi.AskReplyCreate{Answer: asksapi.AskReplyCreateAnswerYes})
		}()
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, 1, sender.n[notice.KindAskReply])
	require.Equal(t, 1, w.count(`SELECT count(*) FROM asks WHERE first_yes_notified_at IS NOT NULL`))
}

// TestCustomerClosedIsNotDemand: only asks that ran their course count in
// the demand rollup.
func TestCustomerClosedIsNotDemand(t *testing.T) {
	w := newWorld(t)
	w.store("near", 300)
	d, err := w.ask(ids.Zero, "")
	require.NoError(t, err)
	_, err = w.svc.closeMine(w.asCustomer(), d.Ask.AskId)
	require.NoError(t, err)
	rows, err := w.svc.RollupDay(context.Background(), testNow)
	require.NoError(t, err)
	require.Zero(t, rows)
	require.Zero(t, w.count(`SELECT count(*) FROM rollup_daily_ask_demand`))
}

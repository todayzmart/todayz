package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/testclock"
	"github.com/todayz/backend/internal/testkit"
)

// The W2-02 tests (SC-BK, 08-testing.md): the real router and pipeline on a
// fresh database, with the API's insert-only River client so placing and
// accepting enqueue their timers, and the timers run through the registered
// kinds (internal/jobs/orders.go) on a platformjobs.Fake with the same clock.
// Pickup codes are read into variables and never printed: a failing
// assertion on a response that may carry one names the status only.

// logBuf collects the API's JSON logs (no redaction backstop) so a test can
// prove the code never reaches them.
type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// orderShop is a verified business with a live, always-open store S (plus
// S2, and a second business with a live store SY), its owner, a manager, a
// cashier of S and a cashier of S2 only, three items of S with stock, and
// two signed-in customers.
type orderShop struct {
	t                                          *testing.T
	a                                          *bizAPI
	cfg                                        *config.Config
	logs                                       *logBuf
	runner                                     *platformjobs.Fake
	owner, manager, cashier, cashierS2, owner2 string
	B, S, S2, SY                               string
	sid                                        string
	cust, cust2                                string
	atta, butter, salt                         string // store item ids of S
	price                                      map[string]int
	sessions                                   []*session
}

// session is one signed-in principal: the token field it keeps fresh and
// its refresh cookie. Access tokens last authn.AccessTTL (15 min), shorter
// than a pickup hold, so every clock move refreshes them (AU-07 rotation).
type session struct {
	tok    *string
	app    string
	cookie *http.Cookie
}

var appOrigins = map[string]string{"customer": "https://app.todayz.localhost", "business": "https://business.todayz.localhost"}

func cookieOf(t *testing.T, r resp) *http.Cookie {
	t.Helper()
	cs := (&http.Response{Header: r.hdr}).Cookies()
	require.Len(t, cs, 1, "one refresh cookie")
	return cs[0]
}

// login signs phone into app (as bizAPI.signIn does) and keeps the session
// so advance and set can refresh it.
func (o *orderShop) login(dst *string, phone, app string) {
	o.t.Helper()
	a := o.a
	w := a.call("", http.MethodPost, "/v1/auth/otp/send", map[string]string{
		"phone": phone, "app": app, "purpose": "login", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
	}, nil)
	require.Equal(o.t, http.StatusOK, w.code, "otp send: %d", w.code)
	w = a.call("", http.MethodPost, "/v1/auth/otp/verify", map[string]string{
		"app": app, "requestId": w.body["requestId"].(string), "code": testOTP, "deviceId": ids.New(a.clk.Now()).String(),
	}, map[string]string{"User-Agent": "orders-http-test"})
	require.Equal(o.t, http.StatusOK, w.code, "otp verify: %d", w.code)
	a.clk.Advance(31 * time.Second)
	*dst = w.body["accessToken"].(string)
	o.sessions = append(o.sessions, &session{tok: dst, app: app, cookie: cookieOf(o.t, w)})
}

// refresh renews every session's access token at the current clock.
func (o *orderShop) refresh() {
	o.t.Helper()
	for _, s := range o.sessions {
		w := o.a.call("", http.MethodPost, "/v1/auth/refresh", map[string]string{"app": s.app}, map[string]string{
			"Origin": appOrigins[s.app], "X-TZ-Client": "orders-http-test/1.0", "Cookie": s.cookie.Name + "=" + s.cookie.Value,
		})
		require.Equal(o.t, http.StatusOK, w.code, "refresh: %d", w.code)
		*s.tok = w.body["accessToken"].(string)
		s.cookie = cookieOf(o.t, w)
	}
}

// advance moves the clock and refreshes every session.
func (o *orderShop) advance(d time.Duration) {
	o.t.Helper()
	o.a.clk.Advance(d)
	o.refresh()
}

// set puts the clock at t and refreshes every session.
func (o *orderShop) set(t time.Time) {
	o.t.Helper()
	o.a.clk.Set(t)
	o.refresh()
}

const (
	custPhone  = strangerPhone // the customer
	cust2Phone = manager2Phone // a second customer
)

// allDay is a store open every hour of every day (business.ParseHours).
var allDay = func() map[string]any {
	w := map[string]any{}
	for _, d := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
		w[d] = []any{map[string]string{"open": "00:00", "close": "24:00"}}
	}
	return map[string]any{"weekly": w, "holidays": []any{}}
}()

func newOrderShop(t *testing.T) *orderShop {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	cfg, err := config.Load(env(localEnv(tdb)), nil)
	require.NoError(t, err)
	clk := clock.NewFake(time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)) // Friday 11:30 IST
	lb := &logBuf{}
	log := slog.New(slog.NewJSONHandler(lb, &slog.HandlerOptions{Level: slog.LevelDebug}))
	q, err := queue.NewInsertClient(tdb.Pool, queue.Options{Log: log})
	require.NoError(t, err)
	st := settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{Log: log})
	d := deps{
		Config: cfg, Log: log, Clock: clk,
		DB: db.New(tdb.Pool), Ready: &health.Readiness{}, Settings: st, Queue: q,
		TestClock: testclock.New(clock.NewOffset(nil), nopStore{}, log),
	}
	h, err := buildRouter(d, idempotency.NewMemoryStore(), contractValidator(t))
	require.NoError(t, err)
	a := &bizAPI{t: t, h: h, clk: clk, tdb: tdb}

	reg := platformjobs.NewRegistry()
	jobs.Register(reg, jobs.Deps{Pool: tdb.Pool, Clock: clk, Settings: st, Config: cfg, Log: log})
	o := &orderShop{t: t, a: a, cfg: cfg, logs: lb, runner: platformjobs.NewFake(reg, clk)}

	o.login(&o.owner, ownerPhone, "business")
	o.login(&o.owner2, otherOwnerPhone, "business")
	bid, sid := a.newBusiness(o.owner, "Lakshmi Traders")
	o.sid = sid
	o.B, o.S = "/v1/biz/businesses/"+bid, "/v1/biz/businesses/"+bid+"/stores/"+sid
	r := a.call(o.owner, http.MethodPost, o.B+"/stores", map[string]any{"name": "Lakshmi Traders 2", "locality": "BTM Layout"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	sid2 := r.body["id"].(string)
	o.S2 = o.B + "/stores/" + sid2
	by, sy := a.newBusiness(o.owner2, "Ganesh Stores")
	o.SY = "/v1/biz/businesses/" + by + "/stores/" + sy

	invite := func(dst *string, phone, role string, stores []string) {
		body := map[string]any{"phone": phone, "role": role}
		if stores != nil {
			body["storeIds"] = stores
		}
		r := a.call(o.owner, http.MethodPost, o.B+"/staff", body, nil)
		require.Equal(t, http.StatusCreated, r.code, r.raw)
		staff := r.body["id"].(string)
		o.login(dst, phone, "business")
		r = a.call(*dst, http.MethodPost, o.B+"/staff/"+staff+"/accept", nil, nil)
		require.Equal(t, http.StatusOK, r.code, r.raw)
	}
	invite(&o.manager, managerPhone, "manager", nil)
	invite(&o.cashier, cashierPhone, "cashier", []string{sid})
	invite(&o.cashierS2, cashier2Phone, "cashier", []string{sid2})

	hours, err := json.Marshal(allDay)
	require.NoError(t, err)
	for _, b := range []string{bid, by} {
		a.exec(`UPDATE businesses SET status = 'verified' WHERE id = $1`, uuidOf(t, b))
	}
	for _, s := range []string{sid, sid2, sy} {
		a.exec(`UPDATE stores SET status = 'live', live_at = $1, accepts_pickup = true, hours = $2 WHERE id = $3`,
			clk.Now(), hours, uuidOf(t, s))
	}

	atta, butter, salt := ids.New(clk.Now()).String(), ids.New(clk.Now()).String(), ids.New(clk.Now()).String()
	a.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, mrp_paise, barcodes) VALUES
	  ($1, 'Aashirvaad Atta', '0190c47e-0000-7000-8000-000000000101', 5, 'kg', 30000, '{8901725133856}'),
	  ($2, 'Amul Butter', '0190c47e-0000-7000-8000-000000000101', 100, 'g', 6000, '{8901262010016}'),
	  ($3, 'Tata Salt', '0190c47e-0000-7000-8000-000000000101', 1, 'kg', 2800, '{8904043901015}')`,
		uuidOf(t, atta), uuidOf(t, butter), uuidOf(t, salt))
	o.price = map[string]int{}
	item := func(product string, price int) string {
		r := a.call(o.manager, http.MethodPost, o.S+"/items", map[string]any{"productId": product, "pricePaise": price, "openingStock": 500}, nil)
		require.Equal(t, http.StatusCreated, r.code, r.raw)
		id := r.body["id"].(string)
		o.price[id] = price
		return id
	}
	o.atta, o.butter, o.salt = item(atta, 29000), item(butter, 5500), item(salt, 2500)

	o.login(&o.cust, custPhone, "customer")
	o.login(&o.cust2, cust2Phone, "customer")
	return o
}

// ext is a problem's extension (problem+json carries them under
// "extensions").
func ext(r resp, key string) any {
	e, _ := r.body["extensions"].(map[string]any)
	return e[key]
}

// noCode: an order body shows no pickup code (absent or null).
func noCode(t *testing.T, body map[string]any, msg string) {
	t.Helper()
	require.Nil(t, body["code"], msg)
}

// timerJobs counts a timer kind's River jobs for an order due at at. The
// row's args are the platform envelope; the kind's own args are under "a".
func (o *orderShop) timerJobs(kind, id string, at time.Time) int {
	return o.a.count(`SELECT count(*) FROM river_job WHERE kind = $1 AND args->'a'->>'orderId' = $2 AND scheduled_at = $3`, kind, id, at)
}

// --- helpers ---------------------------------------------------------------------

type line struct {
	item string
	qty  int
}

func (o *orderShop) total(lines []line) int {
	n := 0
	for _, l := range lines {
		n += o.price[l.item] * l.qty
	}
	return n
}

func (o *orderShop) cart(lines []line, expected int) map[string]any {
	items := make([]any, 0, len(lines))
	for _, l := range lines {
		items = append(items, map[string]any{"storeItemId": l.item, "quantity": l.qty})
	}
	return map[string]any{"storeId": o.sid, "fulfilment": "pickup", "items": items, "creditsToUse": 0, "expectedTotalPaise": expected}
}

// quote prices lines (POST /v1/me/checkout/quote takes no expected total).
func (o *orderShop) quote(lines ...line) resp {
	o.t.Helper()
	body := o.cart(lines, 0)
	delete(body, "expectedTotalPaise")
	return o.a.call(o.cust, http.MethodPost, "/v1/me/checkout/quote", body, nil)
}

// place books lines as tok with a fresh Idempotency-Key.
func (o *orderShop) place(tok string, lines ...line) resp {
	o.t.Helper()
	return o.a.call(tok, http.MethodPost, "/v1/me/orders", o.cart(lines, o.total(lines)), idem(o.a))
}

// order places one butter for the customer and returns its id.
func (o *orderShop) order() string {
	o.t.Helper()
	r := o.place(o.cust, line{o.butter, 1})
	require.Equal(o.t, http.StatusCreated, r.code, r.raw)
	return r.body["id"].(string)
}

// orderOf places lines for the customer and returns the order's id.
func (o *orderShop) orderOf(lines ...line) string {
	o.t.Helper()
	r := o.place(o.cust, lines...)
	require.Equal(o.t, http.StatusCreated, r.code, r.raw)
	return r.body["id"].(string)
}

// lineIDs are an order's line ids in the order the store's GET lists them.
func (o *orderShop) lineIDs(id string) []string {
	o.t.Helper()
	g := o.a.call(o.cashier, http.MethodGet, o.S+"/orders/"+id, nil, nil)
	require.Equal(o.t, http.StatusOK, g.code, g.raw)
	var out []string
	for _, l := range g.body["lines"].([]any) {
		out = append(out, l.(map[string]any)["id"].(string))
	}
	return out
}

// accepted places an order and has the cashier accept it.
func (o *orderShop) accepted() string {
	o.t.Helper()
	id := o.order()
	r := o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(o.t, http.StatusOK, r.code, r.raw)
	return id
}

// staff runs a store action on S.
func (o *orderShop) staff(tok, id, action string, body any) resp {
	o.t.Helper()
	return o.a.call(tok, http.MethodPost, o.S+"/orders/"+id+"/"+action, body, idem(o.a))
}

// me runs a customer action.
func (o *orderShop) me(tok, id, action string, body any) resp {
	o.t.Helper()
	return o.a.call(tok, http.MethodPost, "/v1/me/orders/"+id+"/"+action, body, idem(o.a))
}

// code reads the pickup code from the customer's GET. It never prints the
// response.
func (o *orderShop) code(id string) string {
	o.t.Helper()
	r := o.a.call(o.cust, http.MethodGet, "/v1/me/orders/"+id, nil, nil)
	require.Equal(o.t, http.StatusOK, r.code, "GET my order: status %d", r.code)
	c, ok := r.body["code"].(string)
	require.True(o.t, ok, "the owner's GET of a held order carries the code")
	require.Regexp(o.t, `^\d{4}$`, c, "the code is 4 digits")
	return c
}

func wrongCode(c string) string {
	var n int
	_, _ = fmt.Sscanf(c, "%d", &n)
	return fmt.Sprintf("%04d", (n+1)%10000)
}

func (o *orderShop) verify(tok, id, code string) resp {
	o.t.Helper()
	return o.staff(tok, id, "verify-code", map[string]any{"code": code, "paymentMethod": "upi"})
}

func (o *orderShop) status(id string) string {
	o.t.Helper()
	var s string
	require.NoError(o.t, o.a.tdb.OwnerPool.QueryRow(context.Background(), `SELECT status FROM orders WHERE id = $1`, uuidOf(o.t, id)).Scan(&s))
	return s
}

func (o *orderShop) times(id string) (acceptBy, holdUntil *time.Time) {
	o.t.Helper()
	require.NoError(o.t, o.a.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT accept_by, hold_until FROM orders WHERE id = $1`, uuidOf(o.t, id)).Scan(&acceptBy, &holdUntil))
	return acceptBy, holdUntil
}

// stock is an item's on_hand and reserved.
func (o *orderShop) stock(item string) (onHand, reserved int) {
	o.t.Helper()
	require.NoError(o.t, o.a.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT on_hand, reserved FROM store_items WHERE id = $1`, uuidOf(o.t, item)).Scan(&onHand, &reserved))
	return onHand, reserved
}

func (o *orderShop) held(id string) int {
	return o.a.count(`SELECT count(*) FROM stock_reservations WHERE order_id = $1 AND status = 'held'`, uuidOf(o.t, id))
}

func (o *orderShop) strikes() int {
	return o.a.count(`SELECT count(*) FROM strikes WHERE subject_type = 'customer'`)
}

// timer runs one timer kind for an order now, through the registered kind.
func (o *orderShop) timer(kind, id string, hold *time.Time) platformjobs.FakeRun {
	o.t.Helper()
	oid, err := ids.Parse(id)
	require.NoError(o.t, err)
	fr, err := o.runner.RunEvent(context.Background(), kind, orders.TimerArgs{OrderID: oid, HoldUntil: hold})
	require.NoError(o.t, err)
	return fr
}

func expiredRun(t *testing.T, fr platformjobs.FakeRun) {
	t.Helper()
	require.Equal(t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	require.EqualValues(t, 1, fr.Result.Summary["expired"])
}

func noopRun(t *testing.T, fr platformjobs.FakeRun) {
	t.Helper()
	require.Equal(t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	require.EqualValues(t, 0, fr.Result.Summary["expired"])
	require.EqualValues(t, 1, fr.Result.Summary["noop"])
}

func problemCode(r resp) string {
	c, _ := r.body["code"].(string)
	return c
}

// --- SC-BK ---------------------------------------------------------------------

// BK-01: a pickup order of 3 in-stock items is placed, accept_by is now +
// order_accept_timeout_min, the store is told, the accept timer is queued,
// and nothing is reserved (D-01).
func TestBK01PlacePickupOrder(t *testing.T) {
	o := newOrderShop(t)
	lines := []line{{o.atta, 1}, {o.butter, 2}, {o.salt, 3}}
	q := o.quote(lines...)
	require.Equal(t, http.StatusOK, q.code, q.raw)
	require.EqualValues(t, o.total(lines), q.body["totalPaise"])
	require.Equal(t, true, q.body["orderable"])

	now := o.a.clk.Now()
	r := o.place(o.cust, lines...)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)
	require.Equal(t, "placed", r.body["status"])
	noCode(t, r.body, "the create response carries no code")
	require.Len(t, r.body["lines"], 3)
	require.EqualValues(t, o.total(lines), r.body["totalPaise"])
	acceptBy, hold := o.times(id)
	require.NotNil(t, acceptBy)
	require.True(t, acceptBy.Equal(now.Add(10*time.Minute)), "accept_by = placed + order_accept_timeout_min")
	require.Nil(t, hold)

	for _, it := range []string{o.atta, o.butter, o.salt} {
		onHand, reserved := o.stock(it)
		require.Equal(t, 500, onHand)
		require.Zero(t, reserved, "nothing is reserved at placing (D-01)")
	}
	require.Zero(t, o.a.count(`SELECT count(*) FROM stock_reservations`))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1 AND to_status = 'placed' AND actor_kind = 'customer'`, uuidOf(t, id)))
	require.Positive(t, o.a.count(`SELECT count(*) FROM notifications WHERE kind = 'new_order' AND store_id = $1 AND ref_id = $2`,
		uuidOf(t, o.sid), uuidOf(t, id)), "the store is told")
	require.Equal(t, 1, o.timerJobs(orders.KindAcceptTimeout, id, *acceptBy), "the accept timer is queued for accept_by")

	// The store sees it in its inbox, with no code.
	l := o.a.call(o.cashier, http.MethodGet, o.S+"/orders", nil, nil)
	require.Equal(t, http.StatusOK, l.code, l.raw)
	require.Len(t, l.body["items"], 1)
	g := o.a.call(o.cashier, http.MethodGet, o.S+"/orders/"+id, nil, nil)
	require.Equal(t, http.StatusOK, g.code, g.raw)
	noCode(t, g.body, "")
	require.EqualValues(t, 5, g.body["codeAttemptsLeft"])
}

// BK-02: an item that went short after the quote: 409 insufficient_stock
// naming the line; nothing is placed or held.
func TestBK02InsufficientStockAfterQuote(t *testing.T) {
	o := newOrderShop(t)
	lines := []line{{o.butter, 3}}
	q := o.quote(lines...)
	require.Equal(t, http.StatusOK, q.code, q.raw)
	require.Equal(t, true, q.body["orderable"])

	r := o.a.call(o.manager, http.MethodPost, o.S+"/items/"+o.butter+"/movements", map[string]any{"kind": "damage", "quantity": 499}, idem(o.a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)

	r = o.place(o.cust, lines...)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "insufficient_stock", problemCode(r))
	require.Equal(t, o.butter, ext(r, "storeItemId"))
	require.EqualValues(t, 1, ext(r, "available"))
	require.Zero(t, o.a.count(`SELECT count(*) FROM orders`))
	require.Zero(t, o.a.count(`SELECT count(*) FROM stock_reservations`))
}

// BK-03: the price changed after the quote: 409 price_changed with the new
// quote; confirming the new total places the order.
func TestBK03PriceChangedAfterQuote(t *testing.T) {
	o := newOrderShop(t)
	lines := []line{{o.butter, 2}}
	old := o.total(lines)

	cur := o.a.call(o.manager, http.MethodGet, o.S+"/items/"+o.butter, nil, nil)
	require.Equal(t, http.StatusOK, cur.code, cur.raw)
	r := o.a.call(o.manager, http.MethodPatch, o.S+"/items/"+o.butter, map[string]any{"pricePaise": 5000}, ifMatch(cur))
	require.Equal(t, http.StatusOK, r.code, r.raw)

	r = o.a.call(o.cust, http.MethodPost, "/v1/me/orders", o.cart(lines, old), idem(o.a))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "price_changed", problemCode(r))
	quote, ok := ext(r, "quote").(map[string]any)
	require.True(t, ok, r.raw)
	require.EqualValues(t, 10000, quote["totalPaise"])
	require.Zero(t, o.a.count(`SELECT count(*) FROM orders`))

	r = o.a.call(o.cust, http.MethodPost, "/v1/me/orders", o.cart(lines, 10000), idem(o.a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.EqualValues(t, 10000, r.body["totalPaise"])
}

// BK-04: the same Idempotency-Key twice is one order; a different body
// under the same key is 422.
func TestBK04IdempotentCreate(t *testing.T) {
	o := newOrderShop(t)
	lines := []line{{o.butter, 1}}
	key := idem(o.a)
	body := o.cart(lines, o.total(lines))
	r1 := o.a.call(o.cust, http.MethodPost, "/v1/me/orders", body, key)
	require.Equal(t, http.StatusCreated, r1.code, r1.raw)
	r2 := o.a.call(o.cust, http.MethodPost, "/v1/me/orders", body, key)
	require.Equal(t, http.StatusCreated, r2.code, r2.raw)
	require.Equal(t, r1.body["id"], r2.body["id"], "the replay answers the first order")
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM orders`))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM river_job WHERE kind = $1`, orders.KindAcceptTimeout))

	other := o.cart([]line{{o.salt, 1}}, o.price[o.salt])
	r3 := o.a.call(o.cust, http.MethodPost, "/v1/me/orders", other, key)
	require.Equal(t, http.StatusUnprocessableEntity, r3.code, r3.raw)
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM orders`))

	r := o.a.call(o.cust, http.MethodPost, "/v1/me/orders", body, nil)
	require.Equal(t, http.StatusBadRequest, r.code, "an Idempotency-Key is required: %s", r.raw)
}

// BK-05: the store accepts in full: accepted, hold_until = accepted_at +
// pickup_hold_min, one held reservation per line, available drops and on
// hand does not, the customer sees the code, and the no-show timer is
// queued for the hold.
func TestBK05AcceptAll(t *testing.T) {
	o := newOrderShop(t)
	r := o.place(o.cust, line{o.butter, 2}, line{o.salt, 1})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)

	now := o.a.clk.Now()
	r = o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "accepted", r.body["status"])
	noCode(t, r.body, "")
	_, hold := o.times(id)
	require.NotNil(t, hold)
	require.True(t, hold.Equal(now.Add(30*time.Minute)), "hold_until = accepted_at + pickup_hold_min")
	require.Equal(t, 2, o.held(id))
	onHand, reserved := o.stock(o.butter)
	require.Equal(t, 500, onHand, "on hand is unchanged (BK-36)")
	require.Equal(t, 2, reserved)
	_, reserved = o.stock(o.salt)
	require.Equal(t, 1, reserved)
	o.code(id)
	require.Equal(t, 1, o.timerJobs(orders.KindNoShow, id, *hold))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM notifications WHERE kind = 'order_update' AND ref_id = $1 AND params->>'variant' = 'accepted'`, uuidOf(t, id)))

	r = o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusConflict, r.code, "a second accept: %s", r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))

	// More than the order asked for is an invalid value, not a partial accept.
	id2 := o.order()
	lineID := o.lineIDs(id2)[0]
	r = o.staff(o.cashier, id2, "accept", map[string]any{"lines": []any{map[string]any{"orderItemId": lineID, "quantity": 2}}})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Equal(t, "more than the order asked for", fieldsOf(r)["body.lines[0].quantity"], r.raw)
	require.Equal(t, "placed", o.status(id2))
	require.Zero(t, o.held(id2))
}

// BK-06: no answer by accept_by: the timer expires the order (not_accepted)
// and the customer is told.
func TestBK06AcceptTimeout(t *testing.T) {
	o := newOrderShop(t)
	id := o.order()
	o.advance(10 * time.Minute)
	expiredRun(t, o.timer(orders.KindAcceptTimeout, id, nil))
	require.Equal(t, "expired", o.status(id))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM orders WHERE status_reason = 'not_accepted'`))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1 AND to_status = 'expired' AND actor_kind = 'system' AND reason = 'not_accepted'`, uuidOf(t, id)))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM notifications WHERE kind = 'order_update' AND ref_id = $1 AND params->>'variant' = 'expired'`, uuidOf(t, id)))
	r := o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	// The credit hold's release is TestEndReleasesHoldHTTP's.
	// TODO(lead): the store's unaccepted counter is deferred (store_reliability).
}

// BK-07: the store rejects with a reason; the customer sees it.
func TestBK07Reject(t *testing.T) {
	o := newOrderShop(t)
	id := o.order()
	r := o.staff(o.cashier, id, "reject", map[string]any{"reason": "other"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "other needs a note: %s", r.raw)
	r = o.staff(o.cashier, id, "reject", map[string]any{"reason": "changed_mind"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "a customer reason: %s", r.raw)
	r = o.staff(o.cashier, id, "reject", map[string]any{"reason": "out_of_stock"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "rejected", r.body["status"])
	g := o.a.call(o.cust, http.MethodGet, "/v1/me/orders/"+id, nil, nil)
	require.Equal(t, http.StatusOK, g.code)
	require.Equal(t, "rejected", g.body["status"])
	require.Equal(t, "out_of_stock", g.body["statusReason"])
	noCode(t, g.body, "a finished order shows no code")
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM notifications WHERE kind = 'order_update' AND ref_id = $1 AND params->>'variant' = 'rejected'`, uuidOf(t, id)))

	id2 := o.accepted()
	r = o.staff(o.cashier, id2, "reject", map[string]any{"reason": "too_busy"})
	require.Equal(t, http.StatusConflict, r.code, "an accepted order is not rejected: %s", r.raw)
	id3 := o.order()
	r = o.staff(o.cashier, id3, "reject", map[string]any{"reason": "other", "note": "shop closing for a function"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
}

// BK-11: the right code collects: one sale_online movement per line
// consumes its reservation, on hand drops, the payment method is recorded.
func TestBK11CollectWithCode(t *testing.T) {
	o := newOrderShop(t)
	r := o.place(o.cust, line{o.butter, 2}, line{o.atta, 1})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)
	r = o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	code := o.code(id)

	r = o.verify(o.cashier, id, code)
	require.Equal(t, http.StatusOK, r.code, "verify: status %d", r.code)
	require.Equal(t, "collected", r.body["status"])
	require.Equal(t, "upi", r.body["paymentMethod"])
	require.Equal(t, 2, o.a.count(`SELECT count(*) FROM stock_movements WHERE source = 'sale_online' AND ref_type = 'order' AND ref_id = $1`, uuidOf(t, id)))
	require.Equal(t, 2, o.a.count(`SELECT count(*) FROM stock_reservations r JOIN stock_movements m ON m.id = r.consumed_movement_id
	  WHERE r.order_id = $1 AND r.status = 'consumed' AND m.delta = -r.quantity`, uuidOf(t, id)))
	onHand, reserved := o.stock(o.butter)
	require.Equal(t, 498, onHand)
	require.Zero(t, reserved)
	onHand, reserved = o.stock(o.atta)
	require.Equal(t, 499, onHand)
	require.Zero(t, reserved)
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM orders WHERE payment_status = 'paid' AND payment_method = 'upi' AND collected_at IS NOT NULL AND cashback_credits = 0`))

	r = o.verify(o.cashier, id, code)
	require.Equal(t, http.StatusConflict, r.code, "a second hand-over")
	require.Equal(t, "invalid_transition", problemCode(r))
	// Credits capture and cashback are TestCollectCapturesHoldHTTP's and
	// TestCollectPaysPlacedCashbackHTTP's.
	// TODO(lead): the review prompt is deferred (no WP sends it yet).
}

// BK-12: four wrong codes then the right one collects; five wrong lock the
// code; the customer's new code works and the old one does not.
func TestBK12WrongCodesAndLock(t *testing.T) {
	o := newOrderShop(t)
	id := o.accepted()
	code := o.code(id)
	for left := 4; left >= 1; left-- {
		r := o.verify(o.cashier, id, wrongCode(code))
		require.Equal(t, http.StatusUnprocessableEntity, r.code, "a wrong code is auth's code_invalid (422)")
		require.Equal(t, "code_invalid", problemCode(r))
		require.EqualValues(t, left, ext(r, "attemptsLeft"))
	}
	r := o.verify(o.cashier, id, code)
	require.Equal(t, http.StatusOK, r.code)
	require.Equal(t, "collected", r.body["status"])

	id2 := o.accepted()
	code2 := o.code(id2)
	for i := 1; i <= 5; i++ {
		r = o.verify(o.cashier, id2, wrongCode(code2))
		if i < 5 {
			require.Equal(t, http.StatusUnprocessableEntity, r.code)
			require.Equal(t, "code_invalid", problemCode(r))
			require.EqualValues(t, 5-i, ext(r, "attemptsLeft"))
		} else {
			require.Equal(t, http.StatusConflict, r.code)
			require.Equal(t, "code_locked", problemCode(r), "the fifth wrong code locks")
		}
	}
	r = o.verify(o.cashier, id2, code2)
	require.Equal(t, http.StatusConflict, r.code)
	require.Equal(t, "code_locked", problemCode(r), "a locked code refuses the right one too")
	g := o.a.call(o.cashier, http.MethodGet, o.S+"/orders/"+id2, nil, nil)
	require.Equal(t, http.StatusOK, g.code, g.raw)
	require.Equal(t, true, g.body["codeLocked"])
	require.EqualValues(t, 0, g.body["codeAttemptsLeft"])
	c := o.a.call(o.cust, http.MethodGet, "/v1/me/orders/"+id2, nil, nil)
	require.Equal(t, http.StatusOK, c.code)
	require.Equal(t, true, c.body["codeLocked"])
	noCode(t, c.body, "a locked code is not shown")

	r = o.me(o.cust, id, "new-code", nil)
	require.Equal(t, http.StatusConflict, r.code, "a collected order gets no new code: %s", r.raw)
	r = o.me(o.cust, id2, "new-code", nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, false, r.body["codeLocked"])
	noCode(t, r.body, "an action's response carries no code")
	fresh := o.code(id2)
	r = o.me(o.cust, id2, "new-code", nil)
	require.Equal(t, http.StatusConflict, r.code, "an unlocked code is not replaced: %s", r.raw)
	if fresh != code2 {
		r = o.verify(o.cashier, id2, code2)
		require.Equal(t, http.StatusUnprocessableEntity, r.code)
		require.Equal(t, "code_invalid", problemCode(r), "the old code no longer works")
	}
	r = o.verify(o.cashier, id2, fresh)
	require.Equal(t, http.StatusOK, r.code)
	require.Equal(t, "collected", r.body["status"])
}

// BK-13: a code after expiry is 409 invalid_transition: no sale, no movement.
func TestBK13CodeAfterExpiry(t *testing.T) {
	o := newOrderShop(t)
	id := o.accepted()
	code := o.code(id)
	_, hold := o.times(id)
	o.advance(30 * time.Minute)
	expiredRun(t, o.timer(orders.KindNoShow, id, hold))
	r := o.verify(o.cashier, id, code)
	require.Equal(t, http.StatusConflict, r.code)
	require.Equal(t, "invalid_transition", problemCode(r))
	require.Zero(t, o.a.count(`SELECT count(*) FROM stock_movements WHERE source = 'sale_online'`))
	require.Equal(t, "expired", o.status(id))
}

// BK-14: no pickup within pickup_hold_min: expired (no_show), a strike for
// the customer, the reservation released.
func TestBK14NoShow(t *testing.T) {
	o := newOrderShop(t)
	id := o.accepted()
	_, hold := o.times(id)
	_, reserved := o.stock(o.butter)
	require.Equal(t, 1, reserved)

	o.advance(30 * time.Minute)
	expiredRun(t, o.timer(orders.KindNoShow, id, hold))
	require.Equal(t, "expired", o.status(id))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM orders WHERE status_reason = 'no_show'`))
	require.Equal(t, 1, o.strikes())
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM strikes WHERE reason = 'no_show' AND order_id = $1`, uuidOf(t, id)))
	require.Zero(t, o.held(id))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM stock_reservations WHERE order_id = $1 AND status = 'released' AND released_reason = 'no_show'`, uuidOf(t, id)))
	onHand, reserved := o.stock(o.butter)
	require.Equal(t, 500, onHand)
	require.Zero(t, reserved)
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM customer_profiles WHERE strikes_count_30d = 1 AND limited_until IS NULL`))
	// The credit hold's release is TestEndReleasesHoldHTTP's.
	// TODO(lead): BK-16 (the limit after noshow_strikes) is deferred to its
	// own test with the banner.
}

// BK-15: "+10 min" before the hold ends adds pickup_extension_min once; a
// second tap and a tap after the hold are refused. The first no-show timer
// then finds the hold moved and does nothing; the extension's timer expires
// the order.
func TestBK15Extend(t *testing.T) {
	o := newOrderShop(t)
	id := o.accepted()
	_, hold := o.times(id)
	o.advance(25 * time.Minute)
	r := o.me(o.cust, id, "extend", nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, false, r.body["canExtend"])
	_, newHold := o.times(id)
	require.True(t, newHold.Equal(hold.Add(10*time.Minute)), "hold_until + pickup_extension_min")
	r = o.me(o.cust, id, "extend", nil)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "extension_already_used", problemCode(r))
	require.Equal(t, 1, o.timerJobs(orders.KindNoShow, id, *newHold), "the extension queues its own timer")

	o.advance(5 * time.Minute) // the first hold
	noopRun(t, o.timer(orders.KindNoShow, id, hold))
	require.Equal(t, "accepted", o.status(id))
	o.advance(10 * time.Minute) // the extended hold
	expiredRun(t, o.timer(orders.KindNoShow, id, newHold))
	require.Equal(t, "expired", o.status(id))

	id2 := o.accepted()
	o.advance(31 * time.Minute)
	r = o.me(o.cust, id2, "extend", nil)
	require.Equal(t, http.StatusConflict, r.code, "after the hold: %s", r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))
	id3 := o.order()
	r = o.me(o.cust, id3, "extend", nil)
	require.Equal(t, http.StatusConflict, r.code, "a placed order has no hold: %s", r.raw)
}

// BK-17: the customer cancels in placed, accepted and ready; stock goes
// back; a collected order cannot be cancelled.
func TestBK17CustomerCancel(t *testing.T) {
	o := newOrderShop(t)
	cancel := func(id string) resp {
		return o.me(o.cust, id, "cancel", map[string]any{"reason": "changed_mind"})
	}
	id := o.order()
	r := o.me(o.cust, id, "cancel", map[string]any{"reason": "declined_partial"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "declined_partial is confirm-partial's: %s", r.raw)
	r = o.me(o.cust, id, "cancel", map[string]any{"reason": "out_of_stock"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "a shop reason: %s", r.raw)
	r = o.me(o.cust, id, "cancel", map[string]any{"reason": "other"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "other needs a note: %s", r.raw)
	r = cancel(id)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "cancelled", r.body["status"])

	id = o.accepted()
	r = cancel(id)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Zero(t, o.held(id))

	id = o.accepted()
	r = o.staff(o.cashier, id, "ready", nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "ready", r.body["status"])
	r = o.me(o.cust, id, "cancel", map[string]any{"reason": "other", "note": "bought it elsewhere"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Zero(t, o.held(id))
	_, reserved := o.stock(o.butter)
	require.Zero(t, reserved, "every hold is back")
	require.Equal(t, 3, o.a.count(`SELECT count(*) FROM order_events WHERE to_status = 'cancelled' AND actor_kind = 'customer'`))

	id = o.accepted()
	r = o.verify(o.cashier, id, o.code(id))
	require.Equal(t, http.StatusOK, r.code)
	r = cancel(id)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))

	// After accept_by a placed order is the timer's, not the customer's.
	id = o.order()
	o.advance(10 * time.Minute)
	r = cancel(id)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
}

// BK-18: a manager or owner cancels a held order with a ShopCancelReason;
// a cashier gets 403; a reason outside the list, or other without a note,
// is 422. No strike.
func TestBK18ShopCancel(t *testing.T) {
	o := newOrderShop(t)
	id := o.accepted()
	r := o.staff(o.cashier, id, "cancel", map[string]any{"reason": "out_of_stock"})
	require.Equal(t, http.StatusForbidden, r.code, "a cashier cannot cancel (D-65): %s", r.raw)
	r = o.staff(o.manager, id, "cancel", map[string]any{"reason": "changed_mind"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "a customer reason: %s", r.raw)
	r = o.staff(o.manager, id, "cancel", map[string]any{"reason": "too_busy"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "a reject reason: %s", r.raw)
	r = o.staff(o.manager, id, "cancel", map[string]any{"reason": "other"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "other needs a note: %s", r.raw)
	require.Equal(t, "accepted", o.status(id))
	r = o.staff(o.manager, id, "cancel", map[string]any{"reason": "customer_unreachable"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "cancelled", r.body["status"])
	require.Equal(t, "customer_unreachable", r.body["statusReason"])
	require.Zero(t, o.held(id))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM notifications WHERE kind = 'order_update' AND ref_id = $1 AND NOT params ? 'variant'`, uuidOf(t, id)),
		"the customer is told (the general text: the catalogue has no cancelled variant)")

	id = o.accepted()
	r = o.staff(o.cashier, id, "ready", nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = o.staff(o.owner, id, "cancel", map[string]any{"reason": "other", "note": "power cut"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Zero(t, o.strikes(), "no strike for a shop cancel (D-68)")

	id = o.order()
	r = o.staff(o.manager, id, "cancel", map[string]any{"reason": "closing_soon"})
	require.Equal(t, http.StatusConflict, r.code, "a placed order is rejected, not cancelled: %s", r.raw)
}

// BK-19: the customer's cancel and the shop's accept at the same instant.
// Both take the order row lock, so they serialise:
//   - cancel first: the order is cancelled and the accept is 409
//     invalid_transition (the row's "exactly one wins, the loser gets 409");
//   - accept first: the cancel then applies to an accepted order, which 03
//     §4.1 allows the customer, so it is 200 too and the hold is released.
//
// TODO(lead): 08's BK-19 expects a 409 for the loser in both orders. That
// needs the cancel to name the state it saw (If-Match on the order version),
// a contract change. Until then both orders end cancelled with no stock held.
// Either way the stock and the events must agree.
func TestBK19RaceCancelVersusAccept(t *testing.T) {
	o := newOrderShop(t)
	outcomes := map[string]int{}
	for i := 0; i < 8; i++ {
		id := o.order()
		var acc, can resp
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			acc = o.staff(o.cashier, id, "accept", map[string]any{})
		}()
		go func() {
			defer wg.Done()
			<-start
			can = o.me(o.cust, id, "cancel", map[string]any{"reason": "changed_mind"})
		}()
		close(start)
		wg.Wait()
		require.Equal(t, http.StatusOK, can.code, "the cancel always lands: %s", can.raw)
		require.Equal(t, "cancelled", o.status(id))
		require.Zero(t, o.held(id), "a cancelled order holds nothing")
		events := o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1`, uuidOf(t, id))
		switch acc.code {
		case http.StatusConflict: // cancel first
			require.Equal(t, "invalid_transition", problemCode(acc))
			require.Equal(t, 2, events, "placed, cancelled")
			require.Zero(t, o.a.count(`SELECT count(*) FROM stock_reservations WHERE order_id = $1`, uuidOf(t, id)))
			outcomes["cancel first"]++
		case http.StatusOK: // accept first
			require.Equal(t, 3, events, "placed, accepted, cancelled")
			require.Equal(t, 1, o.a.count(`SELECT count(*) FROM stock_reservations WHERE order_id = $1 AND status = 'released'`, uuidOf(t, id)))
			outcomes["accept first"]++
		default:
			t.Fatalf("accept: %d %s", acc.code, acc.raw)
		}
	}
	_, reserved := o.stock(o.butter)
	require.Zero(t, reserved, "nothing stays reserved")
	onHand, _ := o.stock(o.butter)
	require.Equal(t, 500, onHand, "no stock moved")
	t.Logf("BK-19 outcomes over 8 rounds: %v", outcomes)
}

// BK-20: the no-show timer and a code verify around hold_until: exactly one
// outcome; a collected order has no strike.
func TestBK20RaceExpiryVersusVerify(t *testing.T) {
	o := newOrderShop(t)
	race := func(offset time.Duration) (resp, platformjobs.FakeRun, string) {
		id := o.accepted()
		code := o.code(id)
		_, hold := o.times(id)
		o.set(hold.Add(offset))
		var v resp
		var fr platformjobs.FakeRun
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			v = o.verify(o.cashier, id, code)
		}()
		go func() {
			defer wg.Done()
			<-start
			oid, _ := ids.Parse(id)
			fr, _ = o.runner.RunEvent(context.Background(), orders.KindNoShow, orders.TimerArgs{OrderID: oid, HoldUntil: hold})
		}()
		close(start)
		wg.Wait()
		return v, fr, id
	}
	for i := 0; i < 4; i++ {
		// A second before the hold ends: the hand-over wins; the timer is
		// early (retried) or finds the order collected.
		strikes := o.strikes()
		v, fr, id := race(-time.Second)
		require.Equal(t, http.StatusOK, v.code, "verify before the hold: %d", v.code)
		require.Equal(t, "collected", o.status(id))
		if fr.Status == platformjobs.StatusSucceeded {
			require.EqualValues(t, 1, fr.Result.Summary["noop"])
		} else {
			require.Equal(t, platformjobs.StatusFailed, fr.Status)
			require.ErrorContains(t, fr.Err, "before its deadline")
		}
		require.Equal(t, strikes, o.strikes(), "no strike for a collected order")

		// A second after: the timer expires it; the hand-over is refused.
		v, fr, id = race(time.Second)
		require.Equal(t, http.StatusConflict, v.code)
		require.Equal(t, "invalid_transition", problemCode(v))
		expiredRun(t, fr)
		require.Equal(t, "expired", o.status(id))
		require.Equal(t, strikes+1, o.strikes())
		require.Zero(t, o.a.count(`SELECT count(*) FROM stock_movements WHERE source = 'sale_online' AND ref_id = $1`, uuidOf(t, id)))
	}
}

// BK-21: a timer that runs twice, late, early, or for an order that moved
// on changes nothing it should not.
func TestBK21TimerLateAndDuplicate(t *testing.T) {
	o := newOrderShop(t)

	// Accept timer: early → fails (retried) and changes nothing.
	id := o.order()
	fr := o.timer(orders.KindAcceptTimeout, id, nil)
	require.Equal(t, platformjobs.StatusFailed, fr.Status)
	require.ErrorContains(t, fr.Err, "before its deadline")
	require.Equal(t, "placed", o.status(id))

	// Late by 5 minutes → expires; a second run is a no-op.
	o.advance(15 * time.Minute)
	expiredRun(t, o.timer(orders.KindAcceptTimeout, id, nil))
	noopRun(t, o.timer(orders.KindAcceptTimeout, id, nil))
	require.Equal(t, "expired", o.status(id))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1 AND to_status = 'expired'`, uuidOf(t, id)))

	// The accept timer of an accepted order does nothing.
	id = o.accepted()
	o.advance(11 * time.Minute)
	noopRun(t, o.timer(orders.KindAcceptTimeout, id, nil))
	require.Equal(t, "accepted", o.status(id))

	// No-show: twice, and late → one expiry, one strike.
	_, hold := o.times(id)
	o.set(hold.Add(5 * time.Minute))
	expiredRun(t, o.timer(orders.KindNoShow, id, hold))
	noopRun(t, o.timer(orders.KindNoShow, id, hold))
	require.Equal(t, 1, o.strikes())
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM customer_profiles WHERE strikes_count_30d = 1`))

	// No-show of a collected order does nothing.
	id = o.accepted()
	_, hold = o.times(id)
	r := o.verify(o.cashier, id, o.code(id))
	require.Equal(t, http.StatusOK, r.code)
	o.set(hold.Add(time.Minute))
	noopRun(t, o.timer(orders.KindNoShow, id, hold))
	require.Equal(t, "collected", o.status(id))
	require.Equal(t, 1, o.strikes())

	// An unknown order is a no-op too.
	noopRun(t, o.timer(orders.KindNoShow, ids.New(o.a.clk.Now()).String(), nil))
}

// BK-22: auto-accept is deferred, so an order at an auto_accept store stays
// placed for a manual decision.
func TestBK22AutoAcceptStoreStaysPlaced(t *testing.T) {
	o := newOrderShop(t)
	o.a.exec(`UPDATE stores SET auto_accept = true WHERE id = $1`, uuidOf(t, o.sid))
	id := o.order()
	require.Equal(t, "placed", o.status(id))
	require.Zero(t, o.held(id))
	// TODO(lead): auto-accept (BK-22's accepted-instantly half) is deferred.
}

// --- deletion, tenancy, roles, the code ----------------------------------------------

// D-31 / BK-44: no account deletion while an order is open.
func TestOrdersBlockAccountDeletion(t *testing.T) {
	o := newOrderShop(t)
	id := o.order()
	r := o.a.deleteMe(o.cust, custPhone, "customer")
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "account_has_active_order", problemCode(r))
	require.Zero(t, o.a.count(`SELECT count(*) FROM users WHERE status = 'deleted'`))

	r = o.me(o.cust, id, "cancel", map[string]any{"reason": "changed_mind"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	o.advance(31 * time.Second) // past the OTP resend gap
	r = o.a.deleteMe(o.cust, custPhone, "customer")
	require.Equal(t, http.StatusNoContent, r.code, r.raw)
}

// Another customer, another business, a cashier of another store and an
// anonymous caller reach nothing of an order.
func TestOrdersTenancyHTTP(t *testing.T) {
	o := newOrderShop(t)
	id := o.order()
	r := o.a.call("", http.MethodGet, "/v1/me/orders/"+id, nil, nil)
	require.Equal(t, http.StatusUnauthorized, r.code, r.raw)
	r = o.a.call(o.cust2, http.MethodGet, "/v1/me/orders/"+id, nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = o.me(o.cust2, id, "cancel", map[string]any{"reason": "changed_mind"})
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = o.a.call(o.cust2, http.MethodGet, "/v1/me/orders", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Empty(t, r.body["items"])

	// The other business's owner: their own store path finds no such order;
	// S's path is not theirs.
	r = o.a.call(o.owner2, http.MethodGet, o.SY+"/orders/"+id, nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = o.a.call(o.owner2, http.MethodPost, o.SY+"/orders/"+id+"/accept", map[string]any{}, idem(o.a))
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = o.a.call(o.owner2, http.MethodGet, o.S+"/orders/"+id, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = o.staff(o.owner2, id, "accept", map[string]any{})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)

	// A cashier of S2 only: not S; S2's path finds no such order.
	r = o.staff(o.cashierS2, id, "accept", map[string]any{})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = o.a.call(o.cashierS2, http.MethodPost, o.S2+"/orders/"+id+"/accept", map[string]any{}, idem(o.a))
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = o.a.call(o.cashierS2, http.MethodGet, o.S2+"/orders", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Empty(t, r.body["items"])

	// A customer token on the store routes.
	r = o.staff(o.cust, id, "accept", map[string]any{})
	require.Contains(t, []int{http.StatusForbidden, http.StatusUnauthorized}, r.code, r.raw)
	require.Equal(t, "placed", o.status(id))

	// The cashier of S does every counter action except cancel (D-65).
	r = o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = o.staff(o.cashier, id, "ready", nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = o.staff(o.cashier, id, "cancel", map[string]any{"reason": "out_of_stock"})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = o.verify(o.cashier, id, o.code(id))
	require.Equal(t, http.StatusOK, r.code)

	// Delivery and return actions are not built: 409 for a visible order,
	// 404 for another store's.
	for _, act := range []struct {
		path string
		body any
	}{
		{"dispatch", nil},
		{"fail-delivery", map[string]any{"reason": "customer_unreachable"}},
		{"confirm-return", map[string]any{"outcome": "returned"}},
	} {
		r = o.staff(o.cashier, id, act.path, act.body)
		require.Equal(t, http.StatusConflict, r.code, "%s: %s", act.path, r.raw)
		r = o.a.call(o.cashierS2, http.MethodPost, o.S2+"/orders/"+id+"/"+act.path, act.body, idem(o.a))
		require.Equal(t, http.StatusNotFound, r.code, "%s: %s", act.path, r.raw)
	}
}

// The pickup code never reaches a log, an order event, job arguments, a
// notification, a stream payload, an action's response or the store's view.
func TestOrderCodeNeverLeaks(t *testing.T) {
	o := newOrderShop(t)
	id := o.accepted()
	code := o.code(id)
	bad := wrongCode(code)
	for i := 0; i < 2; i++ {
		r := o.verify(o.cashier, id, bad)
		require.Equal(t, http.StatusUnprocessableEntity, r.code)
		require.NotContains(t, r.raw, `"`+code+`"`)
		require.NotContains(t, r.raw, `"`+bad+`"`, "a refusal does not echo the code sent")
	}
	g := o.a.call(o.cashier, http.MethodGet, o.S+"/orders/"+id, nil, nil)
	require.Equal(t, http.StatusOK, g.code)
	noCode(t, g.body, "")
	l := o.a.call(o.cust, http.MethodGet, "/v1/me/orders", nil, nil)
	require.Equal(t, http.StatusOK, l.code)
	require.NotContains(t, l.raw, `"code"`, "the list carries no code")
	ext := o.me(o.cust, id, "extend", nil)
	require.Equal(t, http.StatusOK, ext.code)
	noCode(t, ext.body, "")
	r := o.verify(o.cashier, id, code)
	require.Equal(t, http.StatusOK, r.code)
	noCode(t, r.body, "")

	// The second order expires through the timer, so its path is covered too.
	id2 := o.accepted()
	code2 := o.code(id2)
	_, hold := o.times(id2)
	o.set(hold.Add(time.Second))
	expiredRun(t, o.timer(orders.KindNoShow, id2, hold))

	for _, c := range []string{code, code2} {
		require.Zero(t, o.a.count(`SELECT count(*) FROM order_events WHERE meta <> '{}'::jsonb OR reason = $1`, c))
		require.Zero(t, o.a.count(`SELECT count(*) FROM notifications WHERE params::text LIKE '%' || $1 || '%'`, `"`+c+`"`))
		require.Zero(t, o.a.count(`SELECT count(*) FROM outbox_events WHERE payload ? 'code' OR payload::text LIKE '%' || $1 || '%'`, `"`+c+`"`),
			"stream payloads carry no code")
	}
	require.Positive(t, o.a.count(`SELECT count(*) FROM outbox_events`))
	require.Zero(t, o.a.count(`SELECT count(*) FROM river_job WHERE kind LIKE 'order_%' AND ((args->'a') - 'orderId' - 'holdUntil') <> '{}'::jsonb`),
		"timer arguments are the order id and the hold only")
	require.Positive(t, o.a.count(`SELECT count(*) FROM river_job WHERE kind LIKE 'order_%'`))

	// Logs: no attribute is the code, and none names it.
	named := regexp.MustCompile(`(?i)code["=:\s]+"?(` + code + `|` + code2 + `|` + bad + `)\b`)
	logs := o.logs.String()
	require.NotEmpty(t, logs)
	for _, ln := range strings.Split(strings.TrimSpace(logs), "\n") {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(ln), &rec))
		delete(rec, "time")
		walkStrings(rec, func(s string) {
			if s == code || s == code2 || s == bad {
				t.Errorf("a log attribute is a pickup code (record %q)", rec["msg"])
			}
			if named.MatchString(s) {
				t.Errorf("a log attribute names a pickup code (record %q)", rec["msg"])
			}
		})
		b, _ := json.Marshal(rec)
		if named.Match(b) {
			t.Errorf("a log record names a pickup code (record %q)", rec["msg"])
		}
	}
}

func walkStrings(v any, f func(string)) {
	switch x := v.(type) {
	case string:
		f(x)
	case map[string]any:
		for _, e := range x {
			walkStrings(e, f)
		}
	case []any:
		for _, e := range x {
			walkStrings(e, f)
		}
	}
}

// --- the transition table (03 §4.1, 11 × 11 × 4) --------------------------------

type move struct{ from, to, actor string }

// planMoves is 03 §4.1: every (from, to, actor) the plan allows, with the
// reason when it is not built in W2-02.
var planMoves = map[move]string{
	{"placed", "accepted", "staff"}:                  "",
	{"placed", "awaiting_customer", "staff"}:         "",
	{"placed", "rejected", "staff"}:                  "",
	{"placed", "expired", "system"}:                  "",
	{"placed", "cancelled", "customer"}:              "",
	{"awaiting_customer", "cancelled", "customer"}:   "",
	{"accepted", "cancelled", "customer"}:            "",
	{"ready", "cancelled", "customer"}:               "",
	{"awaiting_customer", "accepted", "customer"}:    "",
	{"awaiting_customer", "cancelled", "system"}:     "",
	{"accepted", "ready", "staff"}:                   "",
	{"accepted", "collected", "staff"}:               "",
	{"ready", "collected", "staff"}:                  "",
	{"accepted", "expired", "system"}:                "", // pickup no-show; delivery dispatch_timeout is deferred
	{"ready", "expired", "system"}:                   "",
	{"accepted", "cancelled", "staff"}:               "",
	{"ready", "cancelled", "staff"}:                  "",
	{"accepted", "out_for_delivery", "staff"}:        "delivery (BK-23–25)",
	{"out_for_delivery", "delivered", "staff"}:       "delivery (BK-23–25)",
	{"out_for_delivery", "delivery_failed", "staff"}: "delivery (BK-23–25)",
	{"placed", "cancelled", "admin"}:                 "admin cancel (D-20)",
	{"awaiting_customer", "cancelled", "admin"}:      "admin cancel (D-20)",
	{"accepted", "cancelled", "admin"}:               "admin cancel (D-20)",
	{"ready", "cancelled", "admin"}:                  "admin cancel (D-20)",
	{"out_for_delivery", "cancelled", "admin"}:       "admin cancel (D-20)",
}

// reasonFor is a valid status_reason for a status (ck_orders_status_reason).
func reasonFor(status string) *string {
	r := map[string]string{"rejected": "too_busy", "expired": "not_accepted", "cancelled": "changed_mind", "delivery_failed": "customer_unreachable"}[status]
	if r == "" {
		return nil
	}
	return &r
}

// force puts an order in a status past the guard (seeding only) with the
// given deadlines.
func (o *orderShop) force(id, status string, acceptBy, hold time.Time) {
	o.t.Helper()
	ctx := context.Background()
	tx, err := o.a.tdb.OwnerPool.Begin(ctx)
	require.NoError(o.t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `ALTER TABLE orders DISABLE TRIGGER trg_orders_guard`)
	require.NoError(o.t, err)
	_, err = tx.Exec(ctx, `UPDATE orders SET status = $1, status_reason = $2, accept_by = $3, hold_until = $4,
	  partial_confirm_by = CASE WHEN $1 = 'awaiting_customer' THEN $4::timestamptz END,
	  accepted_at = CASE WHEN $1 IN ('placed','awaiting_customer') THEN NULL ELSE placed_at END WHERE id = $5`,
		status, reasonFor(status), acceptBy, hold, uuidOf(o.t, id))
	require.NoError(o.t, err)
	// An awaiting order's lines carry the quantities the shop took.
	_, err = tx.Exec(ctx, `UPDATE order_items SET quantity_accepted = CASE WHEN $1 = 'awaiting_customer' THEN quantity_requested END
	  WHERE order_id = $2`, status, uuidOf(o.t, id))
	require.NoError(o.t, err)
	_, err = tx.Exec(ctx, `ALTER TABLE orders ENABLE TRIGGER trg_orders_guard`)
	require.NoError(o.t, err)
	require.NoError(o.t, tx.Commit(ctx))
}

// TestOrderTransitionTable runs every action of every actor on an order in
// every status and checks the moves that happen are exactly 03 §4.1's (less
// the deferred ones), over all 11 × 11 × 4 (from, to, actor) triples. The
// admin cancel is deferred, so admin has no action yet.
func TestOrderTransitionTable(t *testing.T) {
	o := newOrderShop(t)
	type action struct {
		actor string
		run   func(id string)
	}
	verify := func(id string) {
		c := "0000"
		if s := o.status(id); s == "accepted" || s == "ready" {
			c = o.code(id)
		}
		o.verify(o.owner, id, c)
	}
	actions := map[string]action{
		"customer cancel":          {"customer", func(id string) { o.me(o.cust, id, "cancel", map[string]any{"reason": "changed_mind"}) }},
		"customer confirm-partial": {"customer", func(id string) { o.me(o.cust, id, "confirm-partial", map[string]any{"accept": true}) }},
		"customer extend":          {"customer", func(id string) { o.me(o.cust, id, "extend", nil) }},
		"customer new-code":        {"customer", func(id string) { o.me(o.cust, id, "new-code", nil) }},
		"staff accept":             {"staff", func(id string) { o.staff(o.owner, id, "accept", map[string]any{}) }},
		"staff partial accept": {"staff", func(id string) {
			o.staff(o.owner, id, "accept", map[string]any{"lines": []any{map[string]any{"orderItemId": o.lineIDs(id)[0], "quantity": 1}}})
		}},
		"staff reject":   {"staff", func(id string) { o.staff(o.owner, id, "reject", map[string]any{"reason": "too_busy"}) }},
		"staff ready":    {"staff", func(id string) { o.staff(o.owner, id, "ready", nil) }},
		"staff verify":   {"staff", verify},
		"staff cancel":   {"staff", func(id string) { o.staff(o.owner, id, "cancel", map[string]any{"reason": "out_of_stock"}) }},
		"staff dispatch": {"staff", func(id string) { o.staff(o.owner, id, "dispatch", nil) }},
		"staff fail-delivery": {"staff", func(id string) {
			o.staff(o.owner, id, "fail-delivery", map[string]any{"reason": "customer_unreachable"})
		}},
		"staff confirm-return": {"staff", func(id string) { o.staff(o.owner, id, "confirm-return", map[string]any{"outcome": "returned"}) }},
		"system accept timer":  {"system", func(id string) { o.timer(orders.KindAcceptTimeout, id, nil) }},
		"system no-show timer": {"system", func(id string) { o.timer(orders.KindNoShow, id, nil) }},
		"system partial timer": {"system", func(id string) { o.timer(orders.KindPartialTimeout, id, nil) }},
	}

	seen := map[move]string{} // the move → the action that made it
	for _, from := range orders.Statuses() {
		for name, act := range actions {
			id := o.orderOf(line{o.butter, 2}) // 2, so a partial accept can take 1
			now := o.a.clk.Now()
			acceptBy, hold := now.Add(10*time.Minute), now.Add(30*time.Minute)
			if act.actor == "system" {
				acceptBy, hold = now.Add(-time.Minute), now.Add(-time.Minute)
			}
			o.force(id, from, acceptBy, hold)
			act.run(id)
			if to := o.status(id); to != from {
				m := move{from, to, act.actor}
				seen[m] = name
			}
		}
	}

	actors := []string{"customer", "staff", "system", "admin"}
	cells := 0
	for _, from := range orders.Statuses() {
		for _, to := range orders.Statuses() {
			for _, actor := range actors {
				cells++
				m := move{from, to, actor}
				why, planned := planMoves[m]
				want := planned && why == ""
				name, got := seen[m]
				if want != got {
					t.Errorf("%s → %s by %s: want allowed=%v, got allowed=%v (%s)", from, to, actor, want, got, name)
				}
				if planned {
					require.True(t, orders.CanMove(from, to, orders.Pickup) || orders.CanMove(from, to, orders.Delivery),
						"the plan's %s → %s is in the machine", from, to)
				}
			}
		}
	}
	require.Equal(t, 11*11*4, cells)
}

// TestMachineMatchesTheGuard: orders.CanMove and trg_orders_guard agree on
// every (from, to) pair for both fulfilments.
func TestMachineMatchesTheGuard(t *testing.T) {
	o := newOrderShop(t)
	ctx := context.Background()
	pick := o.order()
	// A delivery order cannot be booked yet; insert one as placed.
	del := ids.New(o.a.clk.Now()).String()
	o.a.exec(`INSERT INTO orders (id, customer_id, store_id, fulfilment, subtotal_paise, items_after_offers_paise,
	  item_subtotal_paise, total_paise, code_enc, code_hmac, placed_at)
	  SELECT $1, customer_id, store_id, 'delivery', 100, 100, 100, 100, '\x00', decode(repeat('ab', 32), 'hex'), placed_at
	  FROM orders WHERE id = $2`, uuidOf(t, del), uuidOf(t, pick))

	for _, c := range []struct{ id, fulfilment string }{{pick, orders.Pickup}, {del, orders.Delivery}} {
		for _, from := range orders.Statuses() {
			for _, to := range orders.Statuses() {
				tx, err := o.a.tdb.OwnerPool.Begin(ctx)
				require.NoError(t, err)
				_, err = tx.Exec(ctx, `ALTER TABLE orders DISABLE TRIGGER trg_orders_guard`)
				require.NoError(t, err)
				_, err = tx.Exec(ctx, `UPDATE orders SET status = $1, status_reason = $2 WHERE id = $3`, from, reasonFor(from), uuidOf(t, c.id))
				require.NoError(t, err)
				_, err = tx.Exec(ctx, `ALTER TABLE orders ENABLE TRIGGER trg_orders_guard`)
				require.NoError(t, err)
				_, err = tx.Exec(ctx, `UPDATE orders SET status = $1, status_reason = $2 WHERE id = $3`, to, reasonFor(to), uuidOf(t, c.id))
				want := from == to || orders.CanMove(from, to, c.fulfilment)
				if want {
					require.NoError(t, err, "%s %s → %s", c.fulfilment, from, to)
				} else {
					require.ErrorContains(t, err, "invalid transition", "%s %s → %s", c.fulfilment, from, to)
				}
				require.NoError(t, tx.Rollback(ctx))
			}
		}
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/ids"
)

// `todayz rollup demo` is the synthetic traffic behind `make demo` (W3-07,
// 09 §36, 05 §333): it drives the local API over loopback as one seeded
// customer and the seeded shop owner, so the dashboards, Pulse and the
// business app show activity. Every tick it searches, sends a RUM batch and
// app events, and books one pickup order at the seeded store, which the shop
// accepts, marks ready and hands over with the customer's code (most ticks)
// or the customer cancels. The worker running under `make dev` runs the
// timers and the rollup jobs.
//
// It signs in through the mocks' latest OTP (as e2e/fixtures/principals.ts
// does) and prints only step names, statuses and problem codes: never a
// body, an OTP, a pickup code, a token or a phone. It refuses any API or
// mocks URL that is not loopback or *.todayz.localhost, and its client only
// reaches loopback.
//
// TODO(lead): store rejects and accept-timer expiries are left out: they put
// strikes on the seeded store (01 §8), which would pause it for the business
// app and the journeys. The orders use the QA packs and Maggi, the seeded
// store's deepest stock; `make reset` puts the stock and the customer's
// wallet back.

// The seeded people and store (internal/seed: identity.go, business.go,
// catalogue.go). Phones are reserved test numbers and are never printed.
const (
	demoCustomerPhone = "+919000000002" // customer.ravi
	demoOwnerPhone    = "+919000000011" // staff.owner
	demoBusinessID    = "01KE0S1W6W8ZV7T1C3A6MB0001"
	demoStoreID       = "01KE0S1W6W8ZV7T1C3A6MS0001" // SL, Sri Lakshmi Indiranagar
	demoTurnstile     = "XXXX.DUMMY.TOKEN.XXXX"      // the mocks' always-pass token
)

// SL's store items with the most stock: Maggi (I2) and the QA packs I9,
// I10, I11 and I13 (seqID(storeItemIDPrefix, 100+n)); I12 (₹0.50) is left
// out.
var demoItems = []string{
	"01KE0S1W6W8ZV7T1C3A6MQ0036", "01KE0S1W6W8ZV7T1C3A6MQ003D", "01KE0S1W6W8ZV7T1C3A6MQ003E",
	"01KE0S1W6W8ZV7T1C3A6MQ003F", "01KE0S1W6W8ZV7T1C3A6MQ003H",
}

// demoQueries mix hits, a near miss and a query nothing stocks, so the
// zero-result panels move too. The query text goes only to the API.
var demoQueries = []string{"maggi", "qa pack", "butter", "milk", "atta", "noodles", "dolo", "curd", "kombucha", "saffron honey"}

var (
	demoRadii  = []int{1000, 2000, 5000, 10000}
	demoRoutes = []string{"/", "/search", "/s/$slug", "/orders/$orderId", "/wallet"}
	demoOrigin = map[string]string{"customer": "https://app.todayz.localhost", "business": "https://business.todayz.localhost"}
)

type demoConfig struct {
	API, Mocks string
	// CustomerPhone and OwnerPhone sign in; never printed.
	CustomerPhone, OwnerPhone string
	BusinessID                string
	StoreID                   string
	Items                     []string
	Lat, Lng                  float64
	Every                     time.Duration
	For                       time.Duration // 0: until the context ends
	CancelPct                 int           // share of orders the customer cancels instead of collecting
}

// demoOTP returns the OTP sent to phone after since.
type demoOTP func(ctx context.Context, phone string, since time.Time) (string, error)

type demoSession struct {
	app    string
	phone  string
	token  string
	cookie *http.Cookie
}

// demoDoer sends the demo's requests: the egress-guarded client in the
// binary, the in-process router in tests.
type demoDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type demoRun struct {
	cfg    demoConfig
	hc     demoDoer
	clk    clock.Clock
	rnd    *rand.Rand
	otp    demoOTP
	out    io.Writer
	cust   *demoSession
	owner  *demoSession
	counts map[string]int
}

func runRollupDemo(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	fs := flag.NewFlagSet("rollup demo", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := demoConfig{CustomerPhone: demoCustomerPhone, OwnerPhone: demoOwnerPhone, BusinessID: demoBusinessID, StoreID: demoStoreID, Items: demoItems, Lat: 12.9719, Lng: 77.6412}
	fs.StringVar(&cfg.API, "api", "http://127.0.0.1:8180", "the local API")
	fs.StringVar(&cfg.Mocks, "mocks", "http://127.0.0.1:4010", "the local mocks server")
	fs.DurationVar(&cfg.Every, "every", 10*time.Second, "time between ticks")
	fs.DurationVar(&cfg.For, "for", 0, "stop after this long (default: until Ctrl-C)")
	fs.IntVar(&cfg.CancelPct, "cancel-pct", 15, "percent of orders the customer cancels")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return usageError{msg: "rollup demo takes [--api URL] [--mocks URL] [--every 10s] [--for 10m] [--cancel-pct 15]"}
	}
	if err := cfg.check(); err != nil {
		return usageError{msg: err.Error()}
	}
	switch getenv("APP_ENV") {
	case "local", "test":
	default:
		return errors.New("rollup demo runs only with APP_ENV local or test")
	}
	hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true, Timeout: 30 * time.Second})
	d := newDemoRun(cfg, hc, clock.Real{}, mocksOTP(hc, cfg.Mocks), out)
	return d.run(ctx)
}

// check refuses anything but a local API: the generator writes orders.
func (c demoConfig) check() error {
	for name, raw := range map[string]string{"--api": c.API, "--mocks": c.Mocks} {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("%s must be an http(s) origin", name)
		}
		h := u.Hostname()
		if h != "127.0.0.1" && h != "localhost" && h != "::1" && !strings.HasSuffix(h, ".todayz.localhost") {
			return fmt.Errorf("%s must be loopback or *.todayz.localhost", name)
		}
	}
	if c.Every < time.Second {
		return errors.New("--every is at least 1s")
	}
	if c.CancelPct < 0 || c.CancelPct > 100 {
		return errors.New("--cancel-pct is 0–100")
	}
	return nil
}

func newDemoRun(cfg demoConfig, hc demoDoer, clk clock.Clock, otp demoOTP, out io.Writer) *demoRun {
	cfg.API = strings.TrimSuffix(cfg.API, "/")
	seed := uint64(clk.Now().UnixNano()) // #nosec G115 -- a demo's random seed
	return &demoRun{
		cfg: cfg, hc: hc, clk: clk, otp: otp, out: out,
		rnd:    rand.New(rand.NewPCG(seed, seed>>1)), // #nosec G404 -- synthetic traffic, not security
		counts: map[string]int{},
	}
}

func (d *demoRun) run(ctx context.Context) error {
	var err error
	if d.cust, err = d.signIn(ctx, d.cfg.CustomerPhone, "customer"); err != nil {
		return fmt.Errorf("customer sign-in: %w", err)
	}
	if d.owner, err = d.signIn(ctx, d.cfg.OwnerPhone, "business"); err != nil {
		return fmt.Errorf("owner sign-in: %w", err)
	}
	_, _ = fmt.Fprintf(d.out, "demo: signed in; a tick every %s against %s\n", d.cfg.Every, d.cfg.API)
	var stop <-chan time.Time
	if d.cfg.For > 0 {
		stop = time.After(d.cfg.For)
	}
	tick := time.NewTicker(d.cfg.Every)
	defer tick.Stop()
	for n := 1; ; n++ {
		if err := d.tick(ctx); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(d.out, "demo: tick %d: %s\n", n, d.summary())
		select {
		case <-ctx.Done():
			return nil
		case <-stop:
			return nil
		case <-tick.C:
		}
	}
}

// tick is one round of traffic. Only a transport error (the API is down)
// stops the run; a refused step is counted by its problem code.
func (d *demoRun) tick(ctx context.Context) error {
	for range 1 + d.rnd.IntN(3) {
		if err := d.search(ctx); err != nil {
			return err
		}
	}
	if err := d.rum(ctx); err != nil {
		return err
	}
	if err := d.events(ctx); err != nil {
		return err
	}
	return d.order(ctx)
}

func (d *demoRun) search(ctx context.Context) error {
	q := url.Values{}
	q.Set("q", demoQueries[d.rnd.IntN(len(demoQueries))])
	q.Set("lat", fmt.Sprintf("%.4f", d.cfg.Lat+(d.rnd.Float64()-0.5)/100))
	q.Set("lng", fmt.Sprintf("%.4f", d.cfg.Lng+(d.rnd.Float64()-0.5)/100))
	q.Set("radius", fmt.Sprint(demoRadii[d.rnd.IntN(len(demoRadii))]))
	r, err := d.call(ctx, nil, http.MethodGet, "/v1/discover/search?"+q.Encode(), nil, false)
	if err != nil {
		return err
	}
	d.count("search", r)
	return nil
}

func (d *demoRun) rum(ctx context.Context) error {
	now := d.clk.Now().UnixMilli()
	route := demoRoutes[d.rnd.IntN(len(demoRoutes))]
	samples := []map[string]any{
		{"type": "vital", "name": "LCP", "value": 900 + d.rnd.Float64()*2600, "route": route, "t": now},
		{"type": "vital", "name": "INP", "value": 40 + d.rnd.Float64()*400, "route": route, "t": now},
		{"type": "vital", "name": "CLS", "value": d.rnd.Float64() * 0.2, "route": route, "t": now},
	}
	if d.rnd.IntN(20) == 0 {
		samples = append(samples, map[string]any{"type": "error", "name": "TypeError", "route": route, "t": now,
			"detail": map[string]any{"message": "demo: synthetic error"}})
	}
	r, err := d.call(ctx, nil, http.MethodPost, "/v1/rum", map[string]any{
		"app": "customer", "release": "dev", "platform": "web", "conn": "4g", "samples": samples,
	}, false)
	if err != nil {
		return err
	}
	d.count("rum", r)
	return nil
}

func (d *demoRun) events(ctx context.Context) error {
	now := d.clk.Now()
	ts := now.UTC().Format(time.RFC3339Nano)
	launch := "warm"
	if d.rnd.IntN(3) == 0 {
		launch = "cold"
	}
	r, err := d.call(ctx, d.cust, http.MethodPost, "/v1/events", map[string]any{
		"app": "customer", "platform": "web", "sessionId": ids.New(now).String(),
		"events": []map[string]any{
			{"id": ids.New(now).String(), "name": "session_started", "t": ts},
			{"id": ids.New(now).String(), "name": "app_opened", "t": ts, "props": map[string]any{"launch": launch}},
		},
	}, false)
	if err != nil {
		return err
	}
	d.count("events", r)
	return nil
}

// order books one pickup order and then collects or cancels it, so the
// customer never holds more than one active order.
func (d *demoRun) order(ctx context.Context) error {
	items := []map[string]any{}
	for _, i := range d.rnd.Perm(len(d.cfg.Items))[:1+d.rnd.IntN(2)] {
		items = append(items, map[string]any{"storeItemId": d.cfg.Items[i], "quantity": 1 + d.rnd.IntN(2)})
	}
	cart := map[string]any{"storeId": d.cfg.StoreID, "fulfilment": "pickup", "items": items, "creditsToUse": 0}
	q, err := d.call(ctx, d.cust, http.MethodPost, "/v1/me/checkout/quote", cart, false)
	if err != nil {
		return err
	}
	if q.status != http.StatusOK || q.body["orderable"] != true {
		d.count("quote", q)
		return nil
	}
	cart["expectedTotalPaise"] = q.body["totalPaise"]
	p, err := d.call(ctx, d.cust, http.MethodPost, "/v1/me/orders", cart, true)
	if err != nil {
		return err
	}
	d.count("place", p)
	id, _ := p.body["id"].(string)
	if p.status != http.StatusCreated || id == "" {
		return nil
	}
	if d.rnd.IntN(100) < d.cfg.CancelPct {
		r, err := d.call(ctx, d.cust, http.MethodPost, "/v1/me/orders/"+id+"/cancel", map[string]any{"reason": "changed_mind"}, true)
		if err != nil {
			return err
		}
		d.count("cancel", r)
		return nil
	}
	store := "/v1/biz/businesses/" + d.cfg.BusinessID + "/stores/" + d.cfg.StoreID + "/orders/" + id
	for _, step := range []string{"accept", "ready"} {
		r, err := d.call(ctx, d.owner, http.MethodPost, store+"/"+step, map[string]any{}, true)
		if err != nil {
			return err
		}
		d.count(step, r)
		if r.status != http.StatusOK {
			return nil
		}
	}
	g, err := d.call(ctx, d.cust, http.MethodGet, "/v1/me/orders/"+id, nil, false)
	if err != nil {
		return err
	}
	code, _ := g.body["code"].(string) // never printed
	if g.status != http.StatusOK || code == "" {
		d.count("code", g)
		return nil
	}
	pay := "cash"
	if d.rnd.IntN(2) == 0 {
		pay = "upi"
	}
	r, err := d.call(ctx, d.owner, http.MethodPost, store+"/verify-code", map[string]any{"code": code, "paymentMethod": pay}, true)
	if err != nil {
		return err
	}
	d.count("collect", r)
	return nil
}

// signIn is OTP send → the OTP from the mocks → verify, keeping the access
// token and the refresh cookie.
func (d *demoRun) signIn(ctx context.Context, phone, app string) (*demoSession, error) {
	since := d.clk.Now().Add(-time.Second)
	r, err := d.call(ctx, nil, http.MethodPost, "/v1/auth/otp/send", map[string]any{
		"phone": phone, "purpose": "login", "app": app, "turnstileToken": demoTurnstile,
	}, false)
	if err != nil {
		return nil, err
	}
	reqID, _ := r.body["requestId"].(string)
	if r.status != http.StatusOK || reqID == "" {
		return nil, fmt.Errorf("otp send answered %d %s", r.status, r.code())
	}
	code, err := d.otp(ctx, phone, since)
	if err != nil {
		return nil, err
	}
	r, err = d.call(ctx, nil, http.MethodPost, "/v1/auth/otp/verify", map[string]any{
		"requestId": reqID, "code": code, "app": app, "deviceId": ids.New(d.clk.Now()).String(),
	}, false)
	if err != nil {
		return nil, err
	}
	tok, _ := r.body["accessToken"].(string)
	if r.status != http.StatusOK || tok == "" {
		return nil, fmt.Errorf("otp verify answered %d %s", r.status, r.code())
	}
	s := &demoSession{app: app, phone: phone, token: tok}
	for _, c := range r.cookies {
		if strings.HasPrefix(c.Name, "__Secure-tz_rt_") {
			s.cookie = c
		}
	}
	return s, nil
}

// refresh rotates the session's refresh cookie for a new access token, as
// the app does (Origin and X-TZ-Client are its CSRF guard).
func (d *demoRun) refresh(ctx context.Context, s *demoSession) error {
	if s.cookie == nil {
		return errors.New("no refresh cookie")
	}
	b, _ := json.Marshal(map[string]any{"app": s.app})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.cfg.API+"/v1/auth/refresh", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", demoOrigin[s.app])
	req.Header.Set("X-TZ-Client", "todayz-demo/1.0")
	req.AddCookie(s.cookie)
	r, err := d.do(req)
	if err != nil {
		return err
	}
	tok, _ := r.body["accessToken"].(string)
	if r.status != http.StatusOK || tok == "" {
		return fmt.Errorf("refresh answered %d %s", r.status, r.code())
	}
	s.token = tok
	for _, c := range r.cookies {
		if c.Name == s.cookie.Name {
			s.cookie = c
		}
	}
	return nil
}

type demoReply struct {
	status  int
	body    map[string]any
	cookies []*http.Cookie
}

// code is the problem code of a refused call ("" on success).
func (r demoReply) code() string {
	if c, ok := r.body["code"].(string); ok {
		return c
	}
	return ""
}

// call sends one JSON request as s (nil: anonymous); an unsafe call gets a
// fresh Idempotency-Key. A 401 refreshes the session once and retries.
func (d *demoRun) call(ctx context.Context, s *demoSession, method, path string, body any, idem bool) (demoReply, error) {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return demoReply{}, err
		}
	}
	key := ""
	if idem {
		key = ids.New(d.clk.Now()).String()
	}
	for attempt := 0; ; attempt++ {
		var rd io.Reader
		if raw != nil {
			rd = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, d.cfg.API+path, rd)
		if err != nil {
			return demoReply{}, err
		}
		if raw != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if s != nil {
			req.Header.Set("Authorization", "Bearer "+s.token)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		r, err := d.do(req)
		if err != nil || r.status != http.StatusUnauthorized || s == nil || attempt > 0 {
			return r, err
		}
		if err := d.refresh(ctx, s); err != nil {
			return demoReply{}, fmt.Errorf("session renew: %w", err)
		}
	}
}

func (d *demoRun) do(req *http.Request) (demoReply, error) {
	res, err := d.hc.Do(req)
	if err != nil {
		return demoReply{}, err
	}
	defer res.Body.Close()
	out := demoReply{status: res.StatusCode, cookies: res.Cookies()}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return demoReply{}, err
	}
	if len(b) > 0 && strings.Contains(res.Header.Get("Content-Type"), "json") {
		_ = json.Unmarshal(b, &out.body) // a non-object body leaves body nil
	}
	return out, nil
}

// count tallies a step as "step" on success or "step:<status> <code>".
func (d *demoRun) count(step string, r demoReply) {
	if r.status >= 200 && r.status < 300 {
		d.counts[step]++
		return
	}
	d.counts[fmt.Sprintf("%s:%d %s", step, r.status, r.code())]++
}

func (d *demoRun) summary() string {
	keys := make([]string, 0, len(d.counts))
	for k := range d.counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", strings.TrimSpace(k), d.counts[k]))
	}
	return strings.Join(parts, " ")
}

// mocksOTP reads the latest OTP the mocks' SMS sink holds for phone
// (GET /_mock/sms/latest-otp, which waits up to 8 s for it).
func mocksOTP(hc *http.Client, base string) demoOTP {
	base = strings.TrimSuffix(base, "/")
	return func(ctx context.Context, phone string, since time.Time) (string, error) {
		q := url.Values{"to": {phone}, "since": {since.UTC().Format(time.RFC3339Nano)}, "wait": {"8"}}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/_mock/sms/latest-otp?"+q.Encode(), nil)
		if err != nil {
			return "", err
		}
		res, err := hc.Do(req)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		var b struct {
			OTP string `json:"otp"`
		}
		if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&b) != nil || b.OTP == "" {
			return "", fmt.Errorf("mocks latest-otp answered %d", res.StatusCode)
		}
		return b.OTP, nil
	}
}

package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/testclock"
	"github.com/todayz/backend/internal/testkit"
)

// W3-06 field agents (A-04, A-15, SC-FA-01…10).

const (
	agentPhone      = "+919000000061"
	agent2Phone     = "+919000000062"
	fieldAdminPhone = "+919000000063"
	shopOwnerPhone  = "+919000000064"
	shop2OwnerPhone = "+919000000065"
	staplesCategory = "0190c47e-0000-7000-8000-000000000101"
)

type fieldAPI struct {
	*bizAPI
	settings *settings.Cache
	category string // a top-level business category
	staples  string // a product category, as a ULID
}

func newFieldAPI(t *testing.T) *fieldAPI {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	e := localEnv(tdb)
	// Presigned uploads sign locally; nothing is sent to this endpoint.
	e["R2_ENDPOINT"] = "http://127.0.0.1:9100"
	e["R2_ACCESS_KEY_ID"] = "test-access"
	e["R2_SECRET_ACCESS_KEY"] = "test-secret-value"
	e["R2_BUCKET_PRIVATE"] = "todayz-private"
	cfg, err := config.Load(env(e), nil)
	require.NoError(t, err)
	clk := clock.NewFake(time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)) // Friday 11:30 IST
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{Log: log})
	d := deps{
		Config: cfg, Log: log, Clock: clk, Settings: st,
		DB: db.New(tdb.Pool), Ready: &health.Readiness{},
		TestClock: testclock.New(clock.NewOffset(nil), nopStore{}, log),
	}
	h, err := buildRouter(d, idempotency.NewMemoryStore(), contractValidator(t))
	require.NoError(t, err)
	f := &fieldAPI{bizAPI: &bizAPI{t: t, h: h, clk: clk, tdb: tdb}, settings: st}
	var category pgtype.UUID
	require.NoError(t, tdb.OwnerPool.QueryRow(context.Background(), `SELECT id FROM categories WHERE parent_id IS NULL ORDER BY sort, slug LIMIT 1`).Scan(&category))
	cid, _ := ids.FromUUID(category)
	f.category = cid.String()
	var staples pgtype.UUID
	require.NoError(t, staples.Scan(staplesCategory))
	sid, _ := ids.FromUUID(staples)
	f.staples = sid.String()
	return f
}

// fieldUser is an admin console user with roles, signed in with otp+totp.
type fieldUser struct {
	a      *fieldAPI
	phone  string
	id     string
	secret []byte
}

func (f *fieldAPI) user(phone string, roles ...string) *fieldUser {
	f.t.Helper()
	cust := f.signIn(phone, "customer")
	me := f.call(cust, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(f.t, http.StatusOK, me.code, me.raw)
	id := me.body["id"].(string)
	f.exec(`INSERT INTO admin_users (user_id, roles) VALUES ($1, $2)`, uuidOf(f.t, id), roles)
	return &fieldUser{a: f, phone: phone, id: id}
}

// token signs in afresh at the clock's time (access tokens last 15 min).
func (u *fieldUser) token() string {
	a, t := u.a, u.a.t
	t.Helper()
	w := a.call("", http.MethodPost, "/v1/auth/otp/send", map[string]string{
		"phone": u.phone, "app": "admin", "purpose": "login", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
	}, nil)
	require.Equal(t, http.StatusOK, w.code, "otp send: %d", w.code)
	w = a.call("", http.MethodPost, "/v1/auth/otp/verify", map[string]string{
		"app": "admin", "requestId": w.body["requestId"].(string), "code": testOTP, "deviceId": ids.New(a.clk.Now()).String(),
	}, map[string]string{"User-Agent": "field-test"})
	require.Equal(t, http.StatusOK, w.code, "otp verify: %d", w.code)
	tok := w.body["accessToken"].(string)
	if u.secret == nil {
		e := a.call(tok, http.MethodPost, "/v1/auth/totp/enrol", nil, nil)
		require.Equal(t, http.StatusOK, e.code, "enrol: %d", e.code)
		s, err := identity.DecodeTOTPSecret(e.body["secret"].(string))
		require.NoError(t, err)
		u.secret = s
	}
	a.clk.Advance(31 * time.Second) // a fresh TOTP step
	v := a.call(tok, http.MethodPost, "/v1/auth/totp/verify", map[string]string{"code": identity.TOTPCode(u.secret, a.clk.Now()), "purpose": "login"}, nil)
	require.Equal(t, http.StatusOK, v.code, "totp verify: %d", v.code)
	return v.body["accessToken"].(string)
}

var fieldHours = func() map[string]any {
	w := map[string]any{}
	for _, d := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
		w[d] = []any{map[string]string{"open": "09:00", "close": "21:00"}}
	}
	return map[string]any{"weekly": w}
}()

// onboard makes a business ready to confirm as tok: a category and a full
// store. It returns the business and store ids.
func (f *fieldAPI) onboard(tok, brand, owner string) (bid, sid string) {
	f.t.Helper()
	r := f.call(tok, http.MethodPost, "/v1/admin/field/businesses", map[string]any{
		"brandName": brand, "categoryId": f.category, "ownerPhone": owner,
	}, nil)
	require.Equal(f.t, http.StatusCreated, r.code, r.raw)
	bid = r.body["id"].(string)
	r = f.call(tok, http.MethodPost, "/v1/admin/field/businesses/"+bid+"/stores", map[string]any{
		"name": brand, "addressLine1": "12, 9th Main", "locality": "Jayanagar", "city": "Bengaluru", "pincode": "560041",
		"location": map[string]float64{"lat": 12.925, "lng": 77.583}, "phone": "+919000000066", "hours": fieldHours,
	}, nil)
	require.Equal(f.t, http.StatusCreated, r.code, r.raw)
	require.True(f.t, r.body["hasHours"].(bool))
	return bid, r.body["id"].(string)
}

func (f *fieldAPI) product(name string) string {
	id := ids.New(f.clk.Now()).String()
	f.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, mrp_paise) VALUES ($1, $2, '`+staplesCategory+`', 1, 'kg', 5000)`,
		uuidOf(f.t, id), name)
	return id
}

// SC-FA-01…05, 08: the wizard's calls, the owner's code, attribution.
func TestFieldOnboardingHTTP(t *testing.T) {
	f := newFieldAPI(t)
	agent := f.user(agentPhone, "field_agent")
	tok := agent.token()
	F := "/v1/admin/field/businesses"

	// The owner number may not be the agent's own.
	r := f.call(tok, http.MethodPost, F, map[string]any{"brandName": "Mine", "ownerPhone": agentPhone}, nil)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.ownerPhone")

	// Not ready: the code is not sent.
	r = f.call(tok, http.MethodPost, F, map[string]any{"brandName": "Ravi Medicals", "ownerPhone": shopOwnerPhone}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, agent.id, r.body["onboardedBy"])
	require.Equal(t, "draft", r.body["status"])
	require.Equal(t, false, r.body["ownerConfirmed"])
	require.Equal(t, pii.MaskPhone(shopOwnerPhone), r.body["ownerPhoneMasked"])
	require.NotContains(t, r.raw, shopOwnerPhone[3:])
	early := r.body["id"].(string)
	r = f.call(tok, http.MethodPost, F+"/"+early+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "submit_incomplete", problemCode(r))
	require.Contains(t, fieldsOf(r), "categoryId")

	bid, sid := f.onboard(tok, "Lakshmi Kirana", shopOwnerPhone)
	B := F + "/" + bid

	// Photos: shop_photo and storefront only, no-store, at most 5 per kind.
	photo := func(kind string) resp {
		return f.call(tok, http.MethodPost, B+"/documents", map[string]any{"kind": kind, "contentType": "image/jpeg", "sizeBytes": 2048, "storeId": sid}, nil)
	}
	r = photo("pan")
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	for range 5 {
		r = photo("storefront")
		require.Equal(t, http.StatusCreated, r.code, r.raw)
		require.Equal(t, "no-store", r.hdr.Get("Cache-Control"))
		require.Equal(t, http.MethodPut, r.body["upload"].(map[string]any)["method"])
	}
	r = photo("storefront")
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	r = photo("shop_photo")
	require.Equal(t, http.StatusCreated, r.code, r.raw)

	// Items one by one: a master product and a new one (pending review, D-64).
	S := B + "/stores/" + sid
	atta := f.product("Aashirvaad Atta")
	r = f.call(tok, http.MethodGet, S+"/products?q=aashirvaad", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.NotEmpty(t, r.body["items"])
	r = f.call(tok, http.MethodPost, S+"/items", map[string]any{"productId": atta, "pricePaise": 4800, "openingStock": 20}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, true, r.body["isListed"])
	require.EqualValues(t, 20, r.body["onHand"])
	r = f.call(tok, http.MethodPost, S+"/items", map[string]any{"productId": atta, "pricePaise": 4800}, nil)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	r = f.call(tok, http.MethodPost, S+"/items", map[string]any{"pricePaise": 1200, "newProduct": map[string]any{
		"name": "Ravi Special Masala", "categoryId": f.staples, "packSize": 100, "unit": "g",
	}}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, "pending_review", r.body["productStatus"])
	item := r.body["id"].(string)
	r = f.call(tok, http.MethodPatch, S+"/items/"+item, map[string]any{"pricePaise": 1100}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 1100, r.body["pricePaise"])
	require.Equal(t, 2, f.count(`SELECT count(*) FROM field_item_adds WHERE agent_id = $1`, uuidOf(t, agent.id)))

	// The owner's code: wrong, then right (the reserved test number takes 000000 locally).
	r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "123456"}, nil)
	require.Equal(t, http.StatusUnauthorized, r.code, r.raw)
	require.Equal(t, "otp_expired", problemCode(r), "no code sent yet")
	r = f.call(tok, http.MethodPost, B+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, pii.MaskPhone(shopOwnerPhone), r.body["phoneMasked"])
	require.EqualValues(t, 30, r.body["resendAfterSeconds"])
	require.NotContains(t, r.raw, "code\"")
	r = f.call(tok, http.MethodPost, B+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusTooManyRequests, r.code, r.raw)
	require.NotEmpty(t, r.hdr.Get("Retry-After"))
	r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "123456"}, nil)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Equal(t, "code_invalid", problemCode(r))
	require.EqualValues(t, 4, ext(r, "attemptsLeft"))
	r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "submitted", r.body["status"])
	require.Equal(t, true, r.body["ownerConfirmed"])
	require.Equal(t, 1, f.count(`SELECT count(*) FROM staff s JOIN phone_identities p ON p.user_id = s.user_id
	  WHERE s.business_id = $1 AND s.role = 'owner' AND s.status = 'active'`, uuidOf(t, bid)))
	r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil)
	require.Equal(t, http.StatusConflict, r.code, r.raw, "submitted: no second confirm")

	// The owner signs in to the business app and sees it.
	owner := f.signIn(shopOwnerPhone, "business")
	r = f.call(owner, http.MethodGet, "/v1/biz/businesses", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Contains(t, r.raw, bid)

	// Audit: by the agent as an admin actor, never the number or a code.
	require.Positive(t, f.count(`SELECT count(*) FROM audit_log WHERE action = 'business.submit' AND subject_id = $1 AND actor_kind = 'admin'`, bid))
	require.Zero(t, f.count(`SELECT count(*) FROM audit_log WHERE after::text LIKE '%' || $1 || '%'`, shopOwnerPhone[3:]))

	// The agent's list shows their own.
	r = f.call(tok, http.MethodGet, F, nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 2)
}

// SC-FA-06: five wrong codes lock it; a lapsed code is expired; a new owner
// number ends the old code.
func TestFieldOwnerCodeLimitsHTTP(t *testing.T) {
	f := newFieldAPI(t)
	agent := f.user(agentPhone, "field_agent")
	tok := agent.token()
	bid, _ := f.onboard(tok, "Ganesh Stores", shopOwnerPhone)
	B := "/v1/admin/field/businesses/" + bid
	r := f.call(tok, http.MethodPost, B+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	for i := range 5 {
		r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "111111"}, nil)
		require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
		require.EqualValues(t, 4-i, ext(r, "attemptsLeft"))
	}
	r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil)
	require.Equal(t, http.StatusTooManyRequests, r.code, r.raw)
	require.Equal(t, "otp_locked", problemCode(r))

	// A new code after the gap; it lapses after 5 minutes.
	f.clk.Advance(31 * time.Second)
	r = f.call(tok, http.MethodPost, B+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	f.clk.Advance(5*time.Minute + time.Second)
	tok = agent.token()
	r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil)
	require.Equal(t, http.StatusUnauthorized, r.code, r.raw)
	require.Equal(t, "otp_expired", problemCode(r))

	// A code sent, then the number changed: the code no longer confirms.
	r = f.call(tok, http.MethodPost, B+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = f.call(tok, http.MethodPatch, B, map[string]any{"ownerPhone": shop2OwnerPhone}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, pii.MaskPhone(shop2OwnerPhone), r.body["ownerPhoneMasked"])
	r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil)
	require.Equal(t, http.StatusUnauthorized, r.code, r.raw)
	require.Equal(t, "otp_expired", problemCode(r))
}

// SC-FA-07, D-39: another agent's business is 403; items change only within
// field_agent_edit_window_days of verification (inclusive), and the setting
// moves the window; the list stays readable; an admin is not windowed.
func TestFieldEditWindowHTTP(t *testing.T) {
	f := newFieldAPI(t)
	agent := f.user(agentPhone, "field_agent")
	other := f.user(agent2Phone, "field_agent")
	admin := f.user(fieldAdminPhone, "admin")
	tok := agent.token()
	bid, sid := f.onboard(tok, "Sri Sai Stores", shopOwnerPhone)
	B := "/v1/admin/field/businesses/" + bid
	S := B + "/stores/" + sid
	p1, p2, p3, p4 := f.product("Tata Salt"), f.product("Amul Butter"), f.product("Fortune Oil"), f.product("Parle G")

	// Another agent: 403 everywhere, and their list does not show it.
	otok := other.token()
	for _, c := range []struct{ method, path string }{{http.MethodGet, B}, {http.MethodGet, S + "/items"}} {
		r := f.call(otok, c.method, c.path, nil, nil)
		require.Equal(t, http.StatusForbidden, r.code, "%s %s: %s", c.method, c.path, r.raw)
	}
	r := f.call(otok, http.MethodPost, S+"/items", map[string]any{"productId": p1, "pricePaise": 100}, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = f.call(otok, http.MethodGet, "/v1/admin/field/businesses", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Empty(t, r.body["items"])
	r = f.call(otok, http.MethodGet, "/v1/admin/field/businesses?agentId="+agent.id, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)

	verified := f.clk.Now()
	f.exec(`UPDATE businesses SET status = 'verified', verified_at = $1 WHERE id = $2`, verified, uuidOf(t, bid))

	// 29 days 23 hours: allowed.
	f.clk.Set(verified.Add(29*24*time.Hour + 23*time.Hour))
	tok = agent.token()
	r = f.call(tok, http.MethodPost, S+"/items", map[string]any{"productId": p1, "pricePaise": 2500}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	r = f.call(tok, http.MethodGet, B, nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, true, r.body["itemsEditable"])
	require.NotNil(t, r.body["editableUntil"])

	// 30 days and a second: refused; the list still reads.
	f.clk.Set(verified.Add(30*24*time.Hour + time.Second))
	tok = agent.token()
	r = f.call(tok, http.MethodPost, S+"/items", map[string]any{"productId": p2, "pricePaise": 4500}, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = f.call(tok, http.MethodPost, B+"/stores", map[string]any{"name": "Branch 2"}, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = f.call(tok, http.MethodGet, S+"/items", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, false, r.body["itemsEditable"])
	require.Len(t, r.body["items"], 1)

	// A longer window (the setting, not a constant) lets the agent back in.
	f.exec(`UPDATE platform_settings SET value = '45' WHERE key = 'field_agent_edit_window_days'`)
	f.settings.Invalidate()
	r = f.call(tok, http.MethodPost, S+"/items", map[string]any{"productId": p2, "pricePaise": 4500}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	f.exec(`UPDATE platform_settings SET value = '30' WHERE key = 'field_agent_edit_window_days'`)
	f.settings.Invalidate()
	r = f.call(tok, http.MethodPost, S+"/items", map[string]any{"productId": p3, "pricePaise": 4900}, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)

	// An admin is not windowed.
	atok := admin.token()
	r = f.call(atok, http.MethodPost, S+"/items", map[string]any{"productId": p4, "pricePaise": 1000}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	r = f.call(atok, http.MethodGet, "/v1/admin/field/businesses?agentId="+agent.id, nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)

	// A business the shop made itself is not on the field routes.
	owner := f.signIn(shop2OwnerPhone, "business")
	own, _ := f.newBusiness(owner, "Own Shop")
	r = f.call(atok, http.MethodGet, "/v1/admin/field/businesses/"+own, nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
}

// SC-FA-09, 10 (A-15): an agent's own numbers; only admins list every agent.
func TestFieldStatsHTTP(t *testing.T) {
	f := newFieldAPI(t)
	agent := f.user(agentPhone, "field_agent")
	admin := f.user(fieldAdminPhone, "admin")
	tok := agent.token()
	bid, sid := f.onboard(tok, "Kaveri Stores", shopOwnerPhone)
	f.onboard(tok, "Tunga Stores", shop2OwnerPhone)
	r := f.call(tok, http.MethodPost, "/v1/admin/field/businesses/"+bid+"/stores/"+sid+"/items",
		map[string]any{"productId": f.product("Tata Salt"), "pricePaise": 2500}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	f.exec(`UPDATE businesses SET status = 'verified', verified_at = $1 WHERE id = $2`, f.clk.Now(), uuidOf(t, bid))
	f.exec(`UPDATE stores SET status = 'live', live_at = $1 WHERE id = $2`, f.clk.Now(), uuidOf(t, sid))

	r = f.call(tok, http.MethodGet, "/v1/admin/field/me/stats", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	st := r.body["stats"].(map[string]any)
	require.Equal(t, agent.id, st["agentId"])
	b := st["businesses"].(map[string]any)
	require.EqualValues(t, 2, b["total"])
	require.EqualValues(t, 1, b["verified"])
	require.EqualValues(t, 1, b["draft"])
	require.EqualValues(t, 1, st["storesLive"])
	require.EqualValues(t, 1, st["itemsAdded"])
	require.EqualValues(t, 2, st["inWindow"])
	require.EqualValues(t, 0, st["firstOrderRate"])
	weekly := st["weekly"].([]any)
	require.Len(t, weekly, 8)
	last := weekly[7].(map[string]any)
	require.Equal(t, "2026-10-19", last["weekStart"], "Monday of the IST week")
	require.EqualValues(t, 2, last["onboarded"])
	require.EqualValues(t, 1, last["itemsAdded"])

	r = f.call(tok, http.MethodGet, "/v1/admin/field-agents", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = f.call(admin.token(), http.MethodGet, "/v1/admin/field-agents", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	items := r.body["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, agent.id, items[0].(map[string]any)["agentId"])
}

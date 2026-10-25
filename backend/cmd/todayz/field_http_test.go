package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
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
	bucket   *supportBucket // the private bucket the confirm step HEADs
	category string         // a top-level business category
	staples  string         // a product category, as a ULID
}

func newFieldAPI(t *testing.T) *fieldAPI {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	e := localEnv(tdb)
	// Presigned uploads sign locally; the confirm step HEADs the fake bucket.
	bucket := newSupportBucket(t)
	e["R2_ENDPOINT"] = bucket.srv.URL
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
	f := &fieldAPI{bizAPI: &bizAPI{t: t, h: h, clk: clk, tdb: tdb}, settings: st, bucket: bucket}
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
	}, idem(f.bizAPI))
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
	r := f.call(tok, http.MethodPost, F, map[string]any{"brandName": "Mine", "ownerPhone": agentPhone}, idem(f.bizAPI))
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.ownerPhone")

	// Create needs an Idempotency-Key; a retry with the same key answers the
	// same draft and makes no second owner invite.
	ravi := map[string]any{"brandName": "Ravi Medicals", "ownerPhone": shopOwnerPhone}
	r = f.call(tok, http.MethodPost, F, ravi, nil)
	require.Equal(t, http.StatusBadRequest, r.code, r.raw)
	key := idem(f.bizAPI)
	r = f.call(tok, http.MethodPost, F, ravi, key)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	again := f.call(tok, http.MethodPost, F, ravi, key)
	require.Equal(t, http.StatusCreated, again.code, again.raw)
	require.Equal(t, r.body["id"], again.body["id"])
	require.Equal(t, 1, f.count(`SELECT count(*) FROM businesses WHERE onboarded_by = $1`, uuidOf(t, agent.id)))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM staff WHERE business_id = $1`, uuidOf(t, r.body["id"].(string))))

	// Not ready: the code is not sent.
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
	r = f.call(tok, http.MethodPatch, B, map[string]any{"ownerPhone": shop2OwnerPhone}, nil)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r), "submitted: the owner's number stays")

	// The owner signs in to the business app and sees it.
	owner := f.signIn(shopOwnerPhone, "business")
	r = f.call(owner, http.MethodGet, "/v1/biz/businesses", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Contains(t, r.raw, bid)

	// D-232: an item the owner added is the shop's. The agent cannot change
	// it (their own still change); an admin can.
	r = f.call(owner, http.MethodPost, "/v1/biz/businesses/"+bid+"/stores/"+sid+"/items", map[string]any{"productId": f.product("Fortune Oil"), "pricePaise": 4000}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	shopItem := r.body["id"].(string)
	r = f.call(tok, http.MethodPatch, S+"/items/"+shopItem, map[string]any{"pricePaise": 3900}, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = f.call(tok, http.MethodPatch, S+"/items/"+item, map[string]any{"pricePaise": 1000}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	admin := f.user(fieldAdminPhone, "admin")
	r = f.call(admin.token(), http.MethodPatch, S+"/items/"+shopItem, map[string]any{"pricePaise": 3900}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)

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

	// Every failed try is audited (what happened, never the number or a code).
	fails := func(outcome string) int {
		return f.count(`SELECT count(*) FROM audit_log WHERE action = 'business.owner_code_fail' AND subject_id = $1
		  AND actor_kind = 'admin' AND after->>'outcome' = $2`, bid, outcome)
	}
	require.Equal(t, 4, fails("wrong_code"))
	require.Equal(t, 1, fails("locked_out"))
	require.Equal(t, 1, fails("locked"))
	require.Zero(t, f.count(`SELECT count(*) FROM audit_log WHERE action = 'business.owner_code_fail'
	  AND (after::text LIKE '%' || $1 || '%' OR after::text LIKE '%111111%')`, shopOwnerPhone[3:]))

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
	salt := r.body["id"].(string)
	r = f.call(tok, http.MethodGet, B, nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, true, r.body["itemsEditable"])
	require.NotNil(t, r.body["editableUntil"])

	// The exact end, verified_at + 30 days, is still inside (inclusive).
	end := verified.Add(30 * 24 * time.Hour)
	f.clk.Set(end.Add(-time.Minute))
	tok = agent.token() // the sign-in moves the clock on 31 s
	f.clk.Set(end)
	r = f.call(tok, http.MethodPatch, S+"/items/"+salt, map[string]any{"pricePaise": 2400}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// A second later: refused (item add and change, store, photo); the list still reads.
	f.clk.Set(end.Add(time.Second))
	r = f.call(tok, http.MethodPost, S+"/items", map[string]any{"productId": p2, "pricePaise": 4500}, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = f.call(tok, http.MethodPatch, S+"/items/"+salt, map[string]any{"pricePaise": 2300}, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = f.call(tok, http.MethodPost, B+"/stores", map[string]any{"name": "Branch 2"}, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = f.call(tok, http.MethodPost, B+"/documents", map[string]any{"kind": "storefront", "contentType": "image/jpeg", "sizeBytes": 2048, "storeId": sid}, nil)
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
	require.Equal(t, false, items[0].(map[string]any)["disabled"])

	// A disabled agent stays listed, marked.
	f.exec(`UPDATE admin_users SET disabled_at = $1 WHERE user_id = $2`, f.clk.Now(), uuidOf(t, agent.id))
	r = f.call(admin.token(), http.MethodGet, "/v1/admin/field-agents", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	items = r.body["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, true, items[0].(map[string]any)["disabled"])
	require.EqualValues(t, 2, items[0].(map[string]any)["businesses"].(map[string]any)["total"])
}

// Photos are pending until the PUT is confirmed. Only stored photos count,
// and an upload never confirmed stops holding a place once its URL expires
// (lead review of b42d9ee). A rejected or suspended business takes no photo,
// store or item, from an agent or an admin.
func TestFieldPhotosHTTP(t *testing.T) {
	f := newFieldAPI(t)
	agent := f.user(agentPhone, "field_agent")
	admin := f.user(fieldAdminPhone, "admin")
	tok := agent.token()
	bid, sid := f.onboard(tok, "Hampi Stores", shopOwnerPhone)
	B := "/v1/admin/field/businesses/" + bid
	photo := func(tok string) resp {
		return f.call(tok, http.MethodPost, B+"/documents", map[string]any{"kind": "storefront", "contentType": "image/jpeg", "sizeBytes": 2048, "storeId": sid}, nil)
	}
	confirm := func(up resp) resp {
		doc := up.body["document"].(map[string]any)["id"].(string)
		return f.call(tok, http.MethodPost, B+"/documents/"+doc+"/confirm", nil, nil)
	}
	photos := func() any {
		r := f.call(tok, http.MethodGet, B, nil, nil)
		require.Equal(t, http.StatusOK, r.code, r.raw)
		return r.body["photoCount"]
	}

	// Nothing stored yet: the confirm says so and the photo stays pending.
	up := photo(tok)
	require.Equal(t, http.StatusCreated, up.code, up.raw)
	require.Equal(t, "pending", up.body["document"].(map[string]any)["status"])
	r := confirm(up)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.EqualValues(t, 0, photos())
	f.bucket.put(t, up)
	r = confirm(up)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "uploaded", r.body["status"])
	r = confirm(up)
	require.Equal(t, http.StatusOK, r.code, r.raw, "a second confirm changes nothing")
	require.EqualValues(t, 1, photos())

	// Four uploads that failed: they hold their places while the URLs live.
	var failed resp
	for range 4 {
		failed = photo(tok)
		require.Equal(t, http.StatusCreated, failed.code, failed.raw)
	}
	r = photo(tok)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.EqualValues(t, 1, photos())

	// Once those URLs expire, the failed retries no longer block the sixth.
	f.clk.Advance(20*time.Minute + time.Second)
	tok = agent.token()
	r = photo(tok)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	f.bucket.put(t, failed)
	r = confirm(failed)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r), "an expired upload is added again")

	// A rejected or suspended business: refused for the agent and for an admin.
	atok := admin.token()
	S := B + "/stores/" + sid
	p := f.product("Tata Salt")
	for _, status := range []string{"rejected", "suspended"} {
		f.exec(`UPDATE businesses SET status = $1 WHERE id = $2`, status, uuidOf(t, bid))
		for _, who := range []string{tok, atok} {
			r = photo(who)
			require.Equal(t, http.StatusForbidden, r.code, "%s photo: %s", status, r.raw)
			// The shared store insert refuses it first, as on the owner routes.
			r = f.call(who, http.MethodPost, B+"/stores", map[string]any{"name": "Branch 2"}, nil)
			require.Equal(t, http.StatusConflict, r.code, "%s store: %s", status, r.raw)
			require.Equal(t, "invalid_transition", problemCode(r))
			r = f.call(who, http.MethodPost, S+"/items", map[string]any{"productId": p, "pricePaise": 2500}, nil)
			require.Equal(t, http.StatusForbidden, r.code, "%s item: %s", status, r.raw)
		}
	}
}

// Lead review of b42d9ee: who may send and check the owner code, the caps
// (per agent; per number with sign-in codes), blocked accounts, and a correct
// code that cannot finish staying unused.
func TestFieldOwnerCodeGuardsHTTP(t *testing.T) {
	f := newFieldAPI(t)
	agent := f.user(agentPhone, "field_agent")
	other := f.user(agent2Phone, "field_agent")
	tok, otok := agent.token(), other.token()
	F := "/v1/admin/field/businesses"
	bid, _ := f.onboard(tok, "Durga Stores", shopOwnerPhone)
	B := F + "/" + bid
	unused := func() int {
		return f.count(`SELECT count(*) FROM field_owner_codes WHERE business_id = $1 AND consumed_at IS NULL`, uuidOf(t, bid))
	}

	// Another agent can neither send this shop's code nor check it.
	r := f.call(otok, http.MethodPost, B+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = f.call(tok, http.MethodPost, B+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = f.call(otok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)

	// A correct code while the business is not ready: 409, and the code stays unused.
	f.exec(`UPDATE businesses SET category_id = NULL WHERE id = $1`, uuidOf(t, bid))
	r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "submit_incomplete", problemCode(r))
	require.Equal(t, 1, unused())
	r = f.call(tok, http.MethodPatch, B, map[string]any{"categoryId": f.category}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// The owner's account is blocked after the send: a correct code is 403 and
	// stays unused; no new invite or code goes to a blocked number.
	cust := f.signIn(shopOwnerPhone, "customer")
	me := f.call(cust, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(t, http.StatusOK, me.code, me.raw)
	ownerID := uuidOf(t, me.body["id"].(string))
	f.exec(`UPDATE users SET status = 'blocked' WHERE id = $1`, ownerID)
	r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, 1, unused())
	r = f.call(tok, http.MethodPost, F, map[string]any{"brandName": "Blocked", "ownerPhone": shopOwnerPhone}, idem(f.bizAPI))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	other2, _ := f.onboard(tok, "Kaveri Stores", shop2OwnerPhone)
	r = f.call(tok, http.MethodPatch, F+"/"+other2, map[string]any{"ownerPhone": shopOwnerPhone}, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	f.clk.Advance(31 * time.Second)
	r = f.call(tok, http.MethodPost, B+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM field_owner_codes WHERE business_id = $1`, uuidOf(t, bid)))

	// Unblocked, the same code confirms, once: two at the same time give one 200.
	f.exec(`UPDATE users SET status = 'active' WHERE id = $1`, ownerID)
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() {
			codes[i] = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil).code
		})
	}
	wg.Wait()
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusConflict}, codes)
	require.Equal(t, 0, unused())
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'business.submit' AND subject_id = $1`, bid))

	// Per agent: field_owner_codes_per_agent_hour over every shop and number.
	f.exec(`UPDATE platform_settings SET value = '2' WHERE key = 'field_owner_codes_per_agent_hour'`)
	f.settings.Invalidate()
	r = f.call(tok, http.MethodPost, F+"/"+other2+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	third, _ := f.onboard(tok, "Tunga Stores", "+919000000067")
	r = f.call(tok, http.MethodPost, F+"/"+third+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusTooManyRequests, r.code, r.raw)
	require.Equal(t, "rate_limited", problemCode(r))
	require.NotEmpty(t, r.hdr.Get("Retry-After"))
	// Another agent has their own count.
	theirs, _ := f.onboard(otok, "Netra Stores", "+919000000068")
	r = f.call(otok, http.MethodPost, F+"/"+theirs+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// Per number: sign-in codes to the number count with owner codes (01 §2).
	signIns, _ := f.onboard(otok, "Sharavathi Stores", "+919000000069")
	for range 5 {
		f.signIn("+919000000069", "customer")
		f.clk.Advance(31 * time.Second)
	}
	r = f.call(otok, http.MethodPost, F+"/"+signIns+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusTooManyRequests, r.code, r.raw)
	require.Equal(t, "rate_limited", problemCode(r))
}

package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/business"
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
	cfg      *config.Config
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
	f := &fieldAPI{bizAPI: &bizAPI{t: t, h: h, clk: clk, tdb: tdb}, cfg: cfg, settings: st, bucket: bucket}
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
	// D-241: the confirm also sends the agent's complete store for review.
	require.Equal(t, 1, f.count(`SELECT count(*) FROM stores WHERE id = $1 AND status = 'pending_review'`, uuidOf(t, sid)))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'store.submit' AND subject_type = 'store' AND subject_id = $1`, sid))
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
	atok := admin.token()
	r = f.call(atok, http.MethodPatch, S+"/items/"+shopItem, map[string]any{"pricePaise": 3900}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	// The list says which items each caller may change (editable).
	editable := func(tok string) map[string]any {
		r := f.call(tok, http.MethodGet, S+"/items", nil, nil)
		require.Equal(t, http.StatusOK, r.code, r.raw)
		out := map[string]any{}
		for _, it := range r.body["items"].([]any) {
			m := it.(map[string]any)
			out[m["id"].(string)] = m["editable"]
		}
		return out
	}
	mine, all := editable(tok), editable(atok)
	require.Equal(t, true, mine[item])
	require.Equal(t, false, mine[shopItem])
	for id, e := range all {
		require.Equal(t, true, e, "admin: %s", id)
	}
	require.Len(t, all, len(mine))

	// Audit: by the agent as an admin actor, never the number or a code.
	require.Positive(t, f.count(`SELECT count(*) FROM audit_log WHERE action = 'business.submit' AND subject_id = $1 AND actor_kind = 'admin'`, bid))
	require.Zero(t, f.count(`SELECT count(*) FROM audit_log WHERE after::text LIKE '%' || $1 || '%'`, shopOwnerPhone[3:]))

	// The agent's list shows their own.
	r = f.call(tok, http.MethodGet, F, nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 2)
}

// SC-FA-06: five wrong codes lock it; a lapsed code is expired; a new owner
// number ends the old code. A locked or expired code is audited once.
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
	for range 2 {
		r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil)
		require.Equal(t, http.StatusTooManyRequests, r.code, r.raw)
		require.Equal(t, "otp_locked", problemCode(r))
	}

	// Every failed try is audited (a locked code once) (what happened, never the number or a code).
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
	for range 3 {
		r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil)
		require.Equal(t, http.StatusUnauthorized, r.code, r.raw)
		require.Equal(t, "otp_expired", problemCode(r))
	}
	// Audited once per code, however often it is tried.
	require.Equal(t, 1, fails("expired"))

	// A code sent, then the number changed: the code no longer confirms.
	r = f.call(tok, http.MethodPost, B+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = f.call(tok, http.MethodPatch, B, map[string]any{"ownerPhone": shop2OwnerPhone}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, pii.MaskPhone(shop2OwnerPhone), r.body["ownerPhoneMasked"])
	for range 3 {
		r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil)
		require.Equal(t, http.StatusUnauthorized, r.code, r.raw)
		require.Equal(t, "otp_expired", problemCode(r))
	}
	// The lapsed code and the one for the old number are audited as expired,
	// once each.
	require.Equal(t, 2, fails("expired"))
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

// Lead re-review of 88e4624: only the business's agent confirms a photo, and
// only the declared type and size; field photos never confirmed neither block
// the owner's uploads nor stay: the sweep rejects them and deletes their
// objects, leaving the owner's own pending uploads and live URLs alone.
func TestFieldPhotoSweepHTTP(t *testing.T) {
	f := newFieldAPI(t)
	agent := f.user(agentPhone, "field_agent")
	other := f.user(agent2Phone, "field_agent")
	tok, otok := agent.token(), other.token()
	bid, sid := f.onboard(tok, "Kodagu Stores", shopOwnerPhone)
	B := "/v1/admin/field/businesses/" + bid
	photo := map[string]any{"kind": "storefront", "contentType": "image/jpeg", "sizeBytes": 2048, "storeId": sid}
	upload := func() resp {
		r := f.call(tok, http.MethodPost, B+"/documents", photo, nil)
		require.Equal(t, http.StatusCreated, r.code, r.raw)
		return r
	}
	docID := func(up resp) string { return up.body["document"].(map[string]any)["id"].(string) }
	confirm := func(tok string, up resp) resp {
		return f.call(tok, http.MethodPost, B+"/documents/"+docID(up)+"/confirm", nil, nil)
	}
	status := func(up resp) string {
		var st string
		require.NoError(t, f.tdb.OwnerPool.QueryRow(context.Background(), `SELECT status FROM business_documents WHERE id = $1`, uuidOf(t, docID(up))).Scan(&st))
		return st
	}
	stored := func(up resp, contentType, size string) {
		u, err := url.Parse(up.body["upload"].(map[string]any)["url"].(string))
		require.NoError(t, err)
		f.bucket.mu.Lock()
		defer f.bucket.mu.Unlock()
		f.bucket.objects[u.Path] = [2]string{contentType, size}
	}

	// Another agent cannot confirm it; a file of another type or larger than
	// declared is refused and the photo stays pending.
	up := upload()
	f.bucket.put(t, up)
	r := confirm(otok, up)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	for _, o := range [][2]string{{"image/png", "2048"}, {"image/jpeg", "4096"}} {
		stored(up, o[0], o[1])
		r = confirm(tok, up)
		require.Equal(t, http.StatusUnprocessableEntity, r.code, "%v: %s", o, r.raw)
		require.Equal(t, "pending", status(up))
	}
	f.bucket.put(t, up)
	r = confirm(tok, up)
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// Four never confirmed (one of them stored): with the first, the cap of 5.
	stale := make([]resp, 4)
	for i := range stale {
		stale[i] = upload()
	}
	f.bucket.put(t, stale[0])
	r = f.call(tok, http.MethodPost, B+"/documents", photo, nil)
	require.Equal(t, http.StatusConflict, r.code, r.raw)

	// The owner confirms; once the field URLs have expired, the owner's own
	// upload on the business app is not blocked by them.
	r = f.call(tok, http.MethodPost, B+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	f.clk.Advance(20*time.Minute + time.Second)
	owner := f.signIn(shopOwnerPhone, "business")
	mine := f.call(owner, http.MethodPost, "/v1/biz/businesses/"+bid+"/documents", photo, nil)
	require.Equal(t, http.StatusCreated, mine.code, mine.raw)
	tok = agent.token()
	fresh := upload()

	// The sweep (business_field_photos_sweep) takes only the expired field photos.
	svc, err := business.New(business.Deps{Config: f.cfg, DB: db.New(f.tdb.Pool), Clock: f.clk, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	require.NoError(t, err)
	res, err := svc.SweepFieldPhotos(context.Background())
	require.NoError(t, err)
	require.Equal(t, business.SweepResult{Rejected: 4}, res)
	for _, s := range stale {
		require.Equal(t, "rejected", status(s))
		require.False(t, f.bucket.has(t, s))
	}
	require.Equal(t, 1, f.count(`SELECT count(*) FROM business_documents WHERE id = $1 AND reject_reason = 'not uploaded in time'`, uuidOf(t, docID(stale[0]))))
	require.Equal(t, "uploaded", status(up))
	require.True(t, f.bucket.has(t, up))
	require.Equal(t, "pending", status(mine))
	require.Equal(t, "pending", status(fresh))
	res, err = svc.SweepFieldPhotos(context.Background())
	require.NoError(t, err)
	require.Zero(t, res.Rejected, "a second sweep finds nothing")
}

// Review of fcfaeb20: the sweep claims and deletes without a row lock, so a
// confirm whose bucket HEAD saw the object before the sweep took it must not
// mark the photo uploaded once confirm_by has passed: it answers expired and
// the photo stays pending for the sweep to reject.
func TestFieldPhotoConfirmRacesSweepHTTP(t *testing.T) {
	f := newFieldAPI(t)
	tok := f.user(agentPhone, "field_agent").token()
	bid, sid := f.onboard(tok, "Hampi Stores", shopOwnerPhone)
	B := "/v1/admin/field/businesses/" + bid
	r := f.call(tok, http.MethodPost, B+"/documents", map[string]any{"kind": "storefront", "contentType": "image/jpeg", "sizeBytes": 2048, "storeId": sid}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["document"].(map[string]any)["id"].(string)
	f.bucket.put(t, r)

	// While the confirm's HEAD is answered (the object still there):
	// confirm_by passes and a sweep claims the row, as ClaimStaleFieldDocuments
	// does; its delete of the object follows the claim.
	f.bucket.mu.Lock()
	f.bucket.onHead = func(string) {
		f.bucket.mu.Lock()
		f.bucket.onHead = nil
		f.bucket.mu.Unlock()
		f.clk.Advance(20*time.Minute + time.Second)
		f.exec(`UPDATE business_documents SET sweep_retry_at = $2 WHERE id = $1 AND status = 'pending'`, uuidOf(t, id), f.clk.Now().Add(10*time.Minute))
	}
	f.bucket.mu.Unlock()
	c := f.call(tok, http.MethodPost, B+"/documents/"+id+"/confirm", nil, nil)
	require.Equal(t, http.StatusConflict, c.code, c.raw)
	require.Equal(t, "invalid_transition", c.body["code"], c.raw)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM business_documents WHERE id = $1 AND status = 'pending'`, uuidOf(t, id)))
	require.Zero(t, f.count(`SELECT count(*) FROM audit_log WHERE action = 'document.uploaded' AND subject_id = $1`, id))

	// The sweep rejects it once the claim's lease has passed.
	f.clk.Advance(10*time.Minute + time.Second)
	svc, err := business.New(business.Deps{Config: f.cfg, DB: db.New(f.tdb.Pool), Clock: f.clk, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	require.NoError(t, err)
	res, err := svc.SweepFieldPhotos(context.Background())
	require.NoError(t, err)
	require.Equal(t, business.SweepResult{Rejected: 1}, res)
	require.False(t, f.bucket.has(t, r))
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

	// The owner's account is blocked after the send: a correct code is 422 on
	// the owner number (as any refused number, so it tells nothing more) and
	// stays unused; no new invite or code goes to a blocked number.
	cust := f.signIn(shopOwnerPhone, "customer")
	me := f.call(cust, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(t, http.StatusOK, me.code, me.raw)
	ownerID := uuidOf(t, me.body["id"].(string))
	f.exec(`UPDATE users SET status = 'blocked' WHERE id = $1`, ownerID)
	refused := func(r resp) {
		t.Helper()
		require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
		require.Equal(t, "validation_failed", problemCode(r))
		require.Contains(t, r.raw, "body.ownerPhone")
	}
	refused(f.call(tok, http.MethodPost, B+"/owner-confirm/verify", map[string]any{"code": "000000"}, nil))
	require.Equal(t, 1, unused())
	refused(f.call(tok, http.MethodPost, F, map[string]any{"brandName": "Blocked", "ownerPhone": shopOwnerPhone}, idem(f.bizAPI)))
	other2, _ := f.onboard(tok, "Kaveri Stores", shop2OwnerPhone)
	refused(f.call(tok, http.MethodPatch, F+"/"+other2, map[string]any{"ownerPhone": shopOwnerPhone}, nil))
	f.clk.Advance(31 * time.Second)
	refused(f.call(tok, http.MethodPost, B+"/owner-confirm", nil, nil))
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
	// Retry-After is when the (sends − cap + 1)-th oldest send leaves the hour.
	sentAt := func(place int) time.Time {
		var at time.Time
		require.NoError(t, f.tdb.OwnerPool.QueryRow(context.Background(), `SELECT created_at FROM field_owner_codes
		  WHERE sent_by = $1 ORDER BY created_at OFFSET $2 LIMIT 1`, uuidOf(t, agent.id), place).Scan(&at))
		return at
	}
	retryAfter := func(at time.Time) string {
		wait := at.Add(time.Hour).Sub(f.clk.Now())
		return strconv.Itoa(int((wait + time.Second - 1) / time.Second))
	}
	require.Equal(t, retryAfter(sentAt(0)), r.hdr.Get("Retry-After"))
	f.exec(`UPDATE platform_settings SET value = '1' WHERE key = 'field_owner_codes_per_agent_hour'`)
	f.settings.Invalidate()
	r = f.call(tok, http.MethodPost, F+"/"+third+"/owner-confirm", nil, nil)
	require.Equal(t, http.StatusTooManyRequests, r.code, r.raw)
	require.Equal(t, retryAfter(sentAt(1)), r.hdr.Get("Retry-After"))
	require.NotEqual(t, sentAt(0), sentAt(1))

	// At cap − 1, two sends at the same time: exactly one goes out.
	f.exec(`UPDATE platform_settings SET value = '3' WHERE key = 'field_owner_codes_per_agent_hour'`)
	f.settings.Invalidate()
	fourth, _ := f.onboard(tok, "Hemavathi Stores", "+919000000070")
	sends := make([]int, 2)
	var swg sync.WaitGroup
	for i, b := range []string{third, fourth} {
		swg.Go(func() {
			sends[i] = f.call(tok, http.MethodPost, F+"/"+b+"/owner-confirm", nil, nil).code
		})
	}
	swg.Wait()
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusTooManyRequests}, sends)
	require.Equal(t, 3, f.count(`SELECT count(*) FROM field_owner_codes WHERE sent_by = $1`, uuidOf(t, agent.id)))
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

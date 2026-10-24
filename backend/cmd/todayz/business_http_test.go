package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/testclock"
	"github.com/todayz/backend/internal/testkit"
)

// bizAPI is the real router and pipeline on a fresh database, for the
// business routes (W1-04, SC-BZ). People sign in over HTTP with reserved test
// numbers and the test code, in the business app.
type bizAPI struct {
	t   *testing.T
	h   http.Handler
	clk *clock.Fake
	tdb *testkit.DB
}

func newBizAPI(t *testing.T) *bizAPI {
	t.Helper()
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
	d := deps{
		Config: cfg, Log: log, Clock: clk,
		DB: db.New(tdb.Pool), Ready: &health.Readiness{},
		TestClock: testclock.New(clock.NewOffset(nil), nopStore{}, log),
	}
	h, err := buildRouter(d, idempotency.NewMemoryStore(), contractValidator(t))
	require.NoError(t, err)
	return &bizAPI{t: t, h: h, clk: clk, tdb: tdb}
}

type resp struct {
	code int
	hdr  http.Header
	body map[string]any
	raw  string
}

func (a *bizAPI) call(token, method, path string, body any, hdr map[string]string) resp {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		s, ok := body.(string)
		if !ok {
			b, err := json.Marshal(body)
			require.NoError(a.t, err)
			s = string(b)
		}
		rd = strings.NewReader(s)
	}
	req := httptest.NewRequest(method, "https://api.todayz.localhost"+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = "127.0.0.1:5555"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	a.h.ServeHTTP(w, req)
	out := resp{code: w.Code, hdr: w.Header(), raw: w.Body.String()}
	if strings.HasPrefix(w.Header().Get("Content-Type"), "application/") && w.Body.Len() > 0 {
		require.NoError(a.t, json.Unmarshal(w.Body.Bytes(), &out.body), out.raw)
	}
	return out
}

// signIn signs a reserved test number into an app and returns its access token.
func (a *bizAPI) signIn(phone, app string) string {
	a.t.Helper()
	w := a.call("", http.MethodPost, "/v1/auth/otp/send", map[string]string{
		"phone": phone, "app": app, "purpose": "login", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
	}, nil)
	require.Equal(a.t, http.StatusOK, w.code, w.raw)
	w = a.call("", http.MethodPost, "/v1/auth/otp/verify", map[string]string{
		"app": app, "requestId": w.body["requestId"].(string), "code": testOTP, "deviceId": ids.New(a.clk.Now()).String(),
	}, map[string]string{"User-Agent": "business-http-test"})
	require.Equal(a.t, http.StatusOK, w.code, w.raw)
	a.clk.Advance(31 * time.Second)
	return w.body["accessToken"].(string)
}

func ifMatch(r resp) map[string]string {
	return map[string]string{"If-Match": r.hdr.Get("ETag")}
}

func fieldsOf(r resp) map[string]any {
	f, _ := r.body["fields"].(map[string]any)
	return f
}

func (a *bizAPI) count(sql string, args ...any) int {
	a.t.Helper()
	var n int
	require.NoError(a.t, a.tdb.OwnerPool.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

const (
	ownerPhone    = "+919000000011"
	managerPhone  = "+919000000012"
	cashierPhone  = "+919000000013"
	strangerPhone = "+919000000014"
	cashier2Phone = "+919000000016"
	manager2Phone = "+919000000017"
	// s2CashierPhone never signs in; otherOwnerPhone owns a second business.
	s2CashierPhone  = "+919000000018"
	otherOwnerPhone = "+919000000019"
)

var fullHours = map[string]any{
	"weekly": map[string]any{
		"mon": []any{map[string]string{"open": "09:00", "close": "21:00"}},
		"tue": []any{map[string]string{"open": "09:00", "close": "21:00"}},
		"wed": []any{map[string]string{"open": "09:00", "close": "21:00"}},
		"thu": []any{map[string]string{"open": "09:00", "close": "21:00"}},
		"fri": []any{map[string]string{"open": "09:00", "close": "21:00"}},
		"sat": []any{map[string]string{"open": "18:00", "close": "02:00"}},
		"sun": []any{},
	},
	"holidays": []any{},
}

// TestBusinessHTTP walks onboarding, stores, staff, KYC, settings and
// documents through the real pipeline (SC-BZ-01…04, 08…15; 06 and 07 are the
// hours tables in internal/business).
func TestBusinessHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	a := newBizAPI(t)
	owner := a.signIn(ownerPhone, "business")
	customer := a.signIn(strangerPhone, "customer")

	// --- onboarding: create, save and resume (SC-BZ-02) ---------------------
	r := a.call(customer, http.MethodGet, "/v1/biz/businesses", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a customer-app token is refused: %s", r.raw)

	r = a.call(owner, http.MethodPost, "/v1/biz/businesses", map[string]any{"brandName": "Lakshmi Traders"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	bid := r.body["id"].(string)
	require.Equal(t, "draft", r.body["status"])
	require.Equal(t, "owner", r.body["myRole"])
	require.Regexp(t, `^[A-HJKMNP-Z2-9]{6}$`, r.body["referralCode"])
	require.Equal(t, `"v1"`, r.hdr.Get("ETag"))
	B := "/v1/biz/businesses/" + bid

	r = a.call(owner, http.MethodPatch, B, map[string]any{"legalName": "Lakshmi Traders LLP"}, nil)
	require.Equal(t, http.StatusPreconditionRequired, r.code, r.raw)
	r = a.call(owner, http.MethodPatch, B, map[string]any{"legalName": "Lakshmi Traders LLP"}, map[string]string{"If-Match": `"v9"`})
	require.Equal(t, http.StatusPreconditionFailed, r.code, r.raw)
	cur := a.call(owner, http.MethodGet, B, nil, nil)
	r = a.call(owner, http.MethodPatch, B, map[string]any{"legalName": "Lakshmi Traders LLP"}, ifMatch(cur))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = a.call(owner, http.MethodGet, B, nil, nil)
	require.Equal(t, "Lakshmi Traders LLP", r.body["legalName"], "the draft resumes with the saved answer")
	require.Equal(t, "Lakshmi Traders", r.body["brandName"], "a field left out keeps its value")

	// --- submit with missing fields → the list (SC-BZ-03) -------------------
	r = a.call(owner, http.MethodPost, B+"/submit", nil, nil)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "submit_incomplete", r.body["code"])
	require.Contains(t, fieldsOf(r), "categoryId")
	require.Contains(t, fieldsOf(r), "stores")

	r = a.call(owner, http.MethodPost, B+"/stores", map[string]any{"name": "Lakshmi Traders", "locality": "Jayanagar"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	sid := r.body["id"].(string)
	require.Equal(t, "lakshmi-traders-jayanagar", r.body["slug"])
	require.Regexp(t, `^[A-HJKMNP-Z2-9]{6}$`, r.body["shortCode"])
	require.Equal(t, false, r.body["openNow"], "no hours yet")
	S := B + "/stores/" + sid

	r = a.call(owner, http.MethodPost, B+"/submit", nil, nil)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	for _, f := range []string{"categoryId", "stores[0].addressLine1", "stores[0].city", "stores[0].pincode", "stores[0].location", "stores[0].phone", "stores[0].hours"} {
		require.Contains(t, fieldsOf(r), f)
	}
	require.NotContains(t, fieldsOf(r), "stores")

	cur = a.call(owner, http.MethodGet, S, nil, nil)
	r = a.call(owner, http.MethodPatch, S, map[string]any{
		"addressLine1": "12, 9th Main", "city": "Bengaluru", "pincode": "560041",
		"location": map[string]float64{"lat": 12.925, "lng": 77.583}, "phone": "+919000000021",
	}, ifMatch(cur))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "+919000000021", r.body["phone"])
	cur = a.call(owner, http.MethodGet, S+"/settings", nil, nil)
	require.Equal(t, http.StatusOK, cur.code, cur.raw)
	r = a.call(owner, http.MethodPatch, S+"/settings", map[string]any{"hours": fullHours}, ifMatch(cur))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, true, r.body["openNow"], "Friday 11:30 IST is inside 09:00–21:00")

	cur = a.call(owner, http.MethodGet, B, nil, nil)
	// businesses.category_id is a foreign key (D-211): a seeded category.
	var category pgtype.UUID
	require.NoError(t, a.tdb.OwnerPool.QueryRow(context.Background(), `SELECT id FROM categories WHERE parent_id IS NULL ORDER BY sort, slug LIMIT 1`).Scan(&category))
	categoryID, _ := ids.FromUUID(category)
	r = a.call(owner, http.MethodPatch, B, map[string]any{"categoryId": categoryID.String()}, ifMatch(cur))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = a.call(owner, http.MethodPost, B+"/submit", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "submitted", r.body["status"])
	r = a.call(owner, http.MethodPost, B+"/submit", nil, nil)
	require.Equal(t, "invalid_transition", r.body["code"], r.raw)
	r = a.call(owner, http.MethodPost, S+"/submit", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "pending_review", r.body["status"])

	// --- request-info: the owner sees exactly the fields (SC-BZ-04) ---------
	_, err := a.tdb.OwnerPool.Exec(context.Background(),
		`UPDATE businesses SET status = 'needs_info', needs_info_fields = '{storefrontPhoto}', needs_info_note = 'A clearer storefront photo, please' WHERE id = $1`,
		uuidOf(t, bid))
	require.NoError(t, err)
	r = a.call(owner, http.MethodGet, B, nil, nil)
	require.Equal(t, "needs_info", r.body["status"])
	ir := r.body["infoRequest"].(map[string]any)
	require.Equal(t, []any{"storefrontPhoto"}, ir["fields"])
	require.Equal(t, "A clearer storefront photo, please", ir["note"])

	// --- staff invites and accept (SC-BZ-08) -------------------------------
	r = a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": managerPhone, "role": "manager"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, "invited", r.body["status"])
	require.Equal(t, "90•••••012", r.body["phoneMasked"], r.raw)
	mgrStaff := r.body["id"].(string)
	r = a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": managerPhone, "role": "cashier"}, nil)
	require.Equal(t, http.StatusConflict, r.code, "one invite per phone: %s", r.raw)

	// An invite does not reveal who holds the phone (review H1). The stranger
	// has an account (they signed in to the customer app) with a name; the
	// invite is bound to it, yet shows neither its id nor its name.
	r = a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": strangerPhone, "role": "cashier"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	strangerInvite := r.body["id"].(string)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM staff WHERE id = $1 AND user_id IS NOT NULL`, uuidOf(t, strangerInvite)), "the invite is bound to the account")
	require.Nil(t, r.body["userId"], r.raw)
	require.Nil(t, r.body["name"], r.raw)
	require.Equal(t, "90•••••014", r.body["phoneMasked"], r.raw)
	require.NotNil(t, r.body["inviteExpiresAt"], r.raw)
	_, err = a.tdb.OwnerPool.Exec(context.Background(),
		`UPDATE users SET name = 'Hidden Holder' WHERE id = (SELECT user_id FROM staff WHERE id = $1)`, uuidOf(t, strangerInvite))
	require.NoError(t, err)
	r = a.call(owner, http.MethodPatch, B+"/staff/"+strangerInvite, map[string]any{"canDeliver": true}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Nil(t, r.body["userId"], r.raw)
	require.NotContains(t, r.raw, "Hidden Holder")
	r = a.call(owner, http.MethodGet, B+"/staff", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.NotContains(t, r.raw, "Hidden Holder", "the staff list hides the invited account too")
	found := false
	for _, it := range r.body["items"].([]any) {
		m := it.(map[string]any)
		if m["id"] == strangerInvite {
			found = true
			require.Nil(t, m["userId"], r.raw)
			require.Nil(t, m["name"], r.raw)
			require.Equal(t, "90•••••014", m["phoneMasked"], r.raw)
		}
	}
	require.True(t, found, r.raw)
	r = a.call(owner, http.MethodDelete, B+"/staff/"+strangerInvite, nil, nil)
	require.Equal(t, http.StatusNoContent, r.code, r.raw)

	r = a.call(owner, http.MethodPost, B+"/stores", map[string]any{"name": "Lakshmi Traders", "locality": "Jayanagar"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	sid2 := r.body["id"].(string)
	require.Regexp(t, `^lakshmi-traders-jayanagar-[a-z2-9]{4}$`, r.body["slug"], "a taken slug gets a suffix")
	S2 := B + "/stores/" + sid2

	r = a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": cashierPhone, "role": "cashier", "storeIds": []string{sid}}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	cashStaff := r.body["id"].(string)

	manager := a.signIn(managerPhone, "business")
	cashier := a.signIn(cashierPhone, "business")
	stranger := a.signIn(strangerPhone, "business")

	r = a.call(manager, http.MethodGet, "/v1/biz/businesses", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Empty(t, r.body["items"])
	inv := r.body["invitations"].([]any)
	require.Len(t, inv, 1)
	require.Equal(t, mgrStaff, inv[0].(map[string]any)["staffId"])

	r = a.call(stranger, http.MethodPost, B+"/staff/"+mgrStaff+"/accept", nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, "someone else's invite is not found: %s", r.raw)
	r = a.call(manager, http.MethodGet, B, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "not a member until the invite is accepted: %s", r.raw)
	r = a.call(manager, http.MethodPost, B+"/staff/"+mgrStaff+"/accept", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "active", r.body["status"])
	r = a.call(manager, http.MethodPost, B+"/staff/"+mgrStaff+"/accept", nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, "an accepted invite is gone: %s", r.raw)
	r = a.call(cashier, http.MethodPost, B+"/staff/"+cashStaff+"/accept", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 0, a.count(`SELECT count(*) FROM staff WHERE status = 'active' AND invite_phone_enc IS NOT NULL`), "an accepted invite keeps no phone")

	r = a.call(manager, http.MethodGet, B, nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "manager", r.body["myRole"])

	// --- staff limits by role (SC-BZ-01) -----------------------------------
	r = a.call(manager, http.MethodPost, B+"/staff", map[string]any{"phone": cashier2Phone, "role": "cashier"}, nil)
	require.Equal(t, http.StatusCreated, r.code, "a manager adds a cashier: %s", r.raw)
	cash2Staff := r.body["id"].(string)
	r = a.call(manager, http.MethodPost, B+"/staff", map[string]any{"phone": manager2Phone, "role": "manager"}, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a manager adds a manager: %s", r.raw)
	r = a.call(manager, http.MethodPatch, B+"/staff/"+cashStaff, map[string]any{"role": "manager"}, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a manager promotes a cashier: %s", r.raw)
	r = a.call(manager, http.MethodDelete, B+"/staff/"+mgrStaff, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a manager removes a manager (themselves): %s", r.raw)
	ownerStaff := staffIDOf(t, a.call(owner, http.MethodGet, B+"/staff", nil, nil), "owner")
	r = a.call(manager, http.MethodDelete, B+"/staff/"+ownerStaff, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a manager removes the owner: %s", r.raw)
	r = a.call(owner, http.MethodDelete, B+"/staff/"+ownerStaff, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "the owner's row never changes here: %s", r.raw)
	r = a.call(manager, http.MethodDelete, B+"/staff/"+cash2Staff, nil, nil)
	require.Equal(t, http.StatusNoContent, r.code, "a manager removes a cashier: %s", r.raw)
	require.Equal(t, 0, a.count(`SELECT count(*) FROM staff WHERE status = 'removed' AND invite_phone_hash IS NOT NULL`), "a removed invite keeps no phone")

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, B + "/staff"},
		{http.MethodPost, B + "/staff"},
		{http.MethodPatch, B + "/staff/" + cashStaff},
		{http.MethodDelete, B + "/staff/" + cashStaff},
	} {
		var body any
		switch c.method {
		case http.MethodPost:
			body = map[string]any{"phone": manager2Phone, "role": "cashier"}
		case http.MethodPatch:
			body = map[string]any{"canDeliver": true}
		}
		r = a.call(cashier, c.method, c.path, body, nil)
		require.Equal(t, http.StatusForbidden, r.code, "a cashier: %s %s: %s", c.method, c.path, r.raw)
	}

	r = a.call(owner, http.MethodPatch, B+"/staff/"+cashStaff, map[string]any{"role": "manager"}, nil)
	require.Equal(t, http.StatusOK, r.code, "the owner promotes: %s", r.raw)
	require.Equal(t, []any{sid}, r.body["storeIds"], "the store scope is kept")
	r = a.call(owner, http.MethodPatch, B+"/staff/"+cashStaff, map[string]any{"role": "cashier", "canDeliver": true}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "cashier", r.body["role"])
	require.Equal(t, true, r.body["canDeliver"])
	r = a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": manager2Phone, "role": "manager", "storeIds": []string{sid}}, nil)
	require.Equal(t, http.StatusCreated, r.code, "the owner adds a manager: %s", r.raw)
	mgr2Staff := r.body["id"].(string)

	r = a.call(owner, http.MethodDelete, B, nil, nil)
	require.Equal(t, http.StatusMethodNotAllowed, r.code, "there is no business delete: %s", r.raw)
	r = a.call(owner, http.MethodGet, B, nil, nil)
	require.Equal(t, http.StatusOK, r.code)

	// --- store scoping (SC-BZ-09) ------------------------------------------
	r = a.call(cashier, http.MethodGet, "/v1/biz/businesses", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	items := r.body["items"].([]any)
	require.Len(t, items, 1)
	stores := items[0].(map[string]any)["stores"].([]any)
	require.Len(t, stores, 1, "a cashier scoped to one store sees that store only")
	require.Equal(t, sid, stores[0].(map[string]any)["id"])
	r = a.call(cashier, http.MethodGet, S+"/settings", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)

	manager2 := a.signIn(manager2Phone, "business")
	r = a.call(manager2, http.MethodPost, B+"/staff/"+mgr2Staff+"/accept", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = a.call(manager2, http.MethodGet, B+"/stores", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1, "a scoped manager lists their own store")
	r = a.call(manager2, http.MethodGet, S, nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = a.call(manager2, http.MethodGet, S2, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "another store is refused: %s", r.raw)
	r = a.call(manager2, http.MethodPost, B+"/staff", map[string]any{"phone": cashier2Phone, "role": "cashier"}, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a scoped manager invites only into their stores: %s", r.raw)
	r = a.call(manager2, http.MethodPost, B+"/staff", map[string]any{"phone": cashier2Phone, "role": "cashier", "storeIds": []string{sid}}, nil)
	require.Equal(t, http.StatusCreated, r.code, "a removed invite is invited again: %s", r.raw)
	require.NotEqual(t, cash2Staff, r.body["id"], "the removed invite kept no phone, so the new invite is a new row")
	require.Equal(t, []any{sid}, r.body["storeIds"])

	// A scoped manager acts only inside their stores (review): not on S2's
	// profile or review, and not on a cashier of S2.
	cur = a.call(owner, http.MethodGet, S2, nil, nil)
	require.Equal(t, http.StatusOK, cur.code, cur.raw)
	r = a.call(manager2, http.MethodPatch, S2, map[string]any{"name": "Renamed By Manager"}, ifMatch(cur))
	require.Equal(t, http.StatusForbidden, r.code, "a scoped manager edits another store: %s", r.raw)
	r = a.call(manager2, http.MethodPost, S2+"/submit", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a scoped manager submits another store: %s", r.raw)
	r = a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": s2CashierPhone, "role": "cashier", "storeIds": []string{sid2}}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	s2Cashier := r.body["id"].(string)
	r = a.call(manager2, http.MethodDelete, B+"/staff/"+s2Cashier, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a scoped manager removes a cashier of another store: %s", r.raw)
	r = a.call(owner, http.MethodGet, S2, nil, nil)
	require.Equal(t, "Lakshmi Traders", r.body["name"], "the refused edit changed nothing")
	r = a.call(owner, http.MethodDelete, B+"/staff/"+s2Cashier, nil, nil)
	require.Equal(t, http.StatusNoContent, r.code, r.raw)

	// A member who signed in and was removed comes back on the same row.
	r = a.call(owner, http.MethodDelete, B+"/staff/"+mgr2Staff, nil, nil)
	require.Equal(t, http.StatusNoContent, r.code, r.raw)
	r = a.call(manager2, http.MethodGet, B, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a removed member loses access at once: %s", r.raw)
	r = a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": manager2Phone, "role": "cashier"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, mgr2Staff, r.body["id"], "the removed member's row is taken up again")
	require.Equal(t, "invited", r.body["status"])

	// --- KYC: format, checksum, encryption and visibility (SC-BZ-10…15) -----
	K := B + "/kyc"
	// A business made before its KYC row existed (the create makes one): a
	// GET answers "missing" and writes nothing (review L4).
	_, err = a.tdb.OwnerPool.Exec(context.Background(), `DELETE FROM business_kyc WHERE business_id = $1`, uuidOf(t, bid))
	require.NoError(t, err)
	cur = a.call(owner, http.MethodGet, K, nil, nil)
	require.Equal(t, http.StatusOK, cur.code, cur.raw)
	require.Equal(t, "missing", cur.body["status"])
	require.Equal(t, 0, a.count(`SELECT count(*) FROM business_kyc WHERE business_id = $1`, uuidOf(t, bid)), "a GET writes nothing")
	require.Equal(t, `"v1"`, cur.hdr.Get("ETag"))
	r = a.call(owner, http.MethodPut, K, map[string]any{"pan": "ABCDE1234"}, ifMatch(cur))
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.pan")
	r = a.call(owner, http.MethodPut, K, map[string]any{"pan": "AAPFU0939F", "gstin": "27AAPFU0939F1ZW"}, ifMatch(cur))
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Equal(t, "check digit does not match", fieldsOf(r)["body.gstin"], r.raw)
	r = a.call(owner, http.MethodPut, K, map[string]any{
		"pan": "AAPFU0939F", "gstin": "27AAPFU0939F1ZV", "upiId": "lakshmitraders@okaxis",
		"bankAccount": "001234567890", "bankIfsc": "HDFC0001234", "legalName": "Lakshmi Traders LLP",
	}, ifMatch(cur))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "submitted", r.body["status"])
	require.Equal(t, "939F", r.body["panLast4"])
	require.Equal(t, "F1ZV", r.body["gstinLast4"])
	require.Equal(t, "ders", r.body["upiIdLast4"])
	require.Equal(t, "7890", r.body["bankAccountLast4"])
	require.Equal(t, "HDFC0001234", r.body["bankIfsc"])
	require.NotContains(t, r.raw, "AAPFU0939F")
	require.NotContains(t, r.raw, "lakshmitraders@")
	require.NotContains(t, r.raw, "001234567890")

	var panEnc, gstinEnc, upiEnc, bankEnc []byte
	require.NoError(t, a.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT pan_enc, gstin_enc, upi_id_enc, bank_account_enc FROM business_kyc WHERE business_id = $1`, uuidOf(t, bid)).
		Scan(&panEnc, &gstinEnc, &upiEnc, &bankEnc))
	for name, enc := range map[string][]byte{"pan": panEnc, "gstin": gstinEnc, "upi": upiEnc, "bank": bankEnc} {
		require.NotEmpty(t, enc, name)
		for _, plain := range []string{"AAPFU0939F", "27AAPFU0939F1ZV", "lakshmitraders", "001234567890"} {
			require.NotContains(t, string(enc), plain, "%s is stored sealed", name)
		}
	}
	require.Equal(t, 0, a.count(`SELECT count(*) FROM audit_log WHERE after::text LIKE '%AAPFU0939F%' OR after::text LIKE '%001234567890%' OR after::text LIKE '%lakshmitraders%'`), "the audit keeps field names only")
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'kyc.update'`))

	r = a.call(manager, http.MethodGet, K, nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "939F", r.body["panLast4"])
	require.Nil(t, r.body["bankIfsc"], "a manager sees KYC masked")
	require.Nil(t, r.body["legalName"])
	cur = a.call(owner, http.MethodGet, K, nil, nil)
	r = a.call(manager, http.MethodPut, K, map[string]any{"upiId": "manager.edit@okaxis"}, ifMatch(cur))
	require.Equal(t, http.StatusForbidden, r.code, "a manager does not edit KYC: %s", r.raw)
	r = a.call(cashier, http.MethodGet, K, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a cashier: %s", r.raw)
	r = a.call(stranger, http.MethodGet, K, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "not on the staff: %s", r.raw)

	// --- store settings: audited, asksMuted gated (D-101) -------------------
	cur = a.call(manager, http.MethodGet, S+"/settings", nil, nil)
	require.Equal(t, http.StatusOK, cur.code, cur.raw)
	r = a.call(manager, http.MethodPatch, S+"/settings", map[string]any{"asksMuted": true, "cashbackBp": 150}, ifMatch(cur))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, true, r.body["asksMuted"])
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'store.settings_update' AND (after->>'asksMuted')::bool AND (after->>'cashbackBp')::int = 150`))
	r = a.call(manager, http.MethodPatch, S+"/settings", map[string]any{"asksMuted": false}, ifMatch(cur))
	require.Equal(t, http.StatusPreconditionFailed, r.code, "a stale ETag: %s", r.raw)
	closed := a.clk.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	cur = a.call(manager, http.MethodGet, S+"/settings", nil, nil)
	r = a.call(manager, http.MethodPatch, S+"/settings", map[string]any{"closedUntil": closed}, ifMatch(cur))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, false, r.body["openNow"], "closed by hand")
	require.Equal(t, true, r.body["asksMuted"], "a field left out keeps its value")

	// --- documents ---------------------------------------------------------
	pan := map[string]any{"kind": "pan", "contentType": "application/pdf", "sizeBytes": 482113}
	r = a.call(owner, http.MethodPost, B+"/documents", pan, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, "no-store", r.hdr.Get("Cache-Control"))
	require.Equal(t, "pending", r.body["document"].(map[string]any)["status"], "pending until the upload is confirmed")
	up := r.body["upload"].(map[string]any)
	require.Equal(t, "PUT", up["method"])
	require.Equal(t, map[string]any{"Content-Type": "application/pdf", "Content-Length": "482113"}, up["headers"])
	require.True(t, strings.HasPrefix(up["url"].(string), "http://127.0.0.1:9100/todayz-private/businesses/"+bid+"/documents/"), up["url"])
	require.Contains(t, up["url"], "X-Amz-SignedHeaders=content-length%3Bcontent-type%3Bhost", "the size is signed (review M1)")
	require.NotContains(t, r.raw, "test-secret-value")
	r = a.call(owner, http.MethodPost, B+"/documents", map[string]any{"kind": "pan", "contentType": "application/pdf", "sizeBytes": 1234, "storeId": ids.New(a.clk.Now()).String()}, nil)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	for _, size := range []int64{0, 10<<20 + 1} {
		r = a.call(owner, http.MethodPost, B+"/documents", map[string]any{"kind": "pan", "contentType": "application/pdf", "sizeBytes": size}, nil)
		require.Equal(t, http.StatusUnprocessableEntity, r.code, "size %d: %s", size, r.raw)
		require.Contains(t, fieldsOf(r), "body.sizeBytes", r.raw)
	}
	r = a.call(manager, http.MethodGet, B+"/documents", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "documents are KYC: %s", r.raw)
	r = a.call(owner, http.MethodGet, B+"/documents", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	// At most 5 documents of a kind per business, pending ones included.
	for range 4 {
		r = a.call(owner, http.MethodPost, B+"/documents", pan, nil)
		require.Equal(t, http.StatusCreated, r.code, r.raw)
	}
	r = a.call(owner, http.MethodPost, B+"/documents", pan, nil)
	require.Equal(t, http.StatusConflict, r.code, "the sixth of a kind: %s", r.raw)
	r = a.call(owner, http.MethodPost, B+"/documents", map[string]any{"kind": "gst", "contentType": "image/png", "sizeBytes": 1234}, nil)
	require.Equal(t, http.StatusCreated, r.code, "another kind has its own cap: %s", r.raw)
	require.Equal(t, 6, a.count(`SELECT count(*) FROM business_documents WHERE business_id = $1`, uuidOf(t, bid)))

	// --- nothing personal in the audit log ---------------------------------
	for _, p := range []string{ownerPhone, managerPhone, cashierPhone, cashier2Phone, manager2Phone, "+919000000021", "9th Main"} {
		require.Equal(t, 0, a.count(`SELECT count(*) FROM audit_log WHERE coalesce(before::text, '') || coalesce(after::text, '') LIKE '%' || $1 || '%'`, p), p)
	}
}

// newBusiness creates a draft business with one store as the token's owner
// and returns their ids.
func (a *bizAPI) newBusiness(token, brand string) (bid, sid string) {
	a.t.Helper()
	r := a.call(token, http.MethodPost, "/v1/biz/businesses", map[string]any{"brandName": brand}, nil)
	require.Equal(a.t, http.StatusCreated, r.code, r.raw)
	bid = r.body["id"].(string)
	r = a.call(token, http.MethodPost, "/v1/biz/businesses/"+bid+"/stores", map[string]any{"name": brand, "locality": "Jayanagar"}, nil)
	require.Equal(a.t, http.StatusCreated, r.code, r.raw)
	return bid, r.body["id"].(string)
}

func (a *bizAPI) exec(sql string, args ...any) {
	a.t.Helper()
	_, err := a.tdb.OwnerPool.Exec(context.Background(), sql, args...)
	require.NoError(a.t, err)
}

// TestBusinessTenancyHTTP: the owner of one business reaches nothing of
// another, by its paths or by mixing its store into their own business's path
// (review).
func TestBusinessTenancyHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	a := newBizAPI(t)
	ownerX := a.signIn(ownerPhone, "business")
	ownerY := a.signIn(otherOwnerPhone, "business")
	bx, _ := a.newBusiness(ownerX, "Lakshmi Traders")
	by, sy := a.newBusiness(ownerY, "Ganesh Stores")
	X, Y := "/v1/biz/businesses/"+bx, "/v1/biz/businesses/"+by

	kyc := a.call(ownerY, http.MethodGet, Y+"/kyc", nil, nil)
	require.Equal(t, http.StatusOK, kyc.code, kyc.raw)
	store := a.call(ownerY, http.MethodGet, Y+"/stores/"+sy, nil, nil)
	require.Equal(t, http.StatusOK, store.code, store.raw)
	for _, c := range []struct {
		method, path string
		body         any
		hdr          map[string]string
	}{
		{http.MethodGet, Y, nil, nil},
		{http.MethodGet, Y + "/kyc", nil, nil},
		{http.MethodPut, Y + "/kyc", map[string]any{"upiId": "intruder@okaxis"}, ifMatch(kyc)},
		{http.MethodGet, Y + "/staff", nil, nil},
		{http.MethodPost, Y + "/staff", map[string]any{"phone": cashierPhone, "role": "cashier"}, nil},
		{http.MethodGet, Y + "/stores", nil, nil},
		{http.MethodGet, Y + "/stores/" + sy, nil, nil},
		{http.MethodPatch, Y + "/stores/" + sy, map[string]any{"name": "Taken Over"}, ifMatch(store)},
		{http.MethodGet, Y + "/stores/" + sy + "/settings", nil, nil},
		{http.MethodGet, Y + "/documents", nil, nil},
		// Y's store under X's business, where the caller is the owner.
		{http.MethodGet, X + "/stores/" + sy, nil, nil},
		{http.MethodPatch, X + "/stores/" + sy, map[string]any{"name": "Taken Over"}, ifMatch(store)},
		{http.MethodPost, X + "/stores/" + sy + "/submit", nil, nil},
		{http.MethodGet, X + "/stores/" + sy + "/settings", nil, nil},
		{http.MethodPatch, X + "/stores/" + sy + "/settings", map[string]any{"asksMuted": true}, nil},
		{http.MethodPost, X + "/staff", map[string]any{"phone": cashierPhone, "role": "cashier", "storeIds": []string{sy}}, nil},
	} {
		r := a.call(ownerX, c.method, c.path, c.body, c.hdr)
		require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity}, r.code, "%s %s: %s", c.method, c.path, r.raw)
		if strings.HasPrefix(c.path, Y) {
			require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, r.code, "%s %s: %s", c.method, c.path, r.raw)
		}
	}
	r := a.call(ownerY, http.MethodGet, Y+"/stores/"+sy, nil, nil)
	require.Equal(t, "Ganesh Stores", r.body["name"], "nothing of Y changed")
	require.Equal(t, 0, a.count(`SELECT count(*) FROM business_kyc WHERE status <> 'missing'`))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM staff WHERE role <> 'owner'`))
}

// TestStoreReviewHTTP: a change to what review approved sends a live or
// paused store back to review, and a rejected or suspended business takes no
// store or KYC changes (review M4).
func TestStoreReviewHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	a := newBizAPI(t)
	owner := a.signIn(ownerPhone, "business")
	bid, sid := a.newBusiness(owner, "Lakshmi Traders")
	B := "/v1/biz/businesses/" + bid
	S := B + "/stores/" + sid
	profile := map[string]any{
		"name": "Lakshmi Traders", "addressLine1": "12, 9th Main", "city": "Bengaluru", "pincode": "560041",
		"location": map[string]float64{"lat": 12.925, "lng": 77.583}, "phone": "+919000000021",
	}
	cur := a.call(owner, http.MethodGet, S, nil, nil)
	r := a.call(owner, http.MethodPatch, S, profile, ifMatch(cur))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "draft", r.body["status"], "a draft stays a draft")

	patch := func(body map[string]any) resp {
		t.Helper()
		cur := a.call(owner, http.MethodGet, S, nil, nil)
		r := a.call(owner, http.MethodPatch, S, body, ifMatch(cur))
		require.Equal(t, http.StatusOK, r.code, r.raw)
		return r
	}
	for _, c := range []struct {
		from string
		body map[string]any
	}{
		{"live", map[string]any{"name": "Lakshmi Traders & Sons"}},
		{"live", map[string]any{"addressLine1": "14, 9th Main"}},
		{"live", map[string]any{"location": map[string]float64{"lat": 12.926, "lng": 77.583}}},
		{"live", map[string]any{"phone": "+919000000022"}},
		{"paused", map[string]any{"pincode": "560011"}},
	} {
		a.exec(`UPDATE stores SET status = $2, live_at = now() WHERE id = $1`, uuidOf(t, sid), c.from)
		r = patch(profile)
		require.Equal(t, c.from, r.body["status"], "the same values change nothing: %v", c.body)
		r = patch(c.body)
		require.Equal(t, "pending_review", r.body["status"], "%v", c.body)
		patch(profile)
	}
	require.Equal(t, 5, a.count(`SELECT count(*) FROM audit_log WHERE action = 'store.update'
		AND before->>'status' IN ('live', 'paused') AND after->>'status' = 'pending_review'`))

	kyc := a.call(owner, http.MethodGet, B+"/kyc", nil, nil)
	require.Equal(t, http.StatusOK, kyc.code, kyc.raw)
	for _, status := range []string{"suspended", "rejected"} {
		a.exec(`UPDATE businesses SET status = $2 WHERE id = $1`, uuidOf(t, bid), status)
		r = a.call(owner, http.MethodPost, B+"/stores", map[string]any{"name": "Second Shop"}, nil)
		require.Equal(t, http.StatusConflict, r.code, "%s: %s", status, r.raw)
		require.Equal(t, "invalid_transition", r.body["code"], r.raw)
		cur = a.call(owner, http.MethodGet, S, nil, nil)
		r = a.call(owner, http.MethodPatch, S, map[string]any{"name": "Renamed"}, ifMatch(cur))
		require.Equal(t, http.StatusConflict, r.code, "%s: %s", status, r.raw)
		require.Equal(t, "invalid_transition", r.body["code"], r.raw)
		r = a.call(owner, http.MethodPut, B+"/kyc", map[string]any{"upiId": "lakshmitraders@okaxis"}, ifMatch(kyc))
		require.Equal(t, http.StatusConflict, r.code, "%s: %s", status, r.raw)
		require.Equal(t, "invalid_transition", r.body["code"], r.raw)
	}
	require.Equal(t, 1, a.count(`SELECT count(*) FROM stores WHERE business_id = $1`, uuidOf(t, bid)))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM business_kyc WHERE status <> 'missing'`))
}

// TestStaffInvitesHTTP: invites lapse after 7 days and can be sent again
// (review M2), and two invites of one phone at once give one invite (review).
func TestStaffInvitesHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	a := newBizAPI(t)
	owner := a.signIn(ownerPhone, "business")
	bid, _ := a.newBusiness(owner, "Lakshmi Traders")
	B := "/v1/biz/businesses/" + bid

	// Two invites of the same phone at once: one 201, one 409.
	codes := make(chan int, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			codes <- a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": cashier2Phone, "role": "cashier"}, nil).code
		}()
	}
	close(start)
	got := []int{<-codes, <-codes}
	require.ElementsMatch(t, []int{http.StatusCreated, http.StatusConflict}, got)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM staff WHERE status = 'invited'`))

	// A user-bound invite: the manager has an account.
	manager := a.signIn(managerPhone, "business")
	r := a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": managerPhone, "role": "manager"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	mgr := r.body["id"].(string)
	// A phone-only invite: the cashier signs in later.
	r = a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": cashierPhone, "role": "cashier"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	cash := r.body["id"].(string)
	exp, err := time.Parse(time.RFC3339, r.body["inviteExpiresAt"].(string))
	require.NoError(t, err)
	require.Equal(t, a.clk.Now().Add(7*24*time.Hour).Truncate(time.Second), exp.Truncate(time.Second))

	// Seven days and a second later both have lapsed.
	a.exec(`UPDATE staff SET invited_at = invited_at - interval '7 days 1 second' WHERE status = 'invited'`)
	r = a.call(manager, http.MethodGet, "/v1/biz/businesses", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Empty(t, r.body["invitations"], "a lapsed invite is not listed")
	r = a.call(manager, http.MethodPost, B+"/staff/"+mgr+"/accept", nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, "a lapsed invite is not accepted: %s", r.raw)
	cashier := a.signIn(cashierPhone, "business")
	r = a.call(cashier, http.MethodPost, B+"/staff/"+cash+"/accept", nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, "a lapsed invite is not accepted: %s", r.raw)

	// Inviting again restarts the clock: the account's row is taken up again,
	// a phone-only invite is withdrawn and a new one made for whoever holds
	// the phone now.
	r = a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": managerPhone, "role": "manager"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, mgr, r.body["id"])
	r = a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": cashierPhone, "role": "cashier"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.NotEqual(t, cash, r.body["id"])
	cash2 := r.body["id"].(string)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM staff WHERE id = $1 AND status = 'removed' AND invite_phone_hash IS NULL`, uuidOf(t, cash)))
	r = a.call(manager, http.MethodGet, "/v1/biz/businesses", nil, nil)
	require.Len(t, r.body["invitations"], 1)
	r = a.call(manager, http.MethodPost, B+"/staff/"+mgr+"/accept", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.NotNil(t, r.body["userId"], "an accepted member shows their account")
	r = a.call(cashier, http.MethodPost, B+"/staff/"+cash2+"/accept", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
}

// TestLeadsHTTP: the site's lead form (04 §2.5) answers 202 and stores the
// same details once in 24 hours.
func TestLeadsHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	a := newBizAPI(t)
	lead := map[string]any{
		"name": "Lakshmi Devi", "phone": "+919000000021", "shopName": "Lakshmi Traders",
		"city": "Bengaluru", "source": "site:home:en", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
	}
	for range 2 {
		r := a.call("", http.MethodPost, "/v1/public/leads", lead, nil)
		require.Equal(t, http.StatusAccepted, r.code, r.raw)
		require.Equal(t, "no-store", r.hdr.Get("Cache-Control"))
		require.Equal(t, true, r.body["received"])
	}
	require.Equal(t, 1, a.count(`SELECT count(*) FROM leads`))
	a.clk.Advance(24*time.Hour + time.Second)
	lead["shopName"] = "LAKSHMI   traders"
	r := a.call("", http.MethodPost, "/v1/public/leads", lead, nil)
	require.Equal(t, http.StatusAccepted, r.code, r.raw)
	require.Equal(t, 2, a.count(`SELECT count(*) FROM leads`), "after the window the same details are stored again")
	require.Equal(t, 0, a.count(`SELECT count(*) FROM leads WHERE phone_enc::text LIKE '%9000000021%'`))

	lead["phone"] = "+911234567890"
	r = a.call("", http.MethodPost, "/v1/public/leads", lead, nil)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
}

func uuidOf(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return ids.UUID(id)
}

func staffIDOf(t *testing.T, r resp, role string) string {
	t.Helper()
	require.Equal(t, http.StatusOK, r.code, r.raw)
	for _, it := range r.body["items"].([]any) {
		m := it.(map[string]any)
		if m["role"] == role {
			return m["id"].(string)
		}
	}
	t.Fatalf("no %s in %s", role, r.raw)
	return ""
}

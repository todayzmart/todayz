package mw

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/ratelimit"
	"github.com/todayz/backend/internal/platform/reqctx"
)

var mwT0 = time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)

// fakeAuth maps a token to a principal or an error.
type fakeAuth map[string]any

func (f fakeAuth) Authenticate(_ context.Context, tok string) (authn.Principal, error) {
	switch v := f[tok].(type) {
	case authn.Principal:
		return v, nil
	case error:
		return authn.Principal{}, v
	}
	return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
}

func mwPrincipal(app authn.App, amr authn.AMR) authn.Principal {
	return authn.Principal{UserID: ids.New(mwT0), App: app, SessionID: ids.New(mwT0), AMR: amr}
}

func problemOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	return body.Code
}

func TestAuthn(t *testing.T) {
	cust := mwPrincipal(authn.AppCustomer, authn.AMROTP)
	biz := mwPrincipal(authn.AppBusiness, authn.AMROTP)
	admOTP := mwPrincipal(authn.AppAdmin, authn.AMROTP)
	adm := mwPrincipal(authn.AppAdmin, authn.AMROTPTOTP)
	a := fakeAuth{
		"cust": cust, "biz": biz, "adm-otp": admOTP, "adm": adm,
		"blocked": problem.New(genproblems.AccountBlocked, ""),
		"broken":  problem.Wrap(genproblems.Internal, errors.New("db down")),
	}
	var got authn.Principal
	var signedIn bool
	h := Authn(a)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, signedIn = authn.FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	type tc struct {
		path    string
		headers []string
		status  int
		code    string
	}
	for name, c := range map[string]tc{
		"anonymous public":          {"/v1/discover", nil, 204, ""},
		"anonymous auth":            {"/v1/auth/otp/send", nil, 204, ""},
		"anonymous me":              {"/v1/me", nil, 401, genproblems.Unauthorized},
		"anonymous me sub":          {"/v1/me/orders", nil, 401, genproblems.Unauthorized},
		"anonymous biz":             {"/v1/biz/businesses", nil, 401, genproblems.Unauthorized},
		"anonymous admin":           {"/v1/admin/users", nil, 401, genproblems.Unauthorized},
		"lookalike prefix":          {"/v1/meow", nil, 204, ""},
		"customer me":               {"/v1/me", []string{"Bearer cust"}, 204, ""},
		"scheme case":               {"/v1/me", []string{"bearer cust"}, 204, ""},
		"customer biz":              {"/v1/biz/x", []string{"Bearer cust"}, 403, genproblems.Forbidden},
		"customer admin":            {"/v1/admin/x", []string{"Bearer cust"}, 403, genproblems.Forbidden},
		"business biz":              {"/v1/biz/x", []string{"Bearer biz"}, 204, ""},
		"admin otp only, admin":     {"/v1/admin/x", []string{"Bearer adm-otp"}, 403, genproblems.Forbidden},
		"admin otp only, me":        {"/v1/me", []string{"Bearer adm-otp"}, 403, genproblems.Forbidden},
		"admin otp only, auth":      {"/v1/auth/totp/verify", []string{"Bearer adm-otp"}, 204, ""},
		"admin totp":                {"/v1/admin/x", []string{"Bearer adm"}, 204, ""},
		"admin on biz":              {"/v1/biz/x", []string{"Bearer adm"}, 403, genproblems.Forbidden},
		"bad token public":          {"/v1/discover", []string{"Bearer nope"}, 401, genproblems.Unauthorized},
		"basic scheme":              {"/v1/discover", []string{"Basic abc"}, 401, genproblems.Unauthorized},
		"no token":                  {"/v1/discover", []string{"Bearer "}, 401, genproblems.Unauthorized},
		"no space":                  {"/v1/discover", []string{"Bearer"}, 401, genproblems.Unauthorized},
		"two headers":               {"/v1/me", []string{"Bearer cust", "Bearer cust"}, 401, genproblems.Unauthorized},
		"oversized":                 {"/v1/me", []string{"Bearer " + strings.Repeat("a", authn.MaxTokenLen+1)}, 401, genproblems.Unauthorized},
		"blocked":                   {"/v1/me", []string{"Bearer blocked"}, 403, genproblems.AccountBlocked},
		"session store down":        {"/v1/me", []string{"Bearer broken"}, 500, genproblems.Internal},
		"empty header value counts": {"/v1/discover", []string{""}, 401, genproblems.Unauthorized},
	} {
		got, signedIn = authn.Principal{}, false
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		for _, v := range c.headers {
			req.Header.Add("Authorization", v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, c.status, rec.Code, name)
		if c.code != "" {
			require.Equal(t, c.code, problemOf(t, rec), name)
			continue
		}
		if len(c.headers) > 0 {
			require.True(t, signedIn, name)
			require.False(t, got.IsAnonymous(), name)
		} else {
			require.False(t, signedIn, name)
		}
	}

	// The cookie routes of sign-in ignore Authorization: an expired or junk
	// bearer is anonymous there, never 401 (a client refreshing because its
	// bearer expired still sends it). Other /v1/auth routes and lookalikes keep
	// the check.
	for _, p := range []string{"/v1/auth/otp/send", "/v1/auth/otp/verify", "/v1/auth/refresh", "/v1/auth/logout"} {
		for _, hdr := range [][]string{{"Bearer nope"}, {"Bearer cust"}, {"Basic abc"}, {"Bearer a", "Bearer b"}} {
			got, signedIn = authn.Principal{}, true
			req := httptest.NewRequest(http.MethodPost, p, nil)
			for _, v := range hdr {
				req.Header.Add("Authorization", v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			require.Equal(t, 204, rec.Code, "%s %v", p, hdr)
			require.False(t, signedIn, "%s is anonymous whatever the bearer", p)
		}
	}
	for _, p := range []string{"/v1/auth/totp/verify", "/v1/auth/refresh/", "/v1/auth/refreshx", "/v1/me"} {
		req := httptest.NewRequest(http.MethodPost, p, nil)
		req.Header.Set("Authorization", "Bearer nope")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, 401, rec.Code, p)
	}

	// No authenticator: anonymous requests pass, any Authorization is 401.
	h = Authn(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/discover", nil))
	require.Equal(t, 204, rec.Code)
	req := httptest.NewRequest(http.MethodGet, "/v1/discover", nil)
	req.Header.Set("Authorization", "Bearer cust")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, 401, rec.Code)
}

func TestRateLimit(t *testing.T) {
	clk := clock.NewFake(mwT0)
	l := ratelimit.New(clk, map[string]ratelimit.Rule{
		ratelimit.GroupAuth: {PerMinute: 60, Burst: 2},
		ratelimit.GroupMe:   {PerMinute: 60, Burst: 1},
	})
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := RateLimit(l)(ok)
	do := func(path, ip string, p authn.Principal) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		ctx := reqctx.WithClientIP(req.Context(), ip)
		ctx = authn.WithPrincipal(ctx, p)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req.WithContext(ctx))
		return rec
	}

	r := do("/v1/auth/otp/send", "10.0.0.1", authn.Principal{})
	require.Equal(t, 204, r.Code)
	require.Equal(t, "2", r.Header().Get("RateLimit-Limit"))
	require.Equal(t, "1", r.Header().Get("RateLimit-Remaining"))
	require.NotEmpty(t, r.Header().Get("RateLimit-Reset"))
	require.Equal(t, 204, do("/v1/auth/otp/send", "10.0.0.1", authn.Principal{}).Code)
	r = do("/v1/auth/otp/verify", "10.0.0.1", authn.Principal{})
	require.Equal(t, 429, r.Code, "the auth group shares one bucket per IP")
	require.Equal(t, genproblems.RateLimited, problemOf(t, r))
	require.Equal(t, "1", r.Header().Get("Retry-After"))
	require.Equal(t, 204, do("/v1/auth/otp/send", "10.0.0.2", authn.Principal{}).Code, "another IP")

	// Signed in: per user and app, not per IP.
	p := mwPrincipal(authn.AppCustomer, authn.AMROTP)
	require.Equal(t, 204, do("/v1/me", "10.0.0.3", p).Code)
	require.Equal(t, 429, do("/v1/me", "10.0.0.4", p).Code, "a new IP does not reset a user's bucket")
	require.Equal(t, 204, do("/v1/me", "10.0.0.3", mwPrincipal(authn.AppCustomer, authn.AMROTP)).Code, "another user")
	r = do("/v1/discover", "10.0.0.1", authn.Principal{})
	require.Equal(t, 204, r.Code, "a group without a rule is not limited")
	require.Empty(t, r.Header().Get("RateLimit-Limit"))

	clk.Advance(time.Second)
	require.Equal(t, 204, do("/v1/auth/otp/send", "10.0.0.1", authn.Principal{}).Code, "refilled")

	// A nil limiter is the identity.
	require.Equal(t, 204, func() int {
		rec := httptest.NewRecorder()
		RateLimit(nil)(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/me", nil))
		return rec.Code
	}())

	for path, g := range map[string]string{
		"/v1/auth": ratelimit.GroupAuth, "/v1/auth/x": ratelimit.GroupAuth, "/v1/me/orders": ratelimit.GroupMe,
		"/v1/biz/b": ratelimit.GroupBiz, "/v1/admin": ratelimit.GroupAdmin, "/v1/authx": ratelimit.GroupPublic,
		"/healthz": ratelimit.GroupPublic,
	} {
		require.Equal(t, g, groupOf(path), path)
	}
	require.Equal(t, "1", seconds(0))
	require.Equal(t, "2", seconds(1500*time.Millisecond))
}

type allowOnly string

func (a allowOnly) Check(_ context.Context, p authn.Principal, action string, s authz.Scope) (authz.Decision, error) {
	if p.IsAnonymous() {
		return authz.Decision{}, problem.New(genproblems.Unauthorized, "")
	}
	if action != string(a) {
		return authz.Decision{}, problem.New(genproblems.Forbidden, "")
	}
	return authz.Decision{Action: action, Grant: authz.Allow, Scope: s}, nil
}

func TestAuthzSlot(t *testing.T) {
	var d authz.Decision
	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(authn.WithPrincipal(req.Context(), mwPrincipal(authn.AppBusiness, authn.AMROTP))))
		})
	}).With(Authz(allowOnly("order.accept"), "order.accept")).Get("/v1/biz/businesses/{businessId}/orders", func(w http.ResponseWriter, req *http.Request) {
		d, _ = authz.DecisionFrom(req.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	r.With(Authz(allowOnly("x.y"), "order.accept")).Get("/refused", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.With(Authz(nil, "order.accept")).Get("/nil", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	biz := ids.New(mwT0)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/biz/businesses/"+biz.String()+"/orders", nil))
	require.Equal(t, 204, rec.Code)
	require.Equal(t, biz, d.Scope.BusinessID)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/biz/businesses/nope/orders", nil))
	require.Equal(t, 404, rec.Code)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/refused", nil))
	require.Equal(t, 401, rec.Code, "anonymous")

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nil", nil))
	require.Equal(t, 500, rec.Code, "no authorizer fails closed")
}

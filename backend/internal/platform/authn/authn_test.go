package authn

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
)

var t0 = time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)

// keys returns fresh random Ed25519 signing keys with the given kids, loaded
// through config the way the API loads JWT_SIGNING_KEYS.
func keys(t testing.TB, kids ...string) []config.SigningKey {
	t.Helper()
	var set []map[string]string
	for _, kid := range kids {
		seed := make([]byte, ed25519.SeedSize)
		_, err := rand.Read(seed)
		require.NoError(t, err)
		set = append(set, map[string]string{"kty": "OKP", "crv": "Ed25519", "kid": kid, "d": base64.RawURLEncoding.EncodeToString(seed)})
	}
	raw, err := json.Marshal(map[string]any{"keys": set})
	require.NoError(t, err)
	env := map[string]string{"APP_ENV": "test", "JWT_SIGNING_KEYS": string(raw), "JWT_CURRENT_KID": kids[0]}
	cfg, err := config.Load(func(k string) string { return env[k] }, nil)
	require.NoError(t, err)
	return cfg.Keys.JWTSigningKeys
}

func newTokens(t testing.TB, c clock.Clock, ks []config.SigningKey, current string) *Tokens {
	t.Helper()
	set, err := NewKeySet(ks, current)
	require.NoError(t, err)
	return NewTokens(set, c)
}

func principal(app App) Principal {
	p := Principal{UserID: ids.New(t0), App: app, SessionID: ids.New(t0), AMR: AMROTP}
	if app == AppAdmin {
		p.Roles = []string{"support"}
		p.AMR = AMROTPTOTP
	}
	return p
}

func TestIssueAndVerify(t *testing.T) {
	clk := clock.NewFake(t0)
	tk := newTokens(t, clk, keys(t, "k1"), "k1")
	for _, app := range Apps {
		p := principal(app)
		raw, exp, err := tk.Issue(p)
		require.NoError(t, err, app)
		require.Equal(t, t0.Add(AccessTTL), exp)
		got, err := tk.Verify(raw)
		require.NoError(t, err, app)
		require.Equal(t, p.UserID, got.UserID)
		require.Equal(t, p.SessionID, got.SessionID)
		require.Equal(t, app, got.App)
		require.Equal(t, p.AMR, got.AMR)
		require.Equal(t, t0, got.IssuedAt)
		require.Equal(t, exp, got.ExpiresAt)
		if app == AppAdmin {
			require.Equal(t, []string{"support"}, got.Roles)
		} else {
			require.Empty(t, got.Roles)
		}
	}
	// A customer principal carrying roles never gets them into the token.
	p := principal(AppCustomer)
	p.Roles = []string{"super_admin"}
	raw, _, err := tk.Issue(p)
	require.NoError(t, err)
	got, err := tk.Verify(raw)
	require.NoError(t, err)
	require.Empty(t, got.Roles)
}

func TestIssueRefusesIncompletePrincipals(t *testing.T) {
	tk := newTokens(t, clock.NewFake(t0), keys(t, "k1"), "k1")
	for name, mut := range map[string]func(*Principal){
		"anonymous":  func(p *Principal) { p.UserID = ids.Zero },
		"no app":     func(p *Principal) { p.App = "" },
		"no session": func(p *Principal) { p.SessionID = ids.Zero },
		"bad amr":    func(p *Principal) { p.AMR = "pwd" },
	} {
		p := principal(AppCustomer)
		mut(&p)
		_, _, err := tk.Issue(p)
		require.Error(t, err, name)
	}
}

func TestVerifyExpiry(t *testing.T) {
	clk := clock.NewFake(t0)
	tk := newTokens(t, clk, keys(t, "k1"), "k1")
	raw, _, err := tk.Issue(principal(AppCustomer))
	require.NoError(t, err)
	clk.Advance(AccessTTL - time.Second)
	_, err = tk.Verify(raw)
	require.NoError(t, err)
	clk.Advance(2 * time.Second)
	_, err = tk.Verify(raw)
	require.ErrorIs(t, err, ErrInvalidToken)
	// The test clock moving back does not break a token (iat is not checked).
	clk.Set(t0.Add(-time.Hour))
	_, err = tk.Verify(raw)
	require.NoError(t, err)
}

// TestKidRotation is AU-19: a token signed by a retired kid still verifies
// while that kid stays in the set; once removed, it is refused.
func TestKidRotation(t *testing.T) {
	clk := clock.NewFake(t0)
	ks := keys(t, "k1", "k2")
	old := newTokens(t, clk, ks, "k1")
	raw, _, err := old.Issue(principal(AppBusiness))
	require.NoError(t, err)

	rotated := newTokens(t, clk, ks, "k2")
	require.Equal(t, "k2", rotated.keys.CurrentKID())
	_, err = rotated.Verify(raw)
	require.NoError(t, err, "a retired kid still in the set verifies")
	fresh, _, err := rotated.Issue(principal(AppBusiness))
	require.NoError(t, err)
	tok, _, err := jwt.NewParser().ParseUnverified(fresh, jwt.MapClaims{})
	require.NoError(t, err)
	require.Equal(t, "k2", tok.Header["kid"])

	retired := newTokens(t, clk, ks[1:], "k2")
	_, err = retired.Verify(raw)
	require.ErrorIs(t, err, ErrInvalidToken, "a kid removed from the set is refused")
}

func TestNewKeySet(t *testing.T) {
	ks := keys(t, "k1", "k2")
	_, err := NewKeySet(ks, "nope")
	require.Error(t, err)
	_, err = NewKeySet(append(ks, ks[0]), "k1")
	require.Error(t, err, "duplicate kid")
	bad := ks[0]
	bad.KID = ""
	_, err = NewKeySet([]config.SigningKey{bad}, "")
	require.Error(t, err)
	bad = ks[0]
	bad.Public = bad.Public[:5]
	_, err = NewKeySet([]config.SigningKey{bad}, "k1")
	require.Error(t, err)
}

// forge signs claims with an arbitrary method and key, to try the attacks a
// verifier must refuse.
func forge(t *testing.T, m jwt.SigningMethod, key any, kid string, c jwt.Claims) string {
	t.Helper()
	tok := jwt.NewWithClaims(m, c)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(key)
	require.NoError(t, err)
	return s
}

func validClaims(app App) claims {
	return claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: Issuer, Subject: ids.New(t0).String(), Audience: jwt.ClaimStrings{string(app)},
			IssuedAt: jwt.NewNumericDate(t0), ExpiresAt: jwt.NewNumericDate(t0.Add(AccessTTL)),
		},
		SessionID: ids.New(t0).String(), AMR: AMROTP,
	}
}

func TestVerifyRefusesForgeries(t *testing.T) {
	clk := clock.NewFake(t0)
	ks := keys(t, "k1")
	tk := newTokens(t, clk, ks, "k1")
	priv := ks[0].Private()
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	good := forge(t, jwt.SigningMethodEdDSA, priv, "k1", validClaims(AppCustomer))
	_, err = tk.Verify(good)
	require.NoError(t, err, "the forge helper itself makes valid tokens")

	cases := map[string]string{
		"empty":        "",
		"too long":     strings.Repeat("a", MaxTokenLen+1),
		"garbage":      "not.a.jwt",
		"alg none":     forge(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, "k1", validClaims(AppCustomer)),
		"hs256 by pub": forge(t, jwt.SigningMethodHS256, []byte(ks[0].Public), "k1", validClaims(AppCustomer)),
		"other key":    forge(t, jwt.SigningMethodEdDSA, otherPriv, "k1", validClaims(AppCustomer)),
		"unknown kid":  forge(t, jwt.SigningMethodEdDSA, priv, "k9", validClaims(AppCustomer)),
		"no kid":       forge(t, jwt.SigningMethodEdDSA, priv, "", validClaims(AppCustomer)),
	}
	mutate := map[string]func(*claims){
		"wrong issuer":   func(c *claims) { c.Issuer = "evil" },
		"no issuer":      func(c *claims) { c.Issuer = "" },
		"no exp":         func(c *claims) { c.ExpiresAt = nil },
		"no iat":         func(c *claims) { c.IssuedAt = nil },
		"expired":        func(c *claims) { c.ExpiresAt = jwt.NewNumericDate(t0.Add(-time.Second)) },
		"unknown aud":    func(c *claims) { c.Audience = jwt.ClaimStrings{"storefront"} },
		"two auds":       func(c *claims) { c.Audience = jwt.ClaimStrings{"customer", "admin"} },
		"no aud":         func(c *claims) { c.Audience = nil },
		"bad sub":        func(c *claims) { c.Subject = "42" },
		"bad sid":        func(c *claims) { c.SessionID = "" },
		"bad amr":        func(c *claims) { c.AMR = "pwd" },
		"not yet valid":  func(c *claims) { c.NotBefore = jwt.NewNumericDate(t0.Add(time.Hour)) },
		"customer roles": func(c *claims) { c.Roles = []string{"super_admin"} }, // allowed, but dropped (below)
	}
	for name, mut := range mutate {
		c := validClaims(AppCustomer)
		mut(&c)
		cases[name] = forge(t, jwt.SigningMethodEdDSA, priv, "k1", c)
	}
	for name, raw := range cases {
		p, err := tk.Verify(raw)
		if name == "customer roles" {
			require.NoError(t, err)
			require.Empty(t, p.Roles, "roles in a non-admin token are ignored")
			continue
		}
		require.ErrorIs(t, err, ErrInvalidToken, name)
		require.Equal(t, Principal{}, p, name)
	}

	// A flipped signature byte.
	parts := strings.Split(good, ".")
	sig := []byte(parts[2])
	sig[3] ^= 1
	_, err = tk.Verify(parts[0] + "." + parts[1] + "." + string(sig))
	require.ErrorIs(t, err, ErrInvalidToken)
}

// FuzzVerify feeds the parser arbitrary input: it never panics, and anything
// it accepts was signed by our key (the seeds are valid tokens).
func FuzzVerify(f *testing.F) {
	clk := clock.NewFake(t0)
	ks := keys(f, "k1")
	set, err := NewKeySet(ks, "k1")
	if err != nil {
		f.Fatal(err)
	}
	tk := NewTokens(set, clk)
	for _, app := range Apps {
		raw, _, err := tk.Issue(principal(app))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw)
	}
	f.Add("")
	f.Add("a.b.c")
	f.Add("eyJhbGciOiJub25lIn0.e30.")
	f.Add(strings.Repeat(".", 100))
	f.Fuzz(func(t *testing.T, raw string) {
		p, err := tk.Verify(raw)
		if err != nil {
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("error %v is not ErrInvalidToken", err)
			}
			return
		}
		if p.IsAnonymous() || !p.App.Valid() || p.SessionID == ids.Zero {
			t.Fatalf("accepted an incomplete principal %+v", p)
		}
		tok, _, err := jwt.NewParser().ParseUnverified(raw, jwt.MapClaims{})
		if err != nil || tok.Method.Alg() != "EdDSA" {
			t.Fatalf("accepted a token that is not EdDSA")
		}
	})
}

type fakeSessions struct {
	st  SessionState
	err error
	got []ids.ID
}

func (f *fakeSessions) CheckSession(_ context.Context, userID, sessionID ids.ID, _ App) (SessionState, error) {
	f.got = []ids.ID{userID, sessionID}
	return f.st, f.err
}

func problemCode(t *testing.T, err error) string {
	t.Helper()
	var p *problem.Error
	require.ErrorAs(t, err, &p)
	return p.Code
}

func TestAuthenticator(t *testing.T) {
	clk := clock.NewFake(t0)
	tk := newTokens(t, clk, keys(t, "k1"), "k1")
	cust := principal(AppCustomer)
	raw, _, err := tk.Issue(cust)
	require.NoError(t, err)
	ctx := context.Background()

	// No session store: the token is trusted until it expires.
	a := NewAuthenticator(tk, nil)
	require.Same(t, tk, a.Tokens())
	p, err := a.Authenticate(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, cust.UserID, p.UserID)

	_, err = a.Authenticate(ctx, "junk")
	require.Equal(t, string(genproblems.Unauthorized), problemCode(t, err))

	fs := &fakeSessions{st: SessionState{Active: true}}
	a = NewAuthenticator(tk, fs)
	_, err = a.Authenticate(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, []ids.ID{cust.UserID, cust.SessionID}, fs.got)

	fs.st = SessionState{} // revoked
	_, err = a.Authenticate(ctx, raw)
	require.Equal(t, string(genproblems.Unauthorized), problemCode(t, err))

	fs.st = SessionState{Blocked: true} // AU-17
	_, err = a.Authenticate(ctx, raw)
	require.Equal(t, string(genproblems.AccountBlocked), problemCode(t, err))

	fs.st, fs.err = SessionState{}, errors.New("db down")
	_, err = a.Authenticate(ctx, raw)
	require.Equal(t, string(genproblems.Internal), problemCode(t, err))

	// Admin roles and step-up come from the live session, not the token.
	adm := principal(AppAdmin)
	rawAdm, _, err := tk.Issue(adm)
	require.NoError(t, err)
	step := t0.Add(-time.Minute)
	fs.st, fs.err = SessionState{Active: true, Roles: []string{"finance"}, StepUpAt: step}, nil
	p, err = a.Authenticate(ctx, rawAdm)
	require.NoError(t, err)
	require.Equal(t, []string{"finance"}, p.Roles)
	require.Equal(t, step, p.StepUpAt)
	require.Equal(t, AMROTPTOTP, p.AMR)

	// An otp+totp token whose sign-in holds no TOTP verification (an admin
	// whose TOTP was reset) is only an OTP session: the row is the truth.
	fs.st = SessionState{Active: true, Roles: []string{"finance"}}
	p, err = a.Authenticate(ctx, rawAdm)
	require.NoError(t, err)
	require.Equal(t, AMROTP, p.AMR)
	require.True(t, p.StepUpAt.IsZero())
}

func TestCheckRefreshOrigin(t *testing.T) {
	allowed := []string{"https://app.todayz.in", "https://business.todayz.in", "https://admin.todayz.in"}
	require.NoError(t, CheckRefreshOrigin(AppCustomer, "https://app.todayz.in", "web", allowed))
	require.NoError(t, CheckRefreshOrigin(AppBusiness, "https://business.todayz.in", "pos", allowed))
	require.NoError(t, CheckRefreshOrigin(AppAdmin, "https://admin.todayz.in", "web", allowed))
	for name, c := range map[string][3]string{
		"no client header":   {"customer", "https://app.todayz.in", " "},
		"no origin":          {"customer", "", "web"},
		"foreign origin":     {"customer", "https://evil.example", "web"},
		"another app origin": {"customer", "https://business.todayz.in", "web"},
		"admin from app":     {"admin", "https://app.todayz.in", "web"},
		"null origin":        {"business", "null", "web"},
		"unknown app":        {"storefront", "https://app.todayz.in", "web"},
	} {
		require.ErrorIs(t, CheckRefreshOrigin(App(c[0]), c[1], c[2], allowed), ErrBadOrigin, name)
	}
}

func TestAppsAndPrincipal(t *testing.T) {
	names := map[string]bool{}
	for _, a := range Apps {
		require.True(t, a.Valid())
		require.True(t, strings.HasPrefix(a.CookieName(), "__Secure-"), "D-14: the cookie is __Secure-")
		names[a.CookieName()] = true
		require.NotEqual(t, "anonymous", a.Kind())
		require.Positive(t, a.RefreshTTL())
	}
	require.Len(t, names, 3, "one cookie per app")
	require.Equal(t, AdminRefreshTTL, AppAdmin.RefreshTTL())
	require.Equal(t, RefreshTTL, AppCustomer.RefreshTTL())
	bad := App("storefront")
	require.False(t, bad.Valid())
	require.Empty(t, bad.CookieName())
	require.Equal(t, "anonymous", bad.Kind())
	_, ok := ParseApp("Customer")
	require.False(t, ok)

	p := principal(AppAdmin)
	require.True(t, p.HasRole("support"))
	require.False(t, p.HasRole("super_admin"))

	ctx := WithPrincipal(context.Background(), p)
	got, ok := FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, p.UserID, got.UserID)
	require.Equal(t, reqctx.Principal{Kind: "admin", UserID: p.UserID.String()}, reqctx.PrincipalFrom(ctx))

	ctx = WithPrincipal(context.Background(), Principal{})
	_, ok = FromContext(ctx)
	require.False(t, ok)
	require.Equal(t, reqctx.Anonymous, reqctx.PrincipalFrom(ctx))
	_, ok = FromContext(context.Background())
	require.False(t, ok)
}

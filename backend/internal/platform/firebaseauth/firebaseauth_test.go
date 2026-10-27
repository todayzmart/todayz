package firebaseauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const project = "todayz-test"

type fixture struct {
	key     *rsa.PrivateKey
	kid     string
	now     time.Time
	fetches atomic.Int32
	srv     *httptest.Server
	v       *Verifier
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{key: key, kid: "k1", now: time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.fetches.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=600, must-revalidate")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": f.kid, "kty": "RSA", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(f.srv.Close)
	f.v = New(project, f.srv.URL, f.srv.Client(), func() time.Time { return f.now }, 5*time.Minute)
	return f
}

func (f *fixture) claims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss":          "https://securetoken.google.com/" + project,
		"aud":          project,
		"sub":          "uid-123",
		"iat":          f.now.Add(-time.Minute).Unix(),
		"exp":          f.now.Add(59 * time.Minute).Unix(),
		"auth_time":    f.now.Add(-time.Minute).Unix(),
		"phone_number": "+919000000001",
		"firebase":     map[string]any{"sign_in_provider": "phone", "identities": map[string]any{}},
	}
}

func (f *fixture) sign(t *testing.T, c jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
	tok.Header["kid"] = f.kid
	s, err := tok.SignedString(f.key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVerifyGoodToken(t *testing.T) {
	f := newFixture(t)
	got, err := f.v.Verify(context.Background(), f.sign(t, f.claims()))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.UID != "uid-123" || got.Phone != "+919000000001" || !got.AuthTime.Equal(f.now.Add(-time.Minute)) {
		t.Fatalf("claims = %+v", got)
	}
}

func TestVerifyRefuses(t *testing.T) {
	cases := []struct {
		name string
		edit func(f *fixture, c jwt.MapClaims)
		want error
	}{
		{"wrong audience", func(_ *fixture, c jwt.MapClaims) { c["aud"] = "other-project" }, ErrInvalidToken},
		{"wrong issuer", func(_ *fixture, c jwt.MapClaims) { c["iss"] = "https://securetoken.google.com/other-project" }, ErrInvalidToken},
		{"two audiences", func(_ *fixture, c jwt.MapClaims) { c["aud"] = []string{project, "other"} }, ErrInvalidToken},
		{"expired", func(f *fixture, c jwt.MapClaims) { c["exp"] = f.now.Add(-time.Second).Unix() }, ErrInvalidToken},
		{"no exp", func(_ *fixture, c jwt.MapClaims) { delete(c, "exp") }, ErrInvalidToken},
		{"no iat", func(_ *fixture, c jwt.MapClaims) { delete(c, "iat") }, ErrInvalidToken},
		{"iat in the future", func(f *fixture, c jwt.MapClaims) { c["iat"] = f.now.Add(5 * time.Minute).Unix() }, ErrInvalidToken},
		{"no subject", func(_ *fixture, c jwt.MapClaims) { delete(c, "sub") }, ErrInvalidToken},
		{"missing phone", func(_ *fixture, c jwt.MapClaims) { delete(c, "phone_number") }, ErrInvalidToken},
		{"not a phone sign-in", func(_ *fixture, c jwt.MapClaims) {
			c["firebase"] = map[string]any{"sign_in_provider": "anonymous"}
		}, ErrInvalidToken},
		{"no auth_time", func(_ *fixture, c jwt.MapClaims) { delete(c, "auth_time") }, ErrInvalidToken},
		{"auth_time in the future", func(f *fixture, c jwt.MapClaims) {
			c["auth_time"] = f.now.Add(5 * time.Minute).Unix()
		}, ErrInvalidToken},
		{"sign-in too old", func(f *fixture, c jwt.MapClaims) {
			c["auth_time"] = f.now.Add(-6 * time.Minute).Unix()
		}, ErrStale},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			c := f.claims()
			tc.edit(f, c)
			_, err := f.v.Verify(context.Background(), f.sign(t, c))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestVerifyRefusesBadSignatures(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, f.claims())
	tok.Header["kid"] = f.kid
	forged, _ := tok.SignedString(other)
	if _, err := f.v.Verify(ctx, forged); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("other key: err = %v", err)
	}

	tok = jwt.NewWithClaims(jwt.SigningMethodRS256, f.claims())
	tok.Header["kid"] = "unknown"
	unknown, _ := tok.SignedString(f.key)
	if _, err := f.v.Verify(ctx, unknown); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unknown kid: err = %v", err)
	}

	tok = jwt.NewWithClaims(jwt.SigningMethodRS256, f.claims())
	noKid, _ := tok.SignedString(f.key)
	if _, err := f.v.Verify(ctx, noKid); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("no kid: err = %v", err)
	}

	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, f.claims())
	hs.Header["kid"] = f.kid
	hmac, _ := hs.SignedString([]byte("secret"))
	if _, err := f.v.Verify(ctx, hmac); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("HS256: err = %v", err)
	}

	none := jwt.NewWithClaims(jwt.SigningMethodNone, f.claims())
	none.Header["kid"] = f.kid
	unsigned, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, err := f.v.Verify(ctx, unsigned); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("alg none: err = %v", err)
	}

	for _, raw := range []string{"", "not.a.jwt", string(make([]byte, MaxTokenLen+1))} {
		if _, err := f.v.Verify(ctx, raw); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("garbage: err = %v", err)
		}
	}
}

func TestKeysAreCached(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for range 3 {
		if _, err := f.v.Verify(ctx, f.sign(t, f.claims())); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.fetches.Load(); n != 1 {
		t.Fatalf("fetches = %d, want 1", n)
	}

	// An unknown kid refetches, but at most once a minute.
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, f.claims())
	tok.Header["kid"] = "rotated"
	unknown, _ := tok.SignedString(f.key)
	for range 3 {
		_, _ = f.v.Verify(ctx, unknown)
	}
	if n := f.fetches.Load(); n != 1 {
		t.Fatalf("fetches after unknown kid within a minute = %d, want 1", n)
	}
	f.now = f.now.Add(2 * time.Minute)
	_, _ = f.v.Verify(ctx, unknown)
	if n := f.fetches.Load(); n != 2 {
		t.Fatalf("fetches after a minute = %d, want 2", n)
	}

	// Past max-age (600 s) the keys are fetched again.
	f.now = f.now.Add(11 * time.Minute)
	c := f.claims()
	if _, err := f.v.Verify(ctx, f.sign(t, c)); err != nil {
		t.Fatal(err)
	}
	if n := f.fetches.Load(); n != 3 {
		t.Fatalf("fetches after max-age = %d, want 3", n)
	}
}

func TestKeysUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	v := New(project, srv.URL, srv.Client(), func() time.Time { return now }, 5*time.Minute)
	f := newFixture(t)
	f.now = now
	_, err := v.Verify(context.Background(), f.sign(t, f.claims()))
	if !errors.Is(err, ErrKeys) {
		t.Fatalf("err = %v, want ErrKeys", err)
	}
}

func TestMaxAge(t *testing.T) {
	cases := map[string]time.Duration{
		"":                                       defaultKeyTTL,
		"public, max-age=19302, must-revalidate": 19302 * time.Second,
		"max-age=5":                              minKeyRefetch,
		"max-age=999999":                         maxKeyTTL,
		"max-age=x":                              defaultKeyTTL,
		"no-cache":                               defaultKeyTTL,
	}
	for in, want := range cases {
		if got := maxAge(in); got != want {
			t.Errorf("maxAge(%q) = %v, want %v", in, got, want)
		}
	}
}

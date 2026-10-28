package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
)

const firebaseProject = "todayz-http-test"

// firebaseKeys stands in for Google's JWK URL and signs ID tokens as Firebase
// would after a phone sign-in.
type firebaseKeys struct {
	key *rsa.PrivateKey
	srv *httptest.Server
}

func newFirebaseKeys(t *testing.T) *firebaseKeys {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	k := &firebaseKeys{key: key}
	k.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "k1", "kty": "RSA", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(k.srv.Close)
	return k
}

func (k *firebaseKeys) env() map[string]string {
	return map[string]string{
		"OTP_PROVIDER":          "firebase",
		"FIREBASE_PROJECT_ID":   firebaseProject,
		"FIREBASE_WEB_API_KEY":  "test-web-key",
		"FIREBASE_AUTH_DOMAIN":  firebaseProject + ".firebaseapp.com",
		"FIREBASE_APP_ID":       "1:000000000000:web:0000000000000000",
		"FIREBASE_JWKS_API_URL": k.srv.URL,
	}
}

func (k *firebaseKeys) token(t *testing.T, now time.Time, uid string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": "https://securetoken.google.com/" + firebaseProject, "aud": firebaseProject,
		"sub": uid, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "auth_time": now.Unix(),
		"phone_number": testPhone, "firebase": map[string]any{"sign_in_provider": "phone"},
	})
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(k.key)
	require.NoError(t, err)
	return s
}

// D-275: GET /v1/auth/config says who sends the code; with sms the apps never
// see a Firebase config and firebase/verify is 409 sign_in_method_changed.
func TestAuthConfigSMS(t *testing.T) {
	a := newIdentityAPI(t)
	w := a.do(http.MethodGet, "/v1/auth/config", "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	got := decode(t, w)
	require.Equal(t, "sms", got["otpProvider"])
	require.NotContains(t, got, "firebase")

	body := `{"app":"customer","idToken":"x.y.z","deviceId":"` + ids.New(a.clk.Now()).String() + `"}`
	w = a.do(http.MethodPost, "/v1/auth/firebase/verify", body, nil)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Equal(t, "sign_in_method_changed", decode(t, w)["code"])
}

// D-275: with OTP_PROVIDER=firebase the page signs in to Firebase and swaps
// the ID token for the normal TodayZ session and refresh cookie, once.
func TestFirebaseVerifyHTTP(t *testing.T) {
	keys := newFirebaseKeys(t)
	a := newIdentityAPIEnv(t, keys.env())

	w := a.do(http.MethodGet, "/v1/auth/config", "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	got := decode(t, w)
	require.Equal(t, "firebase", got["otpProvider"])
	fb, ok := got["firebase"].(map[string]any)
	require.True(t, ok, w.Body.String())
	require.Equal(t, firebaseProject, fb["projectId"])
	require.Equal(t, "test-web-key", fb["apiKey"])
	require.NotContains(t, fb, "emulatorUrl")

	// The SMS sign-in is closed while Firebase sends the codes.
	w = a.do(http.MethodPost, "/v1/auth/otp/send", `{"phone":"`+testPhone+`","app":"customer","purpose":"login","turnstileToken":"XXXX.DUMMY.TOKEN.XXXX"}`, nil)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Equal(t, "sign_in_method_changed", decode(t, w)["code"])

	idToken := keys.token(t, a.clk.Now(), "uid-http-1")
	body := `{"app":"customer","idToken":"` + idToken + `","deviceId":"` + ids.New(a.clk.Now()).String() + `"}`
	w = a.do(http.MethodPost, "/v1/auth/firebase/verify", body, map[string]string{"User-Agent": "identity-http-test"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	out := decode(t, w)
	require.Equal(t, "Bearer", out["tokenType"])
	require.NotEmpty(t, out["accessToken"])
	require.NotContains(t, w.Body.String(), "refresh", "the refresh token travels only in the cookie")
	require.Equal(t, cookieName, refreshCookieOf(t, w).Name)

	// The same verification signs in once.
	w = a.do(http.MethodPost, "/v1/auth/firebase/verify", body, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Equal(t, "otp_expired", decode(t, w)["code"])
	require.Empty(t, w.Result().Cookies())

	// A forged token is plain unauthorized.
	bad := `{"app":"customer","idToken":"a.b.c","deviceId":"` + ids.New(a.clk.Now()).String() + `"}`
	w = a.do(http.MethodPost, "/v1/auth/firebase/verify", bad, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Equal(t, "unauthorized", decode(t, w)["code"])
}

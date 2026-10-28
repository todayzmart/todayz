package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/firebaseauth"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/ids"
)

// D-275 live check, opt-in: run by hand against a real Firebase test project
// (docs/runbooks/firebase-phone-auth.md, "Live check"). It needs
// TODAYZ_FIREBASE_LIVE_TOKEN_FILE (a file holding an ID token Google issued
// for a test phone number less than 5 minutes ago) and FIREBASE_PROJECT_ID.
// It calls Google, so make check never sets the variable. The token and the
// phone are never printed.
func TestFirebaseLive(t *testing.T) {
	path, project := os.Getenv("TODAYZ_FIREBASE_LIVE_TOKEN_FILE"), os.Getenv("FIREBASE_PROJECT_ID")
	if path == "" || project == "" {
		t.Skip("TODAYZ_FIREBASE_LIVE_TOKEN_FILE and FIREBASE_PROJECT_ID unset")
	}
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	idToken := strings.TrimSpace(string(raw))
	client := httpx.NewOutboundClient(httpx.OutboundOptions{Timeout: 10 * time.Second})

	// TodayZ's verifier against Google's real keys, as in prod.
	v := firebaseauth.New(project, "", client, time.Now, identity.FirebaseMaxAuthAge)
	c, err := v.Verify(context.Background(), idToken)
	require.NoError(t, err)
	require.NotEmpty(t, c.UID)
	require.NotEmpty(t, c.Phone)
	t.Logf("verify: ok, signed in %s ago", time.Since(c.AuthTime).Round(time.Second))

	// The whole swap through the API. APP_ENV=test refuses egress, so a
	// loopback server hands on Google's real keys unchanged.
	keys := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, firebaseauth.GoogleJWKSURL, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		res, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer res.Body.Close()
		w.Header().Set("Cache-Control", res.Header.Get("Cache-Control"))
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	}))
	t.Cleanup(keys.Close)

	a := newIdentityAPIEnv(t, map[string]string{
		"OTP_PROVIDER":          "firebase",
		"FIREBASE_PROJECT_ID":   project,
		"FIREBASE_WEB_API_KEY":  "live-test",
		"FIREBASE_AUTH_DOMAIN":  project + ".firebaseapp.com",
		"FIREBASE_APP_ID":       "1:000000000000:web:0000000000000000",
		"FIREBASE_JWKS_API_URL": keys.URL,
	})
	a.clk.Set(time.Now())

	body := `{"app":"customer","idToken":"` + idToken + `","deviceId":"` + ids.New(a.clk.Now()).String() + `"}`
	w := a.do(http.MethodPost, "/v1/auth/firebase/verify", body, map[string]string{"User-Agent": "firebase-live-test"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotEmpty(t, decode(t, w)["accessToken"])
	require.Equal(t, cookieName, refreshCookieOf(t, w).Name)
	t.Log("swap: 200, TodayZ session and refresh cookie")

	w = a.do(http.MethodPost, "/v1/auth/firebase/verify", body, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Equal(t, "otp_expired", decode(t, w)["code"])
	t.Log("replay: 401 otp_expired")
}

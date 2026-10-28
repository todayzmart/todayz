package server

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	fbBase  = "/identitytoolkit.googleapis.com"
	fbPhone = "+919000000001"
)

func fbSend(h *harness, phone string) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.postJSON(fbBase+"/v1/accounts:sendVerificationCode?key=test-key",
		map[string]string{"phoneNumber": phone, "recaptchaToken": "fake-token", "clientType": "CLIENT_TYPE_WEB"}, nil)
}

func fbSignIn(h *harness, session, code string) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.postJSON(fbBase+"/v1/accounts:signInWithPhoneNumber?key=test-key",
		map[string]string{"sessionInfo": session, "code": code}, nil)
}

func fbErrorMessage(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	return obj(t, decode(t, rr)["error"])["message"].(string)
}

// fbVerify checks an ID token against /firebase/jwks the way the API's
// firebaseauth verifier does (RS256, kid lookup) and returns its claims.
func fbVerify(t *testing.T, h *harness, token string) map[string]any {
	t.Helper()
	rr := h.get("/firebase/jwks")
	wantStatus(t, rr, http.StatusOK)
	if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age=") {
		t.Errorf("jwks Cache-Control = %q", cc)
	}
	var jwks struct {
		Keys []struct{ Kid, Kty, Alg, N, E string }
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &jwks); err != nil || len(jwks.Keys) != 1 {
		t.Fatalf("jwks = %s", rr.Body.String())
	}
	k := jwks.Keys[0]
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts", len(parts))
	}
	var header map[string]string
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	_ = json.Unmarshal(hb, &header)
	if header["alg"] != "RS256" || header["kid"] != k.Kid || k.Kty != "RSA" {
		t.Fatalf("header %v, key kid %s kty %s", header, k.Kid, k.Kty)
	}
	n, _ := base64.RawURLEncoding.DecodeString(k.N)
	e, _ := base64.RawURLEncoding.DecodeString(k.E)
	pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature: %v", err)
	}
	var claims map[string]any
	pb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(pb, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

func TestFirebasePhoneSignIn(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Now = func() time.Time { return fixedNow } })

	rr := h.get(fbBase + "/v2/recaptchaConfig?key=test-key&clientType=CLIENT_TYPE_WEB&version=RECAPTCHA_ENTERPRISE")
	wantStatus(t, rr, http.StatusOK)
	wantKeys(t, decode(t, rr), "recaptchaEnforcementState", "recaptchaKey")

	rr = fbSend(h, fbPhone)
	wantStatus(t, rr, http.StatusOK)
	session, _ := decode(t, rr)["sessionInfo"].(string)
	if session == "" {
		t.Fatal("no sessionInfo")
	}

	// The code is captured like an SMS.
	rr = h.get("/_mock/sms/latest-otp?to=9000000001")
	wantStatus(t, rr, http.StatusOK)
	code, _ := decode(t, rr)["otp"].(string)
	if len(code) != 6 {
		t.Fatalf("captured otp = %q", code)
	}

	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	rr = fbSignIn(h, session, wrong)
	wantStatus(t, rr, http.StatusBadRequest)
	if m := fbErrorMessage(t, rr); m != "INVALID_CODE" {
		t.Errorf("wrong code message = %q", m)
	}

	// A wrong code keeps the session: the right one still signs in.
	rr = fbSignIn(h, session, code)
	wantStatus(t, rr, http.StatusOK)
	got := decode(t, rr)
	wantKeys(t, got, "expiresIn", "idToken", "isNewUser", "localId", "phoneNumber", "refreshToken")
	if got["isNewUser"] != true || got["phoneNumber"] != fbPhone {
		t.Errorf("isNewUser %v, phoneNumber matches %v", got["isNewUser"], got["phoneNumber"] == fbPhone)
	}
	token := got["idToken"].(string)
	claims := fbVerify(t, h, token)
	want := map[string]any{
		"iss": "https://securetoken.google.com/" + DefaultFirebaseProjectID, "aud": DefaultFirebaseProjectID,
		"sub": got["localId"], "user_id": got["localId"], "phone_number": fbPhone,
		"iat": float64(fixedNow.Unix()), "auth_time": float64(fixedNow.Unix()), "exp": float64(fixedNow.Add(time.Hour).Unix()),
	}
	for k, v := range want {
		if claims[k] != v {
			t.Errorf("claim %s = %v, want %v", k, claims[k], v)
		}
	}
	if fb := obj(t, claims["firebase"]); fb["sign_in_provider"] != "phone" {
		t.Errorf("sign_in_provider = %v", fb["sign_in_provider"])
	}

	// Each code signs in once.
	rr = fbSignIn(h, session, code)
	wantStatus(t, rr, http.StatusBadRequest)
	if m := fbErrorMessage(t, rr); m != "INVALID_SESSION_INFO" {
		t.Errorf("reused session message = %q", m)
	}

	// The SDK looks the user up right after sign-in.
	rr = h.postJSON(fbBase+"/v1/accounts:lookup?key=test-key", map[string]string{"idToken": token}, nil)
	wantStatus(t, rr, http.StatusOK)
	users, _ := decode(t, rr)["users"].([]any)
	if len(users) != 1 || obj(t, users[0])["localId"] != got["localId"] {
		t.Errorf("lookup = %s", rr.Body.String())
	}
	rr = h.postJSON(fbBase+"/v1/accounts:lookup?key=test-key", map[string]string{"idToken": token[:len(token)-4] + "AAAA"}, nil)
	wantStatus(t, rr, http.StatusBadRequest)

	// Same number again: same uid, not new.
	rr = fbSend(h, fbPhone)
	session = decode(t, rr)["sessionInfo"].(string)
	code = decode(t, h.get("/_mock/sms/latest-otp?to=9000000001"))["otp"].(string)
	again := decode(t, fbSignIn(h, session, code))
	if again["localId"] != got["localId"] || again["isNewUser"] != false {
		t.Errorf("second sign-in: same uid %v, isNewUser %v", again["localId"] == got["localId"], again["isNewUser"])
	}
}

func TestFirebaseCodeExpires(t *testing.T) {
	now := fixedNow
	h := newHarness(t, func(c *Config) { c.Now = func() time.Time { return now } })
	session := decode(t, fbSend(h, fbPhone))["sessionInfo"].(string)
	code := decode(t, h.get("/_mock/sms/latest-otp?to=9000000001"))["otp"].(string)
	now = now.Add(firebaseCodeTTL + time.Second)
	rr := fbSignIn(h, session, code)
	wantStatus(t, rr, http.StatusBadRequest)
	if m := fbErrorMessage(t, rr); m != "SESSION_EXPIRED" {
		t.Errorf("expired message = %q", m)
	}
}

func TestFirebaseRejectsBadRequests(t *testing.T) {
	h := newHarness(t, nil)
	cases := []struct {
		name, path string
		body       any
		message    string
	}{
		{"no key", fbBase + "/v1/accounts:sendVerificationCode", map[string]string{"phoneNumber": fbPhone, "recaptchaToken": "x"}, "API key not valid"},
		{"no captcha", fbBase + "/v1/accounts:sendVerificationCode?key=k", map[string]string{"phoneNumber": fbPhone}, "MISSING_RECAPTCHA_TOKEN"},
		{"bad phone", fbBase + "/v1/accounts:sendVerificationCode?key=k", map[string]string{"phoneNumber": "9000000001", "recaptchaToken": "x"}, "INVALID_PHONE_NUMBER"},
		{"short indian phone", fbBase + "/v1/accounts:sendVerificationCode?key=k", map[string]string{"phoneNumber": "+91900000000", "recaptchaToken": "x"}, "INVALID_PHONE_NUMBER"},
		{"not json", fbBase + "/v1/accounts:sendVerificationCode?key=k", "{", "INVALID_ARGUMENT"},
		{"no code", fbBase + "/v1/accounts:signInWithPhoneNumber?key=k", map[string]string{"sessionInfo": "abc"}, "MISSING_CODE"},
		{"unknown session", fbBase + "/v1/accounts:signInWithPhoneNumber?key=k", map[string]string{"sessionInfo": "abc", "code": "123456"}, "INVALID_SESSION_INFO"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := h.postJSON(c.path, c.body, nil)
			wantStatus(t, rr, http.StatusBadRequest)
			if m := fbErrorMessage(t, rr); !strings.HasPrefix(m, c.message) {
				t.Errorf("message = %q, want prefix %q", m, c.message)
			}
		})
	}
	if n := len(h.srv.Store().SMS.List(nil, 0)); n != 0 {
		t.Errorf("%d codes captured from bad requests", n)
	}
}

func TestFirebaseCORS(t *testing.T) {
	h := newHarness(t, nil)
	preflight := func(path, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, path, nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "content-type,x-client-version,x-firebase-gmpid")
		return h.do(req)
	}

	for _, origin := range []string{"https://app.todayz.localhost", "https://business.todayz.localhost", "http://localhost:5301", "http://127.0.0.1:4173"} {
		rr := preflight(fbBase+"/v1/accounts:sendVerificationCode?key=k", origin)
		wantStatus(t, rr, http.StatusNoContent)
		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("%s: allow-origin = %q", origin, got)
		}
		if got := rr.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "x-firebase-gmpid") {
			t.Errorf("%s: allow-headers = %q", origin, got)
		}
	}

	for _, origin := range []string{"https://evil.example", "https://todayz.localhost.evil.example", "null"} {
		rr := preflight(fbBase+"/v1/accounts:sendVerificationCode?key=k", origin)
		wantStatus(t, rr, http.StatusForbidden)
		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s: allow-origin = %q", origin, got)
		}
	}

	// A real call from an app page gets the header; the captured codes never do.
	req := httptest.NewRequest(http.MethodPost, fbBase+"/v1/accounts:sendVerificationCode?key=k",
		strings.NewReader(`{"phoneNumber":"`+fbPhone+`","recaptchaToken":"x"}`))
	req.Header.Set("Origin", "https://app.todayz.localhost")
	rr := h.do(req)
	wantStatus(t, rr, http.StatusOK)
	if rr.Header().Get("Access-Control-Allow-Origin") != "https://app.todayz.localhost" {
		t.Error("send: no allow-origin")
	}
	req = httptest.NewRequest(http.MethodGet, "/_mock/sms/latest-otp", nil)
	req.Header.Set("Origin", "https://app.todayz.localhost")
	if got := h.do(req).Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("/_mock allow-origin = %q", got)
	}
}

func TestFirebaseLogsNoPhoneOrCode(t *testing.T) {
	h := newHarness(t, nil)
	session := decode(t, fbSend(h, fbPhone))["sessionInfo"].(string)
	code := decode(t, h.get("/_mock/sms/latest-otp?to=9000000001"))["otp"].(string)
	token := decode(t, fbSignIn(h, session, code))["idToken"].(string)
	logs := h.logs.String()
	for name, secret := range map[string]string{"phone": "9000000001", "code": code, "session": session, "token": token[:20]} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain the %s", name)
		}
	}
	if !strings.Contains(logs, "accounts:sendVerificationCode") {
		t.Error("logs miss the route template")
	}
}

func TestConfigFromEnvFirebaseProject(t *testing.T) {
	env := map[string]string{"FIREBASE_PROJECT_ID": " todayz-e2e "}
	if got := ConfigFromEnv(func(k string) string { return env[k] }).FirebaseProjectID; got != "todayz-e2e" {
		t.Errorf("FirebaseProjectID = %q", got)
	}
	if got := New(ConfigFromEnv(func(string) string { return "" })).cfg.FirebaseProjectID; got != DefaultFirebaseProjectID {
		t.Errorf("default FirebaseProjectID = %q", got)
	}
}

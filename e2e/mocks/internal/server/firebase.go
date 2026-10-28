package server

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"todayz.local/e2e/mocks/internal/capture"
)

// Firebase Auth phone sign-in (D-275). The apps' FirebaseWebConfig.emulatorUrl
// points the Firebase JS SDK here (connectAuthEmulator keeps only the origin),
// so the SDK calls <origin>/identitytoolkit.googleapis.com/...; in emulator
// mode it skips reCAPTCHA. Unlike Google's own emulator, which issues unsigned
// tokens, this one signs ID tokens with RS256 and publishes the key at
// /firebase/jwks, so the backend checks them exactly as in production
// (FIREBASE_JWKS_API_URL). The code is captured like an SMS: /_mock/sms and
// /_mock/sms/latest-otp show it.
//
// Served: GET v2/recaptchaConfig, POST v1/accounts:sendVerificationCode,
// v1/accounts:signInWithPhoneNumber and v1/accounts:lookup. Token refresh
// (securetoken.googleapis.com) is not: the apps read the ID token once, right
// after sign-in, and sign Firebase out.
const (
	firebaseIdentityPrefix = "/identitytoolkit.googleapis.com"
	// DefaultFirebaseProjectID matches FIREBASE_PROJECT_ID in infra/local/local.env.
	DefaultFirebaseProjectID = "todayz-local"
	firebaseCodeTTL          = 5 * time.Minute
	firebaseTokenTTL         = time.Hour
	firebaseMaxSessions      = 1000
	firebaseBodyLimit        = 16 << 10
)

// firebaseAuth is the emulator's state: the signing key and the codes sent.
type firebaseAuth struct {
	key *rsa.PrivateKey
	kid string

	mu       sync.Mutex
	sessions map[string]*firebaseSession // sessionInfo -> code
	known    map[string]bool             // uid -> signed in before (isNewUser)
}

type firebaseSession struct {
	phone   string
	code    string
	expires time.Time
}

func newFirebaseAuth() *firebaseAuth {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic("tz-mock: firebase key: " + err.Error())
	}
	return &firebaseAuth{key: key, kid: randomHex(8), sessions: map[string]*firebaseSession{}, known: map[string]bool{}}
}

// firebaseError writes Google's error shape; the SDK maps message (e.g.
// INVALID_CODE) to an auth/... code. It never repeats a request value.
func firebaseError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"code": status, "message": message,
		"errors": []map[string]string{{"message": message, "domain": "global", "reason": "invalid"}},
	}})
}

// firebaseCORS lets the app pages (https://*.todayz.localhost, or a Vite or
// Playwright port on localhost) call the identitytoolkit routes. It is never
// applied to /_mock, which holds the captured codes.
func firebaseCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !localOrigin(origin) {
				http.Error(w, "tz-mock: origin not allowed", http.StatusForbidden)
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			if req := r.Header.Get("Access-Control-Request-Headers"); req != "" {
				h.Set("Access-Control-Allow-Headers", req)
			}
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func localOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || strings.HasSuffix(h, ".todayz.localhost") || h == "todayz.localhost"
}

// firebaseKeyed refuses a call without ?key= (the web API key), as Google does.
func firebaseKeyed(r *http.Request) bool { return r.URL.Query().Get("key") != "" }

func (s *Server) firebaseRecaptchaConfig(w http.ResponseWriter, r *http.Request) {
	if !firebaseKeyed(r) {
		firebaseError(w, http.StatusBadRequest, "API key not valid. Please pass a valid API key.")
		return
	}
	// reCAPTCHA Enterprise off for every provider: the SDK uses its (mocked) v2 verifier.
	writeJSON(w, http.StatusOK, map[string]any{
		"recaptchaKey":              "projects/" + s.cfg.FirebaseProjectID + "/keys/tz-mock",
		"recaptchaEnforcementState": []any{},
	})
}

func (s *Server) firebaseSendVerificationCode(w http.ResponseWriter, r *http.Request) {
	if !firebaseKeyed(r) {
		firebaseError(w, http.StatusBadRequest, "API key not valid. Please pass a valid API key.")
		return
	}
	raw, status := readBody(w, r, firebaseBodyLimit)
	if status != 0 {
		firebaseError(w, status, "INVALID_ARGUMENT")
		return
	}
	var req struct {
		PhoneNumber     string `json:"phoneNumber"`
		RecaptchaToken  string `json:"recaptchaToken"`
		CaptchaResponse string `json:"captchaResponse"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		firebaseError(w, http.StatusBadRequest, "INVALID_ARGUMENT")
		return
	}
	// The SDK's emulator-mode verifier sends a placeholder; any value passes, none does not.
	if req.RecaptchaToken == "" && req.CaptchaResponse == "" {
		firebaseError(w, http.StatusBadRequest, "MISSING_RECAPTCHA_TOKEN")
		return
	}
	if !e164Re.MatchString(req.PhoneNumber) {
		firebaseError(w, http.StatusBadRequest, "INVALID_PHONE_NUMBER : Invalid format.")
		return
	}
	to, ok := ProviderPhone(req.PhoneNumber)
	if !ok {
		firebaseError(w, http.StatusBadRequest, "INVALID_PHONE_NUMBER : Invalid format.")
		return
	}
	code := fmt.Sprintf("%06d", randomInt(1_000_000))
	session := randomHex(24)
	now := s.cfg.Now()
	fb := s.firebase
	fb.mu.Lock()
	for k, v := range fb.sessions {
		if now.After(v.expires) {
			delete(fb.sessions, k)
		}
	}
	if len(fb.sessions) >= firebaseMaxSessions {
		fb.mu.Unlock()
		firebaseError(w, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS_TRY_LATER")
		return
	}
	fb.sessions[session] = &firebaseSession{phone: to, code: code, expires: now.Add(firebaseCodeTTL)}
	fb.mu.Unlock()

	body := code + " is your verification code for " + s.cfg.FirebaseProjectID + ".firebaseapp.com."
	s.store.SMS.Add(capture.SMS{
		Meta: s.store.Next(), API: "firebase_send_verification_code", To: to, Sender: "firebase",
		Variables: map[string]string{}, Body: body, OTP: code, RequestID: session[:16],
	})
	writeJSON(w, http.StatusOK, map[string]string{"sessionInfo": session})
}

func (s *Server) firebaseSignInWithPhoneNumber(w http.ResponseWriter, r *http.Request) {
	if !firebaseKeyed(r) {
		firebaseError(w, http.StatusBadRequest, "API key not valid. Please pass a valid API key.")
		return
	}
	raw, status := readBody(w, r, firebaseBodyLimit)
	if status != 0 {
		firebaseError(w, status, "INVALID_ARGUMENT")
		return
	}
	var req struct {
		SessionInfo string `json:"sessionInfo"`
		Code        string `json:"code"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		firebaseError(w, http.StatusBadRequest, "INVALID_ARGUMENT")
		return
	}
	if req.Code == "" {
		firebaseError(w, http.StatusBadRequest, "MISSING_CODE")
		return
	}
	now := s.cfg.Now()
	fb := s.firebase
	fb.mu.Lock()
	sess, ok := fb.sessions[req.SessionInfo]
	switch {
	case !ok:
		fb.mu.Unlock()
		firebaseError(w, http.StatusBadRequest, "INVALID_SESSION_INFO")
		return
	case now.After(sess.expires):
		delete(fb.sessions, req.SessionInfo)
		fb.mu.Unlock()
		firebaseError(w, http.StatusBadRequest, "SESSION_EXPIRED")
		return
	case req.Code != sess.code:
		fb.mu.Unlock()
		firebaseError(w, http.StatusBadRequest, "INVALID_CODE")
		return
	}
	delete(fb.sessions, req.SessionInfo)
	uid := s.firebaseUID(sess.phone)
	isNew := !fb.known[uid]
	fb.known[uid] = true
	fb.mu.Unlock()

	token, err := s.firebaseIDToken(uid, sess.phone, now)
	if err != nil {
		firebaseError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"idToken": token, "refreshToken": randomHex(32), "expiresIn": fmt.Sprint(int(firebaseTokenTTL.Seconds())),
		"localId": uid, "isNewUser": isNew, "phoneNumber": sess.phone,
	})
}

// firebaseLookup answers accounts:lookup for a token this mock signed.
func (s *Server) firebaseLookup(w http.ResponseWriter, r *http.Request) {
	if !firebaseKeyed(r) {
		firebaseError(w, http.StatusBadRequest, "API key not valid. Please pass a valid API key.")
		return
	}
	raw, status := readBody(w, r, firebaseBodyLimit)
	if status != 0 {
		firebaseError(w, status, "INVALID_ARGUMENT")
		return
	}
	var req struct {
		IDToken string `json:"idToken"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		firebaseError(w, http.StatusBadRequest, "INVALID_ARGUMENT")
		return
	}
	claims, ok := s.firebaseVerify(req.IDToken)
	if !ok {
		firebaseError(w, http.StatusBadRequest, "INVALID_ID_TOKEN")
		return
	}
	ms := fmt.Sprint(int64(claims.AuthTime) * 1000)
	writeJSON(w, http.StatusOK, map[string]any{"users": []map[string]any{{
		"localId": claims.Subject, "phoneNumber": claims.Phone,
		"providerUserInfo": []map[string]string{{"providerId": "phone", "rawId": claims.Phone, "phoneNumber": claims.Phone}},
		"lastLoginAt":      ms, "createdAt": ms, "lastRefreshAt": time.Unix(claims.AuthTime, 0).UTC().Format(time.RFC3339),
	}}})
}

// firebaseJWKS is GET /firebase/jwks, the stand-in for Google's
// securetoken JWK URL.
func (s *Server) firebaseJWKS(w http.ResponseWriter, _ *http.Request) {
	pub := s.firebase.key.PublicKey
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
		"kid": s.firebase.kid, "kty": "RSA", "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

// firebaseUID is a stable uid per project and number (28 characters, like Firebase's).
func (s *Server) firebaseUID(phone string) string {
	sum := sha256.Sum256([]byte(s.cfg.FirebaseProjectID + "|" + phone))
	return hex.EncodeToString(sum[:])[:28]
}

type firebaseClaims struct {
	Issuer   string `json:"iss"`
	Audience string `json:"aud"`
	AuthTime int64  `json:"auth_time"`
	UserID   string `json:"user_id"`
	Subject  string `json:"sub"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
	Phone    string `json:"phone_number"`
	Firebase struct {
		Identities     map[string][]string `json:"identities"`
		SignInProvider string              `json:"sign_in_provider"`
	} `json:"firebase"`
}

func (s *Server) firebaseIDToken(uid, phone string, now time.Time) (string, error) {
	c := firebaseClaims{
		Issuer: "https://securetoken.google.com/" + s.cfg.FirebaseProjectID, Audience: s.cfg.FirebaseProjectID,
		AuthTime: now.Unix(), UserID: uid, Subject: uid, IssuedAt: now.Unix(),
		Expires: now.Add(firebaseTokenTTL).Unix(), Phone: phone,
	}
	c.Firebase.Identities = map[string][]string{"phone": {phone}}
	c.Firebase.SignInProvider = "phone"
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": s.firebase.kid, "typ": "JWT"})
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.firebase.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// firebaseVerify checks a token this mock signed (signature and expiry only).
func (s *Server) firebaseVerify(raw string) (firebaseClaims, bool) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return firebaseClaims{}, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return firebaseClaims{}, false
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(&s.firebase.key.PublicKey, crypto.SHA256, sum[:], sig) != nil {
		return firebaseClaims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return firebaseClaims{}, false
	}
	var c firebaseClaims
	if json.Unmarshal(payload, &c) != nil || s.cfg.Now().Unix() >= c.Expires {
		return firebaseClaims{}, false
	}
	return c, true
}

func randomInt(n int64) int64 {
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		panic("tz-mock: random: " + err.Error())
	}
	return v.Int64()
}

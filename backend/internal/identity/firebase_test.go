package identity

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

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/firebaseauth"
	"github.com/todayz/backend/internal/platform/ids"
)

const firebaseProject = "todayz-test"

// firebaseWorld is a fixture with OTP_PROVIDER=firebase and a JWK server
// standing in for Google: tokens it signs go through the real
// firebaseauth.Verifier on the fixture's clock.
type firebaseWorld struct {
	*fixture
	key *rsa.PrivateKey
}

func newFirebaseFixture(t *testing.T, edit func(*config.Config)) *firebaseWorld {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "fb1", "kty": "RSA", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	f := newFixtureWith(t, config.EnvTest, func(cfg *config.Config, d *Deps) {
		cfg.Providers.OTPProvider = config.OTPProviderFirebase
		cfg.Providers.Firebase.ProjectID = firebaseProject
		if edit != nil {
			edit(cfg)
		}
		clk := d.Clock
		d.Firebase = firebaseauth.New(firebaseProject, srv.URL, srv.Client(), func() time.Time { return clk.Now() }, FirebaseMaxAuthAge)
	})
	return &firebaseWorld{fixture: f, key: key}
}

// token signs a Firebase phone sign-in for phone that happened authAgo ago
// (the token itself issued now), with uid "uid-"+phone.
func (w *firebaseWorld) token(phone string, authAgo time.Duration, edit func(jwt.MapClaims)) string {
	w.t.Helper()
	now := w.clk.Now()
	c := jwt.MapClaims{
		"iss": "https://securetoken.google.com/" + firebaseProject, "aud": firebaseProject,
		"sub": "uid-" + phone, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"auth_time": now.Add(-authAgo).Unix(), "phone_number": phone,
		"firebase": map[string]any{"sign_in_provider": "phone"},
	}
	if edit != nil {
		edit(c)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
	tok.Header["kid"] = "fb1"
	s, err := tok.SignedString(w.key)
	require.NoError(w.t, err)
	w.remember(phone, s)
	return s
}

func (w *firebaseWorld) swap(app authn.App, token string) (SignIn, error) {
	out, err := w.svc.VerifyFirebase(w.ctx("10.0.0.1"), FirebaseInput{
		App: app, IDToken: token, DeviceID: ids.New(t0).String(), UserAgent: "test",
	})
	w.remember(out.AccessToken, out.RefreshToken)
	return out, err
}

// D-275: a Firebase phone sign-in becomes the same TodayZ user and session
// an SMS code would, once per verification.
func TestFirebaseSignIn(t *testing.T) {
	w := newFirebaseFixture(t, nil)
	const phone = "+919876543210"

	tok := w.token(phone, 10*time.Second, nil)
	in, err := w.swap(authn.AppCustomer, tok)
	require.NoError(t, err)
	require.True(t, in.IsNewUser)
	p, err := w.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)
	require.Equal(t, in.UserID, p.UserID)
	require.Equal(t, authn.AMROTP, p.AMR)
	require.Equal(t, 1, w.count(`SELECT count(*) FROM sessions WHERE app = 'customer' AND revoked_at IS NULL`))
	require.Equal(t, 1, w.count(`SELECT count(*) FROM customer_profiles`))
	require.Equal(t, []string{"firebase"}, w.strings(`SELECT after->>'method' FROM audit_log WHERE action = 'identity.sign_in'`))
	require.Zero(t, w.count(`SELECT count(*) FROM otp_requests`), "no TodayZ code is made")

	// Replay: the same token, or a refreshed token of the same verification
	// (same auth_time, new iat), signs in only once.
	_, err = w.swap(authn.AppCustomer, tok)
	requireCode(t, err, genproblems.OtpExpired, "the same token twice")
	w.clk.Advance(time.Minute)
	_, err = w.swap(authn.AppCustomer, w.token(phone, 70*time.Second, nil))
	requireCode(t, err, genproblems.OtpExpired, "a refreshed token of the used verification")
	require.Equal(t, 1, w.count(`SELECT count(*) FROM sessions`))

	// A new verification of the same number is the same user.
	again, err := w.swap(authn.AppCustomer, w.token(phone, time.Second, nil))
	require.NoError(t, err)
	require.False(t, again.IsNewUser)
	require.Equal(t, in.UserID, again.UserID)
	require.Equal(t, 1, w.count(`SELECT count(*) FROM users`))

	// The same number signed in by SMS before is the same user too.
	require.Equal(t, 1, w.count(`SELECT count(*) FROM phone_identities`))
}

func TestFirebaseRefusals(t *testing.T) {
	w := newFirebaseFixture(t, nil)
	const phone = "+919876543211"
	cases := []struct {
		name  string
		token string
		code  string
	}{
		{"wrong project", w.token(phone, time.Second, func(c jwt.MapClaims) {
			c["aud"], c["iss"] = "other", "https://securetoken.google.com/other"
		}), genproblems.Unauthorized},
		{"expired", w.token(phone, time.Second, func(c jwt.MapClaims) { c["exp"] = w.clk.Now().Add(-time.Second).Unix() }), genproblems.Unauthorized},
		{"missing phone", w.token(phone, time.Second, func(c jwt.MapClaims) { delete(c, "phone_number") }), genproblems.Unauthorized},
		{"not a phone sign-in", w.token(phone, time.Second, func(c jwt.MapClaims) {
			c["firebase"] = map[string]any{"sign_in_provider": "password"}
		}), genproblems.Unauthorized},
		{"sign-in older than 5 minutes", w.token(phone, FirebaseMaxAuthAge+time.Second, nil), genproblems.OtpExpired},
		{"not an Indian mobile", w.token("+14155550100", time.Second, nil), genproblems.Forbidden},
		{"garbage", "not-a-token", genproblems.Unauthorized},
	}
	for _, tc := range cases {
		_, err := w.swap(authn.AppCustomer, tc.token)
		requireCode(t, err, tc.code, tc.name)
	}
	require.Zero(t, w.count(`SELECT count(*) FROM users`))
	require.Zero(t, w.count(`SELECT count(*) FROM firebase_sign_ins`), "a refused token uses nothing")

	_, err := w.svc.VerifyFirebase(w.ctx("10.0.0.1"), FirebaseInput{App: authn.AppCustomer, IDToken: w.token(phone, time.Second, nil), DeviceID: "nope"})
	requireCode(t, err, genproblems.ValidationFailed)

	// A blocked account is refused, and the verification is used.
	in, err := w.swap(authn.AppCustomer, w.token(phone, time.Second, nil))
	require.NoError(t, err)
	w.owner(`UPDATE users SET status = 'blocked' WHERE id = $1`, ids.UUID(in.UserID))
	w.clk.Advance(time.Second)
	_, err = w.swap(authn.AppCustomer, w.token(phone, 0, nil))
	requireCode(t, err, genproblems.AccountBlocked)
}

// The provider is a deploy setting: each path answers 409 while the other
// is on, so an app with a stale GET /v1/auth/config reads it again.
func TestOTPProviderGate(t *testing.T) {
	w := newFirebaseFixture(t, nil)
	const phone = "+919876543212"
	_, err := w.send(phone, authn.AppCustomer, "10.0.0.1")
	requireCode(t, err, genproblems.SignInMethodChanged, "Firebase sends the sign-in code")
	require.Zero(t, w.sms.sent)
	_, err = w.svc.SendOTP(w.ctx("10.0.0.1"), authn.AppCustomer, phone, PurposeStepUp, "tok")
	require.NoError(t, err, "a customer step-up code is still ours")
	require.Equal(t, 1, w.sms.sent)

	f := newFixture(t, config.EnvTest)
	require.Equal(t, config.OTPProviderSMS, f.svc.OTPProvider())
	_, err = f.svc.VerifyFirebase(f.ctx("10.0.0.1"), FirebaseInput{App: authn.AppCustomer, IDToken: "x", DeviceID: ids.New(t0).String()})
	requireCode(t, err, genproblems.SignInMethodChanged)

	// firebase without a project id (Preflight refuses it deployed) is 503.
	g := newFixtureWith(t, config.EnvTest, func(cfg *config.Config, _ *Deps) { cfg.Providers.OTPProvider = config.OTPProviderFirebase })
	_, err = g.svc.VerifyFirebase(g.ctx("10.0.0.1"), FirebaseInput{App: authn.AppCustomer, IDToken: "x", DeviceID: ids.New(t0).String()})
	requireCode(t, err, genproblems.ServiceUnavailable)
}

// D-264 holds on the Firebase path: only an active admin signs in to the
// admin app, a locked number stays locked, and a good sign-in clears the
// wrong-code count.
func TestFirebaseAdminSignIn(t *testing.T) {
	w := newFirebaseFixture(t, nil)
	const phone = "+919876543213"

	_, err := w.swap(authn.AppAdmin, w.token(phone, time.Second, nil))
	requireCode(t, err, genproblems.Forbidden, "an unknown number never becomes an admin")
	require.Zero(t, w.count(`SELECT count(*) FROM users`))

	w.clk.Advance(time.Second)
	cust, err := w.swap(authn.AppCustomer, w.token(phone, 0, nil))
	require.NoError(t, err)
	w.owner(`INSERT INTO admin_users (user_id, roles) VALUES ($1, ARRAY['support'])`, ids.UUID(cust.UserID))

	hash := w.svc.hash.Phone(phone)
	now := w.clk.Now()
	w.owner(`INSERT INTO admin_signin_locks (phone_hash, locked_at, locked_until) VALUES ($1, $2, $3)`, hash, now, now.Add(time.Hour))
	w.clk.Advance(time.Second)
	_, err = w.swap(authn.AppAdmin, w.token(phone, 0, nil))
	requireLocked(t, err, time.Hour-time.Second, "a locked number")

	w.owner(`DELETE FROM admin_signin_locks`)
	w.owner(`INSERT INTO admin_signin_failures (id, phone_hash, failed_at) VALUES ($1, $2, $3)`, ids.UUID(ids.New(now)), hash, now)
	w.clk.Advance(time.Second)
	adm, err := w.swap(authn.AppAdmin, w.token(phone, 0, nil))
	require.NoError(t, err)
	require.Equal(t, cust.UserID, adm.UserID)
	require.Zero(t, w.failures(phone), "a good sign-in clears the count")
	p, err := w.svc.tokens.Verify(adm.AccessToken)
	require.NoError(t, err)
	require.Equal(t, []string{"support"}, p.Roles)
	st, err := w.svc.CheckSession(w.ctx("10.0.0.1"), adm.UserID, p.SessionID, authn.AppAdmin)
	require.NoError(t, err)
	require.True(t, st.StepUpAt.IsZero(), "a † action still needs the SMS step-up")
}

func TestPurgeFirebaseSignins(t *testing.T) {
	w := newFirebaseFixture(t, nil)
	_, err := w.swap(authn.AppCustomer, w.token("+919876543214", time.Second, nil))
	require.NoError(t, err)
	res, err := Purge(w.ctx("10.0.0.1"), w.svc.db, w.clk.Now())
	require.NoError(t, err)
	require.Zero(t, res.FirebaseSignins, "still within the age limit")
	res, err = Purge(w.ctx("10.0.0.1"), w.svc.db, w.clk.Now().Add(firebaseReplayTTL+time.Second))
	require.NoError(t, err)
	require.EqualValues(t, 1, res.FirebaseSignins)
}

func TestPreflightFirebase(t *testing.T) {
	env := testEnv(t)
	for k, v := range map[string]string{
		"APP_ENV": "prod", "DATABASE_URL": "postgres://todayz_app@127.0.0.1:1/todayz", "API_PUBLIC_URL": "https://api.todayz.in",
		"AWS_REGION": "ap-south-1", "SMS_SENDER_ID": "TODAYZ", "SMS_DLT_ENTITY_ID": "1201169999000000001",
		"SMS_OTP_TEMPLATE_ID": "1207169999000000101", "TURNSTILE_API_URL": "https://challenges.cloudflare.com",
		"TURNSTILE_SECRET_KEY": "prod-turnstile-value", "OTP_PROVIDER": "firebase",
	} {
		env[k] = v
	}
	err := Preflight(loadConfig(t, env))
	require.EqualError(t, err, "identity: required in staging and prod: FIREBASE_PROJECT_ID, FIREBASE_WEB_API_KEY, FIREBASE_AUTH_DOMAIN, FIREBASE_APP_ID")
	env["FIREBASE_PROJECT_ID"] = "todayz-in"
	env["FIREBASE_WEB_API_KEY"] = "AIzaSyEXAMPLE"
	env["FIREBASE_AUTH_DOMAIN"] = "todayz-in.firebaseapp.com"
	env["FIREBASE_APP_ID"] = "1:1:web:1"
	cfg := loadConfig(t, env)
	require.NoError(t, Preflight(cfg))
	v := newFirebaseVerifier(cfg, nil)
	require.NotNil(t, v, "deployed, the keys are Google's (config refuses FIREBASE_JWKS_API_URL)")
}

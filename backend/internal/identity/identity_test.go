package identity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

var t0 = time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)

// testEnv returns a test-environment config with fresh random D-18 keys.
func testEnv(t testing.TB) map[string]string {
	t.Helper()
	rnd := func(n int) []byte {
		b := make([]byte, n)
		_, err := rand.Read(b)
		require.NoError(t, err)
		return b
	}
	seed := rnd(ed25519.SeedSize)
	set, err := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "OKP", "crv": "Ed25519", "kid": "k1", "d": base64.RawURLEncoding.EncodeToString(seed),
	}}})
	require.NoError(t, err)
	m := map[string]string{"APP_ENV": "test", "JWT_SIGNING_KEYS": string(set), "JWT_CURRENT_KID": "k1"}
	for _, n := range config.AppKeyNames[2:] {
		m[n] = base64.StdEncoding.EncodeToString(rnd(32))
	}
	return m
}

func loadConfig(t testing.TB, env map[string]string) *config.Config {
	t.Helper()
	cfg, err := config.Load(func(k string) string { return env[k] }, nil)
	require.NoError(t, err)
	return cfg
}

// smsBox is the SMS provider of the tests: it keeps the last code per phone,
// the way the e2e tests read it from the mocks.
type smsBox struct {
	mu    sync.Mutex
	codes map[string]string
	all   []string // every code sent, checked against the log
	sent  int
	fail  error
}

func (b *smsBox) SendOTP(_ context.Context, e164, code string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fail != nil {
		return b.fail
	}
	if b.codes == nil {
		b.codes = map[string]string{}
	}
	b.codes[e164] = code
	b.all = append(b.all, code)
	b.sent++
	return nil
}

func (b *smsBox) code(phone string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.codes[phone]
}

type human struct {
	ok  bool
	err error
}

func (h *human) Verify(context.Context, string, string) (bool, error) { return h.ok, h.err }

type fixture struct {
	t     *testing.T
	db    *testkit.DB
	clk   *clock.Fake
	svc   *Service
	sms   *smsBox
	human *human
	cfg   *config.Config
	log   *slog.Logger
	out   *lockedBuffer

	mu      sync.Mutex
	secrets []string // values that must never be logged
}

// lockedBuffer collects log output from concurrent requests.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newFixture builds the service on a fresh database. appEnv overrides the
// environment (staging turns the reserved test code off).
func newFixture(t *testing.T, appEnv config.Env) *fixture {
	t.Helper()
	return newFixtureWith(t, appEnv, nil)
}

// newFixtureWith lets a test change the config and Deps before New.
func newFixtureWith(t *testing.T, appEnv config.Env, edit func(*config.Config, *Deps)) *fixture {
	t.Helper()
	tdb := testkit.NewDB(t)
	cfg := loadConfig(t, testEnv(t))
	cfg.Env = appEnv
	clk := clock.NewFake(t0)
	ks, err := authn.NewKeySet(cfg.Keys.JWTSigningKeys, cfg.Keys.JWTCurrentKID)
	require.NoError(t, err)
	out := &lockedBuffer{}
	// An unredacting logger: a phone, code or token in a log line must fail
	// the test, not be masked by the redaction backstop.
	log := slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f := &fixture{t: t, db: tdb, clk: clk, sms: &smsBox{}, human: &human{ok: true}, cfg: cfg, log: log, out: out}
	deps := Deps{
		Config: cfg, DB: db.New(tdb.Pool), Clock: clk, Log: log,
		Tokens: authn.NewTokens(ks, clk), SMS: f.sms, Human: f.human, TestRoutes: true,
		Settings: settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{}),
	}
	if edit != nil {
		edit(cfg, &deps)
	}
	f.svc, err = New(deps)
	require.NoError(t, err)
	t.Cleanup(f.noSecretsLogged)
	return f
}

func (f *fixture) remember(vals ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range vals {
		if v != "" {
			f.secrets = append(f.secrets, v)
		}
	}
}

func (f *fixture) noSecretsLogged() {
	logged := f.out.String()
	f.sms.mu.Lock()
	sent := append([]string(nil), f.sms.all...)
	f.sms.mu.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range append(f.secrets, sent...) {
		require.NotContains(f.t, logged, s, "a phone, code or token reached the log")
	}
}

func (f *fixture) ctx(ip string) context.Context {
	return telemetry.WithLogger(reqctx.WithClientIP(context.Background(), ip), f.log)
}

func (f *fixture) send(phone string, app authn.App, ip string) (SendResult, error) {
	f.remember(phone)
	return f.svc.SendOTP(f.ctx(ip), app, phone, PurposeLogin, "tok")
}

func (f *fixture) verify(app authn.App, reqID ids.ID, code string) (SignIn, error) {
	f.remember(code)
	out, err := f.svc.VerifyOTP(f.ctx("10.0.0.1"), VerifyInput{App: app, RequestID: reqID.String(), Code: code, DeviceID: ids.New(t0).String(), UserAgent: "test"})
	f.remember(out.AccessToken, out.RefreshToken)
	return out, err
}

// signIn sends and verifies, returning the sign-in.
func (f *fixture) signIn(phone string, app authn.App) SignIn {
	f.t.Helper()
	res, err := f.send(phone, app, "10.0.0.1")
	require.NoError(f.t, err)
	out, err := f.verify(app, res.RequestID, f.sms.code(phone))
	require.NoError(f.t, err)
	f.clk.Advance(OTPResendGap)
	return out
}

func (f *fixture) owner(sql string, args ...any) {
	f.t.Helper()
	_, err := f.db.OwnerPool.Exec(context.Background(), sql, args...)
	require.NoError(f.t, err)
}

func (f *fixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	require.NoError(f.t, f.db.OwnerPool.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

func (f *fixture) strings(sql string, args ...any) []string {
	f.t.Helper()
	rows, err := f.db.OwnerPool.Query(context.Background(), sql, args...)
	require.NoError(f.t, err)
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(f.t, err)
	return out
}

func problemOf(t *testing.T, err error) *problem.Error {
	t.Helper()
	var p *problem.Error
	require.ErrorAs(t, err, &p, "want a problem, got %v", err)
	return p
}

func requireCode(t *testing.T, err error, code string, msgAndArgs ...any) {
	t.Helper()
	require.Error(t, err, msgAndArgs...)
	require.Equal(t, code, problemOf(t, err).Code, msgAndArgs...)
}

func retryAfter(t *testing.T, err error) int {
	t.Helper()
	p := problemOf(t, err)
	require.Equal(t, genproblems.RateLimited, p.Code)
	n, convErr := strconv.Atoi(p.Header.Get("Retry-After"))
	require.NoError(t, convErr)
	return n
}

// AU-01: a new number signs in, becomes a user with a sealed phone, and the
// stored OTP is a keyed hash, never the code.
func TestSignInNewCustomer(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const phone = "+919876543210"
	res, err := f.send(phone, authn.AppCustomer, "10.0.0.1")
	require.NoError(t, err)
	require.Equal(t, OTPResendGap, res.ResendAfter)
	code := f.sms.code(phone)
	require.Len(t, code, OTPDigits)

	var codeHash, phoneEnc []byte
	require.NoError(t, f.db.OwnerPool.QueryRow(context.Background(),
		`SELECT code_hash, phone_enc FROM otp_requests WHERE id = $1`, ids.UUID(res.RequestID)).Scan(&codeHash, &phoneEnc))
	require.NotContains(t, string(codeHash), code)
	require.NotContains(t, string(phoneEnc), phone)
	require.Zero(t, f.count(`SELECT count(*) FROM users`), "sending never creates a user")

	in, err := f.verify(authn.AppCustomer, res.RequestID, code)
	require.NoError(t, err)
	require.True(t, in.IsNewUser)
	require.NotEmpty(t, in.AccessToken)
	require.Equal(t, t0.Add(authn.AccessTTL), in.ExpiresAt)
	require.Equal(t, t0.Add(authn.RefreshTTL), in.RefreshExpiresAt)
	require.True(t, validRefreshToken(in.RefreshToken))

	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)
	require.Equal(t, in.UserID, p.UserID)
	require.Equal(t, authn.AppCustomer, p.App)
	require.Equal(t, authn.AMROTP, p.AMR)

	require.Equal(t, 1, f.count(`SELECT count(*) FROM users`))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM customer_profiles WHERE user_id = $1`, ids.UUID(in.UserID)))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM devices WHERE user_id = $1`, ids.UUID(in.UserID)))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, ids.UUID(in.UserID)))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.sign_in'`))
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE refresh_hash = $1`, []byte(in.RefreshToken)), "the refresh token is stored hashed")

	// The code is single use.
	_, err = f.verify(authn.AppCustomer, res.RequestID, code)
	requireCode(t, err, genproblems.OtpExpired)

	// Signing in again finds the same user.
	f.clk.Advance(OTPResendGap)
	again := f.signIn(phone, authn.AppCustomer)
	require.False(t, again.IsNewUser)
	require.Equal(t, in.UserID, again.UserID)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM users`))

	// The same person in the business app is the same user (one identity).
	biz := f.signIn(phone, authn.AppBusiness)
	require.Equal(t, in.UserID, biz.UserID)
}

// AU-06: a known and an unknown number get the same answer.
func TestSendDoesNotRevealAccounts(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	f.signIn("+919876543210", authn.AppCustomer)
	f.clk.Advance(time.Hour)
	known, err := f.send("+919876543210", authn.AppCustomer, "10.0.0.2")
	require.NoError(t, err)
	unknown, err := f.send("+919876543211", authn.AppCustomer, "10.0.0.2")
	require.NoError(t, err)
	require.Equal(t, known.ResendAfter, unknown.ResendAfter)
	// Admin: the send is answered the same for any number; the refusal comes at verify.
	_, err = f.send("+919876543212", authn.AppAdmin, "10.0.0.2")
	require.NoError(t, err)
}

func TestSendValidation(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	ctx := f.ctx("10.0.0.1")
	_, err := f.svc.SendOTP(ctx, authn.AppCustomer, "9876543210", PurposeLogin, "")
	requireCode(t, err, genproblems.ValidationFailed)
	require.Contains(t, problemOf(t, err).Fields, "body.phone")
	_, err = f.svc.SendOTP(ctx, authn.AppCustomer, "+919876543210", "signup", "")
	require.Contains(t, problemOf(t, err).Fields, "body.purpose")
	_, err = f.svc.SendOTP(ctx, "storefront", "+919876543210", PurposeLogin, "")
	require.Contains(t, problemOf(t, err).Fields, "body.app")
	require.Zero(t, f.sms.sent)
}

// AU-03: one send per number per 30 s.
func TestResendGap(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const phone = "+919876543210"
	_, err := f.send(phone, authn.AppCustomer, "10.0.0.1")
	require.NoError(t, err)
	f.clk.Advance(10 * time.Second)
	_, err = f.send(phone, authn.AppCustomer, "10.0.0.9")
	require.Equal(t, 20, retryAfter(t, err), "another IP does not help")
	// Another app shares the per-number limit.
	_, err = f.send(phone, authn.AppBusiness, "10.0.0.1")
	require.Equal(t, 20, retryAfter(t, err))
	f.clk.Advance(20 * time.Second)
	_, err = f.send(phone, authn.AppCustomer, "10.0.0.1")
	require.NoError(t, err)
	require.Equal(t, 2, f.sms.sent)
}

// AU-04: five sends per number per hour.
func TestPerPhoneLimit(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const phone = "+919876543210"
	for i := range OTPPerPhone {
		_, err := f.send(phone, authn.AppCustomer, fmt.Sprintf("10.0.0.%d", i))
		require.NoError(t, err, i)
		f.clk.Advance(OTPResendGap)
	}
	_, err := f.send(phone, authn.AppCustomer, "10.0.1.1")
	// The oldest send was 5 × 30 s ago; it leaves the window in 57.5 min.
	require.Equal(t, int((OTPWindow-OTPPerPhone*OTPResendGap)/time.Second), retryAfter(t, err))
	require.Equal(t, OTPPerPhone, f.sms.sent)
	require.Equal(t, OTPPerPhone, f.count(`SELECT count(*) FROM otp_requests`), "a refused send stores nothing")

	f.clk.Set(t0.Add(OTPWindow))
	_, err = f.send(phone, authn.AppCustomer, "10.0.1.1")
	require.NoError(t, err, "the oldest send has left the window")
}

// AU-05: twenty sends per client IP per hour, whatever the numbers.
func TestPerIPLimit(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	for i := range OTPPerIP {
		_, err := f.send(fmt.Sprintf("+9198765432%02d", i), authn.AppCustomer, "10.0.0.1")
		require.NoError(t, err, i)
	}
	_, err := f.send("+919876543299", authn.AppCustomer, "10.0.0.1")
	require.Equal(t, int(OTPWindow/time.Second), retryAfter(t, err))
	_, err = f.send("+919876543299", authn.AppCustomer, "10.0.0.2")
	require.NoError(t, err, "another IP")
	f.clk.Advance(OTPWindow)
	_, err = f.send("+919876543298", authn.AppCustomer, "10.0.0.1")
	require.NoError(t, err)
}

// AU-05 over IPv6: the limit binds a whole /64, so rotating addresses inside
// one subscriber's network does not reset it; another /64 is unaffected.
func TestPerIPLimitIPv6(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	for i := range OTPPerIP {
		_, err := f.send(fmt.Sprintf("+9198765432%02d", i), authn.AppCustomer, fmt.Sprintf("2001:db8:1:2::%x", i+1))
		require.NoError(t, err, i)
	}
	_, err := f.send("+919876543299", authn.AppCustomer, "2001:db8:1:2:ffff::1")
	require.Equal(t, int(OTPWindow/time.Second), retryAfter(t, err), "same /64, new address")
	_, err = f.send("+919876543299", authn.AppCustomer, "2001:db8:1:3::1")
	require.NoError(t, err, "another /64")
}

// The limits hold under concurrency (advisory locks): of many simultaneous
// sends for one number, exactly one goes through.
func TestConcurrentSends(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.send("+919876543210", authn.AppCustomer, fmt.Sprintf("10.0.0.%d", i))
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else {
			requireCode(t, err, genproblems.RateLimited)
		}
	}
	require.Equal(t, 1, ok)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM otp_requests`))
}

// AU-02: five tries per code, then locked; the count survives a wrong code.
func TestAttemptsThenLocked(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const phone = "+919876543210"
	res, err := f.send(phone, authn.AppCustomer, "10.0.0.1")
	require.NoError(t, err)
	code := f.sms.code(phone)
	wrong := "000001"
	if code == wrong {
		wrong = "000002"
	}
	for i := 1; i <= OTPMaxAttempts; i++ {
		_, err := f.verify(authn.AppCustomer, res.RequestID, wrong)
		p := problemOf(t, err)
		require.Equal(t, genproblems.Unauthorized, p.Code, i)
		require.Equal(t, OTPMaxAttempts-i, p.Extensions["attemptsLeft"], i)
	}
	_, err = f.verify(authn.AppCustomer, res.RequestID, code)
	requireCode(t, err, genproblems.OtpLocked, "even the right code is refused once locked")
	require.Zero(t, f.count(`SELECT count(*) FROM users`))
}

func TestVerifyRefusals(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const phone = "+919876543210"
	res, err := f.send(phone, authn.AppCustomer, "10.0.0.1")
	require.NoError(t, err)
	code := f.sms.code(phone)

	_, err = f.verify(authn.AppBusiness, res.RequestID, code)
	requireCode(t, err, genproblems.OtpExpired, "a code is for the app that asked for it")
	_, err = f.verify(authn.AppCustomer, ids.New(t0), code)
	requireCode(t, err, genproblems.OtpExpired, "unknown request")
	_, err = f.svc.VerifyOTP(f.ctx(""), VerifyInput{App: authn.AppCustomer, RequestID: "nope", Code: code, DeviceID: ids.New(t0).String()})
	requireCode(t, err, genproblems.OtpExpired)
	_, err = f.svc.VerifyOTP(f.ctx(""), VerifyInput{App: authn.AppCustomer, RequestID: res.RequestID.String(), Code: code, DeviceID: "x"})
	require.Contains(t, problemOf(t, err).Fields, "body.deviceId")
	_, err = f.svc.VerifyOTP(f.ctx(""), VerifyInput{App: "x", RequestID: res.RequestID.String(), Code: code, DeviceID: ids.New(t0).String()})
	require.Contains(t, problemOf(t, err).Fields, "body.app")

	// AU-03: five minutes, then expired.
	f.clk.Advance(OTPTTL)
	_, err = f.verify(authn.AppCustomer, res.RequestID, code)
	requireCode(t, err, genproblems.OtpExpired)
}

// 01 §2: the reserved numbers accept 000000 in local and test only.
func TestReservedTestCode(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	res, err := f.send("+919000000042", authn.AppCustomer, "10.0.0.1")
	require.NoError(t, err)
	_, err = f.verify(authn.AppCustomer, res.RequestID, "000000")
	require.NoError(t, err)

	res, err = f.send("+919876543210", authn.AppCustomer, "10.0.0.1")
	require.NoError(t, err)
	if f.sms.code("+919876543210") != "000000" {
		_, err = f.verify(authn.AppCustomer, res.RequestID, "000000")
		requireCode(t, err, genproblems.Unauthorized, "an ordinary number")
	}

	// The /v1/test/clock gate (D-80): each of the three conditions alone
	// turns the test code off.
	for name, edit := range map[string]func(*config.Config, *Deps){
		"staging":                      func(c *config.Config, _ *Deps) { c.Env = config.EnvStaging },
		"database not local or test":   func(_ *config.Config, d *Deps) { d.TestRoutes = false },
		"API listening on every iface": func(c *config.Config, _ *Deps) { c.APIAddr = ":8180" },
		"API on a public address":      func(c *config.Config, _ *Deps) { c.APIAddr = "203.0.113.7:8180" },
	} {
		g := newFixtureWith(t, config.EnvTest, edit)
		require.False(t, g.svc.allowTestCode, name)
		res, err = g.send("+919000000042", authn.AppCustomer, "10.0.0.1")
		require.NoError(t, err, name)
		if g.sms.code("+919000000042") != "000000" {
			_, err = g.verify(authn.AppCustomer, res.RequestID, "000000")
			requireCode(t, err, genproblems.Unauthorized, name)
		}
	}
	require.True(t, newFixtureWith(t, config.EnvLocal, func(c *config.Config, _ *Deps) { c.APIAddr = "localhost:8180" }).svc.allowTestCode)
}

// AU-20: a failed Turnstile check stores and sends nothing.
func TestTurnstile(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	f.human.ok = false
	_, err := f.send("+919876543210", authn.AppCustomer, "10.0.0.1")
	requireCode(t, err, genproblems.Forbidden)
	f.human.ok, f.human.err = false, errors.New("turnstile down")
	_, err = f.send("+919876543210", authn.AppCustomer, "10.0.0.1")
	requireCode(t, err, genproblems.ServiceUnavailable)
	require.Zero(t, f.sms.sent)
	require.Zero(t, f.count(`SELECT count(*) FROM otp_requests`))
}

// A provider failure is 503 and still counts toward the limits.
func TestSMSFailureCounts(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	f.sms.fail = fmt.Errorf("%w: status 500", ErrProvider)
	_, err := f.send("+919876543210", authn.AppCustomer, "10.0.0.1")
	requireCode(t, err, genproblems.ServiceUnavailable)
	require.Contains(t, f.out.String(), "OTP SMS failed", "the log capture is live")
	f.sms.fail = nil
	_, err = f.send("+919876543210", authn.AppCustomer, "10.0.0.1")
	require.Equal(t, int(OTPResendGap/time.Second), retryAfter(t, err))
}

func refreshIn(f *fixture, app authn.App, token string) (SignIn, error) {
	f.remember(token)
	out, err := f.svc.Refresh(f.ctx("10.0.0.1"), RefreshInput{App: app, Token: token, UserAgent: "test"})
	f.remember(out.AccessToken, out.RefreshToken)
	return out, err
}

// AU-07: refresh rotates; replaying a rotated token revokes the family.
func TestRefreshRotationAndReuse(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in := f.signIn("+919876543210", authn.AppCustomer)
	first, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)

	f.clk.Advance(time.Hour)
	r1, err := refreshIn(f, authn.AppCustomer, in.RefreshToken)
	require.NoError(t, err)
	require.NotEqual(t, in.RefreshToken, r1.RefreshToken)
	require.Equal(t, f.clk.Now().Add(authn.RefreshTTL), r1.RefreshExpiresAt, "customer sessions slide")
	p, err := f.svc.tokens.Verify(r1.AccessToken)
	require.NoError(t, err)
	require.Equal(t, first.SessionID, p.SessionID, "the session id is the family, stable across rotation")

	st, err := f.svc.CheckSession(context.Background(), in.UserID, first.SessionID, authn.AppCustomer)
	require.NoError(t, err)
	require.True(t, st.Active)

	r2, err := refreshIn(f, authn.AppCustomer, r1.RefreshToken)
	require.NoError(t, err)

	// Replay of the first, already rotated token, past the grace window.
	f.clk.Advance(RotationGrace + time.Microsecond)
	_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized)
	_, err = refreshIn(f, authn.AppCustomer, r2.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized, "the whole family is revoked")
	st, err = f.svc.CheckSession(context.Background(), in.UserID, first.SessionID, authn.AppCustomer)
	require.NoError(t, err)
	require.False(t, st.Active, "access tokens of the family stop at once")
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.refresh_reuse'`))
	require.Contains(t, f.out.String(), "refresh token reuse", "the log capture is live")
	require.Equal(t, 0, f.count(`SELECT count(*) FROM sessions WHERE revoked_at IS NULL`))
}

// A retried refresh whose response was lost: the token rotated RotationGrace
// ago or less is a plain 401 (no revoke, no audit); a moment later it is reuse.
func TestRefreshRotationGrace(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in := f.signIn("+919876543210", authn.AppCustomer)
	first, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)

	r1, err := refreshIn(f, authn.AppCustomer, in.RefreshToken) // in rotated now
	require.NoError(t, err)

	_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized, "a retry at once")
	f.clk.Advance(RotationGrace)
	_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized, "a retry at the edge of the window")
	require.Contains(t, f.out.String(), "within the grace window", "the log capture is live")

	st, err := f.svc.CheckSession(context.Background(), in.UserID, first.SessionID, authn.AppCustomer)
	require.NoError(t, err)
	require.True(t, st.Active, "inside the window the sign-in lives on")
	require.Equal(t, 0, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.refresh_reuse'`))
	require.Equal(t, 0, f.count(`SELECT count(*) FROM sessions WHERE revoked_reason = 'reuse'`))

	// The holder of the successor carries on (this rotates r1, not in).
	_, err = refreshIn(f, authn.AppCustomer, r1.RefreshToken)
	require.NoError(t, err)

	// Just past the window, the first token is reuse: the family ends.
	f.clk.Advance(time.Microsecond)
	_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized)
	st, err = f.svc.CheckSession(context.Background(), in.UserID, first.SessionID, authn.AppCustomer)
	require.NoError(t, err)
	require.False(t, st.Active)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.refresh_reuse'`))
	require.Equal(t, 0, f.count(`SELECT count(*) FROM sessions WHERE revoked_at IS NULL`))
}

func TestRefreshRefusals(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in := f.signIn("+919876543210", authn.AppCustomer)
	_, err := refreshIn(f, authn.AppCustomer, "short")
	requireCode(t, err, genproblems.Unauthorized)
	unknown, _, err := newRefreshToken()
	require.NoError(t, err)
	_, err = refreshIn(f, authn.AppCustomer, unknown)
	requireCode(t, err, genproblems.Unauthorized)
	_, err = refreshIn(f, authn.AppBusiness, in.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized, "a customer token on the business app")
	f.clk.Advance(authn.RefreshTTL)
	_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized, "expired")
}

func TestLogout(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in := f.signIn("+919876543210", authn.AppCustomer)
	other := f.signIn("+919876543210", authn.AppCustomer) // a second device
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)

	ctx := f.ctx("10.0.0.1")
	requireCode(t, f.svc.Logout(ctx, RefreshInput{App: authn.AppCustomer}), genproblems.Unauthorized, "no cookie")
	require.NoError(t, f.svc.Logout(ctx, RefreshInput{App: authn.AppCustomer, Token: "junk"}))
	require.NoError(t, f.svc.Logout(ctx, RefreshInput{App: authn.AppBusiness, Token: in.RefreshToken}), "another app's token is ignored")
	_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
	require.NoError(t, err, "still signed in after the ignored logout")

	require.NoError(t, f.svc.Logout(ctx, RefreshInput{App: authn.AppCustomer, Token: in.RefreshToken}), "a rotated token still ends its sign-in")
	st, err := f.svc.CheckSession(context.Background(), in.UserID, p.SessionID, authn.AppCustomer)
	require.NoError(t, err)
	require.False(t, st.Active)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.sign_out'`))

	// The other device is untouched (logout-all: TestLogoutAll).
	_, err = refreshIn(f, authn.AppCustomer, other.RefreshToken)
	require.NoError(t, err)
	// Logging out twice is fine.
	require.NoError(t, f.svc.Logout(ctx, RefreshInput{App: authn.AppCustomer, Token: in.RefreshToken}))
}

// AU-17: a blocked user stops at once: sign-in, refresh and every request.
func TestBlockedUser(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in := f.signIn("+919876543210", authn.AppCustomer)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)
	f.owner(`UPDATE users SET status = 'blocked' WHERE id = $1`, ids.UUID(in.UserID))

	st, err := f.svc.CheckSession(context.Background(), in.UserID, p.SessionID, authn.AppCustomer)
	require.NoError(t, err)
	require.True(t, st.Blocked)
	_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
	requireCode(t, err, genproblems.AccountBlocked)
	res, err := f.send("+919876543210", authn.AppCustomer, "10.0.0.1")
	require.NoError(t, err, "the send answers the same (AU-06)")
	_, err = f.verify(authn.AppCustomer, res.RequestID, f.sms.code("+919876543210"))
	requireCode(t, err, genproblems.AccountBlocked)
	_, err = f.svc.GetMe(context.Background(), p)
	requireCode(t, err, genproblems.AccountBlocked)

	f.owner(`UPDATE users SET status = 'deleted', deleted_at = now() WHERE id = $1`, ids.UUID(in.UserID))
	f.clk.Advance(OTPResendGap)
	res, err = f.send("+919876543210", authn.AppCustomer, "10.0.0.1")
	require.NoError(t, err)
	_, err = f.verify(authn.AppCustomer, res.RequestID, f.sms.code("+919876543210"))
	requireCode(t, err, genproblems.AccountBlocked, "a deleted account: account_blocked (04 names no other code)")
	_, err = f.svc.GetMe(context.Background(), p)
	requireCode(t, err, genproblems.AccountBlocked)

	// Unknown session, other user, other app: inactive, not an error.
	st, err = f.svc.CheckSession(context.Background(), ids.New(t0), p.SessionID, authn.AppCustomer)
	require.NoError(t, err)
	require.Equal(t, authn.SessionState{}, st)
}

// Admin sign-in: only an existing, enabled admin; roles come from admin_users;
// an OTP-only admin session is amr otp (TOTP: totp_service_test.go).
func TestAdminSignIn(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const phone = "+919876543210"
	res, err := f.send(phone, authn.AppAdmin, "10.0.0.1")
	require.NoError(t, err)
	_, err = f.verify(authn.AppAdmin, res.RequestID, f.sms.code(phone))
	requireCode(t, err, genproblems.Forbidden, "an unknown number never becomes an admin")
	require.Zero(t, f.count(`SELECT count(*) FROM users`))

	f.clk.Advance(OTPResendGap)
	cust := f.signIn(phone, authn.AppCustomer)
	res, err = f.send(phone, authn.AppAdmin, "10.0.0.1")
	require.NoError(t, err)
	_, err = f.verify(authn.AppAdmin, res.RequestID, f.sms.code(phone))
	requireCode(t, err, genproblems.Forbidden, "a customer is not an admin")

	f.owner(`INSERT INTO admin_users (user_id, roles) VALUES ($1, ARRAY['support','finance'])`, ids.UUID(cust.UserID))
	f.clk.Advance(OTPResendGap)
	adm := f.signIn(phone, authn.AppAdmin)
	require.Equal(t, cust.UserID, adm.UserID)
	require.Equal(t, f.clk.Now().Add(authn.AdminRefreshTTL-OTPResendGap), adm.RefreshExpiresAt)
	p, err := f.svc.tokens.Verify(adm.AccessToken)
	require.NoError(t, err)
	require.Equal(t, authn.AMROTP, p.AMR, "OTP only until TOTP verifies")
	require.Equal(t, []string{"support", "finance"}, p.Roles)

	st, err := f.svc.CheckSession(context.Background(), adm.UserID, p.SessionID, authn.AppAdmin)
	require.NoError(t, err)
	require.True(t, st.Active)
	require.Equal(t, []string{"support", "finance"}, st.Roles)
	require.True(t, st.StepUpAt.IsZero())

	// The admin session is absolute: a refresh keeps the first expiry.
	r, err := refreshIn(f, authn.AppAdmin, adm.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, adm.RefreshExpiresAt, r.RefreshExpiresAt)

	// Disabled: the live session stops and refresh revokes the family.
	f.owner(`UPDATE admin_users SET disabled_at = now() WHERE user_id = $1`, ids.UUID(adm.UserID))
	st, err = f.svc.CheckSession(context.Background(), adm.UserID, p.SessionID, authn.AppAdmin)
	require.NoError(t, err)
	require.False(t, st.Active)
	_, err = refreshIn(f, authn.AppAdmin, r.RefreshToken)
	requireCode(t, err, genproblems.Forbidden)
	require.Equal(t, 0, f.count(`SELECT count(*) FROM sessions WHERE app = 'admin' AND revoked_at IS NULL`))
}

func TestMe(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in := f.signIn("+919876543210", authn.AppCustomer)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)
	ctx := context.Background()

	me, err := f.svc.GetMe(ctx, p)
	require.NoError(t, err)
	require.Equal(t, "98•••••210", me.PhoneMasked)
	require.Nil(t, me.Name)
	require.Equal(t, "en", me.PreferredLang)
	require.NotNil(t, me.Customer)
	require.Regexp(t, `^[A-HJKMNP-Z2-9]{6}$`, me.Customer.ReferralCode)
	require.Nil(t, me.Customer.LimitedUntil)
	require.Empty(t, me.Roles)

	name, lang := "Asha", "kn"
	up, err := f.svc.UpdateMe(ctx, p, me.Version, MePatch{SetName: true, Name: &name, PreferredLang: &lang})
	require.NoError(t, err)
	require.Equal(t, "Asha", *up.Name)
	require.Equal(t, "kn", up.PreferredLang)
	require.Equal(t, me.Version+1, up.Version)

	_, err = f.svc.UpdateMe(ctx, p, me.Version, MePatch{PreferredLang: &lang})
	requireCode(t, err, genproblems.PreconditionFailed, "a stale If-Match")

	// Language only keeps the name; SetName with nil clears it.
	hi := "hi"
	up2, err := f.svc.UpdateMe(ctx, p, up.Version, MePatch{PreferredLang: &hi})
	require.NoError(t, err)
	require.Equal(t, "Asha", *up2.Name)
	up3, err := f.svc.UpdateMe(ctx, p, up2.Version, MePatch{SetName: true})
	require.NoError(t, err)
	require.Nil(t, up3.Name)

	// A strike limit shows while it lasts.
	until := f.clk.Now().Add(48 * time.Hour)
	f.owner(`UPDATE customer_profiles SET limited_until = $1 WHERE user_id = $2`, until, ids.UUID(in.UserID))
	me, err = f.svc.GetMe(ctx, p)
	require.NoError(t, err)
	require.NotNil(t, me.Customer.LimitedUntil)
	require.True(t, until.Equal(*me.Customer.LimitedUntil))

	// The business app shows no customer part.
	bp := p
	bp.App = authn.AppBusiness
	me, err = f.svc.GetMe(ctx, bp)
	require.NoError(t, err)
	require.Nil(t, me.Customer)

	_, err = f.svc.GetMe(ctx, authn.Principal{UserID: ids.New(t0), App: authn.AppCustomer})
	requireCode(t, err, genproblems.Unauthorized, "a user that does not exist")
}

func TestPurge(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in := f.signIn("+919876543210", authn.AppCustomer)
	_, err := f.send("+919876543211", authn.AppCustomer, "10.0.0.1")
	require.NoError(t, err)
	d := db.New(f.db.Pool)

	res, err := Purge(context.Background(), d, f.clk.Now())
	require.NoError(t, err)
	require.Equal(t, PurgeResult{}, res, "nothing is old enough yet")

	res, err = Purge(context.Background(), d, f.clk.Now().Add(OTPRetention+time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(2), res.OTPRequests)
	require.Zero(t, res.Sessions)

	res, err = Purge(context.Background(), d, in.RefreshExpiresAt.Add(SessionRetention+time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Sessions)
	require.Zero(t, f.count(`SELECT count(*) FROM sessions`))
}

// A revoked session goes SessionRetention after its revocation, not after
// its (later) expiry; a live one waits for its expiry. A rotated row always
// outlives its own token, so reuse detection is never cut short.
func TestPurgeRevokedSessions(t *testing.T) {
	require.GreaterOrEqual(t, SessionRetention, authn.RefreshTTL, "a rotated row must outlive its token")
	require.GreaterOrEqual(t, SessionRetention, authn.AdminRefreshTTL)

	f := newFixture(t, config.EnvTest)
	d := db.New(f.db.Pool)
	out := f.signIn("+919876543210", authn.AppCustomer)
	require.NoError(t, f.svc.Logout(f.ctx("10.0.0.1"), RefreshInput{App: authn.AppCustomer, Token: out.RefreshToken}))
	loggedOutAt := f.clk.Now()

	f.clk.Advance(time.Hour)
	kept := f.signIn("+919876543211", authn.AppCustomer)
	r1, err := refreshIn(f, authn.AppCustomer, kept.RefreshToken) // kept's first row: rotated now
	require.NoError(t, err)
	rotatedAt := f.clk.Now()

	purgeAt := func(at time.Time) int64 {
		t.Helper()
		res, err := Purge(context.Background(), d, at)
		require.NoError(t, err)
		return res.Sessions
	}
	require.Zero(t, purgeAt(loggedOutAt.Add(SessionRetention)), "not before its retention")
	require.Equal(t, int64(1), purgeAt(loggedOutAt.Add(SessionRetention+time.Microsecond)), "the logged-out session, well before its expiry")
	require.Equal(t, int64(1), purgeAt(rotatedAt.Add(SessionRetention+time.Microsecond)), "the rotated row")
	require.Equal(t, 1, f.count(`SELECT count(*) FROM sessions WHERE revoked_at IS NULL`), "the live row waits for its expiry")
	require.Zero(t, purgeAt(r1.RefreshExpiresAt.Add(SessionRetention)))
	require.Equal(t, int64(1), purgeAt(r1.RefreshExpiresAt.Add(SessionRetention+time.Microsecond)))
	require.Zero(t, f.count(`SELECT count(*) FROM sessions`))
}

func TestUnconfigured(t *testing.T) {
	cfg := loadConfig(t, map[string]string{"APP_ENV": "test"})
	svc, err := New(Deps{Config: cfg, Clock: clock.NewFake(t0)})
	require.NoError(t, err)
	ctx := context.Background()
	_, err = svc.SendOTP(ctx, authn.AppCustomer, "+919876543210", PurposeLogin, "")
	requireCode(t, err, genproblems.ServiceUnavailable)
	_, err = svc.VerifyOTP(ctx, VerifyInput{})
	requireCode(t, err, genproblems.ServiceUnavailable)
	_, err = svc.Refresh(ctx, RefreshInput{})
	requireCode(t, err, genproblems.ServiceUnavailable)
	requireCode(t, svc.Logout(ctx, RefreshInput{}), genproblems.ServiceUnavailable)
	_, err = svc.GetMe(ctx, authn.Principal{})
	requireCode(t, err, genproblems.ServiceUnavailable)
	_, err = svc.CheckSession(ctx, ids.Zero, ids.Zero, authn.AppCustomer)
	require.Error(t, err)

	_, err = New(Deps{Config: cfg})
	require.Error(t, err, "a clock is required")
}

func TestPreflight(t *testing.T) {
	require.NoError(t, Preflight(loadConfig(t, map[string]string{"APP_ENV": "local"})), "local needs nothing")

	env := testEnv(t)
	env["APP_ENV"] = "staging"
	env["DATABASE_URL"] = "postgres://todayz_app@127.0.0.1:1/todayz"
	env["API_PUBLIC_URL"] = "https://api.staging.todayz.in"
	cfg := loadConfig(t, env)
	err := Preflight(cfg)
	for _, name := range []string{"AWS_REGION", "SMS_SENDER_ID", "SMS_DLT_ENTITY_ID", "SMS_OTP_TEMPLATE_ID", "TURNSTILE_API_URL", "TURNSTILE_SECRET_KEY"} {
		require.ErrorContains(t, err, name)
	}
	require.NotContains(t, err.Error(), "PII_ENC_KEY")
	require.NotContains(t, err.Error(), "SMS_API_URL", "D-212: deployed SMS needs no endpoint override")

	values := map[string]string{
		"AWS_REGION":           "ap-south-1",
		"SMS_SENDER_ID":        "TODAYZ",
		"SMS_DLT_ENTITY_ID":    "1201169999000000001",
		"SMS_OTP_TEMPLATE_ID":  "1207169999000000101",
		"TURNSTILE_API_URL":    "https://challenges.cloudflare.com",
		"TURNSTILE_SECRET_KEY": "staging-turnstile-value",
	}
	for k, v := range values {
		env[k] = v
	}
	require.NoError(t, Preflight(loadConfig(t, env)))

	// Each one alone is enough to refuse, and the error names only it.
	for name := range values {
		short := maps.Clone(env)
		delete(short, name)
		err := Preflight(loadConfig(t, short))
		require.EqualError(t, err, "identity: required in staging and prod: "+name)
		for _, v := range values {
			require.NotContains(t, err.Error(), v, "values are never named")
		}
	}

	// D-212: SMS_API_URL in staging or prod (config.Load refuses it; a config
	// built another way is refused here), named without its value.
	cfg = loadConfig(t, env)
	cfg.Providers.SMSAPIURL = "https://sms.example.com/x"
	err = Preflight(cfg)
	require.EqualError(t, err, "identity: must be unset in staging and prod: SMS_API_URL")
	sms, _ := providers(cfg)
	require.IsType(t, noSMS{}, sms, "never a client that signs for the override")
	require.ErrorContains(t, sms.SendOTP(context.Background(), "+919876543210", "123456"), "SMS_API_URL must be unset in staging and prod")

	// TOTP_ENC_KEY (M2): config.Load already refuses a deployed config
	// without it; Preflight checks it again, like the other keys.
	cfg = loadConfig(t, env)
	cfg.Keys.TOTPEncKey = config.Secret{}
	require.EqualError(t, Preflight(cfg), "identity: required in staging and prod: TOTP_ENC_KEY")

	// The same value as PII_ENC_KEY is refused, without naming either value.
	same := maps.Clone(env)
	same["TOTP_ENC_KEY"] = same["PII_ENC_KEY"]
	err = Preflight(loadConfig(t, same))
	require.EqualError(t, err, "identity: TOTP_ENC_KEY must differ from PII_ENC_KEY")
	require.NotContains(t, err.Error(), same["PII_ENC_KEY"])
	same["APP_ENV"] = "local"
	require.NoError(t, Preflight(loadConfig(t, same)), "local is not checked")
}

// The staging and prod providers: no SMS refuses every send.
func TestProviders(t *testing.T) {
	cfg := loadConfig(t, map[string]string{"APP_ENV": "test"})
	sms, h := providers(cfg)
	require.IsType(t, dropSMS{}, sms)
	require.IsType(t, passHuman{}, h)

	cfg.Env = config.EnvProd
	sms, h = providers(cfg)
	require.IsType(t, noSMS{}, sms)
	require.ErrorIs(t, sms.SendOTP(context.Background(), "+919876543210", "123456"), ErrProvider)
	require.IsType(t, noHuman{}, h, "deployed without Turnstile fails closed")
	ok, err := h.Verify(context.Background(), "token", "10.0.0.1")
	require.False(t, ok)
	require.ErrorIs(t, err, ErrProvider)

	cfg.Providers.SMSAPIURL = "http://127.0.0.1:1"
	cfg.Providers.TurnstileAPIURL = "http://127.0.0.1:1"
	cfg.Env = config.EnvTest
	sms, h = providers(cfg)
	require.IsType(t, noSMS{}, sms, "SMS_API_URL without the DLT ids fails visibly, never drops")
	require.ErrorContains(t, sms.SendOTP(context.Background(), "+919876543210", "123456"), "SMS_SENDER_ID")
	require.IsType(t, &turnstile{}, h)
	require.Equal(t, turnstileTestSecret, h.(*turnstile).secret, "the mock is checked with the public test secret")

	cfg.Providers.SMSSenderID = "TODAYZ"
	cfg.Providers.SMSDLTEntityID = "1000000000000000001"
	sms, _ = providers(cfg)
	require.Equal(t, noSMS{reason: "SMS_OTP_TEMPLATE_ID is unset"}, sms)
	cfg.Providers.SMSOTPTemplateID = "1000000000000000101"
	sms, _ = providers(cfg)
	require.IsType(t, &awsOTP{}, sms)

	cfg.Providers.SMSAPIURL = "https://sms-voice.ap-south-1.amazonaws.com"
	sms, _ = providers(cfg)
	require.IsType(t, noSMS{}, sms, "local and test never sign for a real endpoint")
	require.ErrorContains(t, sms.SendOTP(context.Background(), "+919876543210", "123456"), "SMS_API_URL must be a local address")
}

// D-212: the OTP goes to AWS End User Messaging with the DLT ids, as the
// exact registered text, and expires with the code.
func TestAWSOTP(t *testing.T) {
	type sent struct {
		Target, Auth string
		Body         map[string]any
	}
	got := make(chan sent, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		got <- sent{Target: r.Header.Get("X-Amz-Target"), Auth: r.Header.Get("Authorization"), Body: b}
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		_, _ = w.Write([]byte(`{"MessageId":"m-1"}`))
	}))
	t.Cleanup(srv.Close)

	cfg := loadConfig(t, map[string]string{"APP_ENV": "test"})
	cfg.Providers.SMSAPIURL = srv.URL
	cfg.Providers.SMSSenderID = "TODAYZ"
	cfg.Providers.SMSDLTEntityID = "1000000000000000001"
	cfg.Providers.SMSOTPTemplateID = "1000000000000000101"
	sms, _ := providers(cfg)
	require.NoError(t, sms.SendOTP(context.Background(), "+919876543210", "482913"))
	s := <-got
	require.Equal(t, "PinpointSMSVoiceV2.SendTextMessage", s.Target)
	require.True(t, strings.HasPrefix(s.Auth, "AWS4-HMAC-SHA256 "), "SigV4-signed offline with dummy credentials")
	require.Equal(t, "482913 is your TodayZ login code. Do not share it with anyone.", s.Body["MessageBody"], "the DLT template text, exactly")
	require.Equal(t, "+919876543210", s.Body["DestinationPhoneNumber"])
	require.Equal(t, "TODAYZ", s.Body["OriginationIdentity"])
	require.Equal(t, "TRANSACTIONAL", s.Body["MessageType"])
	require.EqualValues(t, OTPTTL/time.Second, s.Body["TimeToLive"])
	require.Equal(t, map[string]any{"IN_ENTITY_ID": "1000000000000000001", "IN_TEMPLATE_ID": "1000000000000000101"}, s.Body["DestinationCountryParameters"])

	// The field owner code uses the sign-in template until tz_owner_code has
	// a DLT id, then its own registered text and id.
	svc, err := New(Deps{Config: cfg, Clock: clock.NewFake(time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC))})
	require.NoError(t, err)
	require.Same(t, svc.OTPSender(), svc.OwnerCodeSender())
	cfg.Providers.SMSNoticeTemplateIDs = map[string]string{"tz_owner_code": "1000000000000000203"}
	require.NoError(t, svc.OwnerCodeSender().SendOTP(context.Background(), "+919876543210", "482913"))
	s = <-got
	require.Equal(t, "482913 is your TodayZ code to confirm your shop with our field agent. Share it only with that agent.", s.Body["MessageBody"])
	require.Equal(t, map[string]any{"IN_ENTITY_ID": "1000000000000000001", "IN_TEMPLATE_ID": "1000000000000000203"}, s.Body["DestinationCountryParameters"])
}

func TestHelpers(t *testing.T) {
	require.Nil(t, cleanUserAgent("  "))
	long := bytes.Repeat([]byte("é"), maxUserAgent+10)
	ua := cleanUserAgent(string(long))
	require.Equal(t, maxUserAgent, len([]rune(*ua)))
	require.Equal(t, "ok", *cleanUserAgent("ok\xff"))

	require.False(t, validRefreshToken(""))
	require.False(t, validRefreshToken("!"+base64.RawURLEncoding.EncodeToString(make([]byte, 31))[1:]+"A"))
	tok, h, err := newRefreshToken()
	require.NoError(t, err)
	require.True(t, validRefreshToken(tok))
	require.Equal(t, hashRefresh(tok), h)

	for range 50 {
		c, err := randomDigits(OTPDigits)
		require.NoError(t, err)
		require.Regexp(t, `^[0-9]{6}$`, c)
	}
	p := problemOf(t, limited(0, OTPResendGap))
	require.Equal(t, "1", p.Header.Get("Retry-After"), "at least one second")
	p = problemOf(t, limited(-time.Hour, OTPResendGap))
	require.Equal(t, "1", p.Header.Get("Retry-After"), "a clock moved back")
	p = problemOf(t, limited(2*time.Hour, OTPWindow))
	require.Equal(t, "3600", p.Header.Get("Retry-After"), "never past the window")
	p = problemOf(t, limited(1500*time.Millisecond, OTPWindow))
	require.Equal(t, "2", p.Header.Get("Retry-After"), "rounded up")

	require.Nil(t, statusRefusal("active"))
	requireCode(t, statusRefusal("blocked"), genproblems.AccountBlocked)
	requireCode(t, statusRefusal("deleted"), genproblems.AccountBlocked)
	requireCode(t, statusRefusal("anything-new"), genproblems.AccountBlocked, "fails closed")
}

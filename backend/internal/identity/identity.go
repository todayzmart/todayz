// Package identity is sign-in and the signed-in user (W1-01; 01 §2, 02 §3,
// 04 §2.1–2.2, D-14, D-15, D-31, D-60): SMS OTP send and verify, sessions with
// rotating refresh tokens and reuse detection, logout and logout-all, admin
// TOTP (enrol, verify, step-up, recovery codes), the session check behind the
// access token (authn.SessionChecker), GET/PATCH /v1/me and account deletion
// (DELETE /v1/me with the hooks other domains register).
//
// Security rules this package keeps:
//
//   - A phone is stored sealed (pii.Sealer, field "phone") and found by its
//     keyed hash (pii.Hasher.Phone). An OTP is stored only as an HMAC bound to
//     its request id. A refresh token is stored only as its SHA-256.
//   - Codes and token hashes are compared with hmac.Equal.
//   - Nothing here logs a phone, an OTP, a token or a key; errors never carry
//     them either.
//   - The OTP limits (01 §2) are checked in Postgres under advisory locks, so
//     they hold across API instances.
package identity

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/awssms"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/testclock"
)

// OTP rules (01 §2 and the fixed table in 01 §17: product constants, not
// platform_settings keys).
const (
	OTPDigits       = 6
	OTPTTL          = 5 * time.Minute
	OTPMaxAttempts  = 5
	OTPResendGap    = 30 * time.Second
	OTPWindow       = time.Hour
	OTPPerPhone     = 5  // sends per number per OTPWindow
	OTPPerIP        = 20 // sends per client IP per OTPWindow
	PurposeLogin    = "login"
	PurposeStepUp   = "step_up" // re-confirms a signed-in user (DELETE /v1/me); never signs in
	testCode        = "000000"  // reserved test numbers only, local and test only (01 §2)
	refreshTokenLen = 32        // random bytes in a refresh token
)

// MaxActiveOrdersWhenLimited is the active-order cap while no-show strikes
// limit a customer (01 §5.4, C-21). The strike counts and days are settings
// (noshow_*); the cap itself is fixed.
const MaxActiveOrdersWhenLimited = 1

// Deps is what the service needs. Tokens may be nil (no JWT keys in a
// route-only test): sign-in then answers 503 service_unavailable.
type Deps struct {
	Config *config.Config
	DB     *db.DB
	Clock  clock.Clock
	Log    *slog.Logger
	Tokens *authn.Tokens
	// SMS and Human override the providers built from Config (tests).
	SMS   SMSSender
	Human HumanVerifier
	// Hooks seeds the account-deletion hooks (tests); domains register theirs
	// with AddDeletionCheck and AddDeletionHook.
	Hooks *Hooks
	// TestRoutes is true when the binary mounted the test clock, which it
	// does only when the database's platform_environment agrees with a local
	// or test APP_ENV (dbenv). The reserved test code needs it, a local or
	// test APP_ENV and a loopback API_ADDR: exactly the /v1/test/clock gate
	// (D-80), so a local binary pointed at a deployed database, or listening
	// on every interface, never accepts 000000.
	TestRoutes bool
}

// Service implements sign-in, sessions and /v1/me.
type Service struct {
	cfg    *config.Config
	db     *db.DB
	clock  clock.Clock
	log    *slog.Logger
	tokens *authn.Tokens
	sms    SMSSender
	human  HumanVerifier
	seal   *pii.Sealer
	hash   *pii.Hasher
	audit  *audit.Writer
	// totpSeal seals admin TOTP secrets under TOTP_ENC_KEY (field
	// "totp_secret"); nil when the key is unset (TOTP answers 503).
	totpSeal *pii.Sealer
	// hooksMu guards hooks: domains register at mount time, DELETE /v1/me
	// reads them per request.
	hooksMu sync.RWMutex
	hooks   Hooks
	// allowTestCode accepts 000000 for the reserved test numbers (Deps.TestRoutes).
	allowTestCode bool
}

// errUnavailable is why a service cannot serve (no database, no keys); the
// handlers answer 503 and the log names what is missing, never a value.
var errUnavailable = errors.New("identity: not configured")

// New builds the service. A missing database, PII key, JWT key or SMS
// provider does not fail here (the route-only tests build the router without
// them); the operations answer 503 instead. Preflight is what refuses to
// start the API without them in staging and prod (02 §8).
func New(d Deps) (*Service, error) {
	if d.Config == nil || d.Clock == nil {
		return nil, errors.New("identity: config and clock are required")
	}
	s := &Service{
		cfg: d.Config, db: d.DB, clock: d.Clock, log: d.Log, tokens: d.Tokens,
		sms: d.SMS, human: d.Human,
		allowTestCode: d.Config.Env.AllowsTestRoutes() && d.TestRoutes && testclock.Loopback(d.Config.APIAddr),
	}
	if s.sms == nil || s.human == nil {
		sms, human := providers(d.Config)
		if s.sms == nil {
			s.sms = sms
		}
		if s.human == nil {
			s.human = human
		}
	}
	if k := d.Config.Keys.PIIEncKey; k.IsSet() {
		seal, err := pii.NewSealer(k.Bytes())
		if err != nil {
			return nil, errors.New("identity: PII_ENC_KEY must be 32 bytes")
		}
		s.seal = seal
	}
	// Identity's own deletion check runs first (D-106).
	s.hooks.Can = append(s.hooks.Can, CanDeleteFunc(s.canDeleteWhileLimited))
	if d.Hooks != nil {
		s.hooks.Can = append(s.hooks.Can, d.Hooks.Can...)
		s.hooks.On = append(s.hooks.On, d.Hooks.On...)
	}
	if k := d.Config.Keys.TOTPEncKey; k.IsSet() {
		seal, err := pii.NewSealer(k.Bytes())
		if err != nil {
			return nil, errors.New("identity: TOTP_ENC_KEY must be 32 bytes")
		}
		s.totpSeal = seal
	}
	if k := d.Config.Keys.PIIHashKey; k.IsSet() {
		h, err := pii.NewHasher(k.Bytes())
		if err != nil {
			return nil, errors.New("identity: PII_HASH_KEY must be 32 bytes")
		}
		s.hash = h
	}
	if d.DB != nil {
		var ipKey []byte
		if d.Config.Keys.PIIHashKey.IsSet() {
			ipKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		s.audit = audit.New(d.DB, d.Clock, ipKey)
	}
	return s, nil
}

// Preflight is the start-up check of the API (02 §8: auth refuses to start
// without its provider): in staging and prod the PII, TOTP and JWT keys, the
// SMS provider (AWS region, DLT sender, entity and OTP template, D-212),
// and Turnstile must be configured, and
// TOTP_ENC_KEY must not be PII_ENC_KEY (one leaked key must not open both). Turnstile
// is required too (lead decision on the W1-01 review), replacing 02 §8's
// "stricter rate limits" fallback, which named no limits. It names
// variables, never values. New does not run it, so a router can be built
// from a bare config in route tests.
func Preflight(cfg *config.Config) error {
	if !cfg.Env.IsDeployed() {
		return nil
	}
	var missing []string
	k, p := cfg.Keys, cfg.Providers
	if !k.PIIEncKey.IsSet() {
		missing = append(missing, "PII_ENC_KEY")
	}
	if !k.PIIHashKey.IsSet() {
		missing = append(missing, "PII_HASH_KEY")
	}
	if !k.TOTPEncKey.IsSet() {
		missing = append(missing, "TOTP_ENC_KEY")
	}
	if len(k.JWTSigningKeys) == 0 {
		missing = append(missing, "JWT_SIGNING_KEYS")
	}
	// SMS is AWS End User Messaging (D-212): the instance role signs, so
	// there is no key, but the region and the DLT ids must be set.
	missing = append(missing, awssms.Missing(cfg)...)
	if p.SMSOTPTemplateID == "" {
		missing = append(missing, "SMS_OTP_TEMPLATE_ID")
	}
	if p.TurnstileAPIURL == "" {
		missing = append(missing, "TURNSTILE_API_URL")
	}
	if !p.TurnstileSecretKey.IsSet() {
		missing = append(missing, "TURNSTILE_SECRET_KEY")
	}
	if len(missing) > 0 {
		return fmt.Errorf("identity: required in staging and prod: %s", strings.Join(missing, ", "))
	}
	// D-212: a role-signed SMS goes only to the regional AWS endpoint
	// (config.Load refuses it too; this covers a config built another way).
	if f := awssms.Forbidden(cfg); len(f) > 0 {
		return fmt.Errorf("identity: must be unset in staging and prod: %s", strings.Join(f, ", "))
	}
	if subtle.ConstantTimeCompare(k.TOTPEncKey.Bytes(), k.PIIEncKey.Bytes()) == 1 {
		return errors.New("identity: TOTP_ENC_KEY must differ from PII_ENC_KEY")
	}
	return nil
}

// ready reports whether sign-in can run.
func (s *Service) ready() error {
	if s.db == nil || s.seal == nil || s.hash == nil {
		return errUnavailable
	}
	return nil
}

// readyTOTP is readyTokens plus the TOTP key.
func (s *Service) readyTOTP() error {
	if err := s.readyTokens(); err != nil {
		return err
	}
	if s.totpSeal == nil {
		return errUnavailable
	}
	return nil
}

// readyTokens also needs the token service.
func (s *Service) readyTokens() error {
	if err := s.ready(); err != nil {
		return err
	}
	if s.tokens == nil {
		return errUnavailable
	}
	return nil
}

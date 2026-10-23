// Package identity is sign-in and the signed-in user (W1-01; 01 §2, 02 §3,
// 04 §2.1–2.2, D-14, D-15): SMS OTP send and verify, sessions with rotating
// refresh tokens and reuse detection, logout, the session check behind the
// access token (authn.SessionChecker), and GET/PATCH /v1/me.
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
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/pii"
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
	testCode        = "000000" // reserved test numbers only, local and test only (01 §2)
	refreshTokenLen = 32       // random bytes in a refresh token
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
	// Hooks are the phase B account-deletion hooks; nil until W1-01 phase B.
	Hooks *Hooks
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
	hooks  *Hooks
	// allowTestCode accepts 000000 for the reserved test numbers (local and test).
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
		sms: d.SMS, human: d.Human, hooks: d.Hooks,
		allowTestCode: d.Config.Env.AllowsTestRoutes(),
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
// without its provider): in staging and prod the PII and JWT keys and the SMS
// provider must be configured. It names variables, never values. New does
// not run it, so a router can be built from a bare config in route tests.
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
	if len(k.JWTSigningKeys) == 0 {
		missing = append(missing, "JWT_SIGNING_KEYS")
	}
	if p.SMSAPIURL == "" {
		missing = append(missing, "SMS_API_URL")
	}
	if !p.MSG91AuthKey.IsSet() {
		missing = append(missing, "MSG91_AUTH_KEY")
	}
	if len(missing) > 0 {
		return fmt.Errorf("identity: required in staging and prod: %s", strings.Join(missing, ", "))
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

// Package config reads the process configuration from the environment only
// (never from files; the Makefile and Beanstalk export the variables).
//
// Rules (D-18, CLAUDE.md):
//   - A value is never printed, logged or put in an error. Missing or invalid
//     variables are reported by NAME only.
//   - APP_ENV is required and must be local, test, staging or prod.
//   - A missing app key (the D-18 table) is fatal in staging and prod. Locally a
//     missing key is a warning, so `go test` and `todayz version` work without one.
//   - Provider endpoints use TodayZ names (*_API_URL), never an SDK's default
//     variable (for example ANTHROPIC_BASE_URL), so a value exported by another
//     tool is never picked up.
package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// Env is the deployment environment.
type Env string

// The four environments (there is no "dev", D-15).
const (
	EnvLocal   Env = "local"
	EnvTest    Env = "test"
	EnvStaging Env = "staging"
	EnvProd    Env = "prod"
)

// Envs lists every valid environment.
var Envs = []Env{EnvLocal, EnvTest, EnvStaging, EnvProd}

// IsDeployed reports whether the environment is a cloud one (staging or prod).
func (e Env) IsDeployed() bool { return e == EnvStaging || e == EnvProd }

// AllowsTestRoutes reports whether /v1/test/* may be registered (D-80).
func (e Env) AllowsTestRoutes() bool { return e == EnvLocal || e == EnvTest }

// Valid reports whether e is one of the four environments.
func (e Env) Valid() bool { return slices.Contains(Envs, e) }

// AppKeyNames is the D-18 table, in order.
var AppKeyNames = []string{
	"JWT_SIGNING_KEYS",
	"JWT_CURRENT_KID",
	"ORDER_CODE_ENC_KEY",
	"ORDER_CODE_HMAC_KEY",
	"BILL_QR_SIGNING_KEY",
	"PII_ENC_KEY",
	"PII_HASH_KEY",
	"DEVICE_SECRET_ENC_KEY",
	"TOTP_ENC_KEY",
}

// SigningKey is one Ed25519 JWT signing key from JWT_SIGNING_KEYS.
type SigningKey struct {
	KID    string
	Public ed25519.PublicKey
	seed   Secret
}

// Private returns the private key. Call it only where tokens are signed.
func (k SigningKey) Private() ed25519.PrivateKey { return ed25519.NewKeyFromSeed(k.seed.Bytes()) }

// Format prints only the kid: fmt cannot see the unexported seed's own
// redaction, so the whole key redacts itself.
func (k SigningKey) Format(f fmt.State, _ rune) { _, _ = fmt.Fprintf(f, "SigningKey{KID:%s}", k.KID) }

// LogValue implements slog.LogValuer.
func (k SigningKey) LogValue() slog.Value { return slog.StringValue("SigningKey{KID:" + k.KID + "}") }

// AppKeys holds the D-18 keys. The 32-byte keys are decoded from standard base64.
type AppKeys struct {
	JWTSigningKeys     []SigningKey
	JWTCurrentKID      string
	OrderCodeEncKey    Secret
	OrderCodeHMACKey   Secret
	BillQRSigningKey   Secret
	PIIEncKey          Secret
	PIIHashKey         Secret
	DeviceSecretEncKey Secret
	TOTPEncKey         Secret
	// SharePreviewKey is SHARE_PREVIEW_KEY, the secret the customer Pages
	// Function sends with a link preview read so the API counts the visitor's
	// IP, not Cloudflare's (W3-16). make setup generates it locally like the
	// D-18 keys, but it is not in AppKeyNames: the API starts without it and
	// then never trusts the forwarded IP (previews count by Cloudflare IP).
	// Prod sets the same value on the Pages project and the API.
	SharePreviewKey Secret
}

// Providers holds provider endpoints and keys (D-17, D-19). Keys are optional
// locally; a channel works only when its settings flag is on and its key is set.
type Providers struct {
	// SMS is AWS End User Messaging SMS (D-212), signed with the EC2 instance
	// role when deployed, so there is no SMS key. SMSAPIURL overrides the
	// endpoint in local and test (the mocks server); staging and prod refuse
	// it and use the regional AWS endpoint. The DLT values are not secret.
	SMSAPIURL        string
	SMSSenderID      string // SMS_SENDER_ID: the DLT header, sent as the origination identity
	SMSDLTEntityID   string // SMS_DLT_ENTITY_ID: IN_ENTITY_ID on every send
	SMSOTPTemplateID string // SMS_OTP_TEMPLATE_ID: IN_TEMPLATE_ID of the OTP text
	// SMSNoticeTemplateIDs maps a notify catalogue template name (for
	// example tz_order_unseen_hi), or tz_owner_code for the field owner code
	// (identity), to its DLT template id
	// (SMS_NOTICE_TEMPLATE_IDS="name=id,name=id").
	SMSNoticeTemplateIDs map[string]string
	WhatsAppProvider     string
	WhatsAppAPIURL       string
	WhatsAppAPIKey       Secret
	PushAPIURL           string
	VAPIDPublicKey       string
	VAPIDPrivateKey      Secret
	TurnstileAPIURL      string
	TurnstileSiteKey     string
	TurnstileSecretKey   Secret
	STTAPIURL            string
	SarvamAPIKey         Secret
	LLMAPIURL            string
	AnthropicAPIKey      Secret
	MockLLM              bool
	GeocoderProvider     string
	GeocoderAPIKey       Secret
}

// Storage holds R2 (MinIO locally) settings.
type Storage struct {
	Endpoint        string
	AccountID       string
	AccessKeyID     Secret
	SecretAccessKey Secret
	BucketImages    string
	BucketPrivate   string
	BucketTiles     string
}

// Telemetry holds OTel and Sentry settings.
type Telemetry struct {
	OTLPEndpoint string // empty: no exporter
	OTLPHeaders  Secret // "k=v,k2=v2" (Grafana Cloud auth); may be empty
	SentryDSN    Secret // empty: Sentry off. Never used in local or test.
}

// Config is the whole process configuration.
type Config struct {
	Env          Env
	Version      string
	APIAddr      string
	WorkerAddr   string // health/readiness/version only
	APIPublicURL string
	LogLevel     slog.Level

	DatabaseURL        Secret
	DatabaseMigrateURL Secret

	// AWSRegion is AWS_REGION, for the AWS clients the app builds itself
	// (SMS, D-212). The app reads it here; no SDK default chain runs.
	AWSRegion string

	Keys      AppKeys
	Storage   Storage
	Telemetry Telemetry
	Providers Providers

	// NoEgress is true when TODAYZ_NO_EGRESS=1 or APP_ENV=test: outbound HTTP
	// may reach only loopback addresses (SC-PL-04).
	NoEgress bool

	// Shutdown holds the API drain timings from the environment (02 §2.8,
	// D-98). Zero means unset: lifecycle.APITimings applies the defaults and
	// checks them against WriteTimeout and the container stop timeout.
	Shutdown Shutdown

	// Missing lists the names of known variables that are unset (for logging).
	Missing []string
}

// Shutdown is the API's SIGTERM timing (02 §2.8). These are deploy settings
// read from the environment, not platform_settings keys (01 §17 has none).
type Shutdown struct {
	PreStop time.Duration // SHUTDOWN_PRESTOP: keep serving after /readyz turns 503
	Drain   time.Duration // SHUTDOWN_DRAIN: srv.Shutdown's budget for in-flight requests
}

// DefaultLocalAPIAddr is where `todayz api` listens locally (root CLAUDE.md port table).
const DefaultLocalAPIAddr = "127.0.0.1:8180"

// DefaultDeployedAPIAddr is where `todayz api` listens behind the ALB.
const DefaultDeployedAPIAddr = ":8080"

// DefaultLocalWorkerAddr is where `todayz worker` serves /healthz, /readyz
// and /version locally (loopback only; not in Caddy).
const DefaultLocalWorkerAddr = "127.0.0.1:8181"

// DefaultDeployedWorkerAddr is the worker's health port when deployed.
const DefaultDeployedWorkerAddr = ":8081"

// Error lists variable names that are missing or invalid. It never holds a value.
type Error struct {
	Missing []string
	Invalid []string // "NAME: reason"
}

func (e *Error) Error() string {
	var parts []string
	if len(e.Missing) > 0 {
		parts = append(parts, "missing: "+strings.Join(e.Missing, ", "))
	}
	if len(e.Invalid) > 0 {
		parts = append(parts, "invalid: "+strings.Join(e.Invalid, "; "))
	}
	return "config: " + strings.Join(parts, "; ")
}

// Getenv reads one variable; os.Getenv in production, a map in tests.
type Getenv func(string) string

// FromOS reads the process environment.
func FromOS() Getenv { return os.Getenv }

// Load reads and validates the configuration. It logs (on log, if not nil) the
// names of unset variables, never a value. It returns *Error when the
// configuration cannot be used.
func Load(getenv Getenv, log *slog.Logger) (*Config, error) {
	r := &reader{get: getenv}
	c := &Config{}

	env := Env(strings.TrimSpace(getenv("APP_ENV")))
	switch {
	case env == "":
		return nil, &Error{Missing: []string{"APP_ENV"}}
	case !env.Valid():
		return nil, &Error{Invalid: []string{"APP_ENV: must be local, test, staging or prod"}}
	}
	c.Env = env

	c.Version = r.str("APP_VERSION")
	c.APIPublicURL = r.url("API_PUBLIC_URL")
	c.LogLevel = r.level("LOG_LEVEL")
	c.APIAddr = r.addr("API_ADDR")
	if c.APIAddr == "" {
		c.APIAddr = DefaultLocalAPIAddr
		if env.IsDeployed() {
			c.APIAddr = DefaultDeployedAPIAddr
		}
	}
	c.WorkerAddr = r.addr("WORKER_ADDR")
	if c.WorkerAddr == "" {
		c.WorkerAddr = DefaultLocalWorkerAddr
		if env.IsDeployed() {
			c.WorkerAddr = DefaultDeployedWorkerAddr
		}
	}

	c.DatabaseURL = r.secret("DATABASE_URL")
	c.DatabaseMigrateURL = r.secret("DATABASE_MIGRATE_URL")

	c.Keys = r.appKeys()

	c.Storage = Storage{
		Endpoint:        r.url("R2_ENDPOINT"),
		AccountID:       r.str("R2_ACCOUNT_ID"),
		AccessKeyID:     r.secret("R2_ACCESS_KEY_ID"),
		SecretAccessKey: r.secret("R2_SECRET_ACCESS_KEY"),
		BucketImages:    r.str("R2_BUCKET_IMAGES"),
		BucketPrivate:   r.str("R2_BUCKET_PRIVATE"),
		BucketTiles:     r.str("R2_BUCKET_TILES"),
	}
	c.Telemetry = Telemetry{
		OTLPEndpoint: r.url("OTEL_EXPORTER_OTLP_ENDPOINT"),
		OTLPHeaders:  r.secret("OTEL_EXPORTER_OTLP_HEADERS"),
		SentryDSN:    r.secret("SENTRY_DSN"),
	}
	c.AWSRegion = r.awsRegion("AWS_REGION")
	c.Providers = Providers{
		SMSAPIURL:            r.url("SMS_API_URL"),
		SMSSenderID:          r.senderID("SMS_SENDER_ID"),
		SMSDLTEntityID:       r.dltID("SMS_DLT_ENTITY_ID"),
		SMSOTPTemplateID:     r.dltID("SMS_OTP_TEMPLATE_ID"),
		SMSNoticeTemplateIDs: r.dltTemplates("SMS_NOTICE_TEMPLATE_IDS"),
		WhatsAppProvider:     r.str("WHATSAPP_PROVIDER"),
		WhatsAppAPIURL:       r.url("WHATSAPP_API_URL"),
		WhatsAppAPIKey:       r.secret("WHATSAPP_API_KEY"),
		PushAPIURL:           r.url("PUSH_API_URL"),
		VAPIDPublicKey:       r.str("VAPID_PUBLIC_KEY"),
		VAPIDPrivateKey:      r.secret("VAPID_PRIVATE_KEY"),
		TurnstileAPIURL:      r.url("TURNSTILE_API_URL"),
		TurnstileSiteKey:     r.str("TURNSTILE_SITE_KEY"),
		TurnstileSecretKey:   r.secret("TURNSTILE_SECRET_KEY"),
		STTAPIURL:            r.url("STT_API_URL"),
		SarvamAPIKey:         r.secret("SARVAM_API_KEY"),
		LLMAPIURL:            r.url("LLM_API_URL"),
		AnthropicAPIKey:      r.secret("ANTHROPIC_API_KEY"),
		MockLLM:              r.boolDefault("MOCK_LLM", true),
		GeocoderProvider:     r.str("GEOCODER_PROVIDER"),
		GeocoderAPIKey:       r.secret("GEOCODER_API_KEY"),
	}
	if env.IsDeployed() && c.Providers.SMSAPIURL != "" {
		// D-212: staging and prod send to the regional AWS endpoint, signed by
		// the instance role; an override would hand that signed request (and
		// the phone and OTP) to another host.
		r.bad("SMS_API_URL", "must be unset in staging and prod")
	}
	c.NoEgress = r.boolDefault("TODAYZ_NO_EGRESS", false) || env == EnvTest
	c.Shutdown = Shutdown{PreStop: r.duration("SHUTDOWN_PRESTOP"), Drain: r.duration("SHUTDOWN_DRAIN")}

	sort.Strings(r.missing)
	c.Missing = r.missing

	var missingKeys []string
	for _, n := range AppKeyNames {
		if slices.Contains(r.missing, n) {
			missingKeys = append(missingKeys, n)
		}
	}
	if env.IsDeployed() {
		if !c.DatabaseURL.IsSet() {
			missingKeys = append(missingKeys, "DATABASE_URL")
		}
		if c.APIPublicURL == "" {
			missingKeys = append(missingKeys, "API_PUBLIC_URL")
		}
	}

	if log != nil && len(c.Missing) > 0 {
		log.Info("config: unset variables", slog.Any("names", c.Missing))
	}

	switch {
	case len(r.invalid) > 0:
		return nil, &Error{Invalid: r.invalid, Missing: deployedOnly(env, missingKeys)}
	case env.IsDeployed() && len(missingKeys) > 0:
		return nil, &Error{Missing: missingKeys}
	case len(missingKeys) > 0 && log != nil:
		log.Warn("config: app keys missing; run make setup", slog.Any("names", missingKeys))
	}
	return c, nil
}

func deployedOnly(env Env, names []string) []string {
	if env.IsDeployed() {
		return names
	}
	return nil
}

// CORSOrigins returns the exact browser origins allowed to call the API (02 §2.4 step 6).
func (c *Config) CORSOrigins() []string {
	switch c.Env {
	case EnvProd:
		return []string{"https://app.todayz.in", "https://business.todayz.in", "https://admin.todayz.in"}
	case EnvStaging:
		// Placeholder host names until Wave 5 fixes the staging domains.
		return []string{"https://app.staging.todayz.in", "https://business.staging.todayz.in", "https://admin.staging.todayz.in"}
	default:
		return []string{"https://app.todayz.localhost", "https://business.todayz.localhost", "https://admin.todayz.localhost"}
	}
}

// PublicCORSOrigins returns the marketing site origins that may call /v1/public/* without
// credentials (D-102). They never get a credentialed response on any other route.
func (c *Config) PublicCORSOrigins() []string {
	switch c.Env {
	case EnvProd:
		return []string{"https://todayz.in", "https://www.todayz.in"}
	case EnvStaging:
		// Placeholder host name, like CORSOrigins, until Wave 5 fixes the staging domains.
		return []string{"https://staging.todayz.in"}
	default:
		return []string{"https://todayz.localhost"}
	}
}

// CustomerAppOrigin is the customer app's origin, which share links point at
// (W3-16). It is the first of CORSOrigins.
func (c *Config) CustomerAppOrigin() string {
	return c.CORSOrigins()[0]
}

// ImagesOrigin is the public images origin (the R2 images bucket behind
// Cloudflare; MinIO through Caddy locally) that share card images are served
// from (W3-16).
func (c *Config) ImagesOrigin() string {
	switch c.Env {
	case EnvProd:
		return "https://img.todayz.in"
	case EnvStaging:
		// Placeholder host name, like CORSOrigins, until Wave 5 fixes the staging domains.
		return "https://img.staging.todayz.in"
	default:
		return "https://img.todayz.localhost"
	}
}

// ---------------------------------------------------------------- reader

type reader struct {
	get     Getenv
	missing []string
	invalid []string
}

func (r *reader) raw(name string) string {
	v := strings.TrimSpace(r.get(name))
	if v == "" {
		r.missing = append(r.missing, name)
	}
	return v
}

func (r *reader) bad(name, why string) { r.invalid = append(r.invalid, name+": "+why) }

func (r *reader) str(name string) string { return r.raw(name) }

func (r *reader) secret(name string) Secret {
	v := r.raw(name)
	if v == "" {
		return Secret{}
	}
	return NewSecret([]byte(v))
}

func (r *reader) url(name string) string {
	v := r.raw(name)
	if v == "" {
		return ""
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		r.bad(name, "must be an http(s) URL")
		return ""
	}
	return strings.TrimRight(v, "/")
}

func (r *reader) addr(name string) string {
	v := strings.TrimSpace(r.get(name)) // optional: not reported as missing
	if v == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(v); err != nil {
		r.bad(name, "must be host:port")
		return ""
	}
	return v
}

func (r *reader) level(name string) slog.Level {
	v := strings.ToLower(strings.TrimSpace(r.get(name)))
	switch v {
	case "", "info":
		return slog.LevelInfo
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	r.bad(name, "must be debug, info, warn or error")
	return slog.LevelInfo
}

// duration reads an optional Go duration ("15s", "1m15s"). Unset is zero and
// not reported as missing; a value must be above zero and at most an hour.
func (r *reader) duration(name string) time.Duration {
	v := strings.TrimSpace(r.get(name))
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 || d > time.Hour {
		r.bad(name, "must be a Go duration such as 15s, above zero and at most one hour")
		return 0
	}
	return d
}

func (r *reader) boolDefault(name string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(r.get(name))) {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	r.bad(name, "must be 0 or 1")
	return def
}

var (
	awsRegionRe   = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]$`)
	senderIDRe    = regexp.MustCompile(`^[A-Za-z0-9]{1,11}$`)
	dltIDRe       = regexp.MustCompile(`^[0-9]{1,32}$`)
	templateKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// awsRegion reads an AWS region name such as ap-south-1.
func (r *reader) awsRegion(name string) string {
	v := r.raw(name)
	if v != "" && !awsRegionRe.MatchString(v) {
		r.bad(name, "must be an AWS region such as ap-south-1")
		return ""
	}
	return v
}

// senderID reads an SMS sender ID (a DLT header: letters and digits, at most 11).
func (r *reader) senderID(name string) string {
	v := r.raw(name)
	if v != "" && !senderIDRe.MatchString(v) {
		r.bad(name, "must be 1 to 11 letters or digits")
		return ""
	}
	return v
}

// dltID reads a DLT entity or template id (TRAI's ids are numeric).
func (r *reader) dltID(name string) string {
	v := r.raw(name)
	if v != "" && !dltIDRe.MatchString(v) {
		r.bad(name, "must be a numeric DLT id")
		return ""
	}
	return v
}

// dltTemplates reads "name=id,name=id": a notify template name and its
// numeric DLT template id. Errors name the entry's position, never its text.
func (r *reader) dltTemplates(name string) map[string]string {
	v := r.raw(name)
	if v == "" {
		return nil
	}
	out := map[string]string{}
	for i, part := range strings.Split(v, ",") {
		k, id, ok := strings.Cut(strings.TrimSpace(part), "=")
		k, id = strings.TrimSpace(k), strings.TrimSpace(id)
		switch {
		case !ok || !templateKeyRe.MatchString(k) || !dltIDRe.MatchString(id):
			r.bad(name, fmt.Sprintf("entry %d must be name=id (a lowercase template name and a numeric DLT id)", i+1))
			return nil
		case out[k] != "":
			r.bad(name, fmt.Sprintf("entry %d repeats a template name", i+1))
			return nil
		}
		out[k] = id
	}
	return out
}

// key32 reads a 32-byte key written in standard base64.
func (r *reader) key32(name string) Secret {
	v := r.raw(name)
	if v == "" {
		return Secret{}
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil || len(b) != 32 {
		r.bad(name, "must be 32 bytes in standard base64")
		return Secret{}
	}
	return NewSecret(b)
}

type jwk struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	D   string `json:"d"`
}

func (r *reader) appKeys() AppKeys {
	k := AppKeys{
		OrderCodeEncKey:    r.key32("ORDER_CODE_ENC_KEY"),
		OrderCodeHMACKey:   r.key32("ORDER_CODE_HMAC_KEY"),
		BillQRSigningKey:   r.key32("BILL_QR_SIGNING_KEY"),
		PIIEncKey:          r.key32("PII_ENC_KEY"),
		PIIHashKey:         r.key32("PII_HASH_KEY"),
		DeviceSecretEncKey: r.key32("DEVICE_SECRET_ENC_KEY"),
		TOTPEncKey:         r.key32("TOTP_ENC_KEY"),
		SharePreviewKey:    r.key32("SHARE_PREVIEW_KEY"),
	}
	k.JWTCurrentKID = r.raw("JWT_CURRENT_KID")
	set := r.raw("JWT_SIGNING_KEYS")
	if set == "" {
		return k
	}
	keys, err := parseSigningKeys(set)
	if err != nil {
		r.bad("JWT_SIGNING_KEYS", err.Error())
		return k
	}
	k.JWTSigningKeys = keys
	if k.JWTCurrentKID != "" && !slices.ContainsFunc(keys, func(s SigningKey) bool { return s.KID == k.JWTCurrentKID }) {
		r.bad("JWT_CURRENT_KID", "not a kid in JWT_SIGNING_KEYS")
	}
	return k
}

// parseSigningKeys reads a JWK set of Ed25519 keys. Errors describe the shape
// only, never the content.
func parseSigningKeys(s string) ([]SigningKey, error) {
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal([]byte(s), &set); err != nil {
		return nil, errors.New("must be a JSON key set {\"keys\":[...]}")
	}
	if len(set.Keys) == 0 {
		return nil, errors.New("key set is empty")
	}
	seen := map[string]bool{}
	out := make([]SigningKey, 0, len(set.Keys))
	for i, j := range set.Keys {
		if j.Kty != "OKP" || j.Crv != "Ed25519" || j.Kid == "" {
			return nil, fmt.Errorf("key %d: must be kty OKP, crv Ed25519, with a kid", i)
		}
		if seen[j.Kid] {
			return nil, fmt.Errorf("key %d: duplicate kid", i)
		}
		seen[j.Kid] = true
		seed, err := base64.RawURLEncoding.DecodeString(j.D)
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("key %d: d must be a 32-byte seed in base64url", i)
		}
		pub, _ := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		if j.X != "" {
			x, err := base64.RawURLEncoding.DecodeString(j.X)
			if err != nil || !pub.Equal(ed25519.PublicKey(x)) {
				return nil, fmt.Errorf("key %d: x does not match d", i)
			}
		}
		out = append(out, SigningKey{KID: j.Kid, Public: pub, seed: NewSecret(seed)})
	}
	return out, nil
}

package config

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fullEnv returns a complete environment with fresh random keys (nothing secret
// is ever written in this file) plus the list of values that must never leak.
func fullEnv(t *testing.T, appEnv string) (map[string]string, []string) {
	t.Helper()
	rnd := func(n int) []byte {
		b := make([]byte, n)
		_, err := rand.Read(b)
		require.NoError(t, err)
		return b
	}
	seed := rnd(32)
	pub, _ := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	set, err := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "use": "sig", "kid": "k20261022-abc123",
		"x": base64.RawURLEncoding.EncodeToString(pub),
		"d": base64.RawURLEncoding.EncodeToString(seed),
	}}})
	require.NoError(t, err)

	env := map[string]string{
		"APP_ENV":                     appEnv,
		"APP_VERSION":                 "1.2.3",
		"API_PUBLIC_URL":              "https://api.todayz.localhost",
		"DATABASE_URL":                "postgres://u:" + base64.RawURLEncoding.EncodeToString(rnd(12)) + "@127.0.0.1:5433/todayz",
		"JWT_SIGNING_KEYS":            string(set),
		"JWT_CURRENT_KID":             "k20261022-abc123",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318",
		"SENTRY_DSN":                  "https://" + base64.RawURLEncoding.EncodeToString(rnd(12)) + "@sentry.invalid/1",
		"ANTHROPIC_API_KEY":           "sk-test-" + base64.RawURLEncoding.EncodeToString(rnd(24)),
	}
	secrets := []string{env["DATABASE_URL"], env["SENTRY_DSN"], env["ANTHROPIC_API_KEY"],
		base64.RawURLEncoding.EncodeToString(seed), fmt.Sprint(seed), fmt.Sprintf("%x", seed)} //nolint:staticcheck // the decimal byte list form, on purpose
	for _, n := range AppKeyNames[2:] {
		k := rnd(32)
		env[n] = base64.StdEncoding.EncodeToString(k)
		secrets = append(secrets, env[n], fmt.Sprintf("%x", k), strings.Trim(fmt.Sprint(k), "[]")) //nolint:staticcheck // the decimal byte list form, on purpose
	}
	return env, secrets
}

func get(m map[string]string) Getenv { return func(k string) string { return m[k] } }

func captureLog() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func requireNoLeak(t *testing.T, out string, secrets []string) {
	t.Helper()
	for _, s := range secrets {
		require.NotContains(t, out, s, "a secret value reached the output")
	}
}

func TestLoadFull(t *testing.T) {
	env, secrets := fullEnv(t, "local")
	log, buf := captureLog()
	c, err := Load(get(env), log)
	require.NoError(t, err)
	require.Equal(t, EnvLocal, c.Env)
	require.Equal(t, DefaultLocalAPIAddr, c.APIAddr)
	require.Len(t, c.Keys.JWTSigningKeys, 1)
	require.Equal(t, "k20261022-abc123", c.Keys.JWTSigningKeys[0].KID)
	require.Len(t, c.Keys.PIIEncKey.Bytes(), 32)
	require.True(t, c.Providers.MockLLM, "MOCK_LLM defaults to recorded fixtures")

	// The signing key works.
	k := c.Keys.JWTSigningKeys[0]
	sig := ed25519.Sign(k.Private(), []byte("x"))
	require.True(t, ed25519.Verify(k.Public, []byte("x"), sig))

	// Neither the log nor any formatting of the config shows a value.
	requireNoLeak(t, buf.String(), secrets)
	requireNoLeak(t, fmt.Sprintf("%v %+v %#v", c, c, c), secrets)
	b, err := json.Marshal(c)
	require.NoError(t, err)
	requireNoLeak(t, string(b), secrets)
	log.Info("config", slog.Any("cfg", c))
	requireNoLeak(t, buf.String(), secrets)

	// Unset optional names are logged by name.
	require.Contains(t, buf.String(), "SMS_SENDER_ID")
	require.NotContains(t, buf.String(), "MSG91", "D-212: the MSG91 names are gone")
}

// D-212: SMS goes through AWS End User Messaging; its config is non-secret
// DLT ids, checked for shape, and a bad value is named, never shown.
func TestSMSConfig(t *testing.T) {
	env, _ := fullEnv(t, "prod")
	env["AWS_REGION"] = "ap-south-1"
	env["SMS_SENDER_ID"] = "TODAYZ"
	env["SMS_DLT_ENTITY_ID"] = "1201160000000000001"
	env["SMS_OTP_TEMPLATE_ID"] = "1207160000000000101"
	env["SMS_NOTICE_TEMPLATE_IDS"] = " tz_order_unseen=1207160000000000201, tz_order_unseen_hi = 1207160000000000202 "
	c, err := Load(get(env), nil)
	require.NoError(t, err)
	require.Equal(t, "ap-south-1", c.AWSRegion)
	p := c.Providers
	require.Empty(t, p.SMSAPIURL, "deployed: the regional endpoint")
	require.Equal(t, "TODAYZ", p.SMSSenderID)
	require.Equal(t, "1201160000000000001", p.SMSDLTEntityID)
	require.Equal(t, "1207160000000000101", p.SMSOTPTemplateID)
	require.Equal(t, map[string]string{"tz_order_unseen": "1207160000000000201", "tz_order_unseen_hi": "1207160000000000202"}, p.SMSNoticeTemplateIDs)

	for _, c := range []struct{ name, value string }{
		{"AWS_REGION", "Mumbai-7q"},
		{"SMS_SENDER_ID", "TODAYZ-SMS-77"},
		{"SMS_SENDER_ID", "TODAY Z"},
		{"SMS_DLT_ENTITY_ID", "ent-4471"},
		{"SMS_OTP_TEMPLATE_ID", "1207x4471"},
		{"SMS_NOTICE_TEMPLATE_IDS", "tz_order_unseen"},
		{"SMS_NOTICE_TEMPLATE_IDS", "tz_order_unseen=abc4471"},
		{"SMS_NOTICE_TEMPLATE_IDS", "Tz_Order=4471"},
		{"SMS_NOTICE_TEMPLATE_IDS", "tz_a=4471,tz_a=4472"},
		{"SMS_NOTICE_TEMPLATE_IDS", "tz_a=4471,"},
	} {
		bad := maps.Clone(env)
		bad[c.name] = c.value
		_, err := Load(get(bad), nil)
		require.ErrorContains(t, err, c.name, c.value)
		require.NotContains(t, err.Error(), c.value, "the value is never shown")
		require.NotContains(t, err.Error(), "4471", "no part of the value is shown")
	}

	// D-212: staging and prod refuse any SMS_API_URL, even the real AWS
	// endpoint: the role-signed request goes only where the SDK resolves it.
	for _, e := range []string{"prod", "staging"} {
		for _, u := range []string{"https://sms-voice.ap-south-1.amazonaws.com/", "http://127.0.0.1:4010/sms-voice", "https://sms.example.com/4471"} {
			bad := maps.Clone(env)
			bad["APP_ENV"] = e
			bad["SMS_API_URL"] = u
			_, err := Load(get(bad), nil)
			var ce *Error
			require.ErrorAs(t, err, &ce, e+" "+u)
			require.Contains(t, ce.Invalid, "SMS_API_URL: must be unset in staging and prod")
			require.NotContains(t, err.Error(), u, "the value is never shown")
			require.NotContains(t, err.Error(), "4471")
		}
	}
	for _, e := range []string{"local", "test"} {
		ok, _ := fullEnv(t, e)
		ok["SMS_API_URL"] = "http://127.0.0.1:4010/sms-voice"
		c, err := Load(get(ok), nil)
		require.NoError(t, err, e)
		require.Equal(t, "http://127.0.0.1:4010/sms-voice", c.Providers.SMSAPIURL)
	}
}

func TestAppEnv(t *testing.T) {
	_, err := Load(get(map[string]string{}), nil)
	var ce *Error
	require.ErrorAs(t, err, &ce)
	require.Equal(t, []string{"APP_ENV"}, ce.Missing)

	_, err = Load(get(map[string]string{"APP_ENV": "dev"}), nil)
	require.ErrorAs(t, err, &ce)
	require.Contains(t, err.Error(), "APP_ENV")

	for _, e := range Envs {
		env, _ := fullEnv(t, string(e))
		c, err := Load(get(env), nil)
		require.NoError(t, err, e)
		require.Equal(t, e, c.Env)
	}
}

func TestMissingKeysFatalOnlyWhenDeployed(t *testing.T) {
	for _, e := range Envs {
		t.Run(string(e), func(t *testing.T) {
			env, secrets := fullEnv(t, string(e))
			delete(env, "PII_HASH_KEY")
			delete(env, "DEVICE_SECRET_ENC_KEY")
			log, buf := captureLog()
			c, err := Load(get(env), log)
			if e.IsDeployed() {
				var ce *Error
				require.ErrorAs(t, err, &ce)
				require.Equal(t, []string{"PII_HASH_KEY", "DEVICE_SECRET_ENC_KEY"}, ce.Missing)
				requireNoLeak(t, err.Error(), secrets)
				return
			}
			require.NoError(t, err)
			require.False(t, c.Keys.PIIHashKey.IsSet())
			require.Contains(t, buf.String(), "PII_HASH_KEY")
			requireNoLeak(t, buf.String(), secrets)
		})
	}
}

func TestDeployedNeedsDatabaseAndPublicURL(t *testing.T) {
	env, _ := fullEnv(t, "prod")
	delete(env, "DATABASE_URL")
	delete(env, "API_PUBLIC_URL")
	_, err := Load(get(env), nil)
	var ce *Error
	require.ErrorAs(t, err, &ce)
	require.ElementsMatch(t, []string{"DATABASE_URL", "API_PUBLIC_URL"}, ce.Missing)
	c, err := Load(get(map[string]string{"APP_ENV": "prod", "API_ADDR": ""}), nil)
	require.Error(t, err)
	require.Nil(t, c)
}

func TestInvalidValuesNamedNotShown(t *testing.T) {
	env, secrets := fullEnv(t, "local")
	bad := base64.StdEncoding.EncodeToString([]byte("sixteen-byte-key")) // wrong length
	env["ORDER_CODE_ENC_KEY"] = bad
	env["JWT_CURRENT_KID"] = "k-not-in-set"
	env["SMS_API_URL"] = "not a url"
	env["LOG_LEVEL"] = "loud"
	_, err := Load(get(env), nil)
	var ce *Error
	require.ErrorAs(t, err, &ce)
	msg := err.Error()
	for _, n := range []string{"ORDER_CODE_ENC_KEY", "JWT_CURRENT_KID", "SMS_API_URL", "LOG_LEVEL"} {
		require.Contains(t, msg, n)
	}
	require.NotContains(t, msg, bad)
	require.NotContains(t, msg, "k-not-in-set")
	requireNoLeak(t, msg, secrets)
}

func TestBadSigningKeySet(t *testing.T) {
	cases := map[string]string{
		"not json":    "nope",
		"empty":       `{"keys":[]}`,
		"wrong curve": `{"keys":[{"kty":"EC","crv":"P-256","kid":"a","d":"AA"}]}`,
		"short seed":  `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"a","d":"AAAA"}]}`,
		"no kid":      `{"keys":[{"kty":"OKP","crv":"Ed25519","d":"AAAA"}]}`,
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseSigningKeys(v)
			require.Error(t, err)
		})
	}
	// x that does not match d
	seed := make([]byte, 32)
	other := make([]byte, 32)
	other[0] = 1
	pub, _ := ed25519.NewKeyFromSeed(other).Public().(ed25519.PublicKey)
	v := fmt.Sprintf(`{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"a","d":%q,"x":%q}]}`,
		base64.RawURLEncoding.EncodeToString(seed), base64.RawURLEncoding.EncodeToString(pub))
	_, err := parseSigningKeys(v)
	require.ErrorContains(t, err, "x does not match d")
}

func TestNoEgressAndAddr(t *testing.T) {
	env, _ := fullEnv(t, "test")
	c, err := Load(get(env), nil)
	require.NoError(t, err)
	require.True(t, c.NoEgress, "APP_ENV=test always blocks egress")

	env, _ = fullEnv(t, "local")
	env["TODAYZ_NO_EGRESS"] = "1"
	env["API_ADDR"] = "127.0.0.1:9999"
	c, err = Load(get(env), nil)
	require.NoError(t, err)
	require.True(t, c.NoEgress)
	require.Equal(t, "127.0.0.1:9999", c.APIAddr)
	require.Equal(t, DefaultLocalWorkerAddr, c.WorkerAddr)
	require.Equal(t, []string{"https://app.todayz.localhost", "https://business.todayz.localhost", "https://admin.todayz.localhost"}, c.CORSOrigins())
	require.Equal(t, []string{"https://todayz.localhost"}, c.PublicCORSOrigins(), "the marketing site's lead form, without credentials (D-102)")

	env, _ = fullEnv(t, "prod")
	c, err = Load(get(env), nil)
	require.NoError(t, err)
	require.False(t, c.NoEgress)
	require.Equal(t, DefaultDeployedAPIAddr, c.APIAddr)
	require.Equal(t, DefaultDeployedWorkerAddr, c.WorkerAddr)
	require.Equal(t, []string{"https://app.todayz.in", "https://business.todayz.in", "https://admin.todayz.in"}, c.CORSOrigins())
	require.Equal(t, []string{"https://todayz.in", "https://www.todayz.in"}, c.PublicCORSOrigins())
	for _, o := range c.PublicCORSOrigins() {
		require.NotContains(t, c.CORSOrigins(), o, "the site origin never gets credentials (D-102)")
	}

	env["API_ADDR"] = "nope"
	_, err = Load(get(env), nil)
	require.ErrorContains(t, err, "API_ADDR")
}

// TestShutdownDurations (D-98): SHUTDOWN_PRESTOP and SHUTDOWN_DRAIN are
// optional Go durations; unset is zero (lifecycle applies the defaults) and is
// not reported missing; a bad value is named, never shown.
func TestShutdownDurations(t *testing.T) {
	env, _ := fullEnv(t, "prod")
	c, err := Load(get(env), nil)
	require.NoError(t, err)
	require.Zero(t, c.Shutdown)
	require.NotContains(t, c.Missing, "SHUTDOWN_PRESTOP")
	require.NotContains(t, c.Missing, "SHUTDOWN_DRAIN")

	env["SHUTDOWN_PRESTOP"] = "15s"
	env["SHUTDOWN_DRAIN"] = "1m15s"
	c, err = Load(get(env), nil)
	require.NoError(t, err)
	require.Equal(t, Shutdown{PreStop: 15 * time.Second, Drain: 75 * time.Second}, c.Shutdown)

	for _, bad := range []string{"75", "0s", "-1s", "2h", "soon-7d1f"} {
		env["SHUTDOWN_DRAIN"] = bad
		_, err = Load(get(env), nil)
		require.ErrorContains(t, err, "SHUTDOWN_DRAIN", bad)
		require.NotContains(t, err.Error(), bad, "the value is never shown")
	}
}

func TestSecretNeverPrints(t *testing.T) {
	s := NewSecret([]byte("value-that-must-not-print"))
	for _, out := range []string{
		fmt.Sprint(s), fmt.Sprintf("%s|%v|%+v|%#v|%q|%x", s, s, s, s, s, s),
		fmt.Sprintf("%v", struct{ S Secret }{s}),
	} {
		require.NotContains(t, out, "value-that-must-not-print")
		require.Contains(t, out, "[redacted]")
	}
	b, err := json.Marshal(map[string]Secret{"k": s})
	require.NoError(t, err)
	require.Equal(t, `{"k":"[redacted]"}`, string(b))

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "k", s)
	require.False(t, strings.Contains(buf.String(), "value-that-must-not-print"))

	require.Equal(t, "value-that-must-not-print", s.Reveal())
	require.True(t, s.IsSet())
	require.False(t, Secret{}.IsSet())
	require.False(t, NewSecret(nil).IsSet())
}

// fakeT records what a failing testify assertion would print.
type fakeT struct{ out strings.Builder }

func (f *fakeT) Errorf(format string, args ...any) { fmt.Fprintf(&f.out, format, args...) }

// TestSecretHiddenFromReflection: printers that walk struct fields by
// reflection (spew-style dumps in testify failure messages, %+v and %#v of a
// containing struct, slog of a struct) never reach the bytes.
func TestSecretHiddenFromReflection(t *testing.T) {
	const val = "value-that-must-not-print"
	s := NewSecret([]byte(val))
	type holder struct {
		Name string
		Key  Secret
		Ptr  *Secret
	}
	h := holder{Name: "x", Key: s, Ptr: &s}

	// No field of Secret holds the value: only a func.
	rv := reflect.ValueOf(s)
	for i := range rv.NumField() {
		require.Equal(t, reflect.Func, rv.Field(i).Kind(), "field %s", rv.Type().Field(i).Name)
	}

	// testify's failure output dumps both sides with reflection.
	ft := &fakeT{}
	assert.Equal(ft, h, holder{Name: "y"})
	assert.ObjectsAreEqual(h, h)
	require.NotEmpty(t, ft.out.String())
	require.NotContains(t, ft.out.String(), val)

	for _, out := range []string{
		fmt.Sprintf("%+v", h), fmt.Sprintf("%#v", h), fmt.Sprintf("%v", []any{h, &h}),
		fmt.Sprintf("%+v", rv.Interface()),
	} {
		require.NotContains(t, out, val)
	}
	b, err := json.Marshal(h)
	require.NoError(t, err)
	require.NotContains(t, string(b), val)

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "h", h, "s", s, "p", &s)
	require.NotContains(t, buf.String(), val)
	require.Equal(t, val, h.Ptr.Reveal())
}

// D-264: the admin authenticator is gone, and with it TOTP_ENC_KEY. A
// deployed config no longer needs it.
func TestNoAdminTOTPKey(t *testing.T) {
	require.NotContains(t, AppKeyNames, "TOTP_ENC_KEY")
	env, _ := fullEnv(t, "prod")
	delete(env, "TOTP_ENC_KEY")
	_, err := Load(get(env), nil)
	require.NoError(t, err)
}

// D-275: OTP_PROVIDER defaults to sms; the Firebase overrides (key URL,
// emulator) work in local and test and are refused when deployed.
func TestFirebaseConfig(t *testing.T) {
	env, _ := fullEnv(t, "local")
	c, err := Load(get(env), nil)
	require.NoError(t, err)
	require.Equal(t, OTPProviderSMS, c.Providers.OTPProvider, "unset means sms")

	env["OTP_PROVIDER"] = " Firebase "
	env["FIREBASE_PROJECT_ID"] = "todayz-local"
	env["FIREBASE_JWKS_API_URL"] = "http://127.0.0.1:4010/firebase/jwks/"
	env["FIREBASE_AUTH_EMULATOR_URL"] = "https://mocks.todayz.localhost/firebase"
	c, err = Load(get(env), nil)
	require.NoError(t, err)
	require.Equal(t, OTPProviderFirebase, c.Providers.OTPProvider)
	require.Equal(t, "todayz-local", c.Providers.Firebase.ProjectID)
	require.Equal(t, "http://127.0.0.1:4010/firebase/jwks", c.Providers.Firebase.JWKSAPIURL)

	env["OTP_PROVIDER"] = "carrier-pigeon"
	_, err = Load(get(env), nil)
	require.ErrorContains(t, err, "OTP_PROVIDER")
	require.NotContains(t, err.Error(), "carrier-pigeon")

	prod, _ := fullEnv(t, "prod")
	prod["FIREBASE_JWKS_API_URL"] = "https://keys.example.test/jwks"
	prod["FIREBASE_AUTH_EMULATOR_URL"] = "https://emulator.example.test"
	_, err = Load(get(prod), nil)
	require.ErrorContains(t, err, "FIREBASE_JWKS_API_URL")
	require.ErrorContains(t, err, "FIREBASE_AUTH_EMULATOR_URL")
}

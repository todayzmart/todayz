package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/reqctx"
)

// Test values only; none is a real person or credential.
const (
	testPhone   = "9876543210"
	testOTP     = "482913"
	testAddress = "12 MG Road, Bengaluru 560001"
	testPAN     = "ABCDE1234F"
	testGSTIN   = "29ABCDE1234F1Z5"
	testUPI     = "shop@okbank"
	testName    = "Ravi Kumar"
	testToken   = "eyJhbGciOiJFZERTQSJ9.eyJzdWIiOiIwMUpBIn0.c2lnbmF0dXJlLXZhbHVl" // gitleaks:allow: fake JWT, signature is the text "signature-value"
)

var piiValues = []string{testPhone, testOTP, testAddress, testPAN, testGSTIN, testUPI, testName, testToken, "98765 43210"}

func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return NewLogger(&buf, LogOptions{Level: slog.LevelDebug, Service: "api", Env: "test", Version: "t"}), &buf
}

func requireClean(t *testing.T, out string) {
	t.Helper()
	for _, v := range piiValues {
		require.NotContains(t, out, v, "PII reached the log")
	}
}

type signupForm struct {
	Phone   string            `json:"phone"`
	OTP     string            `json:"otp"`
	Address map[string]string `json:"address"`
	Note    string            `json:"note"`
	Items   []struct {
		Code string `json:"code"`
		Qty  int    `json:"qty"`
	} `json:"items"`
}

func TestRedactionDropsPII(t *testing.T) {
	log, buf := newTestLogger()
	ctx := reqctx.WithRequestID(context.Background(), "01JREQ")

	form := signupForm{Phone: testPhone, OTP: testOTP, Address: map[string]string{"line1": testAddress}, Note: "call +91 " + testPhone}
	form.Items = append(form.Items, struct {
		Code string `json:"code"`
		Qty  int    `json:"qty"`
	}{Code: testOTP, Qty: 2})

	log.InfoContext(ctx, "otp sent to "+testPhone+" code "+testOTP,
		slog.String("phone", testPhone),
		slog.String("otp", testOTP),
		slog.String("customerPhone", "+91-"+testPhone),
		slog.String("order_code", testOTP),
		slog.String("authorization", "Bearer "+testToken),
		slog.String("pan", testPAN),
		slog.String("gstin", testGSTIN),
		slog.String("upi_id", testUPI),
		slog.String("name", testName),
		slog.String("password", "hunter2-not-real"),
		slog.String("PII_ENC_KEY", "not-a-real-key-value"),
		slog.String("free_text", "reach me at 98765 43210 or token "+testToken),
		slog.Group("req", slog.String("otp", testOTP), slog.Group("delivery", slog.String("address", testAddress))),
		slog.Any("form", form),
		slog.Any("formPtr", &form),
		slog.Any("headers", map[string][]string{"Authorization": {"Bearer " + testToken}, "Cookie": {"__Secure-tz_rt_c=" + testToken}}),
		slog.Any("err", errors.New("sms to "+testPhone+" failed: otp "+testOTP)),
		slog.Any("raw", json.RawMessage(`{"otp":"`+testOTP+`","nested":{"phone":"`+testPhone+`"}}`)),
	)

	// Attributes bound earlier and groups opened earlier are cleaned too.
	log.With(slog.String("phone", testPhone)).WithGroup("address").Info("bound", slog.String("line1", testAddress))
	log.With(slog.Group("user", slog.String("name", testName))).Info("bound group")

	out := buf.String()
	requireClean(t, out)
	require.NotContains(t, out, "hunter2-not-real")
	require.NotContains(t, out, "not-a-real-key-value")
	require.Contains(t, out, Redacted)
	require.Contains(t, out, `"request_id":"01JREQ"`, "safe fields stay")
	require.Contains(t, out, `"qty":2`, "non-sensitive structured fields stay")
}

func TestOTPIsNeverLogged(t *testing.T) {
	// Every way an OTP could plausibly be handed to the logger.
	log, buf := newTestLogger()
	log.Info("verify", "otp", testOTP)
	log.Info("verify", slog.Int("otp", 482913))
	log.Info("verify", "OTP", testOTP, "otpCode", testOTP, "otp_code", testOTP, "login.otp", testOTP)
	log.Info("your otp is " + testOTP)
	log.Info("your OTP: " + testOTP)
	log.Info(fmt.Sprintf("code=%s", testOTP))
	log.Info("verify", slog.Any("req", map[string]any{"Otp": testOTP}))
	log.Info("verify", slog.Any("req", struct{ Otp string }{testOTP}))
	log.WithGroup("otp").Info("verify", "value", testOTP)
	require.NotContains(t, buf.String(), testOTP)
	require.NotContains(t, buf.String(), "482913")
}

func TestLoggerFields(t *testing.T) {
	log, buf := newTestLogger()
	log.WarnContext(reqctx.WithRequestID(context.Background(), "01JX"), "hello", slog.String("err_code", "not_found"), slog.Int("status", 404))
	var m map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &m))
	for _, k := range []string{"ts", "level", "msg", "service", "env", "version", "request_id", "err_code", "status"} {
		require.Contains(t, m, k)
	}
	require.Equal(t, "warn", m["level"])
	require.Equal(t, "not_found", m["err_code"], "err_code is on the safe list")
	require.NotContains(t, m, "time")
}

func TestIsSensitiveKey(t *testing.T) {
	for _, k := range []string{"phone", "customerPhone", "phone_e164", "otp", "OTPCode", "order_code", "access_token",
		"Authorization", "cookie", "password", "client_secret", "PII_ENC_KEY", "api_key", "apiKey", "address", "addressLine1",
		"pan", "gstin", "upi_id", "name", "storeName", "email", "lat", "sentry_dsn"} {
		require.True(t, IsSensitiveKey(k), k)
	}
	for _, k := range []string{"err_code", "status_code", "request_id", "trace_id", "route", "method", "status", "duration_ms",
		"user_kind", "role", "store_id", "service", "env", "version", "count", "names", "setting", "qty", "panel"} {
		require.False(t, IsSensitiveKey(k), k)
	}
}

func TestRedactString(t *testing.T) {
	cases := map[string]string{
		"call 9876543210 now":           "call [phone] now",
		"call +919876543210":            "call [phone]",
		"call +91 98765 43210":          "call [phone]",
		"call 09876543210":              "call [phone]",
		"Authorization: Bearer abc.def": "Authorization: [redacted] [redacted]",
		"sent with Bearer abc.def":      "sent with Bearer [redacted]",
		"otp 123456 sent":               "otp [redacted] sent",
		"password=pa55":                 "password=[redacted]",
		`{"token":"zzz"}`:               `{"token":"[redacted]"}`,
		"order 12345 total 4999":        "order 12345 total 4999",
		"status 200 in 1234ms":          "status 200 in 1234ms",
		"landline 1234567890":           "landline 1234567890",
	}
	for in, want := range cases {
		require.Equal(t, want, RedactString(in), in)
	}
}

func TestRedactNilAndScalars(t *testing.T) {
	require.Nil(t, redactAny(nil))
	var p *signupForm
	require.Nil(t, redactAny(p))
	require.Equal(t, 5, redactAny(5))
	require.Equal(t, Redacted, redactAny([]byte("raw")))
	require.True(t, strings.Contains(fmt.Sprint(redactAny([]string{"x", testPhone})), "[phone]"))
}

// TestRedactionGaps (T1): each row is one log call that leaked before the
// review fixes; none of the "never" values may reach the output.
func TestRedactionGaps(t *testing.T) {
	const dsnPassword = "hunter2-not-real"
	for _, c := range []struct {
		name  string
		log   func(*slog.Logger)
		never []string
		keep  []string
	}{
		{"phone-shaped int64 under a plain key", func(l *slog.Logger) { l.Info("lookup", slog.Int64("customer", 9876543210)) }, []string{testPhone}, []string{"[phone]"}},
		{"phone-shaped uint with 91", func(l *slog.Logger) { l.Info("lookup", slog.Uint64("n", 919876543210)) }, []string{"919876543210"}, nil},
		{"code-shaped int when the message says otp", func(l *slog.Logger) { l.Info("otp sent", slog.Int("value", 482913)) }, []string{testOTP}, nil},
		{"code-shaped int without code context stays", func(l *slog.Logger) { l.Info("order total", slog.Int("paise", 482913)) }, nil, []string{"482913"}},
		{"dsn password", func(l *slog.Logger) {
			l.Info("dial failed", "err", errors.New("connect postgres://todayz_app:"+dsnPassword+"@db.internal:5432/todayz"))
		}, []string{dsnPassword}, []string{"postgres://todayz_app:***@db.internal"}},
		{"bare code in a value, code word in the message", func(l *slog.Logger) { l.Info("otp delivery", "detail", "sent 482913 to user") }, []string{testOTP}, nil},
		{"bare code in the message", func(l *slog.Logger) { l.Info("sent 482913 to user, otp flow") }, []string{testOTP}, nil},
		{"query key and sig", func(l *slog.Logger) {
			l.Info("call", "url", "https://maps.example/geo?q=x&key=AIzaNotReal123&sig=abc9")
		}, []string{"AIzaNotReal123", "abc9"}, []string{"key=***"}},
		{"query token, auth, password, otp, secret, signature", func(l *slog.Logger) {
			l.Info("call", "url", "/cb?token=t0kx&auth=a1zz&password=p2zz&otp=482913&secret=s3zz&signature=g4zz")
		}, []string{"t0kx", "a1zz", "p2zz", testOTP, "s3zz", "g4zz"}, nil},
		{"phone after underscore", func(l *slog.Logger) { l.Info("cache miss", "k", "user_9876543210") }, []string{testPhone}, []string{"user_[phone]"}},
		{"email", func(l *slog.Logger) { l.Info("invite", "to", "ravi.k+shop@example.co.in") }, []string{"ravi.k+shop@example.co.in"}, []string{"[email]"}},
		{"pin next to a bare number", func(l *slog.Logger) { l.Info("reset", "note", "pin 4821 set") }, []string{"4821"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			log, buf := newTestLogger()
			c.log(log)
			out := buf.String()
			for _, v := range c.never {
				require.NotContains(t, out, v)
			}
			for _, v := range c.keep {
				require.Contains(t, out, v)
			}
		})
	}
}

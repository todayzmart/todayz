package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// SMSSender sends one OTP by SMS. The phone and the code are never logged,
// never put in a URL and never returned in an error.
type SMSSender interface {
	SendOTP(ctx context.Context, e164, code string) error
}

// HumanVerifier checks a Turnstile token (01 §2: Turnstile protects send OTP).
// ok false means the token failed; err means the check could not be made.
type HumanVerifier interface {
	Verify(ctx context.Context, token, remoteIP string) (ok bool, err error)
}

// ErrProvider is the error a provider call returns; the cause (status code,
// provider error type) is wrapped, never the request.
var ErrProvider = errors.New("identity: provider call failed")

// Cloudflare's published always-pass Turnstile test secret
// (developers.cloudflare.com/turnstile/troubleshooting/testing). A public
// dummy value, not a secret; used only in local and test against the mocks.
const turnstileTestSecret = "1x0000000000000000000000000000000AA"

// msg91OTPPath is MSG91's send-OTP path under SMS_API_URL
// (https://docs.msg91.com/otp/sendotp).
const msg91OTPPath = "/api/v5/otp"

// msg91 is the MSG91 OTP client (SMS_API_URL, MSG91_AUTH_KEY,
// MSG91_OTP_TEMPLATE_ID). Every parameter goes in the JSON body so the phone
// and the code never appear in a URL or an access log.
//
// TODO(lead): confirm the production request shape with MSG91. Their docs put
// template_id, mobile and otp in the query string; the mock (W0-07) accepts
// the body as well. Kept in the body until DLT registration (Wave 5).
type msg91 struct {
	client   *http.Client
	endpoint string
	authKey  string
	template string
}

func (m *msg91) SendOTP(ctx context.Context, e164, code string) error {
	body, err := json.Marshal(map[string]string{
		"mobile":      strings.TrimPrefix(e164, "+"),
		"otp":         code,
		"otp_length":  strconv.Itoa(OTPDigits),
		"otp_expiry":  strconv.Itoa(int(OTPTTL / time.Minute)),
		"template_id": m.template,
	})
	if err != nil {
		return fmt.Errorf("%w: msg91: encode", ErrProvider)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: msg91: request", ErrProvider)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("authkey", m.authKey)
	res, err := m.client.Do(req)
	if err != nil {
		// The error names the URL, which holds no phone or code.
		return fmt.Errorf("%w: msg91: %w", ErrProvider, err)
	}
	defer func() { _ = res.Body.Close() }()
	var out struct {
		Type string `json:"type"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&out)
	if res.StatusCode/100 != 2 || out.Type != "success" {
		return fmt.Errorf("%w: msg91: status %d type %q", ErrProvider, res.StatusCode, out.Type)
	}
	return nil
}

// turnstile is Cloudflare's siteverify client (TURNSTILE_API_URL).
type turnstile struct {
	client   *http.Client
	endpoint string
	secret   string
}

func (t *turnstile) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	if token == "" {
		return false, nil
	}
	form := url.Values{"secret": {t.secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return false, fmt.Errorf("%w: turnstile: request", ErrProvider)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := t.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("%w: turnstile: %w", ErrProvider, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%w: turnstile: status %d", ErrProvider, res.StatusCode)
	}
	var out struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&out); err != nil {
		return false, fmt.Errorf("%w: turnstile: decode", ErrProvider)
	}
	return out.Success, nil
}

// dropSMS stands in when SMS_API_URL is unset in local or test (the contract
// smoke runs without the mocks): it sends nothing and logs the variable name.
type dropSMS struct{}

func (dropSMS) SendOTP(ctx context.Context, _, _ string) error {
	telemetry.Logger(ctx).WarnContext(ctx, "identity: SMS_API_URL is unset; the OTP was not sent")
	return nil
}

// noSMS stands in when staging or prod has no SMS provider; every send fails.
type noSMS struct{}

func (noSMS) SendOTP(context.Context, string, string) error {
	return fmt.Errorf("%w: SMS provider not configured", ErrProvider)
}

// passHuman stands in for Turnstile when it is not configured: local and test
// without TURNSTILE_API_URL, or staging and prod without the key (02 §8:
// "stricter rate limits" instead).
type passHuman struct{ reason string }

func (p passHuman) Verify(ctx context.Context, _, _ string) (bool, error) {
	telemetry.Logger(ctx).DebugContext(ctx, "identity: turnstile check skipped", "reason", p.reason)
	return true, nil
}

// providers builds the SMS sender and the Turnstile verifier from config
// (02 §8 provider table). Deployed without SMS: refuse to start.
func providers(cfg *config.Config) (SMSSender, HumanVerifier) {
	client := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: cfg.NoEgress, Timeout: 10 * time.Second})
	p := cfg.Providers
	deployed := cfg.Env.IsDeployed()

	var sms SMSSender
	switch {
	case p.SMSAPIURL != "" && (p.MSG91AuthKey.IsSet() || !deployed):
		key := p.MSG91AuthKey.Reveal()
		if !p.MSG91AuthKey.IsSet() {
			key = "local-mock" // the mock checks presence only
		}
		sms = &msg91{client: client, endpoint: strings.TrimRight(p.SMSAPIURL, "/") + msg91OTPPath, authKey: key, template: p.MSG91OTPTemplateID}
	case deployed:
		// Preflight refuses to start the API like this (02 §8); a router
		// built without it (route tests) answers 503 on send.
		sms = noSMS{}
	default:
		sms = dropSMS{}
	}

	var human HumanVerifier
	switch {
	case p.TurnstileAPIURL != "" && p.TurnstileSecretKey.IsSet():
		human = &turnstile{client: client, endpoint: strings.TrimRight(p.TurnstileAPIURL, "/") + "/turnstile/v0/siteverify", secret: p.TurnstileSecretKey.Reveal()}
	case p.TurnstileAPIURL != "" && !deployed:
		human = &turnstile{client: client, endpoint: strings.TrimRight(p.TurnstileAPIURL, "/") + "/turnstile/v0/siteverify", secret: turnstileTestSecret}
	case deployed:
		// TODO(lead): 02 §8 says "stricter rate limits" when the Turnstile key is
		// missing in staging/prod but gives no numbers; the OTP limits stay as
		// they are until it does.
		human = passHuman{reason: "TURNSTILE_SECRET_KEY unset"}
	default:
		human = passHuman{reason: "TURNSTILE_API_URL unset"}
	}
	return sms, human
}

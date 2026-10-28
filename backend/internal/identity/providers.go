package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/todayz/backend/internal/platform/awssms"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/firebaseauth"
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

// otpText is the OTP SMS, exactly as registered on DLT (D-212; the template
// text is in docs/runbooks/aws-founder-steps.md, part C1). DLT refuses a send
// whose text differs from the template, so change it only with a new
// registration.
const otpText = "%s is your TodayZ login code. Do not share it with anyone."

// ownerCodeTemplate names the field owner code's DLT template in
// SMS_NOTICE_TEMPLATE_IDS, and ownerCodeText is its registered text (part C1
// of the founder steps). Until that id is set, the owner code goes out with
// the sign-in template (OwnerCodeSender).
const (
	ownerCodeTemplate = "tz_owner_code"
	ownerCodeText     = "%s is your TodayZ code to confirm your shop with our field agent. Share it only with that agent."
)

// awsOTP sends OTPs through AWS End User Messaging SMS (awssms, D-212) with
// the OTP's DLT template (SMS_OTP_TEMPLATE_ID). The phone and the code travel
// only in the signed JSON body.
type awsOTP struct {
	client   *awssms.Client
	template string
	text     string // the template's text with one %s for the code; otpText when empty
}

func (a *awsOTP) SendOTP(ctx context.Context, e164, code string) error {
	text := a.text
	if text == "" {
		text = otpText
	}
	_, err := a.client.Send(ctx, awssms.Message{
		To:         e164,
		Body:       fmt.Sprintf(text, code),
		TemplateID: a.template,
		TTL:        OTPTTL, // an OTP delivered after it expires is useless
	})
	if err != nil {
		// The awssms error carries the AWS error code and status only.
		return fmt.Errorf("%w: sms: %w", ErrProvider, err)
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
// Local and test never send to AWS itself.
type dropSMS struct{}

func (dropSMS) SendOTP(ctx context.Context, _, _ string) error {
	telemetry.Logger(ctx).WarnContext(ctx, "identity: SMS_API_URL is unset; the OTP was not sent")
	return nil
}

// noSMS stands in when the SMS provider cannot be built: in staging and prod
// (Preflight refuses to start the API like that), or in local and test when
// SMS_API_URL is set but the rest is missing or wrong, so a run against the
// mocks fails visibly instead of waiting for an OTP that never comes. Every
// send fails; reason names variables, never values.
type noSMS struct{ reason string }

func (n noSMS) SendOTP(context.Context, string, string) error {
	return fmt.Errorf("%w: SMS provider not configured: %s", ErrProvider, n.reason)
}

// noHuman stands in when staging or prod has no Turnstile; every check fails
// as a provider error (503), so no send goes out unchecked.
type noHuman struct{}

func (noHuman) Verify(context.Context, string, string) (bool, error) {
	return false, fmt.Errorf("%w: Turnstile not configured", ErrProvider)
}

// passHuman stands in for Turnstile in local and test without
// TURNSTILE_API_URL.
type passHuman struct{ reason string }

func (p passHuman) Verify(ctx context.Context, _, _ string) (bool, error) {
	telemetry.Logger(ctx).DebugContext(ctx, "identity: turnstile check skipped", "reason", p.reason)
	return true, nil
}

// providers builds the SMS sender and the Turnstile verifier from config
// (02 §8 provider table, D-212). Deployed without SMS, Preflight refuses to
// start; a service built anyway answers 503 on send.
func providers(cfg *config.Config) (SMSSender, HumanVerifier) {
	client := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: cfg.NoEgress, Timeout: 10 * time.Second})
	p := cfg.Providers
	deployed := cfg.Env.IsDeployed()

	var sms SMSSender
	c, err := awssms.New(cfg)
	switch {
	case err == nil && p.SMSOTPTemplateID != "":
		sms = &awsOTP{client: c, template: p.SMSOTPTemplateID}
	case !deployed && p.SMSAPIURL == "":
		sms = dropSMS{}
	case err != nil:
		sms = noSMS{reason: err.Error()}
	default:
		sms = noSMS{reason: "SMS_OTP_TEMPLATE_ID is unset"}
	}

	var human HumanVerifier
	switch {
	case p.TurnstileAPIURL != "" && p.TurnstileSecretKey.IsSet():
		human = &turnstile{client: client, endpoint: strings.TrimRight(p.TurnstileAPIURL, "/") + "/turnstile/v0/siteverify", secret: p.TurnstileSecretKey.Reveal()}
	case p.TurnstileAPIURL != "" && !deployed:
		human = &turnstile{client: client, endpoint: strings.TrimRight(p.TurnstileAPIURL, "/") + "/turnstile/v0/siteverify", secret: turnstileTestSecret}
	case deployed:
		// Preflight refuses to start without Turnstile; should a service be
		// built without it anyway, sends fail closed (503), never unchecked.
		human = noHuman{}
	default:
		human = passHuman{reason: "TURNSTILE_API_URL unset"}
	}
	return sms, human
}

// newFirebaseVerifier checks Firebase ID tokens against Google's public keys
// (in local and test, FIREBASE_JWKS_API_URL, the mocks server's), fetched
// through the egress guard (D-275).
func newFirebaseVerifier(cfg *config.Config, clk clock.Clock) *firebaseauth.Verifier {
	client := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: cfg.NoEgress, Timeout: 10 * time.Second})
	f := cfg.Providers.Firebase
	return firebaseauth.New(f.ProjectID, f.JWKSAPIURL, client, func() time.Time { return clk.Now().UTC() }, FirebaseMaxAuthAge)
}

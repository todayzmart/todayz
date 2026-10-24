// Package awssms sends SMS through AWS End User Messaging SMS
// (pinpoint-sms-voice-v2 SendTextMessage, D-212). Identity sends OTPs with it
// and notify sends SMS notices.
//
// India requires DLT registration: every send carries the registered sender
// ID (SMS_SENDER_ID) as the origination identity and the DLT entity and
// template ids as the IN_ENTITY_ID and IN_TEMPLATE_ID destination country
// parameters. The body must match the registered template text exactly.
//
// Credentials are explicit, never the SDK's default chain (no ~/.aws, no
// environment keys):
//
//   - staging and prod: the EC2 instance role through IMDSv2, with no IMDSv1
//     fallback, on the pinned metadata address and never through a proxy.
//     Requests go to the regional AWS endpoint only: SMS_API_URL must be
//     unset there, so a stray override can never receive a role-signed
//     request (session token, phone, OTP). No SMS key is stored anywhere.
//   - local and test: static dummy credentials, so requests are SigV4-signed
//     offline and go only to the mocks server at SMS_API_URL, which must be a
//     local address there.
//
// Every request goes through httpx.NewOutboundClient (egress guard), and the
// SDK never retries: a send is at most once, so a retry cannot text the same
// OTP or notice twice.
//
// Nothing here logs; errors carry the AWS error code and HTTP status only,
// never the phone, the body or the provider's message (which may echo them).
package awssms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/pinpointsmsvoicev2"
	"github.com/aws/aws-sdk-go-v2/service/pinpointsmsvoicev2/types"
	"github.com/aws/smithy-go"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/egress"
	"github.com/todayz/backend/internal/platform/httpx"
)

const (
	// LocalRegion signs local and test requests when AWS_REGION is unset
	// (infra/local/proc.sh strips AWS_* from the app processes, D-97).
	LocalRegion = "ap-south-1"
	// localKeyID and localSecret are dummy credentials for the mocks server,
	// which checks only that a SigV4 Authorization header is present. They
	// are not keys to anything.
	localKeyID  = "local-mock"
	localSecret = "local-mock" //nolint:gosec // G101: a public dummy value for the local mocks, not a credential
	// imdsEndpoint is pinned so AWS_EC2_METADATA_SERVICE_ENDPOINT cannot move it.
	imdsEndpoint = "http://169.254.169.254"
	sendTimeout  = 10 * time.Second
	// imdsTimeout bounds one metadata request. IMDS answers on the host in
	// milliseconds; a token PUT that never comes back (a hop limit of 1 seen
	// from a container) fails fast as "credentials: imds token timeout".
	imdsTimeout = 2 * time.Second

	// DLT destination country parameters (India).
	paramEntity   = "IN_ENTITY_ID"
	paramTemplate = "IN_TEMPLATE_ID"
	// TTL bounds AWS accepts for TimeToLive, in seconds.
	minTTL = 5
	maxTTL = 259200
)

// ErrSend is what every failed send wraps.
var ErrSend = errors.New("awssms: send failed")

// ErrNotConfigured is what New returns when a required variable is unset.
var ErrNotConfigured = errors.New("awssms: not configured")

// Message is one SMS. To is E.164 and Body the exact registered template text
// with its variables filled in; neither is ever logged or put in an error.
type Message struct {
	To         string
	Body       string
	TemplateID string        // the DLT template id (IN_TEMPLATE_ID)
	TTL        time.Duration // optional; AWS drops the message after it
}

// Client sends SMS through one regional End User Messaging endpoint.
type Client struct {
	api    *pinpointsmsvoicev2.Client
	sender string
	entity string
}

// Forbidden names the variables cfg sets that its environment must not have:
// SMS_API_URL in staging and prod (Preflight, integration status).
func Forbidden(cfg *config.Config) []string {
	if cfg.Env.IsDeployed() && cfg.Providers.SMSAPIURL != "" {
		return []string{"SMS_API_URL"}
	}
	return nil
}

// Missing names the variables a send needs that cfg does not have, in a fixed
// order (Preflight, integration status). Template ids are the caller's.
func Missing(cfg *config.Config) []string {
	var out []string
	p := cfg.Providers
	if cfg.Env.IsDeployed() {
		if cfg.AWSRegion == "" {
			out = append(out, "AWS_REGION")
		}
	} else if p.SMSAPIURL == "" {
		// Local and test never reach AWS: the mocks server or nothing.
		out = append(out, "SMS_API_URL")
	}
	if p.SMSSenderID == "" {
		out = append(out, "SMS_SENDER_ID")
	}
	if p.SMSDLTEntityID == "" {
		out = append(out, "SMS_DLT_ENTITY_ID")
	}
	return out
}

// New builds the client from cfg. It errors (naming variables, never values)
// when a variable is missing, when staging or prod sets SMS_API_URL, or when
// SMS_API_URL outside staging and prod is not a local address: role-signed
// requests go only to AWS, and dummy credentials never go to a real endpoint.
func New(cfg *config.Config) (*Client, error) { return newClient(cfg, nil) }

// wrapTransport lets the package tests stand in for the network: it gets
// each HTTP client's transport ("sms" or "imds") and returns the one to use.
// New passes nil.
type wrapTransport func(kind string, next http.RoundTripper) http.RoundTripper

func newClient(cfg *config.Config, wrap wrapTransport) (*Client, error) {
	if m := Missing(cfg); len(m) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotConfigured, strings.Join(m, ", "))
	}
	if f := Forbidden(cfg); len(f) > 0 {
		return nil, fmt.Errorf("%w: %s must be unset in staging and prod", ErrNotConfigured, strings.Join(f, ", "))
	}
	p := cfg.Providers
	deployed := cfg.Env.IsDeployed()
	httpClient := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: cfg.NoEgress, Timeout: sendTimeout})
	if wrap != nil {
		httpClient.Transport = wrap("sms", httpClient.Transport)
	}
	o := pinpointsmsvoicev2.Options{
		Region:     cfg.AWSRegion,
		HTTPClient: httpClient,
		Retryer:    aws.NopRetryer{},
	}
	if deployed {
		// The metadata client has its own transport: never a proxy (NO_PROXY
		// rarely lists 169.254.169.254), still behind the egress guard.
		imdsClient := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: cfg.NoEgress, Timeout: imdsTimeout, NoProxy: true})
		if wrap != nil {
			imdsClient.Transport = wrap("imds", imdsClient.Transport)
		}
		role := ec2rolecreds.New(func(eo *ec2rolecreds.Options) {
			eo.Client = imds.New(imds.Options{
				HTTPClient:        imdsClient,
				Endpoint:          imdsEndpoint,
				ClientEnableState: imds.ClientEnabled,
				EnableFallback:    aws.FalseTernary, // IMDSv2 tokens only
			})
		})
		o.Credentials = aws.NewCredentialsCache(tagCredentials{role})
	} else {
		if !isLocalURL(p.SMSAPIURL) {
			return nil, fmt.Errorf("%w: SMS_API_URL must be a local address outside staging and prod", ErrNotConfigured)
		}
		o.BaseEndpoint = aws.String(p.SMSAPIURL)
		if o.Region == "" {
			o.Region = LocalRegion
		}
		o.Credentials = credentials.NewStaticCredentialsProvider(localKeyID, localSecret, "")
	}
	return &Client{api: pinpointsmsvoicev2.New(o), sender: p.SMSSenderID, entity: p.SMSDLTEntityID}, nil
}

// tagCredentials marks every failure to get the role's credentials, so
// mapError can tell it from a failed send.
type tagCredentials struct{ next aws.CredentialsProvider }

func (t tagCredentials) Retrieve(ctx context.Context) (aws.Credentials, error) {
	c, err := t.next.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, &credentialsError{cause: err}
	}
	return c, nil
}

type credentialsError struct{ cause error }

func (e *credentialsError) Error() string { return "credentials: " + e.cause.Error() }
func (e *credentialsError) Unwrap() error { return e.cause }

// Send sends m once (no retry) and returns the AWS message id.
func (c *Client) Send(ctx context.Context, m Message) (string, error) {
	if m.TemplateID == "" {
		return "", &sendError{msg: "no DLT template id"}
	}
	if m.To == "" || m.Body == "" {
		return "", &sendError{msg: "empty recipient or body"}
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	in := &pinpointsmsvoicev2.SendTextMessageInput{
		DestinationPhoneNumber: aws.String(m.To),
		OriginationIdentity:    aws.String(c.sender),
		MessageBody:            aws.String(m.Body),
		MessageType:            types.MessageTypeTransactional,
		DestinationCountryParameters: map[string]string{
			paramEntity:   c.entity,
			paramTemplate: m.TemplateID,
		},
	}
	if m.TTL > 0 {
		in.TimeToLive = aws.Int32(int32(min(max(int64(m.TTL/time.Second), minTTL), maxTTL))) //nolint:gosec // G115: clamped to AWS's range
	}
	out, err := c.api.SendTextMessage(ctx, in)
	if err != nil {
		if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		return "", mapError(err)
	}
	return aws.ToString(out.MessageId), nil
}

var reErrorCode = regexp.MustCompile(`^[A-Za-z0-9_.]{1,64}$`)

// mapError keeps only the AWS error code and HTTP status of err; the message
// text is dropped, since AWS may echo the request back in it. A failure to
// get the instance role's credentials (IMDS) reads "credentials: <reason>",
// so the first send in a new environment says what is wrong.
func mapError(err error) error {
	// The IMDS token step drops the error chain (its wrapper has no Unwrap),
	// so the egress guard's error is only in the text there; it is put back.
	blocked := errors.Is(err, egress.ErrBlocked) || strings.Contains(err.Error(), egress.ErrBlocked.Error())
	if blocked {
		err = errors.Join(egress.ErrBlocked, err)
	}
	var ce *credentialsError
	if errors.As(err, &ce) {
		return &sendError{msg: "credentials: " + credentialsReason(err, blocked), cause: err}
	}
	if blocked {
		return &sendError{msg: "egress blocked", cause: err}
	}
	status := 0
	var hs interface{ HTTPStatusCode() int }
	if errors.As(err, &hs) {
		status = hs.HTTPStatusCode()
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		code := ae.ErrorCode()
		if !reErrorCode.MatchString(code) {
			code = "unknown"
		}
		return &sendError{msg: fmt.Sprintf("%s status %d", code, status), cause: err}
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &sendError{msg: "timeout", cause: err}
	case errors.Is(err, context.Canceled):
		return &sendError{msg: "cancelled", cause: err}
	case status != 0:
		return &sendError{msg: fmt.Sprintf("status %d", status), cause: err}
	default:
		return &sendError{msg: "transport", cause: err}
	}
}

// credentialsReason names why the role's credentials could not be had, from
// fixed words and a checked error code only. Some reasons are only in the
// SDK's error text (the token step drops the chain), so the SDK's fixed
// phrases "failed to get API token" and "no EC2 IMDS role found" are matched.
func credentialsReason(err error, blocked bool) string {
	text := err.Error()
	timeout := errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(text, "Client.Timeout exceeded") || strings.Contains(text, "deadline exceeded")
	switch {
	case blocked:
		return "egress blocked"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case strings.Contains(text, "failed to get API token"):
		if timeout {
			return "imds token timeout"
		}
		return "imds token refused"
	case timeout:
		return "imds timeout"
	case strings.Contains(text, "no EC2 IMDS role found"):
		return "no instance role"
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		code := ae.ErrorCode()
		if !reErrorCode.MatchString(code) {
			code = "unknown"
		}
		return "imds " + code
	}
	var hs interface{ HTTPStatusCode() int }
	if errors.As(err, &hs) {
		return fmt.Sprintf("imds status %d", hs.HTTPStatusCode())
	}
	return "imds unavailable"
}

// sendError is a failed send. Its text is built only from the fixed reasons
// above; the cause stays reachable for errors.Is (egress.ErrBlocked, the
// context errors) but is never printed.
type sendError struct {
	msg   string
	cause error
}

func (e *sendError) Error() string { return ErrSend.Error() + ": " + e.msg }

func (e *sendError) Unwrap() []error {
	if e.cause == nil {
		return []error{ErrSend}
	}
	return []error{ErrSend, e.cause}
}

// isLocalURL reports whether raw is an http(s) URL on a local host.
func isLocalURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host == "" {
		return false
	}
	return egress.IsLocalHost(u.Hostname())
}

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
//     fallback. No SMS key is stored anywhere.
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
// when a variable is missing, or when SMS_API_URL outside staging and prod is
// not a local address: dummy credentials never go to a real endpoint.
func New(cfg *config.Config) (*Client, error) {
	if m := Missing(cfg); len(m) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotConfigured, strings.Join(m, ", "))
	}
	p := cfg.Providers
	deployed := cfg.Env.IsDeployed()
	httpClient := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: cfg.NoEgress, Timeout: sendTimeout})
	o := pinpointsmsvoicev2.Options{
		Region:     cfg.AWSRegion,
		HTTPClient: httpClient,
		Retryer:    aws.NopRetryer{},
	}
	if p.SMSAPIURL != "" {
		o.BaseEndpoint = aws.String(p.SMSAPIURL)
	}
	if deployed {
		o.Credentials = aws.NewCredentialsCache(ec2rolecreds.New(func(eo *ec2rolecreds.Options) {
			eo.Client = imds.New(imds.Options{
				HTTPClient:        httpClient,
				Endpoint:          imdsEndpoint,
				ClientEnableState: imds.ClientEnabled,
				EnableFallback:    aws.FalseTernary, // IMDSv2 tokens only
			})
		}))
	} else {
		if !isLocalURL(p.SMSAPIURL) {
			return nil, fmt.Errorf("%w: SMS_API_URL must be a local address outside staging and prod", ErrNotConfigured)
		}
		if o.Region == "" {
			o.Region = LocalRegion
		}
		o.Credentials = credentials.NewStaticCredentialsProvider(localKeyID, localSecret, "")
	}
	return &Client{api: pinpointsmsvoicev2.New(o), sender: p.SMSSenderID, entity: p.SMSDLTEntityID}, nil
}

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
		return "", mapError(err)
	}
	return aws.ToString(out.MessageId), nil
}

var reErrorCode = regexp.MustCompile(`^[A-Za-z0-9_.]{1,64}$`)

// mapError keeps only the AWS error code and HTTP status of err; the message
// text is dropped, since AWS may echo the request back in it.
func mapError(err error) error {
	status := 0
	var hs interface{ HTTPStatusCode() int }
	if errors.As(err, &hs) {
		status = hs.HTTPStatusCode()
	}
	// The credential chain (IMDS) formats its cause with %v, so the egress
	// guard's error is only in the text there; it is put back in the chain.
	if errors.Is(err, egress.ErrBlocked) || strings.Contains(err.Error(), egress.ErrBlocked.Error()) {
		return &sendError{msg: "egress blocked", cause: errors.Join(egress.ErrBlocked, err)}
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

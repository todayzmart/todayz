package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/todayz/backend/internal/platform/awssms"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/notice"
)

// Var is one template variable, in the template's order.
type Var struct{ Name, Value string }

// Message is one outside message. To is E.164; it is never logged, never put
// in a URL and never returned in an error.
type Message struct {
	To       string
	Template string
	Lang     string
	Vars     []Var
	// Text is the rendered body. SMS sends it as is (it must match the DLT
	// template registered for Template, D-212); WhatsApp renders its own
	// registered template and uses Text for nothing.
	Text string
}

// Result is a provider's answer to one accepted message.
type Result struct {
	// ProviderID is the provider's request id (for its delivery reports).
	ProviderID string
	// Units is what the provider bills: SMS segments, WhatsApp messages.
	Units int
}

// Provider sends one outside message on one channel.
type Provider interface {
	// Name is the provider label (aws_sms, msg91), [a-z][a-z0-9_]*.
	Name() string
	Send(ctx context.Context, m Message) (Result, error)
}

// ErrProvider is what a provider call returns; the cause (status, provider
// error type) is wrapped, never the request.
var ErrProvider = errors.New("notify: provider call failed")

// The WhatsApp path under WHATSAPP_API_URL, as MSG91 and the local mock
// (e2e/mocks) serve it.
const (
	msg91WhatsAppPath = "/api/v5/whatsapp/whatsapp-outbound-message/bulk/"
	providerTimeout   = 10 * time.Second
	maxReply          = 64 << 10
)

// dltVarRunes is the most a DLT template variable ({#var#}) may hold in
// India; an SMS with a longer value does not match its template and is
// refused. Deliver cuts every SMS param to it before rendering.
const dltVarRunes = 30

// AWSSMS sends a transactional SMS through AWS End User Messaging SMS
// (awssms, D-212). The body is the rendered catalogue text, which must match
// the DLT template registered for the catalogue's template name; Templates
// maps that name to its DLT template id (SMS_NOTICE_TEMPLATE_IDS). The phone
// travels only in the signed JSON body.
type AWSSMS struct {
	Client    *awssms.Client
	Templates map[string]string // catalogue template name -> DLT template id
}

// Name implements Provider.
func (*AWSSMS) Name() string { return "aws_sms" }

// Send implements Provider.
func (p *AWSSMS) Send(ctx context.Context, m Message) (Result, error) {
	id, ok := p.Templates[m.Template]
	if !ok || id == "" {
		// The template name is a catalogue constant, never user data.
		return Result{}, fmt.Errorf("%w: aws sms: no DLT template id for %s in SMS_NOTICE_TEMPLATE_IDS", ErrProvider, m.Template)
	}
	msgID, err := p.Client.Send(ctx, awssms.Message{To: m.To, Body: m.Text, TemplateID: id})
	if err != nil {
		// The awssms error carries the AWS error code and status only.
		return Result{}, fmt.Errorf("%w: aws sms: %w", ErrProvider, err)
	}
	return Result{ProviderID: clip(msgID, 128), Units: smsSegments(m.Text)}, nil
}

// smsParams returns params with every value cut to dltVarRunes (trailing
// spaces dropped), so the rendered SMS fits its DLT template.
func smsParams(params map[string]string) map[string]string {
	out := make(map[string]string, len(params))
	for k, v := range params {
		if c := clipRunes(v, dltVarRunes); c != v {
			v = strings.TrimRightFunc(c, unicode.IsSpace)
		}
		out[k] = v
	}
	return out
}

// MSG91WhatsApp sends a WhatsApp template message through MSG91's bulk API
// (https://docs.msg91.com/whatsapp). IntegratedNumber is the business's
// WhatsApp sender number registered with MSG91.
type MSG91WhatsApp struct {
	Client           *http.Client
	BaseURL          string // WHATSAPP_API_URL
	AuthKey          string // WHATSAPP_API_KEY
	IntegratedNumber string
}

// Name implements Provider.
func (*MSG91WhatsApp) Name() string { return "msg91" }

// Send implements Provider.
func (p *MSG91WhatsApp) Send(ctx context.Context, m Message) (Result, error) {
	comps := map[string]map[string]string{}
	for i, v := range m.Vars {
		comps[fmt.Sprintf("body_%d", i+1)] = map[string]string{"type": "text", "value": v.Value}
	}
	body, err := json.Marshal(map[string]any{
		"integrated_number": p.IntegratedNumber,
		"content_type":      "template",
		"payload": map[string]any{
			"messaging_product": "whatsapp",
			"type":              "template",
			"template": map[string]any{
				"name":     m.Template,
				"language": map[string]string{"code": m.Lang, "policy": "deterministic"},
				"to_and_components": []map[string]any{{
					"to":         []string{strings.TrimPrefix(m.To, "+")},
					"components": comps,
				}},
			},
		},
	})
	if err != nil {
		return Result{}, fmt.Errorf("%w: msg91 whatsapp: encode", ErrProvider)
	}
	var out struct {
		Status string `json:"status"`
		Data   struct {
			RequestID string `json:"request_id"`
		} `json:"data"`
	}
	status, err := post(ctx, p.Client, strings.TrimRight(p.BaseURL, "/")+msg91WhatsAppPath, p.AuthKey, body, &out)
	if err != nil {
		return Result{}, fmt.Errorf("%w: msg91 whatsapp: %w", ErrProvider, err)
	}
	if status/100 != 2 || out.Status != "success" {
		return Result{}, fmt.Errorf("%w: msg91 whatsapp: status %d %q", ErrProvider, status, clip(out.Status, 20))
	}
	return Result{ProviderID: clip(out.Data.RequestID, 128), Units: 1}, nil
}

// post sends body as JSON with MSG91's authkey header (WhatsApp) and decodes the reply
// into out (a reply that is not JSON leaves out empty). The error names the
// URL (no phone is ever in it) and never the body.
func post(ctx context.Context, c *http.Client, url, key string, body []byte, out any) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, errors.New("request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("authkey", key)
	res, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = res.Body.Close() }()
	_ = json.NewDecoder(io.LimitReader(res.Body, maxReply)).Decode(out)
	return res.StatusCode, nil
}

// smsSegments estimates the SMS parts of text: GSM-7 160 (153 per part when
// split), otherwise UCS-2 70 (67). AWS bills per part, so this is the cost
// metric's unit count.
func smsSegments(text string) int {
	n := utf8.RuneCountInString(text)
	single, multi := 160, 153
	for _, r := range text {
		if r > 0x7f {
			single, multi = 70, 67
			break
		}
	}
	switch {
	case n == 0:
		return 1
	case n <= single:
		return 1
	default:
		return (n + multi - 1) / multi
	}
}

// clip bounds a provider-supplied string kept in a row or an error.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// providersFromConfig builds the outside channels' providers (02 §8). A
// channel without one is "not configured": Send skips it and the integration
// status says which variable is missing.
//
//   - SMS: AWS End User Messaging (D-212) when awssms can be built: in staging
//     and prod AWS_REGION, SMS_SENDER_ID and SMS_DLT_ENTITY_ID (the instance
//     role signs); in local and test SMS_API_URL on a local host (the mocks)
//     instead of the region. SMS_NOTICE_TEMPLATE_IDS maps each catalogue
//     template to its DLT id; an unmapped template fails its delivery.
//   - WhatsApp: not wired. TODO(lead): MSG91's WhatsApp API needs the
//     integrated (sender) number, which config does not carry yet (for
//     example WHATSAPP_SENDER_NUMBER); MSG91WhatsApp is ready and tested
//     against httptest once it does.
//   - Push: deferred (the VAPID sender is a later slice).
func providersFromConfig(cfg *config.Config) map[notice.Channel]Provider {
	out := map[notice.Channel]Provider{}
	if cfg == nil {
		return out
	}
	if c, err := awssms.New(cfg); err == nil {
		out[notice.SMS] = &AWSSMS{Client: c, Templates: cfg.Providers.SMSNoticeTemplateIDs}
	}
	return out
}

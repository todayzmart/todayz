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
	"unicode/utf8"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
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
	// Text is the rendered body, for the segment count only (the provider
	// renders its own registered template).
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
	// Name is the provider label (msg91), [a-z][a-z0-9_]*.
	Name() string
	Send(ctx context.Context, m Message) (Result, error)
}

// ErrProvider is what a provider call returns; the cause (status, provider
// error type) is wrapped, never the request.
var ErrProvider = errors.New("notify: provider call failed")

// Provider paths under the configured base URLs (SMS_API_URL,
// WHATSAPP_API_URL), as MSG91 and the local mock (e2e/mocks) serve them.
const (
	msg91FlowPath     = "/api/v5/flow"
	msg91WhatsAppPath = "/api/v5/whatsapp/whatsapp-outbound-message/bulk/"
	// mockAuthKey stands in for MSG91_AUTH_KEY in local and test: the mock
	// checks that the header is present, never its value.
	mockAuthKey     = "local-mock"
	providerTimeout = 10 * time.Second
	maxReply        = 64 << 10
)

// MSG91SMS sends a transactional SMS through MSG91's flow API
// (https://docs.msg91.com/sms/send-sms). The phone travels only in the JSON
// body.
type MSG91SMS struct {
	Client   *http.Client
	BaseURL  string // SMS_API_URL
	AuthKey  string // MSG91_AUTH_KEY
	SenderID string // MSG91_SENDER_ID
}

// Name implements Provider.
func (*MSG91SMS) Name() string { return "msg91" }

// Send implements Provider.
func (p *MSG91SMS) Send(ctx context.Context, m Message) (Result, error) {
	rcpt := map[string]string{}
	for _, v := range m.Vars {
		rcpt[v.Name] = v.Value
	}
	rcpt["mobiles"] = strings.TrimPrefix(m.To, "+")
	body, err := json.Marshal(map[string]any{
		"template_id": m.Template,
		"sender":      p.SenderID,
		"short_url":   "0",
		"recipients":  []map[string]string{rcpt},
	})
	if err != nil {
		return Result{}, fmt.Errorf("%w: msg91 sms: encode", ErrProvider)
	}
	var out struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	status, err := post(ctx, p.Client, strings.TrimRight(p.BaseURL, "/")+msg91FlowPath, p.AuthKey, body, &out)
	if err != nil {
		return Result{}, fmt.Errorf("%w: msg91 sms: %w", ErrProvider, err)
	}
	if status/100 != 2 || out.Type != "success" {
		return Result{}, fmt.Errorf("%w: msg91 sms: status %d type %q", ErrProvider, status, clip(out.Type, 20))
	}
	return Result{ProviderID: clip(out.Message, 128), Units: smsSegments(m.Text)}, nil
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

// post sends body as JSON with MSG91's authkey header and decodes the reply
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
// split), otherwise UCS-2 70 (67). The provider bills its registered
// template, so this is an estimate for the cost metric.
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
//   - SMS: MSG91 when SMS_API_URL is set and MSG91_AUTH_KEY is set (in local
//     and test the key may be missing: the mock checks presence only).
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
	p := cfg.Providers
	deployed := cfg.Env.IsDeployed()
	if p.SMSAPIURL != "" && (p.MSG91AuthKey.IsSet() || !deployed) {
		key := mockAuthKey
		if p.MSG91AuthKey.IsSet() {
			key = p.MSG91AuthKey.Reveal()
		}
		client := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: cfg.NoEgress, Timeout: providerTimeout})
		out[notice.SMS] = &MSG91SMS{Client: client, BaseURL: p.SMSAPIURL, AuthKey: key, SenderID: p.MSG91SenderID}
	}
	return out
}

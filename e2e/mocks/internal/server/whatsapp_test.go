package server

import (
	"net/url"
	"regexp"
	"testing"
)

func TestWhatsAppMSG91Bulk(t *testing.T) {
	h := newHarness(t, nil)
	body := map[string]any{
		"integrated_number": "919000000050",
		"content_type":      "template",
		"payload": map[string]any{
			"messaging_product": "whatsapp",
			"type":              "template",
			"template": map[string]any{
				"name":      "order_ready",
				"language":  map[string]any{"code": "en", "policy": "deterministic"},
				"namespace": nil,
				"to_and_components": []map[string]any{{
					"to": []string{"919000000008"},
					"components": map[string]any{
						"body_2": map[string]any{"type": "text", "value": "Ravi Stores"},
						"body_1": map[string]any{"type": "text", "value": "TZ-1042"},
					},
				}},
			},
		},
	}
	for _, path := range []string{
		"/whatsapp/api/v5/whatsapp/whatsapp-outbound-message/bulk/",
		"/whatsapp/api/v5/whatsapp/whatsapp-outbound-message/bulk",
	} {
		rr := h.postJSON(path, body, msg91Auth_)
		wantStatus(t, rr, 200)
		m := decode(t, rr)
		wantKeys(t, m, "status", "hasError", "data", "errors")
		if m["status"] != "success" || m["hasError"] != false {
			t.Errorf("response = %v", m)
		}
		wantKeys(t, obj(t, m["data"]), "message", "request_id")
	}
	w := h.srv.Store().WhatsApp.List(nil, 1)[0]
	if w.Provider != "msg91" || w.Kind != "template" || w.To != "+919000000008" || w.Template != "order_ready" || w.Language != "en" {
		t.Errorf("capture = %+v", w)
	}
	if w.Body != "Your order TZ-1042 at Ravi Stores is ready for pickup." {
		t.Errorf("body = %q (components must be ordered body_1, body_2)", w.Body)
	}
}

func TestWhatsAppMSG91Errors(t *testing.T) {
	h := newHarness(t, nil)
	rr := h.postJSON("/whatsapp/api/v5/whatsapp/whatsapp-outbound-message/bulk/", `{}`, nil)
	wantStatus(t, rr, 401)
	m := decode(t, rr)
	wantKeys(t, m, "status", "hasError", "data", "errors")
	if m["hasError"] != true || m["status"] != "fail" {
		t.Errorf("error = %v", m)
	}
	for name, body := range map[string]string{
		"no number":   `{"payload":{"template":{"name":"x","to_and_components":[{"to":["919000000001"]}]}}}`,
		"no template": `{"integrated_number":"91900","payload":{"template":{"to_and_components":[{"to":["919000000001"]}]}}}`,
		"no to":       `{"integrated_number":"91900","payload":{"template":{"name":"x"}}}`,
		"bad json":    `{`,
	} {
		t.Run(name, func(t *testing.T) {
			wantStatus(t, h.postJSON("/whatsapp/api/v5/whatsapp/whatsapp-outbound-message/bulk/", body, msg91Auth_), 400)
		})
	}
	if h.srv.Store().WhatsApp.Len() != 0 {
		t.Error("errors must not be captured")
	}
}

func TestWhatsAppMSG91Session(t *testing.T) {
	h := newHarness(t, nil)
	rr := h.postJSON("/whatsapp/api/v5/whatsapp/whatsapp-outbound-message/", map[string]any{
		"integrated_number": "919000000050",
		"recipient_number":  "919000000009",
		"content_type":      "text",
		"text":              "Your order is on the way.",
	}, msg91Auth_)
	wantStatus(t, rr, 200)
	wantKeys(t, decode(t, rr), "status", "hasError", "data", "errors")
	w := h.srv.Store().WhatsApp.List(nil, 1)[0]
	if w.Kind != "text" || w.Body != "Your order is on the way." || w.To != "+919000000009" {
		t.Errorf("capture = %+v", w)
	}
	wantStatus(t, h.postJSON("/whatsapp/api/v5/whatsapp/whatsapp-outbound-message", `{"integrated_number":"91900"}`, msg91Auth_), 400)
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestWhatsAppGupshup(t *testing.T) {
	h := newHarness(t, nil)
	key := map[string]string{"apikey": "test-placeholder-apikey"}
	rr := h.postForm("/whatsapp/wa/api/v1/msg", url.Values{
		"channel":     {"whatsapp"},
		"source":      {"919000000050"},
		"destination": {"919000000010"},
		"src.name":    {"TodayZ"},
		"message":     {`{"type":"text","text":"Your order TZ-7 is ready."}`},
	}, key)
	wantStatus(t, rr, 202)
	m := decode(t, rr)
	wantKeys(t, m, "status", "messageId")
	if m["status"] != "submitted" || !uuidRe.MatchString(m["messageId"].(string)) {
		t.Errorf("response = %v", m)
	}
	w := h.srv.Store().WhatsApp.List(nil, 1)[0]
	if w.Provider != "gupshup" || w.Body != "Your order TZ-7 is ready." || w.To != "+919000000010" {
		t.Errorf("capture = %+v", w)
	}

	rr = h.postForm("/whatsapp/wa/api/v1/template/msg", url.Values{
		"source":      {"919000000050"},
		"destination": {"919000000011"},
		"template":    {`{"id":"order_update","params":["TZ-8","ready"]}`},
	}, key)
	wantStatus(t, rr, 202)
	if w := h.srv.Store().WhatsApp.List(nil, 1)[0]; w.Body != "Order TZ-8 is now ready." || w.Template != "order_update" {
		t.Errorf("template capture = %+v", w)
	}
}

func TestWhatsAppGupshupErrors(t *testing.T) {
	h := newHarness(t, nil)
	key := map[string]string{"apikey": "test-placeholder-apikey"}
	ok := url.Values{"source": {"919000000050"}, "destination": {"919000000010"}, "message": {"hi"}}
	rr := h.postForm("/whatsapp/wa/api/v1/msg", ok, nil)
	wantStatus(t, rr, 401)
	m := decode(t, rr)
	wantKeys(t, m, "status", "message")
	if m["status"] != "error" {
		t.Errorf("error = %v", m)
	}
	cases := map[string]url.Values{
		"no source":      {"destination": {"919000000010"}, "message": {"hi"}},
		"no destination": {"source": {"919000000050"}, "message": {"hi"}},
		"no message":     {"source": {"919000000050"}, "destination": {"919000000010"}},
		"bad channel":    {"channel": {"sms"}, "source": {"919000000050"}, "destination": {"919000000010"}, "message": {"hi"}},
	}
	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			wantStatus(t, h.postForm("/whatsapp/wa/api/v1/msg", form, key), 400)
		})
	}
	bad := url.Values{"source": {"919000000050"}, "destination": {"919000000010"}, "template": {"nope"}}
	wantStatus(t, h.postForm("/whatsapp/wa/api/v1/template/msg", bad, key), 400)
	// Plain-text message (not JSON) is accepted as the body.
	wantStatus(t, h.postForm("/whatsapp/wa/api/v1/msg", ok, key), 202)
	if w := h.srv.Store().WhatsApp.List(nil, 1)[0]; w.Body != "hi" {
		t.Errorf("plain body = %q", w.Body)
	}
}

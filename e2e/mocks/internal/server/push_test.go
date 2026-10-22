package server

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"todayz.local/e2e/mocks/internal/capture"
)

func pushReq(sub string, body []byte, headers map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/push/"+sub, bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func TestPushHappy(t *testing.T) {
	h := newHarness(t, nil)
	payload := bytes.Repeat([]byte{0xAB}, 180) // stands in for an aes128gcm record
	rr := h.do(pushReq("sub-abc123", payload, map[string]string{
		"TTL":              "86400",
		"Urgency":          "high",
		"Topic":            "order-TZ1042",
		"Content-Encoding": "aes128gcm",
		"Content-Type":     "application/octet-stream",
		"Authorization":    "vapid t=placeholder, k=placeholder",
		PlaintextHeader:    `{"title":"Order ready","body":"Pick up at Ravi Stores","url":"/orders/TZ-1042"}`,
	}))
	wantStatus(t, rr, http.StatusCreated)
	if loc := rr.Header().Get("Location"); !strings.HasPrefix(loc, "/push/_messages/") {
		t.Errorf("Location = %q", loc)
	}
	if rr.Header().Get("TTL") != "86400" {
		t.Errorf("TTL echo = %q", rr.Header().Get("TTL"))
	}
	p := h.srv.Store().Push.List(nil, 1)[0]
	if p.Subscription != "sub-abc123" || p.Status != 201 || p.TTL != "86400" || p.Urgency != "high" ||
		p.Topic != "order-TZ1042" || p.PayloadBytes != 180 || !p.VAPID || p.ContentEncoding != "aes128gcm" {
		t.Errorf("capture = %+v", p)
	}
	if p.Title != "Order ready" || p.Body != "Pick up at Ravi Stores" {
		t.Errorf("plaintext = %q / %q", p.Title, p.Body)
	}
}

func TestPushPlaintextForms(t *testing.T) {
	h := newHarness(t, nil)
	nested := `{"notification":{"title":"Offer","body":"10% off atta"}}`
	for _, hv := range []string{
		base64.StdEncoding.EncodeToString([]byte(nested)),
		base64.RawURLEncoding.EncodeToString([]byte(nested)),
		nested,
	} {
		rr := h.do(pushReq("s1", nil, map[string]string{"TTL": "0", PlaintextHeader: hv}))
		wantStatus(t, rr, 201)
		p := h.srv.Store().Push.List(nil, 1)[0]
		if p.Title != "Offer" || p.Body != "10% off atta" || p.Plaintext != nested {
			t.Errorf("header %q -> %+v", hv, p)
		}
	}
	// No plaintext header: delivered, nothing to show.
	wantStatus(t, h.do(pushReq("s2", []byte{1, 2, 3}, map[string]string{"TTL": "60", "Content-Encoding": "aes128gcm"})), 201)
	if p := h.srv.Store().Push.List(nil, 1)[0]; p.Title != "" || p.PayloadBytes != 3 || p.VAPID {
		t.Errorf("capture = %+v", p)
	}
}

func TestPushErrors(t *testing.T) {
	h := newHarness(t, nil)
	ok := map[string]string{"TTL": "60"}
	cases := []struct {
		name    string
		sub     string
		body    []byte
		headers map[string]string
		status  int
	}{
		{"gone 410", "gone-abc", nil, ok, 410},
		{"gone 404", "gone-404-abc", nil, ok, 404},
		{"gone wins over bad headers", "gone-x", nil, nil, 410},
		{"missing TTL", "s", nil, nil, 400},
		{"bad TTL", "s", nil, map[string]string{"TTL": "-1"}, 400},
		{"bad urgency", "s", nil, map[string]string{"TTL": "1", "Urgency": "urgent"}, 400},
		{"bad topic", "s", nil, map[string]string{"TTL": "1", "Topic": "has space"}, 400},
		{"long topic", "s", nil, map[string]string{"TTL": "1", "Topic": strings.Repeat("a", 33)}, 400},
		{"body without encoding", "s", []byte("x"), ok, 400},
		{"too large", "s", bytes.Repeat([]byte("x"), maxPushPayload+1), map[string]string{"TTL": "1", "Content-Encoding": "aes128gcm"}, 413},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantStatus(t, h.do(pushReq(c.sub, c.body, c.headers)), c.status)
		})
	}
	// Every attempt except the oversize one is recorded with its status.
	gone := h.srv.Store().Push.List(func(p capture.Push) bool { return p.Subscription == "gone-abc" }, 0)
	if len(gone) != 1 || gone[0].Status != 410 {
		t.Errorf("gone capture = %+v", gone)
	}
	// Exactly at the limit is fine.
	wantStatus(t, h.do(pushReq("s", bytes.Repeat([]byte("x"), maxPushPayload), map[string]string{"TTL": "1", "Content-Encoding": "aes128gcm"})), 201)
}

func TestPushSubscriptionWithSlashes(t *testing.T) {
	h := newHarness(t, nil)
	wantStatus(t, h.do(pushReq("fcm/send/abc:def", nil, map[string]string{"TTL": "5"})), 201)
	items := decodeItems(t, h.get("/_mock/push?subscription=fcm/send/abc:def"))
	if len(items) != 1 {
		t.Fatalf("filter by subscription: %v", items)
	}
	wantKeys(t, items[0], "id", "at", "subscription", "status", "ttl", "payload_bytes", "vapid")
}

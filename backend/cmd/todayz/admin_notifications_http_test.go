package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
)

// W3-05 A-16 (read-only, D-224): the catalogue per kind and language, each
// channel's switch, keys set or missing and last error, and a test send that
// lands only in the caller's own in-app inbox.
func TestAdminNotificationsHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "admin")
	tok := desk.token()
	a := o.a
	get := func(path string) resp { t.Helper(); return a.call(tok, http.MethodGet, path, nil, nil) }
	send := func(body any) resp {
		t.Helper()
		return a.call(tok, http.MethodPost, "/v1/admin/notification-templates/test-send", body, nil)
	}

	// Every kind, every language; a missing language shows English, marked.
	r := get("/v1/admin/notification-templates")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	items := r.body["items"].([]any)
	require.NotEmpty(t, items)
	byKind := map[string]map[string]any{}
	for _, it := range items {
		m := it.(map[string]any)
		byKind[m["kind"].(string)] = m
	}
	unseen := byKind["order_unseen"]
	require.Equal(t, "business", unseen["app"])
	require.Equal(t, false, unseen["testable"])
	require.Equal(t, []any{"sms", "whatsapp"}, unseen["channels"])
	for _, p := range unseen["providerTemplates"].([]any) {
		require.Equal(t, false, p.(map[string]any)["smsTemplateIdSet"])
	}
	upd := byKind["order_update"]
	require.Equal(t, true, upd["testable"])
	var kn, hiReady map[string]any
	for _, x := range upd["texts"].([]any) {
		m := x.(map[string]any)
		if m["lang"] == "kn" && m["variant"] == nil {
			kn = m
		}
		if m["lang"] == "hi" && m["variant"] == "ready" {
			hiReady = m
		}
	}
	require.NotNil(t, kn)
	require.NotNil(t, hiReady)
	require.Equal(t, true, hiReady["fallback"])
	require.Contains(t, hiReady["body"], "{store}")

	r = get("/v1/admin/notification-templates?app=customer")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	for _, it := range r.body["items"].([]any) {
		require.Equal(t, "customer", it.(map[string]any)["app"])
	}
	require.Less(t, len(r.body["items"].([]any)), len(items))

	// Integrations: names and set/missing only, the switch, the last failure.
	at := a.clk.Now()
	nid := uuidOf(t, ids.New(at).String())
	// The admin's own customer sign-in (newAdminDesk): one SMS fewer than a
	// new sign-in, which the shared per-IP limit has no room for (D-264).
	cust := desk.cust
	me := a.call(cust, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(t, http.StatusOK, me.code, me.raw)
	a.exec(`INSERT INTO notifications (id, app, user_id, kind, created_at) VALUES ($1, 'customer', $2, 'order_update', $3)`, nid, uuidOf(t, me.body["id"].(string)), at)
	a.exec(`INSERT INTO notification_deliveries (id, notification_id, channel, provider, status, error, created_at, updated_at) VALUES ($1, $2, 'sms', 'aws_sms', 'failed', 'throttled', $3, $3)`, uuidOf(t, ids.New(at).String()), nid, at)
	r = get("/v1/admin/integrations")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	chans := map[string]map[string]any{}
	for _, it := range r.body["items"].([]any) {
		m := it.(map[string]any)
		chans[m["channel"].(string)] = m
	}
	require.Len(t, chans, 3)
	require.Equal(t, true, chans["sms"]["senderBuilt"])
	require.Equal(t, false, chans["push"]["senderBuilt"])
	require.Equal(t, false, chans["whatsapp"]["enabled"])
	require.Equal(t, "throttled", chans["sms"]["lastError"].(map[string]any)["error"])
	require.Nil(t, chans["push"]["lastError"])
	for _, c := range chans {
		for _, v := range c["vars"].([]any) {
			m := v.(map[string]any)
			require.Len(t, m, 2, "a var carries its name and set, never a value")
			require.IsType(t, true, m["set"])
		}
	}

	// Test send: a customer kind lands in my own inbox, in-app only.
	before := a.count(`SELECT count(*) FROM notification_deliveries`)
	r = send(map[string]any{"kind": "order_update", "variant": "ready"})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, "ready", r.body["variant"])
	sent := r.body["notificationId"].(string)
	require.Equal(t, before, a.count(`SELECT count(*) FROM notification_deliveries`))
	inbox := a.call(cust, http.MethodGet, "/v1/me/notifications", nil, nil)
	require.Equal(t, http.StatusOK, inbox.code, inbox.raw)
	var got map[string]any
	for _, it := range inbox.body["items"].([]any) {
		if m := it.(map[string]any); m["id"] == sent {
			got = m
		}
	}
	require.NotNil(t, got)
	require.Equal(t, "order_update", got["kind"])
	require.True(t, strings.Contains(got["body"].(string), "{store}"), "the preview keeps the placeholders")
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'notification.test_send' AND subject_type = 'notification_template' AND subject_id = 'order_update' AND after->>'notificationId' = $1`, sent))

	// A business kind has no personal inbox; unknown kinds and variants name the field.
	r = send(map[string]any{"kind": "order_unseen"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = send(map[string]any{"kind": "nope"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = send(map[string]any{"kind": "order_update", "variant": "nope"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'notification.test_send'`))

	// Support reads neither the templates nor the keys.
	a.exec(`UPDATE admin_users SET roles = ARRAY['support']`)
	tok = desk.token()
	r = get("/v1/admin/notification-templates")
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = get("/v1/admin/integrations")
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = send(map[string]any{"kind": "order_update"})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
}

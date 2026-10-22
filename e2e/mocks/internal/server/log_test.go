package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// D-15: request logs carry method, route template, status and duration only.
// This drives every sensitive path and then searches the raw log output for
// the phone, the OTP, tokens, keys and bodies.
func TestLogsNeverHoldSensitiveValues(t *testing.T) {
	const (
		phone     = "9000000017"
		otp       = "482913"
		authkey   = "authkey-SENTINEL-5d1c"
		apikey    = "apikey-SENTINEL-77aa" // gitleaks:allow (fake sentinel)
		token     = "turnstile-token-SENTINEL-9f2e"
		secret    = "turnstile-secret-SENTINEL-c0de" // gitleaks:allow (fake sentinel)
		pushTitle = "push-title-SENTINEL"
		subID     = "sub-SENTINEL-3b7d"
		body      = "flow-var-SENTINEL"
		llmText   = "llm-prompt-SENTINEL"
	)
	h := newHarness(t, nil)

	// The OTP send, through query and body.
	wantStatus(t, h.postJSON("/msg91/api/v5/otp?template_id=tz_otp_login&mobile=91"+phone+"&otp="+otp, `{"name":"Asha"}`, map[string]string{"authkey": authkey}), 200)
	wantStatus(t, h.postJSON("/msg91/api/v5/otp", map[string]string{"mobile": "91" + phone, "otp": otp}, map[string]string{"authkey": authkey}), 200)
	wantStatus(t, h.postJSON("/msg91/api/v5/otp?mobile=91"+phone+"&otp="+otp+"&authkey="+authkey, "", nil), 200)
	// Errors must not echo either.
	wantStatus(t, h.postJSON("/msg91/api/v5/otp?mobile=91"+phone+"&otp=12ab", "", map[string]string{"authkey": authkey}), 400)
	// Reads of the OTP.
	wantStatus(t, h.get("/_mock/sms/latest-otp?to="+phone), 200)
	wantStatus(t, h.get("/msg91/api/v5/otp/verify?mobile=91"+phone+"&otp="+otp+"&authkey="+authkey), 200)
	// Other providers.
	wantStatus(t, h.postJSON("/msg91/api/v5/flow", map[string]any{"template_id": "tz_order_update", "recipients": []map[string]any{{"mobiles": "91" + phone, "order": body}}}, map[string]string{"authkey": authkey}), 200)
	wantStatus(t, h.postForm("/whatsapp/wa/api/v1/msg", url.Values{"source": {"919000000050"}, "destination": {"91" + phone}, "message": {body}}, map[string]string{"apikey": apikey}), 202)
	wantStatus(t, h.postForm(siteverify, url.Values{"secret": {secret}, "response": {token}}, nil), 200)
	wantStatus(t, h.do(pushReq(subID, nil, map[string]string{"TTL": "1", PlaintextHeader: `{"title":"` + pushTitle + `"}`})), 201)
	wantStatus(t, h.postJSON(msgPath, voiceRequest(llmText), anthropicHeaders), 200)
	wantStatus(t, h.get("/_mock/sms?to="+phone), 200)
	// Unmatched paths log "unmatched", not the path.
	wantStatus(t, h.get("/"+phone+"/"+otp), 404)

	logs := h.logs.String()
	for _, s := range []string{phone, otp, authkey, apikey, token, secret, pushTitle, subID, body, llmText, "Asha", "test-placeholder"} {
		if strings.Contains(logs, s) {
			t.Errorf("log output contains %q", s)
		}
	}

	allowed := map[string]bool{"time": true, "level": true, "msg": true, "method": true, "route": true, "status": true, "duration_ms": true}
	sc := bufio.NewScanner(strings.NewReader(logs))
	lines := 0
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("log line is not JSON: %v", err)
		}
		var extra []string
		for k := range m {
			if !allowed[k] {
				extra = append(extra, k)
			}
		}
		sort.Strings(extra)
		if len(extra) > 0 {
			t.Errorf("unexpected log fields %v", extra)
		}
		if r, _ := m["route"].(string); strings.ContainsAny(r, "?=") {
			t.Errorf("route holds a query: %q", r)
		}
		lines++
	}
	if lines < 13 {
		t.Errorf("expected a log line per request, got %d", lines)
	}
	if !strings.Contains(logs, `"route":"/push/{subscriptionId...}"`) || !strings.Contains(logs, `"route":"unmatched"`) {
		t.Errorf("route templates missing:\n%s", logs)
	}
}

func TestPanicIsRecoveredWithoutDetails(t *testing.T) {
	h := newHarness(t, nil)
	h.srv.mux.HandleFunc("GET /boom", func(_ http.ResponseWriter, _ *http.Request) { panic("secret-SENTINEL") })
	wantStatus(t, h.get("/boom"), 500)
	if strings.Contains(h.logs.String(), "secret-SENTINEL") {
		t.Error("panic value logged")
	}
}

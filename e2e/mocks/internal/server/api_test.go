package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAPIListAndFilters(t *testing.T) {
	h := newHarness(t, nil)
	for _, m := range []string{"919000000021", "919000000022", "919000000021"} {
		wantStatus(t, h.postJSON("/msg91/api/v5/otp?mobile="+m, "", msg91Auth_), 200)
	}
	all := decodeItems(t, h.get("/_mock/sms"))
	if len(all) != 3 || all[0]["id"].(float64) < all[2]["id"].(float64) {
		t.Fatalf("want 3 newest first, got %v", all)
	}
	wantKeys(t, all[0], "id", "at", "api", "to", "template_id", "variables", "body", "otp", "request_id")

	// "+" decodes to a space in a query string; the filter still matches.
	for _, q := range []string{"to=9000000021", "to=%2B919000000021", "to=+919000000021", "to=919000000021"} {
		if got := decodeItems(t, h.get("/_mock/sms?"+q)); len(got) != 2 {
			t.Errorf("%s: %d items", q, len(got))
		}
	}
	if got := decodeItems(t, h.get("/_mock/sms?limit=1")); len(got) != 1 {
		t.Errorf("limit: %d", len(got))
	}
	// Every kind lists, empty as [] not null.
	for _, k := range []string{"whatsapp", "push", "turnstile", "stt", "llm"} {
		rr := h.get("/_mock/" + k)
		wantStatus(t, rr, 200)
		if strings.TrimSpace(rr.Body.String()) != `{"items":[]}` {
			t.Errorf("%s: %s", k, rr.Body.String())
		}
	}
	wantStatus(t, h.get("/_mock/fax"), 404)
}

func TestAPILatestOTP(t *testing.T) {
	h := newHarness(t, nil)
	wantStatus(t, h.get("/_mock/sms/latest-otp"), 400)
	wantStatus(t, h.get("/_mock/sms/latest-otp?to=9000000031"), 404)
	if m := decode(t, h.get("/_mock/sms/latest-otp?to=9000000031")); m["error"] == nil {
		t.Errorf("404 body = %v", m)
	}

	h.postJSON("/msg91/api/v5/otp?mobile=919000000031&otp=111222", "", msg91Auth_)
	h.postJSON("/msg91/api/v5/otp?mobile=919000000031&otp=333444", "", msg91Auth_)
	h.postJSON("/msg91/api/v5/otp?mobile=919000000032&otp=555666", "", msg91Auth_)
	rr := h.get("/_mock/sms/latest-otp?to=%2B919000000031")
	wantStatus(t, rr, 200)
	m := decode(t, rr)
	wantKeys(t, m, "otp", "at")
	if m["otp"] != "333444" {
		t.Errorf("latest = %v", m)
	}
	if _, err := time.Parse(time.RFC3339Nano, m["at"].(string)); err != nil {
		t.Errorf("at = %v", m["at"])
	}

	// since excludes older codes.
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	wantStatus(t, h.get("/_mock/sms/latest-otp?to=9000000031&since="+future), 404)
	wantStatus(t, h.get("/_mock/sms/latest-otp?to=9000000031&since=yesterday"), 400)
	wantStatus(t, h.get("/_mock/sms/latest-otp?to=9000000031&wait=x"), 400)
}

func TestAPILatestOTPWait(t *testing.T) {
	h := newHarness(t, nil)
	since := time.Now().UTC().Format(time.RFC3339Nano)
	go func() {
		time.Sleep(150 * time.Millisecond)
		h.postJSON("/msg91/api/v5/otp?mobile=919000000033&otp=777888", "", msg91Auth_)
	}()
	start := time.Now()
	rr := h.get("/_mock/sms/latest-otp?to=9000000033&wait=3&since=" + since)
	wantStatus(t, rr, 200)
	if m := decode(t, rr); m["otp"] != "777888" {
		t.Errorf("waited = %v", m)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("wait returned late")
	}
	start = time.Now()
	wantStatus(t, h.get("/_mock/sms/latest-otp?to=9000000034&wait=0.3"), 404)
	if d := time.Since(start); d < 250*time.Millisecond {
		t.Errorf("wait ended early: %v", d)
	}
}

func TestAPIResetClearSummary(t *testing.T) {
	h := newHarness(t, nil)
	h.postJSON("/msg91/api/v5/otp?mobile=919000000041", "", msg91Auth_)
	h.do(pushReq("s", nil, map[string]string{"TTL": "1"}))
	sum := decode(t, h.get("/_mock/summary"))
	wantKeys(t, sum, "counts", "kinds", "llm_mode")
	counts := obj(t, sum["counts"])
	if counts["sms"] != float64(1) || counts["push"] != float64(1) || sum["llm_mode"] != "fixtures" {
		t.Errorf("summary = %v", sum)
	}

	del := func(p string) *httptest.ResponseRecorder {
		return h.do(httptest.NewRequest(http.MethodDelete, p, nil))
	}
	wantStatus(t, del("/_mock/sms"), 204)
	if h.srv.Store().SMS.Len() != 0 || h.srv.Store().Push.Len() != 1 {
		t.Error("clear one kind")
	}
	wantStatus(t, del("/_mock/fax"), 404)
	wantStatus(t, del("/_mock/reset"), 204)
	if h.srv.Store().Push.Len() != 0 {
		t.Error("reset")
	}
}

func TestHealthzAndUI(t *testing.T) {
	h := newHarness(t, nil)
	rr := h.get("/healthz")
	wantStatus(t, rr, 200)
	m := decode(t, rr)
	wantKeys(t, m, "status", "llm_mode", "fixtures", "replay_recorded", "fixture_errors")
	if m["status"] != "ok" || m["llm_mode"] != "fixtures" {
		t.Errorf("healthz = %v", m)
	}

	rr = h.get("/")
	wantStatus(t, rr, 200)
	if !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(rr.Header().Get("Content-Security-Policy"), "default-src 'self'") ||
		rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("index headers = %v", rr.Header())
	}
	if !strings.Contains(rr.Body.String(), "TodayZ mocks") {
		t.Error("index body")
	}
	for path, ct := range map[string]string{"/assets/tz.css": "text/css", "/assets/mocks.css": "text/css", "/assets/app.js": "javascript"} {
		rr := h.get(path)
		wantStatus(t, rr, 200)
		if !strings.Contains(rr.Header().Get("Content-Type"), ct) {
			t.Errorf("%s content type = %q", path, rr.Header().Get("Content-Type"))
		}
	}
	wantStatus(t, h.get("/nope"), 404)
	wantStatus(t, h.get("/assets/nope.js"), 404)
}

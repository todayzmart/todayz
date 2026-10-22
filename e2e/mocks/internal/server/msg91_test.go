package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Synthetic values only: reserved test numbers +91 90000 000xx and made-up codes.
const auth = "test-placeholder-authkey"

var msg91Auth_ = map[string]string{"authkey": auth}

func TestMSG91SendOTP(t *testing.T) {
	h := newHarness(t, nil)
	rr := h.postJSON("/msg91/api/v5/otp?template_id=tz_otp_login&mobile=919000000001&otp=482913&otp_expiry=5", `{"Param1":"x"}`, msg91Auth_)
	wantStatus(t, rr, http.StatusOK)
	m := decode(t, rr)
	wantKeys(t, m, "request_id", "type")
	if m["type"] != "success" || m["request_id"] == "" {
		t.Errorf("response = %v", m)
	}

	got := h.srv.Store().SMS.List(nil, 0)
	if len(got) != 1 {
		t.Fatalf("captured %d", len(got))
	}
	s := got[0]
	if s.To != "+919000000001" || s.OTP != "482913" || s.TemplateID != "tz_otp_login" || s.API != "otp" {
		t.Errorf("capture = %+v", s)
	}
	if !strings.HasPrefix(s.Body, "482913 is your TodayZ login code. It expires in 5 minutes.") {
		t.Errorf("body = %q", s.Body)
	}
	if s.Variables["Param1"] != "x" {
		t.Errorf("variables = %v", s.Variables)
	}
	if _, ok := s.Variables["mobile"]; ok {
		t.Error("parameters must not be kept as template variables")
	}
}

func TestMSG91SendOTPBodyParamsAndGenerated(t *testing.T) {
	h := newHarness(t, nil)
	// Parameters in the JSON body, no otp: the mock generates otp_length digits.
	rr := h.postJSON("/msg91/api/v5/otp", map[string]any{"mobile": "919000000002", "otp_length": 6}, msg91Auth_)
	wantStatus(t, rr, http.StatusOK)
	s := h.srv.Store().SMS.List(nil, 0)[0]
	if s.To != "+919000000002" || len(s.OTP) != 6 || !digitsRe.MatchString(s.OTP) {
		t.Errorf("capture = %+v", s)
	}
	if !strings.Contains(s.Body, s.OTP+" is your TodayZ verification code") {
		t.Errorf("default template not used: %q", s.Body)
	}

	// Default length is 4.
	h.postJSON("/msg91/api/v5/otp?mobile=919000000003", "", msg91Auth_)
	if s := h.srv.Store().SMS.List(nil, 1)[0]; len(s.OTP) != 4 {
		t.Errorf("default OTP length = %d", len(s.OTP))
	}
}

func TestMSG91SendOTPErrors(t *testing.T) {
	h := newHarness(t, nil)
	cases := []struct {
		name    string
		path    string
		body    string
		headers map[string]string
		status  int
	}{
		{"no authkey", "/msg91/api/v5/otp?mobile=919000000001", "", nil, 401},
		{"blank authkey", "/msg91/api/v5/otp?mobile=919000000001", "", map[string]string{"authkey": " "}, 401},
		{"no mobile", "/msg91/api/v5/otp", "", msg91Auth_, 400},
		{"short mobile", "/msg91/api/v5/otp?mobile=12345", "", msg91Auth_, 400},
		{"no country code", "/msg91/api/v5/otp?mobile=9000000001", "", msg91Auth_, 400},
		{"leading zero", "/msg91/api/v5/otp?mobile=09000000001", "", msg91Auth_, 400},
		{"91 too long", "/msg91/api/v5/otp?mobile=9190000000011", "", msg91Auth_, 400},
		{"bad otp", "/msg91/api/v5/otp?mobile=919000000001&otp=12ab", "", msg91Auth_, 400},
		{"bad json", "/msg91/api/v5/otp?mobile=919000000001", "{", msg91Auth_, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := h.postJSON(c.path, c.body, c.headers)
			wantStatus(t, rr, c.status)
			m := decode(t, rr)
			wantKeys(t, m, "type", "message")
			if m["type"] != "error" {
				t.Errorf("type = %v", m["type"])
			}
		})
	}
	if n := h.srv.Store().SMS.Len(); n != 0 {
		t.Errorf("errors must not be captured as sent SMS, got %d", n)
	}
	// authkey as a query parameter is accepted too (MSG91 allows it).
	wantStatus(t, h.postJSON("/msg91/api/v5/otp?mobile=919000000001&authkey=x", "", nil), 200)
}

func TestMSG91RetryAndVerify(t *testing.T) {
	h := newHarness(t, nil)
	h.postJSON("/msg91/api/v5/otp?mobile=919000000004&otp=135790", "", msg91Auth_)

	rr := h.postJSON("/msg91/api/v5/otp/retry?mobile=919000000004&retrytype=text", "", msg91Auth_)
	wantStatus(t, rr, 200)
	wantKeys(t, decode(t, rr), "type", "message")
	if s := h.srv.Store().SMS.List(nil, 1)[0]; s.API != "otp_retry" || s.OTP != "135790" {
		t.Errorf("retry capture = %+v", s)
	}
	wantStatus(t, h.postJSON("/msg91/api/v5/otp/retry?mobile=919000000099", "", msg91Auth_), 400)

	verify := func(otp string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/msg91/api/v5/otp/verify?mobile=919000000004&otp="+otp, nil)
		req.Header.Set("authkey", auth)
		return h.do(req)
	}
	rr = verify("135790")
	wantStatus(t, rr, 200)
	if m := decode(t, rr); m["type"] != "success" {
		t.Errorf("verify = %v", m)
	}
	rr = verify("000000")
	wantStatus(t, rr, 400)
	if m := decode(t, rr); m["type"] != "error" {
		t.Errorf("verify wrong = %v", m)
	}
}

// A code works once, and only until otp_expiry (minutes; default one day).
func TestMSG91VerifySingleUseAndExpiry(t *testing.T) {
	now := fixedNow
	h := newHarness(t, func(c *Config) { c.Now = func() time.Time { return now } })
	verify := func(mobile, otp string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/msg91/api/v5/otp/verify?mobile="+mobile+"&otp="+otp, nil)
		req.Header.Set("authkey", auth)
		rr := h.do(req)
		m := decode(t, rr)
		wantKeys(t, m, "type", "message")
		if (rr.Code == 200) != (m["type"] == "success") {
			t.Errorf("status %d with %v", rr.Code, m)
		}
		return m
	}

	wantStatus(t, h.postJSON("/msg91/api/v5/otp?mobile=919000000011&otp=482913&otp_expiry=5", "", msg91Auth_), 200)
	if m := verify("919000000011", "111111"); m["message"] != "OTP not match" {
		t.Errorf("wrong code = %v", m)
	}
	if m := verify("919000000011", "482913"); m["type"] != "success" || m["message"] != "OTP verified success" {
		t.Errorf("first verify = %v", m)
	}
	if m := verify("919000000011", "482913"); m["type"] != "error" || m["message"] != "Mobile no. already verified" {
		t.Errorf("second verify = %v", m)
	}
	// Retry after a successful verify is refused.
	wantStatus(t, h.postJSON("/msg91/api/v5/otp/retry?mobile=919000000011", "", msg91Auth_), 400)

	// Expiry: 5 minutes.
	wantStatus(t, h.postJSON("/msg91/api/v5/otp?mobile=919000000012&otp=246802&otp_expiry=5", "", msg91Auth_), 200)
	now = now.Add(5*time.Minute - time.Second)
	wantStatus(t, h.postJSON("/msg91/api/v5/otp?mobile=919000000013&otp=135791&otp_expiry=5", "", msg91Auth_), 200)
	now = now.Add(time.Second)
	if m := verify("919000000012", "246802"); m["message"] != "OTP expired" {
		t.Errorf("expired verify = %v", m)
	}
	if m := verify("919000000013", "135791"); m["type"] != "success" {
		t.Errorf("unexpired verify = %v", m)
	}
	// Default expiry is a day; a new send replaces the old code.
	wantStatus(t, h.postJSON("/msg91/api/v5/otp?mobile=919000000014&otp=111222", "", msg91Auth_), 200)
	wantStatus(t, h.postJSON("/msg91/api/v5/otp?mobile=919000000014&otp=333444", "", msg91Auth_), 200)
	now = now.Add(23 * time.Hour)
	if m := verify("919000000014", "111222"); m["message"] != "OTP not match" {
		t.Errorf("replaced code = %v", m)
	}
	if m := verify("919000000014", "333444"); m["type"] != "success" {
		t.Errorf("day-old code = %v", m)
	}
	// Verify needs the country code too; a reset forgets every code.
	if m := verify("9000000013", "135791"); m["type"] != "error" {
		t.Errorf("verify without country code = %v", m)
	}
	wantStatus(t, h.postJSON("/msg91/api/v5/otp?mobile=919000000015&otp=555666", "", msg91Auth_), 200)
	req := httptest.NewRequest(http.MethodDelete, "/_mock/reset", nil)
	wantStatus(t, h.do(req), http.StatusNoContent)
	if m := verify("919000000015", "555666"); m["type"] != "error" {
		t.Errorf("verify after reset = %v", m)
	}
}

func TestMSG91Flow(t *testing.T) {
	h := newHarness(t, nil)
	body := map[string]any{
		"template_id": "tz_order_ready",
		"short_url":   "0",
		"recipients": []map[string]any{
			{"mobiles": "919000000005,919000000006", "order": "TZ-1042", "store": "Ravi Stores", "code": "4821"},
		},
	}
	rr := h.postJSON("/msg91/api/v5/flow", body, msg91Auth_)
	wantStatus(t, rr, 200)
	m := decode(t, rr)
	wantKeys(t, m, "type", "message")
	if m["type"] != "success" {
		t.Errorf("flow = %v", m)
	}
	got := h.srv.Store().SMS.List(nil, 0)
	if len(got) != 2 {
		t.Fatalf("captured %d", len(got))
	}
	if got[0].To != "+919000000006" || got[1].To != "+919000000005" {
		t.Errorf("newest first: %s, %s", got[0].To, got[1].To)
	}
	want := "Your TodayZ order TZ-1042 is ready for pickup at Ravi Stores. Show code 4821 at the counter."
	if got[0].Body != want || got[0].API != "flow" || got[0].OTP != "" {
		t.Errorf("flow capture = %+v", got[0])
	}

	// An OTP sent through a flow template is still extracted.
	h.postJSON("/msg91/api/v5/flow/", map[string]any{"flow_id": "tz_otp_login", "recipients": []map[string]any{{"mobiles": "919000000007", "OTP": "246810"}}}, msg91Auth_)
	if s := h.srv.Store().SMS.List(nil, 1)[0]; s.OTP != "246810" {
		t.Errorf("flow OTP = %q", s.OTP)
	}
}

func TestMSG91FlowErrors(t *testing.T) {
	h := newHarness(t, nil)
	for name, body := range map[string]string{
		"no template":   `{"recipients":[{"mobiles":"919000000001"}]}`,
		"no recipients": `{"template_id":"x"}`,
		"no mobiles":    `{"template_id":"x","recipients":[{"VAR1":"a"}]}`,
		"bad mobile":    `{"template_id":"x","recipients":[{"mobiles":"123"}]}`,
		"bad json":      `[`,
	} {
		t.Run(name, func(t *testing.T) {
			rr := h.postJSON("/msg91/api/v5/flow", body, msg91Auth_)
			wantStatus(t, rr, 400)
			wantKeys(t, decode(t, rr), "type", "message")
		})
	}
	wantStatus(t, h.postJSON("/msg91/api/v5/flow", `{}`, nil), 401)
}

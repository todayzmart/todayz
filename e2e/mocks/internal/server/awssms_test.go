package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Synthetic values only: reserved test numbers and the local DLT ids from
// infra/local/local.env.
const (
	awsEntity  = "1000000000000000001"
	awsOTPTpl  = "1000000000000000101"
	awsUnseen  = "1000000000000000201"
	awsSigAuth = "AWS4-HMAC-SHA256 Credential=local-mock/20261024/ap-south-1/sms-voice/aws4_request, SignedHeaders=host;x-amz-date;x-amz-target, Signature=00"
)

func awsHeaders() map[string]string {
	return map[string]string{
		"Authorization": awsSigAuth,
		"X-Amz-Target":  "PinpointSMSVoiceV2.SendTextMessage",
		"Content-Type":  "application/x-amz-json-1.0",
	}
}

func awsBody(to, body, tpl string) map[string]any {
	return map[string]any{
		"DestinationPhoneNumber": to,
		"OriginationIdentity":    "TODAYZ",
		"MessageBody":            body,
		"MessageType":            "TRANSACTIONAL",
		"TimeToLive":             300,
		"DestinationCountryParameters": map[string]string{
			"IN_ENTITY_ID": awsEntity, "IN_TEMPLATE_ID": tpl,
		},
	}
}

// postAWS posts like the SDK: postJSON would overwrite Content-Type.
func (h *harness) postAWS(path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		h.t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return h.do(req)
}

func TestAWSSendTextMessageOTP(t *testing.T) {
	h := newHarness(t, nil)
	for _, path := range []string{"/sms-voice", "/sms-voice/"} {
		rr := h.postAWS(path, awsBody("+919000000001", "482913 is your TodayZ login code. Do not share it with anyone.", awsOTPTpl), awsHeaders())
		wantStatus(t, rr, http.StatusOK)
		m := decode(t, rr)
		wantKeys(t, m, "MessageId")
		if rr.Header().Get("Content-Type") != "application/x-amz-json-1.0" {
			t.Errorf("content type = %q", rr.Header().Get("Content-Type"))
		}
	}
	got := h.srv.Store().SMS.List(nil, 0)
	if len(got) != 2 {
		t.Fatalf("captured %d", len(got))
	}
	s := got[0]
	if s.API != "send_text_message" || s.To != "+919000000001" || s.Sender != "TODAYZ" ||
		s.EntityID != awsEntity || s.TemplateID != awsOTPTpl || s.OTP != "482913" || s.RequestID == "" {
		t.Errorf("capture = %+v", s)
	}

	// The OTP read the journeys use keeps working.
	rr := h.get("/_mock/sms/latest-otp?to=9000000001")
	wantStatus(t, rr, http.StatusOK)
	if decode(t, rr)["otp"] != "482913" {
		t.Errorf("latest otp = %s", rr.Body.String())
	}
}

func TestAWSSendTextMessageNotice(t *testing.T) {
	h := newHarness(t, nil)
	ok := "A new order at Sri Lakshmi Venkateswara Gener has not been opened for 3 minutes. Open the TodayZ business app to accept it."
	wantStatus(t, h.postAWS("/sms-voice", awsBody("+919000000002", ok, awsUnseen), awsHeaders()), http.StatusOK)
	hi := "Lakshmi Traders पर एक नया ऑर्डर 3 मिनट से खोला नहीं गया है। स्वीकार करने के लिए TodayZ बिज़नेस ऐप खोलें।"
	wantStatus(t, h.postAWS("/sms-voice", awsBody("+919000000002", hi, "1000000000000000202"), awsHeaders()), http.StatusOK)
	if s := h.srv.Store().SMS.List(nil, 0)[0]; s.OTP != "" || s.Body != hi {
		t.Errorf("capture = %+v", s)
	}
	// An id the fixtures do not list is accepted as is.
	wantStatus(t, h.postAWS("/sms-voice", awsBody("+919000000002", "anything", "1000000000000000999"), awsHeaders()), http.StatusOK)
	// DryRun answers but records nothing.
	b := awsBody("+919000000002", ok, awsUnseen)
	b["DryRun"] = true
	wantStatus(t, h.postAWS("/sms-voice", b, awsHeaders()), http.StatusOK)
	if n := len(h.srv.Store().SMS.List(nil, 0)); n != 3 {
		t.Errorf("captured %d, want 3", n)
	}
}

func TestAWSSendTextMessageErrors(t *testing.T) {
	const phone = "+919000000017"
	long := "A new order at Sri Lakshmi Venkateswara General Stores has not been opened for 3 minutes. Open the TodayZ business app to accept it."
	drop := func(k string) map[string]string {
		h := awsHeaders()
		delete(h, k)
		return h
	}
	with := func(k string, v any) map[string]any {
		b := awsBody(phone, "123456 is your TodayZ login code. Do not share it with anyone.", awsOTPTpl)
		if v == nil {
			delete(b, k)
		} else {
			b[k] = v
		}
		return b
	}
	cases := []struct {
		name    string
		body    any
		headers map[string]string
		status  int
		typ     string
	}{
		{"no signature", with("x", nil), drop("Authorization"), 403, "MissingAuthenticationTokenException"},
		{"other operation", with("x", nil), map[string]string{"Authorization": awsSigAuth, "X-Amz-Target": "PinpointSMSVoiceV2.SendVoiceMessage", "Content-Type": awsJSONType}, 400, "UnknownOperationException"},
		{"plain json", with("x", nil), map[string]string{"Authorization": awsSigAuth, "X-Amz-Target": awsSMSTarget, "Content-Type": "application/json"}, 400, "SerializationException"},
		{"not json", "x", awsHeaders(), 400, "SerializationException"},
		{"no plus", with("DestinationPhoneNumber", "919000000017"), awsHeaders(), 400, "ValidationException"},
		{"no country code", with("DestinationPhoneNumber", "+9000000017"), awsHeaders(), 400, "ValidationException"},
		{"empty body", with("MessageBody", ""), awsHeaders(), 400, "ValidationException"},
		{"too long", with("MessageBody", strings.Repeat("a", 1601)), awsHeaders(), 400, "ValidationException"},
		{"bad type", with("MessageType", "OTP"), awsHeaders(), 400, "ValidationException"},
		{"ttl low", with("TimeToLive", 4), awsHeaders(), 400, "ValidationException"},
		{"ttl high", with("TimeToLive", 259201), awsHeaders(), 400, "ValidationException"},
		{"no sender", with("OriginationIdentity", nil), awsHeaders(), 400, "ValidationException"},
		{"no DLT ids", with("DestinationCountryParameters", nil), awsHeaders(), 400, "ValidationException"},
		{"no template id", with("DestinationCountryParameters", map[string]string{"IN_ENTITY_ID": awsEntity}), awsHeaders(), 400, "ValidationException"},
		{"text not the template", with("MessageBody", "123456 is your TodayZ code."), awsHeaders(), 400, "ValidationException"},
		{"var over 30", awsBody(phone, long, awsUnseen), awsHeaders(), 400, "ValidationException"},
	}
	h := newHarness(t, nil)
	for _, c := range cases {
		var rr *httptest.ResponseRecorder
		if s, ok := c.body.(string); ok {
			req := httptest.NewRequest(http.MethodPost, "/sms-voice", strings.NewReader(s))
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			rr = h.do(req)
		} else {
			rr = h.postAWS("/sms-voice", c.body, c.headers)
		}
		if rr.Code != c.status {
			t.Errorf("%s: status = %d, want %d: %s", c.name, rr.Code, c.status, rr.Body.String())
			continue
		}
		if got := rr.Header().Get("X-Amzn-ErrorType"); got != c.typ {
			t.Errorf("%s: X-Amzn-ErrorType = %q, want %q", c.name, got, c.typ)
		}
		m := decode(t, rr)
		wantKeys(t, m, "__type", "Message")
		if strings.Contains(rr.Body.String(), "9000000017") || strings.Contains(rr.Body.String(), "123456") {
			t.Errorf("%s: error echoes the request: %s", c.name, rr.Body.String())
		}
	}
	if n := len(h.srv.Store().SMS.List(nil, 0)); n != 0 {
		t.Errorf("refused sends were captured: %d", n)
	}
	if strings.Contains(h.logs.String(), "9000000017") || strings.Contains(h.logs.String(), "123456") {
		t.Error("logs hold the phone or the code")
	}
}

func TestDLTMatch(t *testing.T) {
	tpl := "A new order at {#var#} has not been opened for {#var#} minutes."
	for body, want := range map[string]bool{
		"A new order at Ramu Kirana has not been opened for 3 minutes.":                      true,
		"A new order at " + strings.Repeat("x", 30) + " has not been opened for 3 minutes.":  true,
		"A new order at " + strings.Repeat("x", 31) + " has not been opened for 3 minutes.":  false,
		"A new order at " + strings.Repeat("दु", 15) + " has not been opened for 3 minutes.": true, // 30 runes
		"A new order at Ramu Kirana has not been opened for 3 minutes. Extra":                false,
		"A new order at Ramu (Kirana) has not been opened for 3 minutes.":                    true,
		"a new order at Ramu Kirana has not been opened for 3 minutes.":                      false,
	} {
		if got := DLTMatch(tpl, body); got != want {
			t.Errorf("DLTMatch(%q) = %v, want %v", body, got, want)
		}
	}
	if !DLTMatch("Price (incl. tax) {#var#}.", "Price (incl. tax) 10.") {
		t.Error("template punctuation must match literally")
	}
}

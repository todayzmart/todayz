package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"todayz.local/e2e/mocks/internal/capture"
	"todayz.local/e2e/mocks/internal/fixtures"
)

// defaultOTPTemplate renders an OTP SMS whose template id is not in
// fixtures/sms/templates.json (the real text lives in MSG91's DLT panel).
const defaultOTPTemplate = "##OTP## is your TodayZ verification code. Do not share it with anyone."

const smsBodyLimit = 1 << 20

// msg91Reply writes MSG91's {"type":..., "message":...} envelope.
func msg91Reply(w http.ResponseWriter, status int, typ, message string) {
	writeJSON(w, status, map[string]string{"type": typ, "message": message})
}

// msg91Auth checks that an authkey is present (header, or query as MSG91 also
// accepts). The value is never read beyond "non-empty".
func msg91Auth(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("authkey")) != "" || strings.TrimSpace(r.URL.Query().Get("authkey")) != "" {
		return true
	}
	msg91Reply(w, http.StatusUnauthorized, "error", "Authentication failure")
	return false
}

// stringMap turns a decoded JSON object into strings (numbers and bools are formatted).
func stringMap(in map[string]any) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		switch t := v.(type) {
		case string:
			out[k] = t
		case nil:
			out[k] = ""
		case float64:
			out[k] = strconv.FormatFloat(t, 'f', -1, 64)
		default:
			b, _ := json.Marshal(t)
			out[k] = string(b)
		}
	}
	return out
}

func randomDigits(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		d, _ := rand.Int(rand.Reader, big.NewInt(10))
		b.WriteByte(byte('0' + d.Int64()))
	}
	return b.String()
}

// POST /api/v5/otp?template_id=&mobile=&otp=&otp_length=&otp_expiry=
// Header authkey. Optional JSON body: template variables ({"Param1": "v"}).
// Docs: https://docs.msg91.com/otp/sendotp
func (s *Server) msg91SendOTP(w http.ResponseWriter, r *http.Request) {
	if !msg91Auth(w, r) {
		return
	}
	raw, status := readBody(w, r, smsBodyLimit)
	if status != 0 {
		msg91Reply(w, status, "error", bodyErrText(status))
		return
	}
	vars := map[string]string{}
	if len(strings.TrimSpace(string(raw))) > 0 {
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			msg91Reply(w, http.StatusBadRequest, "error", "Invalid JSON body")
			return
		}
		vars = stringMap(body)
	}
	// Parameters come from the query string, or the JSON body as a fallback;
	// they are not template variables.
	q := r.URL.Query()
	params := map[string]string{}
	for _, k := range []string{"mobile", "template_id", "otp", "otp_length", "otp_expiry", "authkey", "realTimeResponse", "unicode", "invisible", "userip"} {
		params[k] = q.Get(k)
		if params[k] == "" {
			params[k] = vars[k]
		}
		delete(vars, k)
	}
	param := func(name string) string { return params[name] }
	if strings.TrimSpace(param("mobile")) == "" {
		msg91Reply(w, http.StatusBadRequest, "error", "Mobile number missing")
		return
	}
	to, ok := ProviderPhone(param("mobile"))
	if !ok {
		msg91Reply(w, http.StatusBadRequest, "error", "Mobile number is not valid (send the country code and number, e.g. 919000000001)")
		return
	}
	otp := param("otp")
	if otp != "" && !digitsRe.MatchString(otp) {
		msg91Reply(w, http.StatusBadRequest, "error", "Invalid OTP")
		return
	}
	if otp == "" {
		n, err := strconv.Atoi(param("otp_length"))
		if err != nil || n < 4 || n > 9 {
			n = 4 // MSG91's default length
		}
		otp = randomDigits(n)
	}
	templateID := param("template_id")
	tpls, err := fixtures.LoadTemplates(s.cfg.Fixtures, "sms/templates.json")
	if err != nil {
		msg91Reply(w, http.StatusInternalServerError, "error", "tz-mock: "+err.Error())
		return
	}
	expiry := defaultOTPExpiry
	renderVars := map[string]string{"OTP": otp}
	if n, err := strconv.Atoi(param("otp_expiry")); err == nil && n >= 1 && n <= maxOTPExpiryMinutes {
		expiry = time.Duration(n) * time.Minute
		renderVars["expiry"] = strconv.Itoa(n) // minutes; the backend sends it from platform_settings
	}
	for k, v := range vars {
		renderVars[k] = v
	}
	body := RenderMSG91(tpls, templateID, renderVars, defaultOTPTemplate)
	reqID := randomHex(12)
	s.store.SMS.Add(capture.SMS{
		Meta: s.store.Next(), API: "otp", To: to, TemplateID: templateID,
		Variables: vars, Body: body, OTP: ExtractOTP(otp, vars, body), RequestID: reqID,
	})
	s.otps.set(to, otpState{code: otp, expires: s.cfg.Now().Add(expiry), templateID: templateID, vars: vars, body: body})
	writeJSON(w, http.StatusOK, map[string]string{"request_id": reqID, "type": "success"})
}

// OTP expiry: MSG91's otp_expiry is in minutes; it defaults to one day and
// allows up to seven.
const (
	defaultOTPExpiry    = 24 * time.Hour
	maxOTPExpiryMinutes = 7 * 24 * 60
)

// otpState is the OTP-widget state MSG91 keeps per mobile: the last code sent,
// when it expires, and whether it was already verified (codes are single-use).
type otpState struct {
	code       string
	expires    time.Time
	verified   bool
	templateID string
	vars       map[string]string
	body       string
}

type otpStore struct {
	mu sync.Mutex
	m  map[string]otpState
}

func (o *otpStore) set(to string, st otpState) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.m == nil {
		o.m = map[string]otpState{}
	}
	o.m[to] = st
}

func (o *otpStore) clear() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.m = nil
}

func (o *otpStore) get(to string) (otpState, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	st, ok := o.m[to]
	return st, ok
}

// verify checks code for to and marks it used on success. It returns MSG91's
// reply message and whether the code was accepted.
func (o *otpStore) verify(to, code string, now time.Time) (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	st, ok := o.m[to]
	switch {
	case !ok || code == "":
		return "OTP not match", false
	case st.verified:
		return "Mobile no. already verified", false
	case !now.Before(st.expires):
		return "OTP expired", false
	case code != st.code:
		return "OTP not match", false
	}
	st.verified = true
	o.m[to] = st
	return "OTP verified success", true
}

// GET|POST /api/v5/otp/retry?mobile=&retrytype=text — resends the last OTP.
// Docs: https://docs.msg91.com/otp/retry-otp
func (s *Server) msg91RetryOTP(w http.ResponseWriter, r *http.Request) {
	if !msg91Auth(w, r) {
		return
	}
	mobile := r.URL.Query().Get("mobile")
	if strings.TrimSpace(mobile) == "" {
		msg91Reply(w, http.StatusBadRequest, "error", "Mobile number missing")
		return
	}
	to, ok := ProviderPhone(mobile)
	if !ok {
		msg91Reply(w, http.StatusBadRequest, "error", "Mobile number is not valid (send the country code and number, e.g. 919000000001)")
		return
	}
	last, ok := s.otps.get(to)
	if !ok {
		msg91Reply(w, http.StatusBadRequest, "error", "No OTP request found for this mobile")
		return
	}
	if last.verified {
		msg91Reply(w, http.StatusBadRequest, "error", "Mobile no. already verified")
		return
	}
	s.store.SMS.Add(capture.SMS{
		Meta: s.store.Next(), API: "otp_retry", To: to, TemplateID: last.templateID,
		Variables: last.vars, Body: last.body, OTP: ExtractOTP(last.code, last.vars, last.body), RequestID: randomHex(12),
	})
	msg91Reply(w, http.StatusOK, "success", "retry send successfully")
}

// GET /api/v5/otp/verify?otp=&mobile= — checks against the last OTP sent to
// that mobile. A code works once and only until its otp_expiry.
// Docs: https://docs.msg91.com/otp/verify-otp
func (s *Server) msg91VerifyOTP(w http.ResponseWriter, r *http.Request) {
	if !msg91Auth(w, r) {
		return
	}
	q := r.URL.Query()
	to, ok := ProviderPhone(q.Get("mobile"))
	if !ok {
		msg91Reply(w, http.StatusBadRequest, "error", "Mobile number is not valid (send the country code and number, e.g. 919000000001)")
		return
	}
	msg, ok := s.otps.verify(to, q.Get("otp"), s.cfg.Now())
	if !ok {
		msg91Reply(w, http.StatusBadRequest, "error", msg)
		return
	}
	msg91Reply(w, http.StatusOK, "success", msg)
}

// POST /api/v5/flow — template ("flow") SMS.
// Body: {"template_id": "...", "short_url": "0", "recipients": [{"mobiles": "919...", "VAR1": "..."}]}
// Older clients send "flow_id" and "sender"; both are accepted.
// Docs: https://docs.msg91.com/sms/send-sms
func (s *Server) msg91Flow(w http.ResponseWriter, r *http.Request) {
	if !msg91Auth(w, r) {
		return
	}
	raw, status := readBody(w, r, smsBodyLimit)
	if status != 0 {
		msg91Reply(w, status, "error", bodyErrText(status))
		return
	}
	var req struct {
		TemplateID string           `json:"template_id"`
		FlowID     string           `json:"flow_id"`
		Sender     string           `json:"sender"`
		Recipients []map[string]any `json:"recipients"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		msg91Reply(w, http.StatusBadRequest, "error", "Invalid JSON body")
		return
	}
	id := req.TemplateID
	if id == "" {
		id = req.FlowID
	}
	if id == "" {
		msg91Reply(w, http.StatusBadRequest, "error", "template_id missing")
		return
	}
	if len(req.Recipients) == 0 {
		msg91Reply(w, http.StatusBadRequest, "error", "recipients missing")
		return
	}
	type item struct {
		to   string
		vars map[string]string
	}
	var items []item
	for i, rc := range req.Recipients {
		vars := stringMap(rc)
		mobiles := vars["mobiles"]
		delete(vars, "mobiles")
		if strings.TrimSpace(mobiles) == "" {
			msg91Reply(w, http.StatusBadRequest, "error", fmt.Sprintf("recipients[%d].mobiles missing", i))
			return
		}
		for _, m := range strings.Split(mobiles, ",") {
			to, ok := ProviderPhone(m)
			if !ok {
				msg91Reply(w, http.StatusBadRequest, "error", fmt.Sprintf("recipients[%d]: mobile number is not valid (send the country code and number)", i))
				return
			}
			items = append(items, item{to: to, vars: vars})
		}
	}
	tpls, err := fixtures.LoadTemplates(s.cfg.Fixtures, "sms/templates.json")
	if err != nil {
		msg91Reply(w, http.StatusInternalServerError, "error", "tz-mock: "+err.Error())
		return
	}
	reqID := randomHex(12)
	for _, it := range items {
		body := RenderMSG91(tpls, id, it.vars, "")
		s.store.SMS.Add(capture.SMS{
			Meta: s.store.Next(), API: "flow", To: it.to, Sender: req.Sender, TemplateID: id,
			Variables: it.vars, Body: body, OTP: ExtractOTP("", it.vars, body), RequestID: reqID,
		})
	}
	msg91Reply(w, http.StatusOK, "success", reqID)
}

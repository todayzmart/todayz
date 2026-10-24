package server

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"todayz.local/e2e/mocks/internal/capture"
	"todayz.local/e2e/mocks/internal/fixtures"
)

// AWS End User Messaging SMS (pinpoint-sms-voice-v2), D-212. The backend's
// SMS_API_URL points the SDK here (http://127.0.0.1:4010/sms-voice); the SDK
// posts AWS JSON 1.0 to that path with the operation in X-Amz-Target.
// Docs: https://docs.aws.amazon.com/pinpoint/latest/apireference_smsvoicev2/API_SendTextMessage.html
const (
	awsSMSTarget      = "PinpointSMSVoiceV2.SendTextMessage"
	awsJSONType       = "application/x-amz-json-1.0"
	awsMaxBodyChars   = 1600   // SendTextMessage's MessageBody limit
	awsMinTTL         = 5      // seconds
	awsMaxTTL         = 259200 // 72 hours
	dltVarMaxRunes    = 30     // India DLT: each {#var#} holds at most 30 characters
	dltTemplatesFile  = "sms/dlt-templates.json"
	awsSMSRequestPath = "/sms-voice"
)

var e164Re = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// awsError writes the AWS JSON 1.0 error shape the SDK decodes: the type in
// X-Amzn-ErrorType and in "__type", the text in "Message". The message never
// repeats a request value (phone, body).
func awsError(w http.ResponseWriter, status int, typ, msg string) {
	w.Header().Set("X-Amzn-ErrorType", typ)
	w.Header().Set("Content-Type", awsJSONType)
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]string{"__type": typ, "Message": msg})
}

// awsSendTextMessage is POST /sms-voice (and /sms-voice/). It checks what the
// real service and India's DLT rules need, records the message like the
// MSG91 mock did (so /_mock/sms/latest-otp keeps working) and answers
// {"MessageId"}. The SigV4 signature is checked for presence only: the
// backend signs local calls with dummy credentials.
func (s *Server) awsSendTextMessage(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=") {
		awsError(w, http.StatusForbidden, "MissingAuthenticationTokenException", "Missing Authentication Token")
		return
	}
	if r.Header.Get("X-Amz-Target") != awsSMSTarget {
		awsError(w, http.StatusBadRequest, "UnknownOperationException", "tz-mock: only "+awsSMSTarget+" is served")
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), awsJSONType) {
		awsError(w, http.StatusBadRequest, "SerializationException", "tz-mock: Content-Type must be "+awsJSONType)
		return
	}
	raw, status := readBody(w, r, smsBodyLimit)
	if status != 0 {
		awsError(w, status, "ValidationException", bodyErrText(status))
		return
	}
	var req struct {
		DestinationPhoneNumber       string            `json:"DestinationPhoneNumber"`
		OriginationIdentity          string            `json:"OriginationIdentity"`
		MessageBody                  string            `json:"MessageBody"`
		MessageType                  string            `json:"MessageType"`
		TimeToLive                   *int              `json:"TimeToLive"`
		DestinationCountryParameters map[string]string `json:"DestinationCountryParameters"`
		DryRun                       bool              `json:"DryRun"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		awsError(w, http.StatusBadRequest, "SerializationException", "Request body is not valid JSON for SendTextMessage")
		return
	}
	if !e164Re.MatchString(req.DestinationPhoneNumber) {
		awsError(w, http.StatusBadRequest, "ValidationException", "DestinationPhoneNumber must be E.164 (+ and the country code)")
		return
	}
	to, ok := ProviderPhone(req.DestinationPhoneNumber)
	if !ok {
		awsError(w, http.StatusBadRequest, "ValidationException", "DestinationPhoneNumber is not a valid number")
		return
	}
	n := utf8.RuneCountInString(req.MessageBody)
	if n == 0 || n > awsMaxBodyChars {
		awsError(w, http.StatusBadRequest, "ValidationException", "MessageBody must be 1 to 1600 characters")
		return
	}
	switch req.MessageType {
	case "", "TRANSACTIONAL", "PROMOTIONAL":
	default:
		awsError(w, http.StatusBadRequest, "ValidationException", "MessageType must be TRANSACTIONAL or PROMOTIONAL")
		return
	}
	if req.TimeToLive != nil && (*req.TimeToLive < awsMinTTL || *req.TimeToLive > awsMaxTTL) {
		awsError(w, http.StatusBadRequest, "ValidationException", "TimeToLive must be 5 to 259200 seconds")
		return
	}
	entity, template := req.DestinationCountryParameters["IN_ENTITY_ID"], req.DestinationCountryParameters["IN_TEMPLATE_ID"]
	if strings.HasPrefix(to, "+91") {
		// India: DLT needs the registered header and both ids on every send.
		if req.OriginationIdentity == "" {
			awsError(w, http.StatusBadRequest, "ValidationException", "tz-mock: an Indian number needs OriginationIdentity (the DLT sender ID)")
			return
		}
		if entity == "" || template == "" {
			awsError(w, http.StatusBadRequest, "ValidationException", "tz-mock: an Indian number needs DestinationCountryParameters IN_ENTITY_ID and IN_TEMPLATE_ID")
			return
		}
	}
	tpls, err := fixtures.LoadTemplates(s.cfg.Fixtures, dltTemplatesFile)
	if err != nil {
		awsError(w, http.StatusInternalServerError, "InternalServerException", "tz-mock: "+err.Error())
		return
	}
	if tpl, ok := tpls[template]; ok && !DLTMatch(tpl, req.MessageBody) {
		// The real operator drops it after AWS accepts it; failing here
		// shows the drift locally instead of as a silent non-delivery.
		awsError(w, http.StatusBadRequest, "ValidationException", "tz-mock: MessageBody does not match the DLT template for IN_TEMPLATE_ID (each {#var#} at most 30 characters)")
		return
	}
	msgID := randomHex(16)
	if req.DryRun {
		writeJSON(w, http.StatusOK, map[string]string{"MessageId": msgID})
		return
	}
	s.store.SMS.Add(capture.SMS{
		Meta: s.store.Next(), API: "send_text_message", To: to, Sender: req.OriginationIdentity,
		EntityID: entity, TemplateID: template, Variables: map[string]string{},
		Body: req.MessageBody, OTP: ExtractOTP("", nil, req.MessageBody), RequestID: msgID,
	})
	w.Header().Set("Content-Type", awsJSONType)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"MessageId": msgID})
}

// DLTMatch reports whether body is tpl with each "{#var#}" filled by at most
// 30 characters (runes), the rest matching exactly.
func DLTMatch(tpl, body string) bool {
	parts := strings.Split(tpl, "{#var#}")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	re, err := regexp.Compile(`^(?s)` + strings.Join(parts, `(.{0,30})`) + `$`)
	if err != nil {
		return false
	}
	return re.MatchString(body)
}

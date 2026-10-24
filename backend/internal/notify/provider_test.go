package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/egress"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/notice"
)

// The reserved test number (never a real person).
const testPhone = "+919000000011"

// loopback reaches httptest servers only (no egress).
var loopback = httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true, Timeout: 5 * time.Second})

// captured is one request a fake MSG91 received.
type captured struct {
	Path, Query, AuthKey, ContentType string
	Body                              map[string]any
	Raw                               string
}

func fakeMSG91(t *testing.T, status int, reply string) (*httptest.Server, func() []captured) {
	t.Helper()
	var mu sync.Mutex
	var got []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c := captured{Path: r.URL.Path, Query: r.URL.RawQuery, AuthKey: r.Header.Get("authkey"), ContentType: r.Header.Get("Content-Type"), Raw: string(b)}
		_ = json.Unmarshal(b, &c.Body)
		mu.Lock()
		got = append(got, c)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []captured {
		mu.Lock()
		defer mu.Unlock()
		return append([]captured(nil), got...)
	}
}

func unseenMessage() Message {
	return Message{
		To: testPhone, Template: "tz_order_unseen", Lang: "en",
		Vars: []Var{{"store", "Lakshmi Traders"}, {"minutes", "3"}},
		Text: "A new order at Lakshmi Traders has not been opened for 3 minutes.",
	}
}

// awsSMSRequest is one SendTextMessage a fake AWS endpoint received.
type awsSMSRequest struct {
	Target, Auth string
	Body         map[string]any
}

// fakeAWSSMS answers SendTextMessage as AWS JSON 1.0 does: 200 with a
// MessageId, or status with an error type and a message that echoes the phone.
func fakeAWSSMS(t *testing.T, status int, errType string) (*httptest.Server, func() []awsSMSRequest) {
	t.Helper()
	var mu sync.Mutex
	var got []awsSMSRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		got = append(got, awsSMSRequest{Target: r.Header.Get("X-Amz-Target"), Auth: r.Header.Get("Authorization"), Body: b})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		if status != http.StatusOK {
			w.Header().Set("X-Amzn-ErrorType", errType)
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"__type":"`+errType+`","Message":"rejected `+testPhone+`"}`)
			return
		}
		_, _ = io.WriteString(w, `{"MessageId":"aws-msg-1"}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []awsSMSRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]awsSMSRequest(nil), got...)
	}
}

// testSMSConfig is a test config whose SMS goes to url (the mocks or httptest).
func testSMSConfig(url string) *config.Config {
	return &config.Config{Env: config.EnvTest, NoEgress: true, Providers: config.Providers{
		SMSAPIURL: url, SMSSenderID: "TODAYZ", SMSDLTEntityID: "1000000000000000001",
		SMSNoticeTemplateIDs: map[string]string{"tz_order_unseen": "1000000000000000201", "tz_order_unseen_hi": "1000000000000000202"},
	}}
}

func awsSMSFor(t *testing.T, url string) *AWSSMS {
	t.Helper()
	p, ok := providersFromConfig(testSMSConfig(url))[notice.SMS].(*AWSSMS)
	require.True(t, ok)
	return p
}

func TestAWSSMS(t *testing.T) {
	srv, got := fakeAWSSMS(t, http.StatusOK, "")
	p := awsSMSFor(t, srv.URL)
	require.Equal(t, "aws_sms", p.Name())
	res, err := p.Send(context.Background(), unseenMessage())
	require.NoError(t, err)
	require.Equal(t, Result{ProviderID: "aws-msg-1", Units: 1}, res)

	m := unseenMessage()
	m.Template, m.Lang = "tz_order_unseen_hi", "hi"
	m.Text = "Lakshmi Traders पर एक नया ऑर्डर 3 मिनट से खोला नहीं गया है। स्वीकार करने के लिए TodayZ बिज़नेस ऐप खोलें।"
	res, err = p.Send(context.Background(), m)
	require.NoError(t, err)
	require.Equal(t, 2, res.Units, "UCS-2: 70 per single part, 67 per part")

	reqs := got()
	require.Len(t, reqs, 2)
	r := reqs[0]
	require.Equal(t, "PinpointSMSVoiceV2.SendTextMessage", r.Target)
	require.True(t, strings.HasPrefix(r.Auth, "AWS4-HMAC-SHA256 "))
	require.Equal(t, testPhone, r.Body["DestinationPhoneNumber"])
	require.Equal(t, "TODAYZ", r.Body["OriginationIdentity"])
	require.Equal(t, "TRANSACTIONAL", r.Body["MessageType"])
	require.Equal(t, unseenMessage().Text, r.Body["MessageBody"], "the rendered catalogue text is the body")
	require.Equal(t, map[string]any{"IN_ENTITY_ID": "1000000000000000001", "IN_TEMPLATE_ID": "1000000000000000201"}, r.Body["DestinationCountryParameters"])
	require.Equal(t, "1000000000000000202", reqs[1].Body["DestinationCountryParameters"].(map[string]any)["IN_TEMPLATE_ID"], "the Hindi template's DLT id")
	require.Equal(t, m.Text, reqs[1].Body["MessageBody"])
}

func TestAWSSMSUnmappedTemplateFails(t *testing.T) {
	srv, got := fakeAWSSMS(t, http.StatusOK, "")
	p := awsSMSFor(t, srv.URL)
	m := unseenMessage()
	m.Template = "tz_something_new"
	_, err := p.Send(context.Background(), m)
	require.ErrorIs(t, err, ErrProvider)
	require.EqualError(t, err, "notify: provider call failed: aws sms: no DLT template id for tz_something_new in SMS_NOTICE_TEMPLATE_IDS")
	require.Empty(t, got(), "nothing is sent without a DLT template id")
}

func TestAWSSMSFailuresNeverCarryThePhone(t *testing.T) {
	for _, c := range []struct {
		status  int
		errType string
	}{
		{http.StatusBadRequest, "ValidationException"},
		{http.StatusTooManyRequests, "ThrottlingException"},
		{http.StatusForbidden, "AccessDeniedException"},
		{http.StatusInternalServerError, "InternalServerException"},
	} {
		srv, got := fakeAWSSMS(t, c.status, c.errType)
		p := awsSMSFor(t, srv.URL)
		_, err := p.Send(context.Background(), unseenMessage())
		require.ErrorIs(t, err, ErrProvider, c.errType)
		require.ErrorContains(t, err, c.errType, "the AWS error code is kept")
		require.ErrorContains(t, err, fmt.Sprintf("status %d", c.status))
		require.NotContains(t, err.Error(), "9000000011", c.errType)
		require.NotContains(t, err.Error(), "Lakshmi", "never the body")
		require.Less(t, len(err.Error()), 200, c.errType)
		require.Len(t, got(), 1, "never retried: a notice is sent at most once")
	}

	// An endpoint that is down: the transport error carries no phone.
	srv, _ := fakeAWSSMS(t, http.StatusOK, "")
	p := awsSMSFor(t, srv.URL)
	srv.Close()
	_, err := p.Send(context.Background(), unseenMessage())
	require.ErrorIs(t, err, ErrProvider)
	require.NotContains(t, err.Error(), "9000000011")
}

func TestAWSSMSRefusedUnderNoEgress(t *testing.T) {
	// Deployed, egress blocked: the guard refuses the instance-role (IMDS)
	// call and the regional endpoint before anything leaves the machine.
	cfg := testSMSConfig("")
	cfg.Env, cfg.AWSRegion = config.EnvProd, "ap-south-1"
	p, ok := providersFromConfig(cfg)[notice.SMS].(*AWSSMS)
	require.True(t, ok)
	_, err := p.Send(context.Background(), unseenMessage())
	require.ErrorIs(t, err, ErrProvider)
	require.ErrorIs(t, err, egress.ErrBlocked)
	require.NotContains(t, err.Error(), "9000000011")
}

func TestMSG91WhatsApp(t *testing.T) {
	srv, got := fakeMSG91(t, http.StatusOK, `{"status":"success","data":{"request_id":"wa-123"}}`)
	p := &MSG91WhatsApp{Client: loopback, BaseURL: srv.URL, AuthKey: "test-wa", IntegratedNumber: "918000000000"}
	m := unseenMessage()
	m.Lang = "hi"
	m.Template = "tz_order_unseen_hi"
	res, err := p.Send(context.Background(), m)
	require.NoError(t, err)
	require.Equal(t, Result{ProviderID: "wa-123", Units: 1}, res)

	r := got()[0]
	require.Equal(t, "/api/v5/whatsapp/whatsapp-outbound-message/bulk/", r.Path)
	require.Equal(t, "test-wa", r.AuthKey)
	require.Equal(t, "918000000000", r.Body["integrated_number"])
	tpl := r.Body["payload"].(map[string]any)["template"].(map[string]any)
	require.Equal(t, "tz_order_unseen_hi", tpl["name"])
	require.Equal(t, "hi", tpl["language"].(map[string]any)["code"])
	tc := tpl["to_and_components"].([]any)[0].(map[string]any)
	require.Equal(t, []any{"919000000011"}, tc["to"])
	comps := tc["components"].(map[string]any)
	require.Equal(t, "Lakshmi Traders", comps["body_1"].(map[string]any)["value"])
	require.Equal(t, "3", comps["body_2"].(map[string]any)["value"])

	srv2, _ := fakeMSG91(t, http.StatusUnauthorized, `{"status":"fail","errors":"bad key for 919000000011"}`)
	p.BaseURL = srv2.URL
	_, err = p.Send(context.Background(), m)
	require.ErrorIs(t, err, ErrProvider)
	require.NotContains(t, err.Error(), "9000000011")
}

func TestProvidersFromConfig(t *testing.T) {
	require.Empty(t, providersFromConfig(nil))

	local := testSMSConfig("https://mocks.todayz.localhost/sms-voice")
	local.Env = config.EnvLocal
	ps := providersFromConfig(local)
	require.Len(t, ps, 1, "SMS only: WhatsApp needs the sender number, push is a later slice")
	require.Equal(t, local.Providers.SMSNoticeTemplateIDs, ps[notice.SMS].(*AWSSMS).Templates)

	outward := testSMSConfig("https://sms-voice.ap-south-1.amazonaws.com")
	require.Empty(t, providersFromConfig(outward), "local and test never sign for a real endpoint")

	noIDs := testSMSConfig("http://127.0.0.1:4010/sms-voice")
	noIDs.Providers.SMSSenderID = ""
	require.Empty(t, providersFromConfig(noIDs), "no sender ID: not configured")

	prod := testSMSConfig("")
	prod.Env = config.EnvProd
	require.Empty(t, providersFromConfig(prod), "deployed without AWS_REGION: not configured")
	prod.AWSRegion = "ap-south-1"
	require.IsType(t, &AWSSMS{}, providersFromConfig(prod)[notice.SMS], "deployed: the instance role, no endpoint needed")

	require.Empty(t, providersFromConfig(&config.Config{Env: config.EnvLocal}))
}

func TestErrProviderIsTheOnlyWrap(t *testing.T) {
	srv, _ := fakeAWSSMS(t, http.StatusTeapot, "SomethingOdd")
	_, err := awsSMSFor(t, srv.URL).Send(context.Background(), unseenMessage())
	require.True(t, errors.Is(err, ErrProvider))
	require.Contains(t, err.Error(), "status 418")
}

// DLT caps each {#var#} at 30 characters (D-212): Deliver cuts every SMS
// param to 30 runes, so a long store name still matches its template.
func TestSMSParamsFitDLT(t *testing.T) {
	long := "Sri Lakshmi Venkateswara General Stores and Provisions" // 54 runes
	got := smsParams(map[string]string{"store": long, "minutes": "3"})
	require.Equal(t, "Sri Lakshmi Venkateswara Gener", got["store"])
	require.Equal(t, 30, utf8.RuneCountInString(got["store"]))
	require.Equal(t, "3", got["minutes"])

	hi := strings.Repeat("दुकान", 8) // 40 runes, Devanagari
	require.Equal(t, 30, utf8.RuneCountInString(smsParams(map[string]string{"store": hi})["store"]), "runes, not bytes")
	require.Equal(t, "Ramu Kirana", smsParams(map[string]string{"store": "Ramu Kirana"})["store"], "short values untouched")
	require.Equal(t, strings.Repeat("a", 29), smsParams(map[string]string{"store": strings.Repeat("a", 29) + "  tail"})["store"], "no trailing space at the cut")

	tx, ok := textFor(notice.KindOrderUnseen, "en", smsParams(map[string]string{"store": long, "minutes": "3"}))
	require.True(t, ok)
	require.Equal(t, "A new order at Sri Lakshmi Venkateswara Gener has not been opened for 3 minutes. Open the TodayZ business app to accept it.", tx.Body)
}

// The SMS kinds' text must be the DLT-registered template text exactly
// (docs/runbooks/aws-founder-steps.md C1): DLT refuses anything else.
func TestSMSTextMatchesDLTTemplates(t *testing.T) {
	dlt := map[string]string{
		"en": "A new order at {#var#} has not been opened for {#var#} minutes. Open the TodayZ business app to accept it.",
		"hi": "{#var#} पर एक नया ऑर्डर {#var#} मिनट से खोला नहीं गया है। स्वीकार करने के लिए TodayZ बिज़नेस ऐप खोलें।",
	}
	def := catalogue[notice.KindOrderUnseen]
	require.Equal(t, map[string]string{"en": "tz_order_unseen", "hi": "tz_order_unseen_hi"}, def.Templates)
	for lang, want := range dlt {
		require.Equal(t, want, rePlace.ReplaceAllString(def.Text[lang].Body, "{#var#}"), lang)
	}
	for kind, d := range catalogue {
		if slices.Contains(d.Channels, notice.SMS) {
			require.Equal(t, notice.KindOrderUnseen, kind, "a new SMS kind needs its DLT template registered and listed here")
		}
	}
}

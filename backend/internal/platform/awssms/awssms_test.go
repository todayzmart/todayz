package awssms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/egress"
)

const testPhone = "+919000000011"

type request struct {
	Method, Path, Target, Auth, ContentType string
	Body                                    map[string]any
}

// fakeAWS answers SendTextMessage the way the AWS JSON 1.0 protocol does and
// records every request.
type fakeAWS struct {
	*httptest.Server
	calls atomic.Int32
	last  atomic.Pointer[request]
	reply func(w http.ResponseWriter)
}

func newFake(t *testing.T) *fakeAWS {
	t.Helper()
	f := &fakeAWS{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.last.Store(&request{
			Method: r.Method, Path: r.URL.Path, Target: r.Header.Get("X-Amz-Target"),
			Auth: r.Header.Get("Authorization"), ContentType: r.Header.Get("Content-Type"), Body: b,
		})
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		if f.reply != nil {
			f.reply(w)
			return
		}
		_, _ = w.Write([]byte(`{"MessageId":"msg-123"}`))
	}))
	t.Cleanup(f.Close)
	return f
}

func awsError(status int, typ, msg string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("X-Amzn-ErrorType", typ)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"__type":"` + typ + `","Message":"` + msg + `"}`))
	}
}

func testConfig(endpoint string) *config.Config {
	return &config.Config{
		Env:      config.EnvTest,
		NoEgress: true,
		Providers: config.Providers{
			SMSAPIURL:      endpoint,
			SMSSenderID:    "TODAYZ",
			SMSDLTEntityID: "1000000000000000001",
		},
	}
}

func TestSendRequestShape(t *testing.T) {
	f := newFake(t)
	c, err := New(testConfig(f.URL + "/sms-voice"))
	require.NoError(t, err)

	id, err := c.Send(context.Background(), Message{
		To: testPhone, Body: "482913 is your TodayZ login code. Do not share it with anyone.",
		TemplateID: "1000000000000000101", TTL: 5 * time.Minute,
	})
	require.NoError(t, err)
	require.Equal(t, "msg-123", id)
	r := f.last.Load()
	require.Equal(t, http.MethodPost, r.Method)
	require.Equal(t, "/sms-voice", r.Path, "SMS_API_URL's path is kept (the mocks serve /sms-voice)")
	require.Equal(t, "PinpointSMSVoiceV2.SendTextMessage", r.Target)
	require.Equal(t, "application/x-amz-json-1.0", r.ContentType)
	require.True(t, strings.HasPrefix(r.Auth, "AWS4-HMAC-SHA256 Credential=local-mock/"), "signed offline with the dummy key")
	require.Contains(t, r.Auth, "/"+LocalRegion+"/sms-voice/aws4_request", "AWS_REGION unset locally: the local region signs")
	require.Equal(t, map[string]any{
		"DestinationPhoneNumber": testPhone,
		"OriginationIdentity":    "TODAYZ",
		"MessageBody":            "482913 is your TodayZ login code. Do not share it with anyone.",
		"MessageType":            "TRANSACTIONAL",
		"TimeToLive":             float64(300),
		"DestinationCountryParameters": map[string]any{
			"IN_ENTITY_ID":   "1000000000000000001",
			"IN_TEMPLATE_ID": "1000000000000000101",
		},
	}, r.Body)
}

func TestSendTemplateAndTTL(t *testing.T) {
	f := newFake(t)
	cfg := testConfig(f.URL)
	cfg.AWSRegion = "ap-south-2"
	c, err := New(cfg)
	require.NoError(t, err)

	_, err = c.Send(context.Background(), Message{To: testPhone, Body: "notice", TemplateID: "1000000000000000201"})
	require.NoError(t, err)
	r := f.last.Load()
	require.Equal(t, "1000000000000000201", r.Body["DestinationCountryParameters"].(map[string]any)["IN_TEMPLATE_ID"], "the caller's template")
	require.NotContains(t, r.Body, "TimeToLive", "no TTL unless asked")
	require.Contains(t, r.Auth, "/ap-south-2/sms-voice/", "AWS_REGION signs when set")

	_, err = c.Send(context.Background(), Message{To: testPhone, Body: "x", TemplateID: "1", TTL: time.Second})
	require.NoError(t, err)
	require.Equal(t, float64(minTTL), f.last.Load().Body["TimeToLive"], "clamped to AWS's minimum")
	_, err = c.Send(context.Background(), Message{To: testPhone, Body: "x", TemplateID: "1", TTL: 30 * 24 * time.Hour})
	require.NoError(t, err)
	require.Equal(t, float64(maxTTL), f.last.Load().Body["TimeToLive"], "clamped to AWS's maximum")

	calls := f.calls.Load()
	_, err = c.Send(context.Background(), Message{To: testPhone, Body: "x"})
	require.ErrorIs(t, err, ErrSend)
	require.ErrorContains(t, err, "no DLT template id")
	_, err = c.Send(context.Background(), Message{TemplateID: "1", Body: "x"})
	require.ErrorIs(t, err, ErrSend)
	require.Equal(t, calls, f.calls.Load(), "refused before any request")
}

// An AWS error keeps its code and status only: the message (which may echo
// the phone or the body) never reaches the error, and nothing is retried.
func TestSendErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reply  func(http.ResponseWriter)
		expect string
	}{
		{"validation", awsError(400, "ValidationException", "bad destination "+testPhone), "ValidationException status 400"},
		{"throttled", awsError(429, "ThrottlingException", "slow down "+testPhone), "ThrottlingException status 429"},
		{"server", awsError(500, "InternalServerException", "oops "+testPhone), "InternalServerException status 500"},
		{"denied", awsError(403, "AccessDeniedException", "no "+testPhone), "AccessDeniedException status 403"},
		{"not json", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("<html>" + testPhone))
		}, "status 502"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			f.reply = tc.reply
			c, err := New(testConfig(f.URL))
			require.NoError(t, err)
			body := "123456 is your TodayZ login code. Do not share it with anyone."
			_, err = c.Send(context.Background(), Message{To: testPhone, Body: body, TemplateID: "1"})
			require.ErrorIs(t, err, ErrSend)
			require.ErrorContains(t, err, tc.expect)
			require.NotContains(t, err.Error(), "9000000011", "never the phone")
			require.NotContains(t, err.Error(), "123456", "never the body")
			require.Equal(t, int32(1), f.calls.Load(), "a send is never retried (at most once)")
		})
	}
}

func TestSendRefusedUnderNoEgress(t *testing.T) {
	// A deployed config whose endpoint is outside this machine, with egress
	// blocked: the guard refuses before any byte leaves (IMDS included).
	cfg := testConfig("https://sms-voice.ap-south-1.amazonaws.com")
	cfg.Env = config.EnvProd
	cfg.AWSRegion = "ap-south-1"
	c, err := New(cfg)
	require.NoError(t, err)
	_, err = c.Send(context.Background(), Message{To: testPhone, Body: "x", TemplateID: "1"})
	require.ErrorIs(t, err, ErrSend)
	require.ErrorIs(t, err, egress.ErrBlocked)
	require.NotContains(t, err.Error(), "9000000011")
}

func TestNewNamesVariablesNeverValues(t *testing.T) {
	cfg := &config.Config{Env: config.EnvTest}
	require.Equal(t, []string{"SMS_API_URL", "SMS_SENDER_ID", "SMS_DLT_ENTITY_ID"}, Missing(cfg))
	_, err := New(cfg)
	require.ErrorIs(t, err, ErrNotConfigured)
	require.ErrorContains(t, err, "SMS_API_URL, SMS_SENDER_ID, SMS_DLT_ENTITY_ID")

	cfg.Env = config.EnvProd
	require.Equal(t, []string{"AWS_REGION", "SMS_SENDER_ID", "SMS_DLT_ENTITY_ID"}, Missing(cfg), "deployed: the region, no endpoint")

	cfg = testConfig("")
	cfg.Env = config.EnvProd
	cfg.AWSRegion = "ap-south-1"
	require.Empty(t, Missing(cfg))
	_, err = New(cfg)
	require.NoError(t, err, "deployed uses the regional endpoint and the instance role")

	for _, u := range []string{
		"https://sms-voice.ap-south-1.amazonaws.com",
		"http://example.com/sms-voice",
		"http://user:pw@127.0.0.1:4010",
		"ftp://127.0.0.1",
		"127.0.0.1:4010",
	} {
		_, err := New(testConfig(u))
		require.ErrorIs(t, err, ErrNotConfigured, u)
		require.EqualError(t, err, "awssms: not configured: SMS_API_URL must be a local address outside staging and prod")
	}
	for _, u := range []string{"http://127.0.0.1:4010/sms-voice", "https://mocks.todayz.localhost/sms-voice", "http://[::1]:4010", "http://localhost:4010"} {
		_, err := New(testConfig(u))
		require.NoError(t, err, u)
	}
}

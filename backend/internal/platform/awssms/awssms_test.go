package awssms

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	// A deployed config with egress blocked: the guard refuses the IMDS call
	// before any byte leaves, so nothing is signed or sent.
	cfg := testConfig("")
	cfg.Env = config.EnvProd
	cfg.AWSRegion = "ap-south-1"
	c, err := New(cfg)
	require.NoError(t, err)
	_, err = c.Send(context.Background(), Message{To: testPhone, Body: "x", TemplateID: "1"})
	require.ErrorIs(t, err, ErrSend)
	require.ErrorIs(t, err, egress.ErrBlocked)
	require.EqualError(t, err, "awssms: send failed: credentials: egress blocked")

	// The metadata client keeps the egress guard although it has its own
	// transport: it refuses 169.254.169.254 itself, before any dial.
	var imdsNext http.RoundTripper
	_, err = newClient(cfg, func(kind string, next http.RoundTripper) http.RoundTripper {
		if kind == "imds" {
			imdsNext = next
		}
		return next
	})
	require.NoError(t, err)
	require.NotNil(t, imdsNext)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, imdsEndpoint+"/latest/api/token", nil)
	require.NoError(t, err)
	resp, err := imdsNext.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.ErrorIs(t, err, egress.ErrBlocked)
}

// rtFunc serves a request in-process: the deployed tests never touch the
// network, and record the URL the SDK built.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func serveInProcess(h http.Handler) rtFunc {
	return func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		resp := rec.Result()
		resp.Request = r
		return resp, nil
	}
}

type imdsCall struct{ Method, Host, Path, Token, TokenTTL string }

// fakeIMDS answers the IMDSv2 flow (PUT token, then GETs carrying it) for
// the role tz-api-role, and records every call. A GET without the token is
// answered 401, as a real IMDSv2-only instance does.
type fakeIMDS struct {
	mu          sync.Mutex
	calls       []imdsCall
	tokenStatus int    // non-zero: the token PUT answers this status
	roleList    int    // non-zero: the role list answers this status
	credsCode   string // the credentials document's Code (default Success)
}

func (f *fakeIMDS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, imdsCall{
		Method: r.Method, Host: r.URL.Host, Path: r.URL.Path,
		Token: r.Header.Get("X-Aws-Ec2-Metadata-Token"), TokenTTL: r.Header.Get("X-Aws-Ec2-Metadata-Token-Ttl-Seconds"),
	})
	f.mu.Unlock()
	switch {
	case r.Method == http.MethodPut && r.URL.Path == "/latest/api/token":
		if f.tokenStatus != 0 {
			w.WriteHeader(f.tokenStatus)
			return
		}
		w.Header().Set("X-Aws-Ec2-Metadata-Token-Ttl-Seconds", "21600")
		_, _ = io.WriteString(w, "imds-token-1")
	case r.Header.Get("X-Aws-Ec2-Metadata-Token") != "imds-token-1":
		w.WriteHeader(http.StatusUnauthorized)
	case r.Method == http.MethodGet && r.URL.Path == "/latest/meta-data/iam/security-credentials/":
		if f.roleList != 0 {
			w.WriteHeader(f.roleList)
			return
		}
		_, _ = io.WriteString(w, "tz-api-role")
	case r.Method == http.MethodGet && r.URL.Path == "/latest/meta-data/iam/security-credentials/tz-api-role":
		code := f.credsCode
		if code == "" {
			code = "Success"
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"Code": code, "Message": "role detail " + testPhone, "Type": "AWS-HMAC",
			"AccessKeyId": "ASIAFAKEIMDSROLE", "SecretAccessKey": "imds-secret", "Token": "session-from-imds",
			"LastUpdated": "2026-01-01T00:00:00Z", "Expiration": "2099-01-01T00:00:00Z",
		})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeIMDS) snapshot() []imdsCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]imdsCall(nil), f.calls...)
}

type smsCall struct{ Host, Path, Auth, SessionToken string }

// deployedClient builds a prod client whose SMS and IMDS traffic is served
// in-process by sms and imds. It checks that the metadata transport never
// uses a proxy.
func deployedClient(t *testing.T, imds *fakeIMDS, sms *[]smsCall) *Client {
	t.Helper()
	cfg := testConfig("")
	cfg.Env = config.EnvProd
	cfg.NoEgress = false
	cfg.AWSRegion = "ap-south-1"
	var mu sync.Mutex
	smsH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*sms = append(*sms, smsCall{Host: r.URL.Host, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), SessionToken: r.Header.Get("X-Amz-Security-Token")})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		_, _ = w.Write([]byte(`{"MessageId":"msg-role"}`))
	})
	c, err := newClient(cfg, func(kind string, next http.RoundTripper) http.RoundTripper {
		tr, ok := next.(*http.Transport)
		require.True(t, ok, kind)
		if kind == "imds" {
			require.Nil(t, tr.Proxy, "IMDS never goes through HTTP(S)_PROXY")
			return serveInProcess(imds)
		}
		return serveInProcess(smsH)
	})
	require.NoError(t, err)
	return c
}

// Deployed (D-212): credentials come only from the instance role over IMDSv2
// at the pinned address. Keys and profiles in the environment and in ~/.aws,
// and an IMDS endpoint override, are all ignored.
func TestDeployedUsesInstanceRoleOnly(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".aws"), 0o700))
	credsFile := filepath.Join(home, ".aws", "credentials")
	configFile := filepath.Join(home, ".aws", "config")
	require.NoError(t, os.WriteFile(credsFile, []byte("[default]\naws_access_key_id = AKIAFROMHOMEFILE\naws_secret_access_key = file-secret\n\n[tz-test]\naws_access_key_id = AKIAFROMHOMEPROFILE\naws_secret_access_key = file-secret\n"), 0o600))
	require.NoError(t, os.WriteFile(configFile, []byte("[profile tz-test]\nregion = us-east-1\n"), 0o600))
	t.Setenv("HOME", home)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credsFile)
	t.Setenv("AWS_CONFIG_FILE", configFile)
	t.Setenv("AWS_PROFILE", "tz-test")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAFROMENVIRONMENT")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
	t.Setenv("AWS_SESSION_TOKEN", "env-session")
	t.Setenv("AWS_EC2_METADATA_SERVICE_ENDPOINT", "http://127.0.0.1:9")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", "http://127.0.0.1:9/creds")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")

	imds := &fakeIMDS{}
	var sms []smsCall
	c := deployedClient(t, imds, &sms)
	for range 2 {
		id, err := c.Send(context.Background(), Message{To: testPhone, Body: "482913 is your TodayZ login code. Do not share it with anyone.", TemplateID: "1"})
		require.NoError(t, err)
		require.Equal(t, "msg-role", id)
	}

	calls := imds.snapshot()
	require.Equal(t, []string{
		"PUT /latest/api/token",
		"GET /latest/meta-data/iam/security-credentials/",
		"GET /latest/meta-data/iam/security-credentials/tz-api-role",
	}, methodsAndPaths(calls), "IMDSv2 token first, then the role; cached for the second send")
	for i, cl := range calls {
		require.Equal(t, "169.254.169.254", cl.Host, "the pinned address, not AWS_EC2_METADATA_SERVICE_ENDPOINT")
		if i == 0 {
			require.NotEmpty(t, cl.TokenTTL)
			continue
		}
		require.Equal(t, "imds-token-1", cl.Token, "no IMDSv1 call (every GET carries the token)")
	}

	require.Len(t, sms, 2)
	for _, s := range sms {
		require.Equal(t, "sms-voice.ap-south-1.amazonaws.com", s.Host, "the regional endpoint")
		require.True(t, strings.HasPrefix(s.Auth, "AWS4-HMAC-SHA256 Credential=ASIAFAKEIMDSROLE/"), "signed with the role's key")
		require.Contains(t, s.Auth, "/ap-south-1/sms-voice/aws4_request")
		require.Equal(t, "session-from-imds", s.SessionToken)
		require.NotContains(t, s.Auth, "AKIAFROM", "never a key from the environment or ~/.aws")
	}
}

func methodsAndPaths(calls []imdsCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Method+" "+c.Path)
	}
	return out
}

// A credentials failure names its reason (codes and fixed words only), sends
// nothing, and never falls back to IMDSv1.
func TestDeployedCredentialsFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		imds   *fakeIMDS
		expect string
	}{
		{"token refused", &fakeIMDS{tokenStatus: http.StatusForbidden}, "credentials: imds token refused"},
		{"no role", &fakeIMDS{roleList: http.StatusNotFound}, "credentials: no instance role"},
		{"role refused", &fakeIMDS{credsCode: "AssumeRoleUnauthorizedAccess"}, "credentials: imds AssumeRoleUnauthorizedAccess"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imds := tc.imds
			var sms []smsCall
			c := deployedClient(t, imds, &sms)
			_, err := c.Send(context.Background(), Message{To: testPhone, Body: "123456 is your TodayZ login code.", TemplateID: "1"})
			require.ErrorIs(t, err, ErrSend)
			require.EqualError(t, err, "awssms: send failed: "+tc.expect)
			require.Empty(t, sms, "nothing is sent without the role's credentials")
			for _, cl := range imds.snapshot() {
				if cl.Method == http.MethodGet {
					require.Equal(t, "imds-token-1", cl.Token, "no IMDSv1 fallback")
				}
			}
		})
	}
}

// A token PUT that never answers (the IMDS hop limit seen from a container)
// reads "imds token timeout", the line to look for on the first deploy.
func TestDeployedIMDSTokenTimeout(t *testing.T) {
	t.Parallel()
	cfg := testConfig("")
	cfg.Env = config.EnvProd
	cfg.NoEgress = false
	cfg.AWSRegion = "ap-south-1"
	var smsCalls atomic.Int32
	c, err := newClient(cfg, func(kind string, _ http.RoundTripper) http.RoundTripper {
		if kind == "sms" {
			return rtFunc(func(*http.Request) (*http.Response, error) {
				smsCalls.Add(1)
				return nil, io.ErrUnexpectedEOF
			})
		}
		return rtFunc(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
	})
	require.NoError(t, err)
	_, err = c.Send(context.Background(), Message{To: testPhone, Body: "x", TemplateID: "1"})
	require.ErrorIs(t, err, ErrSend)
	require.EqualError(t, err, "awssms: send failed: credentials: imds token timeout")
	require.Zero(t, smsCalls.Load())
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

	// Staging and prod refuse any SMS_API_URL, even AWS's own or a local
	// one: a role-signed request (session token, phone, OTP) goes only to the
	// endpoint the SDK resolves.
	for _, env := range []config.Env{config.EnvProd, config.EnvStaging} {
		for _, u := range []string{"https://sms-voice.ap-south-1.amazonaws.com", "http://127.0.0.1:4010/sms-voice", "https://collector.example.com/x"} {
			bad := testConfig(u)
			bad.Env = env
			bad.AWSRegion = "ap-south-1"
			require.Equal(t, []string{"SMS_API_URL"}, Forbidden(bad))
			_, err := New(bad)
			require.ErrorIs(t, err, ErrNotConfigured, u)
			require.EqualError(t, err, "awssms: not configured: SMS_API_URL must be unset in staging and prod")
		}
	}
	require.Empty(t, Forbidden(testConfig("http://127.0.0.1:4010")), "local and test need it")

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

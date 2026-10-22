package telemetry

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/todayz/backend/internal/platform/egress"
)

func TestSentryAllowed(t *testing.T) {
	cases := []struct {
		env, dsn string
		want     bool
	}{
		{"local", "https://x@sentry.invalid/1", false},
		{"test", "https://x@sentry.invalid/1", false},
		{"staging", "https://x@sentry.invalid/1", true},
		{"prod", "https://x@sentry.invalid/1", true},
		{"prod", "", false},
		{"staging", "", false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, SentryAllowed(c.env, c.dsn), c.env+"/"+c.dsn)
	}
}

func TestSetupWithoutEndpointIsNoop(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, LogOptions{Service: "api", Env: "test"})
	st, shutdown, err := Setup(context.Background(), Options{Service: "api", Env: "test", SentryDSN: "https://x@sentry.invalid/1", Log: log})
	require.NoError(t, err)
	require.Equal(t, Status{}, st, "no endpoint, and Sentry never starts in test")
	require.NoError(t, shutdown(context.Background()))
	require.Contains(t, buf.String(), `"sentry":false`)
	require.NotContains(t, buf.String(), "sentry.invalid", "the DSN is never logged")
}

func TestSetupExportsToLoopbackThroughGuardedClient(t *testing.T) {
	var traces, metrics atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/traces":
			traces.Add(1)
		case "/v1/metrics":
			metrics.Add(1)
		}
		require.Equal(t, "tok-test", r.Header.Get("X-Test-Auth"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	oldTP, oldMP := otel.GetTracerProvider(), otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetTracerProvider(oldTP); otel.SetMeterProvider(oldMP) })

	client := egress.NewClient(egress.Options{Block: true, Timeout: 5 * time.Second})
	st, shutdown, err := Setup(context.Background(), Options{
		Service: "api", Env: "test", Version: "t",
		OTLPEndpoint: srv.URL, OTLPHeaders: "X-Test-Auth=tok-test", HTTPClient: client,
	})
	require.NoError(t, err)
	require.True(t, st.Traces)
	require.True(t, st.Metrics)

	_, span := otel.Tracer("test").Start(context.Background(), "op")
	span.End()
	c, err := otel.Meter("test").Int64Counter(MetricPrefix + "test_total")
	require.NoError(t, err)
	c.Add(context.Background(), 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, shutdown(ctx))
	require.Positive(t, traces.Load())
	require.Positive(t, metrics.Load())
}

func TestParseHeaders(t *testing.T) {
	require.Equal(t, map[string]string{"a": "1", "Authorization": "Basic x=y"}, ParseHeaders(" a=1 , Authorization=Basic x=y,bad,"))
	require.Empty(t, ParseHeaders(""))
}

func TestScrubSentryEvent(t *testing.T) {
	e := &sentry.Event{
		Message:   "failed for 9876543210",
		User:      sentry.User{ID: "01JUSER", Email: "a@b.invalid", IPAddress: "1.2.3.4"},
		Request:   &sentry.Request{Data: `{"otp":"1"}`, Cookies: "c=1", Headers: map[string]string{"Authorization": "Bearer x"}},
		Exception: []sentry.Exception{{Value: "otp 123456 rejected"}},
	}
	out := scrubSentryEvent(e, nil)
	require.Equal(t, "01JUSER", out.User.ID)
	require.Empty(t, out.User.Email)
	require.Empty(t, out.User.IPAddress)
	require.Empty(t, out.Request.Data)
	require.Nil(t, out.Request.Headers)
	require.False(t, strings.Contains(out.Message+out.Exception[0].Value, "9876543210"))
	require.NotContains(t, out.Exception[0].Value, "123456")
	require.Nil(t, scrubSentryEvent(nil, nil))
}

// TestScrubSentryEventURLExtraTags (T4): the request URL is redacted and
// loses its query, Extra and Tags are dropped, and only the trace context
// is kept.
func TestScrubSentryEventURLExtraTags(t *testing.T) {
	e := &sentry.Event{
		Request: &sentry.Request{URL: "https://u:hunter2zz@api.todayz.in/v1/otp/9876543210/verify?otp=482913&key=k1zz", QueryString: "otp=482913"},
		Tags:    map[string]string{"otp": "482913"},
		Contexts: map[string]sentry.Context{
			"trace":   {"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"},
			"device":  {"name": "Ravi phone"},
			"request": {"phone": "9876543210"},
		},
	}
	out := scrubSentryEvent(e, nil)
	require.Equal(t, "https://u:***@api.todayz.in/v1/otp/[phone]/verify", out.Request.URL)
	require.Empty(t, out.Request.QueryString)
	require.Nil(t, out.Tags)
	require.Equal(t, map[string]sentry.Context{"trace": {"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"}}, out.Contexts)

	out = scrubSentryEvent(&sentry.Event{Contexts: map[string]sentry.Context{"os": {"name": "x"}}}, nil)
	require.Nil(t, out.Contexts)
}

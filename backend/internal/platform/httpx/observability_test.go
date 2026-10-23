package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/telemetry"
)

type obsRig struct {
	r     *Router
	logs  *bytes.Buffer
	spans *tracetest.SpanRecorder
	mr    *sdkmetric.ManualReader
}

func newObsRig(t *testing.T) *obsRig {
	t.Helper()
	logs := &bytes.Buffer{}
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	mr := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(mr), sdkmetric.WithView(telemetry.ExponentialView()))
	m, err := telemetry.NewMetrics(mp, "api")
	require.NoError(t, err)
	log := telemetry.NewLogger(logs, telemetry.LogOptions{Service: "api", Env: "test", Version: "t"})
	r := NewRouter(Options{
		Log:            log,
		Idempotency:    idempotency.Options{Store: idempotency.NewMemoryStore()},
		Metrics:        m,
		TracerProvider: tp,
		Propagator:     propagation.TraceContext{},
	})
	r.Get("/v1/orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		telemetry.Logger(r.Context()).InfoContext(r.Context(), "handler ran")
		_, _ = w.Write([]byte("{}"))
	})
	r.Get("/v1/boom", func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.New(genproblems.Internal, ""))
	})
	return &obsRig{r: r, logs: logs, spans: spans, mr: mr}
}

func (o *obsRig) do(path string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	o.r.ServeHTTP(w, req)
	return w
}

func (o *obsRig) collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, o.mr.Collect(context.Background(), &rm))
	return rm
}

func findMetric(rm metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

// TestOneRequestIsMetricLogAndTrace is SC-OB-01 in process: one request shows
// up as a RED metric with an exemplar, a log line and a server span, all with
// the trace id of the caller's traceparent.
func TestOneRequestIsMetricLogAndTrace(t *testing.T) {
	o := newObsRig(t)
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	w := o.do("/v1/orders/01JB0S1W6W8ZV7T1C3A6M9N4PQ", map[string]string{
		"traceparent": "00-" + traceID + "-00f067aa0ba902b7-01",
		"Origin":      "https://app.todayz.localhost",
	})
	require.Equal(t, 200, w.Code)

	// trace: one server span, child of the caller, named by the template.
	ended := o.spans.Ended()
	require.Len(t, ended, 1)
	sp := ended[0]
	require.Equal(t, traceID, sp.SpanContext().TraceID().String())
	require.Equal(t, "GET /v1/orders/{id}", sp.Name())

	// log: the handler line and the summary carry the same trace id and the route template.
	var sawSummary, sawHandler bool
	for _, line := range strings.Split(strings.TrimSpace(o.logs.String()), "\n") {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), line)
		require.Equal(t, traceID, rec["trace_id"], line)
		require.NotEmpty(t, rec["span_id"], line)
		require.NotEmpty(t, rec["request_id"], line)
		switch rec["msg"] {
		case "http request":
			sawSummary = true
			require.Equal(t, "/v1/orders/{id}", rec["route"])
			require.EqualValues(t, 200, rec["status"])
			require.Equal(t, "anonymous", rec["user_kind"])
		case "handler ran":
			sawHandler = true
		}
	}
	require.True(t, sawSummary && sawHandler)

	// metric: the count with allowed labels, and an exemplar linking to the trace.
	rm := o.collect(t)
	require.Empty(t, telemetry.CheckResourceMetrics(&rm))
	cnt, ok := findMetric(rm, "tz_http_server_requests_total")
	require.True(t, ok)
	sum := cnt.Data.(metricdata.Sum[int64])
	require.Len(t, sum.DataPoints, 1)
	attrs := map[string]string{}
	for _, kv := range sum.DataPoints[0].Attributes.ToSlice() {
		attrs[string(kv.Key)] = kv.Value.String()
	}
	require.Equal(t, map[string]string{
		"service": "api", "route": "/v1/orders/{id}", "method": "GET",
		"status_class": "2xx", "code": "none", "app": "customer",
	}, attrs)
	hist, ok := findMetric(rm, "tz_http_server_duration_seconds")
	require.True(t, ok)
	eh := hist.Data.(metricdata.ExponentialHistogram[float64])
	require.Len(t, eh.DataPoints, 1, "an exponential histogram (05 §2.1)")
	require.NotEmpty(t, eh.DataPoints[0].Exemplars)
	require.Equal(t, traceID, fmt.Sprintf("%x", eh.DataPoints[0].Exemplars[0].TraceID))
}

// TestServerErrorsAreFingerprinted: a 5xx logs err_code and a fingerprint and
// counts under status_class 5xx with the problem code.
func TestServerErrorsAreFingerprinted(t *testing.T) {
	o := newObsRig(t)
	require.Equal(t, 500, o.do("/v1/boom", nil).Code)
	logs := o.logs.String()
	require.Contains(t, logs, `"msg":"http request"`)
	require.Contains(t, logs, `"err_code":"internal"`)
	require.Contains(t, logs, `"fingerprint":"`)
	rm := o.collect(t)
	cnt, _ := findMetric(rm, "tz_http_server_requests_total")
	dp := cnt.Data.(metricdata.Sum[int64]).DataPoints[0]
	code, _ := dp.Attributes.Value("code")
	class, _ := dp.Attributes.Value("status_class")
	require.Equal(t, "internal", code.AsString())
	require.Equal(t, "5xx", class.AsString())
	ended := o.spans.Ended()
	require.Len(t, ended, 1)
	require.Equal(t, "Error", ended[0].Status().Code.String())
}

// TestNoPIIInMetricLogOrTrace is SC-OB-02 in process: a phone number in the
// path and query, a bearer token and a cookie reach none of the three.
func TestNoPIIInMetricLogOrTrace(t *testing.T) {
	o := newObsRig(t)
	secrets := []string{"9876543210", "tok_live_abcdef123456", "sessionsecret99"}
	o.do("/v1/orders/9876543210?phone=9876543210&otp=482913", map[string]string{
		"Authorization": "Bearer tok_live_abcdef123456",
		"Cookie":        "sid=sessionsecret99",
	})
	rm := o.collect(t)
	b, err := json.Marshal(rm)
	require.NoError(t, err)
	var spans strings.Builder
	for _, s := range o.spans.Ended() {
		spans.WriteString(s.Name())
		for _, kv := range s.Attributes() {
			spans.WriteString(string(kv.Key) + "=" + kv.Value.String() + " ")
		}
	}
	all := o.logs.String() + string(b) + spans.String()
	for _, s := range append(secrets, "482913") {
		require.NotContains(t, all, s)
	}
}

// TestUnmatchedRoutesShareOneSeries: a scanner hitting random paths cannot
// create series (cardinality).
func TestUnmatchedRoutesShareOneSeries(t *testing.T) {
	o := newObsRig(t)
	for i := 0; i < 20; i++ {
		o.do(fmt.Sprintf("/nope/%d", i), nil)
	}
	rm := o.collect(t)
	cnt, _ := findMetric(rm, "tz_http_server_requests_total")
	require.Len(t, cnt.Data.(metricdata.Sum[int64]).DataPoints, 1)
}

package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func newRig(t *testing.T) (*Metrics, *sdkmetric.ManualReader, *sdkmetric.MeterProvider) {
	t.Helper()
	mr := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(mr), sdkmetric.WithView(ExponentialView()))
	m, err := NewMetrics(mp, "api")
	require.NoError(t, err)
	return m, mr, mp
}

// TestRegisteredMetricsUseAllowedLabels (05 §2.1) fails when any instrument
// this package registers carries a label outside AllowedLabels or a name
// without the tz_ prefix. It drives every instrument once, with hostile
// label inputs.
func TestRegisteredMetricsUseAllowedLabels(t *testing.T) {
	m, mr, mp := newRig(t)
	ctx := context.Background()
	done := m.RequestStart(ctx)
	done("/v1/x/{id}", "GET", 200, "", "customer", 0.01, 10)
	m.SSEOpened(ctx, "customer")
	m.SSEClosed(ctx, "customer")
	m.SSEPublished(ctx, "order")
	m.SSEReconnect(ctx, "customer")
	m.SSEDeliveryLag(ctx, "customer", 0.2)
	for _, k := range []string{"lcp", "inp", "cls", "ttfb", "fcp", "route_change", "long_task", "error"} {
		m.RecordRUM(ctx, RUMSample{App: "customer", Route: "/", Platform: "web", Conn: "4g", AppVersion: "1", Metric: k, Value: 1})
	}
	unreg, err := RegisterRuntime(mp, "api")
	require.NoError(t, err)
	defer func() { _ = unreg() }()

	var rm metricdata.ResourceMetrics
	require.NoError(t, mr.Collect(ctx, &rm))
	require.NotEmpty(t, rm.ScopeMetrics)
	require.Empty(t, CheckResourceMetrics(&rm))
}

// TestLabelGuardCatchesAViolation proves the checker itself fails on a bad
// label and a bad name, so the test above can fail.
func TestLabelGuardCatchesAViolation(t *testing.T) {
	mr := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(mr))
	meter := mp.Meter("x")
	c, _ := meter.Int64Counter("tz_bad_total")
	c.Add(context.Background(), 1, metricOpt(attribute.String("user_id", "01JB")))
	d, _ := meter.Int64Counter("no_prefix_total")
	d.Add(context.Background(), 1)
	var rm metricdata.ResourceMetrics
	require.NoError(t, mr.Collect(context.Background(), &rm))
	bad := CheckResourceMetrics(&rm)
	require.Len(t, bad, 2)
	require.Contains(t, bad[0]+bad[1], `label "user_id"`)
	require.Contains(t, bad[0]+bad[1], "no_prefix_total")
}

func TestLabelValueAndRegistration(t *testing.T) {
	require.Equal(t, "other", LabelValue("kind", "orders.expire"), "unknown until W1-11 registers it")
	RegisterLabelValues("kind", "orders.expire")
	require.Equal(t, "orders.expire", LabelValue("kind", "orders.expire"))
	for _, c := range JobClasses {
		require.Equal(t, c, LabelValue("class", c))
	}
	require.Equal(t, "other", LabelValue("class", "everything"))
	require.Panics(t, func() { RegisterLabelValues("user_id", "x") })
	require.Equal(t, "other", LabelValue("method", "BREW"))
	require.Equal(t, "none", CodeLabel(""))
	require.Equal(t, "other", CodeLabel("Has Space; DROP"))
	require.Equal(t, "4xx", StatusClass(404))
}

// TestRUMCardinalityIsBounded: a hostile browser cannot mint series.
func TestRUMCardinalityIsBounded(t *testing.T) {
	m, mr, _ := newRig(t)
	ctx := context.Background()
	for i := 0; i < 500; i++ {
		m.RecordRUM(ctx, RUMSample{App: "customer", Route: "/r/" + string(rune('a'+i%26)) + string(rune('a'+i/26%26)), Platform: "web", Conn: "4g",
			AppVersion: "v" + string(rune('a'+i%26)), Metric: "lcp", Value: 100})
	}
	var rm metricdata.ResourceMetrics
	require.NoError(t, mr.Collect(ctx, &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, mt := range sm.Metrics {
			if mt.Name != "tz_rum_lcp_seconds" {
				continue
			}
			n := len(mt.Data.(metricdata.ExponentialHistogram[float64]).DataPoints)
			// 60 routes + other, times the versions seen (26 distinct values, bounded by input)
			require.LessOrEqual(t, n, 61*26)
			routes := map[string]struct{}{}
			for _, dp := range mt.Data.(metricdata.ExponentialHistogram[float64]).DataPoints {
				r, _ := dp.Attributes.Value("route")
				routes[r.AsString()] = struct{}{}
			}
			require.LessOrEqual(t, len(routes), 61)
		}
	}
}

func TestStatementName(t *testing.T) {
	require.Equal(t, "GetStoreBySlug", StatementName("-- name: GetStoreBySlug :one\nSELECT * FROM stores WHERE slug = $1"))
	require.Equal(t, "SELECT", StatementName("select 1"))
	require.Equal(t, "query", StatementName("  "))
}

func TestAppFromOrigin(t *testing.T) {
	require.Equal(t, "customer", AppFromOrigin("https://app.todayz.in"))
	require.Equal(t, "business", AppFromOrigin("https://business.todayz.localhost"))
	require.Equal(t, "admin", AppFromOrigin("https://admin.todayz.in"))
	require.Equal(t, "site", AppFromOrigin("https://todayz.in"))
	require.Equal(t, "api", AppFromOrigin(""))
	require.Equal(t, "other", AppFromOrigin("https://evil.example"))
}

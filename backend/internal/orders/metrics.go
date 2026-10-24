package orders

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/internal/platform/telemetry"
)

var (
	metricsOnce      sync.Once
	mReconcileFailed metric.Int64Counter
)

// ordersMetrics registers the package's counters once.
func ordersMetrics() {
	metricsOnce.Do(func() {
		m := otel.Meter("todayz/orders")
		mReconcileFailed, _ = m.Int64Counter(telemetry.MetricPrefix+"order_reconcile_failed_total",
			metric.WithDescription("Orders past a deadline whose timer outcome the reconcile sweep could not apply (retried next run), by timer kind"))
	})
}

// reconcileFailed counts an order the sweep could not apply
// (tz_order_reconcile_failed_total{kind}; alert tz-order-reconcile-failed in
// infra/grafana/alerts/orders.yaml).
func reconcileFailed(ctx context.Context, kind string) {
	ordersMetrics()
	if mReconcileFailed != nil {
		mReconcileFailed.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", telemetry.LabelValue("kind", kind))))
	}
}

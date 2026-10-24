package notify

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Outcomes of one outside message (the `outcome` label). The skipped_*
// outcomes are counted by Send, sent and failed by Deliver.
const (
	OutcomeSent            = "sent"
	OutcomeFailed          = "failed"
	OutcomeSkippedFlag     = "skipped_flag"     // notify_<channel>_enabled is off
	OutcomeSkippedProvider = "skipped_provider" // no provider key (integration missing)
	OutcomeSkippedPref     = "skipped_pref"     // the user switched the kind off
	OutcomeSkippedKind     = "skipped_kind"     // the kind does not use the channel
	OutcomeSkippedPhone    = "skipped_phone"    // no usable phone
	OutcomeSkippedJobs     = "skipped_jobs"     // no job queue in this process
)

func init() {
	ch := make([]string, 0, 3)
	for _, c := range notice.Channels() {
		ch = append(ch, string(c))
	}
	telemetry.RegisterLabelValues("channel", ch...)
	telemetry.RegisterLabelValues("provider", "aws_sms", "msg91", "none")
	telemetry.RegisterLabelValues("outcome", OutcomeSent, OutcomeFailed, OutcomeSkippedFlag,
		OutcomeSkippedProvider, OutcomeSkippedPref, OutcomeSkippedKind, OutcomeSkippedPhone, OutcomeSkippedJobs)
	telemetry.RegisterLabelValues("kind", Kinds()...)
	telemetry.RegisterLabelValues("lang", Langs...)
	telemetry.RegisterLabelValues("app", AppCustomer, AppBusiness)
}

var (
	metricsOnce sync.Once
	mDeliveries metric.Int64Counter
	mCost       metric.Int64Counter
	mUnits      metric.Int64Counter
	mLatency    metric.Float64Histogram
	mInbox      metric.Int64Counter
)

func instruments() {
	metricsOnce.Do(func() {
		m := otel.Meter("todayz/notify")
		mDeliveries, _ = m.Int64Counter("tz_notify_deliveries_total",
			metric.WithDescription("Outside messages by channel, provider, kind and outcome (sent, failed, skipped_*)"))
		mCost, _ = m.Int64Counter("tz_notify_cost_paise_total",
			metric.WithDescription("Estimated provider cost of sent outside messages, in paise"))
		mUnits, _ = m.Int64Counter("tz_notify_units_total",
			metric.WithDescription("Billable units of sent outside messages (SMS segments, WhatsApp messages)"))
		mLatency, _ = m.Float64Histogram("tz_notify_provider_seconds",
			metric.WithDescription("Provider call latency"), metric.WithUnit("s"))
		mInbox, _ = m.Int64Counter("tz_notify_inbox_total",
			metric.WithDescription("In-app inbox rows written, by app and kind"))
	})
}

func attrs(ch notice.Channel, provider, kind, outcome string) metric.MeasurementOption {
	return metric.WithAttributes(
		attribute.String("channel", telemetry.LabelValue("channel", string(ch))),
		attribute.String("provider", telemetry.LabelValue("provider", provider)),
		attribute.String("kind", telemetry.LabelValue("kind", kind)),
		attribute.String("outcome", telemetry.LabelValue("outcome", outcome)),
	)
}

func countDelivery(ctx context.Context, ch notice.Channel, provider, kind, outcome string) {
	instruments()
	if mDeliveries != nil {
		mDeliveries.Add(ctx, 1, attrs(ch, provider, kind, outcome))
	}
}

func countCost(ctx context.Context, ch notice.Channel, provider string, units int, paise int64) {
	instruments()
	o := metric.WithAttributes(
		attribute.String("channel", telemetry.LabelValue("channel", string(ch))),
		attribute.String("provider", telemetry.LabelValue("provider", provider)),
	)
	if mUnits != nil && units > 0 {
		mUnits.Add(ctx, int64(units), o)
	}
	if mCost != nil && paise > 0 {
		mCost.Add(ctx, paise, o)
	}
}

func observeLatency(ctx context.Context, ch notice.Channel, provider string, d time.Duration) {
	instruments()
	if mLatency != nil {
		mLatency.Record(ctx, d.Seconds(), metric.WithAttributes(
			attribute.String("channel", telemetry.LabelValue("channel", string(ch))),
			attribute.String("provider", telemetry.LabelValue("provider", provider)),
		))
	}
}

func countInbox(ctx context.Context, app, kind string) {
	instruments()
	if mInbox != nil {
		mInbox.Add(ctx, 1, metric.WithAttributes(
			attribute.String("app", telemetry.LabelValue("app", app)),
			attribute.String("kind", telemetry.LabelValue("kind", kind)),
		))
	}
}

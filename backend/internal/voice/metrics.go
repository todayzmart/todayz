package voice

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/internal/platform/telemetry"
)

// Draft outcomes on the voice metrics.
const (
	outcomeReady  = "ready"
	outcomeFailed = "failed"
)

func init() { telemetry.RegisterLabelValues("outcome", outcomeReady, outcomeFailed) }

var (
	metricsOnce sync.Once
	mLatency    metric.Float64Histogram
	mMatched    metric.Float64Histogram
	mCost       metric.Int64Counter
)

func instruments() {
	metricsOnce.Do(func() {
		m := otel.Meter("todayz/voice")
		mLatency, _ = m.Float64Histogram("tz_voice_draft_duration_seconds",
			metric.WithDescription("Recording to ready or failed draft: speech to text, candidates and the model, by outcome"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.5, 1, 2, 3, 5, 8, 13, 20, 30, 60, 90))
		mMatched, _ = m.Float64Histogram("tz_voice_matched_ratio",
			metric.WithDescription("Share of a ready voice draft's lines that matched a store item"),
			metric.WithExplicitBucketBoundaries(0, 0.25, 0.5, 0.75, 0.9, 1))
		mCost, _ = m.Int64Counter("tz_voice_cost_paise_total",
			metric.WithDescription("Model cost of voice stock drafts in paise, by outcome"),
			metric.WithUnit("paise"))
	})
}

// recordDraft records one finished draft. matched < 0 skips the matched ratio (a failure).
func recordDraft(ctx context.Context, outcome string, took time.Duration, matched float64, costPaise int64) {
	instruments()
	attrs := metric.WithAttributes(attribute.String("outcome", telemetry.LabelValue("outcome", outcome)))
	if mLatency != nil {
		mLatency.Record(ctx, took.Seconds(), attrs)
	}
	if matched >= 0 && mMatched != nil {
		mMatched.Record(ctx, matched)
	}
	if costPaise > 0 && mCost != nil {
		mCost.Add(ctx, costPaise, attrs)
	}
}

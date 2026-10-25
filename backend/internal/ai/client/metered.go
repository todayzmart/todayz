package client

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Features are the ai_drafts feature names and the customer features that keep no draft
// (11 §1 rules 2 and 11, 04 §2.8). They are the `feature` label values; a feature package
// adds its name here in its own change.
var Features = []string{
	"voice_stock", "supplier_bill", "shelf_photo", "insight_text", "coach_card", "review_reply",
	"banner_copy", "image_check", "list_parse", "search_assist", "voice_search",
	"catalogue_cleanup", "ticket_triage", "risk_summary",
	// ai_selftest is the platform's own eval suite (internal/ai/eval/selftest).
	"ai_selftest",
}

// KnownFeature reports whether name is in Features.
func KnownFeature(name string) bool { return slices.Contains(Features, name) }

// Call outcomes beyond telemetry's base set.
const (
	OutcomeOK          = "ok"
	OutcomeError       = "error"
	OutcomeTimeout     = "timeout"
	OutcomeRefused     = "refused"      // the model declined
	OutcomeTruncated   = "truncated"    // cut at max_tokens
	OutcomeInvalid     = "invalid"      // no tool call, or the output failed the schema or grounding check
	OutcomeRateLimited = "rate_limited" // the provider's 429
	OutcomeCapped      = "capped"       // refused by our daily caps before any call
)

// Rejection reasons for RecordRejected.
const (
	RejectSchema    = "schema"
	RejectGrounding = "grounding"
)

func init() {
	telemetry.RegisterLabelValues("feature", Features...)
	models := make([]string, 0, len(Prices)+1)
	for m := range Prices {
		models = append(models, m)
	}
	telemetry.RegisterLabelValues("model", append(models, "stt")...)
	telemetry.RegisterLabelValues("outcome", OutcomeRefused, OutcomeTruncated, OutcomeInvalid, OutcomeRateLimited, OutcomeCapped)
	telemetry.RegisterLabelValues("reason", RejectSchema, RejectGrounding)
}

var (
	metricsOnce sync.Once
	mCalls      metric.Int64Counter
	mTokens     metric.Int64Counter
	mCost       metric.Int64Counter
	mLatency    metric.Float64Histogram
	mRejected   metric.Int64Counter
)

func instruments() {
	metricsOnce.Do(func() {
		m := otel.Meter("todayz/ai")
		mCalls, _ = m.Int64Counter("tz_ai_calls_total",
			metric.WithDescription("AI provider calls by feature, model and outcome; capped calls never reached the provider"))
		mTokens, _ = m.Int64Counter("tz_ai_tokens_total",
			metric.WithDescription("Tokens billed (input, cache and output)"))
		mCost, _ = m.Int64Counter("tz_ai_cost_paise_total",
			metric.WithDescription("AI spend in paise at list price"))
		mLatency, _ = m.Float64Histogram("tz_ai_latency_seconds",
			metric.WithDescription("AI provider call latency"), metric.WithUnit("s"))
		mRejected, _ = m.Int64Counter("tz_ai_rejected_total",
			metric.WithDescription("Model outputs rejected by the server-side schema or number-grounding check"))
	})
}

func fm(feature, model string) attribute.Set {
	return attribute.NewSet(
		attribute.String("feature", telemetry.LabelValue("feature", feature)),
		attribute.String("model", telemetry.LabelValue("model", model)),
	)
}

// RecordCall counts one call. Guard uses it for capped calls (model "" → other).
func RecordCall(ctx context.Context, feature, model, outcome string) {
	instruments()
	if mCalls != nil {
		mCalls.Add(ctx, 1, metric.WithAttributes(
			attribute.String("feature", telemetry.LabelValue("feature", feature)),
			attribute.String("model", telemetry.LabelValue("model", model)),
			attribute.String("outcome", telemetry.LabelValue("outcome", outcome)),
		))
	}
}

// RecordRejected counts a model output the feature threw away (reason RejectSchema or
// RejectGrounding). The feature then shows its template text or marks the draft failed.
func RecordRejected(ctx context.Context, feature, reason string) {
	instruments()
	if mRejected != nil {
		mRejected.Add(ctx, 1, metric.WithAttributes(
			attribute.String("feature", telemetry.LabelValue("feature", feature)),
			attribute.String("reason", telemetry.LabelValue("reason", reason)),
		))
	}
}

// Outcome maps a call error to its outcome label.
func Outcome(err error) string {
	var pe *ProviderError
	var ne net.Error
	switch {
	case err == nil:
		return OutcomeOK
	case errors.Is(err, ErrRefused):
		return OutcomeRefused
	case errors.Is(err, ErrTruncated):
		return OutcomeTruncated
	case errors.Is(err, ErrNoToolCall):
		return OutcomeInvalid
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return OutcomeTimeout
	case errors.As(err, &pe) && pe.Status == 429:
		return OutcomeRateLimited
	default:
		return OutcomeError
	}
}

// Metered wraps an LLM with metrics, a span, cost and latency (05 §14).
type Metered struct {
	next  LLM
	clock clock.Clock
	tr    trace.Tracer
}

// NewMetered wraps next.
func NewMetered(next LLM, c clock.Clock) *Metered {
	return &Metered{next: next, clock: c, tr: otel.Tracer("todayz/ai")}
}

// Complete implements LLM. The span carries feature, model, outcome and token counts, never
// prompt or reply text.
func (m *Metered) Complete(ctx context.Context, req Request) (Response, error) {
	ctx, span := m.tr.Start(ctx, "ai.complete", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("ai.feature", req.Feature), attribute.String("ai.model", req.Model)))
	defer span.End()
	start := m.clock.Now()
	res, err := m.next.Complete(ctx, req)
	res.Latency = m.clock.Now().Sub(start)
	model := res.Model
	if model == "" {
		model = req.Model
	}
	res.CostPaise = CostPaise(model, res.Usage)
	outcome := Outcome(err)

	instruments()
	set := metric.WithAttributeSet(fm(req.Feature, model))
	RecordCall(ctx, req.Feature, model, outcome)
	if mLatency != nil {
		mLatency.Record(ctx, res.Latency.Seconds(), set)
	}
	tokens := int64(res.Usage.InputTokens + res.Usage.OutputTokens + res.Usage.CacheReadTokens + res.Usage.CacheWriteTokens)
	if mTokens != nil && tokens > 0 {
		mTokens.Add(ctx, tokens, set)
	}
	if mCost != nil && res.CostPaise > 0 {
		mCost.Add(ctx, res.CostPaise, set)
	}
	span.SetAttributes(attribute.String("ai.outcome", outcome),
		attribute.Int("ai.input_tokens", res.Usage.InputTokens), attribute.Int("ai.output_tokens", res.Usage.OutputTokens),
		attribute.Int64("ai.cost_paise", res.CostPaise))
	if err != nil {
		span.SetStatus(codes.Error, outcome)
	}
	return res, err
}

// MeteredSTT wraps an STT with the call and latency metrics (model label "stt"). Speech cost
// is not metered yet (W3-10 REPORT open question).
type MeteredSTT struct {
	next    STT
	clock   clock.Clock
	feature string
	tr      trace.Tracer
}

// NewMeteredSTT wraps next for feature (voice_stock or voice_search).
func NewMeteredSTT(next STT, c clock.Clock, feature string) *MeteredSTT {
	return &MeteredSTT{next: next, clock: c, feature: feature, tr: otel.Tracer("todayz/ai")}
}

// Transcribe implements STT.
func (m *MeteredSTT) Transcribe(ctx context.Context, a Audio) (Transcript, error) {
	ctx, span := m.tr.Start(ctx, "ai.transcribe", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("ai.feature", m.feature)))
	defer span.End()
	start := m.clock.Now()
	t, err := m.next.Transcribe(ctx, a)
	outcome := Outcome(err)
	instruments()
	RecordCall(ctx, m.feature, "stt", outcome)
	if mLatency != nil {
		mLatency.Record(ctx, m.clock.Now().Sub(start).Seconds(), metric.WithAttributeSet(fm(m.feature, "stt")))
	}
	span.SetAttributes(attribute.String("ai.outcome", outcome))
	if err != nil {
		span.SetStatus(codes.Error, outcome)
	}
	return t, err
}

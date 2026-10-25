package business

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/internal/platform/telemetry"
)

// Owner-code failures on the field metric (the `outcome` label): a wrong code
// with tries left, the wrong code that used the last try, and a try after the
// code was locked.
const (
	ownerCodeWrong     = "wrong_code"
	ownerCodeLockedOut = "locked_out"
	ownerCodeLocked    = "locked"
)

func init() {
	telemetry.RegisterLabelValues("outcome", ownerCodeWrong, ownerCodeLockedOut, ownerCodeLocked)
}

var (
	fieldMetricsOnce sync.Once
	mOwnerCodeFails  metric.Int64Counter
)

// countOwnerCodeFailure counts one failed owner-code try. No number, code or
// business is a label.
func countOwnerCodeFailure(ctx context.Context, outcome string) {
	fieldMetricsOnce.Do(func() {
		mOwnerCodeFails, _ = otel.Meter("todayz/business").Int64Counter("tz_field_owner_code_failures_total",
			metric.WithDescription("Failed owner-code tries on the field routes (A-04), by outcome: wrong_code, locked_out, locked"))
	})
	if mOwnerCodeFails != nil {
		mOwnerCodeFails.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", telemetry.LabelValue("outcome", outcome))))
	}
}

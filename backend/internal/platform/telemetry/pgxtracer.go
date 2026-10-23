package telemetry

import (
	"context"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var reSQLCName = regexp.MustCompile(`^\s*--\s*name:\s*([A-Za-z0-9_]+)`)

// StatementName returns the sqlc statement name from a leading
// `-- name: X :one` comment, else the SQL verb ("SELECT", "INSERT", …) so a
// hand-written query still gets a bounded span name. The SQL text and its
// arguments are never recorded (05 §2.3).
func StatementName(sql string) string {
	if m := reSQLCName.FindStringSubmatch(sql); m != nil {
		return m[1]
	}
	f := strings.Fields(sql)
	if len(f) == 0 {
		return "query"
	}
	return strings.ToUpper(strings.TrimRight(f[0], ";("))
}

// PgxTracer is a pgx.QueryTracer that opens a client span per query, named by
// StatementName, with db.system and db.operation.name only.
type PgxTracer struct{ tp trace.TracerProvider }

// NewPgxTracer returns the tracer (the global provider when tp is nil).
func NewPgxTracer(tp trace.TracerProvider) *PgxTracer { return &PgxTracer{tp: tp} }

type spanKey struct{}

// TraceQueryStart implements pgx.QueryTracer.
func (t *PgxTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	tp := t.tp
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	name := StatementName(d.SQL)
	ctx, span := tp.Tracer(MeterName).Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("db.system.name", "postgresql"), attribute.String("db.operation.name", name)))
	return context.WithValue(ctx, spanKey{}, span)
}

// TraceQueryEnd implements pgx.QueryTracer.
func (t *PgxTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	span, ok := ctx.Value(spanKey{}).(trace.Span)
	if !ok {
		return
	}
	if d.Err != nil {
		span.SetStatus(codes.Error, "query failed") // never the error text: it can carry values
	}
	span.End()
}

// Package telemetry bootstraps logs, traces, metrics and error reporting
// (05-observability.md):
//
//   - slog JSON to stdout, field names from 05 (ts, level, msg, service, env,
//     version, request_id, trace_id, span_id, ...), behind the redaction handler.
//   - OpenTelemetry traces and metrics over OTLP HTTP (metric names start tz_).
//   - Sentry, init only: off when SENTRY_DSN is empty, never in local or test.
//
// W1-07 extends this package (RED metrics, runtime and pool metrics).
package telemetry

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/todayz/backend/internal/platform/reqctx"
)

// LogOptions configure the process logger.
type LogOptions struct {
	Level   slog.Leveler
	Service string // "api" or "worker" or a command name
	Env     string
	Version string
}

// NewLogger returns the process logger: JSON, redacted, with request and trace
// ids taken from the context of each call (use the *Context logging methods).
func NewLogger(w io.Writer, o LogOptions) *slog.Logger {
	if o.Level == nil {
		o.Level = slog.LevelInfo
	}
	json := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: o.Level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Key = "ts"
				a.Value = slog.StringValue(a.Value.Time().UTC().Format("2006-01-02T15:04:05.000Z07:00"))
			}
			if len(groups) == 0 && a.Key == slog.LevelKey {
				a.Value = slog.StringValue(levelName(a.Value.Any()))
			}
			return a
		},
	})
	var h slog.Handler = &contextHandler{next: json}
	h = NewRedactHandler(h)
	return slog.New(h).With(
		slog.String("service", o.Service),
		slog.String("env", o.Env),
		slog.String("version", o.Version),
	)
}

// SetDefault makes l the process default logger (slog.Default and the
// standard log package), so nothing reaches stderr unredacted. boot and the
// CLI commands call it as soon as the logger exists.
func SetDefault(l *slog.Logger) { slog.SetDefault(Redacting(l)) }

// Redacting returns l if its handler already redacts, else l behind the
// redaction handler.
func Redacting(l *slog.Logger) *slog.Logger {
	if _, ok := l.Handler().(*RedactHandler); ok {
		return l
	}
	return slog.New(NewRedactHandler(l.Handler()))
}

// Default is the process default logger, always behind the redaction handler.
// Outside this package code never calls slog.Default or the package-level
// slog.Info/Warn/Error/Debug (golangci forbidigo); it takes a logger, or the
// request's from Logger(ctx).
func Default() *slog.Logger { return Redacting(slog.Default()) }

// OrDefault returns l, or Default() when l is nil (for optional logger fields).
func OrDefault(l *slog.Logger) *slog.Logger {
	if l == nil {
		return Default()
	}
	return Redacting(l)
}

type loggerKey struct{}

// WithLogger stores the request logger in ctx (the requestid middleware does).
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, Redacting(l))
}

// Logger returns the request logger stored by WithLogger, else Default().
func Logger(ctx context.Context) *slog.Logger { return LoggerOr(ctx, nil) }

// LoggerOr returns the request logger stored by WithLogger, else fallback
// (behind the redaction handler), else Default().
func LoggerOr(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && l != nil {
			return l
		}
	}
	return OrDefault(fallback)
}

func levelName(v any) string {
	l, ok := v.(slog.Level)
	if !ok {
		return "info"
	}
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warn"
	case l >= slog.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

// contextHandler adds request_id, trace_id and span_id from the context.
type contextHandler struct{ next slog.Handler }

func (h *contextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil {
		if id := reqctx.RequestID(ctx); id != "" {
			r.AddAttrs(slog.String("request_id", id))
		}
		if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
			r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
		}
	}
	return h.next.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return &contextHandler{next: h.next.WithAttrs(as)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{next: h.next.WithGroup(name)}
}

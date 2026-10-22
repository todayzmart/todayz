package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// MetricPrefix starts every TodayZ metric name (05).
const MetricPrefix = "tz_"

// Options configure Setup.
type Options struct {
	Service string
	Env     string // local | test | staging | prod
	Version string

	// OTLPEndpoint is the OTLP HTTP base URL (for example http://127.0.0.1:4318).
	// Empty disables export: tracers and meters are no-ops.
	OTLPEndpoint string
	// OTLPHeaders is "k=v,k2=v2" (Grafana Cloud auth). Never logged.
	OTLPHeaders string

	// SentryDSN enables Sentry. It is ignored in local and test.
	SentryDSN string

	// HTTPClient is the outbound client exporters use (httpx.NewOutboundClient,
	// so the no-egress guard applies).
	HTTPClient *http.Client

	Log *slog.Logger
}

// Status says what Setup turned on (for the start-up log line; no values).
type Status struct {
	Traces  bool
	Metrics bool
	Sentry  bool
}

// Setup installs the global tracer and meter providers and the W3C propagator,
// and initialises Sentry when allowed. The returned shutdown flushes everything.
func Setup(ctx context.Context, o Options) (Status, func(context.Context) error, error) {
	var st Status
	var shutdowns []func(context.Context) error
	shutdown := func(ctx context.Context) error {
		var errs []error
		for i := len(shutdowns) - 1; i >= 0; i-- {
			errs = append(errs, shutdowns[i](ctx))
		}
		return errors.Join(errs...)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	if o.OTLPEndpoint != "" {
		res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
			attribute.String("service.name", "todayz-"+o.Service),
			attribute.String("service.version", o.Version),
			attribute.String("deployment.environment.name", o.Env),
		))
		if err != nil {
			return st, shutdown, err
		}
		headers := ParseHeaders(o.OTLPHeaders)
		base := strings.TrimRight(o.OTLPEndpoint, "/")

		topts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(base + "/v1/traces"), otlptracehttp.WithHeaders(headers)}
		mopts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpointURL(base + "/v1/metrics"), otlpmetrichttp.WithHeaders(headers)}
		if o.HTTPClient != nil {
			topts = append(topts, otlptracehttp.WithHTTPClient(o.HTTPClient))
			mopts = append(mopts, otlpmetrichttp.WithHTTPClient(o.HTTPClient))
		}

		texp, err := otlptracehttp.New(ctx, topts...)
		if err != nil {
			return st, shutdown, err
		}
		tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(texp), sdktrace.WithResource(res))
		otel.SetTracerProvider(tp)
		shutdowns = append(shutdowns, tp.Shutdown)
		st.Traces = true

		mexp, err := otlpmetrichttp.New(ctx, mopts...)
		if err != nil {
			return st, shutdown, err
		}
		mp := sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mexp, sdkmetric.WithInterval(15*time.Second))),
			sdkmetric.WithResource(res),
		)
		otel.SetMeterProvider(mp)
		shutdowns = append(shutdowns, mp.Shutdown)
		st.Metrics = true
	}

	if SentryAllowed(o.Env, o.SentryDSN) {
		opts := sentry.ClientOptions{
			Dsn:         o.SentryDSN,
			Environment: o.Env,
			Release:     o.Version,
			// Collect nothing about the user or the request automatically
			// (stricter than the old SendDefaultPII=false); scrubSentryEvent
			// is the second line.
			DataCollection: &sentry.DataCollection{
				UserInfo:    sentry.Set(false),
				HTTPBodies:  []sentry.BodyType{},
				Cookies:     &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
				QueryParams: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
				HTTPHeaders: &sentry.HeaderCollectionConfig{
					Request:  &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
					Response: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
				},
			},
			AttachStacktrace: true,
			BeforeSend:       scrubSentryEvent,
		}
		if o.HTTPClient != nil {
			opts.HTTPClient = o.HTTPClient
		}
		if err := sentry.Init(opts); err != nil {
			return st, shutdown, errors.New("telemetry: sentry init failed")
		}
		st.Sentry = true
		shutdowns = append(shutdowns, func(context.Context) error {
			sentry.Flush(2 * time.Second)
			return nil
		})
	}

	if o.Log != nil {
		o.Log.InfoContext(ctx, "telemetry ready",
			slog.Bool("traces", st.Traces), slog.Bool("metrics", st.Metrics), slog.Bool("sentry", st.Sentry))
	}
	return st, shutdown, nil
}

// SentryAllowed reports whether Sentry may start: a DSN is set and the
// environment is staging or prod (never local or test).
func SentryAllowed(env, dsn string) bool {
	return dsn != "" && (env == "staging" || env == "prod")
}

// ParseHeaders reads "k=v,k2=v2" (the OTEL_EXPORTER_OTLP_HEADERS format).
func ParseHeaders(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && k != "" {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// scrubSentryEvent drops request bodies, cookies, headers, query strings,
// user data, Tags and every context but the trace context (this sentry-go has
// no Extra; extra data travels in Contexts), and masks
// phone numbers, codes, tokens and URL secrets in the URL and messages.
func scrubSentryEvent(e *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if e == nil {
		return nil
	}
	e.User = sentry.User{ID: e.User.ID}
	e.Tags = nil
	if tr, ok := e.Contexts["trace"]; ok {
		e.Contexts = map[string]sentry.Context{"trace": tr}
	} else {
		e.Contexts = nil
	}
	if e.Request != nil {
		u := e.Request.URL
		if i := strings.IndexAny(u, "?#"); i >= 0 {
			u = u[:i]
		}
		e.Request.URL = RedactString(u)
		e.Request.Data = ""
		e.Request.Cookies = ""
		e.Request.Headers = nil
		e.Request.QueryString = ""
		e.Request.Env = nil
	}
	e.Message = RedactString(e.Message)
	for i := range e.Exception {
		e.Exception[i].Value = RedactString(e.Exception[i].Value)
	}
	for i := range e.Breadcrumbs {
		e.Breadcrumbs[i].Message = RedactString(e.Breadcrumbs[i].Message)
		e.Breadcrumbs[i].Data = nil
	}
	return e
}

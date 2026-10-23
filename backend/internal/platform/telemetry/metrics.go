package telemetry

import (
	"context"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// MeterName is the instrumentation scope of the platform metrics.
const MeterName = "github.com/todayz/backend/internal/platform/telemetry"

// ExponentialView makes every histogram an exponential (native) histogram
// (05 §2.1: no hand-picked buckets). Setup installs it.
func ExponentialView() sdkmetric.View {
	return sdkmetric.NewView(
		sdkmetric.Instrument{Kind: sdkmetric.InstrumentKindHistogram},
		sdkmetric.Stream{Aggregation: sdkmetric.AggregationBase2ExponentialHistogram{MaxSize: 160, MaxScale: 20}},
	)
}

// Metrics holds the platform instruments of W1-07: RED for HTTP, SSE hooks and
// the RUM histograms. Runtime, pool and queue metrics register with
// RegisterRuntime, RegisterPool and RegisterQueue. Job-level metrics belong to
// W1-11. Every attribute key is on AllowedLabels and every attribute value of
// an open-ended label goes through LabelValue.
type Metrics struct {
	service string

	requests metric.Int64Counter
	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
	respSize metric.Int64Histogram

	sseOpen       metric.Int64UpDownCounter
	ssePublished  metric.Int64Counter
	sseReconnects metric.Int64Counter
	sseLag        metric.Float64Histogram

	rumLCP, rumINP, rumTTFB, rumFCP metric.Float64Histogram
	rumCLS                          metric.Float64Histogram
	rumRouteMs                      metric.Float64Histogram
	rumErrors                       metric.Int64Counter
	rumLongTasks                    metric.Int64Counter

	eventsDropped metric.Int64Counter

	routes *routeCap
}

// NewMetrics creates the instruments on mp (the global provider when nil).
// service is "api" or "worker".
func NewMetrics(mp metric.MeterProvider, service string) (*Metrics, error) {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	m := mp.Meter(MeterName)
	x := &Metrics{service: LabelValue("service", service), routes: newRouteCap(60)}
	var err error
	chk := func(e error) {
		if err == nil {
			err = e
		}
	}
	var e error
	x.requests, e = m.Int64Counter("tz_http_server_requests_total", metric.WithDescription("HTTP requests by route template"))
	chk(e)
	x.duration, e = m.Float64Histogram("tz_http_server_duration_seconds", metric.WithUnit("s"), metric.WithDescription("HTTP request duration"))
	chk(e)
	x.active, e = m.Int64UpDownCounter("tz_http_server_active_requests", metric.WithDescription("Requests in flight"))
	chk(e)
	x.respSize, e = m.Int64Histogram("tz_http_server_response_size_bytes", metric.WithUnit("By"))
	chk(e)
	x.sseOpen, e = m.Int64UpDownCounter("tz_sse_open_connections", metric.WithDescription("Open SSE connections"))
	chk(e)
	x.ssePublished, e = m.Int64Counter("tz_sse_events_published_total")
	chk(e)
	x.sseReconnects, e = m.Int64Counter("tz_sse_reconnects_total")
	chk(e)
	x.sseLag, e = m.Float64Histogram("tz_sse_delivery_lag_seconds", metric.WithUnit("s"))
	chk(e)
	x.rumLCP, e = m.Float64Histogram("tz_rum_lcp_seconds", metric.WithUnit("s"))
	chk(e)
	x.rumINP, e = m.Float64Histogram("tz_rum_inp_seconds", metric.WithUnit("s"))
	chk(e)
	x.rumTTFB, e = m.Float64Histogram("tz_rum_ttfb_seconds", metric.WithUnit("s"))
	chk(e)
	x.rumFCP, e = m.Float64Histogram("tz_rum_fcp_seconds", metric.WithUnit("s"))
	chk(e)
	x.rumCLS, e = m.Float64Histogram("tz_rum_cls_score", metric.WithUnit("1"))
	chk(e)
	x.rumRouteMs, e = m.Float64Histogram("tz_rum_route_change_seconds", metric.WithUnit("s"))
	chk(e)
	x.rumErrors, e = m.Int64Counter("tz_rum_errors_total")
	chk(e)
	x.rumLongTasks, e = m.Int64Counter("tz_rum_long_tasks_total")
	chk(e)
	x.eventsDropped, e = m.Int64Counter("tz_events_dropped_total", metric.WithDescription("Product events dropped by POST /v1/events, by reason"))
	chk(e)
	return x, err
}

// RouteLabel normalises a route template for the `route` label: empty or
// "unmatched" stay as they are (unmatched paths share one series).
func RouteLabel(route string) string {
	if route == "" {
		return "unmatched"
	}
	return route
}

// AppFromOrigin maps a request Origin to the `app` label by host (customer,
// business, admin, site), so the API needs no per-app token. Unknown origins
// are "other".
func AppFromOrigin(origin string) string {
	o := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	o = strings.SplitN(o, ":", 2)[0]
	switch {
	case o == "":
		return "api"
	case strings.HasPrefix(o, "app."):
		return "customer"
	case strings.HasPrefix(o, "business."):
		return "business"
	case strings.HasPrefix(o, "admin."):
		return "admin"
	case o == "todayz.in" || o == "todayz.localhost" || strings.HasPrefix(o, "www."):
		return "site"
	}
	return Other
}

// RequestStart marks a request in flight and returns the finisher.
func (m *Metrics) RequestStart(ctx context.Context) func(route, method string, status int, code, app string, seconds float64, bytes int) {
	if m == nil {
		return func(string, string, int, string, string, float64, int) {}
	}
	m.active.Add(ctx, 1, metric.WithAttributes(attribute.String("service", m.service)))
	return func(route, method string, status int, code, app string, seconds float64, bytes int) {
		m.active.Add(ctx, -1, metric.WithAttributes(attribute.String("service", m.service)))
		attrs := metric.WithAttributes(
			attribute.String("service", m.service),
			attribute.String("route", RouteLabel(route)),
			attribute.String("method", LabelValue("method", method)),
			attribute.String("status_class", StatusClass(status)),
			attribute.String("code", CodeLabel(code)),
			attribute.String("app", LabelValue("app", app)),
		)
		m.requests.Add(ctx, 1, attrs)
		m.duration.Record(ctx, seconds, attrs)
		m.respSize.Record(ctx, int64(bytes), metric.WithAttributes(
			attribute.String("service", m.service), attribute.String("route", RouteLabel(route))))
	}
}

// SSE hooks: the notify domain calls them; nothing here knows events.

// SSEOpened and SSEClosed track `tz_sse_open_connections{app}`.
func (m *Metrics) SSEOpened(ctx context.Context, app string) { m.sseOpenDelta(ctx, app, 1) }

// EventDropped counts one product event dropped by POST /v1/events. reason is one of EventDropReasons.
func (m *Metrics) EventDropped(ctx context.Context, reason string) {
	if m == nil {
		return
	}
	m.eventsDropped.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", LabelValue("reason", reason))))
}

// SSEClosed is the counterpart of SSEOpened.
func (m *Metrics) SSEClosed(ctx context.Context, app string) { m.sseOpenDelta(ctx, app, -1) }

func (m *Metrics) sseOpenDelta(ctx context.Context, app string, d int64) {
	if m == nil {
		return
	}
	m.sseOpen.Add(ctx, d, metric.WithAttributes(attribute.String("app", LabelValue("app", app))))
}

// SSEPublished counts one event published to the stream (`channel` is a
// registered label value).
func (m *Metrics) SSEPublished(ctx context.Context, channel string) {
	if m == nil {
		return
	}
	m.ssePublished.Add(ctx, 1, metric.WithAttributes(attribute.String("channel", LabelValue("channel", channel))))
}

// SSEReconnect counts a client resuming with Last-Event-ID.
func (m *Metrics) SSEReconnect(ctx context.Context, app string) {
	if m == nil {
		return
	}
	m.sseReconnects.Add(ctx, 1, metric.WithAttributes(attribute.String("app", LabelValue("app", app))))
}

// SSEDeliveryLag records seconds between publish and write to the client.
func (m *Metrics) SSEDeliveryLag(ctx context.Context, app string, seconds float64) {
	if m == nil {
		return
	}
	m.sseLag.Record(ctx, seconds, metric.WithAttributes(attribute.String("app", LabelValue("app", app))))
}

// RUMSample is one validated browser sample (the /v1/rum handler builds it).
type RUMSample struct {
	App        string
	Route      string // a route template the client sent, already validated
	Platform   string
	Conn       string
	AppVersion string
	Metric     string // lcp, inp, cls, ttfb, fcp, route_change, long_task, error
	Value      float64
}

// RecordRUM turns a sample into the tz_rum_* series. The route and version
// are bounded here (cap per app, last five versions), the rest by LabelValue.
func (m *Metrics) RecordRUM(ctx context.Context, s RUMSample) {
	if m == nil {
		return
	}
	app := LabelValue("app", s.App)
	attrs := metric.WithAttributes(
		attribute.String("app", app),
		attribute.String("route", m.routes.admit(app, s.Route)),
		attribute.String("platform", LabelValue("platform", s.Platform)),
		attribute.String("conn", LabelValue("conn", s.Conn)),
		attribute.String("app_version", m.routes.version(app, s.AppVersion)),
	)
	switch s.Metric {
	case "lcp":
		m.rumLCP.Record(ctx, s.Value/1000, attrs)
	case "inp":
		m.rumINP.Record(ctx, s.Value/1000, attrs)
	case "ttfb":
		m.rumTTFB.Record(ctx, s.Value/1000, attrs)
	case "fcp":
		m.rumFCP.Record(ctx, s.Value/1000, attrs)
	case "cls":
		m.rumCLS.Record(ctx, s.Value, attrs)
	case "route_change":
		m.rumRouteMs.Record(ctx, s.Value/1000, attrs)
	case "long_task":
		m.rumLongTasks.Add(ctx, 1, attrs)
	case "error":
		m.rumErrors.Add(ctx, 1, attrs)
	}
}

// routeCap bounds the cardinality the browser could inject: at most max
// distinct routes per app (then "other") and the last five app versions.
type routeCap struct {
	mu       sync.Mutex
	max      int
	routes   map[string]map[string]struct{}
	versions map[string][]string
}

func newRouteCap(max int) *routeCap {
	return &routeCap{max: max, routes: map[string]map[string]struct{}{}, versions: map[string][]string{}}
}

func (c *routeCap) admit(app, route string) string {
	if route == "" {
		return "unknown"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	set := c.routes[app]
	if set == nil {
		set = map[string]struct{}{}
		c.routes[app] = set
	}
	if _, ok := set[route]; ok {
		return route
	}
	if len(set) >= c.max {
		return Other
	}
	set[route] = struct{}{}
	return route
}

// version keeps a recency list of five versions per app; a sixth evicts the
// oldest, and a sample of an evicted version reads as "other" until it is
// seen again (it then takes a slot).
func (c *routeCap) version(app, v string) string {
	if v == "" {
		return "unknown"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	list := c.versions[app]
	for i, x := range list {
		if x == v {
			// move to the end (most recent)
			list = append(append(list[:i:i], list[i+1:]...), v)
			c.versions[app] = list
			return v
		}
	}
	list = append(list, v)
	if len(list) > 5 {
		list = list[len(list)-5:]
	}
	c.versions[app] = list
	return v
}

// ReqNotes is a per-request scratchpad the access log reads after the
// handler returns: the problem writer notes the code, authn (W1-01) notes who
// the caller was. Inner middleware cannot pass values outward through the
// request context, hence the holder.
type ReqNotes struct {
	mu                          sync.Mutex
	code, userKind, role, store string
}

type notesKey struct{}

// WithReqNotes returns ctx carrying a fresh holder.
func WithReqNotes(ctx context.Context) (context.Context, *ReqNotes) {
	h := &ReqNotes{}
	return context.WithValue(ctx, notesKey{}, h), h
}

func notesFrom(ctx context.Context) *ReqNotes {
	h, _ := ctx.Value(notesKey{}).(*ReqNotes)
	return h
}

// NoteErrCode records the problem code of the response being written.
func NoteErrCode(ctx context.Context, code string) {
	if h := notesFrom(ctx); h != nil {
		h.mu.Lock()
		h.code = code
		h.mu.Unlock()
	}
}

// NotePrincipal records the caller for the access log (user_kind, role,
// store_id; never the user id). authz.Authorize calls it once a check has
// passed; before that the access log falls back to reqctx's kind.
func NotePrincipal(ctx context.Context, userKind, role, storeID string) {
	if h := notesFrom(ctx); h != nil {
		h.mu.Lock()
		h.userKind, h.role, h.store = userKind, role, storeID
		h.mu.Unlock()
	}
}

// Code returns the noted problem code, or "".
func (h *ReqNotes) Code() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.code
}

// Principal returns the noted user kind, role and store id.
func (h *ReqNotes) Principal() (userKind, role, storeID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.userKind, h.role, h.store
}

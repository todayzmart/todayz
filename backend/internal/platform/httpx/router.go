// Package httpx builds the API's HTTP router (chi) with the 02 §2.4
// middleware pipeline, and the outbound HTTP client every provider uses.
package httpx

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/platform/httpx/mw"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/httpx/route"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Options configure the router.
type Options struct {
	Log         *slog.Logger
	CORSOrigins []string
	ClientIP    mw.ClientIPOptions
	Idempotency idempotency.Options
	// Validator is the OpenAPI request validator (W0-04); nil passes all.
	Validator mw.Validator
}

// Pipeline returns the global middleware in 02 §2.4 order (authz is per
// route; mw/doc.go lists the slots). routes resolves each request's
// route.Policy.
func Pipeline(o Options, routes *route.Table) []func(http.Handler) http.Handler {
	o.Log = telemetry.OrDefault(o.Log)
	return []func(http.Handler) http.Handler{
		mw.RequestID(o.Log),           // 1
		mw.SecHeaders(),               // 2
		mw.Recover(o.Log),             // 3
		mw.OTel(),                     // 4  W1-07
		mw.ClientIP(o.ClientIP),       // 5
		mw.AccessLog(o.Log),           // 6  W1-07 extends
		mw.CORS(o.CORSOrigins),        // 7
		mw.RoutePolicy(routes),        // 8
		mw.BodyLimit(),                // 9
		mw.Authn(),                    // 10 W1-01
		mw.RateLimit(),                // 11 W1-01
		mw.Idempotency(o.Idempotency), // 12
		mw.Validate(o.Validator),      // 13 W0-04
	}
}

// WriteTimeout bounds one response; idempotency.DefaultLease must outlive it.
const WriteTimeout = 60 * time.Second

// NewRouter returns a chi router with the pipeline mounted and problem-JSON
// 404 and 405 answers.
func NewRouter(o Options) *chi.Mux {
	r := chi.NewRouter()
	r.Use(Pipeline(o, route.NewTable(r))...)
	r.NotFound(problem.NotFound)
	r.MethodNotAllowed(problem.MethodNotAllowed)
	return r
}

// NewServer returns an http.Server with conservative timeouts.
func NewServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      WriteTimeout,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}

// Package httpx builds the API's HTTP router (chi) with the 02 §2.4
// middleware pipeline, and the outbound HTTP client every provider uses.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	genproblems "github.com/todayz/backend/gen/problems"
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
	// PublicCORSOrigins may call /v1/public/* only, without credentials (D-102).
	PublicCORSOrigins []string
	ClientIP          mw.ClientIPOptions
	Idempotency       idempotency.Options
	// Validator is the OpenAPI request validator (W0-04); nil passes all.
	// When it also has Has(method, pattern string) bool, Router.Check refuses
	// a mounted route the contract does not know.
	Validator mw.Validator
	// Policies answers for routes that did not declare a route.Policy with
	// route.Handle (W0-04: the contract's x-idempotency and x-upload); nil
	// leaves them at the zero Policy.
	Policies route.Source
	// Metrics records the RED metrics (W1-07); nil records none.
	Metrics *telemetry.Metrics
	// TracerProvider and Propagator override the global ones (tests use an
	// in-memory exporter); nil uses the globals.
	TracerProvider trace.TracerProvider
	Propagator     propagation.TextMapPropagator
	// Authenticator verifies bearer tokens (slot 11, W1-01); nil makes every
	// request anonymous and any Authorization header 401.
	Authenticator mw.Authenticator
	// Limiter is slot 12's per-principal / per-IP limiter (W1-01); nil limits
	// nothing. Pass a nil interface, not a nil *ratelimit.Limiter.
	Limiter mw.Limiter
}

// Pipeline returns the global middleware, slots 1–12 of 02 §2.4, which run
// before chi picks the route (mw/doc.go lists the slots). routes resolves
// each request's route.Policy.
func Pipeline(o Options, routes *route.Table) []func(http.Handler) http.Handler {
	o.Log = telemetry.OrDefault(o.Log)
	return []func(http.Handler) http.Handler{
		mw.RequestID(o.Log),                         // 1
		mw.SecHeaders(),                             // 2
		mw.Recover(o.Log),                           // 3
		mw.OTel(o.TracerProvider, o.Propagator),     // 4  W1-07
		mw.ClientIP(o.ClientIP),                     // 5
		mw.AccessLog(o.Log, o.Metrics),              // 6  W1-07
		mw.CORS(o.CORSOrigins, o.PublicCORSOrigins), // 7
		mw.PathGuard(),                              // 8  W0-04
		mw.RoutePolicy(routes),                      // 9
		mw.BodyLimit(),                              // 10
		mw.Authn(o.Authenticator),                   // 11 W1-01
		mw.RateLimit(o.Limiter),                     // 12 W1-01
	}
}

// Inline returns slots 13–14, which every registered route gets after chi
// has picked it (D-100): the contract validator, then idempotency, so a
// refused request is never stored under its key.
func Inline(o Options) []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{
		mw.Validate(o.Validator),      // 13 W0-04
		mw.Idempotency(o.Idempotency), // 14
	}
}

// WriteTimeout bounds one response; idempotency.DefaultLease must outlive it,
// and the API's SHUTDOWN_DRAIN must exceed it (lifecycle, D-98).
const WriteTimeout = 60 * time.Second

// Router is the API router. Routes are registered on it as on any chi.Router
// (Get, Method, With…); each gets the inline slots. It must not be given a
// sub-router (Route or Mount): the inline validator reads chi's route
// pattern, which a sub-router hides, and Check refuses one.
type Router struct {
	chi.Router
	mux *chi.Mux
	tbl *route.Table
	v   mw.Validator
}

// NewRouter returns the router with the pipeline mounted and problem-JSON
// 404 and 405 (with Allow) answers.
func NewRouter(o Options) *Router {
	mux := chi.NewRouter()
	tbl := route.NewTableWith(mux, o.Policies)
	mux.Use(Pipeline(o, tbl)...)
	mux.NotFound(problem.NotFound)
	mux.MethodNotAllowed(methodNotAllowed(mux))
	return &Router{Router: mux.With(Inline(o)...), mux: mux, tbl: tbl, v: o.Validator}
}

// ServeHTTP serves through the whole pipeline.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) { r.mux.ServeHTTP(w, req) }

// Check runs after every route is registered and returns every reason the
// API must not start: a route whose code policy is weaker than the
// contract's, a sub-router, or a mounted route the contract does not know.
func (r *Router) Check() error {
	var errs []error
	if err := r.tbl.Check(); err != nil {
		errs = append(errs, err)
	}
	for _, rt := range r.mux.Routes() {
		if rt.SubRoutes != nil {
			errs = append(errs, fmt.Errorf("%s: a sub-router (Route or Mount) is not allowed; register full patterns on the router (D-100)", rt.Pattern))
		}
		if len(rt.Pattern) > 1 && strings.HasSuffix(rt.Pattern, "/") {
			// chi serves `/v1/x/` and `/v1/x` as different routes and the
			// contract never has the former (apigen refuses it), so a mounted
			// trailing slash could only be a route the validator cannot find
			// (D-100 round 2, Low 5).
			errs = append(errs, fmt.Errorf("%s: a mounted pattern may not end in `/` (D-100)", rt.Pattern))
		}
	}
	if h, ok := r.v.(interface {
		Has(method, pattern string) bool
	}); ok {
		_ = chi.Walk(r.mux, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			if !h.Has(method, pattern) {
				errs = append(errs, fmt.Errorf("%s %s is mounted but not in the contract", method, pattern))
			}
			return nil
		})
	}
	return errors.Join(errs...)
}

// methodNotAllowed answers 405 with Allow naming the mounted methods (RFC
// 9110 §15.5.6); chi's own 405 handler cannot once it is replaced.
func methodNotAllowed(mux *chi.Mux) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.RawPath
		if path == "" {
			path = r.URL.Path
		}
		var allow []string
		for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			if mux.Find(chi.NewRouteContext(), m, path) != "" {
				allow = append(allow, m)
			}
		}
		p := problem.New(genproblems.MethodNotAllowed, "")
		if len(allow) > 0 {
			p = p.WithHeader("Allow", strings.Join(allow, ", "))
		}
		problem.Write(w, r, p)
	}
}

// NewServer returns an http.Server with conservative timeouts.
//
// Request contexts derive from BaseContext, a background context: never from
// the signal context, so SIGTERM does not cancel a request in flight; the
// drain (srv.Shutdown, lifecycle.Server) lets it finish (02 §2.8, D-98).
func NewServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		BaseContext:       func(net.Listener) context.Context { return context.Background() },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      WriteTimeout,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}

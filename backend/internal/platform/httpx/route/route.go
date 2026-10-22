// Package route carries per-route options that global middleware needs
// before chi has picked the route: whether the route takes an
// Idempotency-Key, and whether it accepts an upload-sized body.
//
// A route declares its options where it is registered:
//
//	r.Method(http.MethodPost, "/v1/me/orders",
//		route.Handle(route.Policy{Idempotency: route.IdempotencyRequired}, h))
//
// route.Handle must be the outermost wrapper of the endpoint (r.With(...)
// middlewares are fine: they wrap it in a chi.ChainHandler, which Table looks
// through). A route without route.Handle has the zero Policy: no idempotency
// (the Idempotency-Key header is ignored) and the JSON body limit.
package route

import (
	"context"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
)

// Idempotency is a route's Idempotency-Key rule (04 §1.6).
type Idempotency int

// Idempotency rules.
const (
	// IdempotencyNone ignores the header: the request runs every time.
	IdempotencyNone Idempotency = iota
	// IdempotencyOptional honours the header when the client sends one.
	IdempotencyOptional
	// IdempotencyRequired refuses an unsafe request without the header (400
	// idempotency_key_required).
	IdempotencyRequired
)

// Policy is one route's options.
type Policy struct {
	Idempotency Idempotency
	// Upload lets the body reach the upload limit (10 MB) instead of the
	// JSON limit (1 MB), whatever the Content-Type says.
	Upload bool
}

// Handle attaches p to h.
func Handle(p Policy, h http.Handler) http.Handler { return &handler{p: p, h: h} }

// HandleFunc is Handle for a function.
func HandleFunc(p Policy, h http.HandlerFunc) http.Handler { return Handle(p, h) }

type handler struct {
	p Policy
	h http.Handler
}

func (x *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { x.h.ServeHTTP(w, r) }

// policyOf finds the Policy on a registered endpoint.
func policyOf(h http.Handler) (Policy, bool) {
	for range 8 {
		switch x := h.(type) {
		case *handler:
			return x.p, true
		case *chi.ChainHandler:
			h = x.Endpoint
		default:
			return Policy{}, false
		}
	}
	return Policy{}, false
}

type ctxKey struct{}

// WithPolicy stores p in ctx.
func WithPolicy(ctx context.Context, p Policy) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the request's Policy (the zero Policy when none).
func FromContext(ctx context.Context) Policy {
	p, _ := ctx.Value(ctxKey{}).(Policy)
	return p
}

// Table maps method + route pattern to Policy for one router. It is built on
// the first request, after every route is registered.
type Table struct {
	mux  *chi.Mux
	once sync.Once
	m    map[string]Policy
}

// NewTable returns the table for mux.
func NewTable(mux *chi.Mux) *Table { return &Table{mux: mux} }

func (t *Table) build() {
	t.m = map[string]Policy{}
	_ = chi.Walk(t.mux, func(method, pattern string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		if p, ok := policyOf(h); ok {
			t.m[method+" "+pattern] = p
		}
		return nil
	})
}

// Lookup returns the Policy of the route that serves method and path.
func (t *Table) Lookup(method, path string) Policy {
	t.once.Do(t.build)
	pattern := t.mux.Find(chi.NewRouteContext(), method, path)
	if pattern == "" {
		return Policy{}
	}
	return t.m[method+" "+pattern]
}

// Middleware stores the request's Policy in its context.
func (t *Table) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.RawPath
		if path == "" {
			path = r.URL.Path
		}
		next.ServeHTTP(w, r.WithContext(WithPolicy(r.Context(), t.Lookup(r.Method, path))))
	})
}

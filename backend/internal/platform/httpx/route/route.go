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
// through). The contract is the other source (the operation's x-idempotency
// and x-upload, W0-04): a route's effective Policy is the two merged, the
// stronger idempotency rule and Upload when either says so (D-100). Code that
// declares a weaker Policy than the contract is a conflict Table.Check
// reports, so the API refuses to start. A route neither declares has the
// zero Policy: no idempotency (the Idempotency-Key header is ignored) and the
// JSON body limit.
package route

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
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

func (i Idempotency) String() string {
	switch i {
	case IdempotencyOptional:
		return "optional"
	case IdempotencyRequired:
		return "required"
	default:
		return "none"
	}
}

// Policy is one route's options.
type Policy struct {
	Idempotency Idempotency
	// Upload lets the body reach the upload limit (10 MB) instead of the
	// JSON limit (1 MB), whatever the Content-Type says.
	Upload bool
}

// Merge returns the stronger of p and q: the higher idempotency rule, and
// Upload when either has it.
func (p Policy) Merge(q Policy) Policy {
	out := p
	if q.Idempotency > out.Idempotency {
		out.Idempotency = q.Idempotency
	}
	out.Upload = out.Upload || q.Upload
	return out
}

// weakerThan reports whether p relaxes something q requires.
func (p Policy) weakerThan(q Policy) bool {
	return p.Idempotency < q.Idempotency || (q.Upload && !p.Upload)
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

// Source supplies the Policy the contract declares for a route (W0-04: the
// operation's x-idempotency and x-upload). ok is false when the operation
// declares neither.
type Source interface {
	Policy(method, pattern string) (Policy, bool)
}

// Table maps method + route pattern to the merged Policy for one router. It
// is built on the first request (or Check), after every route is registered.
type Table struct {
	mux       *chi.Mux
	src       Source
	once      sync.Once
	m         map[string]Policy
	conflicts []error
}

// NewTable returns the table for mux.
func NewTable(mux *chi.Mux) *Table { return &Table{mux: mux} }

// NewTableWith returns the table for mux with src (may be nil) supplying the
// contract's Policy, merged with the one declared in code.
func NewTableWith(mux *chi.Mux, src Source) *Table { return &Table{mux: mux, src: src} }

func (t *Table) build() {
	t.m = map[string]Policy{}
	_ = chi.Walk(t.mux, func(method, pattern string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		code, inCode := policyOf(h)
		var spec Policy
		inSpec := false
		if t.src != nil {
			spec, inSpec = t.src.Policy(method, pattern)
		}
		// Upload lifts the body limit and skips body validation, so only the
		// contract may grant it: code claiming it on an operation the contract
		// does not mark x-upload is a conflict (D-100 round 2, Low 3). Without a
		// source (NewTable) there is no contract to check against.
		if t.src != nil && code.Upload && !spec.Upload {
			t.conflicts = append(t.conflicts, fmt.Errorf("%s %s: code declares upload but the contract does not mark it x-upload; only the contract grants an upload (D-100)", method, pattern))
		}
		switch {
		case inCode && inSpec:
			if code.weakerThan(spec) {
				t.conflicts = append(t.conflicts, fmt.Errorf("%s %s: code declares idempotency %s, upload %t but the contract says idempotency %s, upload %t; code may only strengthen the contract (D-100)",
					method, pattern, code.Idempotency, code.Upload, spec.Idempotency, spec.Upload))
			}
			t.m[method+" "+pattern] = code.Merge(spec)
		case inCode:
			t.m[method+" "+pattern] = code
		case inSpec:
			t.m[method+" "+pattern] = spec
		}
		return nil
	})
	sort.Slice(t.conflicts, func(i, j int) bool { return t.conflicts[i].Error() < t.conflicts[j].Error() })
}

// Check builds the table and returns every route whose code Policy is weaker
// than the contract's.
func (t *Table) Check() error {
	t.once.Do(t.build)
	return errors.Join(t.conflicts...)
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

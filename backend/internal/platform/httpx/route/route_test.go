package route

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func ok(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

// TestTableFindsPolicies: policies are found on top-level routes, through
// r.With chains, in r.Route and r.Mount sub-routers, and with URL params;
// any other route has the zero Policy.
func TestTableFindsPolicies(t *testing.T) {
	r := chi.NewRouter()
	tbl := NewTable(r)
	var seen Policy
	r.Use(tbl.Middleware, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seen = FromContext(req.Context())
			next.ServeHTTP(w, req)
		})
	})
	req := Policy{Idempotency: IdempotencyRequired}
	up := Policy{Upload: true}
	r.Method(http.MethodPost, "/v1/orders", HandleFunc(req, ok))
	r.With(func(h http.Handler) http.Handler { return h }).Method(http.MethodPost, "/v1/with", HandleFunc(up, ok))
	r.Route("/v1/me", func(r chi.Router) {
		r.Method(http.MethodPost, "/orders/{id}/cancel", HandleFunc(Policy{Idempotency: IdempotencyOptional}, ok))
	})
	sub := chi.NewRouter()
	sub.Method(http.MethodPut, "/photo", HandleFunc(up, ok))
	r.Mount("/v1/shops/{shopID}", sub)
	r.Post("/v1/plain", ok)

	for _, c := range []struct {
		method, path string
		want         Policy
	}{
		{http.MethodPost, "/v1/orders", req},
		{http.MethodGet, "/v1/orders", Policy{}}, // another method: 405, no policy
		{http.MethodPost, "/v1/with", up},
		{http.MethodPost, "/v1/me/orders/01J/cancel", Policy{Idempotency: IdempotencyOptional}},
		{http.MethodPut, "/v1/shops/abc/photo", up},
		{http.MethodPost, "/v1/plain", Policy{}},
		{http.MethodPost, "/v1/nope", Policy{}},
	} {
		seen = Policy{Idempotency: -1}
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(c.method, c.path, nil))
		require.Equal(t, c.want, seen, c.method+" "+c.path)
		require.Equal(t, c.want, tbl.Lookup(c.method, c.path), c.method+" "+c.path)
	}
}

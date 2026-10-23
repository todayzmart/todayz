package main

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/gen/api/spec"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/httpx/openapi"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/testclock"
)

// localRouter builds the real router in the environment that mounts the
// most (local), with the bundled contract.
func localRouter(t *testing.T) (*httpx.Router, *openapi.Validator) {
	t.Helper()
	v, err := openapi.New(spec.Bundle)
	require.NoError(t, err)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tc := testclock.New(clock.NewOffset(nil), nopStore{}, log)
	d := deps{Config: &config.Config{Env: config.EnvLocal, APIAddr: config.DefaultLocalAPIAddr}, Log: log, Clock: clock.Real{}, Ready: &health.Readiness{}, TestClock: tc}
	r, err := buildRouter(d, idempotency.NewMemoryStore(), v)
	require.NoError(t, err, "buildRouter refuses a route the contract does not know, a sub-router, or a code policy weaker than the contract's (D-100)")
	return r, v
}

// TestRoutesAreInTheContract (W0-04, D-100): every route the API mounts is
// an operation in the bundled contract, method and chi pattern alike. The
// validator fails closed (500) on a mounted route it cannot find, and
// buildRouter refuses to start with one; this test says which.
func TestRoutesAreInTheContract(t *testing.T) {
	r, v := localRouter(t)
	var missing []string
	n := 0
	require.NoError(t, chi.Walk(r, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		n++
		if !v.Has(method, pattern) {
			missing = append(missing, method+" "+pattern)
		}
		return nil
	}))
	require.NotZero(t, n)
	require.Empty(t, missing, "routes the contract does not know: add the operation to backend/api/paths/<domain>.yaml and run make gen")
}

// unmountedOperations lists contract operations whose domain has not landed
// yet (D-100): they may stay unmounted. Remove an entry when its domain
// mounts the route; the test fails if an entry is in fact mounted.
var unmountedOperations = map[string]bool{
	// W2-02 orders: contract first; removed when routes_orders.go mounts them.
	"POST /v1/me/checkout/quote":                                                 true,
	"GET /v1/me/orders":                                                          true,
	"POST /v1/me/orders":                                                         true,
	"GET /v1/me/orders/{orderId}":                                                true,
	"POST /v1/me/orders/{orderId}/cancel":                                        true,
	"POST /v1/me/orders/{orderId}/confirm-partial":                               true,
	"POST /v1/me/orders/{orderId}/extend":                                        true,
	"POST /v1/me/orders/{orderId}/new-code":                                      true,
	"GET /v1/biz/businesses/{bid}/stores/{sid}/orders":                           true,
	"GET /v1/biz/businesses/{bid}/stores/{sid}/orders/{orderId}":                 true,
	"POST /v1/biz/businesses/{bid}/stores/{sid}/orders/{orderId}/accept":         true,
	"POST /v1/biz/businesses/{bid}/stores/{sid}/orders/{orderId}/reject":         true,
	"POST /v1/biz/businesses/{bid}/stores/{sid}/orders/{orderId}/ready":          true,
	"POST /v1/biz/businesses/{bid}/stores/{sid}/orders/{orderId}/dispatch":       true,
	"POST /v1/biz/businesses/{bid}/stores/{sid}/orders/{orderId}/fail-delivery":  true,
	"POST /v1/biz/businesses/{bid}/stores/{sid}/orders/{orderId}/confirm-return": true,
	"POST /v1/biz/businesses/{bid}/stores/{sid}/orders/{orderId}/cancel":         true,
	"POST /v1/biz/businesses/{bid}/stores/{sid}/orders/{orderId}/verify-code":    true,
}

// TestContractOperationsAreMounted (D-100): the reverse of
// TestRoutesAreInTheContract. Every operation in the bundled contract is a
// mounted route, unless unmountedOperations lists it.
func TestContractOperationsAreMounted(t *testing.T) {
	r, v := localRouter(t)
	mounted := map[string]bool{}
	require.NoError(t, chi.Walk(r, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		mounted[method+" "+pattern] = true
		return nil
	}))
	var missing, stale []string
	for _, op := range v.Operations() {
		switch {
		case mounted[op] && unmountedOperations[op]:
			stale = append(stale, op)
		case !mounted[op] && !unmountedOperations[op]:
			missing = append(missing, op)
		}
	}
	require.Empty(t, missing, "contract operations without a route: mount them, or list them in unmountedOperations until their domain lands")
	require.Empty(t, stale, "mounted now: remove from unmountedOperations")
}

// TestBuildRouterRefusesRouteOutsideContract (D-100): a mounted route the
// contract does not know fails start-up, not just this test file.
func TestBuildRouterRefusesRouteOutsideContract(t *testing.T) {
	v, err := openapi.New(spec.Bundle)
	require.NoError(t, err)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := deps{Config: &config.Config{Env: config.EnvLocal, APIAddr: config.DefaultLocalAPIAddr}, Log: log, Clock: clock.Real{}, Ready: &health.Readiness{}}
	r := httpx.NewRouter(httpx.Options{Log: log, Validator: v, Policies: v})
	routes(r, d)
	r.Get("/v1/stray", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	err = r.Check()
	require.Error(t, err)
	require.Contains(t, err.Error(), "GET /v1/stray")
}

// reDirectStrict matches a direct use of oapi-codegen's server constructors.
// Every domain registers through its generated Register, which applies
// openapi.StrictOptions (problem+json on both error paths, D-100 M2).
var reDirectStrict = regexp.MustCompile(`\b(NewStrictHandler|NewStrictHandlerWithOptions|HandlerWithOptions|HandlerFromMux|HandlerFromMuxWithBaseURL)\(`)

// TestDomainsRegisterThroughGeneratedRegister (D-100 M2) scans the source:
// only gen/ may call oapi-codegen's constructors. internal/devtools holds
// apigen's template that writes the generated Register, so it is the one
// non-generated place the names appear by design.
func TestDomainsRegisterThroughGeneratedRegister(t *testing.T) {
	require.True(t, reDirectStrict.MatchString("h := NewStrictHandlerWithOptions(ssi, nil, opts)"), "the matcher works")
	require.True(t, reDirectStrict.MatchString("HandlerFromMux(si, r)"))
	require.False(t, reDirectStrict.MatchString("ordersapi.Register(r, impl)"))

	var offenders []string
	for _, root := range []string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")} {
		require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "gen" || d.Name() == "testdata" || d.Name() == "devtools" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if reDirectStrict.Match(src) {
				offenders = append(offenders, path)
			}
			return nil
		}))
	}
	require.Empty(t, offenders, "call the domain's generated Register(r, impl) instead; it applies openapi.StrictOptions")
}

// contractValidator loads the bundled contract for tests of the real router.
func contractValidator(t *testing.T) *openapi.Validator {
	t.Helper()
	v, err := openapi.New(spec.Bundle)
	require.NoError(t, err)
	return v
}

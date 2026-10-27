package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/gen/api/spec"
	"github.com/todayz/backend/internal/testkit"
)

// schemathesisEnv is what make check sets for the Schemathesis tests: the pinned
// binary in .tools/schemathesis and e2e/contract. Without them a plain `go test`
// skips, so the venv is never a requirement of the quick loop.
func schemathesisEnv(t *testing.T) (st, dir string) {
	t.Helper()
	st, dir = os.Getenv("TODAYZ_SCHEMATHESIS"), os.Getenv("TODAYZ_CONTRACT_DIR")
	if st == "" || dir == "" {
		t.Skip("TODAYZ_SCHEMATHESIS and TODAYZ_CONTRACT_DIR unset (make check sets them)")
	}
	return st, dir
}

// runSchemathesis runs e2e/contract/run.sh smoke for a spec file against base and
// returns the combined output and the exit error (nil on success).
func runSchemathesis(t *testing.T, ctx context.Context, st, dir, specPath, base string) (string, error) {
	t.Helper()
	problems, err := filepath.Abs(filepath.Join("..", "..", "api", "problems"))
	require.NoError(t, err)
	cmd := exec.CommandContext(ctx, filepath.Join(dir, "run.sh"), "smoke", specPath, base) // #nosec G204 -- paths from make check
	cmd.Env = append(os.Environ(),
		"TODAYZ_SCHEMATHESIS="+st,
		"TODAYZ_CONTRACT_OUT="+t.TempDir(),
		"TODAYZ_PROBLEMS_DIR="+problems,
	)
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr
}

// TestContractSmoke runs Schemathesis' examples and coverage phases
// (e2e/contract/run.sh smoke) against an in-process api on a fresh database:
// every operation with its contract examples and the schema-derived positive and
// negative cases, every error a registered problem (08 §3.8, SC-PL-02). The api
// alone answers /readyz, so no worker is started.
func TestContractSmoke(t *testing.T) {
	st, dir := schemathesisEnv(t)
	db := testkit.NewDB(t)
	e := localEnv(db)
	addr := freeAddr(t)
	e["API_ADDR"] = addr
	e["LOG_LEVEL"] = "warn"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var apiOut syncBuffer
	apiDone := make(chan int, 1)
	go func() { apiDone <- run(ctx, []string{"api"}, env(e), &apiOut, &apiOut) }()
	waitOK(t, "http://"+addr+"/readyz")

	// The binary's own bundle, so the run tests exactly what the server validates with.
	specPath := filepath.Join(t.TempDir(), "openapi.yaml")
	require.NoError(t, os.WriteFile(specPath, spec.Bundle, 0o600))
	report, runErr := runSchemathesis(t, ctx, st, dir, specPath, "http://"+addr)
	require.NoError(t, runErr, "schemathesis smoke failed:\n%s", report)
	require.Contains(t, report, "todayz hooks: todayz_problem check on", "the hooks did not load")
	require.Contains(t, report, "No issues found")

	cancel()
	require.Equal(t, 0, <-apiDone, apiOut.String())
	all := report + apiOut.String()
	for _, k := range []string{"PII_HASH_KEY", "DEVICE_SECRET_ENC_KEY", "JWT_SIGNING_KEYS", "PII_ENC_KEY"} {
		require.NotContains(t, all, e[k], "%s reached the output", k)
	}
}

// fixtureSpec is one operation whose only error response is a Problem, so the
// only check that can fail on a 409 is todayz_problem.
const fixtureSpec = `openapi: 3.1.0
info: {title: fixture, version: "0"}
paths:
  /v1/test/fixture:
    get:
      operationId: fixture
      parameters:
        - name: q
          in: query
          required: true
          schema: {type: string}
          example: hello
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: object}
              example: {}
        default:
          description: error
          content:
            application/problem+json:
              schema: {type: object}
`

// TestContractProblemCheckFires is the failing fixture for SC-PL-02: a server
// answering a problem whose code is not in api/problems fails the run, and the
// same answer with a registered code passes.
func TestContractProblemCheckFires(t *testing.T) {
	st, dir := schemathesisEnv(t)
	specPath := filepath.Join(t.TempDir(), "openapi.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(fixtureSpec), 0o600))

	// A server answering every GET with a 409 problem carrying code, and other
	// methods with the 405 the coverage phase expects.
	answer := func(code string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/problem+json")
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				w.WriteHeader(http.StatusMethodNotAllowed)
				_, _ = w.Write([]byte(`{"type":"https://todayz.in/errors/method_not_allowed","title":"x","status":405,"code":"method_not_allowed"}`))
				return
			}
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"type":"https://todayz.in/errors/` + code + `","title":"x","status":409,"code":"` + code + `"}`))
		}
	}

	t.Run("unregistered code fails", func(t *testing.T) {
		srv := httptest.NewServer(answer("stock_gone"))
		defer srv.Close()
		report, runErr := runSchemathesis(t, context.Background(), st, dir, specPath, srv.URL)
		require.Error(t, runErr, "an unregistered code passed:\n%s", report)
		require.Contains(t, report, "todayz_problem")
		require.Contains(t, report, "code 'stock_gone' is not registered in api/problems")
	})

	t.Run("registered code passes", func(t *testing.T) {
		srv := httptest.NewServer(answer("conflict"))
		defer srv.Close()
		report, runErr := runSchemathesis(t, context.Background(), st, dir, specPath, srv.URL)
		require.NoError(t, runErr, "a registered code failed:\n%s", report)
		require.Contains(t, report, "No issues found")
	})
}

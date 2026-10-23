package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// realAPI is backend/api, whose root.yaml, common/, problems/ and
// schemas/common.yaml every fixture is joined with.
const realAPI = "../../../api"

// fixtureAPI assembles testdata/<name> into a temporary api directory and
// returns it with the root document path apigen root should write.
func fixtureAPI(t *testing.T, name string) (api, out string) {
	t.Helper()
	dir := t.TempDir()
	api = filepath.Join(dir, "api")
	require.NoError(t, os.MkdirAll(filepath.Join(api, "schemas"), 0o750))
	copyFile(t, filepath.Join(realAPI, "root.yaml"), filepath.Join(api, "root.yaml"))
	copyFile(t, filepath.Join(realAPI, "schemas", "common.yaml"), filepath.Join(api, "schemas", "common.yaml"))
	for _, d := range []string{"common", "problems"} {
		require.NoError(t, os.CopyFS(filepath.Join(api, d), os.DirFS(filepath.Join(realAPI, d))))
	}
	// Every fixture starts from valid-orders' schemas; its own files overlay them.
	require.NoError(t, os.CopyFS(filepath.Join(api, "schemas"), os.DirFS(filepath.Join("testdata", "valid-orders", "schemas"))))
	fixture := filepath.Join("testdata", name)
	require.NoError(t, os.CopyFS(filepath.Join(api, "paths"), os.DirFS(filepath.Join(fixture, "paths"))))
	if _, err := os.Stat(filepath.Join(fixture, "schemas")); err == nil {
		overlay(t, filepath.Join(fixture, "schemas"), filepath.Join(api, "schemas"))
	}
	return api, filepath.Join(dir, "gen", "root.yaml")
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src) // #nosec G304 -- test fixtures
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, b, 0o600))
}

// overlay copies every file of src over dst (os.CopyFS refuses to overwrite).
func overlay(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	require.NoError(t, err)
	for _, e := range entries {
		copyFile(t, filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
	}
}

// runFixture is `apigen root` then `apigen check` on the root document
// (kin-openapi follows the file references, so no Redocly is needed).
func runFixture(api, out string, expect int) (string, error) {
	if err := runRoot(api, out); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err := runCheck(api, out, expect, &buf)
	return buf.String(), err
}

func TestValidFixture(t *testing.T) {
	api, out := fixtureAPI(t, "valid-orders")
	report, err := runFixture(api, out, 3)
	require.NoError(t, err)
	require.Equal(t, "apigen check: 3 method+path pairs under /v1 (me 3)\n", report)

	// The joined root carries every domain path, the common components and a tag per domain.
	root, err := os.ReadFile(out) // #nosec G304 -- the temp dir
	require.NoError(t, err)
	for _, want := range []string{
		"DO NOT EDIT",
		"/v1/me/orders/{oid}:",
		"$ref: ../api/paths/orders.yaml#/~1v1~1me~1orders~1{oid}",
		"OrderRequest:",
		"$ref: ../api/schemas/orders.yaml#/OrderRequest",
		"IdempotencyKey:",
		"name: orders",
	} {
		require.Contains(t, string(root), want)
	}
}

// TestValidVariants are shapes the lints must accept: the business list at
// the root of its namespace (D-100 M1) and `x-idempotency: none` without the
// key parameter (D-100 M-1).
func TestValidVariants(t *testing.T) {
	for fixture, want := range map[string]string{
		"biz-businesses-exact": "apigen check: 1 method+path pairs under /v1 (biz 1)\n",
		"idempotency-none":     "apigen check: 1 method+path pairs under /v1 (me 1)\n",
	} {
		t.Run(fixture, func(t *testing.T) {
			api, out := fixtureAPI(t, fixture)
			report, err := runFixture(api, out, 0)
			require.NoError(t, err)
			require.Equal(t, want, report)
		})
	}
}

func TestExpectCount(t *testing.T) {
	api, out := fixtureAPI(t, "valid-orders")
	_, err := runFixture(api, out, 4)
	require.EqualError(t, err, "expected 4 method+path pairs under /v1, found 3 (04 §2)")
}

// TestFixtures proves each lint fires (SC-PL-01, SC-PL-02, 04 §1.2, §1.4, §1.6).
func TestFixtures(t *testing.T) {
	cases := []struct{ fixture, want string }{
		{"duplicate-path", "path /v1/me/orders is already defined in"},
		{"duplicate-operationid", `operationId "listOrders" is already used by GET /v1/me/orders`},
		{"no-operationid", "GET /v1/me/orders has no operationId"},
		{"wrong-tag", "tags must be [orders] (the file's domain), got [me]"},
		{"duplicate-schema", "schema Order is already defined in"},
		{"reason-outside-common", "OrderCancelReason: reason lists live only in schemas/common.yaml"},
		{"stray-prefix", "/v2/me/orders: not under a namespace (04 §1.1: /v1/discover, /v1/me, /v1/biz/businesses/{bid}, /v1/admin, /v1/public, /v1/auth, /v1/rum, /v1/events, /v1/client-errors, /v1/test) and not /healthz, /readyz or /version"},
		{"outside-v1", "/metrics: not under a namespace"},
		{"no-default-problem", "GET /v1/me/orders: needs `default: {$ref: ../common/responses.yaml#/Problem}` (04 §1.4)"},
		{"missing-example", "POST /v1/me/orders: request body application/json needs an example"},
		{"missing-response-example", "POST /v1/me/orders: response 201 application/json needs an example"},
		{"invalid-example", `invalid operation POST: invalid example: Error at "/items/0/quantity": value must be an integer`},
		{"open-object", "POST /v1/me/orders: request body application/json: #/OrderItem accepts unknown fields; add `additionalProperties: false` or `x-open-object: true` (04 §1.4)"},
		{"idempotency-without-key", "POST /v1/me/orders: x-idempotency needs the parameter `$ref: ../common/parameters.yaml#/IdempotencyKey`"},
		{"key-without-idempotency", "GET /v1/me/orders: takes Idempotency-Key, so say `x-idempotency: required` (or optional)"},
		{"idempotency-on-get", "GET /v1/me/orders: x-idempotency is only for POST, PUT, PATCH and DELETE"},
		{"upload-on-get", "GET /v1/me/orders: x-upload is only for POST and PUT"},
		{"unregistered-problem", `GET /v1/me/orders: x-problems lists "order_gone", which is not in api/problems/ (SC-PL-02)`},
		{"literal-beside-param", "under /v1/me/orders: literal recent sits beside {oid}; move it to its own resource (04 §1.2)"},
		{"param-name-mismatch", "under /v1/me/orders: one parameter name per position, got {id} and {oid} (04 §1.2)"},
		{"ulid-outside", "GET /v1/discover/items/{iid}: path parameter {iid} is a ULID; ids appear only under /v1/me, /v1/biz, /v1/admin (04 §1.2)"},
		{"discover-id", "GET /v1/discover/stores/{storeId}: paths under /v1/discover/stores/ take only {slug} (schemas/common.yaml#/Slug) (04 §1.2)"},
		// D-100: the review's lints.
		{"unsafe-without-idempotency", "PATCH /v1/me/orders/{oid}: an unsafe operation under /v1/me, /v1/biz, /v1/admin states `x-idempotency: required`, `optional` or `none` explicitly (04 §1.6, D-100)"},
		{"idempotency-none-with-key", "POST /v1/me/orders: `x-idempotency: none` ignores the key, so drop the Idempotency-Key parameter (04 §1.6)"},
		{"idempotency-bad-value", "POST /v1/me/orders: x-idempotency must be none, optional or required, got always"},
		{"money-without-required", "POST /v1/me/orders: `x-money: true` needs `x-idempotency: required` (04 §1.6, D-100)"},
		{"array-without-maxitems", "POST /v1/me/orders: request body application/json: ../schemas/orders.yaml#/OrderRequest.items: an array in a request body needs maxItems (D-100)"},
		{"string-without-maxlength", "POST /v1/me/orders: request body application/json: #/OrderItem.sku: a string in a request body needs maxLength, an enum, a const, `format: binary` or a pattern that bounds its length (D-100)"},
		{"open-object-with-properties", "POST /v1/me/orders: request body application/json: ../schemas/orders.yaml#/OrderRequest declares properties, so `x-open-object: true` is not allowed; say `additionalProperties: false` (D-100)"},
		{"ref-other-domain", `paths/orders.yaml: $ref "../schemas/catalogue.yaml#/ItemList" reaches schemas/catalogue.yaml; a domain file references only its own schemas, schemas/common.yaml and common/ (D-100)`},
		{"banned-keyword", `schemas/orders.yaml: schemacheck: OrderItem.properties.note.if: keyword "if" is not allowed: the request validator does not enforce it (D-100)`},
		// D-100 round 2.
		{"merge-patch-without-upload", "PATCH /v1/me/orders/{oid}: request body application/merge-patch+json needs `x-upload: true`; a non-upload body is application/json only (D-100)"},
		{"json-plus-text", "POST /v1/me/orders: request body text/plain needs `x-upload: true`; a non-upload body is application/json only (D-100)"},
		{"nullable-array", "POST /v1/me/orders: request body application/json: ../schemas/orders.yaml#/OrderRequest.items: an array in a request body needs maxItems (D-100)"},
		{"nullable-string", "POST /v1/me/orders: request body application/json: #/OrderItem.sku: a string in a request body needs maxLength"},
		{"nullable-object", "POST /v1/me/orders: request body application/json: #/OrderItem accepts unknown fields; add `additionalProperties: false` or `x-open-object: true` (04 §1.4)"},
		{"money-id-without-x-money", "POST /v1/me/orders: operationId createOrder is on the 04 §1.6 money list (apigen moneyOperations), so it needs `x-money: true` (D-100)"},
		{"trailing-slash", `path "/v1/me/orders/{oid}/" must start with / and have no trailing slash or query`}, // apigen root refuses it first; TestLintPathShapesTrailingSlash covers the check step
		{"pattern-alternation", "#/OrderItem.sku: a string in a request body needs maxLength"},
		{"pattern-escaped-dollar", "#/OrderItem.sku: a string in a request body needs maxLength"},
		{"banned-dynamicref", `OrderItem.properties.sku.$dynamicRef: keyword "$dynamicRef" is not allowed`},
		{"banned-dynamicanchor", `OrderItem.properties.sku.$dynamicAnchor: keyword "$dynamicAnchor" is not allowed`},
		{"banned-contentschema", `OrderItem.properties.sku.contentSchema: keyword "contentSchema" is not allowed`},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			api, out := fixtureAPI(t, c.fixture)
			_, err := runFixture(api, out, 0)
			require.Error(t, err)
			require.Contains(t, err.Error(), c.want)
		})
	}
}

// TestRealContract keeps backend/api itself passing both apigen steps.
func TestRealContract(t *testing.T) {
	api, err := filepath.Abs(realAPI)
	require.NoError(t, err)
	out := filepath.Join(t.TempDir(), "gen", "root.yaml")
	report, err := runFixture(api, out, 0)
	require.NoError(t, err)
	require.Contains(t, report, "apigen check: ")
}

// toolPath returns the binary a test needs: env, else the path make setup
// installs it at (relative to this package). A plain `go test` skips when
// neither exists, so it works before make setup; under make check
// (TODAYZ_CHECK=1) a missing tool fails instead, so the round trip never
// silently skips (D-100 L9).
func toolPath(t *testing.T, env, installed string) string {
	t.Helper()
	if p := os.Getenv(env); p != "" {
		return p
	}
	p := filepath.Join("..", "..", "..", "..", installed)
	if _, err := os.Stat(p); err != nil {
		if os.Getenv("TODAYZ_CHECK") != "" {
			t.Fatalf("%s not installed and %s unset under make check: run make setup", installed, env)
		}
		t.Skipf("%s not installed: run make setup (or set %s)", installed, env)
	}
	return p
}

// TestBoundedPattern pins what counts as a length-bounding pattern (D-100 H-2).
func TestBoundedPattern(t *testing.T) {
	for pat, want := range map[string]bool{
		`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`: true,
		`^[a-z]{1,8}$`:                  true,
		`^\+91[0-9]{10}$`:               true, // escaped + is a literal
		`^[+*]{2}$`:                     true, // inside a class
		`^(ab){3}$`:                     true,
		`^[a-z0-9]+(-[a-z0-9]+)*$`:      false,
		`^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`: false,
		`^[a-z]{3,}$`: false,
		`[a-z]{3}`:    false, // unanchored
		`^[a-z]{3}`:   false,
		``:            false,
		// D-100 round 2, Low 6.
		`^a|b.*$`:        false, // a top-level alternation: the anchors cover one branch each
		`^a|b$`:          false,
		`^(a|b){2}$`:     true,  // the alternation is inside a group
		`^[a-z]{3}\$`:    false, // an escaped dollar is a literal, so the pattern is unanchored
		`^[a-z]{3}\\$`:   true,  // an escaped backslash, then the anchor
		`^[|]{2}$`:       true,  // inside a class
		`^(a|b)|c$`:      false,
		`^((a|b)|c){2}$`: true,
	} {
		require.Equal(t, want, boundedPattern(pat), pat)
	}
}

// TestLintPathShapesTrailingSlash (D-100 round 2, Low 5): the check step
// refuses a path ending in `/` on its own, so a bundle that skipped apigen
// root is caught too; `/` itself is fine.
func TestLintPathShapesTrailingSlash(t *testing.T) {
	require.Empty(t, lintPathShapes([]string{"/", "/v1/me/orders", "/v1/me/orders/{oid}"}))
	errs := lintPathShapes([]string{"/v1/me/orders/", "/v1/me/orders/{oid}/"})
	require.Len(t, errs, 2)
	require.EqualError(t, errs[0], "/v1/me/orders/: a path may not end in `/` (04 §1.2)")
	require.EqualError(t, errs[1], "/v1/me/orders/{oid}/: a path may not end in `/` (04 §1.2)")
}

// TestCodegenRoundTrip is the acceptance check: an operation in the contract
// becomes a Go stub, and removing it removes the stub. Like make gen it bundles
// the root with Redocly (oapi-codegen does not follow file references) and
// needs the pinned oapi-codegen from make setup.
func TestCodegenRoundTrip(t *testing.T) {
	tool := toolPath(t, "TODAYZ_OAPI_CODEGEN", filepath.Join(".tools", "bin", "oapi-codegen"))
	redocly := toolPath(t, "TODAYZ_REDOCLY", filepath.Join("shared", "api-client", "node_modules", ".bin", "redocly"))
	gen := func(fixture string) string {
		api, out := fixtureAPI(t, fixture)
		require.NoError(t, runRoot(api, out))
		bundle := filepath.Join(filepath.Dir(out), "openapi.yaml")
		cmd := exec.Command(redocly, "bundle", out, "-o", bundle) // #nosec G204 -- test tool path
		cmd.Env = append(os.Environ(), "REDOCLY_TELEMETRY=off", "REDOCLY_SUPPRESS_UPDATE_NOTICE=true")
		stdout, err := cmd.CombinedOutput()
		require.NoError(t, err, string(stdout))
		dir := filepath.Join(filepath.Dir(out), "api")
		require.NoError(t, runCodegen(api, bundle, dir, tool, io.Discard))
		_, err = os.Stat(filepath.Join(dir, "spec", "openapi.yaml"))
		require.NoError(t, err, "the spec package embeds the bundle")
		src, err := os.ReadFile(filepath.Join(dir, "orders", "api.gen.go")) // #nosec G304 -- the temp dir
		require.NoError(t, err)
		reg, err := os.ReadFile(filepath.Join(dir, "orders", "register.gen.go")) // #nosec G304 -- the temp dir
		require.NoError(t, err, "every domain gets register.gen.go (D-100 M2)")
		require.Contains(t, string(reg), "package ordersapi")
		require.Contains(t, string(reg), "func Register(r chi.Router, ssi StrictServerInterface, middlewares ...StrictMiddlewareFunc)")
		require.Contains(t, string(reg), "StrictHTTPServerOptions(openapi.StrictOptions())")
		require.Contains(t, string(reg), "ErrorHandlerFunc: openapi.StrictRequestError")
		return string(src)
	}

	with := gen("valid-orders")
	require.Contains(t, with, "package ordersapi")
	require.Contains(t, with, "PlaceOrder(")
	require.Contains(t, with, "type OrderRequest struct")
	require.Contains(t, with, "ListOrders(")

	without := gen("valid-get-only")
	require.Contains(t, without, "ListOrders(")
	require.NotContains(t, without, "PlaceOrder(")
}

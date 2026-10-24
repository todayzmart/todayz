package openapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/httpx/mw"
	"github.com/todayz/backend/internal/platform/httpx/openapi"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/httpx/route"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
)

// spec is a small contract in the shape of api/: closed objects, the
// Idempotency-Key and If-Match headers, a ULID path parameter, a CSV upload
// and a binary media upload.
const spec = `
openapi: 3.1.0
info: {title: test, version: "1"}
paths:
  /v1/me/orders:
    get:
      operationId: listOrders
      parameters:
        - name: limit
          in: query
          schema: {type: integer, minimum: 1, maximum: 50}
      responses:
        "200": {description: ok}
    post:
      operationId: createOrder
      x-idempotency: required
      parameters:
        - $ref: "#/components/parameters/IdempotencyKey"
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: "#/components/schemas/OrderRequest"}
      responses:
        "201": {description: created}
  /v1/me/orders/{oid}:
    parameters:
      - name: oid
        in: path
        required: true
        schema: {$ref: "#/components/schemas/Ulid"}
    get:
      operationId: getOrder
      responses:
        "200": {description: ok}
    patch:
      operationId: updateOrder
      x-idempotency: optional
      parameters:
        - $ref: "#/components/parameters/IdempotencyKey"
        - name: If-Match
          in: header
          required: true
          schema: {type: string}
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: false
              properties:
                status: {type: string, enum: [confirmed, cancelled]}
      responses:
        "200": {description: ok}
  /v1/biz/businesses/{bid}/catalogue/imports:
    post:
      operationId: importCatalogue
      x-upload: true
      parameters:
        - name: bid
          in: path
          required: true
          schema: {$ref: "#/components/schemas/Ulid"}
      requestBody:
        required: true
        content:
          text/csv:
            schema: {type: string}
      responses:
        "202": {description: accepted}
  /v1/biz/businesses/{bid}/media:
    post:
      operationId: uploadMedia
      x-upload: true
      x-idempotency: none
      parameters:
        - name: bid
          in: path
          required: true
          schema: {$ref: "#/components/schemas/Ulid"}
      requestBody:
        required: true
        content:
          application/vnd.openxmlformats-officedocument.spreadsheetml.sheet:
            schema: {type: string, format: binary}
          image/jpeg:
            schema: {type: string, format: binary}
          audio/webm:
            schema: {type: string, format: binary}
          multipart/form-data:
            schema:
              type: object
              properties:
                file: {type: string, format: binary}
      responses:
        "202": {description: accepted}
components:
  parameters:
    IdempotencyKey:
      name: Idempotency-Key
      in: header
      required: false
      schema: {$ref: "#/components/schemas/Ulid"}
  schemas:
    Ulid:
      type: string
      pattern: "^[0-9A-HJKMNP-TV-Z]{26}$"
    OrderRequest:
      type: object
      additionalProperties: false
      required: [items]
      properties:
        note:
          type: string
          pattern: "^[a-z ]*$"
        when:
          type: string
          format: date-time
        extra:
          oneOf:
            - type: array
              maxItems: 2
              items: {type: string}
            - type: object
              additionalProperties: false
              properties:
                tags:
                  anyOf:
                    - type: array
                      maxItems: 1
                      items: {type: string}
                    - type: array
                      maxItems: 3
                      items: {type: integer}
        items:
          type: array
          minItems: 1
          maxItems: 50
          items:
            type: object
            additionalProperties: false
            required: [sku, quantity]
            properties:
              sku: {type: string}
              quantity: {type: integer, minimum: 1, maximum: 3}
`

const (
	ulid    = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	ordersP = "/v1/me/orders"
	orderP  = "/v1/me/orders/" + ulid
	importP = "/v1/biz/businesses/" + ulid + "/catalogue/imports"
	mediaP  = "/v1/biz/businesses/" + ulid + "/media"
	validJS = `{"items":[{"sku":"a","quantity":2}]}`
)

var user = reqctx.Principal{Kind: "customer", UserID: ulid}

func newValidator(t *testing.T) *openapi.Validator {
	t.Helper()
	v, err := openapi.New([]byte(spec))
	require.NoError(t, err)
	return v
}

// testMux mounts every pattern of spec for every method; routed uses it to
// give a request the chi route context the validator reads (D-100).
var testMux = func() *chi.Mux {
	m := chi.NewRouter()
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, p := range []string{ordersP, "/v1/me/orders/{oid}", "/v1/biz/businesses/{bid}/catalogue/imports", "/v1/biz/businesses/{bid}/media", "/v1/me/things"} {
		m.Handle(p, noop)
	}
	return m
}()

// routed returns req with chi's route context filled as the real router
// fills it before the inline validator runs.
func routed(req *http.Request) *http.Request {
	rctx := chi.NewRouteContext()
	path := req.URL.RawPath
	if path == "" {
		path = req.URL.Path
	}
	testMux.Find(rctx, req.Method, path)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// jsonReq is a routed request from a signed-in customer.
func jsonReq(method, target, body string) *http.Request {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return routed(req.WithContext(reqctx.WithPrincipal(req.Context(), user)))
}

// fields returns the problem's code, status and fields.
func fields(t *testing.T, err error) (string, int, map[string]string) {
	t.Helper()
	var p *problem.Error
	require.ErrorAs(t, err, &p)
	return p.Code, problem.Status(err), p.Fields
}

func TestNewRejectsInvalidSpec(t *testing.T) {
	_, err := openapi.New([]byte("openapi: 3.1.0\ninfo: {title: t}\npaths: {}\n"))
	require.Error(t, err) // info.version is required
	_, err = openapi.New([]byte("not: [yaml"))
	require.Error(t, err)
}

// TestNewRefusesUnknownIdempotency (D-100 M-1): x-idempotency is none,
// optional or required; anything else is a typo the API must not start with.
func TestNewRefusesUnknownIdempotency(t *testing.T) {
	bad := strings.Replace(spec, "x-idempotency: optional", "x-idempotency: always", 1)
	_, err := openapi.New([]byte(bad))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PATCH /v1/me/orders/{oid}: x-idempotency must be none, optional or required")
}

// TestNewRefusesUnenforcedKeywords (D-100 M-2): a schema keyword the
// built-in visitor does not enforce would pass every body; New refuses it
// and names the schema.
func TestNewRefusesUnenforcedKeywords(t *testing.T) {
	bad := strings.Replace(spec, "        note:\n", "        note:\n          if: {type: string}\n", 1)
	_, err := openapi.New([]byte(bad))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `keyword "if" is not allowed`)
	assert.Contains(t, err.Error(), "components.schemas.OrderRequest.properties.note")
}

func TestValidPasses(t *testing.T) {
	v := newValidator(t)
	for _, req := range []*http.Request{
		jsonReq(http.MethodPost, ordersP, validJS),
		jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":1}],"note":"gift wrap","when":"2026-10-22T10:00:00Z"}`),
		jsonReq(http.MethodGet, ordersP+"?limit=50", ""),
		jsonReq(http.MethodGet, orderP, ""),
	} {
		assert.NoError(t, v.ValidateRequest(req), req.Method+" "+req.URL.String())
	}
	// The body is readable again after validation.
	req := jsonReq(http.MethodPost, ordersP, validJS)
	require.NoError(t, v.ValidateRequest(req))
	b, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.Equal(t, validJS, string(b))
}

func TestMissingField(t *testing.T) {
	v := newValidator(t)
	code, status, f := fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"note":"hi"}`)))
	assert.Equal(t, "validation_failed", code)
	assert.Equal(t, http.StatusUnprocessableEntity, status, "validation_failed is 422 (D-100)")
	assert.Equal(t, map[string]string{"body.items": "required"}, f)

	// Nested: the path names the array element.
	_, _, f = fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":1},{"sku":"b"}]}`)))
	assert.Equal(t, map[string]string{"body.items[1].quantity": "required"}, f)
}

func TestWrongType(t *testing.T) {
	v := newValidator(t)
	_, _, f := fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":"two"}]}`)))
	assert.Equal(t, map[string]string{"body.items[0].quantity": "value must be an integer"}, f)

	_, _, f = fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":"none"}`)))
	assert.Equal(t, map[string]string{"body.items": "value must be an array"}, f)
}

// A whole number written as 1.0 or 1e0 is an integer to JSON Schema; the
// handler reads it as a plain integer, so encoding/json can fill an int
// (D-217, contract fuzzing on GitHub).
func TestWholeNumbersReachTheHandlerAsIntegers(t *testing.T) {
	v := newValidator(t)
	type order struct {
		Items []struct {
			Sku      string `json:"sku"`
			Quantity int    `json:"quantity"`
		} `json:"items"`
		Note string `json:"note"`
	}
	for _, body := range []string{
		`{"items":[{"sku":"a","quantity":2.0}]}`,
		`{"items":[{"sku":"a","quantity":2e0}]}`,
		`{"items":[{"sku":"a","quantity":0.2E1}],"note":"x"}`,
	} {
		req := jsonReq(http.MethodPost, ordersP, body)
		require.NoError(t, v.ValidateRequest(req), body)
		b, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Equal(t, int64(len(b)), req.ContentLength, body)
		var o order
		require.NoError(t, json.Unmarshal(b, &o), body)
		require.Len(t, o.Items, 1)
		assert.Equal(t, 2, o.Items[0].Quantity, body)
	}

	// Nothing to rewrite: the bytes are untouched (key order, spacing, a fraction).
	for _, body := range []string{validJS, `{ "items": [ {"sku":"e.E","quantity":3} ], "note": "a b" }`} {
		req := jsonReq(http.MethodPost, ordersP, body)
		require.NoError(t, v.ValidateRequest(req))
		b, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Equal(t, body, string(b))
	}

	// A fraction stays a refused non-integer.
	_, _, f := fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":2.5}]}`)))
	assert.Equal(t, map[string]string{"body.items[0].quantity": "value must be an integer"}, f)
}

func TestUnknownFieldRefused(t *testing.T) {
	// 04 §1.4: request objects are closed (additionalProperties: false).
	v := newValidator(t)
	_, _, f := fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":1}],"bogus":1}`)))
	assert.Equal(t, map[string]string{"body.bogus": "not allowed"}, f)

	_, _, f = fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":1,"gift":true}]}`)))
	assert.Equal(t, map[string]string{"body.items[0].gift": "not allowed"}, f)
}

// TestUnknownKeyIsRedacted (D-100 L4, security review L-2): an unknown key
// is echoed in the field path only when it is a plain token; anything else
// (a key carrying a payload, or a long one) is written as <redacted>.
func TestUnknownKeyIsRedacted(t *testing.T) {
	v := newValidator(t)
	for key, want := range map[string]string{
		"ok_Key9":                   "body.ok_Key9",
		strings.Repeat("a", 64):     "body." + strings.Repeat("a", 64),
		strings.Repeat("a", 65):     "body.<redacted>",
		"we!rd":                     "body.<redacted>",
		"<script>alert(1)</script>": "body.<redacted>",
		"a.b":                       "body.<redacted>",
		"name with space":           "body.<redacted>",
	} {
		err := v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":1}],"`+key+`":1}`))
		_, _, f := fields(t, err)
		assert.Equal(t, map[string]string{want: "not allowed"}, f, key)
		if want == "body.<redacted>" {
			assert.NotContains(t, err.Error(), key)
		}
	}
}

func TestRangeAndFormat(t *testing.T) {
	v := newValidator(t)
	_, _, f := fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":4}]}`)))
	assert.Equal(t, map[string]string{"body.items[0].quantity": "number must be at most 3"}, f)

	_, _, f = fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[]}`)))
	assert.Equal(t, map[string]string{"body.items": "minimum number of items is 1"}, f)

	_, _, f = fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":1}],"when":"yesterday"}`)))
	assert.Equal(t, map[string]string{"body.when": "not a valid date-time"}, f)

	_, _, f = fields(t, v.ValidateRequest(jsonReq(http.MethodPatch, orderP, `{"status":"shipped"}`)))
	assert.Equal(t, map[string]string{"header.If-Match": "required", "body.status": "not one of the allowed values"}, f)
}

func TestSeveralProblemsAtOnce(t *testing.T) {
	v := newValidator(t)
	_, _, f := fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"bogus":1,"note":"Hi!"}`)))
	assert.Equal(t, map[string]string{
		"body.items": "required",
		"body.bogus": "not allowed",
		"body.note":  "invalid format",
	}, f)
}

// TestFieldsAreCapped (D-100, security review H-2): a body with more
// problems than MaxFields gets MaxFields of them and fieldsTruncated, not an
// error body that grows with the request.
func TestFieldsAreCapped(t *testing.T) {
	v := newValidator(t)
	// 30 items with two problems each: 60 problems, under maxItems.
	items := strings.Repeat(`{"sku":1,"quantity":"x"},`, 30)
	err := v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[`+items[:len(items)-1]+`]}`))
	var p *problem.Error
	require.ErrorAs(t, err, &p)
	assert.Equal(t, "validation_failed", p.Code)
	assert.Len(t, p.Fields, openapi.MaxFields)
	assert.Equal(t, true, p.Extensions["fieldsTruncated"])

	// Under the cap nothing is marked.
	err = v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":1,"quantity":"x"}]}`))
	require.ErrorAs(t, err, &p)
	assert.Len(t, p.Fields, 2)
	assert.Nil(t, p.Extensions["fieldsTruncated"])
}

func TestValueNeverLeaks(t *testing.T) {
	v := newValidator(t)
	const secret = "SECRET-9876"
	for _, body := range []string{
		`{"items":[{"sku":"a","quantity":1}],"note":"` + secret + `"}`,
		`{"items":[{"sku":"a","quantity":1}],"when":"` + secret + `"}`,
		`{"items":[{"sku":"a","quantity":"` + secret + `"}]}`,
		`{"items":[{"sku":"a","quantity":1}],"` + secret + `":1}`,
	} {
		err := v.ValidateRequest(jsonReq(http.MethodPost, ordersP, body))
		require.Error(t, err)
		_, _, f := fields(t, err)
		for path, reason := range f {
			assert.NotContains(t, reason, secret, body)
			assert.NotContains(t, path, secret, body)
		}
		// The problem's Error() (logged) carries no value either.
		assert.NotContains(t, err.Error(), secret)
	}
	req := jsonReq(http.MethodPatch, orderP, `{"status":"`+secret+`"}`)
	req.Header.Set("If-Match", `"1"`)
	_, _, f := fields(t, v.ValidateRequest(req))
	assert.Equal(t, map[string]string{"body.status": "not one of the allowed values"}, f)
}

func TestParameters(t *testing.T) {
	v := newValidator(t)
	_, _, f := fields(t, v.ValidateRequest(jsonReq(http.MethodGet, ordersP+"?limit=0", "")))
	assert.Equal(t, map[string]string{"query.limit": "number must be at least 1"}, f)

	_, _, f = fields(t, v.ValidateRequest(jsonReq(http.MethodGet, ordersP+"?limit=many", "")))
	assert.Equal(t, map[string]string{"query.limit": "invalid"}, f)

	// The path parameter is the one chi extracted (D-100).
	_, _, f = fields(t, v.ValidateRequest(jsonReq(http.MethodGet, "/v1/me/orders/not-a-ulid", "")))
	assert.Equal(t, map[string]string{"path.oid": "invalid format"}, f)

	req := jsonReq(http.MethodPost, ordersP, validJS)
	req.Header.Set("Idempotency-Key", "nope")
	_, _, f = fields(t, v.ValidateRequest(req))
	assert.Equal(t, map[string]string{"header.Idempotency-Key": "invalid format"}, f)
}

// TestUndeclaredQueryRefused: a query name the operation does not declare is
// refused like an unknown body property, and a strange name is not echoed.
func TestUndeclaredQueryRefused(t *testing.T) {
	v := newValidator(t)
	require.NoError(t, v.ValidateRequest(jsonReq(http.MethodGet, ordersP+"?limit=5", "")))

	_, _, f := fields(t, v.ValidateRequest(jsonReq(http.MethodGet, ordersP+"?limit=5&extra=1", "")))
	assert.Equal(t, map[string]string{"query.extra": "not allowed"}, f)

	err := v.ValidateRequest(jsonReq(http.MethodGet, ordersP+"?%3Cscript%3E=1", ""))
	_, _, f = fields(t, err)
	assert.Equal(t, map[string]string{"query.<redacted>": "not allowed"}, f)
	assert.NotContains(t, err.Error(), "script")
}

func TestWrongContentType(t *testing.T) {
	v := newValidator(t)
	for _, ct := range []string{"text/plain", "application/xml", "", "application/json; charset=\""} {
		req := jsonReq(http.MethodPost, ordersP, validJS)
		req.Header.Set("Content-Type", ct)
		code, status, _ := fields(t, v.ValidateRequest(req))
		assert.Equal(t, "unsupported_media_type", code, ct)
		assert.Equal(t, http.StatusUnsupportedMediaType, status, ct)
	}
	// A parameter is allowed.
	req := jsonReq(http.MethodPost, ordersP, validJS)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	assert.NoError(t, v.ValidateRequest(req))
}

func TestMalformedAndEmptyBody(t *testing.T) {
	v := newValidator(t)
	for _, body := range []string{`{"items":[`, `{"items":[{"sku":"a","quantity":1}]} x`, `{"items":[{"sku":"a","quantity":1}]}{}`, `[] []`} {
		code, status, _ := fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, body)))
		assert.Equal(t, "bad_request", code, body)
		assert.Equal(t, http.StatusBadRequest, status, body)
	}

	req := httptest.NewRequest(http.MethodPost, ordersP, nil)
	req.Header.Set("Content-Type", "application/json")
	code, status, f := fields(t, v.ValidateRequest(routed(req.WithContext(reqctx.WithPrincipal(req.Context(), user)))))
	assert.Equal(t, "validation_failed", code)
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, map[string]string{"body": "required"}, f)
}

func TestOversizedBody(t *testing.T) {
	// bodylimit (slot 10) wraps the body in http.MaxBytesReader; the validator
	// reads past the limit and must answer 413.
	v := newValidator(t)
	req := httptest.NewRequest(http.MethodPost, ordersP, strings.NewReader(validJS))
	req.Header.Set("Content-Type", "application/json")
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 8)
	code, status, _ := fields(t, v.ValidateRequest(routed(req.WithContext(reqctx.WithPrincipal(req.Context(), user)))))
	assert.Equal(t, "payload_too_large", code)
	assert.Equal(t, http.StatusRequestEntityTooLarge, status)
}

// ampBody is the security review's H-2 proof: a JSON body of empty objects
// in the items array, as large as the 1 MB body limit lets through.
func ampBody() string {
	var b strings.Builder
	b.WriteString(`{"items":[`)
	for b.Len() < mw.DefaultBodyLimit-4 {
		b.WriteString("{},")
	}
	s := b.String()
	return s[:len(s)-1] + "]}"
}

// TestAmplificationIsRefused (security review H-2): the 1 MB body is refused
// as 400 bad_request before any schema work, allocating a few megabytes at
// most and naming no field. Before D-100 the schema visitor built a
// SchemaError per element and allocated about 340 MB.
func TestAmplificationIsRefused(t *testing.T) {
	v := newValidator(t)
	body := ampBody()
	require.LessOrEqual(t, len(body), mw.DefaultBodyLimit)
	require.Greater(t, len(body), mw.DefaultBodyLimit-8)
	req := jsonReq(http.MethodPost, ordersP, body)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	err := v.ValidateRequest(req)
	runtime.ReadMemStats(&after)

	code, status, f := fields(t, err)
	assert.Equal(t, "bad_request", code)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Empty(t, f, "no field per element")
	allocated := after.TotalAlloc - before.TotalAlloc
	assert.Less(t, allocated, uint64(5<<20), "allocated %d bytes for a %d byte body", allocated, len(body))
}

// TestArrayPastMaxItemsIsRefused (H-2): an array longer than its maxItems is
// refused before the visitor sees its elements, so 51 broken items are one
// 400, not 51 fields.
func TestArrayPastMaxItemsIsRefused(t *testing.T) {
	v := newValidator(t)
	items := strings.Repeat(`{},`, 51)
	code, status, f := fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[`+items[:len(items)-1]+`]}`)))
	assert.Equal(t, "bad_request", code)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Empty(t, f)

	// Exactly maxItems is the schema's business.
	items = strings.Repeat(`{"sku":"a","quantity":1},`, 50)
	assert.NoError(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[`+items[:len(items)-1]+`]}`)))
}

// TestTooManyValuesIsRefused (H-2): past MaxValues the body is refused by
// the token count alone, whatever the schema.
func TestTooManyValuesIsRefused(t *testing.T) {
	v := newValidator(t)
	note := strings.Repeat("[", openapi.MaxValues) + strings.Repeat("]", openapi.MaxValues)
	code, _, _ := fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":1}],"note":`+note+`}`)))
	assert.Equal(t, "bad_request", code)
}

// TestAnonymousIs401 (D-100 D3, round 2 M-B): under /v1/me, /v1/biz and
// /v1/admin an anonymous caller is refused before validation, so it never
// learns a field name, whatever is wrong with its body.
func TestAnonymousIs401(t *testing.T) {
	v := newValidator(t)
	for _, tc := range []struct{ method, target, body string }{
		{http.MethodPost, importP, "bogus"},
		{http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":9}],"bogus":true}`},
		{http.MethodPost, ordersP, "not json"},
		{http.MethodGet, orderP, ""},
	} {
		var body io.Reader
		if tc.body != "" {
			body = strings.NewReader(tc.body)
		}
		req := httptest.NewRequest(tc.method, tc.target, body)
		req.Header.Set("Content-Type", "application/json")
		code, status, f := fields(t, v.ValidateRequest(routed(req)))
		assert.Equal(t, "unauthorized", code, tc.target)
		assert.Equal(t, http.StatusUnauthorized, status, tc.target)
		assert.Empty(t, f, tc.target)
	}
}

// TestNewRefusesMediaTypes (D-100 round 2, M-A): a request body is
// application/json unless the operation is x-upload, and then it is anything
// but JSON. New refuses the contract, so the API never starts with a body the
// validator would neither decode nor stream on purpose.
func TestNewRefusesMediaTypes(t *testing.T) {
	for name, tc := range map[string]struct{ old, repl, want string }{
		"non-upload PATCH with merge-patch": {
			"          application/json:\n            schema:\n              type: object\n              additionalProperties: false\n              properties:\n                status:",
			"          application/merge-patch+json:\n            schema:\n              type: object\n              additionalProperties: false\n              properties:\n                status:",
			"PATCH /v1/me/orders/{oid}: request body application/merge-patch+json needs x-upload: true; a non-upload body is application/json only (D-100)",
		},
		"JSON plus text": {
			"          application/json:\n            schema: {$ref: \"#/components/schemas/OrderRequest\"}\n",
			"          application/json:\n            schema: {$ref: \"#/components/schemas/OrderRequest\"}\n          text/plain:\n            schema: {type: string}\n",
			"POST /v1/me/orders: request body text/plain needs x-upload: true; a non-upload body is application/json only (D-100)",
		},
		"upload with JSON": {
			"          text/csv:\n            schema: {type: string}\n",
			"          application/json:\n            schema: {type: string}\n",
			"POST /v1/biz/businesses/{bid}/catalogue/imports: an x-upload operation may not take application/json (D-100)",
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := strings.Replace(spec, tc.old, tc.repl, 1)
			require.NotEqual(t, spec, bad, "the replacement must apply")
			_, err := openapi.New([]byte(bad))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestMaxItemsUnderOneOf (D-100 round 2, M-C): the pre-scan follows oneOf
// and anyOf. An array no fitting branch admits is 400 before the visitor;
// one that some branch admits goes on to schema validation.
func TestMaxItemsUnderOneOf(t *testing.T) {
	v := newValidator(t)
	post := func(extra string) (string, int, map[string]string) {
		return fields(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":1}],"extra":`+extra+`}`)))
	}
	code, status, f := post(`["a","b","c"]`)
	assert.Equal(t, "bad_request", code, "3 items under the array branch's maxItems 2")
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Empty(t, f)

	code, status, _ = post(`{"tags":[1,2,3,4]}`)
	assert.Equal(t, "bad_request", code, "4 tags: neither anyOf branch admits it")
	assert.Equal(t, http.StatusBadRequest, status)

	// Two tags: the second anyOf branch admits the length, so the schema decides (strings, not integers).
	code, status, f = post(`{"tags":["a","b"]}`)
	assert.Equal(t, "validation_failed", code)
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.NotEmpty(t, f)

	assert.NoError(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":1}],"extra":["a","b"]}`)))
	assert.NoError(t, v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":1}],"extra":{"tags":[1,2,3]}}`)))
}

// errorBody is the D-100 round 2 Low 2 body: 1 MB of unknown properties
// (24 items × 200 unknown string fields, 9,627 values) that passes the
// pre-scan (under MaxValues, every array within maxItems) and makes the
// schema visitor collect an error per unknown field.
func errorBody() string {
	var b strings.Builder
	b.WriteString(`{"items":[`)
	val := strings.Repeat("v", 200)
	for i := 0; i < 24; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('{')
		for j := 0; j < 200; j++ {
			if j > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `"f%03d_%02d":"%s"`, j, i, val)
		}
		b.WriteByte('}')
	}
	b.WriteString("]}")
	return b.String()
}

// TestErrorAmplificationBound (D-100 round 2, Low 2): kin-openapi v0.149.0
// has no "stop after N errors" option (only FailFast, which hides every
// field, or MultiErrors), so the bound on a body that passes the pre-scan
// but fails everywhere is measured instead of configured. Measured on the
// build machine (Apple M-series, Go 1.26.10, three runs): the 1,022,459-byte
// errorBody with 4,800 unknown fields (plus 48 missing required ones)
// allocates 10.8 MB and takes 17–20 ms wall-clock; the response names
// MaxFields of them. Against the 1 MB body that is about 10× amplification,
// where the pre-D-100 visitor allocated about 340 MB for ampBody. The
// assertions are generous ceilings (200 MB, 2 s) so the test fails only on a
// regression of the kind the review feared (hundreds of MB, seconds), not
// on a slow CI runner.
func TestErrorAmplificationBound(t *testing.T) {
	v := newValidator(t)
	body := errorBody()
	require.LessOrEqual(t, len(body), mw.DefaultBodyLimit)
	require.Greater(t, len(body), mw.DefaultBodyLimit*9/10)
	req := jsonReq(http.MethodPost, ordersP, body)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	err := v.ValidateRequest(req)
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)

	code, status, f := fields(t, err)
	assert.Equal(t, "validation_failed", code)
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Len(t, f, openapi.MaxFields)
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("errorBody: %d bytes, %d bytes allocated, %s", len(body), allocated, elapsed)
	assert.Less(t, allocated, uint64(200<<20), "allocated %d bytes for a %d byte body", allocated, len(body))
	assert.Less(t, elapsed, 2*time.Second)
}

// TestNonJSONBodies (D-100 M3, L10): a declared media type other than JSON is
// checked for its media type only; the bytes reach the handler as they came.
func TestNonJSONBodies(t *testing.T) {
	v := newValidator(t)
	bin := "\x00\x01\xff\xfePK\x03\x04 not json at all"
	for _, ct := range []string{
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"image/jpeg",
		"audio/webm",
		"multipart/form-data; boundary=xyz",
	} {
		req := httptest.NewRequest(http.MethodPost, mediaP, strings.NewReader(bin))
		req.Header.Set("Content-Type", ct)
		req = routed(req.WithContext(reqctx.WithPrincipal(req.Context(), user)))
		require.NoError(t, v.ValidateRequest(req), ct)
		b, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Equal(t, bin, string(b), ct)
	}
	for _, ct := range []string{"application/json", "text/csv", "image/png", ""} {
		req := httptest.NewRequest(http.MethodPost, mediaP, strings.NewReader(bin))
		req.Header.Set("Content-Type", ct)
		req = routed(req.WithContext(reqctx.WithPrincipal(req.Context(), user)))
		code, _, _ := fields(t, v.ValidateRequest(req))
		assert.Equal(t, "unsupported_media_type", code, ct)
	}
	csv := jsonReq(http.MethodPost, importP, "sku,qty\n")
	csv.Header.Set("Content-Type", "text/csv")
	assert.NoError(t, v.ValidateRequest(csv))
}

// TestMountedButNotInContractFailsClosed (D-100 D2): a route chi knows and
// the contract does not is 500, never a pass-through.
func TestMountedButNotInContractFailsClosed(t *testing.T) {
	v := newValidator(t)
	req := jsonReq(http.MethodGet, "/v1/me/things", "")
	code, status, _ := fields(t, v.ValidateRequest(req))
	assert.Equal(t, "internal", code)
	assert.Equal(t, http.StatusInternalServerError, status)

	// No chi context at all (the validator was not mounted inline): 500 too.
	code, _, _ = fields(t, v.ValidateRequest(httptest.NewRequest(http.MethodGet, ordersP, nil)))
	assert.Equal(t, "internal", code)
}

func TestHasPolicyAndOperations(t *testing.T) {
	v := newValidator(t)
	assert.True(t, v.Has(http.MethodPost, "/v1/me/orders"))
	assert.True(t, v.Has(http.MethodGet, "/v1/me/orders/{oid}"))
	assert.False(t, v.Has(http.MethodDelete, "/v1/me/orders/{oid}"))
	assert.False(t, v.Has(http.MethodGet, "/v1/me/orders/{id}"))

	p, ok := v.Policy(http.MethodPost, "/v1/me/orders")
	assert.True(t, ok)
	assert.Equal(t, route.Policy{Idempotency: route.IdempotencyRequired}, p)
	p, ok = v.Policy(http.MethodPatch, "/v1/me/orders/{oid}")
	assert.True(t, ok)
	assert.Equal(t, route.Policy{Idempotency: route.IdempotencyOptional}, p)
	p, ok = v.Policy(http.MethodPost, "/v1/biz/businesses/{bid}/catalogue/imports")
	assert.True(t, ok)
	assert.Equal(t, route.Policy{Upload: true}, p)
	p, ok = v.Policy(http.MethodPost, "/v1/biz/businesses/{bid}/media")
	assert.True(t, ok, "x-idempotency: none is an explicit policy")
	assert.Equal(t, route.Policy{Upload: true}, p)
	_, ok = v.Policy(http.MethodGet, "/v1/me/orders")
	assert.False(t, ok, "an operation without the extensions has no policy")

	assert.Equal(t, []string{
		"GET /v1/me/orders",
		"GET /v1/me/orders/{oid}",
		"PATCH /v1/me/orders/{oid}",
		"POST /v1/biz/businesses/{bid}/catalogue/imports",
		"POST /v1/biz/businesses/{bid}/media",
		"POST /v1/me/orders",
	}, v.Operations())
}

func TestTableMergesPoliciesWithSpec(t *testing.T) {
	v := newValidator(t)
	mux := chi.NewRouter()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.Post("/v1/me/orders", ok)
	mux.Post("/v1/biz/businesses/{bid}/catalogue/imports", ok)
	// Code may strengthen the contract (D-100 M-1).
	mux.Method(http.MethodPatch, "/v1/me/orders/{oid}", route.Handle(route.Policy{Idempotency: route.IdempotencyRequired}, ok))
	tbl := route.NewTableWith(mux, v)
	require.NoError(t, tbl.Check())
	assert.Equal(t, route.Policy{Idempotency: route.IdempotencyRequired}, tbl.Lookup(http.MethodPost, ordersP))
	assert.Equal(t, route.Policy{Upload: true}, tbl.Lookup(http.MethodPost, importP))
	assert.Equal(t, route.Policy{Idempotency: route.IdempotencyRequired}, tbl.Lookup(http.MethodPatch, orderP))
	assert.Equal(t, route.Policy{}, route.NewTable(mux).Lookup(http.MethodPost, ordersP), "no source: zero policy")

	// Code may not weaken it: the table reports the conflict and serves the
	// stronger rule anyway.
	weak := chi.NewRouter()
	weak.Method(http.MethodPost, "/v1/me/orders", route.Handle(route.Policy{Idempotency: route.IdempotencyOptional}, ok))
	weak.Method(http.MethodPost, "/v1/biz/businesses/{bid}/catalogue/imports", route.Handle(route.Policy{Idempotency: route.IdempotencyRequired}, ok))
	tbl = route.NewTableWith(weak, v)
	err := tbl.Check()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "POST /v1/me/orders: code declares idempotency optional")
	assert.Contains(t, err.Error(), "POST /v1/biz/businesses/{bid}/catalogue/imports: code declares idempotency required, upload false")
	assert.Equal(t, route.Policy{Idempotency: route.IdempotencyRequired}, tbl.Lookup(http.MethodPost, ordersP))
	assert.Equal(t, route.Policy{Idempotency: route.IdempotencyRequired, Upload: true}, tbl.Lookup(http.MethodPost, importP))
}

// newPipeline mounts the real 02 §2.4 pipeline with the validator in slot 13
// and the contract's policies in slot 9, behind a stand-in for authn that
// signs every request in as user.
func newPipeline(t *testing.T, v *openapi.Validator) (http.Handler, *[]byte) {
	t.Helper()
	var got []byte
	r := httpx.NewRouter(httpx.Options{
		Validator:     v,
		Policies:      v,
		Authenticator: stubAuthenticator{},
		Idempotency: idempotency.Options{
			Store: idempotency.NewMemoryStore(),
			Clock: clock.NewFake(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)),
		},
	})
	echo := func(status int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			got = b
			w.WriteHeader(status)
		}
	}
	r.Post("/v1/me/orders", echo(http.StatusCreated))
	r.Get("/v1/me/orders", echo(http.StatusOK))
	r.Get("/v1/me/orders/{oid}", echo(http.StatusOK))
	r.Patch("/v1/me/orders/{oid}", echo(http.StatusOK))
	r.Post("/v1/biz/businesses/{bid}/catalogue/imports", echo(http.StatusAccepted))
	r.Post("/v1/biz/businesses/{bid}/media", echo(http.StatusAccepted))
	require.NoError(t, r.Check())
	// Sign every request in through slot 11 (W1-01): a business-app token
	// under /v1/biz, a customer-app token elsewhere.
	signedIn := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		tok := "customer"
		if strings.HasPrefix(req.URL.Path, "/v1/biz/") {
			tok = "business"
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		r.ServeHTTP(w, req)
	})
	return signedIn, &got
}

// stubAuthenticator signs in user with the app its token names.
type stubAuthenticator struct{}

func (stubAuthenticator) Authenticate(_ context.Context, tok string) (authn.Principal, error) {
	app, ok := authn.ParseApp(tok)
	if !ok {
		return authn.Principal{}, problem.New("unauthorized", "")
	}
	id, err := ids.Parse(user.UserID)
	if err != nil {
		return authn.Principal{}, err
	}
	return authn.Principal{UserID: id, App: app, SessionID: id, AMR: authn.AMROTP}, nil
}

func do(r http.Handler, req *http.Request) (*httptest.ResponseRecorder, problem.Body) {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var body problem.Body
	if strings.HasPrefix(rec.Header().Get("Content-Type"), problem.ContentType) {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return rec, body
}

// plainReq is an unrouted request: the pipeline routes it.
func plainReq(method, target, body string) *http.Request {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func TestPipeline(t *testing.T) {
	v := newValidator(t)
	r, got := newPipeline(t, v)

	t.Run("valid request reaches the handler with its body", func(t *testing.T) {
		req := plainReq(http.MethodPost, ordersP, validJS)
		req.Header.Set("Idempotency-Key", ulid)
		rec, _ := do(r, req)
		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, validJS, string(*got))
	})
	t.Run("the validator sees chi's route pattern and parameters", func(t *testing.T) {
		req := plainReq(http.MethodPatch, orderP, `{"status":"confirmed"}`)
		req.Header.Set("If-Match", `"1"`)
		rec, body := do(r, req)
		assert.Equal(t, http.StatusOK, rec.Code, body.Code)

		rec, body = do(r, plainReq(http.MethodGet, "/v1/me/orders/not-a-ulid", ""))
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Equal(t, map[string]string{"path.oid": "invalid format"}, body.Fields)
	})
	t.Run("x-idempotency required comes from the contract", func(t *testing.T) {
		rec, body := do(r, plainReq(http.MethodPost, ordersP, validJS))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "idempotency_key_required", body.Code)
	})
	t.Run("validation failure is 422 problem JSON", func(t *testing.T) {
		req := plainReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":9}]}`)
		req.Header.Set("Idempotency-Key", ulid[:25]+"W")
		rec, body := do(r, req)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Equal(t, "validation_failed", body.Code)
		assert.Equal(t, 422, body.Status)
		assert.Equal(t, problem.TypeBase+"validation_failed", body.Type)
		assert.Equal(t, map[string]string{"body.items[0].quantity": "number must be at most 3"}, body.Fields)
		assert.NotEmpty(t, body.RequestID)
	})
	t.Run("a refused request is never stored under its key", func(t *testing.T) {
		// D-100 D3: validation runs before idempotency. The same key with the
		// corrected body is a fresh request, not a replay or a mismatch.
		key := ulid[:25] + "T"
		req := plainReq(http.MethodPost, ordersP, `{"items":[{"sku":"a","quantity":9}]}`)
		req.Header.Set("Idempotency-Key", key)
		rec, _ := do(r, req)
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

		req = plainReq(http.MethodPost, ordersP, validJS)
		req.Header.Set("Idempotency-Key", key)
		rec, body := do(r, req)
		assert.Equal(t, http.StatusCreated, rec.Code, body.Code)
		assert.Empty(t, rec.Header().Get(idempotency.HeaderReplayed))
	})
	t.Run("wrong content type is 415", func(t *testing.T) {
		req := plainReq(http.MethodPost, ordersP, validJS)
		req.Header.Set("Idempotency-Key", ulid[:25]+"X") // a fresh key: the same key replays
		req.Header.Set("Content-Type", "text/plain")
		rec, body := do(r, req)
		assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
		assert.Equal(t, "unsupported_media_type", body.Code)
	})
	t.Run("oversized by Content-Length is 413 before validation", func(t *testing.T) {
		req := plainReq(http.MethodPost, ordersP, strings.Repeat(" ", mw.DefaultBodyLimit+1)+validJS)
		req.Header.Set("Idempotency-Key", ulid[:25]+"Y")
		rec, body := do(r, req)
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		assert.Equal(t, "payload_too_large", body.Code)
	})
	t.Run("oversized chunked body is 413", func(t *testing.T) {
		// Idempotency is optional here and no key is sent, so the validator is
		// the first reader of the body.
		big := strings.Repeat(" ", mw.DefaultBodyLimit+1) + `{"status":"confirmed"}`
		req := httptest.NewRequest(http.MethodPatch, orderP, io.NopCloser(strings.NewReader(big))) // unknown length
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"1"`)
		rec, body := do(r, req)
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		assert.Equal(t, "payload_too_large", body.Code)
	})
	t.Run("the 1 MB amplification body is 400 through the pipeline", func(t *testing.T) {
		req := plainReq(http.MethodPost, ordersP, ampBody())
		req.Header.Set("Idempotency-Key", ulid[:25]+"Z")
		rec, body := do(r, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "bad_request", body.Code)
		assert.Empty(t, body.Fields)
	})
	t.Run("x-upload lifts the limit from the contract", func(t *testing.T) {
		csv := strings.Repeat("sku,qty\n", (mw.DefaultBodyLimit+1)/8)
		req := httptest.NewRequest(http.MethodPost, importP, strings.NewReader(csv))
		req.Header.Set("Content-Type", "text/csv")
		rec, body := do(r, req)
		assert.Equal(t, http.StatusAccepted, rec.Code, body.Code)
		assert.Equal(t, csv, string(*got))
	})
	t.Run("unknown route is chi's 404", func(t *testing.T) {
		rec, body := do(r, plainReq(http.MethodGet, "/v1/me/nothing", ""))
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, "not_found", body.Code)
	})
	t.Run("undeclared method is chi's 405 with Allow", func(t *testing.T) {
		for _, tc := range []struct{ method, path, allow string }{
			{http.MethodDelete, ordersP, "GET, POST"},
			{http.MethodPut, ordersP, "GET, POST"},
			{http.MethodPost, orderP, "GET, PATCH"},
			{http.MethodDelete, orderP, "GET, PATCH"},
		} {
			rec, body := do(r, plainReq(tc.method, tc.path, ""))
			assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, tc.method+" "+tc.path)
			assert.Equal(t, "method_not_allowed", body.Code)
			assert.Equal(t, tc.allow, rec.Header().Get("Allow"), tc.method+" "+tc.path)
		}
	})
	t.Run("nil validator passes everything", func(t *testing.T) {
		plain := httpx.NewRouter(httpx.Options{Authenticator: stubAuthenticator{}})
		plain.Post("/v1/me/orders", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
		req := plainReq(http.MethodPost, ordersP, `{"bogus":true}`)
		req.Header.Set("Authorization", "Bearer customer") // slot 11 (W1-01) refuses an anonymous /v1/me
		rec, _ := do(plain, req)
		assert.Equal(t, http.StatusCreated, rec.Code)
	})
}

// TestEncodedSlashCannotSplitRouters is the security review's H-1 proof as
// a regression test: a path parameter carrying an encoded slash plus an
// invalid escape made chi and kin-openapi route differently, and the
// validator passed what chi then served to another handler. Now one router
// decides (D-100 D2) and the path guard (slot 8) refuses the request before
// routing. The handler is never reached.
func TestEncodedSlashCannotSplitRouters(t *testing.T) {
	v := newValidator(t)
	r, got := newPipeline(t, v)
	for _, raw := range []string{
		ordersP + `/a%2F"`,
		ordersP + "/a%2F\xc3\xa9",
		ordersP + "/a%2f%22",
		ordersP + "/a%2F%22",
		ordersP + `/a%5Cb`,
		ordersP + "/" + ulid + "%2Fcatalogue",
	} {
		*got = nil
		rec, body := do(r, plainReq(http.MethodGet, raw, ""))
		assert.Equal(t, http.StatusBadRequest, rec.Code, raw)
		assert.Equal(t, "bad_request", body.Code, raw)
		assert.Nil(t, *got, "the handler ran for %q", raw)
	}
	// A plain parameter still routes and validates.
	rec, body := do(r, plainReq(http.MethodGet, orderP, ""))
	assert.Equal(t, http.StatusOK, rec.Code, body.Code)
}

// TestAnonymousBizThroughPipeline (D-100 D3): without a principal the
// pipeline answers 401 under /v1/biz before naming a field.
func TestAnonymousBizThroughPipeline(t *testing.T) {
	v := newValidator(t)
	r := httpx.NewRouter(httpx.Options{Validator: v, Policies: v})
	r.Post("/v1/biz/businesses/{bid}/catalogue/imports", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	req := plainReq(http.MethodPost, importP, `{"bogus":1}`)
	rec, body := do(r, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "unauthorized", body.Code)
	assert.Empty(t, body.Fields)
}

// TestOpenAPI31Keywords proves the built-in visitor (see New) enforces the
// 3.1 keywords the contract uses.
func TestOpenAPI31Keywords(t *testing.T) {
	const s31 = `
openapi: 3.1.0
info: {title: test, version: "1"}
paths:
  /v1/me/things:
    post:
      operationId: createThing
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: false
              required: [kind, label, amount]
              properties:
                kind: {const: thing}
                label: {type: [string, "null"], maxLength: 5}
                amount: {type: number, exclusiveMinimum: 0}
                extra:
                  oneOf:
                    - {type: integer}
                    - {type: string, pattern: "^[a-z]+$"}
      responses:
        "201": {description: created}
`
	v, err := openapi.New([]byte(s31))
	require.NoError(t, err)
	post := func(body string) error { return v.ValidateRequest(jsonReq(http.MethodPost, "/v1/me/things", body)) }

	assert.NoError(t, post(`{"kind":"thing","label":null,"amount":0.5}`))
	assert.NoError(t, post(`{"kind":"thing","label":"ab","amount":1,"extra":"abc"}`))

	_, _, f := fields(t, post(`{"kind":"other","label":"ab","amount":1}`))
	assert.Contains(t, f, "body.kind", "const")
	_, _, f = fields(t, post(`{"kind":"thing","label":12,"amount":1}`))
	assert.Equal(t, map[string]string{"body.label": "value must be one of string, null"}, f, "type array")
	_, _, f = fields(t, post(`{"kind":"thing","label":"toolong","amount":1}`))
	assert.Equal(t, map[string]string{"body.label": "maximum string length is 5"}, f)
	_, _, f = fields(t, post(`{"kind":"thing","label":null,"amount":0}`))
	assert.Equal(t, map[string]string{"body.amount": "number must be more than 0"}, f, "numeric exclusiveMinimum")
	_, _, f = fields(t, post(`{"kind":"thing","label":null,"amount":1,"extra":"ABC"}`))
	assert.Equal(t, map[string]string{"body.extra": "must match exactly one of the allowed shapes"}, f)
}

func TestErrorIsProblem(t *testing.T) {
	v := newValidator(t)
	err := v.ValidateRequest(jsonReq(http.MethodPost, ordersP, `{}`))
	assert.True(t, errors.Is(err, problem.New("validation_failed", "")))
	assert.False(t, bytes.Contains([]byte(err.Error()), []byte("schema")), "no schema dump in the error: %s", err)
}

// TestStrictOptions (D-100 M2): the options every generated Register uses
// answer problem+json on both of oapi-codegen's error paths, never its
// plain-text error that echoes the request.
func TestStrictOptions(t *testing.T) {
	o := openapi.StrictOptions()
	require.NotNil(t, o.RequestErrorHandlerFunc)
	require.NotNil(t, o.ResponseErrorHandlerFunc)

	req := httptest.NewRequest(http.MethodPost, ordersP, nil)
	rec := httptest.NewRecorder()
	o.RequestErrorHandlerFunc(rec, req, errors.New("can't bind body: SECRET-9876"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, problem.ContentType, rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), `"code":"bad_request"`)
	assert.NotContains(t, rec.Body.String(), "SECRET-9876")

	rec = httptest.NewRecorder()
	o.ResponseErrorHandlerFunc(rec, req, problem.New("not_found", ""))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"not_found"`)

	rec = httptest.NewRecorder()
	o.ResponseErrorHandlerFunc(rec, req, errors.New("db down: SECRET-9876"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"internal"`)
	assert.NotContains(t, rec.Body.String(), "SECRET-9876")
}

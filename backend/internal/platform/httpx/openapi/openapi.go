// Package openapi validates requests against the bundled contract
// (api/openapi.yaml, embedded by make gen as gen/api/spec) and reads each
// operation's route policy from it (W0-04; 04 §1.4, §1.6; D-100).
//
// The validator is slot 13 of the pipeline (mw.Validate), inline on every
// registered route, after chi has picked the route: one router decides. It
// finds the operation by method and chi's route pattern and takes the path
// parameters chi extracted, so a path can never match one route for chi and
// another for the contract. A path chi does not know never reaches it (chi
// answers 404, or 405 with Allow); a mounted route the contract does not
// know fails closed with 500 internal, and httpx.Router.Check refuses to
// start the API with one.
//
// A request the contract refuses gets 422 validation_failed with one entry
// per problem in Fields ("body.items[2].quantity": "required"; at most
// MaxFields, then extensions.fieldsTruncated), 415 unsupported_media_type for
// the wrong Content-Type, 400 bad_request for a body that is not JSON, has
// trailing data, more than MaxValues values or an array past its maxItems
// (the body is refused before any schema work, security review H-2), 413
// payload_too_large past the body limit, and 401 unauthorized for an
// anonymous caller under /v1/me, /v1/biz or /v1/admin before any field is
// named. Reasons never repeat the client's value. A JSON body is buffered
// (at most the body limit, 1 MB), pre-scanned for the value budget and
// maxItems, then validated. A request body may be application/json only,
// unless the operation is x-upload: true, and then it may not be JSON at
// all; New refuses any other contract at startup. An upload body is checked
// for its media type and size only; the handler streams it (no buffering).
package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/go-chi/chi/v5"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/httpx/openapi/schemacheck"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/httpx/route"
	"github.com/todayz/backend/internal/platform/reqctx"
)

const (
	// MaxFields caps the Fields of one validation_failed; past it the body
	// carries extensions.fieldsTruncated = true (D-100).
	MaxFields = 50
	// MaxValues caps the JSON values (keys, scalars and containers) in one
	// body; past it the body is refused before any schema work (H-2).
	MaxValues = 10000

	jsonMediaType = "application/json"
)

// authPrefixes are the route prefixes an anonymous caller is refused on
// before validation (D-100; W1-01 replaces the prefix list with the
// operation's security requirement).
var authPrefixes = []string{"/v1/me", "/v1/biz", "/v1/admin"}

type operation struct {
	pattern string
	method  string
	item    *openapi3.PathItem
	op      *openapi3.Operation
}

// Validator checks requests against one OpenAPI document. It implements
// mw.Validator and route.Source.
type Validator struct {
	doc      *openapi3.T
	opts     *openapi3filter.Options
	ops      map[string]*operation   // "METHOD /path/{param}"
	policies map[string]route.Policy // "METHOD /path/{param}" → the operation's x-idempotency and x-upload
}

// New parses and validates spec (YAML or JSON). It refuses a document whose
// schemas use a keyword the validator does not enforce (schemacheck) or an
// x-idempotency other than none, optional or required.
func New(spec []byte) (*Validator, error) {
	if err := schemacheck.Forbidden(spec); err != nil {
		return nil, fmt.Errorf("openapi: %w", err)
	}
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		return nil, fmt.Errorf("openapi: %w", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("openapi: %w", err)
	}
	// kin-openapi validates the bodies and parameters of a 3.1 document with
	// a JSON Schema 2020-12 library: it re-compiles the schema on every
	// request and reports one string per failure that quotes the client's
	// value ("maximum: got 9, want 3"). Marking this in-memory copy 3.0 after
	// validation makes it use its built-in visitor instead: compiled once,
	// one structured SchemaError per failure (field, JSON pointer, reason),
	// and it honours the 3.1 keywords the contract uses (type arrays with
	// "null", const, numeric exclusiveMinimum/Maximum; TestOpenAPI31Keywords).
	doc.OpenAPI = "3.0.3"
	doc.Servers = nil
	registerDecoder()
	v := &Validator{
		doc: doc,
		opts: &openapi3filter.Options{
			MultiError: true,
			// Authentication is slot 11 (authn) and per-route authz, not the contract.
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			// The idempotency hash is over the raw bytes (04 §1.6): never rewrite the body.
			SkipSettingDefaults: true,
		},
		ops:      map[string]*operation{},
		policies: map[string]route.Policy{},
	}
	for p, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			key := method + " " + p
			v.ops[key] = &operation{pattern: p, method: method, item: item, op: op}
			pol, has := route.Policy{}, false
			switch x := op.Extensions["x-idempotency"]; x {
			case nil:
			case "none":
				has = true
			case "optional":
				pol.Idempotency, has = route.IdempotencyOptional, true
			case "required":
				pol.Idempotency, has = route.IdempotencyRequired, true
			default:
				return nil, fmt.Errorf("openapi: %s %s: x-idempotency must be none, optional or required", method, p)
			}
			up, _ := op.Extensions["x-upload"].(bool)
			if up {
				pol.Upload, has = true, true
			}
			if err := checkMediaTypes(op, up); err != nil {
				return nil, fmt.Errorf("openapi: %s %s: %w", method, p, err)
			}
			if has {
				v.policies[key] = pol
			}
		}
	}
	return v, nil
}

// checkMediaTypes applies the request media-type rule (D-100 round 2, M-A):
// an operation's request body is application/json only, unless the operation
// is x-upload: true, and then it is anything but JSON. A contract that
// breaks the rule never starts, so every JSON body goes through decodeJSON
// and every upload body is streamed; nothing is half-validated.
func checkMediaTypes(op *openapi3.Operation, upload bool) error {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return nil
	}
	for ct := range op.RequestBody.Value.Content {
		isJSON := ct == jsonMediaType
		switch {
		case upload && isJSON:
			return fmt.Errorf("an x-upload operation may not take %s (D-100)", jsonMediaType)
		case !upload && !isJSON:
			return fmt.Errorf("request body %s needs x-upload: true; a non-upload body is %s only (D-100)", ct, jsonMediaType)
		}
	}
	return nil
}

// Has reports whether the contract defines method + pattern (a chi route
// pattern, which uses the same {param} syntax as OpenAPI).
func (v *Validator) Has(method, pattern string) bool {
	_, ok := v.ops[method+" "+pattern]
	return ok
}

// Operations lists every "METHOD /path/{param}" in the contract, sorted.
func (v *Validator) Operations() []string {
	out := make([]string, 0, len(v.ops))
	for k := range v.ops {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Policy implements route.Source from the operation's x-idempotency and
// x-upload (04 §1.6). ok is false when the operation declares neither.
func (v *Validator) Policy(method, pattern string) (route.Policy, bool) {
	p, ok := v.policies[method+" "+pattern]
	return p, ok
}

// ValidateRequest implements mw.Validator. It needs chi's route context: the
// route pattern names the operation and chi's parameters are the path
// parameters (D-100).
func (v *Validator) ValidateRequest(r *http.Request) error {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return problem.Wrap(genproblems.Internal, errors.New("openapi: no chi route context: the validator runs inline after chi has routed"))
	}
	pattern := rctx.RoutePattern()
	op := v.ops[r.Method+" "+pattern]
	if op == nil {
		// Mounted but not in the contract: fail closed (Router.Check refuses this at start-up).
		return problem.Wrap(genproblems.Internal, fmt.Errorf("openapi: %s %s is mounted but not in the contract", r.Method, pattern))
	}
	if needsPrincipal(pattern) && reqctx.PrincipalFrom(r.Context()).IsAnonymous() {
		return problem.New(genproblems.Unauthorized, "")
	}
	params, err := pathParams(rctx)
	if err != nil {
		return problem.New(genproblems.BadRequest, "The request path is not valid")
	}
	if unknown := undeclaredQuery(r, op.item, op.op); unknown != nil {
		return unknown
	}
	opts := *v.opts
	if whole := body(r, op.op, &opts); whole != nil {
		return whole
	}
	in := &openapi3filter.RequestValidationInput{
		Request:    r,
		PathParams: params,
		Route:      &routers.Route{Spec: v.doc, Path: op.pattern, PathItem: op.item, Method: op.method, Operation: op.op},
		Options:    &opts,
	}
	if err := openapi3filter.ValidateRequest(r.Context(), in); err != nil {
		return toProblem(err)
	}
	return nil
}

// undeclaredQuery refuses a query name the operation does not declare, as an
// unknown body property is refused; kin-openapi lets one through. A name
// outside reToken is echoed as <redacted> (D-100 L4).
func undeclaredQuery(r *http.Request, item *openapi3.PathItem, op *openapi3.Operation) error {
	query := r.URL.Query()
	if len(query) == 0 {
		return nil
	}
	declared := map[string]bool{}
	for _, params := range []openapi3.Parameters{item.Parameters, op.Parameters} {
		for _, p := range params {
			if p.Value != nil && p.Value.In == openapi3.ParameterInQuery {
				declared[p.Value.Name] = true
			}
		}
	}
	names := make([]string, 0, len(query))
	for name := range query {
		if !declared[name] {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	p := problem.New(genproblems.ValidationFailed, "")
	for _, name := range names {
		if !reToken.MatchString(name) {
			name = "<redacted>"
		}
		addField(p, "query."+name, "not allowed")
	}
	return p
}

func needsPrincipal(pattern string) bool {
	for _, p := range authPrefixes {
		if pattern == p || strings.HasPrefix(pattern, p+"/") {
			return true
		}
	}
	return false
}

// pathParams takes chi's parameters, decoded. The path guard (slot 8) has
// refused an encoded slash or backslash already, so decoding is safe.
func pathParams(rctx *chi.Context) (map[string]string, error) {
	out := map[string]string{}
	for i, k := range rctx.URLParams.Keys {
		if k == "" || k == "*" || i >= len(rctx.URLParams.Values) {
			continue
		}
		val, err := url.PathUnescape(rctx.URLParams.Values[i])
		if err != nil {
			return nil, err
		}
		out[k] = val
	}
	return out, nil
}

// body applies the Content-Type rules before kin-openapi sees the request:
// an operation without a body skips body validation; an empty body on a
// required one is "body: required"; a missing, malformed or undeclared
// Content-Type is 415; a declared media type other than JSON is accepted on
// its media type alone and streamed to the handler (D-100 M3, L10).
func body(r *http.Request, op *openapi3.Operation, opts *openapi3filter.Options) error {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		opts.ExcludeRequestBody = true
		return nil
	}
	rb := op.RequestBody.Value
	hasBody := r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0
	if !hasBody {
		if rb.Required {
			return problem.New(genproblems.ValidationFailed, "").WithField("body", "required")
		}
		opts.ExcludeRequestBody = true
		return nil
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return problem.New(genproblems.UnsupportedMediaType, "")
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return problem.New(genproblems.UnsupportedMediaType, "")
	}
	if len(rb.Content) == 0 {
		opts.ExcludeRequestBody = true
		return nil
	}
	if rb.Content.Get(mt) == nil {
		return problem.New(genproblems.UnsupportedMediaType, "")
	}
	if mt != jsonMediaType {
		opts.ExcludeRequestBody = true
	}
	return nil
}

// toProblem maps kin-openapi's errors to the 04 §1.4 problem shape.
func toProblem(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return problem.New(genproblems.PayloadTooLarge, "")
	}
	p := problem.New(genproblems.ValidationFailed, "")
	var multi openapi3.MultiError
	errs := []error{err}
	if errors.As(err, &multi) {
		errs = multi
	}
	for _, e := range errs {
		var re *openapi3filter.RequestError
		if !errors.As(e, &re) {
			return problem.Wrap(genproblems.ValidationFailed, e)
		}
		if whole := requestError(p, re); whole != nil {
			return whole
		}
	}
	return p
}

// requestError adds re's fields to p, or returns a problem that replaces p
// (415 for the wrong Content-Type, 400 bad_request for a body that is not
// JSON or past a limit, 413 past the body limit).
func requestError(p *problem.Error, re *openapi3filter.RequestError) error {
	switch {
	case re.Parameter != nil:
		field := re.Parameter.In + "." + re.Parameter.Name
		var se *openapi3.SchemaError
		switch {
		case errors.Is(re.Err, openapi3filter.ErrInvalidRequired):
			addField(p, field, "required")
		case errors.As(re.Err, &se):
			addField(p, field, schemaReason(se))
		default:
			addField(p, field, "invalid")
		}
	case re.RequestBody != nil:
		var pe *openapi3filter.ParseError
		var le *limitError
		switch {
		case re.Err == nil && strings.HasPrefix(re.Reason, "header Content-Type"):
			return problem.New(genproblems.UnsupportedMediaType, "")
		case errors.Is(re.Err, openapi3filter.ErrInvalidRequired):
			addField(p, "body", "required")
		case errors.As(re.Err, &le):
			return problem.New(genproblems.BadRequest, le.detail)
		case errors.As(re.Err, &pe):
			var mbe *http.MaxBytesError
			switch {
			case errors.As(pe.RootCause(), &mbe):
				return problem.New(genproblems.PayloadTooLarge, "")
			case pe.Kind == openapi3filter.KindUnsupportedFormat:
				// The contract allows a media type kin-openapi cannot decode.
				return problem.New(genproblems.UnsupportedMediaType, "")
			}
			return problem.New(genproblems.BadRequest, "The request body is not valid JSON")
		default:
			schemaErrors(p, re.Err)
		}
	default:
		addField(p, "request", "invalid")
	}
	return nil
}

// addField adds one field reason, up to MaxFields; past that the problem
// says fieldsTruncated instead of growing.
func addField(p *problem.Error, path, reason string) {
	if _, dup := p.Fields[path]; !dup && len(p.Fields) >= MaxFields {
		_ = p.WithExtension("fieldsTruncated", true)
		return
	}
	_ = p.WithField(path, reason)
}

// schemaErrors adds one field per schema error in err (a SchemaError or a
// MultiError of them).
func schemaErrors(p *problem.Error, err error) {
	// The concrete type, not errors.As: a oneOf SchemaError wraps the
	// MultiError of its branches, which is detail the client does not need.
	if multi, ok := err.(openapi3.MultiError); ok { //nolint:errorlint // concrete type on purpose, see above
		for _, e := range multi {
			schemaErrors(p, e)
		}
		return
	}
	var se *openapi3.SchemaError
	if errors.As(err, &se) {
		path := se.JSONPointer()
		// `property "x" is missing|unsupported`: the pointer may already end in x.
		name := quoted(se.Reason)
		if n := len(path); n > 0 && path[n-1] == name {
			name = ""
		}
		switch se.SchemaField {
		case "required":
			addField(p, fieldPath(path, name), "required")
		case "properties":
			addField(p, fieldPath(path, name), "not allowed")
		default:
			addField(p, fieldPath(path, ""), schemaReason(se))
		}
		return
	}
	addField(p, "body", "invalid")
}

var reQuoted = regexp.MustCompile(`"([^"]*)"`)

// quoted returns the first double-quoted token in s (the property name in
// `property "x" is missing`).
func quoted(s string) string {
	if m := reQuoted.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// reToken is what a field path may echo: a client key outside it (an unknown
// property with any other character) is written as <redacted> (D-100 L4).
var reToken = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

// fieldPath renders a JSON pointer as 04 §1.4 does: body.items[2].quantity.
func fieldPath(pointer []string, last string) string {
	var b strings.Builder
	b.WriteString("body")
	for _, tok := range append(pointer, last) {
		switch {
		case tok == "":
		case isIndex(tok):
			b.WriteString("[" + tok + "]")
		case reToken.MatchString(tok):
			b.WriteString("." + tok)
		default:
			b.WriteString(".<redacted>")
		}
	}
	return b.String()
}

func isIndex(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// schemaReason is the short reason for a schema error. It never contains the
// value the client sent: the regular expression and format rules are
// summarised, and any other reason that echoes the value says "invalid".
func schemaReason(se *openapi3.SchemaError) string {
	var reason string
	switch se.SchemaField {
	case "pattern":
		reason = "invalid format"
	case "format":
		if f := quoted(se.Reason); f != "" {
			reason = "not a valid " + f
		} else {
			reason = "invalid format"
		}
	case "oneOf":
		reason = "must match exactly one of the allowed shapes"
	case "anyOf":
		reason = "must match one of the allowed shapes"
	case "allOf":
		reason = "must match all of the allowed shapes"
	case "enum":
		reason = "not one of the allowed values"
	case "required":
		reason = "required"
	case "properties":
		reason = "not allowed"
	default:
		reason = se.Reason
	}
	if s, ok := se.Value.(string); ok && s != "" && strings.Contains(reason, s) {
		reason = "invalid"
	}
	return reason
}

// --- JSON body decoder (H-2) -------------------------------------------------

var decoderOnce sync.Once

// registerDecoder replaces kin-openapi's application/json decoder, process
// wide, with one that refuses an oversized body before building it.
func registerDecoder() {
	decoderOnce.Do(func() { openapi3filter.RegisterBodyDecoder(jsonMediaType, decodeJSON) })
}

// limitError is a body refused for its size, before schema validation.
type limitError struct{ detail string }

func (e *limitError) Error() string { return e.detail }

var errTrailing = errors.New("trailing data after the JSON value")

// decodeJSON counts the body's values first (a token scan that allocates
// almost nothing) and refuses past MaxValues; then decodes, refuses trailing
// data, and refuses an array longer than the schema's maxItems before the
// schema visitor sees it. Without it a 1 MB body of empty objects made the
// visitor allocate hundreds of megabytes (security review H-2).
func decodeJSON(body io.Reader, _ http.Header, schema *openapi3.SchemaRef, _ openapi3filter.EncodingFn) (any, error) {
	rs, ok := body.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(body)
		if err != nil {
			return nil, &openapi3filter.ParseError{Kind: openapi3filter.KindOther, Cause: err}
		}
		rs = bytes.NewReader(data)
	}
	dec := json.NewDecoder(rs)
	n := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, &openapi3filter.ParseError{Kind: openapi3filter.KindInvalidFormat, Cause: err}
		}
		if d, isDelim := tok.(json.Delim); isDelim && (d == '}' || d == ']') {
			continue
		}
		if n++; n > MaxValues {
			return nil, &limitError{detail: "The request body has too many values"}
		}
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, &openapi3filter.ParseError{Kind: openapi3filter.KindOther, Cause: err}
	}
	dec = json.NewDecoder(rs)
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, &openapi3filter.ParseError{Kind: openapi3filter.KindInvalidFormat, Cause: err}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, &openapi3filter.ParseError{Kind: openapi3filter.KindInvalidFormat, Cause: errTrailing}
	}
	if err := checkMaxItems(schema, value); err != nil {
		return nil, err
	}
	return value, nil
}

// checkMaxItems walks value beside schema (properties, items,
// additionalProperties, allOf, oneOf and anyOf; the lint makes every request
// array carry maxItems) and refuses the first array past its maxItems. Under
// oneOf and anyOf the value is refused when no branch admits it: a branch
// with a larger maxItems may still match, and schema validation decides
// which one.
func checkMaxItems(s *openapi3.SchemaRef, value any) error {
	if s == nil || s.Value == nil {
		return nil
	}
	sv := s.Value
	switch x := value.(type) {
	case []any:
		if sv.MaxItems != nil && uint64(len(x)) > *sv.MaxItems {
			return &limitError{detail: "The request body has an array longer than the contract allows"}
		}
		for _, e := range x {
			if err := checkMaxItems(sv.Items, e); err != nil {
				return err
			}
		}
	case map[string]any:
		for k, e := range x {
			child := sv.Properties[k]
			if child == nil {
				child = sv.AdditionalProperties.Schema
			}
			if err := checkMaxItems(child, e); err != nil {
				return err
			}
		}
	}
	for _, a := range sv.AllOf {
		if err := checkMaxItems(a, value); err != nil {
			return err
		}
	}
	for _, branches := range [][]*openapi3.SchemaRef{sv.OneOf, sv.AnyOf} {
		if err := checkBranches(branches, value); err != nil {
			return err
		}
	}
	return nil
}

// checkBranches returns the first branch's error when every branch of a
// oneOf or anyOf whose type fits value refuses it, and nil when one admits
// it. A branch of another type (an object branch beside an array value) is
// not consulted: it cannot admit the value, and whether some branch matches
// at all is schema validation's business, not the pre-scan's.
func checkBranches(branches []*openapi3.SchemaRef, value any) error {
	var first error
	for _, b := range branches {
		if b == nil || b.Value == nil || !typeFits(b.Value, value) {
			continue
		}
		err := checkMaxItems(b, value)
		if err == nil {
			return nil
		}
		if first == nil {
			first = err
		}
	}
	return first
}

// typeFits reports whether s's declared type admits an array or object value
// (no type admits everything).
func typeFits(s *openapi3.Schema, value any) bool {
	if s.Type == nil || len(*s.Type) == 0 {
		return true
	}
	switch value.(type) {
	case []any:
		return s.Type.Includes(openapi3.TypeArray)
	case map[string]any:
		return s.Type.Includes(openapi3.TypeObject)
	}
	return true
}

// --- oapi-codegen strict server options (D-100 M2) --------------------------

// StrictHTTPServerOptions mirrors oapi-codegen's StrictHTTPServerOptions
// field for field, so a generated package converts it without importing
// anything: StrictHTTPServerOptions(openapi.StrictOptions()).
type StrictHTTPServerOptions struct {
	RequestErrorHandlerFunc  func(w http.ResponseWriter, r *http.Request, err error)
	ResponseErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}

// StrictOptions returns the options every generated Register uses: both
// error paths answer problem+json, never oapi-codegen's plain-text error that
// echoes the request.
func StrictOptions() StrictHTTPServerOptions {
	return StrictHTTPServerOptions{RequestErrorHandlerFunc: StrictRequestError, ResponseErrorHandlerFunc: StrictResponseError}
}

// StrictRequestError answers a request oapi-codegen could not bind (a
// parameter or body it could not decode after the validator passed it) with
// 400 bad_request and a generic detail; the cause stays in the log.
func StrictRequestError(w http.ResponseWriter, r *http.Request, err error) {
	problem.Write(w, r, problem.Wrap(genproblems.BadRequest, err))
}

// StrictResponseError answers an error a strict handler returned: a
// *problem.Error as itself, anything else as 500 internal.
func StrictResponseError(w http.ResponseWriter, r *http.Request, err error) {
	problem.Write(w, r, err)
}

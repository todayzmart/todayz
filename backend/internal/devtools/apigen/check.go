package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/todayz/backend/internal/platform/httpx/problem/registry"
)

// namespaces are the only path prefixes under /v1 (04 §1.1, SC-PL-01), with
// the short name the counts are reported under (04 §2 "endpoint count").
var namespaces = []struct{ Prefix, Name string }{
	{"/v1/discover", "discover"},
	{"/v1/me", "me"},
	{"/v1/biz/businesses/{bid}", "biz"},
	{"/v1/admin", "admin"},
	{"/v1/public", "public"},
	{"/v1/auth", "auth"},
	{"/v1/rum", "telemetry"},
	{"/v1/events", "telemetry"},
	{"/v1/client-errors", "telemetry"},
	{"/v1/test", "test"},
}

// exactPaths are accepted as themselves although no namespace prefix covers
// them: the business list sits at the root of the biz namespace (D-100 M1).
var exactPaths = map[string]string{"/v1/biz/businesses": "biz"}

// healthPaths are the only paths outside /v1 (SC-PL-01).
var healthPaths = map[string]bool{"/healthz": true, "/readyz": true, "/version": true}

// ulidPrefixes are where ULID path parameters may appear (04 §1.2).
var ulidPrefixes = []string{"/v1/me", "/v1/biz", "/v1/admin"}

// idempotencyPrefixes are where every unsafe operation states x-idempotency
// explicitly, `none` included, so a missing key policy is a decision, not an
// oversight (04 §1.6, D-100).
var idempotencyPrefixes = ulidPrefixes

const (
	extIdempotency = "x-idempotency"
	extMoney       = "x-money"
	extUpload      = "x-upload"
	extProblems    = "x-problems"
	extOpenObject  = "x-open-object"
	idempotencyKey = "Idempotency-Key"
	problemJSON    = "application/problem+json"
	jsonMediaType  = "application/json"
)

// moneyOperations are the operationIds of the operations 04 §1.6 lists as
// moving money, credits or stock under a required Idempotency-Key: order
// create, every order transition (customer, shop, admin), confirm-return,
// code verify, POS sale, POS reverse, sync, bill-claim confirm, counter-code
// verify, credit adjustment request and approve, payout mark-paid, job
// run-now (D-95) and every AI draft confirm. An operation in the contract
// with one of these ids must carry `x-money: true` (and so `x-idempotency:
// required`); an id not in the contract yet is fine (D-100 round 2, Low 4).
// The ids follow 04 §1.3's <verb><Resource> convention; a domain WP that
// names its operation differently renames it here in the same change, with
// a line in 04 §1.6.
var moneyOperations = []string{
	// order create and the customer transitions (04 §2 me)
	"createOrder", "cancelOrder", "confirmPartialOrder",
	// shop transitions (04 §2 biz)
	"acceptOrder", "rejectOrder", "readyOrder", "dispatchOrder", "failOrderDelivery",
	"confirmOrderReturn", "cancelStoreOrder", "verifyOrderCode",
	// admin transition
	"cancelAdminOrder",
	// POS and sync
	"syncStore", "createPosSale", "reversePosSale",
	// bill claims and counter codes
	"confirmBillClaim", "verifyCounterCode",
	// credits and payouts
	"createCreditAdjustment", "approveCreditAdjustment", "markPayoutPaid",
	// jobs (D-95)
	"runJob",
	// AI draft confirms (11): shop, customer, admin
	"confirmVoiceStockDraft", "confirmSupplierBillDraft", "confirmShelfPhotoDraft",
	"confirmReviewReplyDraft", "confirmBannerCopyDraft",
	"confirmListSearchDraft", "confirmVoiceSearchDraft",
	"confirmCatalogueCleanupDraft", "confirmTicketTriageDraft", "confirmRiskSummaryDraft",
}

// isMoneyOperation reports whether id is in moneyOperations.
func isMoneyOperation(id string) bool {
	for _, m := range moneyOperations {
		if m == id {
			return true
		}
	}
	return false
}

// isUnsafe reports whether method changes state (04 §1.6).
func isUnsafe(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

// loadDoc loads the bundle (or the unbundled root document: kin-openapi follows
// file references, which is how the fixtures run without Redocly) and
// validates it with its examples.
func loadDoc(ctx context.Context, path string) (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := doc.Validate(ctx, openapi3.EnableExamplesValidation()); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

// counts is the number of method+path pairs per namespace.
type counts map[string]int

func (c counts) total() int {
	n := 0
	for k, v := range c {
		if k != "health" {
			n += v
		}
	}
	return n
}

// lintDoc runs every TodayZ lint on a loaded document. reg is the problem code
// registry (api/problems).
func lintDoc(doc *openapi3.T, reg *registry.Registry) (counts, error) {
	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	ulidPattern, slugPattern := componentPattern(doc, "Ulid"), componentPattern(doc, "Slug")
	if ulidPattern == "" || slugPattern == "" {
		fail("components.schemas must define Ulid and Slug with a pattern (schemas/common.yaml)")
	}

	paths := sortedKeys(doc.Paths.Map())
	errs = append(errs, lintPathShapes(paths)...)
	c := counts{}
	for _, p := range paths {
		item := doc.Paths.Value(p)
		ns := namespaceOf(p)
		if ns == "" {
			fail("%s: not under a namespace (04 §1.1: %s) and not /healthz, /readyz or /version", p, namespaceList())
		}
		for _, method := range sortedKeys(item.Operations()) {
			op := item.Operations()[method]
			c[ns]++
			where := method + " " + p
			params := append(append(openapi3.Parameters{}, item.Parameters...), op.Parameters...)

			// Path parameters: ULIDs only under /v1/me, /v1/biz, /v1/admin; only
			// {slug} under /v1/discover/stores/.
			for _, pr := range params {
				if pr.Value == nil || pr.Value.In != openapi3.ParameterInPath || pr.Value.Schema == nil || pr.Value.Schema.Value == nil {
					continue
				}
				pat := pr.Value.Schema.Value.Pattern
				if pat == ulidPattern && !hasAnyPrefix(p, ulidPrefixes) {
					fail("%s: path parameter {%s} is a ULID; ids appear only under %s (04 §1.2)", where, pr.Value.Name, strings.Join(ulidPrefixes, ", "))
				}
				if strings.HasPrefix(p, "/v1/discover/stores/") && (pr.Value.Name != "slug" || pat != slugPattern) {
					fail("%s: paths under /v1/discover/stores/ take only {slug} (schemas/common.yaml#/Slug) (04 §1.2)", where)
				}
			}

			// Every operation ends in the Problem response.
			if !hasProblemDefault(doc, op) {
				fail("%s: needs `default: {$ref: ../common/responses.yaml#/Problem}` (04 §1.4)", where)
			}

			// Examples on every request body and every 2xx response. A request
			// body is application/json only, unless the operation is x-upload
			// and then it is anything but JSON (D-100 round 2, M-A): the
			// validator decodes every JSON body and streams every upload.
			isUpload, _ := op.Extensions[extUpload].(bool)
			if rb := op.RequestBody; rb != nil && rb.Value != nil {
				for _, ct := range sortedKeys(rb.Value.Content) {
					mt := rb.Value.Content[ct]
					switch {
					case isUpload && ct == jsonMediaType:
						fail("%s: an `%s: true` operation may not take %s (D-100)", where, extUpload, jsonMediaType)
					case !isUpload && ct != jsonMediaType:
						fail("%s: request body %s needs `%s: true`; a non-upload body is %s only (D-100)", where, ct, extUpload, jsonMediaType)
					}
					if !hasExample(mt) {
						fail("%s: request body %s needs an example (04 §1.4; the mocks are generated from it)", where, ct)
					}
					errs = append(errs, lintClosedObjects(mt.Schema, where+": request body "+ct, map[*openapi3.Schema]bool{})...)
					errs = append(errs, lintBounds(mt.Schema, where+": request body "+ct, "an inline schema", map[*openapi3.Schema]bool{})...)
				}
			}
			for _, code := range sortedKeys(op.Responses.Map()) {
				if len(code) != 3 || code[0] != '2' {
					continue
				}
				r := op.Responses.Map()[code]
				if r.Value == nil {
					continue
				}
				for _, ct := range sortedKeys(r.Value.Content) {
					if !hasExample(r.Value.Content[ct]) {
						fail("%s: response %s %s needs an example", where, code, ct)
					}
				}
			}

			// x-idempotency ⇔ the Idempotency-Key header (04 §1.6); `none` says
			// the route ignores the key and takes no parameter (D-100).
			mode, hasMode := op.Extensions[extIdempotency]
			modeStr, _ := mode.(string)
			hasKey := false
			for _, pr := range params {
				if pr.Value != nil && pr.Value.In == openapi3.ParameterInHeader && strings.EqualFold(pr.Value.Name, idempotencyKey) {
					hasKey = true
				}
			}
			switch {
			case hasMode:
				switch modeStr {
				case "required", "optional", "none":
				default:
					fail("%s: %s must be none, optional or required, got %v", where, extIdempotency, mode)
				}
				if !isUnsafe(method) {
					fail("%s: %s is only for POST, PUT, PATCH and DELETE", where, extIdempotency)
				}
				switch {
				case modeStr == "none" && hasKey:
					fail("%s: `%s: none` ignores the key, so drop the Idempotency-Key parameter (04 §1.6)", where, extIdempotency)
				case modeStr != "none" && !hasKey:
					fail("%s: %s needs the parameter `$ref: ../common/parameters.yaml#/IdempotencyKey`", where, extIdempotency)
				}
			case hasKey:
				fail("%s: takes Idempotency-Key, so say `%s: required` (or optional)", where, extIdempotency)
			case isUnsafe(method) && hasAnyPrefix(p, idempotencyPrefixes):
				fail("%s: an unsafe operation under %s states `%s: required`, `optional` or `none` explicitly (04 §1.6, D-100)", where, strings.Join(idempotencyPrefixes, ", "), extIdempotency)
			}
			// x-money marks an operation that moves money or credits: the key is never optional.
			money, hasMoney := op.Extensions[extMoney]
			if hasMoney {
				if b, _ := money.(bool); !b {
					fail("%s: %s must be true when present", where, extMoney)
				} else if modeStr != "required" {
					fail("%s: `%s: true` needs `%s: required` (04 §1.6, D-100)", where, extMoney, extIdempotency)
				}
			}
			if isMoneyOperation(op.OperationID) && !hasMoney {
				fail("%s: operationId %s is on the 04 §1.6 money list (apigen moneyOperations), so it needs `%s: true` (D-100)", where, op.OperationID, extMoney)
			}
			if up, ok := op.Extensions[extUpload]; ok {
				if b, _ := up.(bool); !b {
					fail("%s: %s must be true when present", where, extUpload)
				} else if method != "POST" && method != "PUT" {
					fail("%s: %s is only for POST and PUT", where, extUpload)
				}
			}

			// x-problems: every code registered (SC-PL-02).
			if raw, ok := op.Extensions[extProblems]; ok {
				list, isList := raw.([]any)
				if !isList || len(list) == 0 {
					fail("%s: %s must be a non-empty list of codes", where, extProblems)
				}
				for _, v := range list {
					code, _ := v.(string)
					if _, ok := reg.Lookup(code); !ok {
						fail("%s: %s lists %q, which is not in api/problems/ (SC-PL-02)", where, extProblems, code)
					}
				}
			}
		}
	}
	return c, errors.Join(errs...)
}

// lintPathShapes applies 04 §1.2 to the set of paths: no literal beside a
// parameter under the same parent, one parameter name per position.
func lintPathShapes(paths []string) []error {
	var errs []error
	type child struct{ params, literals []string }
	children := map[string]*child{}
	for _, p := range paths {
		if len(p) > 1 && strings.HasSuffix(p, "/") {
			// chi would mount `/v1/x/` as a different route from `/v1/x`, and
			// the validator looks operations up by chi's pattern (D-100 round 2, Low 5).
			errs = append(errs, fmt.Errorf("%s: a path may not end in `/` (04 §1.2)", p))
			continue
		}
		segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
		for i, s := range segs {
			parent := "/" + strings.Join(segs[:i], "/")
			c := children[parent]
			if c == nil {
				c = &child{}
				children[parent] = c
			}
			if strings.HasPrefix(s, "{") {
				c.params = appendUnique(c.params, s)
			} else {
				c.literals = appendUnique(c.literals, s)
			}
		}
	}
	for _, parent := range sortedKeys(children) {
		c := children[parent]
		if len(c.params) > 1 {
			errs = append(errs, fmt.Errorf("under %s: one parameter name per position, got %s (04 §1.2)", parent, strings.Join(c.params, " and ")))
		}
		if len(c.params) > 0 && len(c.literals) > 0 {
			errs = append(errs, fmt.Errorf("under %s: literal %s sits beside %s; move it to its own resource (04 §1.2)", parent, strings.Join(c.literals, ", "), c.params[0]))
		}
	}
	return errs
}

func appendUnique(l []string, s string) []string {
	for _, x := range l {
		if x == s {
			return l
		}
	}
	return append(l, s)
}

// lintClosedObjects fails on an object schema in a request body that accepts
// unknown fields: it needs `additionalProperties: false` or `x-open-object: true`.
func lintClosedObjects(ref *openapi3.SchemaRef, where string, seen map[*openapi3.Schema]bool) []error {
	if ref == nil || ref.Value == nil || seen[ref.Value] {
		return nil
	}
	s := ref.Value
	seen[s] = true
	var errs []error
	// Includes, not Is: a nullable field is `type: [object, "null"]` (D-100 round 2, M-C).
	isObject := s.Type.Includes(openapi3.TypeObject) || len(s.Properties) > 0
	if isObject {
		open, _ := s.Extensions[extOpenObject].(bool)
		closed := s.AdditionalProperties.Has != nil && !*s.AdditionalProperties.Has
		name := ref.Ref
		if name == "" {
			name = "an inline object"
		}
		switch {
		case !open && !closed:
			errs = append(errs, fmt.Errorf("%s: %s accepts unknown fields; add `additionalProperties: false` or `%s: true` (04 §1.4)", where, name, extOpenObject))
		case open && len(s.Properties) > 0:
			// A declared shape that also takes anything would let unknown
			// fields ride beside validated ones (D-100, security review L-3).
			errs = append(errs, fmt.Errorf("%s: %s declares properties, so `%s: true` is not allowed; say `additionalProperties: false` (D-100)", where, name, extOpenObject))
		}
	}
	for _, name := range sortedKeys(s.Properties) {
		errs = append(errs, lintClosedObjects(s.Properties[name], where, seen)...)
	}
	for _, sub := range [][]*openapi3.SchemaRef{s.AllOf, s.OneOf, s.AnyOf} {
		for _, r := range sub {
			errs = append(errs, lintClosedObjects(r, where, seen)...)
		}
	}
	errs = append(errs, lintClosedObjects(s.Items, where, seen)...)
	errs = append(errs, lintClosedObjects(s.AdditionalProperties.Schema, where, seen)...)
	return errs
}

// lintBounds (D-100, security review H-2) fails on a request body schema
// whose size the validator could not cap before decoding: an array without
// maxItems, or a string without maxLength that no enum, const, binary format
// or length-bounding pattern limits. at names the schema in the message.
func lintBounds(ref *openapi3.SchemaRef, where, at string, seen map[*openapi3.Schema]bool) []error {
	if ref == nil || ref.Value == nil || seen[ref.Value] {
		return nil
	}
	s := ref.Value
	seen[s] = true
	if ref.Ref != "" {
		at = ref.Ref
	}
	var errs []error
	// Includes, not Is: `type: [array, "null"]` is still an array (D-100 round 2, M-C).
	if s.Type.Includes(openapi3.TypeArray) && s.MaxItems == nil {
		errs = append(errs, fmt.Errorf("%s: %s: an array in a request body needs maxItems (D-100)", where, at))
	}
	if s.Type.Includes(openapi3.TypeString) && s.MaxLength == nil && !boundedString(s) {
		errs = append(errs, fmt.Errorf("%s: %s: a string in a request body needs maxLength, an enum, a const, `format: binary` or a pattern that bounds its length (D-100)", where, at))
	}
	for _, name := range sortedKeys(s.Properties) {
		errs = append(errs, lintBounds(s.Properties[name], where, at+"."+name, seen)...)
	}
	for _, sub := range []struct {
		kw   string
		refs []*openapi3.SchemaRef
	}{{"allOf", s.AllOf}, {"oneOf", s.OneOf}, {"anyOf", s.AnyOf}} {
		for i, r := range sub.refs {
			errs = append(errs, lintBounds(r, where, fmt.Sprintf("%s.%s[%d]", at, sub.kw, i), seen)...)
		}
	}
	errs = append(errs, lintBounds(s.Items, where, at+".items", seen)...)
	errs = append(errs, lintBounds(s.AdditionalProperties.Schema, where, at+".additionalProperties", seen)...)
	return errs
}

// boundedString reports whether something other than maxLength caps the
// string: an enum or const (fixed values), format binary (the body limit
// applies) or a pattern whose every repetition is counted.
func boundedString(s *openapi3.Schema) bool {
	return s.Format == "binary" || len(s.Enum) > 0 || s.Const != nil || boundedPattern(s.Pattern)
}

// boundedPattern reports whether pat is anchored at both ends and has no
// open-ended repetition (`*`, `+`, `{n,}`), so it bounds the string's length.
// A top-level alternation outside a group (`^a|b.*$`) is refused: the anchors
// then belong to the outer branches only, so the pattern is not anchored as
// a whole. A final `\$` is an escaped dollar, not an anchor (D-100 round 2,
// Low 6).
func boundedPattern(pat string) bool {
	if len(pat) < 2 || pat[0] != '^' || pat[len(pat)-1] != '$' || escapedEnd(pat) {
		return false
	}
	inClass, depth := false, 0
	for i := 0; i < len(pat); i++ {
		c := pat[i]
		switch {
		case c == '\\':
			i++ // the escaped character is a literal
		case inClass:
			inClass = c != ']'
		case c == '[':
			inClass = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == '|' && depth == 0:
			return false
		case c == '*' || c == '+':
			return false
		case c == '{':
			j := strings.IndexByte(pat[i:], '}')
			if j < 0 || strings.HasSuffix(pat[i+1:i+j], ",") {
				return false
			}
			i += j
		}
	}
	return true
}

// escapedEnd reports whether pat's last character is escaped: an odd run of
// backslashes precedes it, so `\$` is a literal dollar and `\\$` is not.
func escapedEnd(pat string) bool {
	n := 0
	for i := len(pat) - 2; i >= 0 && pat[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

func hasExample(mt *openapi3.MediaType) bool {
	return mt != nil && (mt.Example != nil || len(mt.Examples) > 0)
}

// hasProblemDefault reports whether the operation's default response is the
// shared Problem response: problem+json whose schema is components Problem.
func hasProblemDefault(doc *openapi3.T, op *openapi3.Operation) bool {
	if op.Responses == nil {
		return false
	}
	def := op.Responses.Default()
	if def == nil || def.Value == nil {
		return false
	}
	mt := def.Value.Content[problemJSON]
	if mt == nil || mt.Schema == nil || mt.Schema.Value == nil {
		return false
	}
	if strings.HasSuffix(mt.Schema.Ref, "/Problem") {
		return true
	}
	problem := doc.Components.Schemas["Problem"]
	return problem != nil && problem.Value == mt.Schema.Value
}

func componentPattern(doc *openapi3.T, name string) string {
	if doc.Components == nil {
		return ""
	}
	if s := doc.Components.Schemas[name]; s != nil && s.Value != nil {
		return s.Value.Pattern
	}
	return ""
}

func namespaceOf(p string) string {
	if healthPaths[p] {
		return "health"
	}
	if ns, ok := exactPaths[p]; ok {
		return ns
	}
	for _, ns := range namespaces {
		if p == ns.Prefix || strings.HasPrefix(p, ns.Prefix+"/") {
			return ns.Name
		}
	}
	return ""
}

func namespaceList() string {
	l := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		l = append(l, ns.Prefix)
	}
	return strings.Join(l, ", ")
}

func hasAnyPrefix(p string, prefixes []string) bool {
	for _, pre := range prefixes {
		if p == pre || strings.HasPrefix(p, pre+"/") {
			return true
		}
	}
	return false
}

// runCheck loads the bundle, lints it and prints the counts; expect > 0 also
// fails unless the /v1 total matches (04 §2: 294 once every WP has landed).
func runCheck(api, bundle string, expect int, w io.Writer) error {
	reg, err := registry.Load(os.DirFS(filepath.Join(api, "problems")))
	if err != nil {
		return err
	}
	doc, err := loadDoc(context.Background(), bundle)
	if err != nil {
		return err
	}
	c, err := lintDoc(doc, reg)
	if err != nil {
		return err
	}
	names := sortedKeys(c)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s %d", n, c[n]))
	}
	fmt.Fprintf(w, "apigen check: %d method+path pairs under /v1 (%s)\n", c.total(), strings.Join(parts, ", "))
	if expect > 0 && c.total() != expect {
		return fmt.Errorf("expected %d method+path pairs under /v1, found %d (04 §2)", expect, c.total())
	}
	return nil
}

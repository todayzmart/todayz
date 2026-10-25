// Package schema builds the JSON Schemas of model tools and checks what the model sends back
// (11 §1 rules 4 and 5).
//
// Rules this package holds:
//   - A tool schema is strict: every object is closed (additionalProperties false) and lists
//     every property as required; an optional value is a nullable type, never a missing key.
//   - Decode reads the tool input into the feature's Go type with unknown fields refused, then
//     runs the type's Validate. A model output that fails either is rejected, never repaired.
//   - Grounded (ground.go) rejects free text with a number the backend input did not contain.
package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// S is one JSON Schema node.
type S = map[string]any

// Strict tool schemas (strict: true) take no numeric, length or count bounds, so the builders
// state the bound in the description, where the model reads it, and the decoded type's
// Validate enforces it (Decode runs it).

// String is a string the model should keep to maxLen characters.
func String(desc string, maxLen int) S {
	return S{"type": "string", "description": bound(desc, fmt.Sprintf("at most %d characters", maxLen))}
}

// Enum is a string from values.
func Enum(desc string, values ...string) S {
	return S{"type": "string", "description": desc, "enum": values}
}

// Integer is an integer in [minV, maxV].
func Integer(desc string, minV, maxV int64) S {
	return S{"type": "integer", "description": bound(desc, fmt.Sprintf("from %d to %d", minV, maxV))}
}

// Number is a number in [minV, maxV].
func Number(desc string, minV, maxV float64) S {
	return S{"type": "number", "description": bound(desc, fmt.Sprintf("from %g to %g", minV, maxV))}
}

func bound(desc, b string) string {
	if desc == "" {
		return b
	}
	return desc + " (" + b + ")"
}

// Bool is a boolean.
func Bool(desc string) S { return S{"type": "boolean", "description": desc} }

// Nullable lets s also be null (strict schemas have no optional keys).
func Nullable(s S) S {
	out := S{}
	for k, v := range s {
		out[k] = v
	}
	switch t := s["type"].(type) {
	case string:
		out["type"] = []string{t, "null"}
	case []string:
		out["type"] = append(append([]string(nil), t...), "null")
	}
	if e, ok := s["enum"].([]string); ok {
		vals := make([]any, 0, len(e)+1)
		for _, v := range e {
			vals = append(vals, v)
		}
		out["enum"] = append(vals, nil)
	}
	return out
}

// Array is a list of items with at most maxItems entries.
func Array(desc string, items S, maxItems int) S {
	return S{"type": "array", "description": bound(desc, fmt.Sprintf("at most %d items", maxItems)), "items": items}
}

// Object is a closed object; every property is required.
func Object(desc string, props map[string]S) S {
	req := make([]string, 0, len(props))
	p := make(map[string]any, len(props))
	for k, v := range props {
		req = append(req, k)
		p[k] = v
	}
	sort.Strings(req)
	o := S{"type": "object", "properties": p, "required": req, "additionalProperties": false}
	if desc != "" {
		o["description"] = desc
	}
	return o
}

// ErrInvalid wraps every Decode failure; the feature counts it as client.RejectSchema.
var ErrInvalid = errors.New("schema: model output is invalid")

// Validator is a decoded tool input that checks its own rules (ranges, ids from the candidate
// list, cross-field rules).
type Validator interface {
	Validate() error
}

// Decode reads raw (the tool input) into out, refusing unknown fields, trailing data and a
// missing input, then runs out.Validate when out has one.
func Decode(raw json.RawMessage, out any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("%w: empty", ErrInvalid)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if dec.More() {
		return fmt.Errorf("%w: trailing data", ErrInvalid)
	}
	if v, ok := out.(Validator); ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalid, err)
		}
	}
	return nil
}

// Package schemacheck refuses the JSON Schema keywords the request validator
// does not enforce (D-100, security review M-2). kin-openapi's built-in
// visitor, which the validator uses for its structured errors, ignores the
// 2019-09 and 2020-12 conditional and unevaluated keywords; a contract that
// relied on one would look stricter than the API is. openapi.New and apigen
// both call it, so such a schema never reaches a running validator.
package schemacheck

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Banned lists the keywords the validator does not enforce.
var Banned = []string{
	"if", "then", "else",
	"dependentRequired", "dependentSchemas",
	"contains", "minContains", "maxContains",
	"patternProperties", "propertyNames",
	"unevaluatedProperties", "unevaluatedItems",
	"prefixItems",
	// D-100 round 2, Low 7: dynamic references resolve against the
	// evaluation path, which kin-openapi does not model, and contentSchema
	// describes the decoded content, which it does not decode.
	"$dynamicRef", "$dynamicAnchor", "contentSchema",
}

var banned = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range Banned {
		m[k] = true
	}
	return m
}()

// named lists the members whose children are names (paths, media types,
// property or component names, status codes), not keywords.
var named = map[string]bool{
	"properties": true, "schemas": true, "parameters": true, "responses": true,
	"content": true, "headers": true, "links": true, "callbacks": true,
	"securitySchemes": true, "requestBodies": true, "encoding": true,
	"paths": true, "mapping": true, "variables": true, "definitions": true,
	"$defs": true, "webhooks": true,
}

// opaque lists the members whose subtree is data, not schema.
var opaque = map[string]bool{
	"example": true, "examples": true, "default": true, "const": true,
	"enum": true, "description": true, "summary": true, "required": true,
	"security": true, "tags": true, "info": true, "servers": true,
}

// Forbidden parses doc (YAML or JSON) and returns an error naming the first
// banned keyword and the path it sits at.
func Forbidden(doc []byte) error {
	var v any
	if err := yaml.Unmarshal(doc, &v); err != nil {
		return fmt.Errorf("schemacheck: %w", err)
	}
	return Check("", v)
}

// Check walks v (maps and slices as the YAML or JSON decoder produced them)
// and returns an error naming the first banned keyword under path.
func Check(path string, v any) error {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if banned[k] {
				return fmt.Errorf("schemacheck: %s: keyword %q is not allowed: the request validator does not enforce it (D-100)", join(path, k), k)
			}
			if opaque[k] || strings.HasPrefix(k, "x-") {
				continue
			}
			if named[k] {
				if children, ok := x[k].(map[string]any); ok {
					for _, name := range sortedKeys(children) {
						if err := Check(join(join(path, k), name), children[name]); err != nil {
							return err
						}
					}
					continue
				}
			}
			if err := Check(join(path, k), x[k]); err != nil {
				return err
			}
		}
	case []any:
		for i, e := range x {
			if err := Check(path+"["+strconv.Itoa(i)+"]", e); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func join(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}

package schemacheck

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForbiddenNamesTheKeywordAndPath(t *testing.T) {
	for _, kw := range Banned {
		doc := "openapi: 3.1.0\ncomponents:\n  schemas:\n    Thing:\n      type: object\n      " + kw + ": {}\n"
		err := Forbidden([]byte(doc))
		require.Error(t, err, kw)
		require.Contains(t, err.Error(), "components.schemas.Thing."+kw)
		require.Contains(t, err.Error(), `"`+kw+`"`)
	}
	// Nested under a property and inside an array branch.
	err := Forbidden([]byte("components:\n  schemas:\n    T:\n      properties:\n        a:\n          oneOf:\n            - type: string\n            - contains: {}\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "components.schemas.T.properties.a.oneOf[1].contains")
}

func TestForbiddenIgnoresNamesAndData(t *testing.T) {
	doc := strings.Join([]string{
		"openapi: 3.1.0",
		"info: {title: t, version: '1', description: 'if then else'}",
		"paths:",
		"  /v1/if: {get: {operationId: if, responses: {'200': {description: ok}}}}",
		"components:",
		"  schemas:",
		"    if: {type: object, properties: {then: {type: string, enum: [contains, else]}, contains: {type: integer, default: 3, example: 4}}}",
		"    prefixItems: {type: string, x-contains: {if: 1}}",
		"  parameters:",
		"    then: {name: then, in: query, schema: {type: string}}",
		"  responses:",
		"    else: {description: no}",
		"  examples:",
		"    e: {value: {if: 1, then: 2}}",
	}, "\n")
	require.NoError(t, Forbidden([]byte(doc)))
	require.Error(t, Forbidden([]byte("not: [yaml")))
}

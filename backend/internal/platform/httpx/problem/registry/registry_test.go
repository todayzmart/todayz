package registry

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// The real registry files load and keep the invariants.
func TestRealRegistryLoads(t *testing.T) {
	r, err := Load(os.DirFS(filepath.Join("..", "..", "..", "..", "..", "api", "problems")))
	require.NoError(t, err)
	e, ok := r.Lookup("idempotency_mismatch")
	require.True(t, ok)
	require.Equal(t, 422, e.Status, "04 §1.6: same key + different body is 422")
	e, ok = r.Lookup("invalid_transition")
	require.True(t, ok)
	require.Equal(t, 409, e.Status)
	require.Equal(t, "problems.invalid_transition", e.I18n)
	require.Contains(t, r.Codes(), "internal")
}

const commonOK = `
domain: common
problems:
  - {code: internal, status: 500, i18n: problems.internal, title: x}
  - {code: invalid_transition, status: 409, i18n: problems.invalid_transition, title: x}
  - {code: not_found, status: 404, i18n: problems.not_found, title: x}
  - {code: validation_failed, status: 422, i18n: problems.validation_failed, title: x}
  - {code: idempotency_mismatch, status: 422, i18n: problems.idempotency_mismatch, title: x}
`

func TestValidation(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"duplicate across files": {
			"common.yaml": {Data: []byte(commonOK)},
			"orders.yaml": {Data: []byte("domain: orders\nproblems:\n  - {code: not_found, status: 404, i18n: problems.not_found, title: x}\n")},
		},
		"domain must match file": {
			"common.yaml": {Data: []byte(commonOK)},
			"orders.yaml": {Data: []byte("domain: pos\nproblems:\n  - {code: a_b, status: 409, i18n: problems.a_b, title: x}\n")},
		},
		"bad status": {
			"common.yaml": {Data: []byte(commonOK)},
			"orders.yaml": {Data: []byte("domain: orders\nproblems:\n  - {code: a_b, status: 200, i18n: problems.a_b, title: x}\n")},
		},
		"bad code": {
			"common.yaml": {Data: []byte(commonOK)},
			"orders.yaml": {Data: []byte("domain: orders\nproblems:\n  - {code: NotSnake, status: 409, i18n: problems.NotSnake, title: x}\n")},
		},
		"bad i18n": {
			"common.yaml": {Data: []byte(commonOK)},
			"orders.yaml": {Data: []byte("domain: orders\nproblems:\n  - {code: a_b, status: 409, i18n: orders.a_b, title: x}\n")},
		},
		"unknown field": {
			"common.yaml": {Data: []byte(commonOK)},
			"orders.yaml": {Data: []byte("domain: orders\nproblems:\n  - {code: a_b, status: 409, i18n: problems.a_b, title: x, http: 1}\n")},
		},
		"missing title": {
			"common.yaml": {Data: []byte(commonOK)},
			"orders.yaml": {Data: []byte("domain: orders\nproblems:\n  - {code: a_b, status: 409, i18n: problems.a_b}\n")},
		},
		"invalid_transition outside common": {
			"common.yaml": {Data: []byte("domain: common\nproblems:\n  - {code: internal, status: 500, i18n: problems.internal, title: x}\n")},
			"orders.yaml": {Data: []byte("domain: orders\nproblems:\n  - {code: invalid_transition, status: 409, i18n: problems.invalid_transition, title: x}\n")},
		},
		"no files": {},
	}
	for name, fsys := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(fsys)
			require.Error(t, err)
		})
	}

	r, err := Load(fstest.MapFS{
		"common.yaml": {Data: []byte(commonOK)},
		"orders.yaml": {Data: []byte("domain: orders\nproblems:\n  - {code: insufficient_stock, status: 409, i18n: problems.insufficient_stock, title: x}\n")},
	})
	require.NoError(t, err)
	e, ok := r.Lookup("insufficient_stock")
	require.True(t, ok)
	require.Equal(t, "orders", e.Domain)
}

func TestGoName(t *testing.T) {
	require.Equal(t, "IdempotencyMismatch", GoName("idempotency_mismatch"))
	require.Equal(t, "NotFound", GoName("not_found"))
	require.Equal(t, "Internal", GoName("internal"))
}

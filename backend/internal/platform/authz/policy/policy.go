// Package policy holds the permission tables, one file per domain
// (<domain>.go, owned by that domain's WP, D-50). A file registers one table
// from init():
//
//	func init() {
//		Register(authz.Table{Domain: "orders", Actions: []authz.Action{
//			{Name: "order.accept", Kind: authz.KindBusiness,
//				Grants: map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow, authz.Cashier: authz.Allow},
//				Doc: "accept an order (02 §3.3)"},
//		}})
//	}
//
// and commits testdata/<domain>.golden, which TestGolden writes with
// `go test ./internal/platform/authz/policy -update` and then compares on
// every run: each (role × action) pair is one reviewed line (AU-14). The
// API wires authz.New(policy.All(), …); docs/generated/permissions.md is
// produced from the same tables.
package policy

import (
	"fmt"
	"sort"
	"sync"

	"github.com/todayz/backend/internal/platform/authz"
)

var (
	mu     sync.Mutex
	tables = map[string]authz.Table{}
)

// Register adds a domain's table. Registering a domain twice panics (at
// init, so the binary never starts with it).
func Register(t authz.Table) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := tables[t.Domain]; dup {
		panic(fmt.Sprintf("policy: domain %q registered twice", t.Domain))
	}
	tables[t.Domain] = t
}

// All returns every registered table, sorted by domain.
func All() []authz.Table {
	mu.Lock()
	defer mu.Unlock()
	out := make([]authz.Table, 0, len(tables))
	for _, t := range tables {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out
}

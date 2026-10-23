// Package authz is the permission check (02 §3.3, D-38, D-50, D-64…D-66): a
// policy engine over per-domain tables of actions, enforced per operation
// (middleware slot 15) and readable by handlers for the conditional grants.
//
// Stable interface (W1-01 phase A; every domain builds on it):
//
//   - Grant, Role, Kind, Action, Table: the policy file format. Each domain
//     writes internal/platform/authz/policy/<domain>.go, which registers one
//     Table from init() with policy.Register; policy.All() returns them all.
//   - Engine (New, Check, Authorize) and Decision / DecisionFrom.
//   - StaffResolver: business membership, implemented by W1-04.
//   - Strict: the strict-server middleware a domain passes to its generated
//     Register; mw.Authz for a hand-mounted route.
//
// The golden harness (policy/golden_test.go) prints every (role × action)
// pair of every table and compares it with policy/testdata/<domain>.golden
// (AU-14); `go test ./internal/platform/authz/policy -update` rewrites them.
package authz

import (
	"fmt"
	"regexp"
	"strings"
)

// Grant is one cell of the matrix. The order is strength: when an admin has
// several roles, the strongest grant of any role wins.
type Grant uint8

// The grants of 02 §3.3. Every grant but Allow and Deny is conditional: the
// engine lets the call through and the handler applies the condition, read
// from DecisionFrom(ctx).Grant.
const (
	Deny      Grant = iota // –
	Own                    // own: only rows the caller created (field agent)
	OwnWindow              // own, window: own, and only inside field_agent_edit_window_days (D-39)
	Money                  // money: only `money`-class rows (finance on jobs, D-95)
	NotOwn                 // not own: never a row the caller created (two-person rule, D-73)
	Masked                 // masked: sensitive fields masked to the last 4 characters
	Allow                  // ✓
)

var grantNames = [...]string{"-", "own", "own,window", "money", "not-own", "masked", "allow"}

// String is the golden-file spelling.
func (g Grant) String() string {
	if int(g) < len(grantNames) {
		return grantNames[g]
	}
	return fmt.Sprintf("grant(%d)", g)
}

// Conditional reports whether the handler must apply a condition.
func (g Grant) Conditional() bool { return g != Allow && g != Deny }

// Role is a business role (per business, from the staff row) or an admin role
// (D-64, from admin_users).
type Role string

// Business roles.
const (
	Owner   Role = "owner"
	Manager Role = "manager"
	Cashier Role = "cashier"
)

// Admin roles (D-64).
const (
	FieldAgent Role = "field_agent"
	Support    Role = "support"
	Catalogue  Role = "catalogue"
	Analyst    Role = "analyst"
	Finance    Role = "finance"
	Admin      Role = "admin"
	SuperAdmin Role = "super_admin"
)

// BusinessRoles and AdminRoles are the matrix columns, in the plan's order.
var (
	BusinessRoles = []Role{Owner, Manager, Cashier}
	AdminRoles    = []Role{FieldAgent, Support, Catalogue, Analyst, Finance, Admin, SuperAdmin}
)

// IsAdminRole reports whether r is one of the D-64 roles.
func IsAdminRole(r string) bool {
	for _, a := range AdminRoles {
		if string(a) == r {
			return true
		}
	}
	return false
}

// Kind is who an action is for.
type Kind uint8

// The kinds.
const (
	// KindCustomer: a signed-in customer-app user, on their own resources (/v1/me).
	KindCustomer Kind = iota + 1
	// KindAny: any signed-in user of any app (GET /v1/me).
	KindAny
	// KindBusinessUser: any signed-in business-app user, no business scope
	// (creating a business, listing my businesses).
	KindBusinessUser
	// KindBusiness: a staff member of the scope's business, by role (Grants
	// over BusinessRoles), with the store in their store_ids.
	KindBusiness
	// KindAdmin: an admin with TOTP, by role (Grants over AdminRoles).
	KindAdmin
)

var kindNames = map[Kind]string{KindCustomer: "customer", KindAny: "any", KindBusinessUser: "business-user", KindBusiness: "business", KindAdmin: "admin"}

func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return fmt.Sprintf("kind(%d)", k)
}

// Action is one row of the matrix.
type Action struct {
	// Name is the permission key, `<area>.<verb>` (e.g. `order.accept`,
	// `admin.settings.edit`), unique across all tables.
	Name string
	Kind Kind
	// Grants is the row for KindBusiness (keys from BusinessRoles) or
	// KindAdmin (keys from AdminRoles). A missing role is Deny. Empty for the
	// other kinds.
	Grants map[Role]Grant
	// StepUp marks a † action: the admin's TOTP must be younger than
	// authn.StepUpWindow, else 403 step_up_required. KindAdmin only.
	StepUp bool
	// Doc is the "Used by" column.
	Doc string
}

// Table is one domain's policy file.
type Table struct {
	Domain  string
	Actions []Action
}

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// validate checks one action against the format rules.
func (a Action) validate() error {
	if !nameRE.MatchString(a.Name) {
		return fmt.Errorf("action %q: name must be dotted snake_case", a.Name)
	}
	var cols []Role
	switch a.Kind {
	case KindCustomer, KindAny, KindBusinessUser:
		if len(a.Grants) > 0 {
			return fmt.Errorf("action %s: kind %s takes no grants", a.Name, a.Kind)
		}
	case KindBusiness:
		cols = BusinessRoles
	case KindAdmin:
		cols = AdminRoles
	default:
		return fmt.Errorf("action %s: unknown kind", a.Name)
	}
	if a.StepUp && a.Kind != KindAdmin {
		return fmt.Errorf("action %s: step-up is for admin actions only", a.Name)
	}
	for r, g := range a.Grants {
		if !contains(cols, r) {
			return fmt.Errorf("action %s: role %q is not a %s role", a.Name, r, a.Kind)
		}
		if g > Allow {
			return fmt.Errorf("action %s: role %s: unknown grant", a.Name, r)
		}
	}
	if a.Kind == KindBusiness || a.Kind == KindAdmin {
		any := false
		for _, g := range a.Grants {
			any = any || g != Deny
		}
		if !any {
			return fmt.Errorf("action %s: no role is granted", a.Name)
		}
	}
	return nil
}

func contains(rs []Role, r Role) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

// Golden renders a table as the golden text: one line per (action × role)
// pair, plus the kind and step-up line of each action. Stable order: the
// table's action order, then the plan's role order.
func Golden(t Table) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# domain %s: one line per (role x action); regenerate with -update\n", t.Domain)
	for _, a := range t.Actions {
		fmt.Fprintf(&b, "%s kind=%s step-up=%t\n", a.Name, a.Kind, a.StepUp)
		var cols []Role
		switch a.Kind {
		case KindBusiness:
			cols = BusinessRoles
		case KindAdmin:
			cols = AdminRoles
		}
		for _, r := range cols {
			fmt.Fprintf(&b, "%s %s %s\n", a.Name, r, a.Grants[r])
		}
	}
	return b.String()
}

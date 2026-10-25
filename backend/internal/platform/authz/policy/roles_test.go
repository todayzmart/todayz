package policy

import (
	"strings"
	"testing"

	"github.com/todayz/backend/internal/platform/authz"
)

// TestAdminRoleMatrix is AD-01 (08-testing.md) on the registered tables: the admin role rules of
// D-64 written out from the plan, so a policy file that drifts fails here as well as in its golden.
// Areas whose tables have not landed yet (payouts, credits adjust, risk, field stats) are matched by
// prefix and start being checked the day their actions are registered. Each route's 403 comes from
// its handler's authz.Authorize call, which the domain's own handler tests exercise.
func TestAdminRoleMatrix(t *testing.T) {
	admin := map[string]authz.Action{}
	for _, tbl := range All() {
		for _, a := range tbl.Actions {
			if a.Kind == authz.KindAdmin {
				admin[a.Name] = a
			}
		}
	}
	grant := func(name string, r authz.Role) authz.Grant {
		t.Helper()
		a, ok := admin[name]
		if !ok {
			t.Fatalf("admin action %s is not registered", name)
		}
		return a.Grants[r]
	}
	only := func(name string, want ...authz.Role) {
		t.Helper()
		for _, r := range authz.AdminRoles {
			allowed := grant(name, r) != authz.Deny
			if allowed != contains(want, r) {
				t.Errorf("%s: %s allowed=%t, want only %v", name, r, allowed, want)
			}
		}
	}

	t.Run("catalogue works the review queue and nothing on money, orders or settings", func(t *testing.T) {
		for _, name := range []string{"admin.catalogue.view", "admin.catalogue.review"} {
			if g := grant(name, "catalogue"); g != authz.Allow {
				t.Errorf("%s: catalogue = %s, want allow", name, g)
			}
		}
		for name, a := range admin {
			if hasAnyPrefix(name, "admin.order.", "admin.credit", "admin.payout", "admin.settings.", "admin.risk") && a.Grants["catalogue"] != authz.Deny {
				t.Errorf("%s: catalogue = %s, want -", name, a.Grants["catalogue"])
			}
		}
	})

	t.Run("analyst reads Pulse and writes nothing", func(t *testing.T) {
		if g := grant("admin.pulse.view", "analyst"); g != authz.Allow {
			t.Errorf("admin.pulse.view: analyst = %s, want allow", g)
		}
		for name, a := range admin {
			if a.Grants["analyst"] != authz.Deny && !isRead(name) {
				t.Errorf("%s: analyst = %s, but it is a write", name, a.Grants["analyst"])
			}
		}
	})

	t.Run("Pulse is every role except field_agent", func(t *testing.T) {
		only("admin.pulse.view", "support", "catalogue", "analyst", "finance", "admin", "super_admin")
	})

	t.Run("support tickets are support, admin and super_admin (A-14)", func(t *testing.T) {
		only("admin.support.tickets", "support", "admin", "super_admin")
	})

	t.Run("KYC verify is admin and super_admin only", func(t *testing.T) {
		only("admin.business.verify", "admin", "super_admin")
	})

	t.Run("money settings are super_admin only, behind a fresh code", func(t *testing.T) {
		only("admin.settings.edit_money", "super_admin")
		if !admin["admin.settings.edit_money"].StepUp {
			t.Error("admin.settings.edit_money: step-up is off")
		}
	})

	t.Run("a field agent sees only their own numbers", func(t *testing.T) {
		for name, a := range admin {
			if strings.HasPrefix(name, "admin.field.") {
				if g := a.Grants["field_agent"]; g != authz.Own && g != authz.OwnWindow {
					t.Errorf("%s: field_agent = %s, want own", name, g)
				}
			}
		}
	})
}

// isRead is true for an action whose last segment is a read verb.
func isRead(name string) bool {
	verb := name[strings.LastIndex(name, ".")+1:]
	return verb == "view" || verb == "subject" || verb == "stream"
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func contains(rs []authz.Role, r authz.Role) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

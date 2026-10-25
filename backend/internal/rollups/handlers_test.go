package rollups

import (
	"context"
	"errors"
	"reflect"
	"testing"

	api "github.com/todayz/backend/gen/api/admin-pulse"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
)

// Every admin-pulse operation has an action (a missing one fails closed with
// 500), and both are admin.pulse.view.
func TestOpsCoverEveryOperation(t *testing.T) {
	it := reflect.TypeOf((*api.StrictServerInterface)(nil)).Elem()
	if it.NumMethod() != len(PulseOps) {
		t.Fatalf("%d operations, %d in PulseOps", it.NumMethod(), len(PulseOps))
	}
	for i := 0; i < it.NumMethod(); i++ {
		if PulseOps[it.Method(i).Name] != policy.AdminPulseView {
			t.Errorf("%s → %q, want %s", it.Method(i).Name, PulseOps[it.Method(i).Name], policy.AdminPulseView)
		}
	}
}

// A range outside the enum is a 422 on query.range even if the validator
// were bypassed; the service is never reached.
func TestHandlersRefuseABadRange(t *testing.T) {
	bad := api.AdminPulseRange("1y")
	h := handlers{s: nil}
	_, err1 := h.GetAdminPulse(context.Background(), api.GetAdminPulseRequestObject{Params: api.GetAdminPulseParams{Range: &bad}})
	_, err2 := h.GetAdminPulseMap(context.Background(), api.GetAdminPulseMapRequestObject{Params: api.GetAdminPulseMapParams{Range: &bad}})
	for _, err := range []error{err1, err2} {
		var p *problem.Error
		if !errors.As(err, &p) || p.Code != genproblems.ValidationFailed {
			t.Fatalf("err = %v, want validation_failed", err)
		}
	}
	if checkRange(nil) != nil {
		t.Error("no range means the default, not an error")
	}
}

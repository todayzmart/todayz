package authz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
)

func TestAdminGate(t *testing.T) {
	clk := clock.NewFake(t0)
	g := NewAdminGate(clk)
	as := func(p authn.Principal) context.Context { return authn.WithPrincipal(context.Background(), p) }

	fresh := user(authn.AppAdmin, string(Support))
	fresh.StepUpAt = t0.Add(-time.Minute)

	t.Run("anonymous is 401", func(t *testing.T) {
		_, err := g.Require(context.Background(), []Role{Support}, false)
		require.Equal(t, genproblems.Unauthorized, code(t, err))
		_, err = g.Require(as(authn.Principal{}), []Role{Support}, false)
		require.Equal(t, genproblems.Unauthorized, code(t, err))
	})
	t.Run("other apps are 403", func(t *testing.T) {
		for _, app := range []authn.App{authn.AppCustomer, authn.AppBusiness} {
			_, err := g.Require(as(user(app, string(SuperAdmin))), []Role{Support}, false)
			require.Equal(t, genproblems.Forbidden, code(t, err), app)
		}
	})
	t.Run("an admin without totp is 403", func(t *testing.T) {
		p := fresh
		p.AMR = authn.AMROTP
		_, err := g.Require(as(p), []Role{Support}, false)
		require.Equal(t, genproblems.Forbidden, code(t, err))
	})
	t.Run("role", func(t *testing.T) {
		got, err := g.Require(as(fresh), []Role{Finance, Support}, false)
		require.NoError(t, err)
		require.Equal(t, fresh.UserID, got.UserID)
		require.Equal(t, []string{string(Support)}, got.Roles)

		_, err = g.Require(as(fresh), []Role{Finance}, false)
		require.Equal(t, genproblems.Forbidden, code(t, err))

		none := user(authn.AppAdmin)
		_, err = g.Require(as(none), []Role{Support}, false)
		require.Equal(t, genproblems.Forbidden, code(t, err))

		// A role name the plan does not know grants nothing.
		odd := user(authn.AppAdmin, "owner")
		_, err = g.Require(as(odd), []Role{Support}, false)
		require.Equal(t, genproblems.Forbidden, code(t, err))
	})
	t.Run("super_admin holds every role", func(t *testing.T) {
		sa := user(authn.AppAdmin, string(SuperAdmin))
		for _, r := range AdminRoles {
			_, err := g.Require(as(sa), []Role{r}, false)
			require.NoError(t, err, r)
		}
	})
	t.Run("step-up window", func(t *testing.T) {
		p := fresh
		p.StepUpAt = time.Time{}
		_, err := g.Require(as(p), []Role{Support}, true)
		require.Equal(t, genproblems.StepUpRequired, code(t, err))

		p.StepUpAt = t0.Add(-authn.StepUpWindow)
		_, err = g.Require(as(p), []Role{Support}, true)
		require.NoError(t, err, "exactly at the window is still fresh")

		p.StepUpAt = t0.Add(-authn.StepUpWindow - time.Second)
		_, err = g.Require(as(p), []Role{Support}, true)
		require.Equal(t, genproblems.StepUpRequired, code(t, err))

		// The clock moving is what expires it.
		p.StepUpAt = t0
		_, err = g.Require(as(p), []Role{Support}, true)
		require.NoError(t, err)
		clk.Advance(11 * time.Minute)
		_, err = g.Require(as(p), []Role{Support}, true)
		require.Equal(t, genproblems.StepUpRequired, code(t, err))
		_, err = g.Require(as(p), []Role{Support}, false)
		require.NoError(t, err, "no step-up asked: stale TOTP is fine")
		clk.Set(t0)
	})
	t.Run("role is checked before step-up", func(t *testing.T) {
		p := fresh
		p.StepUpAt = time.Time{}
		_, err := g.Require(as(p), []Role{Finance}, true)
		require.Equal(t, genproblems.Forbidden, code(t, err))
	})
	t.Run("misuse fails closed", func(t *testing.T) {
		_, err := g.Require(as(fresh), nil, false)
		require.Equal(t, genproblems.Internal, code(t, err))
		_, err = g.Require(as(fresh), []Role{Owner}, false)
		require.Equal(t, genproblems.Internal, code(t, err))
		_, err = (&AdminGate{}).Require(as(fresh), []Role{Support}, false)
		require.Equal(t, genproblems.Internal, code(t, err))
		var nilGate *AdminGate
		_, err = nilGate.Require(as(fresh), []Role{Support}, false)
		require.Equal(t, genproblems.Internal, code(t, err))
	})
}

func TestAdminGateMiddleware(t *testing.T) {
	g := NewAdminGate(clock.NewFake(t0))
	roles := []Role{Finance}
	mw := g.Middleware(roles, true)
	roles[0] = Support // the middleware keeps its own copy

	ran := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { ran = true; w.WriteHeader(http.StatusNoContent) }))

	serve := func(p authn.Principal) *httptest.ResponseRecorder {
		ran = false
		r := httptest.NewRequest(http.MethodPost, "/v1/admin/x", nil)
		r = r.WithContext(authn.WithPrincipal(r.Context(), p))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	fin := user(authn.AppAdmin, string(Finance))
	w := serve(fin)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), genproblems.StepUpRequired)
	require.False(t, ran)

	fin.StepUpAt = t0
	w = serve(fin)
	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, ran)

	sup := user(authn.AppAdmin, string(Support))
	sup.StepUpAt = t0
	w = serve(sup)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, ran)

	w = serve(authn.Principal{})
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.False(t, ran)
}

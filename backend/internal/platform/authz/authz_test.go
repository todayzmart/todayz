package authz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

var t0 = time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)

var testTable = Table{Domain: "test", Actions: []Action{
	{Name: "me.view", Kind: KindAny},
	{Name: "cart.edit", Kind: KindCustomer},
	{Name: "business.create", Kind: KindBusinessUser},
	{Name: "order.accept", Kind: KindBusiness, Grants: map[Role]Grant{Owner: Allow, Manager: Allow, Cashier: Allow}},
	{Name: "stock.adjust", Kind: KindBusiness, Grants: map[Role]Grant{Owner: Allow, Manager: Allow}},
	{Name: "admin.users.view", Kind: KindAdmin, Grants: map[Role]Grant{Support: Masked, Admin: Allow, FieldAgent: Own}},
	{Name: "admin.settings.edit", Kind: KindAdmin, StepUp: true, Grants: map[Role]Grant{SuperAdmin: Allow}},
}}

type staff struct {
	members map[ids.ID]Membership // by business
	stores  map[ids.ID]ids.ID     // store → business
	err     error
}

func (s staff) Membership(_ context.Context, _, businessID ids.ID) (Membership, bool, error) {
	if s.err != nil {
		return Membership{}, false, s.err
	}
	m, ok := s.members[businessID]
	return m, ok, nil
}

func (s staff) StoreBusiness(_ context.Context, storeID ids.ID) (ids.ID, bool, error) {
	if s.err != nil {
		return ids.Zero, false, s.err
	}
	b, ok := s.stores[storeID]
	return b, ok, nil
}

func code(t *testing.T, err error) string {
	t.Helper()
	var p *problem.Error
	require.ErrorAs(t, err, &p)
	return p.Code
}

func user(app authn.App, roles ...string) authn.Principal {
	p := authn.Principal{UserID: ids.New(t0), App: app, SessionID: ids.New(t0), AMR: authn.AMROTP, Roles: roles}
	if app == authn.AppAdmin {
		p.AMR = authn.AMROTPTOTP
	}
	return p
}

func engine(t *testing.T, clk clock.Clock, s StaffResolver) *Engine {
	t.Helper()
	e, err := New([]Table{testTable}, Options{Clock: clk, Staff: s})
	require.NoError(t, err)
	return e
}

func TestKinds(t *testing.T) {
	e := engine(t, clock.NewFake(t0), nil)
	ctx := context.Background()
	for _, app := range authn.Apps {
		d, err := e.Check(ctx, user(app), "me.view", Scope{})
		require.NoError(t, err, app)
		require.Equal(t, Allow, d.Grant)
		require.Equal(t, "me.view", d.Action)
	}
	_, err := e.Check(ctx, user(authn.AppCustomer), "cart.edit", Scope{})
	require.NoError(t, err)
	_, err = e.Check(ctx, user(authn.AppBusiness), "cart.edit", Scope{})
	require.Equal(t, genproblems.Forbidden, code(t, err))
	_, err = e.Check(ctx, user(authn.AppBusiness), "business.create", Scope{})
	require.NoError(t, err)
	_, err = e.Check(ctx, user(authn.AppCustomer), "business.create", Scope{})
	require.Equal(t, genproblems.Forbidden, code(t, err))

	_, err = e.Check(ctx, authn.Principal{}, "me.view", Scope{})
	require.Equal(t, genproblems.Unauthorized, code(t, err))
	_, err = e.Check(ctx, user(authn.AppCustomer), "no.such", Scope{})
	require.Equal(t, genproblems.Internal, code(t, err), "an undefined action fails closed")
	require.True(t, e.Has("me.view"))
	require.False(t, e.Has("no.such"))
}

// TestBusinessScope covers AU-09: a staff member of business A never acts on
// business B or on B's store through A's path.
func TestBusinessScope(t *testing.T) {
	bizA, bizB := ids.New(t0), ids.New(t0.Add(time.Second))
	storeA1, storeA2, storeB := ids.New(t0), ids.New(t0.Add(time.Minute)), ids.New(t0.Add(time.Hour))
	s := staff{
		members: map[ids.ID]Membership{bizA: {Role: Cashier, StoreIDs: []ids.ID{storeA1}}},
		stores:  map[ids.ID]ids.ID{storeA1: bizA, storeA2: bizA, storeB: bizB},
	}
	e := engine(t, clock.NewFake(t0), s)
	ctx := context.Background()
	cashier := user(authn.AppBusiness)

	d, err := e.Check(ctx, cashier, "order.accept", Scope{BusinessID: bizA, StoreID: storeA1})
	require.NoError(t, err)
	require.Equal(t, Cashier, d.Role)
	require.Equal(t, Allow, d.Grant)

	for name, sc := range map[string]Scope{
		"other business":           {BusinessID: bizB},
		"other business's store":   {BusinessID: bizA, StoreID: storeB},
		"store outside store_ids":  {BusinessID: bizA, StoreID: storeA2},
		"unknown store":            {BusinessID: bizA, StoreID: ids.New(t0.Add(48 * time.Hour))},
		"no business in the scope": {},
	} {
		_, err := e.Check(ctx, cashier, "order.accept", sc)
		require.Equal(t, genproblems.Forbidden, code(t, err), name)
	}
	_, err = e.Check(ctx, cashier, "stock.adjust", Scope{BusinessID: bizA, StoreID: storeA1})
	require.Equal(t, genproblems.Forbidden, code(t, err), "the cashier column is deny")
	_, err = e.Check(ctx, user(authn.AppCustomer), "order.accept", Scope{BusinessID: bizA})
	require.Equal(t, genproblems.Forbidden, code(t, err), "a customer token never acts on a business")

	// Every store of the business when store_ids is nil.
	s.members[bizA] = Membership{Role: Manager}
	_, err = e.Check(ctx, cashier, "stock.adjust", Scope{BusinessID: bizA, StoreID: storeA2})
	require.NoError(t, err)

	// No resolver: every business action is refused.
	_, err = engine(t, clock.NewFake(t0), nil).Check(ctx, cashier, "order.accept", Scope{BusinessID: bizA})
	require.Equal(t, genproblems.Forbidden, code(t, err))

	// A failed lookup is 500, never an allow.
	failing := engine(t, clock.NewFake(t0), staff{err: errors.New("db down")})
	_, err = failing.Check(ctx, cashier, "order.accept", Scope{BusinessID: bizA, StoreID: storeA1})
	require.Equal(t, genproblems.Internal, code(t, err))
	_, err = failing.Check(ctx, cashier, "order.accept", Scope{BusinessID: bizA})
	require.Equal(t, genproblems.Internal, code(t, err))
}

func TestAdminGrantsAndStepUp(t *testing.T) {
	clk := clock.NewFake(t0)
	e := engine(t, clk, nil)
	ctx := context.Background()

	d, err := e.Check(ctx, user(authn.AppAdmin, "support"), "admin.users.view", Scope{})
	require.NoError(t, err)
	require.Equal(t, Masked, d.Grant)
	require.True(t, d.Grant.Conditional())

	// The strongest grant of any role wins.
	d, err = e.Check(ctx, user(authn.AppAdmin, "field_agent", "support", "admin"), "admin.users.view", Scope{})
	require.NoError(t, err)
	require.Equal(t, Allow, d.Grant)
	require.Equal(t, Admin, d.Role)

	_, err = e.Check(ctx, user(authn.AppAdmin, "analyst"), "admin.users.view", Scope{})
	require.Equal(t, genproblems.Forbidden, code(t, err))
	_, err = e.Check(ctx, user(authn.AppAdmin), "admin.users.view", Scope{})
	require.Equal(t, genproblems.Forbidden, code(t, err), "no roles")

	noTOTP := user(authn.AppAdmin, "admin")
	noTOTP.AMR = authn.AMROTP
	_, err = e.Check(ctx, noTOTP, "admin.users.view", Scope{})
	require.Equal(t, genproblems.Forbidden, code(t, err), "AU-11: an admin without TOTP")
	_, err = e.Check(ctx, user(authn.AppBusiness, "super_admin"), "admin.users.view", Scope{})
	require.Equal(t, genproblems.Forbidden, code(t, err), "roles on a non-admin token are not admin roles")

	// † actions: TOTP younger than the step-up window.
	sa := user(authn.AppAdmin, "super_admin")
	_, err = e.Check(ctx, sa, "admin.settings.edit", Scope{})
	require.Equal(t, genproblems.StepUpRequired, code(t, err), "never stepped up")
	sa.StepUpAt = t0.Add(-authn.StepUpWindow)
	_, err = e.Check(ctx, sa, "admin.settings.edit", Scope{})
	require.NoError(t, err, "exactly at the window edge")
	clk.Advance(time.Second)
	_, err = e.Check(ctx, sa, "admin.settings.edit", Scope{})
	require.Equal(t, genproblems.StepUpRequired, code(t, err))
}

func TestNewValidatesTables(t *testing.T) {
	_, err := New(nil, Options{})
	require.Error(t, err, "a clock is required")
	clk := Options{Clock: clock.NewFake(t0)}

	bad := map[string]Action{
		"name":             {Name: "Bad Name", Kind: KindAny},
		"one segment":      {Name: "order", Kind: KindAny},
		"grants on any":    {Name: "x.y", Kind: KindAny, Grants: map[Role]Grant{Owner: Allow}},
		"unknown kind":     {Name: "x.y", Kind: Kind(42)},
		"step-up business": {Name: "x.y", Kind: KindBusiness, StepUp: true, Grants: map[Role]Grant{Owner: Allow}},
		"admin role biz":   {Name: "x.y", Kind: KindBusiness, Grants: map[Role]Grant{Support: Allow}},
		"biz role admin":   {Name: "x.y", Kind: KindAdmin, Grants: map[Role]Grant{Owner: Allow}},
		"unknown grant":    {Name: "x.y", Kind: KindAdmin, Grants: map[Role]Grant{Admin: Grant(99)}},
		"nobody granted":   {Name: "x.y", Kind: KindAdmin, Grants: map[Role]Grant{Admin: Deny}},
		"no grants":        {Name: "x.y", Kind: KindBusiness},
	}
	for name, a := range bad {
		_, err := New([]Table{{Domain: "d", Actions: []Action{a}}}, clk)
		require.Error(t, err, name)
	}
	_, err = New([]Table{{Actions: []Action{{Name: "x.y", Kind: KindAny}}}}, clk)
	require.Error(t, err, "a table without a domain")
	_, err = New([]Table{
		{Domain: "a", Actions: []Action{{Name: "x.y", Kind: KindAny}}},
		{Domain: "b", Actions: []Action{{Name: "x.y", Kind: KindCustomer}}},
	}, clk)
	require.ErrorContains(t, err, "defined twice")
}

func TestNamesAndGolden(t *testing.T) {
	require.Equal(t, "-", Deny.String())
	require.Equal(t, "allow", Allow.String())
	require.Equal(t, "own,window", OwnWindow.String())
	require.Equal(t, "grant(99)", Grant(99).String())
	require.False(t, Allow.Conditional())
	require.False(t, Deny.Conditional())
	require.True(t, Money.Conditional())
	require.Equal(t, "business", KindBusiness.String())
	require.Equal(t, "kind(42)", Kind(42).String())
	require.True(t, IsAdminRole("super_admin"))
	require.False(t, IsAdminRole("owner"))

	g := Golden(testTable)
	require.Contains(t, g, "# domain test:")
	require.Contains(t, g, "order.accept kind=business step-up=false\n")
	require.Contains(t, g, "stock.adjust cashier -\n")
	require.Contains(t, g, "admin.settings.edit kind=admin step-up=true\n")
	require.Contains(t, g, "admin.users.view support masked\n")
	// One line per (action × role) plus one header per action, plus the title.
	lines := strings.Count(g, "\n")
	require.Equal(t, 1+len(testTable.Actions)+2*len(BusinessRoles)+2*len(AdminRoles), lines)
}

func request(t *testing.T, pattern, path string, p authn.Principal) *http.Request {
	t.Helper()
	var out *http.Request
	r := chi.NewRouter()
	r.Get(pattern, func(_ http.ResponseWriter, r *http.Request) { out = r })
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(authn.WithPrincipal(req.Context(), p))
	r.ServeHTTP(httptest.NewRecorder(), req)
	require.NotNil(t, out, "route %s did not match %s", pattern, path)
	return out
}

func TestScopeFromAndAuthorize(t *testing.T) {
	biz, store := ids.New(t0), ids.New(t0.Add(time.Second))
	s := staff{members: map[ids.ID]Membership{biz: {Role: Owner}}, stores: map[ids.ID]ids.ID{store: biz}}
	e := engine(t, clock.NewFake(t0), s)
	owner := user(authn.AppBusiness)

	r := request(t, "/v1/biz/businesses/{businessId}/stores/{storeId}/orders", "/v1/biz/businesses/"+biz.String()+"/stores/"+store.String()+"/orders", owner)
	sc, err := ScopeFrom(r)
	require.NoError(t, err)
	require.Equal(t, Scope{BusinessID: biz, StoreID: store}, sc)
	ctx, err := Authorize(e, r, "order.accept")
	require.NoError(t, err)
	d, ok := DecisionFrom(ctx)
	require.True(t, ok)
	require.Equal(t, Owner, d.Role)
	require.Equal(t, sc, d.Scope)

	r = request(t, "/v1/biz/businesses/{businessId}", "/v1/biz/businesses/not-a-ulid", owner)
	_, err = ScopeFrom(r)
	require.Equal(t, genproblems.NotFound, code(t, err))
	_, err = Authorize(e, r, "order.accept")
	require.Equal(t, genproblems.NotFound, code(t, err))

	_, err = Authorize(nil, r, "order.accept")
	require.Equal(t, genproblems.Internal, code(t, err), "no authorizer fails closed")

	r = request(t, "/v1/me", "/v1/me", authn.Principal{})
	_, err = Authorize(e, r, "me.view")
	require.Equal(t, genproblems.Unauthorized, code(t, err))
	_, ok = DecisionFrom(context.Background())
	require.False(t, ok)
}

type handlerFunc func(context.Context, http.ResponseWriter, *http.Request, any) (any, error)

func TestStrict(t *testing.T) {
	e := engine(t, clock.NewFake(t0), nil)
	mw := Strict[handlerFunc](e, map[string]string{"GetMe": "me.view", "SendOtp": Public, "Edit": "cart.edit"})
	var seen *Decision
	next := handlerFunc(func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		if d, ok := DecisionFrom(ctx); ok {
			seen = &d
		}
		return "ok", nil
	})
	call := func(op string, p authn.Principal) (any, error) {
		seen = nil
		req := request(t, "/x", "/x", p)
		return mw(next, op)(req.Context(), httptest.NewRecorder(), req, nil)
	}

	out, err := call("GetMe", user(authn.AppCustomer))
	require.NoError(t, err)
	require.Equal(t, "ok", out)
	require.NotNil(t, seen)
	require.Equal(t, "me.view", seen.Action)

	out, err = call("SendOtp", authn.Principal{})
	require.NoError(t, err, "a public operation runs for an anonymous caller")
	require.Equal(t, "ok", out)
	require.Nil(t, seen)

	_, err = call("GetMe", authn.Principal{})
	require.Equal(t, genproblems.Unauthorized, code(t, err))
	_, err = call("Edit", user(authn.AppBusiness))
	require.Equal(t, genproblems.Forbidden, code(t, err))
	_, err = call("Unlisted", user(authn.AppCustomer))
	require.Equal(t, genproblems.Internal, code(t, err), "an operation missing from the map fails closed")
}

package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Scope is what a business action applies to, from the path
// (/v1/biz/businesses/{bid}/stores/{sid}/…, 02 §3.1).
type Scope struct {
	BusinessID ids.ID // required for KindBusiness
	StoreID    ids.ID // zero for business-level actions
}

// Membership is a user's staff row in one business.
type Membership struct {
	Role Role
	// StoreIDs limits the stores; nil means every store of the business.
	StoreIDs []ids.ID
}

// StaffResolver answers business membership (W1-04 implements it on the
// staff and stores tables). ok false means "no active staff row" or "no such
// store"; err is for a failed lookup only.
type StaffResolver interface {
	Membership(ctx context.Context, userID, businessID ids.ID) (m Membership, ok bool, err error)
	StoreBusiness(ctx context.Context, storeID ids.ID) (businessID ids.ID, ok bool, err error)
}

// Decision is an allowed check. Handlers read it with DecisionFrom to apply a
// conditional grant (Masked, Own, OwnWindow, Money, NotOwn).
type Decision struct {
	Action string
	Grant  Grant
	// Role is the business role, or for an admin the role that gave Grant.
	Role  Role
	Scope Scope
}

// Options configure the engine.
type Options struct {
	Clock clock.Clock
	// Staff resolves business membership; nil refuses every KindBusiness action.
	Staff StaffResolver
}

// Engine checks principals against the tables. Safe for concurrent use.
type Engine struct {
	actions map[string]Action
	clock   clock.Clock
	staff   StaffResolver
}

// New validates the tables and returns the engine. Action names must be
// unique across tables.
func New(tables []Table, o Options) (*Engine, error) {
	if o.Clock == nil {
		return nil, errors.New("authz: a clock is required")
	}
	e := &Engine{actions: map[string]Action{}, clock: o.Clock, staff: o.Staff}
	var errs []error
	for _, t := range tables {
		if t.Domain == "" {
			errs = append(errs, errors.New("authz: a table without a domain"))
		}
		for _, a := range t.Actions {
			if err := a.validate(); err != nil {
				errs = append(errs, fmt.Errorf("authz: %s: %w", t.Domain, err))
				continue
			}
			if _, dup := e.actions[a.Name]; dup {
				errs = append(errs, fmt.Errorf("authz: %s: action %s is defined twice", t.Domain, a.Name))
				continue
			}
			e.actions[a.Name] = a
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return e, nil
}

// Has reports whether action is defined.
func (e *Engine) Has(action string) bool { _, ok := e.actions[action]; return ok }

// Errors from Check are problems: 401 unauthorized (anonymous), 403
// forbidden (refused), 403 step_up_required (a † action without a fresh SMS step-up),
// 500 internal (unknown action, a failed lookup).
func refused() error { return problem.New(genproblems.Forbidden, "") }

// Check decides whether p may perform action on s.
func (e *Engine) Check(ctx context.Context, p authn.Principal, action string, s Scope) (Decision, error) {
	a, ok := e.actions[action]
	if !ok {
		// A route naming an undefined action is a programming error: fail closed.
		return Decision{}, problem.Wrap(genproblems.Internal, fmt.Errorf("authz: unknown action %q", action))
	}
	if p.IsAnonymous() {
		return Decision{}, problem.New(genproblems.Unauthorized, "")
	}
	d := Decision{Action: action, Scope: s}
	switch a.Kind {
	case KindAny:
		d.Grant = Allow
	case KindCustomer:
		if p.App != authn.AppCustomer {
			return Decision{}, refused()
		}
		d.Grant = Allow
	case KindBusinessUser:
		if p.App != authn.AppBusiness {
			return Decision{}, refused()
		}
		d.Grant = Allow
	case KindBusiness:
		return e.checkBusiness(ctx, p, a, d)
	case KindAdmin:
		return e.checkAdmin(p, a, d)
	default:
		return Decision{}, refused()
	}
	return d, nil
}

func (e *Engine) checkBusiness(ctx context.Context, p authn.Principal, a Action, d Decision) (Decision, error) {
	s := d.Scope
	if p.App != authn.AppBusiness || s.BusinessID == ids.Zero || e.staff == nil {
		return Decision{}, refused()
	}
	if s.StoreID != ids.Zero {
		bid, ok, err := e.staff.StoreBusiness(ctx, s.StoreID)
		if err != nil {
			return Decision{}, problem.Wrap(genproblems.Internal, err)
		}
		if !ok || bid != s.BusinessID {
			return Decision{}, refused() // another business's store (AU-09)
		}
	}
	m, ok, err := e.staff.Membership(ctx, p.UserID, s.BusinessID)
	if err != nil {
		return Decision{}, problem.Wrap(genproblems.Internal, err)
	}
	if !ok {
		return Decision{}, refused()
	}
	g := a.Grants[m.Role]
	if g == Deny {
		return Decision{}, refused()
	}
	if s.StoreID != ids.Zero && m.StoreIDs != nil && !slices.Contains(m.StoreIDs, s.StoreID) {
		return Decision{}, refused()
	}
	d.Grant, d.Role = g, m.Role
	return d, nil
}

func (e *Engine) checkAdmin(p authn.Principal, a Action, d Decision) (Decision, error) {
	if p.App != authn.AppAdmin {
		return Decision{}, refused()
	}
	for _, r := range p.Roles {
		if g := a.Grants[Role(r)]; g > d.Grant {
			d.Grant, d.Role = g, Role(r)
		}
	}
	if d.Grant == Deny {
		return Decision{}, refused()
	}
	if a.StepUp {
		at := p.StepUpAt
		if at.IsZero() || e.clock.Now().Sub(at) > authn.StepUpWindow {
			return Decision{}, problem.New(genproblems.StepUpRequired, "")
		}
	}
	return d, nil
}

type decisionKey struct{}

// WithDecision stores d in ctx (slot 15 does this).
func WithDecision(ctx context.Context, d Decision) context.Context {
	return context.WithValue(ctx, decisionKey{}, d)
}

// DecisionFrom returns the decision slot 15 made for this request.
func DecisionFrom(ctx context.Context) (Decision, bool) {
	d, ok := ctx.Value(decisionKey{}).(Decision)
	return d, ok
}

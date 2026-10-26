// Package assist is what the admin console's AI assistants share (W3-13, D-233: catalogue
// clean-up AI-A1, ticket triage AI-A2, risk summary AI-A3): the admin caller, the read of
// one of the caller's own drafts and the confirm's ownership check.
//
// An admin draft has no store (ids.Zero) and belongs to the admin who asked for it: another
// admin's draft is the common 404, so a draft never shows one agent's work to another, and
// the route's own policy (the screen's) has already checked the caller may see the subject.
package assist

import (
	"context"
	"errors"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Caller is the signed-in admin the route's check passed.
func Caller(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.App != authn.AppAdmin {
		return authn.Principal{}, problem.Wrap(genproblems.Internal, errors.New("assist: no admin caller"))
	}
	return p, nil
}

// Own is the Check of a confirm: a store's draft or another admin's is the common 404.
func Own(caller ids.ID) func(drafts.Draft) error {
	return func(d drafts.Draft) error {
		if d.Store != ids.Zero || d.Actor != caller {
			return retail.NotFound()
		}
		return nil
	}
}

// Load reads one of the caller's own drafts (closing a stale one first, as retail.Load).
func Load(ctx context.Context, k retail.Kit, feature string, caller, id ids.ID) (drafts.Draft, error) {
	d, err := k.Load(ctx, feature, ids.Zero, id)
	if err != nil {
		return drafts.Draft{}, err
	}
	if err := Own(caller)(d); err != nil {
		return drafts.Draft{}, err
	}
	return d, nil
}

// Can reports whether the caller holds an admin action (a per-line check at confirm, and
// canApply on a draft line). A nil authorizer allows nothing.
func Can(ctx context.Context, az authz.Authorizer, p authn.Principal, action string) bool {
	if az == nil {
		return false
	}
	_, err := az.Check(ctx, p, action, authz.Scope{})
	return err == nil
}

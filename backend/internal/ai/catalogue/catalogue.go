// Package catalogue drafts a clean-up of a product waiting in the A-08 review queue (AI-A1,
// W3-13, D-233): its fields normalised, the active product to merge it into with a reason,
// and its category's names in the five languages.
//
// Rules this package holds:
//   - Everything is a draft (11 §1 rule 1). Confirm applies a line through A-08's own writer,
//     as the screen's buttons would, audited with the assistant's reason: normalise edits the
//     pending product, duplicate merges it, categoryNames renames its category. Nothing merges
//     or changes from a model output alone.
//   - Normalise and duplicate are exclusive: both accepted or edited is 422 (merge or edit).
//   - Each line needs the action its A-08 button needs: normalise and categoryNames
//     admin.catalogue.manage, duplicate admin.catalogue.review. A line the caller may not apply
//     is not_permitted and the draft stays open (canApply tells the screen first).
//   - The duplicate is one of the queue's own look-alikes (pg_trgm, DuplicateCandidates); the
//     model picks by reference and may pick none. A pack size is the product's own or one
//     written in its name or variant, and every number in a drafted name, variant, brand or
//     reason is one the model was given (D-230). Category names are drafted only while the
//     category lacks a language, each in its own script; a language it has keeps its name.
//   - The common 404 while ai_catalogue_enabled is off or the key is missing; the route needs
//     admin.catalogue.review (A-08's queue), and a draft is its creator's only.
package catalogue

import (
	"context"
	"errors"

	catapi "github.com/todayz/backend/gen/api/admin-catalogue"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// Feature is the drafts feature name.
const Feature = "catalogue_cleanup"

// Catalogue is A-08's writes the confirm applies (admin.Service).
type Catalogue interface {
	AssistUpdateProduct(ctx context.Context, actor, id ids.ID, body catapi.AdminProductUpdate) error
	AssistMergePending(ctx context.Context, actor, id, into ids.ID) error
	AssistCategoryNames(ctx context.Context, actor, id ids.ID, names catapi.AdminCategoryNames) error
}

// Deps are the service's dependencies.
type Deps struct {
	DB        *db.DB
	Clock     clock.Clock
	Settings  *settings.Cache
	LLM       client.LLM
	Kit       retail.Kit
	Catalogue Catalogue
	// Authz answers canApply and the confirm's per-line check; without it no line may be
	// applied.
	Authz authz.Authorizer
}

// Service is the catalogue clean-up draft.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil || d.Catalogue == nil {
		return nil, errors.New("catalogue: a clock and the catalogue writer are required")
	}
	d.Kit.Clock, d.Kit.DB = d.Clock, d.DB
	return &Service{d: d}, nil
}

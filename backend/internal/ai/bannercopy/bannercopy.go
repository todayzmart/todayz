// Package bannercopy drafts banner copy (AI-R9, B-15 banners): three headline and sub-line
// options for an offer or a few items, in the shop's language and in English, each with a
// tint and an item for the photo. The owner picks one and edits it in the banner editor.
//
// Rules this package holds:
//   - Nothing is published (11 §1 rule 1): confirm answers the picked copy for the banner
//     editor (POST .../banners does the rest); it writes no banner.
//   - The model sees only the offer's terms and the items' names and prices. Every number in
//     every option must be one of those (schema.Ungrounded); an option that is not is dropped,
//     and a draft with no grounded option fails as grounding.
//   - The item for an option is picked by its number in the facts, mapped to the store item
//     by the server, never an id from the model.
//   - Language: the shop's (the owner's preferred language), English when unset.
//   - The common 404 while ai_bannercopy_enabled is off or the key is missing; the route needs
//     banners.manage (owner and manager).
package bannercopy

import (
	"errors"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/settings"
)

// Feature is the drafts feature name.
const Feature = "banner_copy"

// Fixed numbers (the contract's limits, not settings).
const (
	MaxItems    = 10 // BannerCopyDraftRequest.itemIds maxItems
	Options     = 3  // the options one draft carries
	MaxHeadline = 40 //settings:ok BannerCopyLine.headline maxLength (the banner template's)
	MaxSubline  = 60 // BannerCopyLine.subline maxLength
)

// Tints are the banner template tints (BannerTint).
var Tints = []string{"green", "amber", "rose", "sky", "lilac", "peach"}

// Deps are the service's dependencies.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	LLM      client.LLM
	Kit      retail.Kit
}

// Service is the banner copy draft.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("bannercopy: a clock is required")
	}
	d.Kit.Clock, d.Kit.DB = d.Clock, d.DB
	return &Service{d: d}, nil
}

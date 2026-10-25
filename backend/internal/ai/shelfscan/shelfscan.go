// Package shelfscan is the shelf photo onboarding (AI-R3, B-03 onboarding): a photo of a
// shelf becomes draft lines naming the products seen, each matched to a master product, that
// a person checks, prices and confirms as store items.
//
// Rules this package holds:
//   - The photo goes to the model once and is never stored. Matching is the backend's (pg_trgm
//     word similarity on brand, name, variant and pack), never an id from the model.
//   - Nothing is listed until a person confirms (11 §1 rule 1). Confirm adds each accepted or
//     edited line as a store item through the catalogue's own create (the same checks as adding
//     by hand: price at most the MRP, item.price.edit), all in one transaction.
//   - The price to add at is the product's MRP unless edited. A line without a product, a
//     product the store already sells, a product named twice, or an accepted line whose
//     product has no MRP is 422 and nothing is applied.
//   - The common 404 while ai_shelfscan_enabled is off or the key is missing; the route needs
//     item.manage (owner and manager).
package shelfscan

import (
	"context"
	"errors"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// Feature is the drafts feature name.
const Feature = "shelf_photo"

// Fixed numbers (the contract's limits, not settings).
const (
	// MaxLines bounds the products of one photo (ShelfPhotoDraft.lines maxItems).
	MaxLines = 40 //settings:ok the contract's ShelfPhotoDraft.lines maxItems
	// MaxOpening bounds the opening stock of a line (ShelfPhotoLineValue.openingStock).
	MaxOpening = 9999
	// MaxPaise bounds a price (₹10,00,000).
	MaxPaise = 100_000_000
	// matchMinScore is the least word similarity a master product needs to be a line's match.
	matchMinScore = 0.5
)

// NewItem is one confirmed line as the catalogue creates it.
type NewItem struct {
	ProductID    ids.ID
	PricePaise   int64
	OpeningStock int
}

// Items creates a store item through the catalogue's create, inside the confirm's
// transaction. Its problems carry the items route's field paths.
type Items interface {
	Add(ctx context.Context, sc authz.Scope, it NewItem) error
}

// Deps are the service's dependencies.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	LLM      client.LLM
	Kit      retail.Kit
	Items    Items
}

// Service is the shelf photo onboarding.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("shelfscan: a clock is required")
	}
	d.Kit.Clock = d.Clock
	return &Service{d: d}, nil
}

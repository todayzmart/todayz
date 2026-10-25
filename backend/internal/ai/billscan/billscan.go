// Package billscan is the supplier bill scan (AI-R2, B-07 restock): a photo of a
// distributor's bill becomes draft restock lines (item, quantity, unit cost, MRP, expiry)
// that a person checks, edits and confirms.
//
// Rules this package holds:
//   - The photo goes to the model once and is never stored. The draft keeps the lines the
//     model read, each matched to a store item by the backend (pg_trgm word similarity on
//     the item's name, brand, variant and pack), never by an id from the model.
//   - Nothing changes stock until a person confirms (11 §1 rule 1). Confirm writes one
//     supplier_bill stock in per accepted or edited line through the catalogue's stock
//     writer, with the unit cost and the expiry as a new batch, naming the draft.
//   - A line with no matched item must be edited to name one; a cost above the item's MRP
//     or an expiry before today (IST) is refused (422), never written.
//   - Unmatched lines can be added as products from the review screen (a link to add an
//     item); the draft itself never creates a product. TODO(lead): AI-R2 says "unmatched
//     lines offered as create product"; conservative reading, a link, not an automatic create.
//   - Every endpoint is the common 404 while ai_billscan_enabled is off or the model key is
//     missing (guard.Require); the route needs stock.restock (owner and manager).
package billscan

import (
	"context"
	"errors"
	"time"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// Feature is the drafts feature name.
const Feature = "supplier_bill"

// Fixed numbers (the contract's limits, not settings).
const (
	// MaxLines bounds the lines of one bill.
	MaxLines = 60 //settings:ok the contract's SupplierBillDraft.lines maxItems
	// MaxQuantity bounds a line's quantity (SupplierBillLine.quantity).
	MaxQuantity = 9999
	// MaxPaise bounds a unit cost or an MRP (₹10,00,000).
	MaxPaise = 100_000_000
	// matchMinScore is the least word similarity a store item needs to be a line's match;
	// below it the person picks the item.
	matchMinScore = 0.45
)

// Errors a Stock adapter returns for the confirm to answer 422.
var (
	ErrItemGone = errors.New("billscan: the item is not in the store")
	ErrQuantity = errors.New("billscan: quantity out of range")
)

// StockIn is one confirmed line as the catalogue writes it.
type StockIn struct {
	StoreID, ItemID, DraftID, Who ids.ID
	Qty                           int
	CostPaise                     *int64
	ExpiresOn                     *time.Time // IST date as midnight UTC
}

// Stock writes a supplier_bill stock in through the catalogue's stock writer, inside the
// confirm's transaction.
type Stock interface {
	AddFromBill(ctx context.Context, in StockIn) error
}

// Deps are the service's dependencies.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	LLM      client.LLM
	Kit      retail.Kit
	Stock    Stock
}

// Service is the supplier bill scan.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("billscan: a clock is required")
	}
	d.Kit.Clock, d.Kit.DB = d.Clock, d.DB
	return &Service{d: d}, nil
}

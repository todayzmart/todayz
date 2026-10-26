// Package voice is the voice stock update (B-06, AI-R1, 02 §8, D-65): a shop speaks sales,
// restocks, counts and price changes, and gets draft lines to confirm.
//
// Rules this package holds:
//   - A recording becomes an ai_drafts row (feature voice_stock). Speech to text, the PII
//     scrubber and the model run in the background; the draft turns ready with its lines or
//     failed with a short code. Nothing changes stock, prices or sales until a person
//     confirms (11 §1 rule 1). The recording itself is never stored.
//   - The model matches items only from the store's candidate list (the top MaxCandidates
//     items by units sold in the last SalesWindow); it answers with short refs (c1, c2, …)
//     that the backend maps back to store item ids, so an id the store does not have can
//     never come out of the model. A ref not in the list becomes an unmatched line.
//   - Line kinds are sale, restock, count and set_price (D-65). "Out of stock" is a count
//     of 0. A sale is a POS sale with no customer (cash, at the items' prices at confirm).
//   - Confirm checks every line against the confirmer's permission: sale needs pos.sell,
//     count stock.count (cashier and above), restock stock.restock and set_price
//     item.price.edit (manager and above). A line the confirmer may not make is
//     not_permitted and not applied; the draft shows it disabled ("Ask a manager") and stays
//     open with those lines until a manager confirms or someone discards them.
//   - Every endpoint is the common 404 while ai_voice_stock_enabled or feature_voice_enabled
//     is off or a provider key is missing (guard.Require).
//   - Transcripts are kept TranscriptRetention for quality review, then removed from the
//     draft by the hourly voice_transcript_retention job (02 §8 step 6).
package voice

import (
	"context"
	"errors"
	"time"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// Feature is the drafts feature name.
const Feature = "voice_stock"

// Fixed numbers of 02 §8 and 01 §17 (fixed constants, not settings).
const (
	// MaxCandidates is how many of the store's items the model may match (02 §8 step 3).
	MaxCandidates = 2000 //settings:ok 02 §8 fixed candidate count, not a product setting
	// SalesWindow ranks the candidates by units sold in this window.
	SalesWindow = 90 * 24 * time.Hour
	// TranscriptRetention is how long a transcript stays on its draft (02 §8 step 6).
	TranscriptRetention = 90 * 24 * time.Hour
	// MaxSeconds is the voice recording length (01 §17 fixed constants); the app stops there.
	MaxSeconds = 15
	// MaxAudioBytes bounds one recording (15 s of Opus is far below it; the contract's limit).
	MaxAudioBytes = 1 << 20
	// MaxQuantity bounds a line's quantity (the contract's 0…9999).
	MaxQuantity = 9999
	// MaxPricePaise bounds a set_price line (the contract's limit, ₹10,00,000).
	MaxPricePaise = 100_000_000
	// MaxLines bounds the model's lines for one recording.
	MaxLines = 50
	// ProcessTimeout bounds speech to text plus the model call in the background.
	ProcessTimeout = 90 * time.Second
	// StaleAfter closes a draft still processing this long after it was created as failed
	// (timeout) when it is read: the process that ran it has gone.
	StaleAfter = 3 * time.Minute
)

// Line kinds (VoiceStockLineKind).
const (
	KindSale     = "sale"
	KindRestock  = "restock"
	KindCount    = "count"
	KindSetPrice = "set_price"
)

// Kinds are the line kinds in contract order.
var Kinds = []string{KindSale, KindRestock, KindCount, KindSetPrice}

// Failure codes stored on a failed draft besides client.Outcome's.
const (
	FailEmpty = "empty" // nothing was heard, or nothing in it was a stock change
)

// Errors a Stock adapter returns for the confirm to answer 422.
var (
	ErrItemGone = errors.New("voice: the item is not in the store")
	ErrQuantity = errors.New("voice: quantity out of range")
	ErrStale    = errors.New("voice: the item changed under the confirm")
	errNullID   = errors.New("voice: an item row has no id")
)

// SaleLine is one line of the sale a confirm writes.
type SaleLine struct {
	ItemID         ids.ID
	Quantity       int
	UnitPricePaise int64
}

// Seller writes one POS sale with no customer (pos.Service.CreateSale in the binary). It runs
// inside the confirm's transaction; clientID makes it replay-safe.
type Seller interface {
	Sell(ctx context.Context, sc authz.Scope, clientID ids.ID, lines []SaleLine) error
}

// Stock writes the non-sale lines through the catalogue (its stock writer and price edit)
// inside the confirm's transaction. Who is the confirmer.
type Stock interface {
	Restock(ctx context.Context, storeID, itemID ids.ID, qty int, draftID, who ids.ID) error
	Count(ctx context.Context, storeID, itemID ids.ID, counted int, who ids.ID) error
	SetPrice(ctx context.Context, storeID, itemID ids.ID, pricePaise int64) error
}

// Deps are the service's dependencies.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	Authz    authz.Authorizer
	LLM      client.LLM
	STT      client.STT
	Guard    *guard.Guard
	Drafts   *drafts.Service
	Seller   Seller
	Stock    Stock
	// Go runs the background processing of one recording; nil starts a goroutine. Tests
	// pass a runner that waits.
	Go func(func())
}

// Service is the voice stock update.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) (*Service, error) {
	// Only the clock is required, as in the other domains: the route-only build (the contract
	// test) has no database or authorizer, and a call would fail before using them.
	if d.Clock == nil {
		return nil, errors.New("voice: a clock is required")
	}
	if d.Go == nil {
		d.Go = func(f func()) { go f() }
	}
	return &Service{d: d}, nil
}

package main

import (
	"context"
	"errors"

	"github.com/go-chi/chi/v5"

	posapi "github.com/todayz/backend/gen/api/pos"
	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/pos"
	"github.com/todayz/backend/internal/voice"
)

// voiceSeams lets a test swap the voice deps (a waiting background runner,
// recording clients); nil in the binary.
var voiceSeams func(*voice.Deps)

// newVoice builds W3-04's voice stock update on the AI kit (W3-10): the metered
// model and speech clients, the guard and the drafts store. Sales go through
// the pos service, restocks, counts and prices through the catalogue.
func newVoice(d *deps) *voice.Service {
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	k := aiKitFor(d)
	vd := voice.Deps{
		DB: d.DB, Clock: d.Clock, Settings: d.Settings, Authz: az,
		LLM: k.LLM, STT: k.STT(voice.Feature), Guard: k.Guard, Drafts: k.Drafts,
		Seller: posSeller{newPos(d)},
		Stock:  catalogueStock{db: d.DB, w: catalogue.NewWriter(d.Clock), clock: d.Clock},
	}
	if voiceSeams != nil {
		voiceSeams(&vd)
	}
	svc, err := voice.New(vd)
	if err != nil {
		// Only a missing clock; deps always has one.
		panic(err.Error())
	}
	return svc
}

// mountVoice mounts W3-04's routes: availability, and create, read and confirm a voice stock
// draft (B-06).
func mountVoice(r chi.Router, d *deps) {
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	voice.Register(r, newVoice(d), az)
}

// posSeller adapts pos.Service to voice.Seller: one cash sale with no
// customer under the client id voice gives it (a replay finds the same sale).
type posSeller struct{ s *pos.Service }

var _ voice.Seller = posSeller{}

// Sell implements voice.Seller.
func (a posSeller) Sell(ctx context.Context, sc authz.Scope, clientID ids.ID, lines []voice.SaleLine) error {
	body := posapi.PosSaleCreateRequest{ClientId: clientID.String(), PaymentMethod: posapi.Cash, Lines: make([]posapi.PosSaleLineInput, len(lines))}
	for i, l := range lines {
		item := l.ItemID.String()
		body.Lines[i] = posapi.PosSaleLineInput{
			ClientId: ids.New(ids.Time(clientID)).String(), StoreItemId: &item,
			UnitPricePaise: int(l.UnitPricePaise), Quantity: l.Quantity,
		}
	}
	_, err := a.s.CreateSale(ctx, sc, body)
	return err
}

// catalogueStock adapts the catalogue's stock writer and price edit to
// voice.Stock, in the confirm's transaction.
type catalogueStock struct {
	db    *db.DB
	w     *catalogue.Writer
	clock clock.Clock
}

var _ voice.Stock = catalogueStock{}

// Restock implements voice.Stock: a restock movement that names the draft.
func (a catalogueStock) Restock(ctx context.Context, storeID, itemID ids.ID, qty int, draftID, who ids.ID) error {
	_, err := a.w.AddStock(ctx, a.db.Q(ctx), catalogue.StockIn{
		StoreID: storeID, ItemID: itemID, Qty: int32(qty), Source: catalogue.SourceRestock, // #nosec G115 -- voice validates qty ≤ voice.MaxQuantity
		RefType: "ai_draft", RefID: draftID, Who: catalogue.Who{UserID: who},
	})
	return voiceStockErr(err)
}

// Count implements voice.Stock: a count correction for the difference.
func (a catalogueStock) Count(ctx context.Context, storeID, itemID ids.ID, counted int, who ids.ID) error {
	_, _, err := a.w.Count(ctx, a.db.Q(ctx), catalogue.Count{
		StoreID: storeID, ItemID: itemID, Counted: int32(counted), Who: catalogue.Who{UserID: who}, // #nosec G115 -- voice validates counted ≤ voice.MaxQuantity
	})
	return voiceStockErr(err)
}

// SetPrice implements voice.Stock.
func (a catalogueStock) SetPrice(ctx context.Context, storeID, itemID ids.ID, pricePaise int64) error {
	return voiceStockErr(catalogue.SetPrice(ctx, a.db.Q(ctx), a.clock.Now(), storeID, itemID, pricePaise))
}

func voiceStockErr(err error) error {
	var over *catalogue.OverError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, catalogue.ErrItemNotFound):
		return voice.ErrItemGone
	case errors.Is(err, catalogue.ErrStale):
		return voice.ErrStale
	case errors.Is(err, catalogue.ErrQuantity), errors.As(err, &over):
		return voice.ErrQuantity
	}
	return err
}

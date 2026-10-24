package jobs

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/pos"
)

// registerpos declares the pos domain's job kinds. W2-04 has none: a bill
// claim expires by its own time (the reads compare expires_at), and the
// credits held at confirm are released by the credits domain's sweep.
func registerpos(r *Registry, d Deps) {}

// PosStock is the catalogue's stock writer as pos.Stock: a sale line is one
// sale_pos movement referring to the sale, idempotent on the line's client
// id; a reversal is a pos_reversal against that movement; an offline stock
// entry is a count, a restock or an uncapped damage or expired removal
// (02 §4.3: the device saw it happen). The API's routes file uses it.
func PosStock(c clock.Clock) pos.Stock { return posStock{w: catalogue.NewWriter(c)} }

type posStock struct{ w *catalogue.Writer }

func (s posStock) Sell(ctx context.Context, tx pgx.Tx, in pos.StockSale) (ids.ID, error) {
	m, err := s.w.Sell(ctx, tx, catalogue.Sale{
		StoreID: in.StoreID, ItemID: in.ItemID, Qty: in.Qty,
		Source: catalogue.SourceSalePOS, RefType: "pos_sale", RefID: in.SaleID,
		ClientID: in.ClientID, Who: catalogue.Who{UserID: in.UserID, DeviceID: in.DeviceID},
	})
	if err != nil {
		return ids.Zero, posStockErr(err)
	}
	return m.ID, nil
}

func (s posStock) Reverse(ctx context.Context, tx pgx.Tx, in pos.StockReversal) (ids.ID, error) {
	m, err := s.w.Reverse(ctx, tx, catalogue.Reversal{
		StoreID: in.StoreID, MovementID: in.MovementID, Qty: in.Qty, Note: in.Note,
		Who: catalogue.Who{UserID: in.UserID},
	})
	if err != nil {
		return ids.Zero, posStockErr(err)
	}
	return m.ID, nil
}

func (s posStock) Change(ctx context.Context, tx pgx.Tx, in pos.StockChange) (pos.StockResult, error) {
	who := catalogue.Who{UserID: in.UserID, DeviceID: in.DeviceID}
	var (
		m   catalogue.Movement
		ok  = true
		err error
	)
	switch in.Kind {
	case pos.ChangeCount:
		m, ok, err = s.w.Count(ctx, tx, catalogue.Count{
			StoreID: in.StoreID, ItemID: in.ItemID, Counted: in.Qty, ClientID: in.ClientID, Note: in.Note, Who: who,
		})
	case pos.ChangeRestock:
		m, err = s.w.AddStock(ctx, tx, catalogue.StockIn{
			StoreID: in.StoreID, ItemID: in.ItemID, Qty: in.Qty, Source: catalogue.SourceRestock,
			ClientID: in.ClientID, Note: in.Note, Who: who,
		})
	case pos.ChangeDamage, pos.ChangeExpired:
		src := catalogue.SourceDamage
		if in.Kind == pos.ChangeExpired {
			src = catalogue.SourceExpired
		}
		m, err = s.w.Remove(ctx, tx, catalogue.Removal{
			StoreID: in.StoreID, ItemID: in.ItemID, Qty: in.Qty, Source: src,
			ClientID: in.ClientID, Note: in.Note, Who: who,
		})
	default:
		return pos.StockResult{}, pos.ErrStockInvalid
	}
	if err != nil {
		return pos.StockResult{}, posStockErr(err)
	}
	return pos.StockResult{MovementID: m.ID, Wrote: ok, Replayed: m.Replayed}, nil
}

// posStockErr maps the catalogue's refusals to the pos seam's.
func posStockErr(err error) error {
	var over *catalogue.OverError
	switch {
	case errors.Is(err, catalogue.ErrItemNotFound), errors.Is(err, catalogue.ErrMovementNotFound):
		return pos.ErrItemNotFound
	case errors.Is(err, catalogue.ErrClientIDReused):
		return pos.ErrClientIDReused
	case errors.Is(err, catalogue.ErrQuantity), errors.Is(err, catalogue.ErrSource),
		errors.Is(err, catalogue.ErrNotReversible), errors.As(err, &over):
		return pos.ErrStockInvalid
	}
	return err
}

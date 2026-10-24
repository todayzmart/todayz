package jobs

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/pos"
)

// KindPosBillClaimExpiry expires bill claims past their 120 s and releases
// the credits a confirmed one held (D-131).
const KindPosBillClaimExpiry = "pos_bill_claim_expiry"

// registerpos declares the pos domain's job kinds: the bill-claim sweep. A
// claim's reads and its sale already treat it as expired at expires_at; the
// sweep only gives the held credits back.
func registerpos(r *Registry, d Deps) {
	var (
		mu  sync.Mutex
		svc *pos.Service
	)
	service := func() (*pos.Service, error) {
		mu.Lock()
		defer mu.Unlock()
		if svc != nil {
			return svc, nil
		}
		s, err := pos.New(PosJobDeps(d))
		if err != nil {
			return nil, err
		}
		svc = s
		return s, nil
	}
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindPosBillClaimExpiry, Owner: "W2-04", Class: platformjobs.ClassTimer,
		Description: "Expires bill QR claims past their 120 s and releases the credits a confirmed one held",
		Schedule:    "* * * * *", UniqueBy: platformjobs.PerTick, Timeout: 30 * time.Second, MaxAttempts: 3,
		Runbook: "docs/runbooks/pos-bill-claims.md",
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			s, err := service()
			if err != nil {
				return platformjobs.Result{}, err
			}
			r, err := s.ExpireClaims(ctx)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{
				"expired": int64(r.Expired), "released": int64(r.Released), "failed": int64(r.Failed),
			}}, nil
		},
	})
}

// PosDeps completes base with the pos seams the API's routes and the worker
// must share: the stock writer, credits and referrals. Both build their
// service through it, so the sweep releases a hold through the same ledger
// the routes held it with (D-131).
func PosDeps(base pos.Deps) pos.Deps {
	base.Stock = PosStock(base.Clock)
	// TODO(lead): W2-01 wires its credits ledger here and W2-03 its
	// referrals; until then the do-nothing defaults (no hold exists).
	base.Credits = pos.NoCredits{}
	base.Referrals = pos.NoReferrals{}
	return base
}

// PosJobDeps is the pos service's deps in the worker (the bill-claim
// sweep): PosDeps over the job deps.
func PosJobDeps(d Deps) pos.Deps {
	pd := pos.Deps{Config: d.Config, Clock: d.Clock, Log: d.Log, Settings: d.Settings}
	if d.Pool != nil {
		pd.DB = db.New(d.Pool)
	}
	return PosDeps(pd)
}

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

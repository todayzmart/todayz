package jobs

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/notify"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/pos"
	"github.com/todayz/backend/internal/referrals"
)

// KindPosBillClaimExpiry expires bill claims past their 120 s and releases
// the credits a confirmed one held (D-131).
const KindPosBillClaimExpiry = "pos_bill_claim_expiry"

// registerpos declares the pos domain's job kinds: the bill-claim sweep. A
// claim's reads and its sale already treat it as expired at expires_at; the
// sweep only gives the held credits back.
func registerpos(r *Registry, d Deps) {
	// The sweep's enqueuer (its notices) is built on its first run, as the
	// orders timers' is.
	enqueuer := lazyEnqueuer(r, d, "pos sweep")
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
		pd, err := PosJobDeps(d, enqueuer)
		if err != nil {
			return nil, err
		}
		s, err := pos.New(pd)
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
// must share: the stock writer, the credits ledger (W2-01) and referrals
// (W2-03). Both build their service through it, so the sweep releases a
// hold through the same ledger the routes held it with (D-131, D-132).
func PosDeps(base pos.Deps, cs *credits.Service, rs *referrals.Service) pos.Deps {
	base.Stock = PosStock(base.Clock)
	base.Credits, base.Referrals = PosCredits(cs), PosReferrals(rs)
	return base
}

// PosJobDeps is the pos service's deps in the worker (the bill-claim
// sweep): PosDeps over the job deps, with the worker's own notices. enq
// builds the worker's enqueuer on first use (nil without a pool).
func PosJobDeps(d Deps, enq func() (platformjobs.Enqueuer, error)) (pos.Deps, error) {
	pd := pos.Deps{Config: d.Config, Clock: d.Clock, Log: d.Log, Settings: d.Settings}
	var e platformjobs.Enqueuer
	if enq != nil {
		var err error
		if e, err = enq(); err != nil {
			return pos.Deps{}, err
		}
	}
	nd := notify.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log, Jobs: e}
	if d.Pool != nil {
		pool := db.New(d.Pool)
		pd.DB, nd.DB, nd.Staff = pool, pool, business.NewStaffResolver(pool)
	}
	ns, err := notify.New(nd)
	if err != nil {
		return pos.Deps{}, err
	}
	cs, err := CreditsService(d, ns)
	if err != nil {
		return pos.Deps{}, err
	}
	rs, err := ReferralsService(d, ns, e)
	if err != nil {
		return pos.Deps{}, err
	}
	return PosDeps(pd, cs, rs), nil
}

// PosCredits is the credits ledger as pos.Credits (D-132). A bill claim's
// hold is the credits.RefBillClaim ref of the claim; the sale's cashback is
// the credits.RefPOSSale ref of the sale, posted on its own so a replayed
// sale finds it posted (cashback:pos_sale:<id>). Capture refuses a released
// hold (RefuseReleased) and posts no cashback: that comes with Cashback.
func PosCredits(s *credits.Service) pos.Credits { return posCredits{s: s} }

type posCredits struct{ s *credits.Service }

func claimRef(id ids.ID) credits.Ref { return credits.Ref{Type: credits.RefBillClaim, ID: id} }

func saleRef(id ids.ID) credits.Ref { return credits.Ref{Type: credits.RefPOSSale, ID: id} }

func (c posCredits) Allow(ctx context.Context, customerID, storeID ids.ID, requested int, eligiblePaise int64) (pos.CreditsAllowance, error) {
	a, err := c.s.Allow(ctx, customerID, storeID, requested, eligiblePaise)
	return pos.CreditsAllowance{Allowed: a.Allowed, Limit: string(a.Limit)}, err
}

func (c posCredits) Hold(ctx context.Context, customerID, storeID, claimID ids.ID, n int, eligiblePaise int64) (int, error) {
	a, err := c.s.Hold(ctx, credits.HoldRequest{
		Ref: claimRef(claimID), CustomerID: customerID, StoreID: storeID, Credits: n, EligiblePaise: eligiblePaise,
	})
	return a.Allowed, err
}

func (c posCredits) Capture(ctx context.Context, claimID, customerID, storeID ids.ID) error {
	none := 0
	_, err := c.s.Complete(ctx, credits.Completion{
		Ref: claimRef(claimID), CustomerID: customerID, StoreID: storeID, CashbackBP: &none, RefuseReleased: true,
	})
	if errors.Is(err, credits.ErrReleased) {
		return pos.ErrHoldReleased
	}
	return err
}

func (c posCredits) Release(ctx context.Context, claimID ids.ID) error {
	_, err := c.s.Release(ctx, claimRef(claimID))
	return err
}

func (c posCredits) Cashback(ctx context.Context, in pos.Cashback) (int, error) {
	bp := in.CashbackBP
	done, err := c.s.Complete(ctx, credits.Completion{
		Ref: saleRef(in.SaleID), CustomerID: in.CustomerID, StoreID: in.StoreID,
		EligiblePaise: in.PaidPaise, CashbackBP: &bp,
	})
	return done.Cashback, err
}

func (c posCredits) ReverseSale(ctx context.Context, r pos.SaleReversal) error {
	rv := credits.Reversal{Ref: saleRef(r.SaleID), CustomerID: r.CustomerID, StoreID: r.StoreID}
	if r.ClaimID != ids.Zero {
		rv.Spend = claimRef(r.ClaimID)
	}
	_, err := c.s.Reverse(ctx, rv)
	return err
}

// PosReferrals is referrals as pos.Referrals: a linked sale is a
// referrals.RefPOSSale outcome, passed in the sale's transaction; the
// rewards post after it commits (D-169).
func PosReferrals(s *referrals.Service) pos.Referrals { return posReferrals{s: s} }

type posReferrals struct{ s *referrals.Service }

func (r posReferrals) Qualify(ctx context.Context, p pos.Purchase) error {
	return r.s.OnPurchaseCompleted(ctx, referrals.Outcome{
		RefType: referrals.RefPOSSale, RefID: p.SaleID, CustomerID: p.CustomerID, StoreID: p.StoreID,
		EligiblePaise: p.EligiblePaise, CreditsUsed: p.CreditsUsed, LinkMethod: p.LinkMethod,
	})
}

func (r posReferrals) Revoke(ctx context.Context, _, saleID ids.ID) error {
	return r.s.OnPurchaseReversed(ctx, referrals.RefPOSSale, saleID)
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

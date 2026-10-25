package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/payouts"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

const payoutsRunbook = "docs/runbooks/payouts.md"

// registerpayouts declares W3-01's monthly statement close: on the 1st at
// 00:30 IST it closes the month before (01 §9). A rerun skips the
// businesses already closed, so it never computes or pays twice; paying is
// finance's mark paid, never the job.
func registerpayouts(r *Registry, d Deps) {
	var (
		mu  sync.Mutex
		svc *payouts.Service
	)
	service := func() (*payouts.Service, error) {
		mu.Lock()
		defer mu.Unlock()
		if svc != nil {
			return svc, nil
		}
		if d.Pool == nil {
			return nil, errors.New("payouts: no database")
		}
		s, err := PayoutsService(d, nil)
		if err != nil {
			return nil, err
		}
		svc = s
		return svc, nil
	}
	platformjobs.Define(r, platformjobs.Kind{
		Name: payouts.KindClose, Class: platformjobs.ClassMoney,
		Description: "Closes last month's statements per business and computes each one's payout, held while KYC, a risk flag or a suspension is open (01 §9, D-09)",
		Schedule:    "30 0 1 * *", UniqueBy: platformjobs.PerMonth,
		Owner: "W3-01", Runbook: payoutsRunbook, Timeout: 10 * time.Minute, Batched: true, MaxAttempts: 3,
		Handler: func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			s, err := service()
			if err != nil {
				return platformjobs.Result{}, err
			}
			at := run.PeriodStart
			if at.IsZero() {
				at = d.Clock.Now()
			}
			res, err := s.CloseMonth(ctx, payouts.ClosePeriod(at))
			return platformjobs.Result{Summary: res.Summary()}, err
		},
	})
}

// PayoutsService builds the payouts service on d's pool: the audit writer,
// the credits ledger for mark paid and open (may be nil) for the export.
func PayoutsService(d Deps, open payouts.OpenPayee) (*payouts.Service, error) {
	pd := payouts.Deps{Clock: d.Clock, Open: open}
	if d.Pool != nil {
		pool := db.New(d.Pool)
		var hashKey []byte
		if d.Config != nil {
			hashKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		pd.DB, pd.Audit = pool, audit.New(pool, d.Clock, hashKey)
		cs, err := CreditsService(d, nil)
		if err != nil {
			return nil, err
		}
		pd.Ledger = PayoutsLedger(cs)
	}
	return payouts.New(pd)
}

// PayoutsLedger is the credits Poster as payouts.Ledger: one payout
// transaction keyed by the payout id, each store's store_credit down by its
// line and platform_payouts_cash up by the total.
func PayoutsLedger(s *credits.Service) payouts.Ledger { return payoutsLedger{s: s} }

type payoutsLedger struct{ s *credits.Service }

func (l payoutsLedger) Pay(ctx context.Context, p payouts.Payment) (ids.ID, error) {
	legs := make([]credits.Leg, 0, len(p.Lines)+1)
	var total int64
	for _, ln := range p.Lines {
		legs = append(legs, credits.Leg{Account: credits.Account{Kind: credits.StoreCredit, Owner: ln.StoreID}, Amount: -ln.Credits})
		total += ln.Credits
	}
	legs = append(legs, credits.Leg{Account: credits.Account{Kind: credits.PlatformPayoutsCash}, Amount: total})
	posted, err := l.s.Post(ctx, credits.Posting{
		Kind: credits.TxPayout, RefType: "payout", RefID: p.PayoutID,
		IdempotencyKey: credits.KeyPrefix[credits.TxPayout] + p.PayoutID.String(),
		CreatedBy:      p.By, Memo: p.Memo, Legs: legs,
	})
	switch {
	case errors.Is(err, credits.ErrUnavailable):
		return ids.Zero, fmt.Errorf("%w: %w", payouts.ErrUnavailable, err)
	case err != nil:
		return ids.Zero, err
	}
	return posted.TransactionID, nil
}

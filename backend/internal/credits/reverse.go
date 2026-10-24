package credits

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/creditsdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
)

// Reverse implements Reverser (01 §7, D-08, D-74, D-132). Under the
// customer's and the store's account locks, then the spend's hold, it posts
// reverse:<Ref>: the captured credits leave the store's account and go back
// to the lots they were spent from, the part on a lot that is due by now to
// platform_expired. Then, reading the available balance after that restore
// (D-74), it posts clawback:<Ref>: the cashback paid on Ref, up to that
// balance, from the wallet back to the store; the store absorbs the rest.
// reverse:<Ref> is posted (with no entries when nothing was spent) whenever
// there is anything to reverse, so it marks the reversal: a repeat returns
// what it posted, and Complete on Ref is refused from then on.
func (s *Service) Reverse(ctx context.Context, r Reversal) (Reversed, error) {
	spend := r.spend()
	if !r.Ref.valid() || !spend.valid() || r.CustomerID == ids.Zero || r.StoreID == ids.Zero {
		return Reversed{}, ErrInvalid
	}
	if err := s.dbReady(ctx); err != nil {
		return Reversed{}, err
	}
	var out Reversed
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q, now := s.q(ctx), s.now()
		c := r.CustomerID
		acc, err := s.lock(ctx, q, now, held(c), wallet(c), store(r.StoreID))
		if err != nil {
			return err
		}
		w, st := acc[wallet(c)].ID, acc[store(r.StoreID)].ID
		revKey := r.Ref.key("reverse")
		if _, err := q.TransactionByKey(ctx, revKey); err == nil {
			out, err = s.reversed(ctx, q, r, w, st)
			return err
		} else if !db.IsNoRows(err) {
			return wrap("reversal key", err)
		}
		h, hasHold, err := s.holdByRef(ctx, q, spend)
		if err != nil {
			return err
		}
		var spent []portion
		if hasHold {
			if h.CustomerID != ids.UUID(c) || h.StoreID != ids.UUID(r.StoreID) {
				return ErrInvalid
			}
			if h, err = q.LockHold(ctx, h.ID); err != nil {
				return wrap("lock hold", err)
			}
			switch h.Status {
			case holdHeld:
				return ErrHoldOpen
			case holdCaptured:
				if spent, err = s.capturedPortions(ctx, q, h); err != nil {
					return err
				}
			}
		}
		cb, err := s.cashbackPaid(ctx, q, r.Ref, w, st)
		if err != nil {
			return err
		}
		if len(spent) == 0 && cb == 0 {
			return nil
		}
		back, _, backN, expiredN := splitDue(spent, now)
		var es []entry
		if n := backN + expiredN; n > 0 {
			es = append(es, entry{Account: st, Amount: -int64(n)})
			if backN > 0 {
				es = append(es, entry{Account: w, Amount: int64(backN)})
			}
			if expiredN > 0 {
				es = append(es, entry{Account: platformIDs[PlatformExpired], Platform: true, Amount: int64(expiredN)})
			}
		}
		txID, dup, err := s.post(ctx, q, now, txSpec{Kind: TxReversal, Ref: r.Ref, Key: revKey, Entries: es})
		if err != nil {
			return err
		}
		if dup {
			return errors.New("credits: reversal posted under the lock that found none")
		}
		// A due lot keeps what it had: its share went to platform_expired.
		for _, p := range back {
			if err := s.consume(ctx, q, now, ids.UUID(p.Lot), txID, h.ID, "restore", p.N, 0); err != nil {
				return err
			}
		}
		out.Restored, out.Expired = backN, expiredN
		if cb == 0 {
			return nil
		}
		avail, err := q.SpendableCredits(ctx, creditsdb.SpendableCreditsParams{CustomerID: ids.UUID(c), Now: now})
		if err != nil {
			return wrap("available", err)
		}
		x := min(cb, int(avail))
		out.ClawedBack, out.Shortfall = x, cb-x
		if x == 0 {
			return nil
		}
		rows, err := q.SpendableLots(ctx, ids.UUID(c))
		if err != nil {
			return wrap("lots", err)
		}
		ps, ok := takeFIFO(lotStates(rows), x, now)
		if !ok {
			return errors.New("credits: lots short of the spendable balance")
		}
		txID, _, err = s.post(ctx, q, now, txSpec{Kind: TxReversal, Ref: r.Ref, Key: r.Ref.key("clawback"), Entries: []entry{
			{Account: w, Amount: -int64(x)}, {Account: st, Amount: int64(x)},
		}})
		if err != nil {
			return err
		}
		for _, p := range ps {
			if err := s.consume(ctx, q, now, ids.UUID(p.Lot), txID, pgtype.UUID{}, "clawback", -p.N, 0); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Reversed{}, err
	}
	return out, nil
}

// capturedPortions are what hold h's capture spent, per lot.
func (s *Service) capturedPortions(ctx context.Context, q *creditsdb.Queries, h creditsdb.CreditHold) ([]portion, error) {
	rows, err := q.CapturedPortions(ctx, h.ID)
	if err != nil {
		return nil, wrap("captured portions", err)
	}
	ps := make([]portion, 0, len(rows))
	for _, r := range rows {
		id, _ := ids.FromUUID(r.LotID)
		ps = append(ps, portion{Lot: id, N: int(r.N), ExpiresAt: r.ExpiresAt})
	}
	if sumPortions(ps) != int(h.Captured) {
		return nil, errors.New("credits: captured portions do not match the hold")
	}
	return ps, nil
}

// cashbackPaid is the cashback posted on ref, which must have gone from the
// store's account st to the wallet w (else the reversal names the wrong
// customer or store: ErrInvalid).
func (s *Service) cashbackPaid(ctx context.Context, q *creditsdb.Queries, ref Ref, w, st pgtype.UUID) (int, error) {
	legs, err := q.PostedLegs(ctx, ref.key("cashback"))
	if err != nil {
		return 0, wrap("cashback legs", err)
	}
	cb := 0
	for _, l := range legs {
		switch {
		case l.Amount > 0 && l.AccountID == w:
			cb += int(l.Amount)
		case l.Amount < 0 && l.AccountID == st:
		default:
			return 0, ErrInvalid
		}
	}
	return cb, nil
}

// reversed is what the reversal of r posted, read back for a repeat.
func (s *Service) reversed(ctx context.Context, q *creditsdb.Queries, r Reversal, w, st pgtype.UUID) (Reversed, error) {
	amount := func(key string, k AccountKind) (int, error) {
		n, err := q.KeyAmount(ctx, creditsdb.KeyAmountParams{IdempotencyKey: key, Kind: string(k)})
		return int(n), wrap("reversal amount", err)
	}
	var out Reversed
	var err error
	if out.Restored, err = amount(r.Ref.key("reverse"), CustomerWallet); err != nil {
		return out, err
	}
	if out.Expired, err = amount(r.Ref.key("reverse"), PlatformExpired); err != nil {
		return out, err
	}
	clawed, err := amount(r.Ref.key("clawback"), CustomerWallet)
	if err != nil {
		return out, err
	}
	cb, err := s.cashbackPaid(ctx, q, r.Ref, w, st)
	if err != nil {
		return out, err
	}
	out.ClawedBack = -clawed
	out.Shortfall = cb - out.ClawedBack
	return out, nil
}

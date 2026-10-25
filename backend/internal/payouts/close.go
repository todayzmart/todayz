package payouts

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/payoutsdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// The statement line kinds (ck_payout_statement_lines_kind), in CSV order.
const (
	lineOpening     = "opening"
	lineCreditsIn   = "credits_in"
	lineReferralIn  = "referral_in"
	lineCashbackOut = "cashback_out"
	lineReversals   = "reversals"
	lineAdjustments = "adjustments"
	linePayoutsOut  = "payouts_out"
	lineClosing     = "closing"
)

// lineKind maps a ledger transaction kind to its statement column. Anything
// that is not a capture, referral, cashback, reversal or payout (an admin
// adjustment, today none touch a store) is an adjustment.
func lineKind(tx string) string {
	switch tx {
	case "spend_capture":
		return lineCreditsIn
	case "referral_reward":
		return lineReferralIn
	case "cashback":
		return lineCashbackOut
	case "reversal":
		return lineReversals
	case "payout":
		return linePayoutsOut
	}
	return lineAdjustments
}

// totals are a statement's columns, credits. cashback_out and payouts_out
// are what left the stores (positive); the rest are signed.
type totals struct {
	Opening, CreditsIn, ReferralIn, CashbackOut, Reversals, Adjustments, PayoutsOut, Closing int64
}

func (t *totals) add(kind string, amount int64) {
	switch kind {
	case lineOpening:
		t.Opening += amount
	case lineCreditsIn:
		t.CreditsIn += amount
	case lineReferralIn:
		t.ReferralIn += amount
	case lineCashbackOut:
		t.CashbackOut += amount
	case lineReversals:
		t.Reversals += amount
	case lineAdjustments:
		t.Adjustments += amount
	case linePayoutsOut:
		t.PayoutsOut += amount
	case lineClosing:
		t.Closing += amount
	}
}

// ties is 01 §9's identity (ck_payout_statements_ties).
func (t totals) ties() bool {
	return t.Opening+t.CreditsIn+t.ReferralIn-t.CashbackOut+t.Reversals+t.Adjustments-t.PayoutsOut == t.Closing
}

// storeStatement is one store's lines for the month: kind → amount, count.
type storeStatement struct {
	StoreID ids.ID
	Amount  map[string]int64
	Count   map[string]int32
	Closing int64
}

// CloseResult counts what one close did.
type CloseResult struct {
	Closed, Skipped, Empty, Payouts, Held, Cancelled int
}

// Summary is the job run's summary.
func (r CloseResult) Summary() map[string]int64 {
	return map[string]int64{
		"closed": int64(r.Closed), "skipped": int64(r.Skipped), "empty": int64(r.Empty),
		"payouts": int64(r.Payouts), "held": int64(r.Held), "cancelled": int64(r.Cancelled),
	}
}

// ClosePeriod returns the month a run at t closes: the IST month before the
// one holding t. The month is stepped in IST: stepping the UTC instant of
// 1 July 00:00 IST (30 June 18:30 UTC) would land on 31 May IST.
func ClosePeriod(t time.Time) time.Time {
	return clock.StartOfMonthIST(t).In(clock.IST).AddDate(0, -1, 0)
}

// CloseMonth writes the statement of the IST month holding month for every
// business with a store credit account and no statement for it yet, and
// computes each one's payout (D-09: at most one open payout per business,
// recomputed at each close). Each business is one transaction; a rerun
// skips the businesses it did, so it never computes a payout twice.
func (s *Service) CloseMonth(ctx context.Context, month time.Time) (CloseResult, error) {
	var res CloseResult
	if err := s.writeReady(ctx); err != nil {
		return res, err
	}
	period := periodDate(month.In(clock.IST))
	_, to := monthBounds(period)
	if to.After(s.clock.Now()) {
		return res, fmt.Errorf("payouts: %s has not ended", periodKey(period))
	}
	bs, err := s.q(ctx).BusinessesToClose(ctx, period)
	if err != nil {
		return res, fmt.Errorf("payouts: businesses to close: %w", err)
	}
	for _, b := range bs {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if err := s.closeBusiness(ctx, mustID(b), period, &res); err != nil {
			return res, err
		}
		// Each business commits on its own, so a run that yields here (the
		// worker draining) resumes with the rest: closed ones are not listed.
		if err := platformjobs.Yield(ctx); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Service) closeBusiness(ctx context.Context, business ids.ID, period pgtype.Date, res *CloseResult) error {
	from, to := monthBounds(period)
	var r CloseResult
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		r = CloseResult{}
		q, now := s.q(ctx), s.now()
		bid := ids.UUID(business)
		open, err := q.LockOpenPayout(ctx, bid)
		hasOpen := err == nil
		if err != nil && !db.IsNoRows(err) {
			return fmt.Errorf("payouts: open payout: %w", err)
		}
		rows, err := q.StoreLedger(ctx, payoutsdb.StoreLedgerParams{FromAt: from, BusinessID: bid, ToAt: to})
		if err != nil {
			return fmt.Errorf("payouts: store ledger: %w", err)
		}
		stores, sum := storeStatements(rows)
		if quiet(stores) {
			r.Empty = 1
			return nil
		}
		if !sum.ties() {
			return fmt.Errorf("payouts: statement of %s does not tie", business)
		}
		sid := s.newID(now)
		err = q.InsertStatement(ctx, payoutsdb.InsertStatementParams{
			ID: ids.UUID(sid), BusinessID: bid, Period: period,
			Opening: sum.Opening, CreditsIn: sum.CreditsIn, CashbackOut: sum.CashbackOut, ReferralIn: sum.ReferralIn,
			Adjustments: sum.Adjustments, Reversals: sum.Reversals, PayoutsOut: sum.PayoutsOut, Closing: sum.Closing,
			Now: &now,
		})
		if db.IsUniqueViolation(err, "ux_payout_statements_business_period") {
			r.Skipped = 1
			return errSkipped
		}
		if err != nil {
			return fmt.Errorf("payouts: statement: %w", err)
		}
		for _, st := range stores {
			for _, k := range []string{lineOpening, lineCreditsIn, lineReferralIn, lineCashbackOut, lineReversals, lineAdjustments, linePayoutsOut, lineClosing} {
				a, n := st.Amount[k], st.Count[k]
				if a == 0 && n == 0 && k != lineOpening && k != lineClosing {
					continue
				}
				if err := q.InsertStatementLine(ctx, payoutsdb.InsertStatementLineParams{
					StatementID: ids.UUID(sid), StoreID: ids.UUID(st.StoreID), Kind: k, Amount: a, Count: n,
				}); err != nil {
					return fmt.Errorf("payouts: statement line: %w", err)
				}
			}
		}
		r.Closed = 1
		later, err := q.HasLaterStatement(ctx, payoutsdb.HasLaterStatementParams{BusinessID: bid, Period: period})
		if err != nil {
			return fmt.Errorf("payouts: later statement: %w", err)
		}
		after := map[string]any{"statement_id": sid.String(), "period": periodKey(period), "closing_credits": sum.Closing}
		if !later {
			p, err := s.computePayout(ctx, q, now, business, sid, stores, sum, open, hasOpen, &r)
			if err != nil {
				return err
			}
			if p != ids.Zero {
				after["payout_id"] = p.String()
			}
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorSystem, Action: "payouts.statement_close",
			SubjectType: "business", SubjectID: business.String(), After: after,
		})
		return err
	})
	if errors.Is(err, errSkipped) {
		err = nil
	}
	if err != nil {
		return err
	}
	res.Closed += r.Closed
	res.Skipped += r.Skipped
	res.Empty += r.Empty
	res.Payouts += r.Payouts
	res.Held += r.Held
	res.Cancelled += r.Cancelled
	return nil
}

// errSkipped rolls back a close another run did first.
var errSkipped = errors.New("payouts: statement already closed")

// quiet is a month with no entries and nothing carried in: no statement.
func quiet(ss []storeStatement) bool {
	for _, s := range ss {
		if s.Amount[lineOpening] != 0 {
			return false
		}
		for _, c := range s.Count {
			if c > 0 {
				return false
			}
		}
	}
	return true
}

// storeStatements folds StoreLedger's rows (store, transaction kind) into
// each store's lines and the business's totals.
func storeStatements(rows []payoutsdb.StoreLedgerRow) ([]storeStatement, totals) {
	var (
		out []storeStatement
		sum totals
	)
	for _, r := range rows {
		id := mustID(r.StoreID)
		if len(out) == 0 || out[len(out)-1].StoreID != id {
			out = append(out, storeStatement{StoreID: id, Amount: map[string]int64{}, Count: map[string]int32{}})
		}
		st := &out[len(out)-1]
		st.Amount[lineOpening] += r.Before
		k := lineKind(r.Kind)
		amount := r.Amount
		if k == lineCashbackOut || k == linePayoutsOut {
			amount = -amount
		}
		st.Amount[k] += amount
		st.Count[k] += r.N
		st.Closing += r.Before + r.Amount
	}
	for i := range out {
		st := &out[i]
		st.Amount[lineClosing] = st.Closing
		for k, a := range st.Amount {
			sum.add(k, a)
		}
	}
	return out, sum
}

// computePayout sets the business's open payout from this close: the
// amount every store can pay, held while a cause is open, cancelled when
// nothing is payable.
func (s *Service) computePayout(ctx context.Context, q *payoutsdb.Queries, now time.Time, business, statement ids.ID,
	stores []storeStatement, sum totals, open payoutsdb.Payout, hasOpen bool, r *CloseResult,
) (ids.ID, error) {
	bid := ids.UUID(business)
	balances, err := q.StoreAccounts(ctx, bid)
	if err != nil {
		return ids.Zero, fmt.Errorf("payouts: store balances: %w", err)
	}
	lines := payable(stores, balances, sum.Closing)
	var amount int64
	for _, l := range lines {
		amount += l.Credits
	}
	if amount <= 0 {
		if !hasOpen {
			return ids.Zero, nil
		}
		if err := q.CancelPayout(ctx, payoutsdb.CancelPayoutParams{Now: now, LastStatementID: ids.UUID(statement), ID: open.ID}); err != nil {
			return ids.Zero, fmt.Errorf("payouts: cancel payout: %w", err)
		}
		if err := q.DeletePayoutLines(ctx, open.ID); err != nil {
			return ids.Zero, fmt.Errorf("payouts: payout lines: %w", err)
		}
		r.Cancelled = 1
		return mustID(open.ID), nil
	}
	c, err := s.causes(ctx, q, business)
	if err != nil {
		return ids.Zero, err
	}
	status, reason, note := statusPending, c.reason(), (*string)(nil)
	if hasOpen && open.Status == statusHeld {
		// A close never lifts a hold (unhold is finance's, A-11): a manual
		// hold keeps its note; an automatic one takes the cause open now, or
		// keeps its own when none is. TODO(lead): lift an automatic hold
		// whose cause has closed?
		switch {
		case *open.HoldReason == holdManual:
			reason, note = holdManual, open.HoldNote
		case reason == "":
			reason = *open.HoldReason
		}
	}
	var hr *string
	if reason != "" {
		status, hr = statusHeld, &reason
		r.Held = 1
	}
	var id ids.ID
	if hasOpen {
		id = mustID(open.ID)
		if err := q.RecomputePayout(ctx, payoutsdb.RecomputePayoutParams{
			Amount: amount, Status: status, HoldReason: hr, HoldNote: note, Now: now,
			LastStatementID: ids.UUID(statement), ID: open.ID,
		}); err != nil {
			return ids.Zero, fmt.Errorf("payouts: recompute payout: %w", err)
		}
		if err := q.DeletePayoutLines(ctx, open.ID); err != nil {
			return ids.Zero, fmt.Errorf("payouts: payout lines: %w", err)
		}
	} else {
		id = s.newID(now)
		if err := q.InsertPayout(ctx, payoutsdb.InsertPayoutParams{
			ID: ids.UUID(id), BusinessID: bid, Amount: amount, Status: status, HoldReason: hr, Now: now,
			LastStatementID: ids.UUID(statement),
		}); err != nil {
			return ids.Zero, fmt.Errorf("payouts: payout: %w", err)
		}
	}
	for _, l := range lines {
		if err := q.InsertPayoutLine(ctx, payoutsdb.InsertPayoutLineParams{PayoutID: ids.UUID(id), StoreID: ids.UUID(l.StoreID), Amount: l.Credits}); err != nil {
			return ids.Zero, fmt.Errorf("payouts: payout line: %w", err)
		}
	}
	if err := q.SetStatementPayout(ctx, payoutsdb.SetStatementPayoutParams{PayoutID: ids.UUID(id), Amount: &amount, ID: ids.UUID(statement)}); err != nil {
		return ids.Zero, fmt.Errorf("payouts: statement payout: %w", err)
	}
	r.Payouts = 1
	telemetry.Logger(ctx).InfoContext(ctx, "payouts: payout computed", "business_id", business.String(),
		"payout_id", id.String(), "status", status)
	return id, nil
}

// payable splits what the business can be paid across its stores. A store
// pays at most what it held at the close and what it holds now (activity
// after midnight can only lower it), never below zero; the business is paid
// at most its net closing, so one store's negative balance is carried
// forward against the others (01 §9). The amount is taken from the stores
// in id order. TODO(lead): conservative reading of "the payable balance".
func payable(stores []storeStatement, balances []payoutsdb.StoreAccountsRow, closing int64) []Line {
	now := map[ids.ID]int64{}
	var current int64
	for _, b := range balances {
		now[mustID(b.StoreID)] = b.Balance
		current += b.Balance
	}
	type share struct {
		id ids.ID
		n  int64
	}
	var caps []share
	var capacity int64
	for _, st := range stores {
		n := min(st.Closing, now[st.StoreID])
		if n > 0 {
			caps = append(caps, share{st.StoreID, n})
			capacity += n
		}
	}
	slices.SortFunc(caps, func(a, b share) int { return slices.Compare(a.id[:], b.id[:]) })
	left := min(closing, current, capacity)
	var out []Line
	for _, c := range caps {
		if left <= 0 {
			break
		}
		n := min(c.n, left)
		out = append(out, Line{StoreID: c.id, Credits: n})
		left -= n
	}
	return out
}

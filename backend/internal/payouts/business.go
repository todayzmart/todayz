package payouts

import (
	"context"
	"encoding/base64"
	"slices"

	api "github.com/todayz/backend/gen/api/admin-payouts"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/payoutsdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// B-10: the business's own statements. stores is the member's store limit
// (nil: every store, D-125): a limited member sees only those stores' lines
// and totals, and no payout, which is the whole business's (conservative
// reading, accepted by the lead 25 Oct).

// BusinessStatements lists a business's statements, newest month first.
func (s *Service) BusinessStatements(ctx context.Context, business ids.ID, stores []ids.ID, limit *int, cursor *string) (api.BusinessStatementList, error) {
	if err := s.readReady(ctx); err != nil {
		return api.BusinessStatementList{}, err
	}
	lim := pageLimit(limit)
	p := payoutsdb.BusinessStatementsParams{BusinessID: ids.UUID(business), Lim: int32(lim + 1)} // #nosec G115 -- pageLimit clamps lim to [1, maxLimit]
	if cursor != nil && *cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(*cursor)
		if err != nil {
			return api.BusinessStatementList{}, badCursor()
		}
		if p.Before, err = parsePeriod("query.cursor", string(b)); err != nil {
			return api.BusinessStatementList{}, badCursor()
		}
	}
	q := s.q(ctx)
	rows, err := q.BusinessStatements(ctx, p)
	if err != nil {
		return api.BusinessStatementList{}, internal("statements", err)
	}
	out := api.BusinessStatementList{Items: []api.BusinessStatement{}, ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			c := base64.RawURLEncoding.EncodeToString([]byte(periodKey(rows[i-1].Period)))
			out.NextCursor = &c
			break
		}
		st, _, err := s.businessStatement(ctx, q, payoutsdb.BusinessStatementRow(r), stores, false)
		if err != nil {
			return api.BusinessStatementList{}, err
		}
		out.Items = append(out.Items, st)
	}
	return out, nil
}

// BusinessStatement is one month's statement by store.
func (s *Service) BusinessStatement(ctx context.Context, business ids.ID, stores []ids.ID, period string) (api.BusinessStatementDetail, error) {
	if err := s.readReady(ctx); err != nil {
		return api.BusinessStatementDetail{}, err
	}
	pd, err := parsePeriod("path.period", period)
	if err != nil {
		return api.BusinessStatementDetail{}, err
	}
	q := s.q(ctx)
	r, err := q.BusinessStatement(ctx, payoutsdb.BusinessStatementParams{BusinessID: ids.UUID(business), Period: pd})
	if db.IsNoRows(err) {
		return api.BusinessStatementDetail{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return api.BusinessStatementDetail{}, internal("statement", err)
	}
	st, lines, err := s.businessStatement(ctx, q, r, stores, true)
	if err != nil {
		return api.BusinessStatementDetail{}, err
	}
	return api.BusinessStatementDetail{Statement: st, Stores: lines, ServerNow: s.now()}, nil
}

// BusinessStatementCSV is BusinessStatement as the CSV, one row per store.
func (s *Service) BusinessStatementCSV(ctx context.Context, business ids.ID, stores []ids.ID, period string) ([]byte, error) {
	d, err := s.BusinessStatement(ctx, business, stores, period)
	if err != nil {
		return nil, err
	}
	return statementCSV(d.Statement.Period, d.Stores), nil
}

// businessStatement builds one statement; the store lines are read when the
// caller wants them or the member is limited to some stores.
func (s *Service) businessStatement(ctx context.Context, q *payoutsdb.Queries, r payoutsdb.BusinessStatementRow, stores []ids.ID, withLines bool) (api.BusinessStatement, []api.StatementStore, error) {
	t := totals{
		Opening: r.Opening, CreditsIn: r.CreditsIn, ReferralIn: r.ReferralIn, CashbackOut: r.CashbackOut,
		Reversals: r.Reversals, Adjustments: r.Adjustments, PayoutsOut: r.PayoutsOut, Closing: r.Closing,
	}
	var lines []api.StatementStore
	if withLines || stores != nil {
		rows, err := q.StatementLines(ctx, r.ID)
		if err != nil {
			return api.BusinessStatement{}, nil, internal("statement lines", err)
		}
		var sub totals
		lines, sub = storeTotals(rows, stores)
		if stores != nil {
			t = sub
		}
	}
	out := api.BusinessStatement{Period: periodKey(r.Period), Totals: apiTotals(t)}
	if r.ClosedAt != nil {
		out.ClosedAt = r.ClosedAt.UTC()
	}
	// F: each statement shows its own payout. Once a later close recomputed
	// (or cancelled) the payout, this statement's amount went on into it, so
	// it reads carried with what this close computed; only the payout's last
	// statement shows the live state.
	switch {
	case stores != nil || r.PayoutStatus == nil || r.PayoutAmount == nil:
	case !r.PayoutCurrent && r.ComputedPayout != nil:
		out.Payout = &api.BusinessPayoutState{AmountPaise: paise(*r.ComputedPayout), Status: api.BusinessPayoutStateStatusCarried}
	case r.PayoutCurrent && *r.PayoutStatus != statusCancelled:
		ps := &api.BusinessPayoutState{AmountPaise: paise(*r.PayoutAmount), Status: api.BusinessPayoutStateStatusProcessing}
		switch {
		case *r.PayoutStatus == statusPaid:
			ps.Status, ps.Utr, ps.PaidAt = api.BusinessPayoutStateStatusPaid, r.PayoutUtr, utc(r.PayoutPaidAt)
		case r.PayoutHoldReason != nil && (*r.PayoutHoldReason == holdKYC || *r.PayoutHoldReason == holdDetails):
			ps.Status = api.BusinessPayoutStateStatusNeedsKyc
		}
		out.Payout = ps
	}
	return out, lines, nil
}

// storeTotals groups a statement's lines by store (name order), keeping only
// the stores in allowed when it is not nil, and sums what it kept.
func storeTotals(rows []payoutsdb.StatementLinesRow, allowed []ids.ID) ([]api.StatementStore, totals) {
	out := []api.StatementStore{}
	var sum totals
	var cur *totals
	var entries int32
	flush := func() {
		if cur != nil {
			out[len(out)-1].Totals = apiTotals(*cur)
			out[len(out)-1].Entries = int(entries)
		}
	}
	var last ids.ID
	for _, r := range rows {
		id := mustID(r.StoreID)
		if allowed != nil && !slices.Contains(allowed, id) {
			continue
		}
		if cur == nil || id != last {
			flush()
			out = append(out, api.StatementStore{StoreId: id.String(), StoreName: r.StoreName})
			cur, last, entries = &totals{}, id, 0
		}
		cur.add(r.Kind, r.Amount)
		sum.add(r.Kind, r.Amount)
		if r.Kind != lineOpening && r.Kind != lineClosing {
			entries += r.Count
		}
	}
	flush()
	return out, sum
}

func apiTotals(t totals) api.StatementTotals {
	return api.StatementTotals{
		OpeningPaise: paise(t.Opening), CreditsInPaise: paise(t.CreditsIn), ReferralInPaise: paise(t.ReferralIn),
		CashbackOutPaise: paise(t.CashbackOut), ReversalsPaise: paise(t.Reversals), AdjustmentsPaise: paise(t.Adjustments),
		PayoutsOutPaise: paise(t.PayoutsOut), ClosingPaise: paise(t.Closing),
	}
}

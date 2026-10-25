package payouts

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgtype"

	api "github.com/todayz/backend/gen/api/admin-payouts"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/payoutsdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

const (
	defaultLimit = 20  //settings:ok the contract's Limit default
	maxLimit     = 100 //settings:ok the contract's Limit maximum
	maxReason    = 500 //settings:ok contract bound (AdminPayoutNote.reason ≤ 500)
)

// utrPattern is a bank's UTR or UPI reference as ck_payouts_utr stores it.
// TODO(lead): NEFT/RTGS UTRs are 16–22 letters and digits, UPI RRNs 12
// digits; 6–30 upper-case letters and digits takes both.
var utrPattern = regexp.MustCompile(`^[A-Z0-9]{6,30}$`)

// --- statements ------------------------------------------------------------------

// Statements lists one period's statements in business name order (A-11).
func (s *Service) Statements(ctx context.Context, period string, limit *int, cursor *string) (api.AdminStatementList, error) {
	if err := s.readReady(ctx); err != nil {
		return api.AdminStatementList{}, err
	}
	pd, err := parsePeriod("query.period", period)
	if err != nil {
		return api.AdminStatementList{}, err
	}
	lim := pageLimit(limit)
	p := payoutsdb.AdminStatementsParams{Period: pd, Lim: int32(lim + 1)} //nolint:gosec // lim ≤ maxLimit
	if cursor != nil && *cursor != "" {
		name, id, err := nameCursor(*cursor)
		if err != nil {
			return api.AdminStatementList{}, err
		}
		p.CursorName, p.CursorID = &name, ids.UUID(id)
	}
	rows, err := s.q(ctx).AdminStatements(ctx, p)
	if err != nil {
		return api.AdminStatementList{}, internal("statements", err)
	}
	out := api.AdminStatementList{Items: []api.AdminStatement{}, Period: periodKey(pd), ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			c := base64.RawURLEncoding.EncodeToString([]byte(rows[i-1].BrandName + "\x00" + mustID(rows[i-1].ID).String()))
			out.NextCursor = &c
			break
		}
		out.Items = append(out.Items, adminStatement(payoutsdb.AdminStatementRow(r)))
	}
	return out, nil
}

// Statement is one statement with its per-store lines.
func (s *Service) Statement(ctx context.Context, id ids.ID) (api.AdminStatementDetail, error) {
	if err := s.readReady(ctx); err != nil {
		return api.AdminStatementDetail{}, err
	}
	q := s.q(ctx)
	r, err := q.AdminStatement(ctx, ids.UUID(id))
	if db.IsNoRows(err) {
		return api.AdminStatementDetail{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return api.AdminStatementDetail{}, internal("statement", err)
	}
	lines, err := q.StatementLines(ctx, r.ID)
	if err != nil {
		return api.AdminStatementDetail{}, internal("statement lines", err)
	}
	stores, _ := storeTotals(lines, nil)
	return api.AdminStatementDetail{Statement: adminStatement(r), Stores: stores, ServerNow: s.now()}, nil
}

// StatementCSV is Statement as the CSV, one row per store.
func (s *Service) StatementCSV(ctx context.Context, id ids.ID) ([]byte, error) {
	d, err := s.Statement(ctx, id)
	if err != nil {
		return nil, err
	}
	return statementCSV(d.Statement.Period, d.Stores), nil
}

func adminStatement(r payoutsdb.AdminStatementRow) api.AdminStatement {
	out := api.AdminStatement{
		Id: mustID(r.ID).String(), BusinessId: mustID(r.BusinessID).String(), BusinessName: r.BrandName,
		BusinessStatus: r.BusinessStatus, KycStatus: api.PayoutKycStatus(r.KycStatus), OpenRiskFlag: r.OpenRiskFlag,
		Period: periodKey(r.Period), Totals: apiTotals(totals{
			Opening: r.Opening, CreditsIn: r.CreditsIn, ReferralIn: r.ReferralIn, CashbackOut: r.CashbackOut,
			Reversals: r.Reversals, Adjustments: r.Adjustments, PayoutsOut: r.PayoutsOut, Closing: r.Closing,
		}),
	}
	if r.ClosedAt != nil {
		out.ClosedAt = r.ClosedAt.UTC()
	}
	if r.PayoutID.Valid && r.PayoutStatus != nil && r.PayoutAmount != nil {
		out.Payout = &api.AdminPayoutBrief{
			Id: mustID(r.PayoutID).String(), Status: api.PayoutStatus(*r.PayoutStatus),
			AmountPaise: paise(*r.PayoutAmount), Utr: r.PayoutUtr, HoldReason: holdCause(r.PayoutHoldReason),
		}
	}
	return out
}

// --- payouts -----------------------------------------------------------------------

// Payouts lists payouts newest first; period and status filter when given.
func (s *Service) Payouts(ctx context.Context, period *string, status *string, limit *int, cursor *string) (api.AdminPayoutList, error) {
	if err := s.readReady(ctx); err != nil {
		return api.AdminPayoutList{}, err
	}
	lim := pageLimit(limit)
	p := payoutsdb.PayoutRowsParams{Status: status, Lim: int32(lim + 1)} //nolint:gosec // lim ≤ maxLimit
	if period != nil {
		pd, err := parsePeriod("query.period", *period)
		if err != nil {
			return api.AdminPayoutList{}, err
		}
		p.Period = pd
	}
	if cursor != nil && *cursor != "" {
		at, id, err := timeCursor(*cursor)
		if err != nil {
			return api.AdminPayoutList{}, err
		}
		p.CursorAt, p.CursorID = &at, ids.UUID(id)
	}
	rows, err := s.q(ctx).PayoutRows(ctx, p)
	if err != nil {
		return api.AdminPayoutList{}, internal("payouts", err)
	}
	out := api.AdminPayoutList{Items: []api.AdminPayout{}, ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			prev := rows[i-1]
			c := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(prev.CreatedAt.UnixNano(), 10) + "." + mustID(prev.ID).String()))
			out.NextCursor = &c
			break
		}
		out.Items = append(out.Items, adminPayout(payoutsdb.Payout{
			ID: r.ID, BusinessID: r.BusinessID, Amount: r.Amount, Status: r.Status, HoldReason: r.HoldReason,
			HoldNote: r.HoldNote, ExportedAt: r.ExportedAt, ComputedAt: r.ComputedAt, LastStatementID: r.LastStatementID,
			Utr: r.Utr, PaidAt: r.PaidAt, PaidBy: r.PaidBy, TransactionID: r.TransactionID, CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt, Version: r.Version,
		}, r.BrandName, r.StatementPeriod))
	}
	return out, nil
}

// Payout is one payout with its store lines, the causes open now and the
// payee's masked details (last 4 only, D-44, D-85).
func (s *Service) Payout(ctx context.Context, id ids.ID) (api.AdminPayoutDetail, error) {
	if err := s.readReady(ctx); err != nil {
		return api.AdminPayoutDetail{}, err
	}
	return s.payoutDetail(ctx, s.q(ctx), id)
}

func (s *Service) payoutDetail(ctx context.Context, q *payoutsdb.Queries, id ids.ID) (api.AdminPayoutDetail, error) {
	r, err := q.PayoutDetail(ctx, ids.UUID(id))
	if db.IsNoRows(err) {
		return api.AdminPayoutDetail{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return api.AdminPayoutDetail{}, internal("payout", err)
	}
	lines, err := q.PayoutLines(ctx, r.ID)
	if err != nil {
		return api.AdminPayoutDetail{}, internal("payout lines", err)
	}
	c, err := s.causes(ctx, q, mustID(r.BusinessID))
	if err != nil {
		return api.AdminPayoutDetail{}, internal("payout", err)
	}
	out := api.AdminPayoutDetail{
		Payout: adminPayout(payoutsdb.Payout{
			ID: r.ID, BusinessID: r.BusinessID, Amount: r.Amount, Status: r.Status, HoldReason: r.HoldReason,
			HoldNote: r.HoldNote, ExportedAt: r.ExportedAt, ComputedAt: r.ComputedAt, LastStatementID: r.LastStatementID,
			Utr: r.Utr, PaidAt: r.PaidAt, PaidBy: r.PaidBy, TransactionID: r.TransactionID, CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt, Version: r.Version,
		}, r.BrandName, r.StatementPeriod),
		Causes:    api.AdminPayoutCauses{BusinessSuspended: c.Suspended, KycVerified: c.KYCVerified, OpenRiskFlag: c.RiskFlag},
		Lines:     []api.AdminPayoutLine{},
		ServerNow: s.now(),
	}
	for _, l := range lines {
		out.Lines = append(out.Lines, api.AdminPayoutLine{StoreId: mustID(l.StoreID).String(), StoreName: l.StoreName, AmountPaise: paise(l.Amount)})
	}
	// admin.payout.bank_view has the same grants as admin.payout.view, so
	// every reader of the payout gets the masked block (W3-01 report).
	kyc := "missing"
	if r.KycStatus != nil {
		kyc = *r.KycStatus
	}
	out.Payee = &api.AdminPayoutPayee{
		KycStatus: api.PayoutKycStatus(kyc), UpiIdLast4: r.UpiIDLast4, BankAccountLast4: r.BankAccountLast4, Ifsc: r.BankIfsc,
	}
	return out, nil
}

func adminPayout(p payoutsdb.Payout, business string, period pgtype.Date) api.AdminPayout {
	out := api.AdminPayout{
		Id: mustID(p.ID).String(), BusinessId: mustID(p.BusinessID).String(), BusinessName: business,
		AmountPaise: paise(p.Amount), Status: api.PayoutStatus(p.Status), HoldReason: holdCause(p.HoldReason),
		HoldNote: p.HoldNote, StatementId: optID(p.LastStatementID), ExportedAt: utc(p.ExportedAt),
		ComputedAt: p.ComputedAt.UTC(), Utr: p.Utr, PaidAt: utc(p.PaidAt), PaidBy: optID(p.PaidBy),
		CreatedAt: p.CreatedAt.UTC(), Version: int(p.Version),
	}
	if period.Valid {
		k := periodKey(period)
		out.Period = &k
	}
	return out
}

func holdCause(s *string) *api.PayoutHoldCause {
	if s == nil {
		return nil
	}
	c := api.PayoutHoldCause(*s)
	return &c
}

// --- hold, unhold, mark paid --------------------------------------------------------

// Hold holds a pending payout by hand (A-11): reason is the hold note.
func (s *Service) Hold(ctx context.Context, by authn.Principal, id ids.ID, reason string) (api.AdminPayoutDetail, error) {
	reason, err := cleanReason(reason)
	if err != nil {
		return api.AdminPayoutDetail{}, err
	}
	return s.change(ctx, id, func(ctx context.Context, q *payoutsdb.Queries, p payoutsdb.Payout, now time.Time) error {
		if p.Status != statusPending {
			return problem.New(genproblems.InvalidTransition, "")
		}
		m := holdManual
		if err := q.SetPayoutHold(ctx, payoutsdb.SetPayoutHoldParams{HoldReason: &m, HoldNote: &reason, Now: now, ID: p.ID}); err != nil {
			return internal("hold", err)
		}
		return s.auditPayout(ctx, by, "payouts.hold", p, map[string]any{"status": statusHeld, "hold_reason": holdManual, "reason": reason})
	})
}

// Unhold releases a held payout once no automatic cause is open.
func (s *Service) Unhold(ctx context.Context, by authn.Principal, id ids.ID, reason string) (api.AdminPayoutDetail, error) {
	reason, err := cleanReason(reason)
	if err != nil {
		return api.AdminPayoutDetail{}, err
	}
	return s.change(ctx, id, func(ctx context.Context, q *payoutsdb.Queries, p payoutsdb.Payout, now time.Time) error {
		if p.Status != statusHeld {
			return problem.New(genproblems.InvalidTransition, "")
		}
		c, err := s.causes(ctx, q, mustID(p.BusinessID))
		if err != nil {
			return internal("unhold", err)
		}
		if c.open() {
			return problem.New(genproblems.PayoutHoldCauseOpen, c.reason())
		}
		if err := q.ClearPayoutHold(ctx, payoutsdb.ClearPayoutHoldParams{Now: now, ID: p.ID}); err != nil {
			return internal("unhold", err)
		}
		return s.auditPayout(ctx, by, "payouts.unhold", p, map[string]any{"status": statusPending, "reason": reason})
	})
}

// MarkPaid records that the bank paid a pending payout: the posting moves
// each store's line from store_credit to platform_payouts_cash, in the same
// transaction as the payout row and the audit row. amountPaise is the amount
// the admin saw; a payout recomputed since is refused, not paid. A retry with
// the same UTR answers the paid payout; it never posts twice.
func (s *Service) MarkPaid(ctx context.Context, by authn.Principal, id ids.ID, utr string, amountPaise int, reason string) (api.AdminPayoutDetail, error) {
	reason, err := cleanReason(reason)
	if err != nil {
		return api.AdminPayoutDetail{}, err
	}
	utr = strings.ToUpper(strings.TrimSpace(utr))
	if !utrPattern.MatchString(utr) {
		return api.AdminPayoutDetail{}, fieldErr("body.utr", "6 to 30 letters and digits")
	}
	if s.ledger == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "payouts: the ledger is not configured")
		return api.AdminPayoutDetail{}, problem.Wrap(genproblems.ServiceUnavailable, ErrUnavailable)
	}
	return s.change(ctx, id, func(ctx context.Context, q *payoutsdb.Queries, p payoutsdb.Payout, now time.Time) error {
		switch {
		case p.Status == statusPaid && p.Utr != nil && *p.Utr == utr:
			return errAlreadyDone
		case p.Status == statusPaid:
			return problem.New(genproblems.PayoutAlreadyPaid, "")
		case p.Status != statusPending:
			return problem.New(genproblems.PayoutNotPayable, "")
		case paise(p.Amount) != amountPaise:
			return problem.New(genproblems.PayoutAmountChanged, "")
		}
		c, err := s.causes(ctx, q, mustID(p.BusinessID))
		if err != nil {
			return internal("mark paid", err)
		}
		if c.open() {
			return problem.New(genproblems.PayoutHoldCauseOpen, c.reason())
		}
		u := utr
		if other, err := q.PayoutByUTR(ctx, &u); err == nil && other != p.ID {
			return problem.New(genproblems.UtrInUse, "")
		} else if err != nil && !db.IsNoRows(err) {
			return internal("utr", err)
		}
		rows, err := q.PayoutLines(ctx, p.ID)
		if err != nil {
			return internal("payout lines", err)
		}
		pay := Payment{PayoutID: mustID(p.ID), By: by.UserID, Memo: "payout " + utr}
		var total int64
		for _, l := range rows {
			pay.Lines = append(pay.Lines, Line{StoreID: mustID(l.StoreID), Credits: l.Amount})
			total += l.Amount
		}
		if total != p.Amount || len(pay.Lines) == 0 {
			return internal("mark paid", errors.New("payout lines do not add up to the payout"))
		}
		tx, err := s.ledger.Pay(ctx, pay)
		if err != nil {
			return internal("posting", err)
		}
		err = q.MarkPayoutPaid(ctx, payoutsdb.MarkPayoutPaidParams{Utr: &u, Now: &now, PaidBy: ids.UUID(by.UserID), TransactionID: ids.UUID(tx), ID: p.ID})
		if db.IsUniqueViolation(err, "ux_payouts_utr") {
			return problem.New(genproblems.UtrInUse, "")
		}
		if err != nil {
			return internal("mark paid", err)
		}
		return s.auditPayout(ctx, by, "payouts.mark_paid", p, map[string]any{
			"status": statusPaid, "utr": utr, "amount_credits": p.Amount, "transaction_id": tx.String(), "reason": reason,
		})
	})
}

// errAlreadyDone ends a change that an earlier identical request made.
var errAlreadyDone = errors.New("payouts: already done")

// change locks the payout, applies fn in one transaction and answers the
// payout as it is after.
func (s *Service) change(ctx context.Context, id ids.ID, fn func(context.Context, *payoutsdb.Queries, payoutsdb.Payout, time.Time) error) (api.AdminPayoutDetail, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminPayoutDetail{}, err
	}
	var out api.AdminPayoutDetail
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q, now := s.q(ctx), s.now()
		p, err := q.LockPayout(ctx, ids.UUID(id))
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return internal("payout", err)
		}
		if err := fn(ctx, q, p, now); err != nil && !errors.Is(err, errAlreadyDone) {
			return err
		}
		out, err = s.payoutDetail(ctx, q, id)
		return err
	})
	if err != nil {
		return api.AdminPayoutDetail{}, internal("payout", err)
	}
	return out, nil
}

func (s *Service) auditPayout(ctx context.Context, by authn.Principal, action string, p payoutsdb.Payout, after map[string]any) error {
	before := map[string]any{"status": p.Status, "amount_credits": p.Amount, "version": p.Version}
	if p.HoldReason != nil {
		before["hold_reason"] = *p.HoldReason
	}
	_, err := s.audit.Write(ctx, audit.Entry{
		ActorKind: audit.ActorAdmin, ActorID: by.UserID, Action: action,
		SubjectType: "payout", SubjectID: mustID(p.ID).String(), Before: before, After: after,
	})
	if err != nil {
		return internal("audit", err)
	}
	return nil
}

// --- export -------------------------------------------------------------------------

// Export is the bank/UPI batch CSV of a period's pending payouts with the
// payee fields decrypted (D-85), formula-safe cells (D-93). A payout whose
// business has a cause open now is left out, as a held one is. The rows are
// stamped exported_at and the audit row holds their ids only.
func (s *Service) Export(ctx context.Context, by authn.Principal, period string) ([]byte, error) {
	if err := s.writeReady(ctx); err != nil {
		return nil, err
	}
	pd, err := parsePeriod("query.period", period)
	if err != nil {
		return nil, err
	}
	if s.open == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "payouts: the export needs PII_ENC_KEY")
		return nil, problem.Wrap(genproblems.ServiceUnavailable, ErrUnavailable)
	}
	var out []byte
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q, now := s.q(ctx), s.now()
		rows, err := q.ExportRows(ctx, pd)
		if err != nil {
			return internal("export", err)
		}
		var buf bytes.Buffer
		w := newCSV(&buf)
		w.row("payout_id", "period", "business_id", "legal_name", "beneficiary_name", "amount_inr", "upi_id", "bank_account_number", "ifsc")
		var stamped []pgtype.UUID
		var paid []string
		for _, r := range rows {
			bid := mustID(r.BusinessID)
			c, err := s.causes(ctx, q, bid)
			if err != nil {
				return internal("export", err)
			}
			if c.open() {
				continue
			}
			upi, err := s.openField(PayeeUPI, bid, r.UpiIDEnc)
			if err != nil {
				return internal("export", err)
			}
			bank, err := s.openField(PayeeBankAccount, bid, r.BankAccountEnc)
			if err != nil {
				return internal("export", err)
			}
			w.row(mustID(r.ID).String(), periodKey(r.StatementPeriod), bid.String(), r.LegalName, deref(r.BeneficiaryName),
				inr(paise(r.Amount)), upi, bank, deref(r.BankIfsc))
			stamped = append(stamped, r.ID)
			paid = append(paid, mustID(r.ID).String())
		}
		if len(stamped) > 0 {
			if err := q.StampExported(ctx, payoutsdb.StampExportedParams{Now: &now, Ids: stamped}); err != nil {
				return internal("export", err)
			}
		}
		if _, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: by.UserID, Action: "payouts.export",
			SubjectType: "payout_period", SubjectID: periodKey(pd), After: map[string]any{"payout_ids": paid, "count": len(paid)},
		}); err != nil {
			return internal("audit", err)
		}
		out = buf.Bytes()
		return nil
	})
	if err != nil {
		return nil, internal("export", err)
	}
	return out, nil
}

func (s *Service) openField(f PayeeField, business ids.ID, sealed []byte) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	return s.open(f, business, sealed)
}

// --- helpers --------------------------------------------------------------------------

func cleanReason(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > maxReason {
		return "", fieldErr("body.reason", "1 to "+strconv.Itoa(maxReason)+" characters")
	}
	if strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }) >= 0 {
		return "", fieldErr("body.reason", "must not hold control characters")
	}
	return s, nil
}

func pageLimit(limit *int) int {
	if limit == nil {
		return defaultLimit
	}
	return min(max(*limit, 1), maxLimit)
}

func badCursor() error { return fieldErr("query.cursor", "not a cursor from this list") }

func nameCursor(c string) (string, ids.ID, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", ids.Zero, badCursor()
	}
	name, sid, ok := strings.Cut(string(b), "\x00")
	id, err := ids.Parse(sid)
	if !ok || err != nil {
		return "", ids.Zero, badCursor()
	}
	return name, id, nil
}

func timeCursor(c string) (time.Time, ids.ID, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, ids.Zero, badCursor()
	}
	ns, sid, ok := strings.Cut(string(b), ".")
	n, err1 := strconv.ParseInt(ns, 10, 64)
	id, err2 := ids.Parse(sid)
	if !ok || err1 != nil || err2 != nil {
		return time.Time{}, ids.Zero, badCursor()
	}
	return time.Unix(0, n).UTC(), id, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

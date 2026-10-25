// Package payouts is W3-01's monthly statements and payouts (01 §9; D-09,
// D-85, D-93): the close on the 1st at 00:30 IST, the holds, A-11's mark paid
// with a UTR and the bank/UPI export, and the business's statements (B-10).
//
// A statement never moves money: it is read from the store_credit ledger of
// the business's stores in the IST month. A payout moves money only when
// finance marks it paid, through the credits ledger (Ledger), in the same
// transaction as the payout row and its audit row. Amounts here are credits
// (1 credit = 100 paise), as in the ledger; the API shows paise.
package payouts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/payoutsdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/money"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// KindClose is the monthly statement close's job kind.
const KindClose = "payouts_statement_close"

// ErrUnavailable is a missing collaborator (the database, the audit writer,
// the ledger or the payee key): the routes answer 503.
var ErrUnavailable = errors.New("payouts: not configured")

// The payout statuses and hold causes (03 payouts).
const (
	statusPending   = "pending"
	statusHeld      = "held"
	statusPaid      = "paid"
	statusCancelled = "cancelled"

	holdSuspended = "business_suspended"
	holdKYC       = "kyc_incomplete"
	holdRisk      = "risk_flag"
	holdManual    = "manual"
)

// PayeeField names a sealed payee field of the business's KYC row.
type PayeeField string

// The sealed payee fields the export opens.
const (
	PayeeUPI         PayeeField = "upi_id"
	PayeeBankAccount PayeeField = "bank_account"
)

// OpenPayee opens one sealed KYC field of a business (W1-04 seals them per
// business). Its error never carries the value.
type OpenPayee func(f PayeeField, business ids.ID, sealed []byte) (string, error)

// Line is one store's share of a payment, in credits.
type Line struct {
	StoreID ids.ID
	Credits int64
}

// Payment is a payout's posting: each store's store_credit down by its line,
// platform_payouts_cash up by the total. PayoutID makes it idempotent.
type Payment struct {
	PayoutID ids.ID
	By       ids.ID
	Lines    []Line
	Memo     string
}

// Ledger posts a payout in the caller's transaction (the credits Poster,
// adapted in internal/jobs/payouts.go). A repeat of the same payout posts
// nothing and returns the first transaction.
type Ledger interface {
	Pay(ctx context.Context, p Payment) (transactionID ids.ID, err error)
}

// Deps are the service's collaborators.
type Deps struct {
	DB     *db.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Ledger Ledger
	// Open decrypts the payee fields for the export; without it the export
	// answers 503.
	Open OpenPayee
}

// Service implements the statements, payouts and their routes.
type Service struct {
	db     *db.DB
	clock  clock.Clock
	audit  *audit.Writer
	ledger Ledger
	open   OpenPayee
	ids    *ids.Generator
}

// New builds the service. Only the clock is required; a missing database,
// audit writer or ledger is a 503 on the routes that need it.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("payouts: a clock is required")
	}
	return &Service{db: d.DB, clock: d.Clock, audit: d.Audit, ledger: d.Ledger, open: d.Open, ids: ids.NewGenerator()}, nil
}

func (s *Service) readReady(ctx context.Context) error {
	if s.db == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "payouts: not configured (needs the database)")
		return problem.Wrap(genproblems.ServiceUnavailable, ErrUnavailable)
	}
	return nil
}

// writeReady is the database and the audit writer every change needs.
func (s *Service) writeReady(ctx context.Context) error {
	if err := s.readReady(ctx); err != nil {
		return err
	}
	if s.audit == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "payouts: the audit writer is not configured")
		return problem.Wrap(genproblems.ServiceUnavailable, ErrUnavailable)
	}
	return nil
}

// now is the clock's time at the database's precision.
func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) q(ctx context.Context) *payoutsdb.Queries { return payoutsdb.New(s.db.Q(ctx)) }

func (s *Service) newID(now time.Time) ids.ID { return s.ids.New(now) }

// --- small helpers ---------------------------------------------------------------

// causes are the automatic hold causes now (01 §9).
type causes struct {
	Suspended, KYCVerified, RiskFlag bool
}

func (c causes) open() bool { return c.Suspended || !c.KYCVerified || c.RiskFlag }

// reason is the hold reason the open causes give, in this order: a suspended
// business, then KYC, then a risk flag; "" when none is open.
func (c causes) reason() string {
	switch {
	case c.Suspended:
		return holdSuspended
	case !c.KYCVerified:
		return holdKYC
	case c.RiskFlag:
		return holdRisk
	}
	return ""
}

func (s *Service) causes(ctx context.Context, q *payoutsdb.Queries, business ids.ID) (causes, error) {
	r, err := q.HoldCauses(ctx, ids.UUID(business))
	if err != nil {
		return causes{}, fmt.Errorf("payouts: hold causes: %w", err)
	}
	return causes{Suspended: r.Suspended, KYCVerified: r.KycVerified, RiskFlag: r.RiskFlag}, nil
}

func mustID(u pgtype.UUID) ids.ID {
	id, _ := ids.FromUUID(u)
	return id
}

func optID(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := mustID(u).String()
	return &s
}

func paise(credits int64) int { return int(credits * money.PaisePerCredit) }

// periodKey is a statement period as the API names it ("2026-09").
func periodKey(d pgtype.Date) string { return d.Time.Format("2006-01") }

func periodDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC), Valid: true}
}

// parsePeriod reads "2006-01" as the first of that month; path is the field
// the error names.
func parsePeriod(path, s string) (pgtype.Date, error) {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return pgtype.Date{}, fieldErr(path, "not a month (YYYY-MM)")
	}
	return periodDate(t), nil
}

// monthBounds is the IST month of period as [from, to).
func monthBounds(period pgtype.Date) (from, to time.Time) {
	from = time.Date(period.Time.Year(), period.Time.Month(), 1, 0, 0, 0, 0, clock.IST)
	return from.UTC(), from.AddDate(0, 1, 0).UTC()
}

func fieldErr(path, reason string) *problem.Error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

func internal(what string, err error) error {
	var p *problem.Error
	if errors.As(err, &p) {
		return p
	}
	if errors.Is(err, ErrUnavailable) {
		return problem.Wrap(genproblems.ServiceUnavailable, err)
	}
	return problem.Wrap(genproblems.Internal, fmt.Errorf("payouts: %s: %w", what, err))
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

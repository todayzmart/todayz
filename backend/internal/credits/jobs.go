package credits

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/gen/sqlc/creditsdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// The job kinds (internal/jobs/credits.go registers them).
const (
	KindExpire    = "credits_expire"
	KindRemind    = "credits_expiry_reminder"
	KindReconcile = "ledger_reconcile"
)

// A job run works at most jobBatches batches of jobBatch customers, so one
// attempt stays well inside its timeout; the next run carries on.
const (
	jobBatch   = 100
	jobBatches = 20
)

// The reconcile checks (03 §6), the `reason` label of tz_ledger_reconcile_drift.
const (
	DriftUnbalanced     = "unbalanced"       // a transaction whose entries do not sum to zero
	DriftNegative       = "negative"         // a customer wallet, held or lot amount below zero
	DriftLotsVsBalances = "lots_vs_balances" // Σ lot remaining ≠ wallet, or Σ lot held ≠ held
	DriftHolds          = "holds"            // a hold whose amounts break its status
	DriftHeldVsHolds    = "held_vs_holds"    // Σ open holds ≠ the customer's held balance
	DriftCacheDrift     = "cache_drift"      // account_balances ≠ Σ entries
	DriftLotDrift       = "lot_drift"        // a lot ≠ original + Σ its consumptions
)

func init() {
	telemetry.RegisterLabelValues("reason", DriftUnbalanced, DriftNegative, DriftLotsVsBalances,
		DriftHolds, DriftHeldVsHolds, DriftCacheDrift, DriftLotDrift,
		string(LimitStoreCap), string(LimitMonthlyCap))
}

var (
	metricsOnce sync.Once
	mDrift      metric.Int64Gauge
	mCapRefused metric.Int64Counter
	mExpired    metric.Int64Counter
	mFlagFailed metric.Int64Counter
)

func instruments() {
	metricsOnce.Do(func() {
		m := otel.Meter("todayz/credits")
		mDrift, _ = m.Int64Gauge("tz_ledger_reconcile_drift",
			metric.WithDescription("Rows breaking a ledger invariant at the last reconcile, by check; anything above 0 pages"))
		mCapRefused, _ = m.Int64Counter("tz_credits_cap_refusals_total",
			metric.WithDescription("Credit holds refused by a cap (D-30, D-76), by cap"))
		mExpired, _ = m.Int64Counter("tz_credits_expired_total",
			metric.WithDescription("Credits expired by the expiry job"))
		mFlagFailed, _ = m.Int64Counter("tz_credits_risk_flag_failures_total",
			metric.WithDescription("Cap-refusal risk flags not written (an error or the flag timeout); the refusal stands"))
	})
}

func countCapRefused(ctx context.Context, l Limit) {
	instruments()
	if mCapRefused != nil {
		mCapRefused.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", telemetry.LabelValue("reason", string(l)))))
	}
}

func countFlagFailed(ctx context.Context) {
	instruments()
	if mFlagFailed != nil {
		mFlagFailed.Add(ctx, 1)
	}
}

// ExpireDue expires every due lot (credits_expire). Holds and spends expire
// a customer's due lots inline too, so this job only has to keep balances
// honest for customers who do nothing. One transaction per customer.
func (s *Service) ExpireDue(ctx context.Context) (customers, credits int, err error) {
	if err := s.ready(ctx); err != nil {
		return 0, 0, err
	}
	for range jobBatches {
		now := s.now()
		due, err := s.q(ctx).DueCustomers(ctx, creditsdb.DueCustomersParams{Now: now, Lim: jobBatch})
		if err != nil {
			return customers, credits, wrap("due customers", err)
		}
		for _, cu := range due {
			c := mustID(cu)
			n := 0
			err := s.db.InTx(ctx, func(ctx context.Context) error {
				q := s.q(ctx)
				acc, err := s.lock(ctx, q, now, wallet(c), held(c))
				if err != nil {
					return err
				}
				n, err = s.expireDue(ctx, q, now, c, acc[wallet(c)].ID)
				return err
			})
			if err != nil {
				return customers, credits, err
			}
			customers++
			credits += n
		}
		if len(due) < jobBatch {
			break
		}
	}
	instruments()
	if mExpired != nil && credits > 0 {
		mExpired.Add(ctx, int64(credits))
	}
	return customers, credits, nil
}

// RemindExpiring sends each customer one "credits expiring" notice for the
// lots that expire within credit_expiry_reminder_days (01 §7) and marks
// those lots reminded, so a lot is reminded once.
func (s *Service) RemindExpiring(ctx context.Context) (int, error) {
	r, err := s.Rules(ctx)
	if err != nil {
		return 0, err
	}
	if s.notices == nil {
		return 0, errors.New("credits: no notification sender for the reminder")
	}
	sent := 0
	for range jobBatches {
		now := s.now()
		until := now.Add(time.Duration(r.ReminderDays) * 24 * time.Hour)
		cs, err := s.q(ctx).RemindCustomers(ctx, creditsdb.RemindCustomersParams{Now: now, Until: until, Lim: jobBatch})
		if err != nil {
			return sent, wrap("remind customers", err)
		}
		progress := 0
		for _, cu := range cs {
			ok, err := s.remind(ctx, mustID(cu), now, until)
			if err != nil {
				return sent, err
			}
			if ok {
				sent++
				progress++
			}
		}
		// A batch whose lots were all locked by spends reminds nobody: stop
		// and let the next run take them.
		if len(cs) < jobBatch || progress == 0 {
			break
		}
	}
	return sent, nil
}

func (s *Service) remind(ctx context.Context, c ids.ID, now, until time.Time) (bool, error) {
	ok := false
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		lots, err := q.RemindLots(ctx, creditsdb.RemindLotsParams{CustomerID: ids.UUID(c), Now: now, Until: until})
		if err != nil || len(lots) == 0 {
			return wrap("remind lots", err)
		}
		n := 0
		lotIDs := make([]pgtype.UUID, 0, len(lots))
		for _, l := range lots {
			n += int(l.Remaining)
			lotIDs = append(lotIDs, l.ID)
		}
		tx, has := db.TxFrom(ctx)
		if !has {
			return errors.New("credits: no transaction in context")
		}
		// The last day the earliest of them can be spent (01 §7: usable
		// through day X+N).
		last := lots[0].ExpiresAt.Add(-time.Nanosecond).In(clock.IST).Format("2 Jan 2006")
		if _, err := s.notices.Send(ctx, tx, notice.Notification{
			Kind: notice.KindCreditsExpiring, UserID: c,
			Params: map[string]string{"credits": strconv.Itoa(n), "date": last},
			Ref:    &notice.Ref{Type: "wallet", ID: c},
		}); err != nil {
			return wrap("credits expiring notice", err)
		}
		ok = true
		return wrap("mark reminded", q.MarkReminded(ctx, creditsdb.MarkRemindedParams{Now: &now, Ids: lotIDs}))
	})
	return ok, err
}

// Drift is one reconcile result: rows breaking each invariant.
type Drift map[string]int64

// Clean reports whether every check found nothing.
func (d Drift) Clean() bool {
	for _, n := range d {
		if n != 0 {
			return false
		}
	}
	return true
}

// Reconcile checks the ledger invariants (03 §6) and records the counts on
// tz_ledger_reconcile_drift (ledger_reconcile, nightly). It fixes nothing:
// any drift logs an error, which pages (05 §6 "Ledger invariant").
func (s *Service) Reconcile(ctx context.Context) (Drift, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	row, err := s.q(ctx).Reconcile(ctx)
	if err != nil {
		return nil, wrap("reconcile", err)
	}
	d := Drift{
		DriftUnbalanced: row.Unbalanced, DriftNegative: row.Negative, DriftLotsVsBalances: row.LotsVsBalances,
		DriftHolds: row.Holds, DriftHeldVsHolds: row.HeldVsHolds, DriftCacheDrift: row.CacheDrift, DriftLotDrift: row.LotDrift,
	}
	instruments()
	for k, n := range d {
		if mDrift != nil {
			mDrift.Record(ctx, n, metric.WithAttributes(attribute.String("reason", telemetry.LabelValue("reason", k))))
		}
	}
	if !d.Clean() {
		args := []any{"event", "ledger_invariant"}
		for k, n := range d {
			if n != 0 {
				args = append(args, k, n)
			}
		}
		telemetry.Logger(ctx).ErrorContext(ctx, "credits: ledger invariant broken", args...)
	}
	return d, nil
}

package orders

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// KindStrikesRecount recounts every customer's cached strike count and
// no-show limit once a day, so strikes that age out of the window stop
// counting even when no new strike comes (01 §5.4).
const KindStrikesRecount = "customer_strikes_recount"

// strikeCustomer records a strike against the order's customer, once per
// order, and recounts the customer's count and limit from strikes.
func (s *Service) strikeCustomer(ctx context.Context, q *ordersdb.Queries, o ordersdb.Order, reason string, now time.Time, r noShow) error {
	n, err := q.InsertStrike(ctx, ordersdb.InsertStrikeParams{
		ID: ids.UUID(s.ids.New(now)), SubjectType: "customer", SubjectID: o.CustomerID,
		OrderID: o.ID, Reason: reason, CreatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("orders: strike: %w", err)
	}
	if n == 0 {
		return nil // this order's strike is already there
	}
	rows, err := recount(ctx, q, o.CustomerID, false, now, r)
	if err != nil {
		return err
	}
	if rows == 0 {
		telemetry.Logger(ctx).WarnContext(ctx, "orders: no customer profile to record a strike on", "order_id", mustID(o.ID).String())
	}
	return nil
}

// recount recomputes strikes_count_30d and limited_until from strikes: the
// one customer's, or (every) every customer's with a count or a limit.
func recount(ctx context.Context, q *ordersdb.Queries, user pgtype.UUID, every bool, now time.Time, r noShow) (int64, error) {
	p := ordersdb.RecountCustomerStrikesParams{
		Now: now, Every: every, UserID: user, Win: interval(r.window), Lim: interval(r.limitFor),
		Strikes: int32(min(r.strikes, math.MaxInt32)), //nolint:gosec // capped just above
	}
	n, err := q.RecountCustomerStrikes(ctx, p)
	if err != nil {
		return 0, fmt.Errorf("orders: recount strikes: %w", err)
	}
	return n, nil
}

func interval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}

// RecountCustomerStrikes recomputes one customer's count and limit, for a
// strike reset (W3-05) or a test: a reset strike no longer counts.
func (s *Service) RecountCustomerStrikes(ctx context.Context, userID ids.ID) error {
	r, err := s.noShowRules(ctx)
	if err != nil {
		return err
	}
	return s.db.InTx(ctx, func(ctx context.Context) error {
		_, err := recount(ctx, s.q(ctx), ids.UUID(userID), false, s.now(), r)
		return err
	})
}

// RecountStrikes is the KindStrikesRecount handler.
func (s *Service) RecountStrikes(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
	if err := s.readyToWrite(ctx); err != nil {
		return platformjobs.Result{}, err
	}
	r, err := s.noShowRules(ctx)
	if err != nil {
		return platformjobs.Result{}, err
	}
	n, err := recount(ctx, s.q(ctx), pgtype.UUID{}, true, s.now(), r)
	if err != nil {
		return platformjobs.Result{}, err
	}
	return platformjobs.Result{Summary: map[string]int64{"recounted": n}}, nil
}

package orders

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// The orders timers (timer class, docs/runbooks/orders-timers.md).
const (
	// KindAcceptTimeout expires a placed order the shop did not answer by
	// accept_by (BK-06).
	KindAcceptTimeout = "order_accept_timeout"
	// KindNoShow expires a held pickup order nobody collected by hold_until
	// and gives the customer a strike (BK-14).
	KindNoShow = "order_pickup_no_show"
)

// Reasons the system records (03 §4.2 SystemReason).
const (
	reasonNotAccepted = "not_accepted"
	reasonNoShow      = "no_show"
)

// TimerArgs are a timer's job arguments: the order, and for the no-show
// timer the hold it was set for. Nothing else (no code, no customer).
type TimerArgs struct {
	OrderID   ids.ID     `json:"orderId"`
	HoldUntil *time.Time `json:"holdUntil,omitempty"`
}

// errEarly is a timer run before its deadline (a clock skew between the
// queue and the clock). The run fails and is retried; it changes nothing.
var errEarly = errors.New("orders: timer ran before its deadline")

// Timer outcomes, the run's summary keys.
const (
	outcomeExpired = "expired"
	outcomeNoop    = "noop"
)

func timerResult(expired bool) platformjobs.Result {
	if expired {
		return platformjobs.Result{Summary: map[string]int64{outcomeExpired: 1, outcomeNoop: 0}}
	}
	return platformjobs.Result{Summary: map[string]int64{outcomeExpired: 0, outcomeNoop: 1}}
}

// ExpireUnanswered is the KindAcceptTimeout handler. An order that is no
// longer placed (accepted, rejected, cancelled, or a second run) is left as
// it is (BK-21).
func (s *Service) ExpireUnanswered(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
	var a TimerArgs
	if err := run.Decode(&a); err != nil {
		return platformjobs.Result{}, err
	}
	if err := s.readyToWrite(ctx); err != nil {
		return platformjobs.Result{}, err
	}
	expired := false
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		o, err := s.q(ctx).LockOrder(ctx, ids.UUID(a.OrderID))
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("orders: lock order: %w", err)
		}
		if o.Status != StatusPlaced {
			return nil
		}
		if !pastAcceptBy(o, now) {
			return errEarly
		}
		expired = true
		return s.end(ctx, o, StatusExpired, reasonNotAccepted, nil, system, variant("expired"), now)
	})
	if err != nil {
		return platformjobs.Result{}, err
	}
	return timerResult(expired), nil
}

// ExpireNoShow is the KindNoShow handler: a held pickup order past its hold
// expires, its stock goes back and the customer gets a strike; enough
// strikes in the window limit them to one open order (01 §5.4). A run for a
// hold that was since extended, or for an order already finished, does
// nothing (BK-21).
func (s *Service) ExpireNoShow(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
	var a TimerArgs
	if err := run.Decode(&a); err != nil {
		return platformjobs.Result{}, err
	}
	if err := s.readyToWrite(ctx); err != nil {
		return platformjobs.Result{}, err
	}
	window, err := s.duration(ctx, settings.NoshowWindowDays)
	if err != nil {
		return platformjobs.Result{}, err
	}
	limitFor, err := s.duration(ctx, settings.NoshowLimitDays)
	if err != nil {
		return platformjobs.Result{}, err
	}
	strikes, err := s.count(ctx, settings.NoshowStrikes)
	if err != nil {
		return platformjobs.Result{}, err
	}
	expired := false
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		q := s.q(ctx)
		o, err := q.LockOrder(ctx, ids.UUID(a.OrderID))
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("orders: lock order: %w", err)
		}
		if !handable(o.Status) || o.Fulfilment != Pickup || o.HoldUntil == nil {
			return nil
		}
		if !pastHold(o, now) {
			if a.HoldUntil != nil && !a.HoldUntil.Equal(*o.HoldUntil) {
				// The hold was extended; the extension's own timer runs then.
				return nil
			}
			return errEarly
		}
		expired = true
		if err := s.end(ctx, o, StatusExpired, reasonNoShow, nil, system, generic, now); err != nil {
			return err
		}
		return s.strike(ctx, q, o, now, window, limitFor, strikes)
	})
	if err != nil {
		return platformjobs.Result{}, err
	}
	return timerResult(expired), nil
}

// strike records the no-show against the customer, once per order, and
// sets the limit when the window's count reaches the setting.
func (s *Service) strike(ctx context.Context, q *ordersdb.Queries, o ordersdb.Order, now time.Time, window, limitFor time.Duration, limit int) error {
	n, err := q.InsertStrike(ctx, ordersdb.InsertStrikeParams{
		ID: ids.UUID(s.ids.New(now)), SubjectType: "customer", SubjectID: o.CustomerID,
		OrderID: o.ID, Reason: reasonNoShow, CreatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("orders: strike: %w", err)
	}
	if n == 0 {
		return nil // this order's strike is already there
	}
	count, err := q.CountActiveStrikes(ctx, ordersdb.CountActiveStrikesParams{
		SubjectType: "customer", SubjectID: o.CustomerID, Reason: reasonNoShow, Since: now.Add(-window),
	})
	if err != nil {
		return fmt.Errorf("orders: count strikes: %w", err)
	}
	var until *time.Time
	if limit > 0 && int(count) >= limit {
		until = ptr(now.Add(limitFor))
	}
	// TODO(lead): strikes_count_30d is the count at the last strike; nothing
	// lowers it as strikes age out of the window (the limit itself is read
	// from limited_until, which is right).
	rows, err := q.SetCustomerStrikes(ctx, ordersdb.SetCustomerStrikesParams{Strikes: count, LimitedUntil: until, UserID: o.CustomerID})
	if err != nil {
		return fmt.Errorf("orders: customer strikes: %w", err)
	}
	if rows == 0 {
		telemetry.Logger(ctx).WarnContext(ctx, "orders: no customer profile to record a strike on", "order_id", mustID(o.ID).String())
	}
	return nil
}

package orders

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/settings"
)

// The orders timers (timer class, docs/runbooks/orders-timers.md).
const (
	// KindAcceptTimeout expires a placed order the shop did not answer by
	// accept_by (BK-06).
	KindAcceptTimeout = "order_accept_timeout"
	// KindNoShow expires a held pickup order nobody collected by hold_until
	// and gives the customer a strike (BK-14).
	KindNoShow = "order_pickup_no_show"
	// KindPartialTimeout cancels a partly accepted order the customer did
	// not answer by partial_confirm_by (BK-10).
	KindPartialTimeout = "order_partial_timeout"
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
	expired, err := s.expireUnanswered(ctx, a.OrderID)
	if err != nil {
		return platformjobs.Result{}, err
	}
	return timerResult(expired), nil
}

// expireUnanswered expires a placed order past accept_by in a transaction
// of its own; errEarly before it.
func (s *Service) expireUnanswered(ctx context.Context, orderID ids.ID) (bool, error) {
	expired := false
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		o, err := s.q(ctx).LockOrder(ctx, ids.UUID(orderID))
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
	return expired, err
}

// ExpirePartial is the KindPartialTimeout handler: a partly accepted order
// the customer did not answer is cancelled partial_timeout; its credits go
// back and nobody gets a strike (BK-10). An order already answered, or a
// second run, is left as it is (BK-21).
func (s *Service) ExpirePartial(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
	var a TimerArgs
	if err := run.Decode(&a); err != nil {
		return platformjobs.Result{}, err
	}
	if err := s.readyToWrite(ctx); err != nil {
		return platformjobs.Result{}, err
	}
	expired, err := s.expirePartial(ctx, a.OrderID)
	if err != nil {
		return platformjobs.Result{}, err
	}
	return timerResult(expired), nil
}

// expirePartial cancels an awaiting_customer order past partial_confirm_by
// in a transaction of its own; errEarly before it.
func (s *Service) expirePartial(ctx context.Context, orderID ids.ID) (bool, error) {
	expired := false
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		o, err := s.q(ctx).LockOrder(ctx, ids.UUID(orderID))
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("orders: lock order: %w", err)
		}
		if o.Status != StatusAwaitingCustomer {
			return nil
		}
		if !pastPartial(o, now) {
			return errEarly
		}
		expired = true
		return s.end(ctx, o, StatusCancelled, reasonPartialTimeout, nil, system, generic, now)
	})
	return expired, err
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
	rules, err := s.noShowRules(ctx)
	if err != nil {
		return platformjobs.Result{}, err
	}
	expired, err := s.expireNoShow(ctx, a.OrderID, a.HoldUntil, rules)
	if err != nil {
		return platformjobs.Result{}, err
	}
	return timerResult(expired), nil
}

// noShow is the strike rule's settings (01 §5.4).
type noShow struct {
	window, limitFor time.Duration
	strikes          int
}

func (s *Service) noShowRules(ctx context.Context) (noShow, error) {
	window, err := s.duration(ctx, settings.NoshowWindowDays)
	if err != nil {
		return noShow{}, err
	}
	limitFor, err := s.duration(ctx, settings.NoshowLimitDays)
	if err != nil {
		return noShow{}, err
	}
	strikes, err := s.count(ctx, settings.NoshowStrikes)
	if err != nil {
		return noShow{}, err
	}
	return noShow{window: window, limitFor: limitFor, strikes: strikes}, nil
}

// expireNoShow expires a held pickup order past hold_until and strikes the
// customer, in a transaction of its own; errEarly before it, unless the
// hold the timer was set for (setFor) has since moved.
func (s *Service) expireNoShow(ctx context.Context, orderID ids.ID, setFor *time.Time, r noShow) (bool, error) {
	expired := false
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		q := s.q(ctx)
		o, err := q.LockOrder(ctx, ids.UUID(orderID))
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
			if setFor != nil && !setFor.Equal(*o.HoldUntil) {
				// The hold was extended; the extension's own timer runs then.
				return nil
			}
			return errEarly
		}
		expired = true
		if err := lockStrikeCustomer(ctx, q, o.CustomerID); err != nil {
			return err
		}
		if err := s.end(ctx, o, StatusExpired, reasonNoShow, nil, system, generic, now); err != nil {
			return err
		}
		return s.strikeCustomer(ctx, q, o, reasonNoShow, now, r)
	})
	return expired, err
}

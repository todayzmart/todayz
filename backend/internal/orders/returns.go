package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	ordersapi "github.com/todayz/backend/gen/api/orders"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/settings"
)

// Delivery after the accept (BK-24–26): dispatch and its timeout, the
// failed delivery, and the return of its goods (D-69).

const (
	// KindDispatchTimeout expires an accepted delivery order the shop did
	// not dispatch by dispatch_by and gives the store a strike (BK-24).
	KindDispatchTimeout = "order_dispatch_timeout"
	// KindDeliveryReturn is a failed delivery's return step: the reminders
	// to the shop (2 h and 24 h by default, D-218), the auto-confirm at 72 h (D-69, BK-26).
	KindDeliveryReturn = "order_delivery_return"
)

// Reasons and outcomes the delivery steps record (03 §4.2).
const (
	reasonDispatchTimeout = "dispatch_timeout"

	returnReturned    = "returned"
	returnDamaged     = "damaged"
	returnUnconfirmed = "unconfirmed"
)

// The return's auto-confirm after delivery_failed_at (D-69; a constant,
// D-85). The two reminders before it are settings (D-218, returnTimes).
const (
	returnSettleAfter = 72 * time.Hour //settings:ok D-85

	// returnFlagScore is the return_unconfirmed risk flag's score (0–100).
	returnFlagScore = 50 //settings:ok D-85
)

// deliveryFailReasons are the DeliveryFailedReason values.
var deliveryFailReasons = []string{"customer_unreachable", "address_not_found", "customer_refused", "other"}

// pastDispatch: the shop's time to dispatch is over (strict: at
// dispatch_by).
func pastDispatch(o ordersdb.Order, now time.Time) bool {
	return o.DispatchBy != nil && !now.Before(*o.DispatchBy)
}

// dispatchTimer schedules the dispatch timeout at dispatch_by.
func (s *Service) dispatchTimer(ctx context.Context, orderID ids.ID, by time.Time) error {
	t, err := tx(ctx)
	if err != nil {
		return err
	}
	return s.jobs.EnqueueTx(ctx, t, KindDispatchTimeout, TimerArgs{OrderID: orderID},
		platformjobs.EnqueueOpts{ScheduledAt: by, UniqueKey: orderID.String() + ":dispatch"})
}

// returnTimer schedules the return's step (1: first reminder, 2: second,
// 3: auto-confirm) at its time after the failure.
func (s *Service) returnTimer(ctx context.Context, orderID ids.ID, step int, failedAt time.Time, rt returnTimes) error {
	t, err := tx(ctx)
	if err != nil {
		return err
	}
	return s.jobs.EnqueueTx(ctx, t, KindDeliveryReturn, TimerArgs{OrderID: orderID, Step: step},
		platformjobs.EnqueueOpts{ScheduledAt: failedAt.Add(rt.due(step)), UniqueKey: fmt.Sprintf("%s:return:%d", orderID, step)})
}

// returnTimes are the two reminders' times after the failure, from
// delivery_return_first_reminder_hours and
// delivery_return_second_reminder_hours (D-218; their bounds keep
// first < second < 72 h).
type returnTimes struct{ first, second time.Duration }

func (s *Service) returnTimes(ctx context.Context) (returnTimes, error) {
	first, err := s.duration(ctx, settings.DeliveryReturnFirstReminderHours)
	if err != nil {
		return returnTimes{}, err
	}
	second, err := s.duration(ctx, settings.DeliveryReturnSecondReminderHours)
	if err != nil {
		return returnTimes{}, err
	}
	return returnTimes{first, second}, nil
}

// due is how long after the failure a step is due.
func (rt returnTimes) due(step int) time.Duration {
	switch step {
	case 1:
		return rt.first
	case 2:
		return rt.second
	default:
		return returnSettleAfter
	}
}

// also checks a second action on the route's scope (a damaged return needs
// stock.damage on top of order.confirm_return): 403 when refused.
func (s *Service) also(ctx context.Context, action string) error {
	if s.az == nil {
		return problem.Wrap(genproblems.Internal, errors.New("orders: no authorizer"))
	}
	p, _ := authn.FromContext(ctx)
	d, ok := authz.DecisionFrom(ctx)
	if !ok {
		return problem.Wrap(genproblems.Internal, errors.New("orders: no authz decision"))
	}
	_, err := s.az.Check(ctx, p, action, d.Scope)
	return err
}

// --- staff ----------------------------------------------------------------------

// dispatch sends an accepted delivery order out, before dispatch_by
// (BK-24). The customer sees the full phone to the shop from now (BK-27).
func (s *Service) dispatch(ctx context.Context, by, storeID, orderID ids.ID) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	return asProblemOrNil(s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		o, err := s.lockOrder(ctx, orderID, ofStore(storeID))
		if err != nil {
			return err
		}
		if o.Status != StatusAccepted || o.Fulfilment != Delivery || pastDispatch(o, now) ||
			!CanMove(o.Status, StatusOutForDelivery, o.Fulfilment) {
			return invalidTransition()
		}
		v, err := s.q(ctx).SetDispatched(ctx, o.ID)
		if err != nil {
			return fmt.Errorf("orders: dispatch: %w", err)
		}
		return s.record(ctx, o, StatusOutForDelivery, v, staff(by), nil, variant("out_for_delivery"), now)
	}))
}

// failDelivery records a delivery that did not happen (BK-25): the held
// credits go back, no cashback, no stock moves; the goods stay held until
// the shop confirms they came back (confirmReturn) or the return job
// settles it at 72 h.
func (s *Service) failDelivery(ctx context.Context, by, storeID, orderID ids.ID, b *ordersapi.FailDeliveryRequest) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	if b == nil {
		return fieldErr("body", "required")
	}
	reason := string(b.Reason)
	if !slices.Contains(deliveryFailReasons, reason) {
		return fieldErr("body.reason", "not a delivery failure reason")
	}
	note, err := noteFor(reason, b.Note)
	if err != nil {
		return err
	}
	return asProblemOrNil(s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		o, err := s.lockOrder(ctx, orderID, ofStore(storeID))
		if err != nil {
			return err
		}
		// Only a delivery fails at the door; the guard does not look at the
		// fulfilment (a pickup never reaches out_for_delivery anyway).
		if o.Status != StatusOutForDelivery || o.Fulfilment != Delivery || !CanMove(o.Status, StatusDeliveryFailed, o.Fulfilment) {
			return invalidTransition()
		}
		if err := s.credits.Release(ctx, mustID(o.ID)); err != nil {
			return fmt.Errorf("orders: release credits: %w", err)
		}
		v, err := s.q(ctx).SetDeliveryFailed(ctx, ordersdb.SetDeliveryFailedParams{Reason: &reason, Note: note, Now: &now, ID: o.ID})
		if err != nil {
			return fmt.Errorf("orders: delivery failed: %w", err)
		}
		if err := s.redeemer.Release(ctx, mustID(o.ID)); err != nil {
			return fmt.Errorf("orders: release offers: %w", err)
		}
		// TODO(lead): the order_update catalogue has no "delivery_failed"
		// variant; the general text is sent.
		if err := s.record(ctx, o, StatusDeliveryFailed, v, staff(by), &reason, generic, now); err != nil {
			return err
		}
		rt, err := s.returnTimes(ctx)
		if err != nil {
			return err
		}
		return s.returnTimer(ctx, orderID, 1, now, rt)
	}))
}

// confirmReturn settles a failed delivery's goods (D-69): returned puts
// them back on sale; damaged (manager or owner: stock.damage) writes one
// damage movement per held line first. Once: a settled return is 409. The
// order stays delivery_failed.
func (s *Service) confirmReturn(ctx context.Context, by, storeID, orderID ids.ID, b *ordersapi.ConfirmReturnRequest) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	if b == nil {
		return fieldErr("body", "required")
	}
	outcome := string(b.Outcome)
	switch outcome {
	case returnReturned:
	case returnDamaged:
		if err := s.also(ctx, policy.StockDamage); err != nil {
			return err
		}
	default:
		return fieldErr("body.outcome", "returned or damaged")
	}
	return asProblemOrNil(s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		o, err := s.lockOrder(ctx, orderID, ofStore(storeID))
		if err != nil {
			return err
		}
		if o.Status != StatusDeliveryFailed || o.ReturnConfirmedAt != nil {
			return invalidTransition()
		}
		return s.settleReturn(ctx, o, outcome, staff(by), now)
	}))
}

// settleReturn writes a locked failed delivery's return outcome: for
// damaged a damage movement per held line (through the stock writer, which
// takes the stores row then the item, so the items are not locked first),
// then the held stock is released and the outcome recorded.
func (s *Service) settleReturn(ctx context.Context, o ordersdb.Order, outcome string, by actor, now time.Time) error {
	q := s.q(ctx)
	held, err := q.HeldReservations(ctx, o.ID)
	if err != nil {
		return fmt.Errorf("orders: held stock: %w", err)
	}
	if outcome == returnDamaged {
		t, err := tx(ctx)
		if err != nil {
			return err
		}
		for _, r := range held {
			if _, err := s.stock.Damage(ctx, t, Damage{
				StoreID: mustID(o.StoreID), ItemID: mustID(r.StoreItemID), Qty: r.Quantity,
				OrderID: mustID(o.ID), ClientID: mustID(r.ID), UserID: by.id,
			}); err != nil {
				return fmt.Errorf("orders: damage line: %w", err)
			}
		}
	} else if len(held) > 0 {
		// Lock the items in id order first, as end does.
		itemIDs := make([]pgtype.UUID, 0, len(held))
		for i, r := range held {
			if i == 0 || r.StoreItemID != held[i-1].StoreItemID {
				itemIDs = append(itemIDs, r.StoreItemID)
			}
		}
		if _, err := q.LockItems(ctx, itemIDs); err != nil {
			return fmt.Errorf("orders: lock items: %w", err)
		}
	}
	reason := "return_" + outcome
	if _, err := q.ReleaseReservations(ctx, ordersdb.ReleaseReservationsParams{Reason: &reason, OrderID: o.ID}); err != nil {
		return fmt.Errorf("orders: release stock: %w", err)
	}
	v, err := q.SetReturnSettled(ctx, ordersdb.SetReturnSettledParams{Outcome: &outcome, Now: &now, ConfirmedBy: by.uuid(), ID: o.ID})
	if db.IsNoRows(err) {
		return invalidTransition()
	}
	if err != nil {
		return fmt.Errorf("orders: settle return: %w", err)
	}
	if err := q.InsertOrderEvent(ctx, ordersdb.InsertOrderEventParams{
		ID: ids.UUID(s.ids.New(now)), OrderID: o.ID, FromStatus: &o.Status, ToStatus: o.Status,
		ActorKind: by.kind, ActorID: by.uuid(), Reason: &reason, Meta: emptyMeta, CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("orders: event: %w", err)
	}
	return s.publish(ctx, o, v)
}

// --- timers ---------------------------------------------------------------------

// ExpireDispatch is the KindDispatchTimeout handler: an accepted delivery
// order not dispatched by dispatch_by expires, its stock and credits go
// back, and the store (not the customer) gets a strike (BK-24). An order
// already dispatched or finished, or a second run, is left as it is.
func (s *Service) ExpireDispatch(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
	var a TimerArgs
	if err := run.Decode(&a); err != nil {
		return platformjobs.Result{}, err
	}
	if err := s.readyToWrite(ctx); err != nil {
		return platformjobs.Result{}, err
	}
	threshold, err := s.count(ctx, settings.StoreStrikeRiskThreshold)
	if err != nil {
		return platformjobs.Result{}, err
	}
	expired, err := s.expireDispatch(ctx, a.OrderID, threshold)
	if err != nil {
		return platformjobs.Result{}, err
	}
	return timerResult(expired), nil
}

// expireDispatch expires an accepted delivery order past dispatch_by and
// strikes the store, in a transaction of its own; errEarly before it.
func (s *Service) expireDispatch(ctx context.Context, orderID ids.ID, threshold int) (bool, error) {
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
		if o.Status != StatusAccepted || o.Fulfilment != Delivery || o.DispatchBy == nil {
			return nil
		}
		if !pastDispatch(o, now) {
			return errEarly
		}
		expired = true
		if err := s.end(ctx, o, StatusExpired, reasonDispatchTimeout, nil, system, variant("dispatch_timeout"), now); err != nil {
			return err
		}
		return s.strikeStore(ctx, q, o, reasonDispatchTimeout, now, threshold)
	})
	return expired, err
}

// Return step outcomes, the run's summary keys.
const (
	outcomeReminded = "reminded"
	outcomeSettled  = "settled"
)

// ReturnStep is the KindDeliveryReturn handler: the reminders (2 h and
// 24 h by default, D-218) to the store's inbox, then at 72 h the return is settled
// unconfirmed (nobody confirmed it), the held stock goes back and the store
// gets a return_unconfirmed risk flag (D-69). A settled return, or a second
// run, does nothing.
func (s *Service) ReturnStep(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
	var a TimerArgs
	if err := run.Decode(&a); err != nil {
		return platformjobs.Result{}, err
	}
	if err := s.readyToWrite(ctx); err != nil {
		return platformjobs.Result{}, err
	}
	did, err := s.returnStep(ctx, a.OrderID, a.Step)
	if err != nil {
		return platformjobs.Result{}, err
	}
	sum := map[string]int64{outcomeReminded: 0, outcomeSettled: 0, outcomeNoop: 0}
	switch did {
	case "":
		sum[outcomeNoop] = 1
	default:
		sum[did] = 1
	}
	return platformjobs.Result{Summary: sum}, nil
}

// returnStep takes the failed delivery's due step in a transaction of its
// own and reports what it did (outcomeReminded, outcomeSettled or "").
// A timer (step > 0) run before its step is due is errEarly; the sweep
// passes 0 and takes whatever is due.
func (s *Service) returnStep(ctx context.Context, orderID ids.ID, step int) (string, error) {
	did := ""
	rt, err := s.returnTimes(ctx)
	if err != nil {
		return "", err
	}
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		q := s.q(ctx)
		o, err := q.LockOrder(ctx, ids.UUID(orderID))
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("orders: lock order: %w", err)
		}
		if o.Status != StatusDeliveryFailed || o.ReturnConfirmedAt != nil || o.DeliveryFailedAt == nil {
			return nil
		}
		failed := *o.DeliveryFailedAt
		age := now.Sub(failed)
		if step > 0 && age < rt.due(step) {
			return errEarly
		}
		switch {
		case age >= returnSettleAfter:
			details, err := json.Marshal(map[string]string{"orderId": mustID(o.ID).String(), "failedAt": failed.UTC().Format(time.RFC3339)})
			if err != nil {
				return fmt.Errorf("orders: flag details: %w", err)
			}
			if err := q.InsertReturnUnconfirmedFlag(ctx, ordersdb.InsertReturnUnconfirmedFlagParams{
				ID: ids.UUID(s.ids.New(now)), StoreID: o.StoreID, Score: returnFlagScore,
				Details: details, Now: now, OrderKey: mustID(o.ID).String(),
			}); err != nil {
				return fmt.Errorf("orders: return flag: %w", err)
			}
			did = outcomeSettled
			return s.settleReturn(ctx, o, returnUnconfirmed, system, now)
		case age >= rt.second && o.ReturnRemindersSent < 2:
			did = outcomeReminded
			if err := s.remindReturn(ctx, o, 2); err != nil {
				return err
			}
			return s.returnTimer(ctx, orderID, 3, failed, rt)
		case age >= rt.first && o.ReturnRemindersSent < 1:
			did = outcomeReminded
			if err := s.remindReturn(ctx, o, 1); err != nil {
				return err
			}
			return s.returnTimer(ctx, orderID, 2, failed, rt)
		}
		return nil
	})
	return did, err
}

// remindReturn records reminder n and tells the store's inbox the return
// is waiting.
// TODO(lead): the business inbox reaches every staff member who sees the
// store; it has no role filter, so cashiers see the reminder too.
func (s *Service) remindReturn(ctx context.Context, o ordersdb.Order, n int16) error {
	q := s.q(ctx)
	rows, err := q.SetReturnReminders(ctx, ordersdb.SetReturnRemindersParams{Sent: n, ID: o.ID})
	if err != nil {
		return fmt.Errorf("orders: return reminder: %w", err)
	}
	if rows == 0 {
		return nil
	}
	st, err := q.OrderStore(ctx, o.StoreID)
	if err != nil {
		return fmt.Errorf("orders: store: %w", err)
	}
	t, err := tx(ctx)
	if err != nil {
		return err
	}
	if _, err := s.notices.Send(ctx, t, notice.Notification{
		Kind: notice.KindDeliveryReturnPending, BusinessID: mustID(st.BusinessID), StoreID: mustID(st.ID),
		Params: map[string]string{"store": clip(st.Name, maxParamRunes)},
		Ref:    &notice.Ref{Type: "order", ID: mustID(o.ID)},
	}); err != nil {
		return fmt.Errorf("orders: return notice: %w", err)
	}
	return nil
}

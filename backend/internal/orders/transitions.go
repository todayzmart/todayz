package orders

import (
	"context"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	ordersapi "github.com/todayz/backend/gen/api/orders"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/realtime"
	"github.com/todayz/backend/internal/platform/settings"
)

// Who changed an order (order_events.actor_kind).
const (
	actorCustomer = "customer"
	actorStaff    = "staff"
	actorSystem   = "system"
)

// maxParamRunes is the notification catalogue's cap on one param.
const maxParamRunes = 120

// actor is who makes a change; the system has no id.
type actor struct {
	kind string
	id   ids.ID
}

func (a actor) uuid() pgtype.UUID {
	if a.kind == actorSystem {
		return pgtype.UUID{}
	}
	return ids.UUID(a.id)
}

func staff(id ids.ID) actor    { return actor{kind: actorStaff, id: id} }
func customer(id ids.ID) actor { return actor{kind: actorCustomer, id: id} }

var system = actor{kind: actorSystem}

// tell is the customer notification a change sends: none, the kind's
// general text (variant ""), or one of its variants.
type tell struct {
	send    bool
	variant string
}

var (
	silent = tell{}
	// generic is the kind's general "your order has an update" text, for a
	// change the catalogue has no variant for (a shop's cancel, a no-show).
	// TODO(lead): the order_update catalogue (W2-05) has no "cancelled" or
	// "no_show" variant; the general text is sent for both.
	generic = tell{send: true}
)

func variant(v string) tell { return tell{send: true, variant: v} }

// --- the shared steps ---------------------------------------------------------

// lockOrder holds the order row for the rest of the transaction. An order
// the caller may not see is 404, the same as a missing one.
func (s *Service) lockOrder(ctx context.Context, id ids.ID, visible func(ordersdb.Order) bool) (ordersdb.Order, error) {
	o, err := s.q(ctx).LockOrder(ctx, ids.UUID(id))
	if db.IsNoRows(err) {
		return ordersdb.Order{}, notFound()
	}
	if err != nil {
		return ordersdb.Order{}, fmt.Errorf("orders: lock order: %w", err)
	}
	if !visible(o) {
		return ordersdb.Order{}, notFound()
	}
	return o, nil
}

func ofStore(storeID ids.ID) func(ordersdb.Order) bool {
	return func(o ordersdb.Order) bool { return mustID(o.StoreID) == storeID }
}

func ofCustomer(userID ids.ID) func(ordersdb.Order) bool {
	return func(o ordersdb.Order) bool { return mustID(o.CustomerID) == userID }
}

// pastAcceptBy: the shop's time to answer is over (strict: at accept_by).
func pastAcceptBy(o ordersdb.Order, now time.Time) bool {
	return o.AcceptBy != nil && !now.Before(*o.AcceptBy)
}

// pastHold: the pickup hold is over (strict: at hold_until).
func pastHold(o ordersdb.Order, now time.Time) bool {
	return o.HoldUntil != nil && !now.Before(*o.HoldUntil)
}

// publish tells the customer's and the store's streams the order changed.
func (s *Service) publish(ctx context.Context, o ordersdb.Order, version int32) error {
	t, err := tx(ctx)
	if err != nil {
		return err
	}
	ev := realtime.Event{Type: realtime.OrderUpdated, ID: mustID(o.ID), Version: int64(version)}
	if err := realtime.Publish(ctx, t, realtime.UserChannel(mustID(o.CustomerID)), ev); err != nil {
		return err
	}
	return realtime.Publish(ctx, t, realtime.StoreChannel(mustID(o.StoreID)), ev)
}

// record writes the status change's event, the stream events and the
// customer's notification, all in the order's transaction.
func (s *Service) record(ctx context.Context, o ordersdb.Order, to string, version int32, by actor, reason *string, t tell, now time.Time) error {
	from := o.Status
	if err := s.q(ctx).InsertOrderEvent(ctx, ordersdb.InsertOrderEventParams{
		ID: ids.UUID(s.ids.New(now)), OrderID: o.ID, FromStatus: &from, ToStatus: to,
		ActorKind: by.kind, ActorID: by.uuid(), Reason: reason, Meta: emptyMeta, CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("orders: event: %w", err)
	}
	if err := s.publish(ctx, o, version); err != nil {
		return err
	}
	if !t.send {
		return nil
	}
	st, err := s.q(ctx).OrderStore(ctx, o.StoreID)
	if err != nil {
		return fmt.Errorf("orders: store: %w", err)
	}
	params := map[string]string{"store": clip(st.Name, maxParamRunes)}
	if t.variant != "" {
		params["variant"] = t.variant
	}
	txn, err := tx(ctx)
	if err != nil {
		return err
	}
	if _, err := s.notices.Send(ctx, txn, notice.Notification{
		Kind: notice.KindOrderUpdate, UserID: mustID(o.CustomerID), Params: params,
		Ref: &notice.Ref{Type: "order", ID: mustID(o.ID)},
	}); err != nil {
		return fmt.Errorf("orders: order update notice: %w", err)
	}
	return nil
}

// end moves a locked order to rejected, expired or cancelled: it releases
// any held stock, writes the status, reason and note, and records it.
func (s *Service) end(ctx context.Context, o ordersdb.Order, to, reason string, note *string, by actor, t tell, now time.Time) error {
	if !CanMove(o.Status, to, o.Fulfilment) {
		return invalidTransition()
	}
	q := s.q(ctx)
	// Lock the held items in id order first, as accept does: one UPDATE of
	// the reservations moves store_items through the trigger in scan order,
	// which against an accept of another order on the same items could wait
	// in a cycle (a deadlock).
	held, err := q.HeldReservations(ctx, o.ID)
	if err != nil {
		return fmt.Errorf("orders: held stock: %w", err)
	}
	if len(held) > 0 {
		itemIDs := make([]pgtype.UUID, 0, len(held))
		for i, r := range held { // ordered by store_item_id
			if i == 0 || r.StoreItemID != held[i-1].StoreItemID {
				itemIDs = append(itemIDs, r.StoreItemID)
			}
		}
		if _, err := q.LockItems(ctx, itemIDs); err != nil {
			return fmt.Errorf("orders: lock items: %w", err)
		}
	}
	if _, err := q.ReleaseReservations(ctx, ordersdb.ReleaseReservationsParams{Reason: &reason, OrderID: o.ID}); err != nil {
		return fmt.Errorf("orders: release stock: %w", err)
	}
	// Always, not only when CreditsUsed > 0: a hold may be open whatever the
	// order row says, and Release is a no-op without one (D-127).
	if err := s.credits.Release(ctx, mustID(o.ID)); err != nil {
		return fmt.Errorf("orders: release credits: %w", err)
	}
	v, err := q.SetEnded(ctx, ordersdb.SetEndedParams{Status: to, Reason: &reason, Note: note, ID: o.ID})
	if err != nil {
		return fmt.Errorf("orders: end order: %w", err)
	}
	// The offers the order used go back to their limits (W2-07).
	if err := s.redeemer.Release(ctx, mustID(o.ID)); err != nil {
		return fmt.Errorf("orders: release offers: %w", err)
	}
	return s.record(ctx, o, to, v, by, &reason, t, now)
}

// noShowTimer schedules the pickup no-show check at the hold's end. The
// key carries the hold, so an extension's new timer is not swallowed by
// the first one still pending (which then finds the hold moved and does
// nothing).
func (s *Service) noShowTimer(ctx context.Context, orderID ids.ID, holdUntil time.Time) error {
	t, err := tx(ctx)
	if err != nil {
		return err
	}
	return s.jobs.EnqueueTx(ctx, t, KindNoShow, TimerArgs{OrderID: orderID, HoldUntil: &holdUntil},
		platformjobs.EnqueueOpts{ScheduledAt: holdUntil, UniqueKey: fmt.Sprintf("%s:noshow:%d", orderID, holdUntil.UnixMicro())})
}

// noteFor checks a reason's note: required for "other" (03 §4.2), 1–200
// characters when given.
func noteFor(reason string, note *string) (*string, error) {
	if note != nil && (*note == "" || utf8.RuneCountInString(*note) > 200) {
		return nil, fieldErr("body.note", "1 to 200 characters")
	}
	if reason == "other" && note == nil {
		return nil, fieldErr("body.note", "required when the reason is other")
	}
	return note, nil
}

// --- staff ----------------------------------------------------------------------

// accept takes a placed order in full (B-04): it holds every line's stock
// and starts the pickup hold. A partial accept is not built yet (BK-08–10):
// a line below what was asked is 422.
func (s *Service) accept(ctx context.Context, by ids.ID, storeID, orderID ids.ID, body *ordersapi.AcceptOrderRequest) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	hold, err := s.duration(ctx, settings.PickupHoldMin)
	if err != nil {
		return problem.Wrap(genproblems.Internal, err)
	}
	return asProblemOrNil(s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		q := s.q(ctx)
		o, err := s.lockOrder(ctx, orderID, ofStore(storeID))
		if err != nil {
			return err
		}
		if o.Status != StatusPlaced || pastAcceptBy(o, now) || !CanMove(o.Status, StatusAccepted, o.Fulfilment) {
			return invalidTransition()
		}
		lines, err := q.OrderLines(ctx, o.ID)
		if err != nil {
			return fmt.Errorf("orders: lines: %w", err)
		}
		if err := fullAccept(lines, body); err != nil {
			return err
		}

		// Lock the items in id order and check each has the stock.
		need := map[ids.ID]int32{}
		var itemIDs []pgtype.UUID
		for _, l := range lines {
			id := mustID(l.StoreItemID)
			if _, ok := need[id]; !ok {
				itemIDs = append(itemIDs, l.StoreItemID)
			}
			need[id] += l.QuantityRequested
		}
		items, err := q.LockItems(ctx, itemIDs)
		if err != nil {
			return fmt.Errorf("orders: lock items: %w", err)
		}
		for _, it := range items {
			id := mustID(it.ID)
			if avail := it.OnHand - it.Reserved; avail < need[id] {
				return problem.New(genproblems.InsufficientStock, "").
					WithExtension("storeItemId", id.String()).WithExtension("available", max(avail, 0))
			}
		}
		for _, l := range lines {
			if err := q.InsertReservation(ctx, ordersdb.InsertReservationParams{
				ID: ids.UUID(s.ids.New(now)), OrderID: o.ID, OrderItemID: l.ID, StoreItemID: l.StoreItemID,
				Quantity: l.QuantityRequested, Now: now,
			}); err != nil {
				return fmt.Errorf("orders: reserve: %w", err)
			}
		}
		holdUntil := now.Add(hold)
		v, err := q.SetAccepted(ctx, ordersdb.SetAcceptedParams{Now: &now, HoldUntil: &holdUntil, ID: o.ID})
		if err != nil {
			return fmt.Errorf("orders: accept: %w", err)
		}
		if err := q.AcceptAllLines(ctx, o.ID); err != nil {
			return fmt.Errorf("orders: accept lines: %w", err)
		}
		if err := s.record(ctx, o, StatusAccepted, v, staff(by), nil, variant("accepted"), now); err != nil {
			return err
		}
		return s.noShowTimer(ctx, orderID, holdUntil)
	}))
}

// fullAccept checks an accept's lines, when sent: each a line of the order,
// once, at the quantity asked.
func fullAccept(lines []ordersdb.OrderLinesRow, body *ordersapi.AcceptOrderRequest) error {
	if body == nil || body.Lines == nil {
		return nil
	}
	asked := make(map[string]int32, len(lines))
	for _, l := range lines {
		asked[mustID(l.ID).String()] = l.QuantityRequested
	}
	seen := map[string]bool{}
	for i, al := range *body.Lines {
		id, err := ids.Parse(al.OrderItemId)
		q, ok := asked[id.String()]
		if err != nil || !ok || seen[id.String()] {
			return fieldErr(fmt.Sprintf("body.lines[%d].orderItemId", i), "not a line of this order, or repeated")
		}
		seen[id.String()] = true
		field := fmt.Sprintf("body.lines[%d].quantity", i)
		switch {
		case al.Quantity > int(q):
			return fieldErr(field, "more than the order asked for")
		case al.Quantity < int(q):
			// TODO(lead): partial accept (BK-08–10) is deferred; until then a
			// line must be accepted at the quantity asked.
			return fieldErr(field, "a partial accept is not available yet")
		}
	}
	return nil
}

// reject refuses a placed order before its accept deadline.
func (s *Service) reject(ctx context.Context, by, storeID, orderID ids.ID, b *ordersapi.RejectOrderRequest) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	if b == nil {
		return fieldErr("body", "required")
	}
	reason := string(b.Reason)
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
		if o.Status != StatusPlaced || pastAcceptBy(o, now) {
			return invalidTransition()
		}
		return s.end(ctx, o, StatusRejected, reason, note, staff(by), variant("rejected"), now)
	}))
}

// markReady marks an accepted pickup order ready.
func (s *Service) markReady(ctx context.Context, by, storeID, orderID ids.ID) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	return asProblemOrNil(s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		o, err := s.lockOrder(ctx, orderID, ofStore(storeID))
		if err != nil {
			return err
		}
		if o.Status != StatusAccepted || !CanMove(o.Status, StatusReady, o.Fulfilment) {
			return invalidTransition()
		}
		v, err := s.q(ctx).SetReady(ctx, o.ID)
		if err != nil {
			return fmt.Errorf("orders: ready: %w", err)
		}
		return s.record(ctx, o, StatusReady, v, staff(by), nil, variant("ready"), now)
	}))
}

// storeCancel is a manager's cancel of a held order (D-68): stock goes
// back, the customer gets no strike.
func (s *Service) storeCancel(ctx context.Context, by, storeID, orderID ids.ID, b *ordersapi.StoreCancelOrderRequest) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	if b == nil {
		return fieldErr("body", "required")
	}
	reason := string(b.Reason)
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
		if !handable(o.Status) || pastHold(o, now) {
			return invalidTransition()
		}
		return s.end(ctx, o, StatusCancelled, reason, note, staff(by), generic, now)
	}))
}

// verify is the counter hand-over (B-04): the right code collects the
// order, sells each held line (sale_online) and records the payment method.
// A wrong code counts, and the fifth locks the code; the count is committed
// even though the answer is an error.
func (s *Service) verify(ctx context.Context, by, storeID, orderID ids.ID, b *ordersapi.VerifyCodeRequest) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	if b == nil {
		return fieldErr("body", "required")
	}
	if s.codes == nil {
		return problem.Wrap(genproblems.Internal, errNoCodeKeys)
	}
	method := string(b.PaymentMethod)
	if !slices.Contains([]string{"cash", "upi", "card", "other"}, method) {
		return fieldErr("body.paymentMethod", "not a payment method")
	}
	var wrong, locked bool
	var left int
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		q := s.q(ctx)
		o, err := s.lockOrder(ctx, orderID, ofStore(storeID))
		if err != nil {
			return err
		}
		if !handable(o.Status) || o.Fulfilment != Pickup || pastHold(o, now) {
			return invalidTransition()
		}
		if o.CodeLockedAt != nil {
			return problem.New(genproblems.CodeLocked, "")
		}
		if !s.codes.matches(storeID, b.Code, o.CodeHmac) {
			r, err := q.CountWrongCode(ctx, ordersdb.CountWrongCodeParams{LockAt: codeTries, Now: now, ID: o.ID})
			if err != nil {
				return fmt.Errorf("orders: count wrong code: %w", err)
			}
			wrong = true
			if r.CodeLockedAt != nil {
				// TODO(risk): the code-lockout risk flag (01 §6,
				// risk_code_lockouts_store_per_day) is the risk WP's; it counts
				// orders.code_locked_at per store per IST day. Nothing is
				// flagged here.
				locked = true
				return s.publish(ctx, o, r.Version)
			}
			left = max(codeTries-int(r.CodeAttempts), 0)
			return nil
		}
		return s.collect(ctx, o, by, method, now)
	})
	switch {
	case err != nil:
		return asProblem(err)
	case locked:
		return problem.New(genproblems.CodeLocked, "")
	case wrong:
		return problem.New(genproblems.CodeInvalid, "").WithExtension("attemptsLeft", left)
	}
	return nil
}

// collect hands a locked order over: each held line is sold through the
// stock writer (stores row, then the item; in store_item_id order) and its
// reservation marked consumed, then the order is collected.
func (s *Service) collect(ctx context.Context, o ordersdb.Order, by ids.ID, method string, now time.Time) error {
	q := s.q(ctx)
	t, err := tx(ctx)
	if err != nil {
		return err
	}
	held, err := q.HeldReservations(ctx, o.ID)
	if err != nil {
		return fmt.Errorf("orders: held stock: %w", err)
	}
	for _, r := range held {
		mv, err := s.stock.Sell(ctx, t, Sale{
			StoreID: mustID(o.StoreID), ItemID: mustID(r.StoreItemID), Qty: r.Quantity,
			OrderID: mustID(o.ID), ClientID: mustID(r.ID), UserID: by,
		})
		if err != nil {
			return fmt.Errorf("orders: sell line: %w", err)
		}
		if err := q.ConsumeReservation(ctx, ordersdb.ConsumeReservationParams{MovementID: ids.UUID(mv), ID: r.ID}); err != nil {
			return fmt.Errorf("orders: consume stock: %w", err)
		}
	}
	// The held credits go to the store and the store's cashback is paid on
	// the items after offers less the credits (D-02).
	cashback, err := s.credits.Complete(ctx, mustID(o.ID), mustID(o.CustomerID), mustID(o.StoreID), o.ItemsAfterOffersPaise)
	if err != nil {
		return fmt.Errorf("orders: settle credits: %w", err)
	}
	v, err := q.SetCollected(ctx, ordersdb.SetCollectedParams{Now: &now, PaymentMethod: &method, CashbackCredits: i32(cashback), ID: o.ID})
	if err != nil {
		return fmt.Errorf("orders: collect: %w", err)
	}
	if err := s.referrals.OnOrderCompleted(ctx, CompletedOrder{
		OrderID: mustID(o.ID), CustomerID: mustID(o.CustomerID), StoreID: mustID(o.StoreID),
		EligiblePaise: o.ItemsAfterOffersPaise, CreditsUsed: int(o.CreditsUsed),
	}); err != nil {
		return fmt.Errorf("orders: referrals: %w", err)
	}
	return s.record(ctx, o, StatusCollected, v, staff(by), nil, silent, now)
}

// --- customer -------------------------------------------------------------------

// customerCancel is the customer's cancel (C-13): before the shop's
// deadline while placed, or before the hold ends once accepted. Stock goes
// back.
func (s *Service) customerCancel(ctx context.Context, userID, orderID ids.ID, b *ordersapi.CancelOrderRequest) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	if b == nil {
		return fieldErr("body", "required")
	}
	reason := string(b.Reason)
	if b.Reason == ordersapi.CustomerCancelReasonDeclinedPartial {
		// Only the partial-accept answer (confirm-partial) uses it.
		return fieldErr("body.reason", "declined_partial is set by confirm-partial")
	}
	note, err := noteFor(reason, b.Note)
	if err != nil {
		return err
	}
	return asProblemOrNil(s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		o, err := s.lockOrder(ctx, orderID, ofCustomer(userID))
		if err != nil {
			return err
		}
		switch {
		case o.Status == StatusPlaced && !pastAcceptBy(o, now):
		case o.Status == StatusAwaitingCustomer:
		case handable(o.Status) && !pastHold(o, now):
		default:
			return invalidTransition()
		}
		return s.end(ctx, o, StatusCancelled, reason, note, customer(userID), silent, now)
	}))
}

// extend adds pickup_extension_min to the hold, once (01 §6).
func (s *Service) extend(ctx context.Context, userID, orderID ids.ID) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	more, err := s.duration(ctx, settings.PickupExtensionMin)
	if err != nil {
		return problem.Wrap(genproblems.Internal, err)
	}
	return asProblemOrNil(s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		o, err := s.lockOrder(ctx, orderID, ofCustomer(userID))
		if err != nil {
			return err
		}
		if o.Fulfilment != Pickup || !handable(o.Status) || o.HoldUntil == nil {
			return invalidTransition()
		}
		if o.ExtendedAt != nil {
			return problem.New(genproblems.ExtensionAlreadyUsed, "")
		}
		if pastHold(o, now) {
			return invalidTransition()
		}
		holdUntil := o.HoldUntil.Add(more)
		v, err := s.q(ctx).SetExtended(ctx, ordersdb.SetExtendedParams{HoldUntil: &holdUntil, Now: &now, ID: o.ID})
		if err != nil {
			return fmt.Errorf("orders: extend: %w", err)
		}
		if err := s.publish(ctx, o, v); err != nil {
			return err
		}
		return s.noShowTimer(ctx, orderID, holdUntil)
	}))
}

// newCode replaces a locked code (C-13): a fresh code, attempts back to 0.
func (s *Service) newCode(ctx context.Context, userID, orderID ids.ID) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	return asProblemOrNil(s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		o, err := s.lockOrder(ctx, orderID, ofCustomer(userID))
		if err != nil {
			return err
		}
		if !handable(o.Status) || o.CodeLockedAt == nil || pastHold(o, now) {
			return invalidTransition()
		}
		var v int32
		err = s.withFreshCode(ctx, mustID(o.StoreID), orderID, func(ctx context.Context, c sealedCode) error {
			var err error
			v, err = s.q(ctx).SetNewCode(ctx, ordersdb.SetNewCodeParams{CodeEnc: c.enc, CodeHmac: c.mac, ID: o.ID})
			return err
		})
		if err != nil {
			return fmt.Errorf("orders: new code: %w", err)
		}
		return s.publish(ctx, o, v)
	}))
}

// --- helpers --------------------------------------------------------------------

// asProblemOrNil is asProblem that keeps nil.
func asProblemOrNil(err error) error {
	if err == nil {
		return nil
	}
	return asProblem(err)
}

// clip cuts s to at most n runes.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// pgUUIDOf is id as a nullable column: null unless use.
func pgUUIDOf(id *ids.ID, use bool) pgtype.UUID {
	if id == nil || !use {
		return pgtype.UUID{}
	}
	return ids.UUID(*id)
}

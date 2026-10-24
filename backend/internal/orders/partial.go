package orders

import (
	"context"
	"fmt"
	"time"

	ordersapi "github.com/todayz/backend/gen/api/orders"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/settings"
)

// Partial accept (BK-08–10, D-28): the shop takes fewer of some lines, the
// order waits in awaiting_customer with the totals recomputed and the
// credits re-capped, and the customer takes it (the stock is held then) or
// declines it before partial_confirm_by; with no answer it is cancelled
// partial_timeout, with no strike for anyone (D-68).

// Customer and system reasons of a partial accept (03 §4.2).
const (
	reasonDeclinedPartial = "declined_partial"
	reasonPartialTimeout  = "partial_timeout"
)

// pastPartial: the customer's time to answer is over (strict, D-108). An
// awaiting order without a deadline cannot be answered; the timer and the
// sweep cancel it.
func pastPartial(o ordersdb.Order, now time.Time) bool {
	return o.PartialConfirmBy == nil || !now.Before(*o.PartialConfirmBy)
}

// partialBill is an order's money on the accepted quantities.
type partialBill struct {
	lineTotals                                                      []int64
	subtotal, discount, itemsAfterOffers, itemSubtotal, deliveryFee int64
}

// billFor recomputes the order's money on want[i] of each line. The offers
// the order got at placing scale with what is left: a line's offer
// discount by the share of its quantity kept, the bill-level discount by
// the share of the item subtotal kept, both rounded down, so the customer
// never pays more a unit than they agreed to and an offer is never counted
// again. An ask reply's line keeps its replied price (D-101).
//
// TODO(lead): the offers engine is not asked again (its per-customer limit
// would count this order's own redemption against it), so a minimum-bill
// offer stays, scaled, when the smaller bill is under its minimum.
func billFor(o ordersdb.Order, lines []ordersdb.OrderLinesRow, want []int32) partialBill {
	b := partialBill{lineTotals: make([]int64, len(lines))}
	var lineDiscounts int64
	for i, l := range lines {
		asked := l.UnitPricePaise * int64(l.QuantityRequested)
		was := min(max(asked-l.LineTotalPaise, 0), asked)
		sub := l.UnitPricePaise * int64(want[i])
		var off int64
		if l.QuantityRequested > 0 {
			off = was * int64(want[i]) / int64(l.QuantityRequested)
		}
		b.lineTotals[i] = sub - off
		b.subtotal += sub
		lineDiscounts += off
	}
	b.itemSubtotal = b.subtotal - lineDiscounts
	var bill int64
	if wasBill := o.DiscountPaise - (o.SubtotalPaise - o.ItemSubtotalPaise); wasBill > 0 && o.ItemSubtotalPaise > 0 {
		bill = min(wasBill*b.itemSubtotal/o.ItemSubtotalPaise, b.itemSubtotal)
	}
	b.discount = lineDiscounts + bill
	b.itemsAfterOffers = b.subtotal - b.discount
	b.deliveryFee = 0 // pickup only; delivery recomputes its fee here (BK-23)
	return b
}

// partialAccept moves a locked placed order to awaiting_customer with the
// quantities in want (some below what was asked, at least one above 0).
func (s *Service) partialAccept(ctx context.Context, o ordersdb.Order, lines []ordersdb.OrderLinesRow, want []int32, by actor, answerIn time.Duration, now time.Time) error {
	q := s.q(ctx)
	b := billFor(o, lines, want)
	// Credits after the order's rows (D-132): the hold is cut down to what
	// the new eligible amount allows, or released under the minimum (D-28).
	credits, err := s.credits.Recap(ctx, mustID(o.ID), b.itemsAfterOffers)
	if err != nil {
		return fmt.Errorf("orders: recap credits: %w", err)
	}
	if credits < 0 || int64(credits)*100 > b.itemsAfterOffers {
		return fmt.Errorf("orders: recap kept %d credits on %d paise", credits, b.itemsAfterOffers)
	}
	for i, l := range lines {
		if err := q.SetLineAccepted(ctx, ordersdb.SetLineAcceptedParams{
			QuantityAccepted: &want[i], LineTotalPaise: b.lineTotals[i], ID: l.ID, OrderID: o.ID,
		}); err != nil {
			return fmt.Errorf("orders: accept line: %w", err)
		}
	}
	by2 := now.Add(answerIn)
	v, err := q.SetPartial(ctx, ordersdb.SetPartialParams{
		PartialConfirmBy: &by2, SubtotalPaise: b.subtotal, DiscountPaise: b.discount,
		ItemsAfterOffersPaise: b.itemsAfterOffers, ItemSubtotalPaise: b.itemSubtotal, CreditsUsed: i32(credits),
		DeliveryFeePaise: b.deliveryFee, TotalPaise: b.itemsAfterOffers - int64(credits)*100 + b.deliveryFee, ID: o.ID,
	})
	if err != nil {
		return fmt.Errorf("orders: partial accept: %w", err)
	}
	if err := s.record(ctx, o, StatusAwaitingCustomer, v, by, nil, variant("partial"), now); err != nil {
		return err
	}
	t, err := tx(ctx)
	if err != nil {
		return err
	}
	return s.jobs.EnqueueTx(ctx, t, KindPartialTimeout, TimerArgs{OrderID: mustID(o.ID)},
		platformjobs.EnqueueOpts{ScheduledAt: by2, UniqueKey: mustID(o.ID).String() + ":partial"})
}

// confirmPartial is the customer's answer to a partial accept (C-12):
// accept holds the accepted quantities (409 insufficient_stock when one is
// no longer available; the order keeps waiting) and starts the pickup
// hold; decline cancels with declined_partial.
func (s *Service) confirmPartial(ctx context.Context, userID, orderID ids.ID, b *ordersapi.ConfirmPartialRequest) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	if b == nil {
		return fieldErr("body", "required")
	}
	hold, err := s.duration(ctx, settings.PickupHoldMin)
	if err != nil {
		return problem.Wrap(genproblems.Internal, err)
	}
	return asProblemOrNil(s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		o, err := s.lockOrder(ctx, orderID, ofCustomer(userID))
		if err != nil {
			return err
		}
		if o.Status != StatusAwaitingCustomer || pastPartial(o, now) {
			return invalidTransition()
		}
		if !b.Accept {
			return s.end(ctx, o, StatusCancelled, reasonDeclinedPartial, nil, customer(userID), silent, now)
		}
		lines, err := s.q(ctx).OrderLines(ctx, o.ID)
		if err != nil {
			return fmt.Errorf("orders: lines: %w", err)
		}
		want := make([]int32, len(lines))
		for i, l := range lines {
			if l.QuantityAccepted == nil {
				return fmt.Errorf("orders: awaiting order %s has a line with no accepted quantity", mustID(o.ID))
			}
			want[i] = *l.QuantityAccepted
		}
		if err := s.lockStock(ctx, lines, want); err != nil {
			return err
		}
		return s.holdAndStart(ctx, o, lines, want, customer(userID), silent, hold, now)
	}))
}

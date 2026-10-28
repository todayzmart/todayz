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
// re-capped credits shown, and the customer takes it (the stock is held and
// the credits re-capped then) or declines it before partial_confirm_by; with no answer it is cancelled
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

// billFor recomputes the order's money on want[i] of each line (03 §4.2:
// every bill figure is recomputed). The offers are priced again on the
// smaller cart, with only the offers the order redeemed, as they stood when
// it was placed (lead, W2-02 finish): a minimum-bill offer drops off when
// the new item subtotal is under its minimum. Discounts are clamped as at
// checkout. An ask reply's line keeps its replied price (D-101).
//
// TODO(lead): the redemption of an offer that dropped off stays counted
// until the order ends (collected keeps it, any other end releases it); the
// offers engine has no partial release.
func (s *Service) billFor(ctx context.Context, o ordersdb.Order, lines []ordersdb.OrderLinesRow, want []int32) (partialBill, error) {
	b := partialBill{lineTotals: make([]int64, len(lines))}
	plines := make([]PriceLine, len(lines))
	for i, l := range lines {
		plines[i] = PriceLine{StoreItemID: mustID(l.StoreItemID), UnitPricePaise: l.UnitPricePaise,
			Quantity: int(want[i]), Fixed: l.PriceSource == sourceAskReply}
	}
	pr, err := s.pricer.Reprice(ctx, mustID(o.ID), mustID(o.CustomerID), mustID(o.StoreID), o.PlacedAt, plines)
	if err != nil {
		return partialBill{}, fmt.Errorf("orders: reprice offers: %w", err)
	}
	var lineDiscounts int64
	for i, l := range lines {
		sub := l.UnitPricePaise * int64(want[i])
		var off int64
		if i < len(pr.LineDiscounts) {
			off = min(max(pr.LineDiscounts[i], 0), sub)
		}
		b.lineTotals[i] = sub - off
		b.subtotal += sub
		lineDiscounts += off
	}
	b.itemSubtotal = b.subtotal - lineDiscounts
	b.discount = lineDiscounts + min(max(pr.BillDiscount, 0), b.itemSubtotal)
	b.itemsAfterOffers = b.subtotal - b.discount
	if o.Fulfilment == Delivery {
		// Fewer lines only lower the item subtotal, so a fee paid at
		// placing stays the same; an order that was free (at or above the
		// store's free-delivery amount) may drop under it and pay the
		// store's fee as it is now (01 §5.5).
		b.deliveryFee = o.DeliveryFeePaise
		if b.deliveryFee == 0 {
			f, err := s.q(ctx).StoreDeliveryFee(ctx, o.StoreID)
			if err != nil {
				return partialBill{}, fmt.Errorf("orders: delivery fee: %w", err)
			}
			b.deliveryFee = deliveryFee(b.itemSubtotal, f.DeliveryFeePaise, f.DeliveryFreeAbovePaise)
		}
	}
	return b, nil
}

// partialAccept moves a locked placed order to awaiting_customer with the
// quantities in want (some below what was asked, at least one above 0).
func (s *Service) partialAccept(ctx context.Context, o ordersdb.Order, lines []ordersdb.OrderLinesRow, want []int32, by actor, answerIn time.Duration, now time.Time) error {
	q := s.q(ctx)
	b, err := s.billFor(ctx, o, lines, want)
	if err != nil {
		return err
	}
	// The credits the order would keep, shown as a preview: the hold stays
	// whole until the customer confirms (01 §7, lead W2-02 finish), and a
	// decline or timeout releases all of it.
	credits, err := s.credits.Kept(ctx, int(o.CreditsUsed), b.itemsAfterOffers)
	if err != nil {
		return fmt.Errorf("orders: credits preview: %w", err)
	}
	if err := checkCredits(credits, b.itemsAfterOffers); err != nil {
		return err
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

// checkCredits refuses a credits figure the bill cannot carry.
func checkCredits(credits int, itemsAfterOffers int64) error {
	if credits < 0 || int64(credits)*100 > itemsAfterOffers {
		return fmt.Errorf("orders: %d credits on %d paise", credits, itemsAfterOffers)
	}
	return nil
}

// recapCredits cuts the confirmed order's hold down to what its new bill
// allows (D-28) and writes what it kept. The ledger comes after the order
// and its items (D-132). It normally keeps the preview; it keeps more only
// when the credit rules loosened while the order waited (the customer pays
// less), and never fewer: confirmPartial has refused that already, and a
// hold that shrank anyway is price_changed too.
func (s *Service) recapCredits(ctx context.Context, o ordersdb.Order) error {
	kept, err := s.credits.Recap(ctx, mustID(o.ID), o.ItemsAfterOffersPaise)
	if err != nil {
		return fmt.Errorf("orders: recap credits: %w", err)
	}
	if err := checkCredits(kept, o.ItemsAfterOffersPaise); err != nil {
		return err
	}
	if kept < int(o.CreditsUsed) {
		return problem.New(genproblems.PriceChanged, "")
	}
	if kept == int(o.CreditsUsed) {
		return nil
	}
	if err := s.q(ctx).SetOrderCredits(ctx, ordersdb.SetOrderCreditsParams{
		CreditsUsed: i32(kept), TotalPaise: o.ItemsAfterOffersPaise - int64(kept)*100 + o.DeliveryFeePaise, ID: o.ID,
	}); err != nil {
		return fmt.Errorf("orders: order credits: %w", err)
	}
	return nil
}

// confirmPartial is the customer's answer to a partial accept (C-12):
// accept holds the accepted quantities (409 insufficient_stock when one is
// no longer available; the order keeps waiting), re-caps the credits and
// starts the pickup hold; decline cancels with declined_partial. When the
// credit rules tightened while the order waited (credit_max_per_order
// dropped), a confirm would keep fewer credits than the preview showed and
// charge more than the customer agreed to: it lowers the preview instead,
// keeps the order waiting and answers 409 price_changed, so the customer
// sees the new total and answers again (lead review, W2-02 finish).
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
	lowered := false
	err = s.db.InTx(ctx, func(ctx context.Context) error {
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
		kept, err := s.credits.Kept(ctx, int(o.CreditsUsed), o.ItemsAfterOffersPaise)
		if err != nil {
			return fmt.Errorf("orders: credits preview: %w", err)
		}
		if kept < int(o.CreditsUsed) {
			lowered = true
			return s.lowerPreview(ctx, o, kept)
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
		if err := s.lockStock(ctx, o.StoreID, lines, want, now); err != nil {
			return err
		}
		if err := s.recapCredits(ctx, o); err != nil {
			return err
		}
		return s.holdAndStart(ctx, o, lines, want, customer(userID), silent, hold, now)
	})
	if err == nil && lowered {
		return problem.New(genproblems.PriceChanged, "")
	}
	return asProblemOrNil(err)
}

// lowerPreview writes the credits a confirm would keep now, and the total
// with them, as the order's preview; the hold stays whole until the
// customer answers.
func (s *Service) lowerPreview(ctx context.Context, o ordersdb.Order, kept int) error {
	if err := checkCredits(kept, o.ItemsAfterOffersPaise); err != nil {
		return err
	}
	if _, err := s.q(ctx).SetPreviewCredits(ctx, ordersdb.SetPreviewCreditsParams{
		CreditsUsed: i32(kept), TotalPaise: o.ItemsAfterOffersPaise - int64(kept)*100 + o.DeliveryFeePaise, ID: o.ID,
	}); err != nil {
		return fmt.Errorf("orders: lower credits preview: %w", err)
	}
	return nil
}

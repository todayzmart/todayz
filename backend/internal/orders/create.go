package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

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

// uxAskReply keeps an ask reply on at most one open order (D-101).
const uxAskReply = "ux_orders_ask_reply_active"

// emptyMeta is an order_events row with nothing more to say.
var emptyMeta = json.RawMessage(`{}`)

// snapshot is the product as the customer booked it (order_items.product_snapshot).
type snapshot struct {
	ProductID string  `json:"productId"`
	Name      string  `json:"name"`
	Variant   *string `json:"variant,omitempty"`
	PackSize  float64 `json:"packSize"`
	Unit      string  `json:"unit"`
	ImageKey  *string `json:"imageKey,omitempty"`
	MrpPaise  *int64  `json:"mrpPaise,omitempty"`
}

// create places an order (C-11): every check, then the order, its lines, the
// placed event, the store stream event, the accept timer and the store's
// "new order", in one transaction. Nothing is reserved (D-01), unless the
// store auto-accepts (BK-22). Repeating the request with the same
// Idempotency-Key replays the first answer (the route's idempotency layer,
// 04 §1.6).
func (s *Service) create(ctx context.Context, c cart, expectedTotal int) (ids.ID, error) {
	if err := s.readyToWrite(ctx); err != nil {
		return ids.Zero, err
	}
	p, err := principal(ctx)
	if err != nil {
		return ids.Zero, err
	}
	acceptIn, err := s.duration(ctx, settings.OrderAcceptTimeoutMin)
	if err != nil {
		return ids.Zero, problem.Wrap(genproblems.Internal, err)
	}
	hold, err := s.duration(ctx, settings.PickupHoldMin)
	if err != nil {
		return ids.Zero, problem.Wrap(genproblems.Internal, err)
	}
	var orderID ids.ID
	var stale bool
	var quoteStore ordersdb.StoreForBookingRow
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		q := s.q(ctx)
		st, storeID, err := s.bookableStore(ctx, c)
		if err != nil {
			return err
		}
		// A delivery cart is told the next delivery window first; the shop
		// must be open too (its hours, or closed by the shop).
		var address []byte
		if c.fulfilment == Delivery {
			if err := s.deliveryClosed(st, now); err != nil {
				return err
			}
		}
		if !s.open(st.Hours, st.ClosedUntil, now) {
			return problem.New(genproblems.StoreClosed, "")
		}
		if c.fulfilment == Delivery {
			if address, err = s.deliveryAddress(ctx, p.UserID, st, c); err != nil {
				return err
			}
		}

		// The customer's row, held to the end: a second order of theirs and
		// an account deletion wait here, so the checks below hold.
		cu, err := q.LockCustomer(ctx, ids.UUID(p.UserID))
		if db.IsNoRows(err) {
			return problem.New(genproblems.Unauthorized, "")
		}
		if err != nil {
			return fmt.Errorf("orders: lock customer: %w", err)
		}
		if cu.Status != "active" {
			return problem.New(genproblems.Forbidden, "")
		}
		if err := orderLimit(ctx, q, p.UserID, cu.LimitedUntil, now); err != nil {
			return err
		}
		if st.AutoAccept {
			// The items are locked before the offers and the credits, the
			// order an accept and a cancel take them in (items, then the
			// ledger), so the instant accept below cannot deadlock with
			// them; the price then reads the stock it will hold.
			if err := lockCartItems(ctx, q, c); err != nil {
				return err
			}
		}

		qq, err := s.price(ctx, p.UserID, st, c, now)
		if err != nil {
			return err
		}
		if c.fulfilment == Delivery {
			if err := belowMinimum(qq); err != nil {
				return err
			}
		}
		if err := s.checkCreditsAsked(ctx, qq); err != nil {
			return err
		}
		if qq.askReplyID != nil && !qq.askUsable {
			// TODO(lead): until W3-16 gives an AskPricer, every reply is
			// refused here (ask_price_expired) and the customer books at the
			// catalogue price shown in extensions.quote.
			return problem.New(genproblems.AskPriceExpired, "").WithExtension("quote", qq.view())
		}
		if i := qq.firstShort; i >= 0 {
			return problem.New(genproblems.InsufficientStock, "").
				WithExtension("storeItemId", qq.lines[i].id.String()).
				WithExtension("available", max(qq.lines[i].available, 0))
		}
		if qq.total != int64(expectedTotal) {
			return problem.New(genproblems.PriceChanged, "").WithExtension("quote", qq.view())
		}

		acceptBy := now.Add(acceptIn)
		orderID = s.ids.New(now)
		var askReply = pgUUIDOf(qq.askReplyID, qq.askUsable)
		var version int32
		err = s.withFreshCode(ctx, storeID, orderID, func(ctx context.Context, code sealedCode) error {
			v, err := s.q(ctx).InsertOrder(ctx, ordersdb.InsertOrderParams{
				ID: ids.UUID(orderID), CustomerID: ids.UUID(p.UserID), StoreID: st.ID, AskReplyID: askReply,
				Fulfilment: c.fulfilment, AddressSnapshot: address, SubtotalPaise: qq.subtotal, DiscountPaise: qq.discount,
				ItemsAfterOffersPaise: qq.itemsAfterOffers, ItemSubtotalPaise: qq.itemSubtotal,
				CreditsUsed: i32(qq.creditsUsed), DeliveryFeePaise: qq.fee, TotalPaise: qq.total,
				CashbackBp: st.CashbackBp, CodeEnc: code.enc, CodeHmac: code.mac, AcceptBy: &acceptBy, PlacedAt: now,
			})
			version = v
			return err
		})
		if db.IsUniqueViolation(err, uxAskReply) {
			return problem.New(genproblems.AskReplyUsed, "")
		}
		if err != nil {
			return fmt.Errorf("orders: insert order: %w", err)
		}
		if err := s.redeemer.Redeem(ctx, orderID, p.UserID, qq.offers); err != nil {
			if errors.Is(err, ErrOfferGone) {
				stale, quoteStore = true, st
				return errStaleOffers
			}
			return fmt.Errorf("orders: redeem offers: %w", err)
		}
		if qq.creditsUsed > 0 {
			if err := s.holdCredits(ctx, qq, orderID, p.UserID, st, c, now); err != nil {
				return err
			}
		}
		for _, l := range qq.lines {
			snap, err := json.Marshal(snapshot{
				ProductID: l.productID.String(), Name: l.row.Name, Variant: l.row.Variant, PackSize: l.row.PackSize,
				Unit: l.row.Unit, ImageKey: l.row.ImageKey, MrpPaise: l.row.MrpPaise,
			})
			if err != nil {
				return fmt.Errorf("orders: snapshot: %w", err)
			}
			if err := q.InsertOrderItem(ctx, ordersdb.InsertOrderItemParams{
				ID: ids.UUID(s.ids.New(now)), OrderID: ids.UUID(orderID), StoreItemID: ids.UUID(l.id),
				ProductSnapshot: snap, UnitPricePaise: l.unitPrice, QuantityRequested: i32(l.qty),
				LineTotalPaise: l.total, PriceSource: l.priceSource, CreatedAt: now,
			}); err != nil {
				return fmt.Errorf("orders: insert line: %w", err)
			}
		}
		if err := q.InsertOrderEvent(ctx, ordersdb.InsertOrderEventParams{
			ID: ids.UUID(s.ids.New(now)), OrderID: ids.UUID(orderID), ToStatus: StatusPlaced,
			ActorKind: actorCustomer, ActorID: ids.UUID(p.UserID), Meta: emptyMeta, CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("orders: placed event: %w", err)
		}
		t, err := tx(ctx)
		if err != nil {
			return err
		}
		if err := realtime.Publish(ctx, t, realtime.StoreChannel(storeID),
			realtime.Event{Type: realtime.OrderCreated, ID: orderID, Version: int64(version)}); err != nil {
			return err
		}
		accepted := false
		if st.AutoAccept {
			if accepted, err = s.autoAccept(ctx, orderID, hold, now); err != nil {
				return err
			}
		}
		if !accepted {
			if err := s.jobs.EnqueueTx(ctx, t, KindAcceptTimeout, TimerArgs{OrderID: orderID},
				platformjobs.EnqueueOpts{ScheduledAt: acceptBy, UniqueKey: orderID.String() + ":accept"}); err != nil {
				return fmt.Errorf("orders: accept timer: %w", err)
			}
		}
		// An auto-accepted order is still new to the shop: it has to pack it.
		if _, err := s.notices.Send(ctx, t, notice.Notification{
			Kind: notice.KindNewOrder, BusinessID: mustID(st.BusinessID), StoreID: storeID,
			Params: map[string]string{"store": clip(st.Name, maxParamRunes)},
			Ref:    &notice.Ref{Type: "order", ID: orderID},
		}); err != nil {
			return fmt.Errorf("orders: new order notice: %w", err)
		}
		return nil
	})
	if stale {
		// An offer ran out between the quote and the order: the transaction
		// is rolled back, so price again and show the customer the new total.
		qq, qerr := s.price(ctx, p.UserID, quoteStore, c, s.now())
		if qerr != nil {
			return ids.Zero, asProblem(qerr)
		}
		return ids.Zero, problem.New(genproblems.PriceChanged, "").WithExtension("quote", qq.view())
	}
	if err != nil {
		return ids.Zero, asProblem(err)
	}
	return orderID, nil
}

// lockCartItems locks the cart's items in id order (LockItems). An id that
// does not parse is left to the price's field error.
func lockCartItems(ctx context.Context, q *ordersdb.Queries, c cart) error {
	itemIDs := make([]pgtype.UUID, 0, len(c.items))
	for _, it := range c.items {
		if id, err := ids.Parse(it.StoreItemId); err == nil {
			itemIDs = append(itemIDs, ids.UUID(id))
		}
	}
	if len(itemIDs) == 0 {
		return nil
	}
	if _, err := q.LockItems(ctx, itemIDs); err != nil {
		return fmt.Errorf("orders: lock items: %w", err)
	}
	return nil
}

// autoAccept accepts a new order at once for a store with auto-accept on
// (BK-22, 01 §5) when every line is available, as the system: it holds the
// stock and starts the pickup hold, as a shop's full accept does. A line
// short of stock leaves the order placed for the shop to answer. create
// has locked the items already, so the check here is exact.
// create refuses a cart with a short line (BK-02, 01 §5.3) after reading
// the stock under that lock, at every store (lead, W2-02 finish), so at an
// auto-accept store an order with an unavailable item is refused with
// insufficient_stock rather than placed; the "stays placed" branch below is
// the backstop for a stock that moved anyway.
func (s *Service) autoAccept(ctx context.Context, orderID ids.ID, hold time.Duration, now time.Time) (bool, error) {
	q := s.q(ctx)
	o, err := q.LockOrder(ctx, ids.UUID(orderID))
	if err != nil {
		return false, fmt.Errorf("orders: lock new order: %w", err)
	}
	lines, err := q.OrderLines(ctx, o.ID)
	if err != nil {
		return false, fmt.Errorf("orders: lines: %w", err)
	}
	want := make([]int32, len(lines))
	for i, l := range lines {
		want[i] = l.QuantityRequested
	}
	if err := s.lockStock(ctx, lines, want); err != nil {
		if problem.CodeOf(err) == genproblems.InsufficientStock {
			return false, nil
		}
		return false, err
	}
	return true, s.holdAndStart(ctx, o, lines, want, system, variant("accepted"), hold, now)
}

// holdCredits holds the order's credits in its transaction. The quote's
// Allow did not lock, so a spend or a cap reached since can still refuse:
// a credit rule answers its own problem; a wallet that no longer covers the
// credits answers price_changed with the quote as it is now.
func (s *Service) holdCredits(ctx context.Context, qq quoted, orderID, customerID ids.ID, st ordersdb.StoreForBookingRow, c cart, now time.Time) error {
	al, err := s.credits.Hold(ctx, CreditsHold{
		OrderID: orderID, CustomerID: customerID, StoreID: mustID(st.ID),
		Credits: qq.creditsUsed, EligiblePaise: qq.itemsAfterOffers,
	})
	if err != nil {
		return fmt.Errorf("orders: hold credits: %w", err)
	}
	if al.Allowed >= qq.creditsUsed {
		return nil
	}
	if code, ok := creditProblems[al.Limit]; ok {
		return problem.New(code, "")
	}
	fresh, err := s.price(ctx, customerID, st, c, now)
	if err != nil {
		return err
	}
	return problem.New(genproblems.PriceChanged, "").WithExtension("quote", fresh.view())
}

// checkCreditsAsked refuses an order that would use fewer credits than
// asked, instead of placing it with fewer (04 §2, CR-02, D-216). The
// refusal is the quote's creditsLimit, in the credit rules' order (minimum,
// store cap, monthly cap, then the max), so placing and the quote name the
// same rule; more than credit_max_per_order with no rule limit (a short
// wallet) is still credit_max_exceeded. Anything else (the wallet, the ₹1
// floor) is price_changed with the quote as it is now. The quote clamps
// and says why in creditsLimit (D-76); placing does not.
func (s *Service) checkCreditsAsked(ctx context.Context, qq quoted) error {
	if qq.creditsRequested <= qq.creditsUsed {
		return nil
	}
	if code, ok := creditProblems[qq.creditsLimit]; ok {
		return problem.New(code, "")
	}
	maxPer, err := s.count(ctx, settings.CreditMaxPerOrder)
	if err != nil {
		return err
	}
	if qq.creditsRequested > maxPer {
		return problem.New(genproblems.CreditMaxExceeded, "")
	}
	return problem.New(genproblems.PriceChanged, "").WithExtension("quote", qq.view())
}

// creditProblems are the credit rules' refusals (wallet.yaml).
var creditProblems = map[string]string{
	"below_credit_minimum": genproblems.BelowCreditMinimum,
	"credit_max_exceeded":  genproblems.CreditMaxExceeded,
	"credit_monthly_cap":   genproblems.CreditMonthlyCap,
	"store_credit_cap":     genproblems.StoreCreditCap,
}

// errStaleOffers rolls the order back when an offer ran out; create then
// answers price_changed with a fresh quote.
var errStaleOffers = errors.New("orders: an offer ran out")

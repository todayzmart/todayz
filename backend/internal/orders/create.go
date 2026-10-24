package orders

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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
// placed event, the accept timer, the store's "new order" and the store
// stream event, in one transaction. Nothing is reserved (D-01). Repeating
// the request with the same Idempotency-Key replays the first answer (the
// route's idempotency layer, 04 §1.6).
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
	var orderID ids.ID
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		q := s.q(ctx)
		st, storeID, err := s.bookableStore(ctx, c)
		if err != nil {
			return err
		}
		if !s.open(st.Hours, st.ClosedUntil, now) {
			return problem.New(genproblems.StoreClosed, "")
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
		if cu.LimitedUntil != nil && now.Before(*cu.LimitedUntil) {
			// 01 §5.4: after the no-show limit, one open order at a time.
			n, err := q.ActiveOrderCount(ctx, ids.UUID(p.UserID))
			if err != nil {
				return fmt.Errorf("orders: active orders: %w", err)
			}
			if n > 0 {
				return problem.New(genproblems.OrderingSuspended, "").WithExtension("until", cu.LimitedUntil.UTC())
			}
		}

		qq, err := s.price(ctx, p.UserID, st, c, now)
		if err != nil {
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
				Fulfilment: Pickup, SubtotalPaise: qq.subtotal, DiscountPaise: qq.discount,
				ItemsAfterOffersPaise: qq.itemsAfterOffers, ItemSubtotalPaise: qq.itemSubtotal,
				CreditsUsed: i32(qq.creditsUsed), DeliveryFeePaise: qq.fee, TotalPaise: qq.total,
				CodeEnc: code.enc, CodeHmac: code.mac, AcceptBy: &acceptBy, PlacedAt: now,
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
		if err := s.jobs.EnqueueTx(ctx, t, KindAcceptTimeout, TimerArgs{OrderID: orderID},
			platformjobs.EnqueueOpts{ScheduledAt: acceptBy, UniqueKey: orderID.String() + ":accept"}); err != nil {
			return fmt.Errorf("orders: accept timer: %w", err)
		}
		if _, err := s.notices.Send(ctx, t, notice.Notification{
			Kind: notice.KindNewOrder, BusinessID: mustID(st.BusinessID), StoreID: storeID,
			Params: map[string]string{"store": clip(st.Name, maxParamRunes)},
			Ref:    &notice.Ref{Type: "order", ID: orderID},
		}); err != nil {
			return fmt.Errorf("orders: new order notice: %w", err)
		}
		return realtime.Publish(ctx, t, realtime.StoreChannel(storeID),
			realtime.Event{Type: realtime.OrderCreated, ID: orderID, Version: int64(version)})
	})
	if err != nil {
		return ids.Zero, asProblem(err)
	}
	return orderID, nil
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

// creditProblems are the credit rules' refusals (wallet.yaml).
var creditProblems = map[string]string{
	"below_credit_minimum": genproblems.BelowCreditMinimum,
	"credit_max_exceeded":  genproblems.CreditMaxExceeded,
	"credit_monthly_cap":   genproblems.CreditMonthlyCap,
	"store_credit_cap":     genproblems.StoreCreditCap,
}

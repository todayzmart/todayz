package orders

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	ordersapi "github.com/todayz/backend/gen/api/orders"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// Delivery checkout (BK-23, BK-23b): the address and radius, the minimum,
// the fee and the delivery hours. The quote and create ask the same checks.

// deliveryOn reports whether delivery is switched on platform-wide
// (feature_delivery_enabled).
func (s *Service) deliveryOn(ctx context.Context) (bool, error) {
	on, err := s.settings.Bool(ctx, settings.FeatureDeliveryEnabled)
	if err != nil {
		return false, fmt.Errorf("orders: %s: %w", settings.FeatureDeliveryEnabled, err)
	}
	return on, nil
}

// deliveryAddress loads the cart's address, the customer's own, and refuses
// one outside the store's radius (or a store with no location) with
// store_not_bookable, extensions.reason outside_delivery_radius. It returns
// the snapshot create writes onto the order.
func (s *Service) deliveryAddress(ctx context.Context, customerID ids.ID, st ordersdb.StoreForBookingRow, c cart) ([]byte, error) {
	if c.addressID == nil {
		return nil, fieldErr("body.addressId", "required for delivery")
	}
	aid, err := ids.Parse(*c.addressID)
	if err != nil {
		return nil, fieldErr("body.addressId", "not an id")
	}
	a, err := s.q(ctx).AddressForDelivery(ctx, ordersdb.AddressForDeliveryParams{
		ID: ids.UUID(aid), UserID: ids.UUID(customerID), StoreID: st.ID,
	})
	if db.IsNoRows(err) {
		return nil, fieldErr("body.addressId", "not one of your addresses")
	}
	if err != nil {
		return nil, fmt.Errorf("orders: address: %w", err)
	}
	if !st.HasLocation || !a.InRadius {
		return nil, problem.New(genproblems.StoreNotBookable, "").WithExtension("reason", "outside_delivery_radius")
	}
	snap, err := json.Marshal(ordersapi.OrderAddress{
		Label: a.Label, Line1: a.Line1, Line2: a.Line2, Landmark: a.Landmark, Pincode: a.Pincode,
		Lat: float32(a.Lat), Lng: float32(a.Lng),
	})
	if err != nil {
		return nil, fmt.Errorf("orders: address snapshot: %w", err)
	}
	return snap, nil
}

// deliveryFee is the fee for an item subtotal: none at or above the store's
// free-delivery amount (01 §5.5).
func deliveryFee(itemSubtotal, fee int64, freeAbove *int64) int64 {
	if freeAbove != nil && itemSubtotal >= *freeAbove {
		return 0
	}
	return fee
}

// belowMinimum refuses a delivery cart whose item subtotal is under the
// store's minimum (D-94: the minimum is on the item subtotal, at or above
// passes).
func belowMinimum(q quoted) error {
	if min := q.store.DeliveryMinOrderPaise; q.itemSubtotal < min {
		return problem.New(genproblems.BelowMinimum, "").
			WithExtension("minimumPaise", min).
			WithExtension("itemSubtotalPaise", q.itemSubtotal)
	}
	return nil
}

// deliveryHours are the store's delivery hours, its opening hours when it
// set none.
func deliveryHours(st ordersdb.StoreForBookingRow) []byte {
	if len(st.DeliveryHours) > 0 {
		return st.DeliveryHours
	}
	return st.Hours
}

// closedFor is the hours check that quote and place share (01 §5, D-219):
// a delivery order follows the delivery hours alone (delivery_closed), a
// pickup order the counter hours (store_closed); closed_until applies to
// both. The quote asks it for delivery only: a pickup quote stays
// informational (D-216) and placing refuses outside counter hours.
func (s *Service) closedFor(st ordersdb.StoreForBookingRow, f string, now time.Time) error {
	if f == Delivery {
		return s.deliveryClosed(st, now)
	}
	if s.open == nil {
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	if !s.open(st.Hours, st.ClosedUntil, now) {
		return problem.New(genproblems.StoreClosed, "")
	}
	return nil
}

// deliveryClosed refuses a delivery cart outside the store's delivery hours
// (or while it is closed by the shop) with delivery_closed and the next
// window in extensions.nextDeliveryWindow (null when nothing opens within
// a week).
func (s *Service) deliveryClosed(st ordersdb.StoreForBookingRow, now time.Time) error {
	if s.open == nil {
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	hours := deliveryHours(st)
	if s.open(hours, st.ClosedUntil, now) {
		return nil
	}
	var next any
	if w, ok := s.nextWindow(hours, st.ClosedUntil, now); ok {
		next = w
	}
	return problem.New(genproblems.DeliveryClosed, "").WithExtension("nextDeliveryWindow", next)
}

// window is a NextDeliveryWindow.
type window struct {
	OpensAt  time.Time `json:"opensAt"`
	ClosesAt time.Time `json:"closesAt"`
}

const (
	windowStep  = 15 * time.Minute
	windowAhead = 7 * 24 * time.Hour
)

// nextWindow finds the next delivery window after now: it walks forward in
// quarter hours for up to a week, then narrows each edge to the minute.
func (s *Service) nextWindow(hours []byte, closedUntil *time.Time, now time.Time) (window, bool) {
	open := func(t time.Time) bool { return s.open(hours, closedUntil, t) }
	start := now.Truncate(time.Minute).Add(time.Minute) // closed now, so from the next minute
	end := start.Add(windowAhead)
	t := start
	for !open(t) {
		t = t.Add(windowStep)
		if t.After(end) {
			return window{}, false
		}
	}
	lo := t.Add(-windowStep)
	if lo.Before(start) {
		lo = start.Add(-time.Minute)
	}
	opens := edge(open, lo, t)
	u := opens
	for open(u) && u.Before(opens.Add(windowAhead)) {
		u = u.Add(windowStep)
	}
	closes := u
	if !open(u) {
		closes = edge(func(x time.Time) bool { return !open(x) }, u.Add(-windowStep), u)
	}
	return window{OpensAt: opens.UTC(), ClosesAt: closes.UTC()}, true
}

// edge is the first minute in (lo, hi] where ok turns true; ok(hi) holds.
func edge(ok func(time.Time) bool, lo, hi time.Time) time.Time {
	for hi.Sub(lo) > time.Minute {
		mid := lo.Add(hi.Sub(lo) / 2).Truncate(time.Minute)
		if mid.Equal(lo) {
			break
		}
		if ok(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

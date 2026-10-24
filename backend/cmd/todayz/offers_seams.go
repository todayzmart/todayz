package main

import (
	"context"
	"errors"
	"time"

	"github.com/todayz/backend/gen/events"
	"github.com/todayz/backend/internal/banners"
	"github.com/todayz/backend/internal/offers"
	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/search"
)

// The offers domain meets orders, basket search and banners through small
// interfaces each caller owns (backend/CLAUDE.md); these adapters are the only
// place that knows both sides.

// offersForOrders adapts offers.Service to orders.Pricer and orders.Redeemer.
type offersForOrders struct{ s *offers.Service }

var (
	_ orders.Pricer   = offersForOrders{}
	_ orders.Redeemer = offersForOrders{}
)

// Price implements orders.Pricer: the online channel at the service clock's now.
func (a offersForOrders) Price(ctx context.Context, customerID, storeID ids.ID, lines []orders.PriceLine) (orders.Pricing, error) {
	cart := offers.Cart{StoreID: storeID, CustomerID: customerID, Channel: offers.ChannelOnline, Lines: make([]offers.Line, len(lines))}
	for i, l := range lines {
		cart.Lines[i] = offers.Line{StoreItemID: l.StoreItemID, UnitPricePaise: l.UnitPricePaise, Quantity: l.Quantity, Fixed: l.Fixed}
	}
	q, err := a.s.Price(ctx, cart)
	if err != nil {
		return orders.Pricing{}, err
	}
	return pricing(q, len(lines)), nil
}

// pricing is an offers quote of n lines as orders.Pricing.
func pricing(q offers.Quote, n int) orders.Pricing {
	out := orders.Pricing{LineDiscounts: make([]int64, n), BillDiscount: q.BillDiscountPaise}
	for i := range n {
		if i < len(q.Lines) {
			out.LineDiscounts[i] = q.Lines[i].DiscountPaise
		}
	}
	for _, ap := range q.Applied {
		out.Offers = append(out.Offers, orders.AppliedOffer{OfferID: ap.OfferID, Title: ap.Title, DiscountPaise: ap.DiscountPaise})
	}
	return out
}

// Reprice implements orders.Pricer: the offers the order holds, at its
// placing time.
func (a offersForOrders) Reprice(ctx context.Context, orderID, customerID, storeID ids.ID, placedAt time.Time, lines []orders.PriceLine) (orders.Pricing, error) {
	cart := offers.Cart{StoreID: storeID, CustomerID: customerID, Channel: offers.ChannelOnline, At: placedAt, Lines: make([]offers.Line, len(lines))}
	for i, l := range lines {
		cart.Lines[i] = offers.Line{StoreItemID: l.StoreItemID, UnitPricePaise: l.UnitPricePaise, Quantity: l.Quantity, Fixed: l.Fixed}
	}
	q, err := a.s.Reprice(ctx, offers.Ref{OrderID: orderID}, cart)
	if err != nil {
		return orders.Pricing{}, err
	}
	return pricing(q, len(lines)), nil
}

// Redeem implements orders.Redeemer. An offer that ran out is orders.ErrOfferGone.
func (a offersForOrders) Redeem(ctx context.Context, orderID, customerID ids.ID, applied []orders.AppliedOffer) error {
	q := offers.Quote{Applied: make([]offers.Applied, len(applied))}
	for i, ap := range applied {
		q.Applied[i] = offers.Applied{OfferID: ap.OfferID, Title: ap.Title, DiscountPaise: ap.DiscountPaise}
	}
	err := a.s.Redeem(ctx, offers.Ref{OrderID: orderID}, customerID, q)
	if errors.Is(err, offers.ErrUnavailable) {
		return orders.ErrOfferGone
	}
	return err
}

// Release implements orders.Redeemer.
func (a offersForOrders) Release(ctx context.Context, orderID ids.ID) error {
	return a.s.Release(ctx, offers.Ref{OrderID: orderID})
}

// offersForSearch adapts offers.Service to search.Pricer (basket totals).
type offersForSearch struct{ s *offers.Service }

var _ search.Pricer = offersForSearch{}

// LineDiscounts implements search.Pricer: item-level offers only, at time t.
// A minimum-bill offer is the shop's, applied at checkout, so it is left out.
func (a offersForSearch) LineDiscounts(ctx context.Context, customerID, storeID ids.ID, lines []search.PriceLine, t time.Time) ([]int64, error) {
	cart := offers.Cart{StoreID: storeID, CustomerID: customerID, Channel: offers.ChannelOnline, At: t, Lines: make([]offers.Line, len(lines))}
	for i, l := range lines {
		cart.Lines[i] = offers.Line{StoreItemID: l.StoreItemID, UnitPricePaise: l.UnitPricePaise, Quantity: l.Quantity}
	}
	q, err := a.s.Price(ctx, cart)
	if err != nil {
		return nil, err
	}
	out := make([]int64, len(lines))
	for i := range lines {
		if i < len(q.Lines) {
			out[i] = q.Lines[i].DiscountPaise
		}
	}
	return out, nil
}

// bannersForOffers adapts banners.Service to offers.BannerSource (the feed).
type bannersForOffers struct{ s *banners.Service }

var _ offers.BannerSource = bannersForOffers{}

// LiveBanners implements offers.BannerSource.
func (a bannersForOffers) LiveBanners(ctx context.Context, storeIDs []ids.ID, t time.Time) ([]offers.BannerCard, error) {
	live, err := a.s.LiveBanners(ctx, storeIDs, t)
	if err != nil {
		return nil, err
	}
	out := make([]offers.BannerCard, len(live))
	for i, b := range live {
		out[i] = offers.BannerCard{
			ID: b.ID, StoreID: b.StoreID, Headline: b.Headline, ImageKey: b.ImageKey, Template: b.Template,
			TargetType: b.TargetType, TargetID: b.TargetID, TargetItemIDs: b.TargetItemIDs,
		}
	}
	return out, nil
}

// eventCatalogue loads the product-event catalogue; one that does not load is
// a build error, so the API refuses to start.
func eventCatalogue(who string) *eventcat.Catalogue {
	cat, err := eventcat.Load(events.FS)
	if err != nil {
		panic(who + ": event catalogue: " + err.Error())
	}
	return cat
}

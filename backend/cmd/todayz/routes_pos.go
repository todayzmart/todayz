package main

import (
	"context"
	"errors"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/offers"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/pos"
	"github.com/todayz/backend/internal/reviews"
)

// posSeams lets a test swap in recording offers, credits and referrals
// seams; nil in the binary.
var posSeams func(*pos.Deps)

// posDeps is the pos service's deps in the API. Offers are W2-07's (the
// POS channel); stock, credits and referrals come from jobs.PosDeps, which
// the worker's sweep shares, over the API's credits ledger and referrals.
func posDeps(d *deps) pos.Deps {
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	off := offersForPos{newOffers(d)}
	pd := jobs.PosDeps(pos.Deps{Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log, Settings: d.Settings, Authz: az},
		creditsService(d), newReferrals(d))
	pd.Pricer, pd.Redeemer = off, off
	return pd
}

// newPos builds the pos service.
func newPos(d *deps) *pos.Service {
	pd := posDeps(d)
	if posSeams != nil {
		posSeams(&pd)
	}
	svc, err := pos.New(pd)
	if err != nil {
		// A missing clock or a malformed key; boot and buildSecurity refuse
		// those before the routes mount.
		panic("pos: " + err.Error())
	}
	return svc
}

// mountPos mounts W2-04's routes: the online sale and its reversal, bill
// claims and counter codes, the offline sync, the public receipt and the
// customer's purchases (the pos, sync and receipts domains).
func mountPos(r chi.Router, d *deps) {
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	pos.Register(r, newPos(d), az)
}

// offersForPos adapts offers.Service to pos.Pricer and pos.Redeemer: the
// POS channel at the service clock's now.
type offersForPos struct{ s *offers.Service }

var (
	_ pos.Pricer   = offersForPos{}
	_ pos.Redeemer = offersForPos{}
)

// Price implements pos.Pricer at at (the zero time is the offers clock's
// now). An online sale's catalogued lines carry the catalogue price
// (anything else is price_changed), so no line is Fixed.
func (a offersForPos) Price(ctx context.Context, customerID, storeID ids.ID, at time.Time, lines []pos.PriceLine) (pos.Pricing, error) {
	cart := offers.Cart{StoreID: storeID, CustomerID: customerID, Channel: offers.ChannelPOS, Lines: make([]offers.Line, len(lines)), At: at}
	for i, l := range lines {
		cart.Lines[i] = offers.Line{StoreItemID: l.StoreItemID, UnitPricePaise: l.UnitPricePaise, Quantity: l.Quantity}
	}
	q, err := a.s.Price(ctx, cart)
	if err != nil {
		return pos.Pricing{}, err
	}
	out := pos.Pricing{LineDiscounts: make([]int64, len(lines)), BillDiscount: q.BillDiscountPaise}
	for i := range lines {
		if i < len(q.Lines) {
			out.LineDiscounts[i] = q.Lines[i].DiscountPaise
		}
	}
	for _, ap := range q.Applied {
		out.Offers = append(out.Offers, pos.AppliedOffer{OfferID: ap.OfferID, Title: ap.Title, DiscountPaise: ap.DiscountPaise})
	}
	return out, nil
}

// Redeem implements pos.Redeemer. An offer that ran out is pos.ErrOfferGone.
func (a offersForPos) Redeem(ctx context.Context, saleID, customerID ids.ID, applied []pos.AppliedOffer) error {
	q := offers.Quote{Applied: make([]offers.Applied, len(applied))}
	for i, ap := range applied {
		q.Applied[i] = offers.Applied{OfferID: ap.OfferID, Title: ap.Title, DiscountPaise: ap.DiscountPaise}
	}
	err := a.s.Redeem(ctx, offers.Ref{POSSaleID: saleID}, customerID, q)
	if errors.Is(err, offers.ErrUnavailable) {
		return pos.ErrOfferGone
	}
	return err
}

// Release implements pos.Redeemer.
func (a offersForPos) Release(ctx context.Context, saleID ids.ID) error {
	return a.s.Release(ctx, offers.Ref{POSSaleID: saleID})
}

// posForReviews adapts pos.Service to reviews.SaleSource.
type posForReviews struct{ s *pos.Service }

var _ reviews.SaleSource = posForReviews{}

// Sale implements reviews.SaleSource.
func (a posForReviews) Sale(ctx context.Context, saleID ids.ID) (reviews.Sale, bool, error) {
	r, ok, err := a.s.SaleRef(ctx, saleID)
	if err != nil || !ok {
		return reviews.Sale{}, ok, err
	}
	return reviews.Sale{StoreID: r.StoreID, CustomerID: r.CustomerID, LinkMethod: r.LinkMethod, Completed: r.Completed}, true, nil
}

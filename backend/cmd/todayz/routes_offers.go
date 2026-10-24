package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/offers"
	"github.com/todayz/backend/internal/platform/authz"
)

// newOffers builds the offers service: the shop's offers, the pricing that
// orders and basket search share, and the nearby feed with its banner cards.
func newOffers(d *deps) *offers.Service {
	svc, err := offers.New(offers.Deps{
		Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log, Settings: d.Settings,
		Banners: bannersForOffers{newBanners(d)}, Events: eventCatalogue("offers"),
	})
	if err != nil {
		panic("offers: " + err.Error())
	}
	return svc
}

// mountOffers mounts W2-07's offer routes: a shop's offers under
// /v1/biz/stores/{sid}/offers and the public GET /v1/discover/offers.
func mountOffers(r chi.Router, d *deps) {
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	offers.Register(r, newOffers(d), az)
}

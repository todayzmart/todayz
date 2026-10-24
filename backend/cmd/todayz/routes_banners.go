package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/banners"
	"github.com/todayz/backend/internal/platform/authz"
)

// newBanners builds the banners service. Both mountBanners (its routes) and
// mountOffers (the feed's banner cards) use one built from the same deps.
func newBanners(d *deps) *banners.Service {
	svc, err := banners.New(banners.Deps{
		Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log, Settings: d.Settings,
		Events: eventCatalogue("banners"),
	})
	if err != nil {
		// A missing clock; boot refuses that before the routes mount.
		panic("banners: " + err.Error())
	}
	return svc
}

// mountBanners mounts W2-07's banner routes: a shop's banners under
// /v1/biz/stores/{sid}/banners and the customer's tap.
func mountBanners(r chi.Router, d *deps) {
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	banners.Register(r, newBanners(d), az)
}

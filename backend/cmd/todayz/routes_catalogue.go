package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/platform/authz"
)

// mountCatalogue mounts the catalogue and stock routes (W1-05): categories,
// product search, store items, movements and batches. The engine runs the
// route's check; the service runs the second check of a movement kind or a
// price change.
func mountCatalogue(r chi.Router, d *deps) {
	cd := catalogue.Deps{Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log, Settings: d.Settings}
	var az authz.Authorizer
	if d.Authz != nil {
		az, cd.Authz = d.Authz, d.Authz
	}
	svc, err := catalogue.New(cd)
	if err != nil {
		// Only a missing clock gets here; the API always has one.
		panic("catalogue: " + err.Error())
	}
	catalogue.Register(r, svc, az)
}

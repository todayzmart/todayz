package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/pos"
)

// mountPos mounts W2-04's routes: the online sale and its reversal, bill
// claims and counter codes, the offline sync, the public receipt and the
// customer's purchases (the pos, sync and receipts domains). Offers,
// credits and referrals are the package's do-nothing defaults until W2-07
// and W2-01 wire theirs here.
func mountPos(r chi.Router, d *deps) {
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	svc, err := pos.New(pos.Deps{
		Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log, Settings: d.Settings,
		Authz: az, Stock: jobs.PosStock(d.Clock),
	})
	if err != nil {
		// A missing clock or a malformed key; boot and buildSecurity refuse
		// those before the routes mount.
		panic("pos: " + err.Error())
	}
	pos.Register(r, svc, az)
}

package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/business/standee"
	"github.com/todayz/backend/internal/platform/authz"
)

// mountStandee mounts W3-09's standee routes under
// /v1/biz/businesses/{bid}/stores/{sid}/standees: the store's standees with
// their scan → sign-up → qualified counts, a new standee and a reorder.
func mountStandee(r chi.Router, d *deps) {
	svc, err := standee.New(standee.Deps{Config: d.Config, DB: d.DB, Clock: d.Clock})
	if err != nil {
		panic("standee: " + err.Error())
	}
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	standee.Register(r, svc, az)
}

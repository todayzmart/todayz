package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/coach"
	"github.com/todayz/backend/internal/platform/authz"
)

// mountCoach mounts W3-11's routes under /v1/biz/businesses/{bid}/stores/{sid}:
// the coach cards of B-19 and the Overview coach slot, Done and Not useful.
// The API only reads and acts on cards; the worker's coach jobs write them.
func mountCoach(r chi.Router, d *deps) {
	svc, err := coach.New(coach.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings})
	if err != nil {
		panic("coach: " + err.Error())
	}
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	coach.Register(r, svc, az)
}

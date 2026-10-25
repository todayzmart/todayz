package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/asks"
	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/search"
)

// newAsks builds the asks service: the routes, the reply price orders reads
// and the unanswered demand insights reads (W3-16, D-101).
func newAsks(d *deps) *asks.Service {
	ad := asks.Deps{
		DB: d.DB, Clock: d.Clock, Settings: d.Settings, Events: eventCatalogue("asks"),
		Normalize: search.Normalize, OpenAt: business.OpenAt,
	}
	if d.DB != nil {
		ad.Notices = newNotifyService(d)
	}
	if d.Authz != nil {
		ad.Authz = d.Authz
	}
	svc, err := asks.New(ad)
	if err != nil {
		panic("asks: " + err.Error())
	}
	return svc
}

// mountAsks mounts W3-16's routes: the customer's asks under /v1/me/asks and
// the store inbox under /v1/biz/businesses/{bid}/stores/{sid}/asks. Account
// deletion deletes the customer's asks and the blocks they caused (01 §16).
func mountAsks(r chi.Router, d *deps) {
	svc := newAsks(d)
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	asks.Register(r, svc, az)
	if d.Identity != nil {
		d.Identity.AddDeletionHook(identity.OnDeleteFunc(svc.OnAccountDelete))
	}
}

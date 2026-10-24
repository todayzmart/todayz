package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/baskets"
	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/search"
)

// mountBaskets mounts W2-14's saved baskets (/v1/me/baskets*). Basket search
// is a discover operation (mountSearch). Free text uses search's
// normalisation; account deletion deletes the baskets (01 §16).
func mountBaskets(r chi.Router, d *deps) {
	svc, err := baskets.New(baskets.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, Normalize: search.Normalize})
	if err != nil {
		panic("baskets: " + err.Error())
	}
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	baskets.Register(r, svc, az)
	if d.Identity != nil {
		d.Identity.AddDeletionHook(identity.OnDeleteFunc(svc.OnAccountDelete))
	}
}

package main

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/gen/events"
	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/search"
)

// mountSearch mounts /v1/places/* and /v1/discover/* (W1-06). Open-now uses
// the business domain's hours rules (W1-04) through search.OpenChecker; the
// offer lookup stays search.NoOffers until offers land (W2-07). A catalogue
// that does not load is a build error: the API refuses to start.
func mountSearch(r chi.Router, d *deps) {
	cat, err := eventcat.Load(events.FS)
	if err != nil {
		panic("search: event catalogue: " + err.Error())
	}
	svc, err := search.New(search.Deps{
		DB: d.DB, Settings: d.Settings, Clock: d.Clock, Log: d.Log,
		Open:   search.OpenFunc(business.OpenAt),
		UserID: func(ctx context.Context) string { return reqctx.PrincipalFrom(ctx).UserID },
	}, cat)
	if err != nil {
		panic("search: " + err.Error())
	}
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	search.Register(r, svc, az)
}

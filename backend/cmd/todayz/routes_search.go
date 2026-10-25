package main

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/gen/events"
	"github.com/todayz/backend/internal/ai/querynorm"
	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/search"
)

// refineChips reads refine chips with the deterministic parser of internal/ai/querynorm
// (D-101, W3-12; no model).
type refineChips struct{ d *deps }

func (r refineChips) ReadChips(ctx context.Context, q string) (string, []search.RefineChip, error) {
	p, err := querynorm.ReadQuery(ctx, r.d.DB, q)
	if err != nil {
		return "", nil, err
	}
	chips := make([]search.RefineChip, 0, len(p.Chips))
	for _, c := range p.Chips {
		chips = append(chips, search.RefineChip{Kind: c.Kind, Value: c.Value, Text: c.Text})
	}
	return p.Rest, chips, nil
}

// mountSearch mounts /v1/places/* and /v1/discover/* (W1-06). Open-now uses
// the business domain's hours rules (W1-04) through search.OpenChecker; the
// offer lookup stays search.NoOffers until offers land (W2-07). A catalogue
// that does not load is a build error: the API refuses to start.
func mountSearch(r chi.Router, d *deps) {
	cat, err := eventcat.Load(events.FS)
	if err != nil {
		panic("search: event catalogue: " + err.Error())
	}
	offs := newOffers(d)
	svc, err := search.New(search.Deps{
		DB: d.DB, Settings: d.Settings, Clock: d.Clock, Log: d.Log,
		Offers: offs, Pricer: offersForSearch{offs},
		Open:   search.OpenFunc(business.OpenAt),
		UserID: func(ctx context.Context) string { return reqctx.PrincipalFrom(ctx).UserID },
		Chips:  refineChips{d},
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

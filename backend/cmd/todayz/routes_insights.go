package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/insights"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/pii"
)

// mountInsights mounts W3-03's routes under /v1/biz/businesses/{bid}/stores/{sid}:
// the B-03 overview, Demand insights with "Not useful", the B-12 analytics and
// the B-09 customers. Unanswered asks join the nearby searches through
// facts.AskDemand once W3-16's asks.Demand lands (D-101); until then zero.
func mountInsights(r chi.Router, d *deps) {
	id := insights.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings}
	if d.Config != nil {
		if k := d.Config.Keys.PIIEncKey; k.IsSet() {
			seal, err := pii.NewSealer(k.Bytes())
			if err != nil {
				panic("insights: PII_ENC_KEY must be 32 bytes")
			}
			id.Seal = seal
		}
	}
	svc, err := insights.New(id)
	if err != nil {
		panic("insights: " + err.Error())
	}
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	insights.Register(r, svc, az)
}

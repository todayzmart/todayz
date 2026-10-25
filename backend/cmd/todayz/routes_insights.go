package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/ai/insighttext"
	"github.com/todayz/backend/internal/insights"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/pii"
)

// mountInsights mounts W3-03's routes under /v1/biz/businesses/{bid}/stores/{sid}:
// the B-03 overview, Demand insights with "Not useful", the B-12 analytics and
// the B-09 customers. Unanswered asks join the nearby searches through
// facts.AskDemand from W3-16's asks.Demand (D-101).
// W3-11's insighttext words the rows (AI-R4) when the platform is configured.
func mountInsights(r chi.Router, d *deps) {
	id := insights.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, Asks: asksForInsights{newAsks(d)}}
	if d.DB != nil && d.Config != nil && d.Settings != nil {
		k := aiKitFor(d)
		id.Texts = insighttext.New(insighttext.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, LLM: k.LLM, Guard: k.Guard})
	}
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

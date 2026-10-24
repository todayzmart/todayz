package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/platform/authz"
)

// mountCredits mounts W2-01's reads (the customer's wallet and lines, the
// business credit account) and registers the deletion forfeiture (D-31)
// with identity. Orders gets the same ledger through creditsService.
func mountCredits(r chi.Router, d *deps) {
	svc := creditsService(d)
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	credits.Register(r, svc, az)
	if d.Identity != nil {
		d.Identity.AddDeletionHook(svc)
	}
}

// creditsService builds the ledger on the API's pool, settings and notices.
func creditsService(d *deps) *credits.Service {
	jd := jobs.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log}
	if d.DB != nil {
		jd.Pool = d.DB.Pool
	}
	svc, err := jobs.CreditsService(jd, newNotifyService(d))
	if err != nil {
		// Only a missing clock, which boot refuses before the routes mount.
		panic("credits: " + err.Error())
	}
	return svc
}

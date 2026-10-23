package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/platform/authz"
)

// mountBusiness mounts /v1/biz/* and /v1/public/leads (W1-04). The lead form
// checks Turnstile with the identity service's verifier; the engine runs the
// second check of a staff or asks change.
func mountBusiness(r chi.Router, d *deps) {
	bd := business.Deps{Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log}
	if d.Identity != nil {
		bd.Human = d.Identity.HumanVerifier()
	}
	var az authz.Authorizer
	if d.Authz != nil {
		az, bd.Authz = d.Authz, d.Authz
	}
	svc, err := business.New(bd)
	if err != nil {
		// Only a malformed PII key gets here, and buildSecurity
		// (identity.New) refuses the same key before the routes mount.
		panic("business: " + err.Error())
	}
	business.Register(r, svc, az)
}

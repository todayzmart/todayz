package main

import (
	"context"

	"github.com/go-chi/chi/v5"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
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
	if d.Identity != nil {
		// D-106: an owner of a business (a draft counts) cannot delete the
		// account; a deletion that goes ahead withdraws its pending invites.
		d.Identity.AddDeletionCheck(identity.CanDeleteFunc(func(ctx context.Context, uid ids.ID) error {
			owns, err := business.OwnsActiveBusiness(ctx, d.DB, uid)
			if err != nil {
				return err
			}
			if owns {
				return problem.New(genproblems.Conflict, "This account owns a business on TodayZ. Close the business first, then delete the account.")
			}
			return nil
		}))
		d.Identity.AddDeletionHook(svc)
	}
}

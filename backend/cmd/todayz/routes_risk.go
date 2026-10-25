package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/risk"
)

// mountRisk mounts W3-02's admin routes: A-13 Risk (/v1/admin/risk/flags*)
// and A-10 Referrals (/v1/admin/referrals*). The admin-referrals domain has
// no routes file of its own (one routes file per domain).
func mountRisk(r chi.Router, d *deps) {
	rd := risk.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, OTPMaxAttempts: identity.OTPMaxAttempts}
	if d.DB != nil {
		var hashKey []byte
		if d.Config != nil {
			hashKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		rd.Audit = audit.New(d.DB, d.Clock, hashKey)
	}
	svc, err := risk.New(rd)
	if err != nil {
		panic("risk: " + err.Error())
	}
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	risk.Register(r, svc, az)
}

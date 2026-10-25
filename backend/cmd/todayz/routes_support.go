package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/support"
)

// mountSupport mounts W3-08's support tickets: the customer's own tickets
// (/v1/me/support/*, C-19), the business's help and tickets
// (/v1/biz/businesses/{bid}/support/*, B-15) and the admin inbox
// (/v1/admin/tickets*, A-14). There is no routes_admin-support.go or
// routes_biz-support.go (one routes file per domain).
func mountSupport(r chi.Router, d *deps) {
	sd := support.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings}
	if d.Config != nil {
		sd.Storage = d.Config.Storage
	}
	if d.DB != nil {
		var hashKey []byte
		if d.Config != nil {
			hashKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		sd.Audit = audit.New(d.DB, d.Clock, hashKey)
	}
	svc, err := support.New(sd)
	if err != nil {
		panic("support: " + err.Error())
	}
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	support.Register(r, svc, az)
}

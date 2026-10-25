package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/rollups"
)

// mountRollups mounts the rollups domain's routes: the admin Pulse (A-02,
// /v1/admin/pulse and /v1/admin/pulse/map) behind admin.pulse.view.
func mountRollups(r chi.Router, d *deps) {
	svc := rollups.New(rollups.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings})
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	rollups.Register(r, svc, az)
}

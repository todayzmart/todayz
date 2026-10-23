package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/platform/audit"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/jobs/adminapi"
)

// mountAdmin mounts the admin domain's routes. Only the work package that builds
// internal/admin edits this file (D-50): build the domain's service from d and
// call its Register, e.g. admin.Register(r, admin.Deps{DB: d.DB, Clock: d.Clock}).
//
// W1-11 mounts the admin-jobs operations here (/v1/admin/jobs*, /v1/admin/job-runs*,
// A-21): they are admin routes and there is no routes_admin-jobs.go (one routes
// file per domain, TestOneMountPerDomain).
func mountAdmin(r chi.Router, d *deps) {
	mountAdminJobs(r, d)
}

// mountAdminJobs builds the same job registry as the worker (so A-21 lists
// what the worker runs) and mounts the admin-jobs API.
func mountAdminJobs(r chi.Router, d *deps) {
	reg := platformjobs.NewRegistry()
	jd := jobs.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log}
	ad := adminapi.Deps{Registry: reg, Clock: d.Clock, Log: d.Log,
		// TODO(W1-01): pass W1-01's admin Gate (session, role, step-up). Until
		// then every admin-jobs request is 401 (fail closed).
		Gate: adminapi.DenyGate{}}
	if d.DB != nil {
		jd.Pool = d.DB.Pool
		ad.DB = d.DB
		var hashKey []byte
		if d.Config != nil {
			hashKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		ad.Audit = audit.New(d.DB, d.Clock, hashKey)
	}
	jobs.Register(reg, jd)
	if d.Queue != nil {
		ad.Enqueuer = platformjobs.NewEnqueuer(reg, d.Queue, d.Clock)
	}
	adminapi.Register(r, ad)
}

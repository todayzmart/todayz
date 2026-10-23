// Package jobs is the registry of job kinds (02 §2.6, D-95).
//
// One line per domain in Domains and one file per domain (<domain>.go) that
// only its work package edits, so parallel WPs never touch the same lines.
// A domain declares each kind with jobs.Define(r, jobs.Kind{...}) from
// internal/platform/jobs (import it as platformjobs or, inside this package,
// alias it); registering a River worker any other way fails
// TestWorkersOnlyThroughDefine (SC-JB-01). The worker builds the registry and
// runs it through platformjobs.Runner; the API builds the same registry to
// list kinds and to enqueue (platformjobs.NewEnqueuer).
package jobs

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/settings"
)

// Registry is the jobs platform's registry, so domain files need no import
// for it.
type Registry = platformjobs.Registry

// Deps are what domain jobs may use. Domains add their own services by
// building them from these inside their register function.
type Deps struct {
	Pool     *pgxpool.Pool
	Clock    clock.Clock
	Settings *settings.Cache
	Config   *config.Config
	Log      *slog.Logger
}

// Domain is one registry line.
type Domain struct {
	Name     string
	Register func(r *Registry, d Deps)
}

// Domains lists every domain, one line each (07 W0-03).
var Domains = []Domain{
	{"identity", registeridentity},
	{"business", registerbusiness},
	{"catalogue", registercatalogue},
	{"search", registersearch},
	{"telemetry", registertelemetry},
	{"credits", registercredits},
	{"orders", registerorders},
	{"referrals", registerreferrals},
	{"pos", registerpos},
	{"notify", registernotify},
	{"reviews", registerreviews},
	{"offers", registeroffers},
	{"banners", registerbanners},
	{"support", registersupport},
	{"payouts", registerpayouts},
	{"risk", registerrisk},
	{"insights", registerinsights},
	{"voice", registervoice},
	{"admin", registeradmin},
	{"rollups", registerrollups},
	{"ai", registerai},
	{"coach", registercoach},
	{"standee", registerstandee},
}

// Register declares the platform kinds (heartbeat, maintenance, the jobs
// watchdog and job_runs retention) and every domain's kinds in r.
func Register(r *Registry, d Deps) {
	platformjobs.DefinePlatform(r, platformjobs.PlatformDeps{Pool: d.Pool, Clock: d.Clock, Settings: d.Settings, Log: d.Log})
	for _, dom := range Domains {
		dom.Register(r, d)
	}
}

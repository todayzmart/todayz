// Package jobs is the registry of River workers and periodic jobs (02 §2.6).
//
// One line per domain in Domains and one file per domain (<domain>.go) that
// only its work package edits, so parallel WPs never touch the same lines.
// The worker process calls Register; the API only inserts jobs.
package jobs

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/platform/settings"
)

// Registry is queue.Registry, so domain files need no import for it.
type Registry = queue.Registry

// Add registers a worker (queue.Add).
func Add[T queue.JobArgs](r *Registry, w queue.Worker[T]) { queue.Add(r, w) }

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

// Register adds the platform jobs (heartbeat, maintenance) and every
// domain's jobs to r.
func Register(r *Registry, d Deps) {
	queue.RegisterPlatform(r, queue.PlatformDeps{Pool: d.Pool, Clock: d.Clock, Log: d.Log})
	for _, dom := range Domains {
		dom.Register(r, d)
	}
}

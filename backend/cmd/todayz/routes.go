package main

// routes.go belongs to the lead (backend/CLAUDE.md). Each domain has one
// line below calling mountX(r, &d), defined in routes_<domain>.go; a domain
// work package edits only its own routes_<domain>.go (D-50).

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/testclock"
)

// deps is what route registration may hand to domains.
type deps struct {
	Config    *config.Config
	Log       *slog.Logger
	Clock     clock.Clock
	DB        *db.DB
	Settings  *settings.Cache
	Queue     *queue.Client // insert-only in the API
	Ready     *health.Readiness
	TestClock *testclock.Service // nil in staging and prod
}

func routes(r chi.Router, d deps) {
	health.Register(r, d.Ready, health.Options{Version: !d.Config.Env.IsDeployed()}) // /healthz, /readyz; /version in local and test
	testclock.Register(r, d.Config.Env, d.Config.APIAddr, d.TestClock)               // /v1/test/clock: local and test, on loopback only (D-80)

	// Domain routes: one line per domain; each mountX lives in routes_<domain>.go,
	// which only that domain's work package edits (D-50).
	mountIdentity(r, &d)
	mountBusiness(r, &d)
	mountCatalogue(r, &d)
	mountSearch(r, &d)
	mountTelemetry(r, &d)
	mountCredits(r, &d)
	mountOrders(r, &d)
	mountReferrals(r, &d)
	mountPos(r, &d)
	mountNotify(r, &d)
	mountReviews(r, &d)
	mountOffers(r, &d)
	mountBanners(r, &d)
	mountSupport(r, &d)
	mountPayouts(r, &d)
	mountRisk(r, &d)
	mountInsights(r, &d)
	mountVoice(r, &d)
	mountAdmin(r, &d)
	mountRollups(r, &d)
	mountAI(r, &d)
	mountCoach(r, &d)
	mountStandee(r, &d)
}

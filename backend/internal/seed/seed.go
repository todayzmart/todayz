// Package seed is the registry of seed data (02 §2.1, 08 §Seed).
//
// `todayz seed dev|demo|test` runs every domain in Domains order (parents
// before children) inside one transaction, as todayz_app (DATABASE_URL), so
// seed data obeys the same privileges as the services. It refuses to run when
// APP_ENV is staging or prod.
//
// One line per domain in Domains and one file per domain (<domain>.go) that
// only its work package edits.
package seed

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Profile selects a data set.
type Profile string

// Profiles.
const (
	// Dev is what `make reset` loads: a few shops, products and users to click through.
	Dev Profile = "dev"
	// Demo is a larger, realistic data set for demos and dashboards.
	Demo Profile = "demo"
	// Test is the fixed data e2e and contract tests rely on.
	Test Profile = "test"
)

// Profiles lists every profile.
var Profiles = []Profile{Dev, Demo, Test}

// Valid reports whether p is a known profile.
func (p Profile) Valid() bool {
	for _, q := range Profiles {
		if p == q {
			return true
		}
	}
	return false
}

// Epoch is the seed clock's "now": seed data is the same on every run.
var Epoch = time.Date(2026, 1, 5, 4, 30, 0, 0, time.UTC) // Monday 10:00 IST

// Deps are what seed functions use.
type Deps struct {
	DB    *db.DB
	Clock clock.Clock // fixed at Epoch unless the caller says otherwise
	Log   *slog.Logger
	// Keys seal and hash seeded PII (phones) the way the services do (W1-01).
	Keys config.AppKeys
}

// Domain is one registry line.
type Domain struct {
	Name string
	Seed func(ctx context.Context, d Deps, p Profile) error
}

// Domains lists every domain in dependency order (07 W0-03).
var Domains = []Domain{
	{"identity", seedidentity},
	{"business", seedbusiness},
	{"catalogue", seedcatalogue},
	{"search", seedsearch},
	{"telemetry", seedtelemetry},
	{"credits", seedcredits},
	{"orders", seedorders},
	{"referrals", seedreferrals},
	{"pos", seedpos},
	{"notify", seednotify},
	{"reviews", seedreviews},
	{"offers", seedoffers},
	{"banners", seedbanners},
	{"support", seedsupport},
	{"payouts", seedpayouts},
	{"risk", seedrisk},
	{"insights", seedinsights},
	{"voice", seedvoice},
	{"admin", seedadmin},
	{"rollups", seedrollups},
	{"ai", seedai},
	{"coach", seedcoach},
	{"standee", seedstandee},
	{"baskets", seedbaskets},
	{"asks", seedasks},
}

// ErrRefused is returned in staging and prod.
type ErrRefused struct{ Env config.Env }

func (e ErrRefused) Error() string {
	return fmt.Sprintf("seed: refused: APP_ENV is %s; seed runs only in local and test", e.Env)
}

// Allowed returns ErrRefused for deployed environments.
func Allowed(env config.Env) error {
	if env.IsDeployed() || !env.Valid() {
		return ErrRefused{Env: env}
	}
	return nil
}

// Run seeds profile p for env in one transaction.
func Run(ctx context.Context, env config.Env, p Profile, d Deps) error {
	if err := Allowed(env); err != nil {
		return err
	}
	if !p.Valid() {
		return fmt.Errorf("seed: unknown profile %q (dev, demo, test)", p)
	}
	if d.Clock == nil {
		d.Clock = clock.NewFake(Epoch)
	}
	if d.Log == nil {
		d.Log = telemetry.Default()
	}
	return d.DB.InTx(ctx, func(ctx context.Context) error {
		for _, dom := range Domains {
			if err := dom.Seed(ctx, d, p); err != nil {
				return fmt.Errorf("seed %s: %w", dom.Name, err)
			}
		}
		d.Log.InfoContext(ctx, "seeded", slog.String("profile", string(p)), slog.Int("domains", len(Domains)))
		return nil
	})
}

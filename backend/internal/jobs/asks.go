package jobs

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/asks"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// The asks kinds (W3-16, docs/runbooks/asks-timers.md).
const (
	// KindAsksCloseWindow closes asks every shop answered or whose reply
	// window is over (timer class: the customer's C-26 waits on it).
	KindAsksCloseWindow = "asks_close_window"
	// KindAsksRetention deletes asks past ask_retention_days.
	KindAsksRetention = "asks_retention"
	// KindRollupAskDemand recomputes today's and yesterday's ask demand.
	KindRollupAskDemand = "rollup_ask_demand"
)

const asksRunbook = "docs/runbooks/asks-timers.md"

// registerasks declares the asks domain's job kinds.
func registerasks(r *Registry, d Deps) {
	svc, err := asks.New(asks.Deps{DB: db.New(d.Pool), Clock: d.Clock, Settings: d.Settings})
	if err != nil {
		panic("jobs: asks: " + err.Error())
	}
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindAsksCloseWindow, Owner: "W3-16", Class: platformjobs.ClassTimer, Runbook: asksRunbook,
		Description: "Closes asks every shop has answered or whose reply window is over",
		Schedule:    "* * * * *", UniqueBy: platformjobs.PerTick, Timeout: 50 * time.Second, MaxAttempts: 1,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			n, err := svc.CloseEnded(ctx)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"closed": n}}, nil
		},
	})
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindAsksRetention, Owner: "W3-16", Class: platformjobs.ClassMaintenance,
		Description: "Deletes asks, their replies and the blocks they caused after ask_retention_days",
		Schedule:    "20 3 * * *", UniqueBy: platformjobs.PerDay, Timeout: 55 * time.Second, MaxAttempts: 3,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			n, err := svc.Retain(ctx)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"deleted": n}}, nil
		},
	})
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindRollupAskDemand, Owner: "W3-16", Class: platformjobs.ClassRollup,
		Description: "Recomputes today's and yesterday's closed-ask demand per store and cell (insights, A-23)",
		Schedule:    "*/15 * * * *", UniqueBy: platformjobs.PerTick, Timeout: rollupTimeout, MaxAttempts: 3,
		Handler: func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			at := run.PeriodStart
			if at.IsZero() {
				at = d.Clock.Now()
			}
			today := clock.StartOfDayIST(at)
			var rows int64
			for _, day := range []time.Time{today.Add(-time.Hour), today} {
				n, err := svc.RollupDay(ctx, day)
				if err != nil {
					return platformjobs.Result{}, err
				}
				rows += n
			}
			return platformjobs.Result{Summary: map[string]int64{"rows": rows}}, nil
		},
	})
}

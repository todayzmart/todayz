package jobs

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/asks"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/share"
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
	// KindSiteMarqueeBuild proposes each city's site strip for A-23.
	KindSiteMarqueeBuild = "site_marquee_build"
	// KindShareCardRender redraws a share card whose image shows another
	// price; a card read queues it (event kind, no schedule).
	KindShareCardRender = share.KindRender
)

const asksRunbook = "docs/runbooks/asks-timers.md"

// registerasks declares the asks domain's job kinds.
func registerasks(r *Registry, d Deps) {
	svc, err := asks.New(asks.Deps{DB: db.New(d.Pool), Clock: d.Clock, Settings: d.Settings})
	if err != nil {
		panic("jobs: asks: " + err.Error())
	}
	sd := share.Deps{
		DB: db.New(d.Pool), Clock: d.Clock, Settings: d.Settings,
		// The jobs never link a card; the origins are unused.
		AppURL: "-", ImageURL: "-",
	}
	if c := d.Config; c != nil {
		if st := share.NewS3Store(c.Storage, httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: c.NoEgress}), d.Clock); st != nil {
			sd.Images = st
		}
	}
	marquee, err := share.New(sd)
	if err != nil {
		panic("jobs: share: " + err.Error())
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
			opens, err := marquee.RetainOpens(ctx)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"deleted": n, "share_opens_deleted": opens}}, nil
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
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindShareCardRender, Owner: "W3-16", Class: platformjobs.ClassMaintenance, Runbook: asksRunbook,
		Description: "Redraws a share card image that shows another price than the item's; a card read queues it and serves the old image",
		Timeout:     30 * time.Second, MaxAttempts: 5,
		Handler: func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			var a share.RenderArgs
			if err := run.Decode(&a); err != nil {
				return platformjobs.Result{}, err
			}
			id, err := ids.Parse(a.CardID)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{}, marquee.Render(ctx, id)
		},
	})
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindSiteMarqueeBuild, Owner: "W3-16", Class: platformjobs.ClassRollup, Runbook: asksRunbook,
		Description: "Proposes each city's site strip from the last site_marquee_window_days of searches (A-23 approves it)",
		Schedule:    "10 4 * * *", UniqueBy: platformjobs.PerDay, Timeout: rollupTimeout, MaxAttempts: 3,
		Handler: func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			at := run.PeriodStart
			if at.IsZero() {
				at = d.Clock.Now()
			}
			n, err := marquee.Build(ctx, at)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"cities": n}}, nil
		},
	})
}

package jobs

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/platform/db"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/rollups"
)

// The rollup kinds (05 §3). Each run recomputes whole IST days from the raw
// tables, so a retry, an overlap or a backfill writes the same rows.
const (
	KindRollupToday     = "rollup_today"
	KindRollupDay       = "rollup_day"
	KindRollupDemand    = "rollup_demand"
	KindRollupCohorts   = "rollup_cohorts"
	KindRollupItemPairs = "rollup_item_pairs"
	KindRollupBanners   = "rollup_banners"
)

// rollupTimeout keeps every rollup run under the 60 s cap for kinds that are
// not batched.
const rollupTimeout = 55 * time.Second

// rollupYesterdayGrace is how long after midnight rollup_today still
// refreshes yesterday, so its final row lands before rollup_day runs.
const rollupYesterdayGrace = 30 * time.Minute

// registerrollups declares the rollups domain's job kinds.
func registerrollups(r *Registry, d Deps) {
	svc := rollups.New(rollups.Deps{DB: db.New(d.Pool), Clock: d.Clock, Settings: d.Settings})
	// day is the IST day a run's period starts in (a backfill or manual run
	// carries the period it was asked for).
	day := func(run platformjobs.Run) rollups.Day {
		if run.PeriodStart.IsZero() {
			return svc.Today()
		}
		return rollups.DayOf(run.PeriodStart)
	}
	define := func(k platformjobs.Kind) {
		k.Owner, k.Class, k.Timeout, k.MaxAttempts = "W3-07", platformjobs.ClassRollup, rollupTimeout, 3
		platformjobs.Define(r, k)
	}

	define(platformjobs.Kind{
		Name: KindRollupToday, Schedule: "*/5 * * * *", UniqueBy: platformjobs.PerTick,
		Description: "Recomputes today's platform and store rollups (Pulse and dashboards 07–11)",
		Handler: func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			d := day(run)
			days := []rollups.Day{d}
			if !run.PeriodStart.IsZero() && run.PeriodStart.Sub(d.Start()) < rollupYesterdayGrace {
				days = append(days, d.Add(-1))
			}
			var stores int64
			for _, x := range days {
				res, err := svc.ComputeDay(ctx, x)
				if err != nil {
					return platformjobs.Result{}, err
				}
				stores += res.Stores
			}
			return platformjobs.Result{Summary: map[string]int64{"days": int64(len(days)), "stores": stores}}, nil
		},
	})
	define(platformjobs.Kind{
		Name: KindRollupDay, Schedule: "30 0 * * *", UniqueBy: platformjobs.PerDay,
		Description: "Recomputes the three complete days before the run's day (platform, store, demand, banners) so late rows land",
		Handler: func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			d := day(run)
			var stores, demand, banners int64
			for i := 1; i <= 3; i++ { //settings:ok 05 §3 recomputes the last 3 days
				x := d.Add(-i)
				res, err := svc.ComputeDay(ctx, x)
				if err != nil {
					return platformjobs.Result{}, err
				}
				n, err := svc.ComputeDemand(ctx, x)
				if err != nil {
					return platformjobs.Result{}, err
				}
				b, err := svc.ComputeBanners(ctx, x)
				if err != nil {
					return platformjobs.Result{}, err
				}
				stores, demand, banners = stores+res.Stores, demand+n, banners+b
			}
			return platformjobs.Result{Summary: map[string]int64{"stores": stores, "demand": demand, "banners": banners}}, nil
		},
	})
	define(platformjobs.Kind{
		Name: KindRollupDemand, Schedule: "5 * * * *", UniqueBy: platformjobs.PerHour,
		Description: "Recomputes today's and yesterday's search demand by H3 cell, query and product",
		Handler: func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			d := day(run)
			var rows int64
			for _, x := range []rollups.Day{d, d.Add(-1)} {
				n, err := svc.ComputeDemand(ctx, x)
				if err != nil {
					return platformjobs.Result{}, err
				}
				rows += n
			}
			return platformjobs.Result{Summary: map[string]int64{"rows": rows}}, nil
		},
	})
	define(platformjobs.Kind{
		Name: KindRollupBanners, Schedule: "*/5 * * * *", UniqueBy: platformjobs.PerTick,
		Description: "Recomputes today's banner impressions, taps and attributed orders (banner_daily)",
		Handler: func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			n, err := svc.ComputeBanners(ctx, day(run))
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"banners": n}}, nil
		},
	})
	define(platformjobs.Kind{
		Name: KindRollupCohorts, Schedule: "15 1 * * *", UniqueBy: platformjobs.PerDay,
		Description: "Recomputes the weekly customer and store cohorts (W0–W12) up to yesterday",
		Handler: func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			n, err := svc.ComputeCohorts(ctx, day(run).Add(-1))
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"rows": n}}, nil
		},
	})
	define(platformjobs.Kind{
		Name: KindRollupItemPairs, Schedule: "0 2 * * *", UniqueBy: platformjobs.PerDay,
		Description: "Rebuilds each store's frequently-bought-together pairs over the insight window (W3-03 reads them)",
		Handler: func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			n, err := svc.ComputeItemPairs(ctx, day(run))
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"pairs": n}}, nil
		},
	})
}

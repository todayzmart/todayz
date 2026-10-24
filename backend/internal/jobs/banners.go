package jobs

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/banners"
	"github.com/todayz/backend/internal/platform/db"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// KindBannersTidy moves banners between scheduled, live and ended by their
// dates (maintenance class: customers see a banner by its dates, so a late run
// changes a label only).
const KindBannersTidy = "banners_tidy"

// registerbanners declares the banners domain's job kinds.
func registerbanners(r *Registry, d Deps) {
	store := db.New(d.Pool)
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindBannersTidy, Owner: "W2-07", Class: platformjobs.ClassMaintenance,
		Description: "Starts scheduled banners and ends banners past their end",
		Schedule:    "* * * * *", UniqueBy: platformjobs.PerTick, Timeout: 50 * time.Second, MaxAttempts: 1,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			res, err := banners.Tidy(ctx, store, d.Clock)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"started": res.Started, "ended": res.Ended}}, nil
		},
	})
}

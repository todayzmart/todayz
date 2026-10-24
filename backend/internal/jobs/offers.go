package jobs

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/offers"
	"github.com/todayz/backend/internal/platform/db"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// KindOffersTidy moves offers between scheduled, live and ended by their dates
// and redemption caps (maintenance class: pricing reads the dates itself, so a
// late run changes a label, never a price).
const KindOffersTidy = "offers_tidy"

// registeroffers declares the offers domain's job kinds.
func registeroffers(r *Registry, d Deps) {
	store := db.New(d.Pool)
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindOffersTidy, Owner: "W2-07", Class: platformjobs.ClassMaintenance,
		Description: "Starts scheduled offers and ends offers past their end or with every redemption used",
		Schedule:    "* * * * *", UniqueBy: platformjobs.PerTick, Timeout: 50 * time.Second, MaxAttempts: 1,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			res, err := offers.Tidy(ctx, store, d.Clock)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{
				"started": res.Started, "ended": res.Ended, "spent": res.Spent,
			}}, nil
		},
	})
}

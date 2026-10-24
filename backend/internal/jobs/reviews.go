package jobs

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/platform/db"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/reviews"
)

// KindStoreRating recomputes every store's rating from its visible reviews
// (rollup class). Every review write already recomputes its store inside the
// transaction; this heals drift and is the only other writer of
// stores.rating_avg and rating_count (D-59).
const KindStoreRating = "store_rating"

// registerreviews declares the reviews domain's job kinds.
func registerreviews(r *Registry, d Deps) {
	svc, err := reviews.New(reviews.Deps{DB: db.New(d.Pool), Clock: d.Clock, Settings: d.Settings})
	if err != nil {
		panic("reviews: " + err.Error())
	}
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindStoreRating, Owner: "W2-06", Class: platformjobs.ClassRollup,
		Description: "Recomputes each store's rating and review count from its visible reviews (Bayesian average, prior 4.0, weight 5)",
		Schedule:    "0 * * * *", UniqueBy: platformjobs.PerTick, Timeout: 60 * time.Second, MaxAttempts: 3,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			n, err := svc.RecomputeAll(ctx)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"stores": n}}, nil
		},
	})
}

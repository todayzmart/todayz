package jobs

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/notify"
	"github.com/todayz/backend/internal/platform/db"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/referrals"
)

// KindReferralSweep posts referral rewards that are due but not yet posted
// (a ledger that was down, a shop with no live store yet) and qualifies new
// shops whose verification came after their last counted sale (W2-03).
const KindReferralSweep = "referral_sweep"

// registerreferrals declares the referrals domain's job kinds.
func registerreferrals(r *Registry, d Deps) {
	// The "referral unlocked" notices enqueue sends, so the enqueuer is
	// built on the first run (as the orders timers do).
	enqueuer := lazyEnqueuer(r, d, "referral sweep")
	svc := func() (*referrals.Service, error) {
		enq, err := enqueuer()
		if err != nil {
			return nil, err
		}
		nd := notify.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log, Jobs: enq}
		// TODO(merge): set Ledger to the W2-01 credits.Poster adapter (as in
		// cmd/todayz/routes_referrals.go); until then rewards stay unposted.
		rd := referrals.Deps{Clock: d.Clock, Settings: d.Settings}
		if d.Pool != nil {
			pool := db.New(d.Pool)
			nd.DB, nd.Staff, rd.DB = pool, business.NewStaffResolver(pool), pool
		}
		ns, err := notify.New(nd)
		if err != nil {
			return nil, err
		}
		rd.Notices = ns
		return referrals.New(rd)
	}
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindReferralSweep, Owner: "W2-03", Class: platformjobs.ClassMaintenance,
		Description: "Posts due referral rewards that have not posted yet and qualifies new shops that reached their count",
		Schedule:    "*/10 * * * *", UniqueBy: platformjobs.PerTick, Timeout: 60 * time.Second, MaxAttempts: 3,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			s, err := svc()
			if err != nil {
				return platformjobs.Result{}, err
			}
			out, err := s.Sweep(ctx)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: out}, nil
		},
	})
}

package jobs

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/db"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/risk"
)

// KindRiskScan runs the rules internal/risk owns (code lockouts, bill-claim
// spread, reversal ratio, self-like referrals, OTP abuse) and raises a flag on
// risk_flags for each new finding (D-78, D-225). It never blocks a person; a
// flag on a business or store holds that business's payout until someone
// clears it on A-13 (01 §9).
const KindRiskScan = "risk_scan"

// registerrisk declares the risk domain's job kinds.
func registerrisk(r *Registry, d Deps) {
	svc, err := risk.New(risk.Deps{
		DB: db.New(d.Pool), Clock: d.Clock, Settings: d.Settings,
		OTPMaxAttempts: identity.OTPMaxAttempts,
	})
	if err != nil {
		panic("risk: " + err.Error())
	}
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindRiskScan, Owner: "W3-02", Class: platformjobs.ClassRollup,
		Description: "Runs the risk rules and raises a deduplicated risk flag for each new finding (D-78, D-225)",
		Schedule:    "*/5 * * * *", UniqueBy: platformjobs.PerTick, Timeout: time.Minute, MaxAttempts: 3,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			// The summary goes back even on an error, so job_runs keeps the per-rule
			// counts of a run where only some rules failed.
			sum, err := svc.Scan(ctx)
			return platformjobs.Result{Summary: sum}, err
		},
	})
}

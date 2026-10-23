package jobs

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/db"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// KindIdentityPurge deletes expired OTP requests and old sessions
// (maintenance class, D-95, D-105).
const KindIdentityPurge = "identity_purge"

// registeridentity declares the identity domain's job kinds.
func registeridentity(r *Registry, d Deps) {
	store := db.New(d.Pool)
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindIdentityPurge, Owner: "W1-01", Class: platformjobs.ClassMaintenance,
		Description: "Deletes OTP requests 24 h after they expire and sessions 30 days after they were revoked or expired",
		Schedule:    "0 * * * *", UniqueBy: platformjobs.PerTick, Timeout: 60 * time.Second, MaxAttempts: 3,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			res, err := identity.Purge(ctx, store, d.Clock.Now())
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{
				"otp_requests": res.OTPRequests, "sessions": res.Sessions,
			}}, nil
		},
	})
}

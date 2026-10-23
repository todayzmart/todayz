package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
)

// IdentityPurgeArgs deletes expired OTP requests and old sessions
// (maintenance class, D-95).
type IdentityPurgeArgs struct{}

// Kind implements river.JobArgs.
func (IdentityPurgeArgs) Kind() string { return "identity.purge" }

// IdentityPurgeWorker runs identity.Purge.
type IdentityPurgeWorker struct {
	river.WorkerDefaults[IdentityPurgeArgs]
	DB    *db.DB
	Clock clock.Clock
	Log   *slog.Logger
}

// Work implements river.Worker.
func (w *IdentityPurgeWorker) Work(ctx context.Context, _ *river.Job[IdentityPurgeArgs]) error {
	res, err := identity.Purge(ctx, w.DB, w.Clock.Now())
	if err != nil {
		return err
	}
	if w.Log != nil {
		w.Log.DebugContext(ctx, "identity purged", slog.Int64("otp_requests", res.OTPRequests), slog.Int64("sessions", res.Sessions))
	}
	return nil
}

// registeridentity adds the identity domain's River workers and periodic jobs.
func registeridentity(r *Registry, d Deps) {
	Add[IdentityPurgeArgs](r, &IdentityPurgeWorker{DB: db.New(d.Pool), Clock: d.Clock, Log: d.Log})
	r.Every(time.Hour, func() river.JobArgs { return IdentityPurgeArgs{} })
}

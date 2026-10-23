// Package adminapi serves the admin-jobs API (04 §2.7, D-95, A-21): the
// job kinds with their health, run history and run detail, and the pause,
// resume, run-now and retry actions. Who may call it is the Gate's answer
// (W1-01's authz.AdminGate); what may be done to a kind is decided here.
package adminapi

import (
	"context"
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	adminjobsapi "github.com/todayz/backend/gen/api/admin-jobs"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// RunEnqueuer enqueues a manual or retry run (platformjobs.RiverEnqueuer).
type RunEnqueuer interface {
	EnqueueRunTx(ctx context.Context, tx pgx.Tx, kind string, o platformjobs.RunOpts) (ids.ID, string, error)
}

// Deps are what the API needs. Registry is built with the same Register
// call as the worker's (internal/jobs.Register), so A-21 lists what the
// worker runs.
type Deps struct {
	Registry *platformjobs.Registry
	DB       *db.DB      // nil: every operation answers 503 (route tests mount without a database)
	Enqueuer RunEnqueuer // nil: run-now and retry answer 503
	Audit    *audit.Writer
	Clock    clock.Clock
	Gate     Gate // W1-01's admin gate (routes_admin.go); nil is DenyGate
	Log      *slog.Logger
}

// Service implements adminjobsapi.StrictServerInterface.
type Service struct {
	d Deps
}

var _ adminjobsapi.StrictServerInterface = (*Service)(nil)

// New returns the service. A nil Gate is DenyGate.
func New(d Deps) *Service {
	if d.Gate == nil {
		d.Gate = DenyGate{}
	}
	if d.Clock == nil {
		d.Clock = clock.Real{}
	}
	if d.Registry == nil {
		d.Registry = platformjobs.NewRegistry()
	}
	d.Log = telemetry.OrDefault(d.Log)
	return &Service{d: d}
}

// Register mounts the 8 admin-jobs operations on r through the generated
// Register (problem+json errors, D-100).
func Register(r chi.Router, d Deps) {
	adminjobsapi.Register(r, New(d))
}

// unavailable is the answer without a database (or queue).
func unavailable() error {
	return problem.New(genproblems.ServiceUnavailable, "the jobs console needs the database")
}

func (s *Service) log(ctx context.Context) *slog.Logger {
	return telemetry.LoggerOr(ctx, s.d.Log)
}

// internal wraps an unexpected error as 500; the cause is logged, never sent.
func internal(err error) error {
	return problem.Wrap(genproblems.Internal, err)
}

package adminapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	adminjobsapi "github.com/todayz/backend/gen/api/admin-jobs"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// Audit actions and subjects (audit_log, D-95: every pause, resume, run-now
// and retry is audited).
const (
	actionPause  = "jobs.pause"
	actionResume = "jobs.resume"
	actionRun    = "jobs.run"
	actionRetry  = "jobs.retry"
	subjectKind  = "job_kind"
	subjectRun   = "job_run"
)

// minReason is the contract's minLength for a reason, applied again after
// trimming so "   " is not a reason.
const minReason = 3

// actor checks who may act on kind name (D-95): admin or super_admin for
// every kind, then super_admin with step-up for a money kind. It returns
// 404 for an unknown kind and 409 job_not_runnable for a timer kind.
func (s *Service) actor(ctx context.Context, name string) (Admin, platformjobs.Kind, error) {
	a, err := s.d.Gate.Require(ctx, actRoles, false)
	if err != nil {
		return Admin{}, platformjobs.Kind{}, err
	}
	k, ok := s.d.Registry.Lookup(name)
	if !ok {
		return Admin{}, platformjobs.Kind{}, problem.New(genproblems.NotFound, "no such job kind")
	}
	return s.actorFor(ctx, a, k)
}

func (s *Service) actorFor(ctx context.Context, a Admin, k platformjobs.Kind) (Admin, platformjobs.Kind, error) {
	if k.Class == platformjobs.ClassMoney {
		var err error
		if a, err = s.d.Gate.Require(ctx, moneyRoles, true); err != nil {
			return Admin{}, platformjobs.Kind{}, err
		}
	}
	if k.Class == platformjobs.ClassTimer {
		return Admin{}, platformjobs.Kind{}, problem.New(genproblems.JobNotRunnable, "timer jobs are never paused, run or retried by hand")
	}
	return a, k, nil
}

// reasonOf validates the request's reason.
func reasonOf(body *string) (string, error) {
	if body == nil {
		return "", problem.New(genproblems.ValidationFailed, "a reason is required").WithField("body.reason", "required")
	}
	r := strings.TrimSpace(*body)
	if len([]rune(r)) < minReason {
		return "", problem.New(genproblems.ValidationFailed, "a reason is required").WithField("body.reason", "too short")
	}
	return r, nil
}

// lockKind serialises admin actions on one kind for the transaction.
func lockKind(ctx context.Context, tx pgx.Tx, kind string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "tz_jobs_admin:"+kind)
	return err
}

func isPaused(ctx context.Context, tx pgx.Tx, kind string) (bool, error) {
	var p bool
	err := tx.QueryRow(ctx, `SELECT paused FROM job_controls WHERE kind = $1`, kind).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return p, err
}

// busySQL: a kind is busy while River holds a job of it that has not
// finished (queued, scheduled, retrying, running) or a run row is running.
const busySQL = `SELECT EXISTS (SELECT 1 FROM river_job WHERE kind = $1
    AND state IN ('available', 'pending', 'retryable', 'running', 'scheduled'))
  OR EXISTS (SELECT 1 FROM job_runs WHERE kind = $1 AND status = 'running')`

// enqueueChecked runs the shared checks of run-now and retry inside tx and
// enqueues the run.
func (s *Service) enqueueChecked(ctx context.Context, k platformjobs.Kind, o platformjobs.RunOpts) (ids.ID, string, error) {
	tx, ok := db.TxFrom(ctx)
	if !ok {
		return ids.ID{}, "", errors.New("adminapi: no transaction")
	}
	if err := lockKind(ctx, tx, k.Name); err != nil {
		return ids.ID{}, "", internal(fmt.Errorf("adminapi: lock: %w", err))
	}
	paused, err := isPaused(ctx, tx, k.Name)
	if err != nil {
		return ids.ID{}, "", internal(fmt.Errorf("adminapi: job_controls: %w", err))
	}
	if paused {
		return ids.ID{}, "", problem.New(genproblems.JobPaused, "resume the job first")
	}
	var busy bool
	if err := tx.QueryRow(ctx, busySQL, k.Name).Scan(&busy); err != nil {
		return ids.ID{}, "", internal(fmt.Errorf("adminapi: busy: %w", err))
	}
	if busy {
		return ids.ID{}, "", problem.New(genproblems.JobAlreadyRunning, "a run of this job is running or queued")
	}
	id, period, err := s.d.Enqueuer.EnqueueRunTx(ctx, tx, k.Name, o)
	switch {
	case errors.Is(err, platformjobs.ErrBadPeriod):
		return ids.ID{}, "", problem.New(genproblems.ValidationFailed, "the period does not match the job's period").
			WithField("body.periodKey", "not a period of this job")
	case errors.Is(err, platformjobs.ErrNotScheduled):
		return ids.ID{}, "", problem.New(genproblems.JobNotRunnable, "only scheduled jobs run by hand")
	case err != nil:
		return ids.ID{}, "", internal(err)
	}
	return id, period, nil
}

// RunAdminJob implements POST /v1/admin/jobs/{kind}/run.
func (s *Service) RunAdminJob(ctx context.Context, req adminjobsapi.RunAdminJobRequestObject) (adminjobsapi.RunAdminJobResponseObject, error) {
	a, k, err := s.actor(ctx, req.Kind)
	if err != nil {
		return nil, err
	}
	if !k.Scheduled() {
		return nil, problem.New(genproblems.JobNotRunnable, "event jobs run from their events, not by hand")
	}
	var reasonIn, periodIn *string
	if req.Body != nil {
		reasonIn, periodIn = &req.Body.Reason, req.Body.PeriodKey
	}
	reason, err := reasonOf(reasonIn)
	if err != nil {
		return nil, err
	}
	if s.d.DB == nil || s.d.Enqueuer == nil || s.d.Audit == nil {
		return nil, unavailable()
	}
	now := s.d.Clock.Now()
	period := ""
	if periodIn != nil {
		period = *periodIn
		start, perr := k.UniqueBy.Start(period)
		if perr != nil {
			return nil, problem.New(genproblems.ValidationFailed, "the period does not match the job's period").
				WithField("body.periodKey", "not a period of this job")
		}
		if start.After(now) {
			return nil, problem.New(genproblems.ValidationFailed, "a future period cannot run").
				WithField("body.periodKey", "in the future")
		}
		// A tick key must name one of the kind's ticks, not any minute.
		if sched, ok := s.d.Registry.ScheduleOf(k.Name); ok && k.UniqueBy == platformjobs.PerTick &&
			!sched.Next(start.Add(-time.Second)).Equal(start) {
			return nil, problem.New(genproblems.ValidationFailed, "the period is not one of the job's ticks").
				WithField("body.periodKey", "not a tick of this job")
		}
	}
	var runID ids.ID
	err = s.d.DB.InTx(ctx, func(ctx context.Context) error {
		var err error
		runID, period, err = s.enqueueChecked(ctx, k, platformjobs.RunOpts{Trigger: platformjobs.TriggerManual, PeriodKey: period, By: a.ID})
		if err != nil {
			return err
		}
		_, err = s.d.Audit.Write(ctx, audit.Entry{ActorKind: audit.ActorAdmin, ActorID: a.ID, Action: actionRun,
			SubjectType: subjectKind, SubjectID: k.Name,
			After: map[string]any{"runId": runID.String(), "periodKey": period, "trigger": platformjobs.TriggerManual, "reason": reason}})
		return err
	})
	if err != nil {
		return nil, asProblem(err)
	}
	s.log(ctx).InfoContext(ctx, "job run requested", slog.String("kind", k.Name), slog.String("run_id", runID.String()))
	return adminjobsapi.RunAdminJob202JSONResponse{RunId: runID.String(), Kind: k.Name,
		Trigger: adminjobsapi.JobTrigger(platformjobs.TriggerManual), PeriodKey: period, ServerNow: s.d.Clock.Now().UTC()}, nil
}

// retryable are the statuses a retry may start from (04 §2.7).
var retryable = map[adminjobsapi.JobRunStatus]bool{
	adminjobsapi.JobRunStatusFailed: true, adminjobsapi.JobRunStatusDiscarded: true, adminjobsapi.JobRunStatusMissed: true,
}

// RetryAdminJobRun implements POST /v1/admin/job-runs/{runId}/retry.
func (s *Service) RetryAdminJobRun(ctx context.Context, req adminjobsapi.RetryAdminJobRunRequestObject) (adminjobsapi.RetryAdminJobRunResponseObject, error) {
	a, err := s.d.Gate.Require(ctx, actRoles, false)
	if err != nil {
		return nil, err
	}
	if s.d.DB == nil || s.d.Enqueuer == nil || s.d.Audit == nil {
		return nil, unavailable()
	}
	run, _, _, _, err := s.loadRun(ctx, req.RunId)
	if err != nil {
		return nil, err
	}
	k, ok := s.d.Registry.Lookup(run.Kind)
	if !ok {
		return nil, problem.New(genproblems.JobNotRunnable, "this job kind is no longer registered")
	}
	if a, k, err = s.actorFor(ctx, a, k); err != nil {
		return nil, err
	}
	if !k.Scheduled() {
		// TODO(lead): an event kind's args are not kept in job_runs, so a
		// failed event run cannot be replayed from A-21; River's own retries
		// cover it. Refused until the lead decides otherwise.
		return nil, problem.New(genproblems.JobNotRunnable, "event job runs are retried by River, not by hand")
	}
	if !retryable[run.Status] {
		return nil, problem.New(genproblems.InvalidTransition, "only a failed, discarded or missed run can be retried")
	}
	var reasonIn *string
	if req.Body != nil {
		reasonIn = &req.Body.Reason
	}
	reason, err := reasonOf(reasonIn)
	if err != nil {
		return nil, err
	}
	period := ""
	if run.PeriodKey != nil {
		period = *run.PeriodKey
	}
	var runID ids.ID
	err = s.d.DB.InTx(ctx, func(ctx context.Context) error {
		var err error
		runID, period, err = s.enqueueChecked(ctx, k, platformjobs.RunOpts{Trigger: platformjobs.TriggerRetry, PeriodKey: period, By: a.ID})
		if err != nil {
			return err
		}
		_, err = s.d.Audit.Write(ctx, audit.Entry{ActorKind: audit.ActorAdmin, ActorID: a.ID, Action: actionRetry,
			SubjectType: subjectRun, SubjectID: run.Id,
			Before: map[string]any{"kind": k.Name, "status": run.Status, "periodKey": period},
			After:  map[string]any{"runId": runID.String(), "trigger": platformjobs.TriggerRetry, "reason": reason}})
		return err
	})
	if err != nil {
		return nil, asProblem(err)
	}
	s.log(ctx).InfoContext(ctx, "job retry requested", slog.String("kind", k.Name), slog.String("run_id", runID.String()))
	retryOf := run.Id
	return adminjobsapi.RetryAdminJobRun202JSONResponse{RunId: runID.String(), Kind: k.Name, RetryOf: &retryOf,
		Trigger: adminjobsapi.JobTrigger(platformjobs.TriggerRetry), PeriodKey: period, ServerNow: s.d.Clock.Now().UTC()}, nil
}

// PauseAdminJob implements POST /v1/admin/jobs/{kind}/pause.
func (s *Service) PauseAdminJob(ctx context.Context, req adminjobsapi.PauseAdminJobRequestObject) (adminjobsapi.PauseAdminJobResponseObject, error) {
	var reason *string
	if req.Body != nil {
		reason = &req.Body.Reason
	}
	st, err := s.setPaused(ctx, req.Kind, true, reason)
	if err != nil {
		return nil, err
	}
	return adminjobsapi.PauseAdminJob200JSONResponse(st), nil
}

// ResumeAdminJob implements POST /v1/admin/jobs/{kind}/resume.
func (s *Service) ResumeAdminJob(ctx context.Context, req adminjobsapi.ResumeAdminJobRequestObject) (adminjobsapi.ResumeAdminJobResponseObject, error) {
	var reason *string
	if req.Body != nil {
		reason = &req.Body.Reason
	}
	st, err := s.setPaused(ctx, req.Kind, false, reason)
	if err != nil {
		return nil, err
	}
	return adminjobsapi.ResumeAdminJob200JSONResponse(st), nil
}

// setPaused pauses or resumes a kind: 409 invalid_transition when it is
// already in that state.
func (s *Service) setPaused(ctx context.Context, name string, pause bool, reasonIn *string) (adminjobsapi.JobControlState, error) {
	a, k, err := s.actor(ctx, name)
	if err != nil {
		return adminjobsapi.JobControlState{}, err
	}
	reason, err := reasonOf(reasonIn)
	if err != nil {
		return adminjobsapi.JobControlState{}, err
	}
	if s.d.DB == nil || s.d.Audit == nil {
		return adminjobsapi.JobControlState{}, unavailable()
	}
	action := actionResume
	if pause {
		action = actionPause
	}
	var c adminjobsapi.JobControl
	err = s.d.DB.InTx(ctx, func(ctx context.Context) error {
		tx, _ := db.TxFrom(ctx)
		if err := lockKind(ctx, tx, k.Name); err != nil {
			return fmt.Errorf("adminapi: lock: %w", err)
		}
		was, err := isPaused(ctx, tx, k.Name)
		if err != nil {
			return fmt.Errorf("adminapi: job_controls: %w", err)
		}
		if was == pause {
			if pause {
				return problem.New(genproblems.InvalidTransition, "the job is already paused")
			}
			return problem.New(genproblems.InvalidTransition, "the job is not paused")
		}
		var (
			paused bool
			r      *string
			by     pgtype.UUID
			at     *time.Time
		)
		// paused_at / resumed_at keep the latest pause window, which the
		// watchdog reads so a tick due while paused is not reported missed.
		if err := tx.QueryRow(ctx, `INSERT INTO job_controls (kind, paused, reason, updated_by, updated_at, paused_at)
			VALUES ($1, $2::boolean, $3, $4, $5::timestamptz, CASE WHEN $2::boolean THEN $5::timestamptz END)
			ON CONFLICT (kind) DO UPDATE SET paused = EXCLUDED.paused, reason = EXCLUDED.reason,
			  updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at,
			  paused_at = CASE WHEN EXCLUDED.paused THEN EXCLUDED.updated_at ELSE job_controls.paused_at END,
			  resumed_at = CASE WHEN EXCLUDED.paused THEN NULL ELSE EXCLUDED.updated_at END
			RETURNING paused, reason, updated_by, updated_at`,
			k.Name, pause, reason, ids.UUID(a.ID), s.d.Clock.Now()).Scan(&paused, &r, &by, &at); err != nil {
			return fmt.Errorf("adminapi: job_controls: %w", err)
		}
		c = control(paused, r, by, at)
		_, err = s.d.Audit.Write(ctx, audit.Entry{ActorKind: audit.ActorAdmin, ActorID: a.ID, Action: action,
			SubjectType: subjectKind, SubjectID: k.Name,
			Before: map[string]any{"paused": was}, After: map[string]any{"paused": pause, "reason": reason}})
		return err
	})
	if err != nil {
		return adminjobsapi.JobControlState{}, asProblem(err)
	}
	s.log(ctx).InfoContext(ctx, "job control changed", slog.String("kind", k.Name), slog.Bool("paused", pause))
	return adminjobsapi.JobControlState{Kind: k.Name, Control: c, ServerNow: s.d.Clock.Now().UTC()}, nil
}

// asProblem passes a *problem.Error through and wraps anything else as 500.
func asProblem(err error) error {
	var pe *problem.Error
	if errors.As(err, &pe) {
		return pe
	}
	return internal(err)
}

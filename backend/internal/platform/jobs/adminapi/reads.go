package adminapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	adminjobsapi "github.com/todayz/backend/gen/api/admin-jobs"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// statsWindow is A-21's "7-day" window for success rate and durations
// (04 §2.7; a fixed product definition, not a setting).
const statsWindow = 7 * 24 * time.Hour

// Run page sizes (common Limit parameter: default 20, at most 100).
const (
	defaultLimit = 20
	maxLimit     = 100
)

// kindState is what the database knows about a kind.
type kindState struct {
	last         *adminjobsapi.JobRunBrief
	lastOK       *time.Time
	lastMissed   *time.Time
	lastFinished *string
	control      adminjobsapi.JobControl
	stats        stats7d
}

type stats7d struct {
	succeeded, failed, cancelled, skipped, discarded, yielded, sumMS, maxMS int64
}

// kindStateSQL reads, per kind: the latest run (missed rows are not runs),
// the latest success, the latest missed row, the latest finished status,
// the pause state and the 7-day job_stats_hourly totals.
const kindStateSQL = `
SELECT k.kind,
  lr.id, lr.status, lr.trigger, lr.period_key, lr.created_at, lr.finished_at,
  (SELECT max(r.created_at) FROM job_runs r WHERE r.kind = k.kind AND r.status = 'succeeded'),
  (SELECT max(r.created_at) FROM job_runs r WHERE r.kind = k.kind AND r.status = 'missed'),
  (SELECT r.status FROM job_runs r WHERE r.kind = k.kind AND r.status IN ('succeeded', 'failed', 'discarded', 'cancelled')
     ORDER BY r.created_at DESC, r.id DESC LIMIT 1),
  coalesce(c.paused, false), c.reason, c.updated_by, c.updated_at,
  st.succeeded, st.failed, st.cancelled, st.skipped, st.discarded, st.yielded, st.sum_ms, st.max_ms
FROM unnest($1::text[]) AS k(kind)
LEFT JOIN LATERAL (
  SELECT r.id, r.status, r.trigger, r.period_key, r.created_at, r.finished_at FROM job_runs r
  WHERE r.kind = k.kind AND r.status <> 'missed' ORDER BY r.created_at DESC, r.id DESC LIMIT 1) lr ON true
LEFT JOIN job_controls c ON c.kind = k.kind
CROSS JOIN LATERAL (
  SELECT coalesce(sum(s.succeeded), 0)::bigint AS succeeded, coalesce(sum(s.failed), 0)::bigint AS failed,
    coalesce(sum(s.cancelled), 0)::bigint AS cancelled, coalesce(sum(s.skipped), 0)::bigint AS skipped,
    coalesce(sum(s.discarded), 0)::bigint AS discarded, coalesce(sum(s.yielded), 0)::bigint AS yielded,
    coalesce(sum(s.sum_ms), 0)::bigint AS sum_ms, coalesce(max(s.max_ms), 0)::bigint AS max_ms
  FROM job_stats_hourly s WHERE s.kind = k.kind AND s.hour >= $2) st`

func (s *Service) kindStates(ctx context.Context, names []string, now time.Time) (map[string]kindState, error) {
	rows, err := s.d.DB.Q(ctx).Query(ctx, kindStateSQL, names, now.Add(-statsWindow))
	if err != nil {
		return nil, fmt.Errorf("adminapi: kind state: %w", err)
	}
	defer rows.Close()
	out := make(map[string]kindState, len(names))
	for rows.Next() {
		var (
			kind                       string
			lrID                       pgtype.UUID
			lrStatus, lrTrig, lrPeriod *string
			lrCreated, lrFinished      *time.Time
			st                         kindState
			paused                     bool
			reason                     *string
			by                         pgtype.UUID
			updatedAt                  *time.Time
		)
		if err := rows.Scan(&kind, &lrID, &lrStatus, &lrTrig, &lrPeriod, &lrCreated, &lrFinished,
			&st.lastOK, &st.lastMissed, &st.lastFinished, &paused, &reason, &by, &updatedAt,
			&st.stats.succeeded, &st.stats.failed, &st.stats.cancelled, &st.stats.skipped,
			&st.stats.discarded, &st.stats.yielded, &st.stats.sumMS, &st.stats.maxMS); err != nil {
			return nil, fmt.Errorf("adminapi: kind state: %w", err)
		}
		if id, ok := ids.FromUUID(lrID); ok && lrStatus != nil && lrTrig != nil && lrCreated != nil {
			st.last = &adminjobsapi.JobRunBrief{Id: id.String(), Status: adminjobsapi.JobRunStatus(*lrStatus),
				Trigger: adminjobsapi.JobTrigger(*lrTrig), PeriodKey: lrPeriod, CreatedAt: lrCreated.UTC(), FinishedAt: utcPtr(lrFinished)}
		}
		st.control = control(paused, reason, by, updatedAt)
		out[kind] = st
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("adminapi: kind state: %w", err)
	}
	return out, nil
}

func control(paused bool, reason *string, by pgtype.UUID, at *time.Time) adminjobsapi.JobControl {
	c := adminjobsapi.JobControl{Paused: paused, Reason: reason, UpdatedAt: utcPtr(at)}
	if id, ok := ids.FromUUID(by); ok {
		v := id.String()
		c.UpdatedBy = &v
	}
	return c
}

// health is the list badge (schemas JobHealth).
func health(st kindState) adminjobsapi.JobHealth {
	switch {
	case st.control.Paused:
		return adminjobsapi.JobHealthPaused
	case st.lastMissed != nil && (st.lastOK == nil || st.lastMissed.After(*st.lastOK)):
		return adminjobsapi.JobHealthMissed
	case st.lastFinished != nil && (*st.lastFinished == string(platformjobs.StatusFailed) || *st.lastFinished == string(platformjobs.StatusDiscarded)):
		return adminjobsapi.JobHealthFailing
	}
	return adminjobsapi.JobHealthOk
}

func (s *Service) summary(k platformjobs.Kind, st kindState, now time.Time) adminjobsapi.JobKindSummary {
	out := adminjobsapi.JobKindSummary{
		Kind: k.Name, Class: adminjobsapi.JobClass(k.Class), Owner: k.Owner, Description: k.Description,
		Scheduled: k.Scheduled(), LastRun: st.last, Control: st.control, Health: health(st),
		Runnable: k.Scheduled() && k.Class != platformjobs.ClassTimer,
		Pausable: k.Class != platformjobs.ClassTimer,
	}
	if k.Scheduled() {
		expr := k.Schedule
		text := scheduleText(expr)
		out.Schedule, out.ScheduleText = &expr, &text
		if sched, ok := s.d.Registry.ScheduleOf(k.Name); ok && !st.control.Paused {
			next := sched.Next(now).UTC()
			out.NextRunAt = &next
		}
	}
	x := st.stats
	runs := x.succeeded + x.failed + x.cancelled + x.skipped + x.discarded + x.yielded
	runs7d := int(runs)
	out.Runs7d = &runs7d
	if done := x.succeeded + x.failed + x.cancelled + x.discarded; done > 0 {
		bp := int(x.succeeded * 10000 / done)
		out.SuccessRate7dBp = &bp
	}
	if runs > 0 {
		avg, maxMS := int(x.sumMS/runs), int(x.maxMS)
		out.AvgDurationMs7d, out.MaxDurationMs7d = &avg, &maxMS
	}
	return out
}

// reader lets through an admin who may read A-21; mayRead then narrows a
// finance-only admin to money kinds.
func (s *Service) reader(ctx context.Context) (Admin, error) {
	return s.d.Gate.Require(ctx, readRoles, false)
}

func mayRead(a Admin, c platformjobs.Class) bool {
	return !financeOnly(a) || c == platformjobs.ClassMoney
}

// kindFor looks a kind up for a reader: 404 when unknown, 403 when the
// reader may not see it.
func (s *Service) kindFor(a Admin, name string) (platformjobs.Kind, error) {
	k, ok := s.d.Registry.Lookup(name)
	if !ok {
		return platformjobs.Kind{}, problem.New(genproblems.NotFound, "no such job kind")
	}
	if !mayRead(a, k.Class) {
		return platformjobs.Kind{}, problem.New(genproblems.Forbidden, "finance reads money jobs only")
	}
	return k, nil
}

// ListAdminJobs implements GET /v1/admin/jobs.
func (s *Service) ListAdminJobs(ctx context.Context, req adminjobsapi.ListAdminJobsRequestObject) (adminjobsapi.ListAdminJobsResponseObject, error) {
	a, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if s.d.DB == nil {
		return nil, unavailable()
	}
	now := s.d.Clock.Now()
	var kinds []platformjobs.Kind
	var names []string
	for _, k := range s.d.Registry.Kinds() {
		if !mayRead(a, k.Class) || (req.Params.Class != nil && string(*req.Params.Class) != string(k.Class)) {
			continue
		}
		kinds = append(kinds, k)
		names = append(names, k.Name)
	}
	states, err := s.kindStates(ctx, names, now)
	if err != nil {
		return nil, internal(err)
	}
	items := make([]adminjobsapi.JobKindSummary, 0, len(kinds))
	for _, k := range kinds {
		sum := s.summary(k, states[k.Name], now)
		if req.Params.Status != nil && sum.Health != *req.Params.Status {
			continue
		}
		items = append(items, sum)
	}
	return adminjobsapi.ListAdminJobs200JSONResponse{Items: items, ServerNow: now.UTC()}, nil
}

// GetAdminJob implements GET /v1/admin/jobs/{kind}.
func (s *Service) GetAdminJob(ctx context.Context, req adminjobsapi.GetAdminJobRequestObject) (adminjobsapi.GetAdminJobResponseObject, error) {
	a, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	k, err := s.kindFor(a, req.Kind)
	if err != nil {
		return nil, err
	}
	if s.d.DB == nil {
		return nil, unavailable()
	}
	now := s.d.Clock.Now()
	states, err := s.kindStates(ctx, []string{k.Name}, now)
	if err != nil {
		return nil, internal(err)
	}
	return adminjobsapi.GetAdminJob200JSONResponse{
		Job: s.summary(k, states[k.Name], now), Settings: settingsOf(k), ServerNow: now.UTC(),
	}, nil
}

func settingsOf(k platformjobs.Kind) adminjobsapi.JobKindSettings {
	q := k.Queue
	if q == "" {
		q = platformjobs.DefaultQueue
	}
	secs := int((k.Timeout + time.Second - 1) / time.Second)
	out := adminjobsapi.JobKindSettings{
		TimeoutSeconds: max(secs, 1), MaxAttempts: k.MaxAttempts, Queue: q, Concurrency: k.Concurrency,
		Batched: k.Batched, UniqueBy: adminjobsapi.JobKindSettingsUniqueBy(k.UniqueBy.String()),
	}
	if k.Runbook != "" {
		rb := k.Runbook
		out.Runbook = &rb
	}
	return out
}

// runColumns are the job_runs columns scanRun reads.
const runColumns = `id, kind, class, trigger, period_key, attempt, status, scheduled_at, started_at, finished_at,
  duration_ms, lateness_ms, summary, trace_id, triggered_by, created_at`

func scanRun(row pgx.Row, extra ...any) (adminjobsapi.JobRun, error) {
	var (
		id, by                   pgtype.UUID
		r                        adminjobsapi.JobRun
		class, trig, status      string
		sched, started, finished *time.Time
		dur, late                *int64
		summary                  []byte
	)
	dst := append([]any{&id, &r.Kind, &class, &trig, &r.PeriodKey, &r.Attempt, &status, &sched, &started, &finished,
		&dur, &late, &summary, &r.TraceId, &by, &r.CreatedAt}, extra...)
	if err := row.Scan(dst...); err != nil {
		return r, err
	}
	rid, _ := ids.FromUUID(id)
	r.Id = rid.String()
	r.Class, r.Trigger, r.Status = adminjobsapi.JobClass(class), adminjobsapi.JobTrigger(trig), adminjobsapi.JobRunStatus(status)
	r.ScheduledAt, r.StartedAt, r.FinishedAt = utcPtr(sched), utcPtr(started), utcPtr(finished)
	r.CreatedAt = r.CreatedAt.UTC()
	r.DurationMs, r.LatenessMs = intPtr(dur), intPtr(late)
	if bid, ok := ids.FromUUID(by); ok {
		v := bid.String()
		r.TriggeredBy = &v
	}
	if len(summary) > 0 {
		var m map[string]int64
		if err := json.Unmarshal(summary, &m); err != nil {
			return r, fmt.Errorf("adminapi: run summary: %w", err)
		}
		c := make(adminjobsapi.JobSummaryCounts, len(m))
		for k, v := range m {
			c[k] = int(v)
		}
		r.Summary = &c
	}
	return r, nil
}

const runsSQL = `SELECT ` + runColumns + ` FROM job_runs
WHERE kind = $1
  AND ($2::text IS NULL OR status = $2)
  AND ($3::text IS NULL OR trigger = $3)
  AND ($4::timestamptz IS NULL OR created_at >= $4)
  AND ($5::timestamptz IS NULL OR created_at < $5)
  AND ($6::timestamptz IS NULL OR (created_at, id) < ($6, $7::uuid))
ORDER BY created_at DESC, id DESC
LIMIT $8`

// ListAdminJobRuns implements GET /v1/admin/jobs/{kind}/runs.
func (s *Service) ListAdminJobRuns(ctx context.Context, req adminjobsapi.ListAdminJobRunsRequestObject) (adminjobsapi.ListAdminJobRunsResponseObject, error) {
	a, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	k, err := s.kindFor(a, req.Kind)
	if err != nil {
		return nil, err
	}
	p := req.Params
	if p.From != nil && p.To != nil && p.From.After(*p.To) {
		return nil, problem.New(genproblems.ValidationFailed, "from is after to").WithField("query.from", "after to")
	}
	limit := defaultLimit
	if p.Limit != nil {
		limit = min(max(*p.Limit, 1), maxLimit)
	}
	var curAt *time.Time
	var curID pgtype.UUID
	if p.Cursor != nil && *p.Cursor != "" {
		at, id, err := decodeCursor(*p.Cursor)
		if err != nil {
			return nil, problem.New(genproblems.ValidationFailed, "bad cursor").WithField("query.cursor", "not a cursor from this list")
		}
		curAt, curID = &at, ids.UUID(id)
	}
	if s.d.DB == nil {
		return nil, unavailable()
	}
	now := s.d.Clock.Now()
	rows, err := s.d.DB.Q(ctx).Query(ctx, runsSQL, k.Name, (*string)(p.Status), (*string)(p.Trigger), p.From, p.To, curAt, curID, limit+1)
	if err != nil {
		return nil, internal(fmt.Errorf("adminapi: runs: %w", err))
	}
	defer rows.Close()
	items := make([]adminjobsapi.JobRun, 0, limit)
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, internal(err)
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, internal(fmt.Errorf("adminapi: runs: %w", err))
	}
	out := adminjobsapi.ListAdminJobRuns200JSONResponse{ServerNow: now.UTC()}
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		c := encodeCursor(last.CreatedAt, last.Id)
		out.NextCursor = &c
	}
	out.Items = items
	return out, nil
}

// encodeCursor makes the opaque cursor after (createdAt, id).
func encodeCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixNano(), 10) + "." + id))
}

func decodeCursor(c string) (time.Time, ids.ID, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, ids.ID{}, err
	}
	ns, sid, ok := strings.Cut(string(b), ".")
	if !ok {
		return time.Time{}, ids.ID{}, errors.New("adminapi: cursor has no id")
	}
	n, err := strconv.ParseInt(ns, 10, 64)
	if err != nil {
		return time.Time{}, ids.ID{}, err
	}
	id, err := ids.Parse(sid)
	if err != nil {
		return time.Time{}, ids.ID{}, err
	}
	return time.Unix(0, n).UTC(), id, nil
}

// GetAdminJobRun implements GET /v1/admin/job-runs/{runId}.
func (s *Service) GetAdminJobRun(ctx context.Context, req adminjobsapi.GetAdminJobRunRequestObject) (adminjobsapi.GetAdminJobRunResponseObject, error) {
	a, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if s.d.DB == nil {
		return nil, unavailable()
	}
	run, note, runErr, riverID, err := s.loadRun(ctx, req.RunId)
	if err != nil {
		return nil, err
	}
	if !mayRead(a, platformjobs.Class(run.Class)) {
		return nil, problem.New(genproblems.Forbidden, "finance reads money jobs only")
	}
	attempts, err := s.attempts(ctx, run, runErr, riverID)
	if err != nil {
		return nil, internal(err)
	}
	out := adminjobsapi.GetAdminJobRun200JSONResponse{Run: run, Note: note, Error: runErr, Attempts: attempts, ServerNow: s.d.Clock.Now().UTC()}
	out.Explore.LogsQuery = logsQuery(run.Id)
	out.Explore.TraceId = run.TraceId
	return out, nil
}

// logsQuery is the LogQL for a run's log lines: the runner logs every line
// of a run with run_id (and the run id is a ULID, so it is safe to quote).
func logsQuery(runID string) string {
	return `{service_name=~"todayz-.+"} |= "` + runID + `"`
}

// loadRun reads one run by id: 404 when there is none.
func (s *Service) loadRun(ctx context.Context, runID string) (run adminjobsapi.JobRun, note, runErr *string, riverID *int64, err error) {
	id, perr := ids.Parse(runID)
	if perr != nil {
		return run, nil, nil, nil, problem.New(genproblems.NotFound, "no such run")
	}
	row := s.d.DB.Q(ctx).QueryRow(ctx, `SELECT `+runColumns+`, note, error, river_job_id FROM job_runs WHERE id = $1
		ORDER BY created_at DESC LIMIT 1`, ids.UUID(id))
	run, err = scanRun(row, &note, &runErr, &riverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return run, nil, nil, nil, problem.New(genproblems.NotFound, "no such run")
	}
	if err != nil {
		return run, nil, nil, nil, internal(fmt.Errorf("adminapi: run: %w", err))
	}
	return run, note, runErr, riverID, nil
}

// attempts is the run's timeline: every logged attempt of the same River
// job, oldest first (just the run itself without a River job, e.g. missed).
func (s *Service) attempts(ctx context.Context, run adminjobsapi.JobRun, runErr *string, riverID *int64) ([]adminjobsapi.JobRunAttempt, error) {
	if riverID == nil {
		return []adminjobsapi.JobRunAttempt{{Id: run.Id, Attempt: run.Attempt, Status: run.Status,
			StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, Error: runErr}}, nil
	}
	rows, err := s.d.DB.Q(ctx).Query(ctx, `SELECT id, attempt, status, started_at, finished_at, error FROM job_runs
		WHERE kind = $1 AND river_job_id = $2 ORDER BY attempt, created_at, id`, run.Kind, *riverID)
	if err != nil {
		return nil, fmt.Errorf("adminapi: attempts: %w", err)
	}
	defer rows.Close()
	var out []adminjobsapi.JobRunAttempt
	for rows.Next() {
		var (
			id                pgtype.UUID
			at                adminjobsapi.JobRunAttempt
			status            string
			started, finished *time.Time
		)
		if err := rows.Scan(&id, &at.Attempt, &status, &started, &finished, &at.Error); err != nil {
			return nil, fmt.Errorf("adminapi: attempts: %w", err)
		}
		aid, _ := ids.FromUUID(id)
		at.Id, at.Status = aid.String(), adminjobsapi.JobRunStatus(status)
		at.StartedAt, at.FinishedAt = utcPtr(started), utcPtr(finished)
		out = append(out, at)
	}
	return out, rows.Err()
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func intPtr(v *int64) *int {
	if v == nil {
		return nil
	}
	i := int(*v)
	return &i
}

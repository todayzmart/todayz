package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	adminjobsapi "github.com/todayz/backend/gen/api/admin-jobs"
	"github.com/todayz/backend/gen/api/spec"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/httpx/openapi"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// stepUpCode is W1-01's step-up problem code (auth.yaml). It is not this
// package's code, so the fake answers with it only once it is registered.
const stepUpCode = "step_up_required"

// caller is who a test request is from; the fake gate reads it.
type caller struct {
	admin  Admin
	stepUp bool
}

type callerKey struct{}

// callerHeader names the test caller; the test wrapper turns it into the
// request context the fake gate reads (W1-01 will read its session).
const callerHeader = "X-Test-Caller"

// gateCall is one Require call the fake gate saw.
type gateCall struct {
	anyOf  []Role
	stepUp bool
}

// fakeGate is the W1-01 seam in tests (TODO(W1-01)): it follows Gate's
// documented contract with the caller the test put in the context.
type fakeGate struct {
	mu    sync.Mutex
	calls []gateCall
}

func (g *fakeGate) Require(ctx context.Context, anyOf []Role, stepUp bool) (Admin, error) {
	g.mu.Lock()
	g.calls = append(g.calls, gateCall{anyOf: append([]Role(nil), anyOf...), stepUp: stepUp})
	g.mu.Unlock()
	c, ok := ctx.Value(callerKey{}).(caller)
	if !ok {
		return Admin{}, problem.New(genproblems.Unauthorized, "no admin session")
	}
	if !c.admin.HasAny(anyOf) {
		return Admin{}, problem.New(genproblems.Forbidden, "role not allowed")
	}
	if stepUp && !c.stepUp {
		if problem.Registered(stepUpCode) {
			return Admin{}, problem.New(stepUpCode, "step up first")
		}
		return Admin{}, problem.New(genproblems.Forbidden, "step up first")
	}
	return c.admin, nil
}

func (g *fakeGate) sawStepUp() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.calls {
		if c.stepUp {
			return true
		}
	}
	return false
}

// istAt is a wall time in IST.
func istAt(y int, mo time.Month, d, h, mi, s int) time.Time {
	return time.Date(y, mo, d, h, mi, s, 0, clock.IST)
}

// testNow is 2026-10-22 10:02:30 IST: rollup_today's latest tick is 10:00.
var testNow = istAt(2026, time.October, 22, 10, 2, 30)

const runbook = "docs/runbooks/jobs-job-failing.md"

func ok(context.Context, platformjobs.Run) (platformjobs.Result, error) {
	return platformjobs.Result{}, nil
}

// defineKinds is the registry the tests use: one kind per rule the API
// applies (rollup, money, scheduled timer, event timer, event notify).
func defineKinds(r *platformjobs.Registry) {
	base := func(name string, c platformjobs.Class) platformjobs.Kind {
		return platformjobs.Kind{Name: name, Owner: "W1-11", Description: "test kind " + name, Class: c,
			Timeout: 10 * time.Second, MaxAttempts: 3, Handler: ok}
	}
	k := base("rollup_today", platformjobs.ClassRollup)
	k.Schedule, k.UniqueBy = "*/5 * * * *", platformjobs.PerTick
	platformjobs.Define(r, k)

	k = base("ledger_reconcile", platformjobs.ClassMoney)
	k.Schedule, k.UniqueBy, k.Runbook = "30 2 * * *", platformjobs.PerDay, runbook
	platformjobs.Define(r, k)

	k = base("hold_expiry_sweep", platformjobs.ClassTimer)
	k.Schedule, k.UniqueBy, k.Runbook = "* * * * *", platformjobs.PerTick, runbook
	platformjobs.Define(r, k)

	k = base("order_accept_expiry", platformjobs.ClassTimer)
	k.Runbook = runbook
	platformjobs.Define(r, k)

	platformjobs.Define(r, base("notify_push", platformjobs.ClassNotify))
}

// apiHarness is the admin-jobs API on a fresh database, behind the real
// router (validator, idempotency) with the fake gate.
type apiHarness struct {
	t       *testing.T
	ctx     context.Context
	db      *testkit.DB
	reg     *platformjobs.Registry
	clk     *clock.Offset
	gate    *fakeGate
	h       http.Handler
	callers map[string]caller
}

// newAPI builds the harness. With worker set, River works the jobs through
// the jobs runner (so a run-now writes its job_runs row); without it jobs
// stay queued, which is what the busy checks need.
func newAPI(t *testing.T, worker bool) *apiHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	a := &apiHarness{t: t, ctx: ctx, db: testkit.NewDB(t), reg: platformjobs.NewRegistry(),
		clk: clock.NewOffset(nil), gate: &fakeGate{}}
	a.clk.Set(testNow)
	defineKinds(a.reg)

	var qc *queue.Client
	var err error
	if worker {
		run, rerr := platformjobs.NewRunner(a.reg, platformjobs.RunnerOptions{Pool: a.db.Pool, Clock: a.clk, Log: quiet()})
		require.NoError(t, rerr)
		qreg := queue.NewRegistry()
		run.Register(qreg)
		qc, err = queue.NewWorkerClient(a.db.Pool, qreg, queue.Options{Log: quiet(), Time: a.clk})
		require.NoError(t, err)
		require.NoError(t, queue.Start(ctx, qc))
		t.Cleanup(func() {
			stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = qc.Stop(stop)
			run.Close()
		})
	} else {
		qc, err = queue.NewInsertClient(a.db.Pool, queue.Options{Log: quiet(), Time: a.clk})
		require.NoError(t, err)
	}

	d := db.New(a.db.Pool)
	v, err := openapi.New(spec.Bundle)
	require.NoError(t, err)
	r := httpx.NewRouter(httpx.Options{Log: quiet(), Validator: v, Policies: v,
		Idempotency: idempotency.Options{Store: idempotency.NewMemoryStore(), Clock: a.clk, Log: quiet()}})
	Register(r, Deps{Registry: a.reg, DB: d, Enqueuer: platformjobs.NewEnqueuer(a.reg, qc, a.clk),
		Audit: audit.New(d, a.clk, nil), Clock: a.clk, Gate: a.gate, Log: quiet()})
	require.NoError(t, r.Check())

	at := testNow
	who := func(rs ...Role) caller {
		at = at.Add(time.Millisecond)
		return caller{admin: Admin{ID: ids.New(at), Roles: rs}}
	}
	a.callers = map[string]caller{
		"admin":       who(RoleAdmin),
		"super":       who(RoleSuperAdmin),
		"analyst":     who(RoleAnalyst),
		"support":     who(RoleSupport),
		"finance":     who(RoleFinance),
		"field_agent": who(RoleFieldAgent),
		"catalogue":   who(RoleCatalogue),
	}
	su := a.callers["super"]
	su.stepUp = true
	a.callers["super_stepup"] = su
	a.h = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if c, ok := a.callers[req.Header.Get(callerHeader)]; ok {
			// What W1-01's authn slot will do: the admin is the principal
			// (the validator refuses an anonymous /v1/admin call with 401).
			ctx := reqctx.WithPrincipal(req.Context(), reqctx.Principal{Kind: "admin", UserID: c.admin.ID.String()})
			req = req.WithContext(context.WithValue(ctx, callerKey{}, c))
		}
		r.ServeHTTP(w, req)
	})
	return a
}

// resp is one answer.
type resp struct {
	status int
	body   []byte
}

func (r resp) problem(t *testing.T) problem.Body {
	t.Helper()
	var p problem.Body
	require.NoError(t, json.Unmarshal(r.body, &p), string(r.body))
	return p
}

func decode[T any](t *testing.T, r resp) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(r.body, &v), string(r.body))
	return v
}

// do sends one request as who ("" is no admin session). key is the
// Idempotency-Key ("" for none).
func (a *apiHarness) do(method, path, who string, body any, key string) resp {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(a.t, err)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequestWithContext(a.ctx, method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if who != "" {
		req.Header.Set(callerHeader, who)
	}
	w := httptest.NewRecorder()
	a.h.ServeHTTP(w, req)
	return resp{status: w.Code, body: w.Body.Bytes()}
}

// newKey is a fresh Idempotency-Key (a ULID).
func newKey() string { return ids.New(testNow).String() }

func reason(r string) map[string]any { return map[string]any{"reason": r} }

func (a *apiHarness) count(q string, args ...any) int {
	a.t.Helper()
	var n int
	require.NoError(a.t, a.db.Pool.QueryRow(a.ctx, q, args...).Scan(&n))
	return n
}

// auditRow is one audit_log row of action.
type auditRow struct {
	actorKind, subjectType, subjectID string
	actorID                           ids.ID
	before, after                     map[string]any
}

func (a *apiHarness) audits(action string) []auditRow {
	a.t.Helper()
	rows, err := a.db.Pool.Query(a.ctx, `SELECT actor_kind, actor_id, subject_type, coalesce(subject_id, ''),
		coalesce(before, 'null'::jsonb), coalesce(after, 'null'::jsonb) FROM audit_log WHERE action = $1 ORDER BY created_at, id`, action)
	require.NoError(a.t, err)
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var (
			r             auditRow
			actor         [16]byte
			before, after []byte
		)
		require.NoError(a.t, rows.Scan(&r.actorKind, &actor, &r.subjectType, &r.subjectID, &before, &after))
		r.actorID = ids.ID(actor)
		require.NoError(a.t, json.Unmarshal(before, &r.before))
		require.NoError(a.t, json.Unmarshal(after, &r.after))
		out = append(out, r)
	}
	require.NoError(a.t, rows.Err())
	return out
}

// riverArgs are the queued River jobs' envelopes of kind.
func (a *apiHarness) riverArgs(kind string) []map[string]any {
	a.t.Helper()
	rows, err := a.db.Pool.Query(a.ctx, `SELECT args FROM river_job WHERE kind = $1 ORDER BY id`, kind)
	require.NoError(a.t, err)
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var b []byte
		require.NoError(a.t, rows.Scan(&b))
		var m map[string]any
		require.NoError(a.t, json.Unmarshal(b, &m))
		out = append(out, m)
	}
	require.NoError(a.t, rows.Err())
	return out
}

// insertRun writes a job_runs row as the runner would have.
func (a *apiHarness) insertRun(kind string, class platformjobs.Class, status platformjobs.Status, trig platformjobs.Trigger,
	period string, created time.Time, riverID *int64, attempt int) ids.ID {
	a.t.Helper()
	id := ids.New(created)
	var p any
	if period != "" {
		p = period
	}
	var errText any
	if status == platformjobs.StatusFailed || status == platformjobs.StatusDiscarded {
		errText = "boom"
	}
	_, err := a.db.Pool.Exec(a.ctx, `INSERT INTO job_runs (id, kind, class, river_job_id, trigger, period_key, attempt, status,
		started_at, finished_at, duration_ms, error, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $9)`,
		ids.UUID(id), kind, string(class), riverID, string(trig), p, attempt, string(status),
		created, created.Add(1500*time.Millisecond), 1500, errText)
	require.NoError(a.t, err)
	return id
}

// --- SC-JB-06: run-now is admin-only, needs a reason, is audited and is
// idempotent; SC-JB-11: a second run while one is queued is refused.

func TestRunNowAdminOnlyAuditedIdempotent(t *testing.T) {
	a := newAPI(t, false)
	const path = "/v1/admin/jobs/rollup_today/run"

	// No admin session: 401, and nothing is enqueued.
	require.Equal(t, http.StatusUnauthorized, a.do("POST", path, "", reason("nightly totals are stale"), newKey()).status)
	// Readers and other roles: 403.
	for _, who := range []string{"analyst", "support", "finance", "field_agent", "catalogue"} {
		r := a.do("POST", path, who, reason("nightly totals are stale"), newKey())
		require.Equal(t, http.StatusForbidden, r.status, who)
		require.Equal(t, genproblems.Forbidden, r.problem(t).Code, who)
	}
	// A reason is required (contract) and must be more than blanks (code).
	r := a.do("POST", path, "admin", map[string]any{}, newKey())
	require.Equal(t, http.StatusUnprocessableEntity, r.status)
	require.Contains(t, r.problem(t).Fields, "body.reason")
	r = a.do("POST", path, "admin", reason("    "), newKey())
	require.Equal(t, http.StatusUnprocessableEntity, r.status)
	require.Equal(t, "too short", r.problem(t).Fields["body.reason"])
	// A period that is not one of the kind's: 422 on body.periodKey.
	for _, p := range []string{"2026-10-22", "2026-10-22T10:03", "2026-10-22T10:10"} {
		r = a.do("POST", path, "admin", map[string]any{"reason": "rerun please", "periodKey": p}, newKey())
		require.Equal(t, http.StatusUnprocessableEntity, r.status, p)
		require.Contains(t, r.problem(t).Fields, "body.periodKey", p)
	}
	require.Equal(t, 0, a.count(`SELECT count(*) FROM river_job`))
	require.Empty(t, a.audits(actionRun))

	// The admin's run: 202 with the run id and the latest tick's period.
	key := newKey()
	r = a.do("POST", path, "admin", reason("nightly totals are stale"), key)
	require.Equal(t, http.StatusAccepted, r.status, string(r.body))
	acc := decode[adminjobsapi.JobRunAccepted](t, r)
	require.Equal(t, "rollup_today", acc.Kind)
	require.Equal(t, adminjobsapi.JobTrigger("manual"), acc.Trigger)
	require.Equal(t, "2026-10-22T10:00", acc.PeriodKey)
	require.True(t, ids.Valid(acc.RunId))

	// The same click again (same key): the same answer, one job.
	r2 := a.do("POST", path, "admin", reason("nightly totals are stale"), key)
	require.Equal(t, http.StatusAccepted, r2.status)
	require.Equal(t, acc.RunId, decode[adminjobsapi.JobRunAccepted](t, r2).RunId)

	jobs := a.riverArgs("rollup_today")
	require.Len(t, jobs, 1)
	admin := a.callers["admin"].admin
	require.Equal(t, acc.RunId, jobs[0]["r"])
	require.Equal(t, "manual", jobs[0]["t"])
	require.Equal(t, "2026-10-22T10:00", jobs[0]["p"])
	require.Equal(t, admin.ID.String(), jobs[0]["by"])

	// Audited once, as the admin, with the reason.
	rows := a.audits(actionRun)
	require.Len(t, rows, 1)
	require.Equal(t, "admin", rows[0].actorKind)
	require.Equal(t, admin.ID, rows[0].actorID)
	require.Equal(t, subjectKind, rows[0].subjectType)
	require.Equal(t, "rollup_today", rows[0].subjectID)
	require.Equal(t, "nightly totals are stale", rows[0].after["reason"])
	require.Equal(t, acc.RunId, rows[0].after["runId"])

	// SC-JB-11: a second run (new key) while the first is queued: 409.
	r = a.do("POST", path, "super", reason("second click"), newKey())
	require.Equal(t, http.StatusConflict, r.status)
	require.Equal(t, genproblems.JobAlreadyRunning, r.problem(t).Code)
	// A run row that is running counts too, once River's job is gone.
	_, err := a.db.Pool.Exec(a.ctx, `UPDATE river_job SET state = 'completed', finalized_at = now() WHERE kind = 'rollup_today'`)
	require.NoError(t, err)
	a.insertRun("rollup_today", platformjobs.ClassRollup, platformjobs.StatusRunning, platformjobs.TriggerSchedule,
		"2026-10-22T10:00", testNow.Add(-time.Minute), nil, 1)
	r = a.do("POST", path, "admin", reason("third click"), newKey())
	require.Equal(t, http.StatusConflict, r.status)
	require.Equal(t, genproblems.JobAlreadyRunning, r.problem(t).Code)
	require.Len(t, a.riverArgs("rollup_today"), 1)
	require.Len(t, a.audits(actionRun), 1)

	// An explicit past period runs that period.
	_, err = a.db.Pool.Exec(a.ctx, `UPDATE job_runs SET status = 'succeeded' WHERE kind = 'rollup_today'`)
	require.NoError(t, err)
	r = a.do("POST", path, "admin", map[string]any{"reason": "backfill one tick", "periodKey": "2026-10-22T09:55"}, newKey())
	require.Equal(t, http.StatusAccepted, r.status, string(r.body))
	require.Equal(t, "2026-10-22T09:55", decode[adminjobsapi.JobRunAccepted](t, r).PeriodKey)
	// A future period is refused.
	_, err = a.db.Pool.Exec(a.ctx, `UPDATE river_job SET state = 'completed', finalized_at = now() WHERE kind = 'rollup_today'`)
	require.NoError(t, err)
	r = a.do("POST", path, "admin", map[string]any{"reason": "too early", "periodKey": "2026-10-22T10:05"}, newKey())
	require.Equal(t, http.StatusUnprocessableEntity, r.status)
	require.Equal(t, "in the future", r.problem(t).Fields["body.periodKey"])
}

// --- SC-JB-07: timer kinds are never paused, run or retried by hand, and
// event kinds are not run by hand.

func TestTimerAndEventKindsAreNotRunnable(t *testing.T) {
	a := newAPI(t, false)
	for _, kind := range []string{"hold_expiry_sweep", "order_accept_expiry"} {
		for _, op := range []string{"run", "pause", "resume"} {
			r := a.do("POST", "/v1/admin/jobs/"+kind+"/"+op, "super_stepup", reason("try it by hand"), newKey())
			require.Equal(t, http.StatusConflict, r.status, kind+" "+op)
			require.Equal(t, genproblems.JobNotRunnable, r.problem(t).Code, kind+" "+op)
		}
	}
	r := a.do("POST", "/v1/admin/jobs/notify_push/run", "admin", reason("send them now"), newKey())
	require.Equal(t, http.StatusConflict, r.status)
	require.Equal(t, genproblems.JobNotRunnable, r.problem(t).Code)

	// A failed timer run is not retried by hand either.
	id := a.insertRun("hold_expiry_sweep", platformjobs.ClassTimer, platformjobs.StatusFailed, platformjobs.TriggerSchedule,
		"2026-10-22T10:01", testNow.Add(-time.Minute), nil, 1)
	r = a.do("POST", "/v1/admin/job-runs/"+id.String()+"/retry", "super_stepup", reason("retry the sweep"), newKey())
	require.Equal(t, http.StatusConflict, r.status)
	require.Equal(t, genproblems.JobNotRunnable, r.problem(t).Code)

	// The list says so: a timer kind is neither runnable nor pausable.
	list := decode[adminjobsapi.JobKindList](t, a.do("GET", "/v1/admin/jobs", "admin", nil, ""))
	for _, k := range list.Items {
		switch k.Kind {
		case "hold_expiry_sweep", "order_accept_expiry":
			require.False(t, k.Runnable, k.Kind)
			require.False(t, k.Pausable, k.Kind)
		case "notify_push":
			require.False(t, k.Runnable, k.Kind)
			require.True(t, k.Pausable, k.Kind)
		default:
			require.True(t, k.Runnable, k.Kind)
		}
	}
	// An unknown kind is 404; nothing was enqueued or audited.
	require.Equal(t, http.StatusNotFound, a.do("POST", "/v1/admin/jobs/no_such_kind/run", "admin", reason("who knows"), newKey()).status)
	require.Equal(t, 0, a.count(`SELECT count(*) FROM river_job`))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM audit_log`))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM job_controls`))
}

// --- SC-JB-08: money kinds need super_admin with step-up; finance reads
// money kinds only.

func TestMoneyKindsNeedSuperAdminWithStepUp(t *testing.T) {
	a := newAPI(t, false)
	const path = "/v1/admin/jobs/ledger_reconcile/run"
	r := a.do("POST", path, "admin", reason("ledger is behind"), newKey())
	require.Equal(t, http.StatusForbidden, r.status)
	require.Equal(t, genproblems.Forbidden, r.problem(t).Code)

	r = a.do("POST", path, "super", reason("ledger is behind"), newKey())
	require.Equal(t, http.StatusForbidden, r.status)
	if problem.Registered(stepUpCode) {
		require.Equal(t, stepUpCode, r.problem(t).Code)
	}
	require.True(t, a.gate.sawStepUp(), "a money action asks the gate for step-up")
	r = a.do("POST", "/v1/admin/jobs/ledger_reconcile/pause", "super", reason("ledger is behind"), newKey())
	require.Equal(t, http.StatusForbidden, r.status)
	require.Equal(t, 0, a.count(`SELECT count(*) FROM river_job`))

	r = a.do("POST", path, "super_stepup", reason("ledger is behind"), newKey())
	require.Equal(t, http.StatusAccepted, r.status, string(r.body))
	acc := decode[adminjobsapi.JobRunAccepted](t, r)
	require.Equal(t, "2026-10-22", acc.PeriodKey) // the 02:30 IST tick of the 22nd
	rows := a.audits(actionRun)
	require.Len(t, rows, 1)
	require.Equal(t, a.callers["super"].admin.ID, rows[0].actorID)

	// finance: the list holds money kinds only; other kinds are 403.
	list := decode[adminjobsapi.JobKindList](t, a.do("GET", "/v1/admin/jobs", "finance", nil, ""))
	require.Len(t, list.Items, 1)
	require.Equal(t, "ledger_reconcile", list.Items[0].Kind)
	require.Equal(t, http.StatusOK, a.do("GET", "/v1/admin/jobs/ledger_reconcile", "finance", nil, "").status)
	require.Equal(t, http.StatusOK, a.do("GET", "/v1/admin/jobs/ledger_reconcile/runs", "finance", nil, "").status)
	require.Equal(t, http.StatusForbidden, a.do("GET", "/v1/admin/jobs/rollup_today", "finance", nil, "").status)
	require.Equal(t, http.StatusForbidden, a.do("GET", "/v1/admin/jobs/rollup_today/runs", "finance", nil, "").status)
	run := a.insertRun("rollup_today", platformjobs.ClassRollup, platformjobs.StatusSucceeded, platformjobs.TriggerSchedule,
		"2026-10-22T09:55", testNow.Add(-7*time.Minute), nil, 1)
	require.Equal(t, http.StatusForbidden, a.do("GET", "/v1/admin/job-runs/"+run.String(), "finance", nil, "").status)
	require.Equal(t, http.StatusOK, a.do("GET", "/v1/admin/job-runs/"+run.String(), "analyst", nil, "").status)
	// Every other reader sees every kind; field_agent and catalogue see none.
	for _, who := range []string{"support", "analyst", "admin"} {
		require.Len(t, decode[adminjobsapi.JobKindList](t, a.do("GET", "/v1/admin/jobs", who, nil, "")).Items, 5, who)
	}
	for _, who := range []string{"field_agent", "catalogue"} {
		require.Equal(t, http.StatusForbidden, a.do("GET", "/v1/admin/jobs", who, nil, "").status, who)
	}
	require.Equal(t, http.StatusUnauthorized, a.do("GET", "/v1/admin/jobs", "", nil, "").status)
}

// --- SC-JB-09: pause and resume are audited, show in the list, and a
// paused kind refuses run-now.

func TestPauseResume(t *testing.T) {
	a := newAPI(t, false)
	const base = "/v1/admin/jobs/rollup_today/"
	r := a.do("POST", base+"pause", "analyst", reason("noisy job"), "")
	require.Equal(t, http.StatusForbidden, r.status)
	r = a.do("POST", base+"pause", "admin", map[string]any{}, "")
	require.Equal(t, http.StatusUnprocessableEntity, r.status)

	r = a.do("POST", base+"pause", "admin", reason("upstream outage"), "")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	st := decode[adminjobsapi.JobControlState](t, r)
	admin := a.callers["admin"].admin
	require.True(t, st.Control.Paused)
	require.Equal(t, "upstream outage", *st.Control.Reason)
	require.Equal(t, admin.ID.String(), *st.Control.UpdatedBy)
	// The offset clock keeps moving from testNow, so a slow run (make check
	// under load) only bounds the stamp: from testNow to the clock after the
	// response, never a fixed tolerance.
	require.False(t, st.Control.UpdatedAt.Before(testNow), "updatedAt %s is before testNow", st.Control.UpdatedAt)
	require.False(t, st.Control.UpdatedAt.After(a.clk.Now()), "updatedAt %s is after the clock", st.Control.UpdatedAt)

	r = a.do("POST", base+"pause", "admin", reason("upstream outage"), "")
	require.Equal(t, http.StatusConflict, r.status)
	require.Equal(t, genproblems.InvalidTransition, r.problem(t).Code)

	// The list: paused health, no next run; the status filter finds it.
	list := decode[adminjobsapi.JobKindList](t, a.do("GET", "/v1/admin/jobs?status=paused", "support", nil, ""))
	require.Len(t, list.Items, 1)
	require.Equal(t, "rollup_today", list.Items[0].Kind)
	require.Equal(t, adminjobsapi.JobHealthPaused, list.Items[0].Health)
	require.Nil(t, list.Items[0].NextRunAt)
	require.True(t, list.Items[0].Control.Paused)

	// Run-now on a paused kind: 409 job_paused, nothing enqueued.
	r = a.do("POST", base+"run", "admin", reason("run anyway"), newKey())
	require.Equal(t, http.StatusConflict, r.status)
	require.Equal(t, genproblems.JobPaused, r.problem(t).Code)
	require.Equal(t, 0, a.count(`SELECT count(*) FROM river_job`))

	r = a.do("POST", base+"resume", "super", reason("upstream is back"), "")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	require.False(t, decode[adminjobsapi.JobControlState](t, r).Control.Paused)
	r = a.do("POST", base+"resume", "admin", reason("upstream is back"), "")
	require.Equal(t, http.StatusConflict, r.status)
	require.Equal(t, genproblems.InvalidTransition, r.problem(t).Code)

	// The pause window is kept for the watchdog: paused_at from the pause,
	// resumed_at from the resume.
	var pausedAt, resumedAt *time.Time
	require.NoError(t, a.db.Pool.QueryRow(context.Background(),
		`SELECT paused_at, resumed_at FROM job_controls WHERE kind = 'rollup_today'`).Scan(&pausedAt, &resumedAt))
	require.NotNil(t, pausedAt)
	require.NotNil(t, resumedAt)
	require.True(t, pausedAt.Equal(st.Control.UpdatedAt.Truncate(time.Microsecond)), "paused_at %s is the pause's stamp %s", pausedAt, st.Control.UpdatedAt)
	require.True(t, resumedAt.After(*pausedAt))

	detail := decode[adminjobsapi.JobKindDetail](t, a.do("GET", "/v1/admin/jobs/rollup_today", "admin", nil, ""))
	require.Equal(t, adminjobsapi.JobHealthOk, detail.Job.Health)
	require.NotNil(t, detail.Job.NextRunAt)
	require.Equal(t, istAt(2026, time.October, 22, 10, 5, 0).UTC(), *detail.Job.NextRunAt)

	p := a.audits(actionPause)
	require.Len(t, p, 1)
	require.Equal(t, admin.ID, p[0].actorID)
	require.Equal(t, false, p[0].before["paused"])
	require.Equal(t, true, p[0].after["paused"])
	require.Equal(t, "upstream outage", p[0].after["reason"])
	rs := a.audits(actionResume)
	require.Len(t, rs, 1)
	require.Equal(t, a.callers["super"].admin.ID, rs[0].actorID)
	require.Equal(t, true, rs[0].before["paused"])
	require.Equal(t, false, rs[0].after["paused"])

	// An event (non-timer) kind can be paused.
	require.Equal(t, http.StatusOK, a.do("POST", "/v1/admin/jobs/notify_push/pause", "admin", reason("provider down"), "").status)
}

// --- SC-JB-10: retry keeps the period, is audited against the original
// run, and starts only from failed, discarded or missed.

func TestRetry(t *testing.T) {
	a := newAPI(t, false)
	failed := a.insertRun("rollup_today", platformjobs.ClassRollup, platformjobs.StatusFailed, platformjobs.TriggerSchedule,
		"2026-10-22T09:50", testNow.Add(-12*time.Minute), nil, 3)
	done := a.insertRun("rollup_today", platformjobs.ClassRollup, platformjobs.StatusSucceeded, platformjobs.TriggerSchedule,
		"2026-10-22T09:55", testNow.Add(-7*time.Minute), nil, 1)
	missed := a.insertRun("rollup_today", platformjobs.ClassRollup, platformjobs.StatusMissed, platformjobs.TriggerSchedule,
		"2026-10-22T09:45", testNow.Add(-17*time.Minute), nil, 0)

	retry := func(id ids.ID, who string) resp {
		return a.do("POST", "/v1/admin/job-runs/"+id.String()+"/retry", who, reason("transient upstream error"), newKey())
	}
	require.Equal(t, http.StatusForbidden, retry(failed, "support").status)
	r := retry(done, "admin")
	require.Equal(t, http.StatusConflict, r.status)
	require.Equal(t, genproblems.InvalidTransition, r.problem(t).Code)
	require.Equal(t, http.StatusNotFound, retry(ids.New(testNow), "admin").status)

	r = retry(failed, "admin")
	require.Equal(t, http.StatusAccepted, r.status, string(r.body))
	acc := decode[adminjobsapi.JobRunAccepted](t, r)
	require.Equal(t, failed.String(), *acc.RetryOf)
	require.Equal(t, "2026-10-22T09:50", acc.PeriodKey)
	require.Equal(t, adminjobsapi.JobTrigger("retry"), acc.Trigger)
	require.NotEqual(t, failed.String(), acc.RunId)
	jobs := a.riverArgs("rollup_today")
	require.Len(t, jobs, 1)
	require.Equal(t, "retry", jobs[0]["t"])
	require.Equal(t, "2026-10-22T09:50", jobs[0]["p"])
	require.Equal(t, acc.RunId, jobs[0]["r"])

	rows := a.audits(actionRetry)
	require.Len(t, rows, 1)
	require.Equal(t, subjectRun, rows[0].subjectType)
	require.Equal(t, failed.String(), rows[0].subjectID)
	require.Equal(t, "failed", rows[0].before["status"])
	require.Equal(t, acc.RunId, rows[0].after["runId"])

	// The missed run is retryable, but not while the retry is queued.
	r = retry(missed, "admin")
	require.Equal(t, http.StatusConflict, r.status)
	require.Equal(t, genproblems.JobAlreadyRunning, r.problem(t).Code)
	_, err := a.db.Pool.Exec(a.ctx, `UPDATE river_job SET state = 'completed', finalized_at = now() WHERE kind = 'rollup_today'`)
	require.NoError(t, err)
	r = retry(missed, "admin")
	require.Equal(t, http.StatusAccepted, r.status, string(r.body))
	require.Equal(t, "2026-10-22T09:45", decode[adminjobsapi.JobRunAccepted](t, r).PeriodKey)

	// A money run needs super_admin with step-up to retry.
	money := a.insertRun("ledger_reconcile", platformjobs.ClassMoney, platformjobs.StatusDiscarded, platformjobs.TriggerSchedule,
		"2026-10-21", testNow.Add(-8*time.Hour), nil, 3)
	require.Equal(t, http.StatusForbidden, retry(money, "admin").status)
	require.Equal(t, http.StatusForbidden, retry(money, "super").status)
	require.Equal(t, http.StatusAccepted, retry(money, "super_stepup").status)
	// An event kind's failed run is not replayed by hand (TODO(lead)).
	ev := a.insertRun("notify_push", platformjobs.ClassNotify, platformjobs.StatusDiscarded, platformjobs.TriggerEvent,
		"", testNow.Add(-time.Hour), nil, 3)
	r = retry(ev, "admin")
	require.Equal(t, http.StatusConflict, r.status)
	require.Equal(t, genproblems.JobNotRunnable, r.problem(t).Code)
}

// --- Reads: health, 7-day stats, run history paging and run detail.

func TestReads(t *testing.T) {
	a := newAPI(t, false)
	// rollup_today: last finished run failed → failing.
	a.insertRun("rollup_today", platformjobs.ClassRollup, platformjobs.StatusSucceeded, platformjobs.TriggerSchedule,
		"2026-10-22T09:50", testNow.Add(-12*time.Minute), nil, 1)
	rid := int64(4242)
	first := a.insertRun("rollup_today", platformjobs.ClassRollup, platformjobs.StatusFailed, platformjobs.TriggerSchedule,
		"2026-10-22T09:55", testNow.Add(-7*time.Minute), &rid, 1)
	second := a.insertRun("rollup_today", platformjobs.ClassRollup, platformjobs.StatusFailed, platformjobs.TriggerSchedule,
		"2026-10-22T09:55", testNow.Add(-6*time.Minute), &rid, 2)
	// ledger_reconcile: a missed day after its last success → missed.
	a.insertRun("ledger_reconcile", platformjobs.ClassMoney, platformjobs.StatusSucceeded, platformjobs.TriggerSchedule,
		"2026-10-20", testNow.Add(-50*time.Hour), nil, 1)
	a.insertRun("ledger_reconcile", platformjobs.ClassMoney, platformjobs.StatusMissed, platformjobs.TriggerSchedule,
		"2026-10-21", testNow.Add(-26*time.Hour), nil, 0)
	// 7-day stats for rollup_today (and one row outside the window).
	_, err := a.db.Pool.Exec(a.ctx, `INSERT INTO job_stats_hourly (kind, hour, succeeded, failed, skipped, sum_ms, max_ms)
		VALUES ('rollup_today', $1, 3, 1, 1, 5000, 2500), ('rollup_today', $2, 100, 0, 0, 100000, 9000)`,
		testNow.Truncate(time.Hour), testNow.Add(-8*24*time.Hour).Truncate(time.Hour))
	require.NoError(t, err)

	list := decode[adminjobsapi.JobKindList](t, a.do("GET", "/v1/admin/jobs", "analyst", nil, ""))
	byKind := map[string]adminjobsapi.JobKindSummary{}
	for _, k := range list.Items {
		byKind[k.Kind] = k
	}
	require.Len(t, byKind, 5)
	ru := byKind["rollup_today"]
	require.Equal(t, adminjobsapi.JobHealthFailing, ru.Health)
	require.Equal(t, second.String(), ru.LastRun.Id)
	require.Equal(t, 5, *ru.Runs7d)
	require.Equal(t, 7500, *ru.SuccessRate7dBp) // 3 of 4 finished (skipped does not count)
	require.Equal(t, 1000, *ru.AvgDurationMs7d)
	require.Equal(t, 2500, *ru.MaxDurationMs7d)
	require.Equal(t, "Every 5 minutes", *ru.ScheduleText)
	require.Equal(t, adminjobsapi.JobHealthMissed, byKind["ledger_reconcile"].Health)
	require.Equal(t, adminjobsapi.JobHealthOk, byKind["notify_push"].Health)
	require.Nil(t, byKind["notify_push"].NextRunAt)
	require.Nil(t, byKind["notify_push"].SuccessRate7dBp)

	failing := decode[adminjobsapi.JobKindList](t, a.do("GET", "/v1/admin/jobs?status=failing", "analyst", nil, ""))
	require.Len(t, failing.Items, 1)
	money := decode[adminjobsapi.JobKindList](t, a.do("GET", "/v1/admin/jobs?class=money", "analyst", nil, ""))
	require.Len(t, money.Items, 1)

	detail := decode[adminjobsapi.JobKindDetail](t, a.do("GET", "/v1/admin/jobs/ledger_reconcile", "admin", nil, ""))
	require.Equal(t, runbook, *detail.Settings.Runbook)
	require.Equal(t, adminjobsapi.JobKindSettingsUniqueBy("day"), detail.Settings.UniqueBy)
	require.Equal(t, 10, detail.Settings.TimeoutSeconds)
	require.Equal(t, "default", detail.Settings.Queue)
	require.Equal(t, http.StatusNotFound, a.do("GET", "/v1/admin/jobs/no_such_kind", "admin", nil, "").status)

	// History: newest first, paged by cursor.
	const runs = "/v1/admin/jobs/rollup_today/runs"
	page := decode[adminjobsapi.JobRunPage](t, a.do("GET", runs+"?limit=2", "support", nil, ""))
	require.Len(t, page.Items, 2)
	require.Equal(t, second.String(), page.Items[0].Id)
	require.Equal(t, first.String(), page.Items[1].Id)
	require.NotNil(t, page.NextCursor)
	page2 := decode[adminjobsapi.JobRunPage](t, a.do("GET", runs+"?limit=2&cursor="+url.QueryEscape(*page.NextCursor), "support", nil, ""))
	require.Len(t, page2.Items, 1)
	require.Nil(t, page2.NextCursor)
	failedOnly := decode[adminjobsapi.JobRunPage](t, a.do("GET", runs+"?status=failed", "support", nil, ""))
	require.Len(t, failedOnly.Items, 2)
	// from inclusive, to exclusive.
	from := testNow.Add(-7 * time.Minute).UTC().Format(time.RFC3339Nano)
	to := testNow.Add(-6 * time.Minute).UTC().Format(time.RFC3339Nano)
	window := decode[adminjobsapi.JobRunPage](t, a.do("GET", runs+"?from="+url.QueryEscape(from)+"&to="+url.QueryEscape(to), "support", nil, ""))
	require.Len(t, window.Items, 1)
	require.Equal(t, first.String(), window.Items[0].Id)
	r := a.do("GET", runs+"?from="+url.QueryEscape(to)+"&to="+url.QueryEscape(from), "support", nil, "")
	require.Equal(t, http.StatusUnprocessableEntity, r.status)
	require.Contains(t, r.problem(t).Fields, "query.from")
	r = a.do("GET", runs+"?cursor=not-a-cursor", "support", nil, "")
	require.Equal(t, http.StatusUnprocessableEntity, r.status)
	require.Contains(t, r.problem(t).Fields, "query.cursor")

	// Run detail: both attempts of the River job, the error, the explore links.
	d := decode[adminjobsapi.JobRunDetail](t, a.do("GET", "/v1/admin/job-runs/"+second.String(), "support", nil, ""))
	require.Equal(t, second.String(), d.Run.Id)
	require.Len(t, d.Attempts, 2)
	require.Equal(t, first.String(), d.Attempts[0].Id)
	require.Equal(t, second.String(), d.Attempts[1].Id)
	require.Equal(t, "boom", *d.Error)
	require.Contains(t, d.Explore.LogsQuery, second.String())
	require.Equal(t, http.StatusNotFound, a.do("GET", "/v1/admin/job-runs/"+ids.New(testNow).String(), "support", nil, "").status)
}

// --- SC-JB-06 end to end: a run-now is worked, and its run row says who
// started it and why it ran.

func TestRunNowIsWorked(t *testing.T) {
	a := newAPI(t, true)
	r := a.do("POST", "/v1/admin/jobs/rollup_today/run", "admin", reason("refresh the totals"), newKey())
	require.Equal(t, http.StatusAccepted, r.status, string(r.body))
	acc := decode[adminjobsapi.JobRunAccepted](t, r)

	var run adminjobsapi.JobRunDetail
	require.Eventually(t, func() bool {
		got := a.do("GET", "/v1/admin/job-runs/"+acc.RunId, "support", nil, "")
		if got.status != http.StatusOK {
			return false
		}
		run = decode[adminjobsapi.JobRunDetail](t, got)
		return run.Run.Status == adminjobsapi.JobRunStatusSucceeded
	}, 30*time.Second, 50*time.Millisecond)
	require.Equal(t, adminjobsapi.JobTrigger("manual"), run.Run.Trigger)
	require.Equal(t, "2026-10-22T10:00", *run.Run.PeriodKey)
	require.Equal(t, a.callers["admin"].admin.ID.String(), *run.Run.TriggeredBy)
	require.Len(t, run.Attempts, 1)

	// Once River has finalized the job, the next run-now is accepted again
	// (River completes jobs in batches, so allow it a moment).
	require.Eventually(t, func() bool {
		return a.count(`SELECT count(*) FROM river_job WHERE kind = 'rollup_today' AND finalized_at IS NULL`) == 0
	}, 30*time.Second, 50*time.Millisecond)
	r = a.do("POST", "/v1/admin/jobs/rollup_today/run", "admin", reason("refresh the totals again"), newKey())
	require.Equal(t, http.StatusAccepted, r.status, string(r.body))
}

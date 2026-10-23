package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/telemetry"
)

func noop(context.Context, Run) (Result, error) { return Result{}, nil }

func validScheduled() Kind {
	return Kind{
		Name: "rollup_today", Owner: "W3-07", Description: "Rolls up today's numbers.",
		Class: ClassRollup, Schedule: "*/5 * * * *", UniqueBy: PerTick,
		Timeout: 30 * time.Second, MaxAttempts: 3, Handler: noop,
	}
}

func validEvent() Kind {
	return Kind{
		Name: "orders_accept_by", Owner: "W2-02", Description: "Expires an unaccepted order.",
		Class: ClassTimer, Timeout: 30 * time.Second, MaxAttempts: 10,
		Runbook: "docs/runbooks/orders-accept-by.md", Handler: noop,
	}
}

// TestValidate is SC-JB-01 and SC-JB-15 (the declaration rules).
func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*Kind)
		base func() Kind
		want string // "" = valid
	}{
		{"scheduled ok", func(*Kind) {}, validScheduled, ""},
		{"event timer ok", func(*Kind) {}, validEvent, ""},
		{"no owner", func(k *Kind) { k.Owner = "" }, validScheduled, "Owner"},
		{"owner not a WP id", func(k *Kind) { k.Owner = "orders team" }, validScheduled, "Owner"},
		{"no class", func(k *Kind) { k.Class = "" }, validScheduled, "Class"},
		{"unknown class", func(k *Kind) { k.Class = "cron" }, validScheduled, "Class"},
		{"no description", func(k *Kind) { k.Description = "" }, validScheduled, "Description"},
		{"timer without runbook", func(k *Kind) { k.Runbook = "" }, validEvent, "needs a Runbook"},
		{"money without runbook", func(k *Kind) { k.Class = ClassMoney }, validScheduled, "needs a Runbook"},
		{"money with runbook", func(k *Kind) { k.Class = ClassMoney; k.Runbook = "docs/runbooks/ledger-reconcile.md" }, validScheduled, ""},
		{"runbook not a runbook path", func(k *Kind) { k.Runbook = "https://wiki/x" }, validEvent, "Runbook must be"},
		{"scheduled without UniqueBy", func(k *Kind) { k.UniqueBy = PeriodNone }, validScheduled, "needs UniqueBy"},
		{"event with UniqueBy", func(k *Kind) { k.UniqueBy = PerDay }, validEvent, "has no UniqueBy"},
		{"bad cron", func(k *Kind) { k.Schedule = "every 5 minutes" }, validScheduled, "5 fields"},
		{"cron that never fires", func(k *Kind) { k.Schedule = "0 0 31 2 *" }, validScheduled, "never fires"},
		{"timeout above 60s", func(k *Kind) { k.Timeout = 61 * time.Second }, validScheduled, "only a Batched kind"},
		{"timeout above 60s batched", func(k *Kind) { k.Timeout = 30 * time.Minute; k.Batched = true }, validScheduled, ""},
		{"timeout exactly 60s", func(k *Kind) { k.Timeout = 60 * time.Second }, validScheduled, ""},
		{"no timeout", func(k *Kind) { k.Timeout = 0 }, validScheduled, "Timeout is required"},
		{"sub-second timeout", func(k *Kind) { k.Timeout = 1500 * time.Millisecond }, validScheduled, "whole seconds"},
		{"no attempts", func(k *Kind) { k.MaxAttempts = 0 }, validScheduled, "MaxAttempts"},
		{"too many attempts", func(k *Kind) { k.MaxAttempts = 26 }, validScheduled, "MaxAttempts"},
		{"bad queue", func(k *Kind) { k.Queue = "Bulk Queue" }, validScheduled, "Queue"},
		{"named queue", func(k *Kind) { k.Queue = "bulk" }, validScheduled, ""},
		{"negative concurrency", func(k *Kind) { k.Concurrency = -1 }, validScheduled, "Concurrency"},
		{"concurrency at the cap", func(k *Kind) { k.Concurrency = maxConcurrency }, validScheduled, ""},
		{"concurrency above the cap", func(k *Kind) { k.Concurrency = maxConcurrency + 1 }, validScheduled, "Concurrency"},
		{"no handler", func(k *Kind) { k.Handler = nil }, validScheduled, "Handler"},
		{"name not snake_case", func(k *Kind) { k.Name = "rollup.today" }, validScheduled, "snake_case"},
		{"name with capitals", func(k *Kind) { k.Name = "RollupToday" }, validScheduled, "snake_case"},
		{"name trailing underscore", func(k *Kind) { k.Name = "rollup_" }, validScheduled, "snake_case"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k := c.base()
			c.mod(&k)
			err := Validate(k)
			if c.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, c.want)
		})
	}
}

func TestDefinePanicsOnInvalidAndDuplicate(t *testing.T) {
	r := NewRegistry()
	Define(r, validScheduled())
	require.PanicsWithError(t, `jobs: kind "rollup_today" is defined twice`, func() { Define(r, validScheduled()) })
	bad := validEvent()
	bad.Runbook = ""
	require.Panics(t, func() { Define(r, bad) })
	_, ok := r.Lookup("orders_accept_by")
	require.False(t, ok, "an invalid kind is not added")
}

func TestRegistryLookupsAndLabels(t *testing.T) {
	r := NewRegistry()
	q := validScheduled()
	q.Name, q.Queue = "rollup_month", "bulk"
	Define(r, validEvent())
	Define(r, validScheduled())
	Define(r, q)

	require.Equal(t, []string{"orders_accept_by", "rollup_month", "rollup_today"}, r.Names())
	k, ok := r.Lookup("rollup_today")
	require.True(t, ok)
	require.Equal(t, ClassRollup, k.Class)
	s, ok := r.ScheduleOf("rollup_today")
	require.True(t, ok)
	require.Equal(t, "*/5 * * * *", s.String())
	_, ok = r.ScheduleOf("orders_accept_by")
	require.False(t, ok, "an event kind has no schedule")

	l := r.LabelValues()
	require.Equal(t, []string{"orders_accept_by", "rollup_month", "rollup_today"}, l.Kinds)
	require.Equal(t, []string{"timer", "money", "notify", "marketing", "ai", "rollup", "maintenance"}, l.Classes)
	require.Equal(t, []string{"bulk", "default"}, l.Queues)

	// Define hands the kind and queue values to W1-07's label allow-list, so
	// telemetry.LabelValue keeps them instead of folding them into "other".
	for _, n := range l.Kinds {
		require.Equal(t, n, telemetry.LabelValue("kind", n))
	}
	for _, q := range l.Queues {
		require.Equal(t, q, telemetry.LabelValue("queue", q))
	}
	require.Equal(t, telemetry.Other, telemetry.LabelValue("kind", "never_defined_kind"))
}

// TestLabelSetsMatchTelemetry keeps the fixed class and status sets equal to
// the ones W1-07's allow-list holds (telemetry.JobClasses, JobStatuses).
func TestLabelSetsMatchTelemetry(t *testing.T) {
	var classes, statuses []string
	for _, c := range Classes() {
		classes = append(classes, string(c))
	}
	for _, s := range Statuses() {
		statuses = append(statuses, string(s))
	}
	require.ElementsMatch(t, telemetry.JobClasses, classes)
	require.ElementsMatch(t, telemetry.JobStatuses, statuses)
	for _, s := range statuses {
		require.Equal(t, s, telemetry.LabelValue("status", s))
	}
}

func TestPeriodKeys(t *testing.T) {
	// 2026-10-23 20:05 UTC is 2026-10-24 01:35 IST.
	at := time.Date(2026, 10, 23, 20, 5, 0, 0, time.UTC)
	require.Equal(t, "2026-10-24T01:35", PerTick.Key(at))
	require.Equal(t, "2026-10-24T01", PerHour.Key(at))
	require.Equal(t, "2026-10-24", PerDay.Key(at))
	require.Equal(t, "2026-10", PerMonth.Key(at))
	require.Empty(t, PeriodNone.Key(at))

	start, err := PerDay.Start("2026-10-24")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 23, 18, 30, 0, 0, time.UTC), start, "IST midnight")
	_, err = PerDay.Start("2026-10")
	require.Error(t, err)
	_, err = PeriodNone.Start("x")
	require.Error(t, err)
}

func TestOutcome(t *testing.T) {
	ctx := context.Background()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	boom := context.DeadlineExceeded
	require.Equal(t, StatusSucceeded, outcome(ctx, nil, false, 1, 3))
	require.Equal(t, StatusFailed, outcome(ctx, boom, false, 1, 3))
	require.Equal(t, StatusDiscarded, outcome(ctx, boom, false, 3, 3))
	require.Equal(t, StatusCancelled, outcome(cancelled, boom, false, 1, 3))
	// A hard cancel at shutdown on the last attempt: River discards the job
	// (the middleware hands it an ordinary error), so the run is discarded.
	stopped, stop := context.WithCancelCause(ctx)
	stop(errors.New("river: stop and cancel"))
	require.Equal(t, StatusCancelled, outcome(stopped, context.Canceled, false, 2, 3), "River retries it")
	require.Equal(t, StatusDiscarded, outcome(stopped, context.Canceled, false, 3, 3))
	require.Equal(t, StatusDiscarded, outcome(cancelled, boom, false, 3, 3))
	// A person's JobCancel is cancelled on any attempt (River marks it cancelled).
	remote, cancelRemote := context.WithCancelCause(ctx)
	cancelRemote(rivertype.ErrJobCancelledRemotely)
	require.Equal(t, StatusCancelled, outcome(remote, context.Canceled, false, 3, 3))
	require.Equal(t, StatusYielded, outcome(ctx, ErrYielded, false, 1, 3))
	require.Equal(t, StatusYielded, outcome(ctx, nil, true, 1, 3), "a run that yielded ends yielded even if the handler swallowed ErrYielded")
}

package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
)

type holdArgs struct {
	HoldID string `json:"holdId"`
}

func TestFakeEventKind(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(ist(2026, 10, 23, 10, 0))
	r := NewRegistry()
	var seen []string
	k := validEvent()
	k.Handler = func(_ context.Context, run Run) (Result, error) {
		var a holdArgs
		if err := run.Decode(&a); err != nil {
			return Result{}, err
		}
		seen = append(seen, a.HoldID)
		require.Equal(t, TriggerEvent, run.Trigger)
		require.Empty(t, run.PeriodKey)
		return Result{Summary: map[string]int64{"expired": 1}}, nil
	}
	Define(r, k)
	Define(r, validScheduled())
	f := NewFake(r, clk)
	var enq Enqueuer = f

	require.NoError(t, enq.EnqueueTx(ctx, nil, "orders_accept_by", holdArgs{"h1"}, EnqueueOpts{ScheduledAt: clk.Now().Add(30 * time.Minute), UniqueKey: "h1"}))
	require.NoError(t, enq.EnqueueTx(ctx, nil, "orders_accept_by", holdArgs{"h1"}, EnqueueOpts{UniqueKey: "h1"}), "a duplicate unique key is a no-op")
	require.Len(t, f.Pending(), 1)
	require.ErrorIs(t, enq.EnqueueTx(ctx, nil, "nope", nil, EnqueueOpts{}), ErrUnknownKind)
	require.ErrorIs(t, enq.EnqueueTx(ctx, nil, "rollup_today", nil, EnqueueOpts{}), ErrNotEvent)

	require.Empty(t, f.RunDue(ctx), "not due before the clock reaches it")
	clk.Advance(31 * time.Minute)
	runs := f.RunDue(ctx)
	require.Len(t, runs, 1)
	require.Equal(t, StatusSucceeded, runs[0].Status)
	require.Equal(t, int64(1), runs[0].Result.Summary["expired"])
	require.Equal(t, []string{"h1"}, seen)
	require.Empty(t, f.Pending())
}

func TestFakeRetriesThenDiscards(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(ist(2026, 10, 23, 10, 0))
	r := NewRegistry()
	k := validEvent()
	k.MaxAttempts = 2
	k.Handler = func(context.Context, Run) (Result, error) { return Result{}, errors.New("provider down") }
	Define(r, k)
	f := NewFake(r, clk)
	require.NoError(t, f.EnqueueTx(ctx, nil, k.Name, holdArgs{"h2"}, EnqueueOpts{}))
	first := f.RunDue(ctx)
	require.Equal(t, StatusFailed, first[0].Status)
	require.Equal(t, 1, first[0].Run.Attempt)
	second := f.RunDue(ctx)
	require.Equal(t, StatusDiscarded, second[0].Status)
	require.Equal(t, 2, second[0].Run.Attempt)
	require.Empty(t, f.Pending())
}

func TestFakeScheduledUniqueBy(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(ist(2026, 10, 23, 10, 7))
	r := NewRegistry()
	k := validScheduled()
	k.Schedule, k.UniqueBy = "0 2 * * *", PerDay
	Define(r, k)
	f := NewFake(r, clk)

	run, err := f.RunScheduled(ctx, k.Name)
	require.NoError(t, err)
	require.Equal(t, StatusSucceeded, run.Status)
	require.Equal(t, "2026-10-23", run.Run.PeriodKey)
	require.Equal(t, ist(2026, 10, 23, 0, 0), run.Run.PeriodStart)
	require.Equal(t, ist(2026, 10, 23, 2, 0), run.Run.ScheduledAt)

	again, err := f.RunScheduled(ctx, k.Name)
	require.NoError(t, err)
	require.Equal(t, StatusSkipped, again.Status, "a period that succeeded runs at most once")

	clk.Advance(24 * time.Hour)
	next, err := f.RunScheduled(ctx, k.Name)
	require.NoError(t, err)
	require.Equal(t, StatusSucceeded, next.Status)
	require.Equal(t, "2026-10-24", next.Run.PeriodKey)
}

func TestFakeYieldWhileDraining(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(ist(2026, 10, 23, 10, 7))
	r := NewRegistry()
	checkpoint := 0
	k := validScheduled()
	k.Name, k.Batched, k.Timeout = "payouts_batch", true, 10*time.Minute
	var f *Fake
	k.Handler = func(ctx context.Context, run Run) (Result, error) {
		for checkpoint < 5 {
			checkpoint++ // one batch, committed
			if checkpoint == 2 {
				f.SetDraining(true)
			}
			if err := Yield(ctx); err != nil {
				return Result{Summary: map[string]int64{"batches": int64(checkpoint)}}, err
			}
		}
		return Result{Summary: map[string]int64{"batches": int64(checkpoint)}}, nil
	}
	Define(r, k)
	f = NewFake(r, clk)

	first, err := f.RunScheduled(ctx, k.Name)
	require.NoError(t, err)
	require.Equal(t, StatusYielded, first.Status)
	require.ErrorIs(t, first.Err, ErrYielded)
	pending := f.Pending()
	require.Len(t, pending, 1)
	require.Equal(t, TriggerContinuation, pending[0].Trigger)
	require.Equal(t, 1, pending[0].Seq)
	require.Equal(t, first.Run.PeriodKey, pending[0].PeriodKey)

	f.SetDraining(false)
	cont := f.RunDue(ctx)
	require.Len(t, cont, 1)
	require.Equal(t, StatusSucceeded, cont[0].Status)
	require.Equal(t, TriggerContinuation, cont[0].Run.Trigger)
	require.Equal(t, 1, cont[0].Run.Seq)
	require.Equal(t, 5, checkpoint)

	again, err := f.RunScheduled(ctx, k.Name)
	require.NoError(t, err)
	require.Equal(t, StatusSkipped, again.Status, "the continuation's success completes the period")
}

func TestYieldOutsideABatchedRunIsANoOp(t *testing.T) {
	require.NoError(t, Yield(context.Background()))
	ctx := context.Background()
	clk := clock.NewFake(ist(2026, 10, 23, 10, 7))
	r := NewRegistry()
	k := validScheduled()
	var f *Fake
	k.Handler = func(ctx context.Context, _ Run) (Result, error) {
		f.SetDraining(true)
		return Result{}, Yield(ctx) // not Batched: Yield never stops the run
	}
	Define(r, k)
	f = NewFake(r, clk)
	run, err := f.RunScheduled(ctx, k.Name)
	require.NoError(t, err)
	require.Equal(t, StatusSucceeded, run.Status)
}

func TestRunDecode(t *testing.T) {
	var a holdArgs
	require.Error(t, Run{Kind: "x"}.Decode(&a), "a scheduled run has no args")
	require.NoError(t, Run{Kind: "x"}.WithArgs([]byte(`{"holdId":"h9"}`)).Decode(&a))
	require.Equal(t, "h9", a.HoldID)
}

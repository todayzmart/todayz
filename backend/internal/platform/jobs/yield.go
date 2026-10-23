package jobs

import "context"

// yielder is what the runner (or the Fake) puts in a batched run's context.
type yielder interface {
	// yield enqueues the run's continuation when the worker drains and
	// reports whether it did.
	yield(ctx context.Context) (bool, error)
}

type yieldKey struct{}

func withYielder(ctx context.Context, y yielder) context.Context {
	return context.WithValue(ctx, yieldKey{}, y)
}

// Yield is called by a Batched kind between batches, after it committed the
// batch's checkpoint (D-98). Normally it returns nil and the handler goes on
// with the next batch. While the worker drains, it enqueues a continuation
// run (trigger continuation, unique key <period>#<n>) and returns
// ErrYielded; the handler returns at once (returning that error is fine) and
// the run ends yielded. The continuation resumes from the checkpoint.
//
// Outside a batched run (a non-batched kind, a test calling a handler
// directly) Yield always returns nil. A failure to enqueue the continuation
// is returned as an error: the handler returns it, the attempt fails, and
// River retries it from the checkpoint.
func Yield(ctx context.Context) error {
	y, ok := ctx.Value(yieldKey{}).(yielder)
	if !ok {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	yielded, err := y.yield(ctx)
	if err != nil {
		return err
	}
	if yielded {
		return ErrYielded
	}
	return nil
}

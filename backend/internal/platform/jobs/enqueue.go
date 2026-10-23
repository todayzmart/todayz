package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Enqueuer enqueues event-triggered kinds. Domain code takes an Enqueuer
// (the River-backed one in the api and worker, the Fake in tests) and
// enqueues inside the transaction that needs the job, so an order and its
// expiry job commit together.
type Enqueuer interface {
	// EnqueueTx enqueues one run of the event kind named kind, with args
	// (marshalled to JSON; it must hold no secrets, and Run.Decode reads it
	// back), inside tx. A rolled-back tx leaves no job.
	EnqueueTx(ctx context.Context, tx pgx.Tx, kind string, args any, o EnqueueOpts) error
}

// EnqueueOpts are optional settings for one enqueue.
type EnqueueOpts struct {
	// ScheduledAt delays the run until then (a timer's deadline). Zero
	// means now. It is read against the injected clock, so the test clock
	// moves it.
	ScheduledAt time.Time
	// UniqueKey, when set, makes the enqueue a no-op while a job of the
	// same kind and key is pending or running (for example the order id for
	// a per-order timer). At most 200 characters.
	UniqueKey string
}

// Errors from EnqueueTx.
var (
	ErrUnknownKind = errors.New("jobs: unknown kind")
	ErrNotEvent    = errors.New("jobs: kind is scheduled, not event-triggered")
)

const maxUniqueKeyLen = 200

func checkEnqueue(r *Registry, kind string, o EnqueueOpts) (Kind, error) {
	k, ok := r.Lookup(kind)
	if !ok {
		return Kind{}, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	if k.Scheduled() {
		return Kind{}, fmt.Errorf("%w: %q", ErrNotEvent, kind)
	}
	if len(o.UniqueKey) > maxUniqueKeyLen {
		return Kind{}, errors.New("jobs: UniqueKey is longer than 200 characters")
	}
	return k, nil
}

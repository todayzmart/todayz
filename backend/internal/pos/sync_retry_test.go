package pos

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/db"
)

// D-132: a sync's transaction runs again on a deadlock or serialization
// failure, at most syncAttempts times, and not on any other error.
func TestRetryTx(t *testing.T) {
	deadlock := &pgconn.PgError{Code: "40P01"}
	serial := &pgconn.PgError{Code: "40001"}
	inTx := func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
	run := func(errs ...error) (calls int, retries []int, err error) {
		err = retryTx(context.Background(), inTx, func(context.Context) error {
			calls++
			if calls <= len(errs) {
				return errs[calls-1]
			}
			return nil
		}, func(attempt int, err error) {
			require.True(t, db.IsRetryable(err))
			retries = append(retries, attempt)
		})
		return calls, retries, err
	}

	calls, retries, err := run(deadlock, deadlock)
	require.NoError(t, err, "two deadlocks, then it commits")
	require.Equal(t, 3, calls)
	require.Equal(t, []int{1, 2}, retries)

	calls, retries, err = run(deadlock, serial, deadlock, deadlock)
	require.ErrorIs(t, err, deadlock, "the third failure is returned")
	require.Equal(t, syncAttempts, calls, "stops after syncAttempts")
	require.Equal(t, []int{1, 2}, retries)

	other := errors.New("not retryable")
	calls, retries, err = run(other)
	require.ErrorIs(t, err, other)
	require.Equal(t, 1, calls)
	require.Empty(t, retries)

	// A context that ends during the backoff stops the retries.
	ctx, cancel := context.WithCancel(context.Background())
	calls = 0
	err = retryTx(ctx, inTx, func(context.Context) error {
		calls++
		return deadlock
	}, func(int, error) { cancel() })
	require.ErrorIs(t, err, deadlock)
	require.Equal(t, 1, calls)
}

func TestRetryBackoff(t *testing.T) {
	for range 100 {
		for attempt, lo := range map[int]time.Duration{1: 20 * time.Millisecond, 2: 40 * time.Millisecond} {
			d := retryBackoff(attempt)
			require.GreaterOrEqual(t, d, lo)
			require.Less(t, d, lo+20*time.Millisecond)
		}
	}
}

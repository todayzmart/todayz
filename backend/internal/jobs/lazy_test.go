package jobs

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// TestLazyEnqueuer: the client is built on the first call and reused after;
// without a pool there is none and no error (tests that only list kinds).
func TestLazyEnqueuer(t *testing.T) {
	// A pool connects lazily: nothing is dialled here.
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/none")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	clk := clock.NewFake(clock.Real{}.Now())

	get := lazyEnqueuer(platformjobs.NewRegistry(), Deps{Pool: pool, Clock: clk}, "test")
	first, err := get()
	require.NoError(t, err)
	require.NotNil(t, first)
	again, err := get()
	require.NoError(t, err)
	require.True(t, first == again, "one client, reused")

	none, err := lazyEnqueuer(platformjobs.NewRegistry(), Deps{Clock: clk}, "test")()
	require.NoError(t, err)
	require.Nil(t, none)
}

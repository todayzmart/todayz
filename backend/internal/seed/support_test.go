package seed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/testkit"
)

func TestSupportSeed(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	d := Deps{DB: db.New(tdb.Pool), Keys: testKeys(t)}
	require.NoError(t, Run(ctx, config.EnvTest, Dev, d))
	require.NoError(t, Run(ctx, config.EnvTest, Dev, d), "a second run is a no-op")

	count := func(sql string) int {
		t.Helper()
		var n int
		require.NoError(t, tdb.Pool.QueryRow(ctx, sql).Scan(&n))
		return n
	}
	require.Equal(t, len(seedTickets), count(`SELECT count(*) FROM support_tickets`))
	require.Equal(t, len(seedTickets), count(`SELECT count(*) FROM support_messages`))
	require.Equal(t, 2, count(`SELECT count(*) FROM support_tickets WHERE opener_type = 'business'`))
	// Every open ticket's due time is its opening plus its priority's setting.
	require.Zero(t, count(`SELECT count(*) FROM support_tickets t JOIN platform_settings s ON s.key = 'support_sla_' || t.priority || '_min'
		WHERE t.status = 'open' AND t.sla_due_at <> t.created_at + (s.value #>> '{}')::int * interval '1 minute'`))
	require.Equal(t, 1, count(`SELECT count(*) FROM support_tickets WHERE status = 'resolved' AND sla_due_at IS NULL AND resolved_at IS NOT NULL`))
}

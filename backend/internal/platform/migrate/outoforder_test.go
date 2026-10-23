package migrate_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/migrate"
	"github.com/todayz/backend/internal/testkit"
)

func sqlFile(table string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE " + table + " (id int);\n-- +goose Down\nDROP TABLE " + table + ";\n")}
}

// TestOutOfOrderBlocks is D-90: WP blocks (D-89) merge in any order, so a
// database that already holds W1-02's block must still take W1-01's file
// when it lands later. Without goose.WithAllowOutofOrder, Up refuses.
func TestOutOfOrderBlocks(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewEmptyDB(t)

	// First the higher block alone (W1-02), as if it merged first.
	later := fstest.MapFS{"migrations/10201_w1-02_orders.sql": sqlFile("w1_02_orders")}
	m, err := migrate.NewFS(db.OwnerPool, later, quiet())
	require.NoError(t, err)
	n, err := m.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n, "River (3) and 10201")
	require.NoError(t, m.Close())

	// Then the binary that also carries the lower block (W1-01).
	both := fstest.MapFS{
		"migrations/10101_w1-01_users.sql":  sqlFile("w1_01_users"),
		"migrations/10201_w1-02_orders.sql": later["migrations/10201_w1-02_orders.sql"],
	}
	m, err = migrate.NewFS(db.OwnerPool, both, quiet())
	require.NoError(t, err)
	defer func() { _ = m.Close() }()

	ok, err := m.AtHead(ctx)
	require.NoError(t, err)
	require.False(t, ok, "10101 is pending even though 10201 is applied")

	n, err = m.Up(ctx)
	require.NoError(t, err, "D-90: the lower block applies after the higher one")
	require.Equal(t, 1, n)
	require.Contains(t, tables(t, db), "w1_01_users")

	st, err := m.Status(ctx)
	require.NoError(t, err)
	require.Len(t, st, 3)
	for _, s := range st {
		require.True(t, s.Applied, s.Name)
	}
	ok, err = m.AtHead(ctx)
	require.NoError(t, err)
	require.True(t, ok)

	// Down unwinds in reverse order of application, not of version (goose v3.28:
	// down() walks the applied rows from the version table newest-first, so
	// 10101, applied last, goes first, then 10201, then River's 3). The end
	// state is the same either way: every table is gone.
	require.NoError(t, m.DownTo(ctx, 0))
	require.Empty(t, tables(t, db))
	st, err = m.Status(ctx)
	require.NoError(t, err)
	for _, s := range st {
		require.False(t, s.Applied, s.Name)
	}
}

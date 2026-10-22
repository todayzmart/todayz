package migrate_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	dbschema "github.com/todayz/backend/db"
	"github.com/todayz/backend/internal/platform/migrate"
)

// TestMigrationFileNames (D-89): every migration in db/migrations has the
// NNNNN_<wp-id>_<what>.sql shape and a number inside its WP's block, and no
// two share a number.
func TestMigrationFileNames(t *testing.T) {
	entries, err := fs.ReadDir(dbschema.Migrations, "migrations")
	require.NoError(t, err)
	seen := map[string]string{}
	n := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		n++
		require.NoError(t, migrate.CheckFileName(name))
		num := name[:5]
		require.Empty(t, seen[num], "%s and %s share number %s", seen[num], name, num)
		seen[num] = name
	}
	require.GreaterOrEqual(t, n, 2)
}

// TestCheckFileNameFires is the fixture proving the check catches a number
// outside the WP's block, a bad shape and River's version.
func TestCheckFileNameFires(t *testing.T) {
	good := []string{
		"00001_w0-03_baseline.sql",
		"00099_w0-07_later.sql",
		"10201_w1-02_users.sql",
		"10299_w1-02_last.sql",
		"30701_w3-07_offer_rules.sql",
		"91001_w9-10_x.sql",
	}
	for _, name := range good {
		require.NoError(t, migrate.CheckFileName(name), name)
	}
	bad := map[string]string{
		"00004_w1-02_users.sql":      "w1-02 numbers its files 10201–10299",
		"10301_w1-02_users.sql":      "w1-02 numbers its files 10201–10299",
		"10200_w1-02_users.sql":      "w1-02 numbers its files 10201–10299",
		"10201_w1-03_users.sql":      "w1-03 numbers its files 10301–10399",
		"00100_w0-03_x.sql":          "w0-03 numbers its files 00001–00099",
		"00003_w0-03_river.sql":      "River's Go migration",
		"10001_w1-00_x.sql":          "there is no WP w1-00",
		"0004_w1-02_users.sql":       "want NNNNN_<wp-id>_<what>.sql",
		"10201_W1-02_users.sql":      "want NNNNN_<wp-id>_<what>.sql",
		"10201_w1-02_Users.sql":      "want NNNNN_<wp-id>_<what>.sql",
		"10201_w1-02_.sql":           "want NNNNN_<wp-id>_<what>.sql",
		"10201_users.sql":            "want NNNNN_<wp-id>_<what>.sql",
		"10201_w1-02_users.down.sql": "want NNNNN_<wp-id>_<what>.sql",
	}
	for name, want := range bad {
		err := migrate.CheckFileName(name)
		require.Error(t, err, name)
		require.Contains(t, err.Error(), want, name)
	}
}

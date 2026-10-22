package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	dbschema "github.com/todayz/backend/db"
)

func defaults(t *testing.T) map[int64][]string {
	t.Helper()
	d, err := Defaults(dbschema.Migrations)
	require.NoError(t, err)
	return d
}

func TestDefaultsAreTheDistinctiveOnes(t *testing.T) {
	d := defaults(t)
	require.Equal(t, []string{"stock_stale_after_hours"}, d[72])
	require.Equal(t, []string{"search_w_text"}, d[4500])
	require.ElementsMatch(t, []string{"admin_adjust_dual_approval_above_paise", "ai_daily_cost_cap_paise"}, d[50000])
	for _, v := range []int64{0, 1, 3, 5, 7, 10, 12, 20, 24, 30, 60, 100, 200, 500, 1000} {
		require.NotContains(t, d, v, "%d is too common to flag", v)
	}
}

// TestFixture: the seeded violations in testdata/src are found, and only they.
func TestFixture(t *testing.T) {
	got, err := Lint(filepath.Join("testdata", "src"), defaults(t))
	require.NoError(t, err)
	var lines []string
	for _, f := range got {
		msg, _, _ := strings.Cut(f.Message, ";")
		lines = append(lines, fmt.Sprintf("%s:%d %s", f.Pos.Filename, f.Pos.Line, msg))
	}
	require.Equal(t, []string{
		"internal/orders/orders.go:8 literal 72 equals the default of stock_stale_after_hours",
		"internal/orders/orders.go:18 literal 50_000 equals the default of admin_adjust_dual_approval_above_paise, ai_daily_cost_cap_paise",
		"internal/orders/orders.go:22 literal 4500.0 equals the default of search_w_text",
		"internal/orders/orders.go:27 //settings:ok needs a reason",
		"internal/orders/orders.go:29 literal 0x7D0 equals the default of insight_wanted_nearby_radius_m, insight_pair_min_share_bp",
	}, lines)
}

// TestBackendIsClean: the real tree passes (make check runs the same).
func TestBackendIsClean(t *testing.T) {
	var out bytes.Buffer
	n, err := run(filepath.Join("..", "..", ".."), &out)
	require.NoError(t, err)
	require.Zero(t, n, out.String())
}

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImagesUsage(t *testing.T) {
	for _, args := range [][]string{
		{"images"},
		{"images", "scrape"},
		{"images", "import", "--limit", "0"},
		{"images", "import", "--limit", "20001"},
		{"images", "import", "extra"},
		{"images", "import", "--contact", ""},
	} {
		code, _, stderr := runCmd(args, map[string]string{"APP_ENV": "local"})
		require.Equal(t, 2, code, args)
		require.Contains(t, stderr, "usage: todayz", args)
	}
}

// The import calls Open Food Facts, so it refuses to run offline (make check
// runs with TODAYZ_NO_EGRESS).
func TestImagesImportRefusedWithoutEgress(t *testing.T) {
	e := appKeys()
	e["APP_ENV"] = "test"
	e["TODAYZ_NO_EGRESS"] = "1"
	code, _, stderr := runCmd([]string{"images", "import", "--dry-run"}, e)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "TODAYZ_NO_EGRESS")
}

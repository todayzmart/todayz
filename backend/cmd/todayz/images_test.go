package main

import (
	"os"
	"path/filepath"
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
		{"images", "types", "--picks", "builtin", "--again"},
		{"images", "types", "--picks", "builtin", "--limit", "5"},
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
	for _, args := range [][]string{
		{"images", "import", "--dry-run"},
		{"images", "types", "--dry-run"},
		{"images", "types", "--picks", "builtin"},
	} {
		code, _, stderr := runCmd(args, e)
		require.Equal(t, 1, code, args)
		require.Contains(t, stderr, "TODAYZ_NO_EGRESS", args)
	}
}

// A pick list that is not one fails before anything is called.
func TestImagesTypesPicksFile(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "picks.csv")
	require.NoError(t, os.WriteFile(bad, []byte("kind,file\ntoor dal,Toor dal.jpg\n"), 0o600))
	for _, name := range []string{bad, filepath.Join(t.TempDir(), "missing.csv")} {
		code, _, _ := runCmd([]string{"images", "types", "--picks", name}, map[string]string{"APP_ENV": "local"})
		require.Equal(t, 1, code, name)
	}
}

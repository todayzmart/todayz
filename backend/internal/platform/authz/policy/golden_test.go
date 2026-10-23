package policy

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
)

var update = flag.Bool("update", false, "rewrite testdata/<domain>.golden from the policy files")

// TestGolden enumerates every (role × action) pair of every table and
// compares it with the reviewed golden file (AU-14). A change to a policy
// file shows up as a diff of the golden file in review.
func TestGolden(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("no policy tables registered")
	}
	for _, tbl := range all {
		t.Run(tbl.Domain, func(t *testing.T) {
			got := authz.Golden(tbl)
			path := filepath.Join("testdata", tbl.Domain+".golden")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil { //nolint:gosec // test fixture
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path) //nolint:gosec // fixed test path
			if err != nil {
				t.Fatalf("%v (run with -update and review the file)", err)
			}
			if string(want) != got {
				t.Errorf("%s is stale; run `go test ./internal/platform/authz/policy -update` and review the diff\n--- want\n%s\n--- got\n%s", path, want, got)
			}
		})
	}
	// Every golden file belongs to a registered table (no orphan after a rename).
	files, _ := filepath.Glob(filepath.Join("testdata", "*.golden"))
	known := map[string]bool{}
	for _, tbl := range all {
		known[filepath.Join("testdata", tbl.Domain+".golden")] = true
	}
	for _, f := range files {
		if !known[f] {
			t.Errorf("%s has no registered table", f)
		}
	}
}

// TestTablesBuildAnEngine proves the registered tables are valid together
// (names unique across domains, grants on the right roles).
func TestTablesBuildAnEngine(t *testing.T) {
	if _, err := authz.New(All(), authz.Options{Clock: clock.Real{}}); err != nil {
		t.Fatal(err)
	}
}

package jobs

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// reRegistration matches every way to give River a worker or a periodic job
// other than jobs.Define (D-95, SC-JB-01).
var reRegistration = regexp.MustCompile(
	`\briver\.(AddWorker|AddWorkerSafely|AddWorkerArgs|AddWorkerArgsSafely|NewPeriodicJob|NewWorkers)\b` +
		`|\bPeriodicJobs\s*[:=]` +
		`|\bqueue\.(Add|AddArgs)\b` +
		`|\.Every\(`)

// registrationAllowed are the only non-test files that may register workers:
// the queue wrapper itself and the jobs runner.
var registrationAllowed = map[string]bool{
	"internal/platform/queue/queue.go": true,
	"internal/platform/jobs/runner.go": true,
}

// forbiddenRegistrations lists file:line of every registration outside the
// allowed files, under root (the backend module), ignoring tests, gen/ and
// tools/.
func forbiddenRegistrations(t *testing.T, root string) []string {
	t.Helper()
	var bad []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "gen" || rel == "tools" || rel == "node_modules" || strings.HasPrefix(d.Name(), ".") && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || registrationAllowed[rel] {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if reRegistration.MatchString(code) {
				bad = append(bad, rel+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	require.NoError(t, err)
	return bad
}

// TestWorkersOnlyThroughDefine is SC-JB-01's first half: a River worker or
// periodic job registered anywhere but through jobs.Define fails.
func TestWorkersOnlyThroughDefine(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "go.mod"))
	require.NoError(t, err, "the backend module root")
	require.Empty(t, forbiddenRegistrations(t, root),
		"declare job kinds with jobs.Define(r, jobs.Kind{...}) in internal/jobs/<domain>.go (D-95)")
}

// TestRegistrationLintCatches proves the lint finds each forbidden form.
func TestRegistrationLintCatches(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(src), 0o600))
	}
	write("internal/orders/jobs.go", "package orders\nfunc f() { river.AddWorker(ws, w) }\n")
	write("internal/pos/jobs.go", "package pos\nfunc f() { queue.Add[x](r, w) }\n")
	write("internal/offers/jobs.go", "package offers\nvar p = river.NewPeriodicJob(s, c, nil)\n")
	write("internal/jobs/admin.go", "package jobs\nfunc f() { r.Every(time.Hour, nil) }\n")
	write("internal/notify/ok.go", "package notify\n// river.AddWorker in a comment is fine\n")
	write("internal/notify/x_test.go", "package notify\nfunc f() { river.AddWorker(ws, w) }\n")
	write("internal/platform/queue/queue.go", "package queue\nfunc f() { river.AddWorker(ws, w) }\n")
	bad := forbiddenRegistrations(t, dir)
	require.Len(t, bad, 4, "%v", bad)
}

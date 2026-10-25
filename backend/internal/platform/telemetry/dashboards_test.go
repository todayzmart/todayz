package telemetry

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	metricName    = regexp.MustCompile(`\btz_[a-z0-9_]+`)
	metricLiteral = regexp.MustCompile(`"(tz_[a-z0-9_]+)"`)
	// metricPrefixed is a name built as telemetry.MetricPrefix + "job_runs_total".
	metricPrefixed = regexp.MustCompile(`MetricPrefix \+ "([a-z0-9_]+)"`)
)

// TestDashboardsUseRegisteredMetrics keeps infra/grafana/dashboards honest: every
// tz_ metric a dashboard queries must be registered somewhere in this package
// (a typo or a renamed metric would otherwise show an empty panel for ever).
func TestDashboardsUseRegisteredMetrics(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "infra", "grafana", "dashboards")
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) == 0 {
		t.Skip("infra/grafana/dashboards not present (backend built on its own)")
	}

	// This package registers the platform metrics; the jobs platform (W1-11)
	// registers tz_job_* in platform/jobs and tz_job_hard_cancelled_total in
	// platform/queue, both as MetricPrefix + "…". The AI platform (W3-10)
	// registers tz_ai_* in ai/client, ai/guard and ai/drafts (dashboard 14).
	known := map[string]bool{}
	var srcs []string
	ai := filepath.Join("..", "..", "ai")
	for _, dir := range []string{".", filepath.Join("..", "jobs"), filepath.Join("..", "queue"),
		filepath.Join(ai, "client"), filepath.Join(ai, "guard"), filepath.Join(ai, "drafts")} {
		fs, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, fs...)
	}
	for _, f := range srcs {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range metricLiteral.FindAllSubmatch(b, -1) {
			known[string(m[1])] = true
		}
		for _, m := range metricPrefixed.FindAllSubmatch(b, -1) {
			known[MetricPrefix+string(m[1])] = true
		}
	}
	for _, name := range []string{"tz_job_runs_total", "tz_job_hard_cancelled_total"} {
		if !known[name] {
			t.Fatalf("the scan did not find %s in platform/jobs or platform/queue; it is broken", name)
		}
	}
	if len(known) < 10 {
		t.Fatalf("found only %d registered metrics; the scan is broken", len(known))
	}

	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range metricName.FindAllString(string(b), -1) {
			if !known[name] {
				t.Errorf("%s: metric %q is not registered by the telemetry package", filepath.Base(f), name)
			}
		}
	}
}

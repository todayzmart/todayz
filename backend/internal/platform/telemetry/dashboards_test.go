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

	known := map[string]bool{}
	srcs, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
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

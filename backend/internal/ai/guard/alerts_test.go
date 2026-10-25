package guard

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/todayz/backend/internal/testkit"
)

// The 80 % and 100 % cost alerts (infra/grafana/alerts/ai.yaml) and dashboard 14
// are W3-10's. internal/rollups lints W3-07's files the same way and skips these.

func repoRoot() string { return filepath.Join("..", "..", "..", "..") }

type aiRule struct {
	UID       string `yaml:"uid"`
	Condition string `yaml:"condition"`
	Data      []struct {
		RefID         string         `yaml:"refId"`
		DatasourceUID string         `yaml:"datasourceUid"`
		Model         map[string]any `yaml:"model"`
	} `yaml:"data"`
	NoDataState string            `yaml:"noDataState"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
	IsPaused    bool              `yaml:"isPaused"`
}

func loadAIRules(t *testing.T) []aiRule {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(), "infra", "grafana", "alerts", "ai.yaml"))
	if os.IsNotExist(err) {
		t.Skip("infra/grafana/alerts not present (backend built on its own)")
	}
	require.NoError(t, err)
	var f struct {
		Groups []struct {
			Folder   string   `yaml:"folder"`
			Interval string   `yaml:"interval"`
			Rules    []aiRule `yaml:"rules"`
		} `yaml:"groups"`
	}
	require.NoError(t, yaml.Unmarshal(b, &f))
	require.Len(t, f.Groups, 1)
	assert.Equal(t, "TodayZ", f.Groups[0].Folder)
	assert.Equal(t, "1m", f.Groups[0].Interval)
	return f.Groups[0].Rules
}

var (
	writeSQL    = regexp.MustCompile(`(?i)\b(insert|update|delete|truncate|drop|alter|grant|create)\b\s`)
	personSQL   = regexp.MustCompile(`(?i)\b(phone|phone_enc|to_enc|address|otp_hash|code_hash|full_name|actor_user_id|confirmed_by)\b`)
	nowCall     = regexp.MustCompile(`\bnow\(\)`)
	timeFilter  = regexp.MustCompile(`\$__timeFilter\(([^()]*(?:\([^()]*\))?[^()]*)\)`)
	promMetric  = regexp.MustCompile(`\btz_ai_[a-z0-9_]+`)
	goMetricDef = regexp.MustCompile(`"(tz_ai_[a-z0-9_]+)"`)
)

// TestAIAlertsLint: two rules on Postgres with a threshold condition, the
// labels and annotations every TodayZ rule carries, a runbook and dashboard
// that exist, and SQL that only reads and selects no person.
func TestAIAlertsLint(t *testing.T) {
	rules := loadAIRules(t)
	want := map[string]string{"tz-ai-cost-80": "warning", "tz-ai-cost-cap-reached": "critical"}
	require.Len(t, rules, len(want))
	for _, r := range rules {
		sev, ok := want[r.UID]
		require.True(t, ok, r.UID)
		assert.Equal(t, map[string]string{"area": "ai", "severity": sev}, r.Labels, r.UID)
		assert.Equal(t, "OK", r.NoDataState, r.UID)
		assert.False(t, r.IsPaused, r.UID)
		for _, k := range []string{"summary", "description", "runbook_url", "__dashboardUid__"} {
			assert.NotEmpty(t, r.Annotations[k], "%s %s", r.UID, k)
		}
		assert.FileExists(t, filepath.Join(repoRoot(), r.Annotations["runbook_url"]))
		assert.Equal(t, "todayz-14", r.Annotations["__dashboardUid__"], r.UID)
		require.Len(t, r.Data, 2, r.UID)
		assert.Equal(t, "todayz-postgres", r.Data[0].DatasourceUID, r.UID)
		assert.Equal(t, "__expr__", r.Data[1].DatasourceUID, r.UID)
		assert.Equal(t, "B", r.Condition, r.UID)
		assert.Equal(t, "threshold", r.Data[1].Model["type"], r.UID)
		sql := r.Data[0].Model["rawSql"].(string)
		assert.False(t, writeSQL.MatchString(sql) || personSQL.MatchString(sql), r.UID)
	}
}

// TestAIAlertsFire (SC-AI-04): with a ₹10 cap, nothing fires at 79 %, the
// warning fires at 80 % and both at 100 %; a cap of 0 (the kill switch) is a
// choice and fires neither. now() is pinned to 10:00 IST.
func TestAIAlertsFire(t *testing.T) {
	rules := loadAIRules(t)
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := tdb.OwnerPool.Exec(ctx, sql, args...)
		require.NoError(t, err)
	}
	spend := func(paise int) {
		exec(`INSERT INTO ai_usage_daily (day, scope, subject, calls, cost_paise, updated_at)
			VALUES ('2026-10-25', 'platform', '00000000-0000-0000-0000-000000000000', 1, $1, $2)
			ON CONFLICT (day, scope, subject) DO UPDATE SET cost_paise = EXCLUDED.cost_paise`, paise, morning)
	}
	firing := func() []string {
		t.Helper()
		var out []string
		for _, r := range rules {
			sql := nowCall.ReplaceAllString(r.Data[0].Model["rawSql"].(string), "$$1::timestamptz")
			var v float64
			require.NoError(t, tdb.Pool.QueryRow(ctx, sql, morning).Scan(&v), r.UID)
			ev := r.Data[1].Model["conditions"].([]any)[0].(map[string]any)["evaluator"].(map[string]any)
			require.Equal(t, "gt", ev["type"], r.UID)
			if v > ev["params"].([]any)[0].(float64) {
				out = append(out, r.UID)
			}
		}
		return out
	}
	exec(`UPDATE platform_settings SET value = '1000' WHERE key = 'ai_daily_cost_cap_paise'`)
	assert.Empty(t, firing(), "no spend yet")
	spend(790)
	assert.Empty(t, firing(), "79 %")
	spend(800)
	assert.Equal(t, []string{"tz-ai-cost-80"}, firing(), "80 %")
	spend(1000)
	assert.Equal(t, []string{"tz-ai-cost-80", "tz-ai-cost-cap-reached"}, firing(), "100 %")
	exec(`UPDATE platform_settings SET value = '0' WHERE key = 'ai_daily_cost_cap_paise'`)
	assert.Empty(t, firing(), "cap 0 is the kill switch, not an overrun")
	// Yesterday's spend is not today's.
	exec(`UPDATE platform_settings SET value = '1000' WHERE key = 'ai_daily_cost_cap_paise'`)
	exec(`UPDATE ai_usage_daily SET day = '2026-10-24'`)
	assert.Empty(t, firing(), "a new IST day")
}

// TestDashboard14: the layout every TodayZ dashboard has, Prometheus panels
// only on metrics the AI code registers, and Postgres panels that run on a
// migrated database, only read and select no person.
func TestDashboard14(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(), "infra", "grafana", "dashboards", "14-ai-quality-cost.json"))
	if os.IsNotExist(err) {
		t.Skip("infra/grafana/dashboards not present (backend built on its own)")
	}
	require.NoError(t, err)
	var d struct {
		UID      string `json:"uid"`
		Title    string `json:"title"`
		Timezone string `json:"timezone"`
		Editable bool   `json:"editable"`
		Panels   []struct {
			ID         int    `json:"id"`
			Type       string `json:"type"`
			Title      string `json:"title"`
			Datasource *struct {
				UID string `json:"uid"`
			} `json:"datasource"`
			Targets []struct {
				Expr   string `json:"expr"`
				RawSQL string `json:"rawSql"`
			} `json:"targets"`
		} `json:"panels"`
	}
	require.NoError(t, json.Unmarshal(b, &d))
	assert.Equal(t, "todayz-14", d.UID)
	assert.True(t, strings.HasPrefix(d.Title, "14 · "), d.Title)
	assert.Equal(t, "Asia/Kolkata", d.Timezone)
	assert.False(t, d.Editable)
	require.NotEmpty(t, d.Panels)
	assert.Equal(t, "text", d.Panels[0].Type, "the first panel says how to read the dashboard")

	known := map[string]bool{}
	for _, dir := range []string{"../client", "../drafts", "."} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			require.NoError(t, err)
			for _, m := range goMetricDef.FindAllSubmatch(src, -1) {
				known[string(m[1])] = true
			}
		}
	}
	require.True(t, known["tz_ai_calls_total"] && known["tz_ai_capped_total"], "the metric scan is broken")

	tdb := testkit.NewDB(t)
	ids := map[int]bool{}
	for _, p := range d.Panels {
		assert.False(t, ids[p.ID], "panel id %d twice", p.ID)
		ids[p.ID] = true
		if p.Datasource == nil {
			continue
		}
		for _, tg := range p.Targets {
			switch p.Datasource.UID {
			case "prometheus":
				ms := promMetric.FindAllString(tg.Expr, -1)
				assert.NotEmpty(t, ms, p.Title)
				for _, m := range ms {
					assert.True(t, known[m], "%s: %s is not registered", p.Title, m)
				}
			case "todayz-postgres":
				assert.False(t, writeSQL.MatchString(tg.RawSQL) || personSQL.MatchString(tg.RawSQL), p.Title)
				sql := timeFilter.ReplaceAllString(tg.RawSQL, "$1 > now() - interval '7 days'")
				rows, err := tdb.Pool.Query(context.Background(), sql)
				if assert.NoError(t, err, "%s\n%s", p.Title, sql) {
					rows.Close()
					assert.NoError(t, rows.Err(), p.Title)
				}
			default:
				t.Errorf("%s: datasource %s is not provisioned", p.Title, p.Datasource.UID)
			}
		}
	}
}

package rollups

import (
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.yaml.in/yaml/v3"

	"github.com/todayz/backend/internal/platform/ids"
)

// W3-07's alert files (05 §5). jobs.yaml is W1-11's, orders.yaml and pos.yaml
// are W2's and have their own checks.
var otherAlertFiles = map[string]bool{"jobs.yaml": true, "orders.yaml": true, "pos.yaml": true}

type alertFile struct {
	file   string
	Groups []struct {
		Name     string      `yaml:"name"`
		Folder   string      `yaml:"folder"`
		Interval string      `yaml:"interval"`
		Rules    []alertRule `yaml:"rules"`
	} `yaml:"groups"`
}

type alertRule struct {
	UID       string `yaml:"uid"`
	Title     string `yaml:"title"`
	Condition string `yaml:"condition"`
	Data      []struct {
		RefID         string         `yaml:"refId"`
		DatasourceUID string         `yaml:"datasourceUid"`
		Model         map[string]any `yaml:"model"`
	} `yaml:"data"`
	NoDataState string            `yaml:"noDataState"`
	For         string            `yaml:"for"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
	IsPaused    bool              `yaml:"isPaused"`
}

func repoRoot() string { return filepath.Join("..", "..", "..") }

func loadAlerts(t *testing.T) (own []alertFile, all []alertFile) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(repoRoot(), "infra", "grafana", "alerts", "*.yaml"))
	if len(files) == 0 {
		t.Skip("infra/grafana/alerts not present (backend built on its own)")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var a alertFile
		if err := yaml.Unmarshal(b, &a); err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
		a.file = filepath.Base(f)
		all = append(all, a)
		if !otherAlertFiles[a.file] {
			own = append(own, a)
		}
	}
	if len(own) != 11 {
		t.Fatalf("found %d of W3-07's 11 alert files", len(own))
	}
	return own, all
}

var (
	alertSeverities = map[string]bool{"critical": true, "warning": true, "info": true, "ops": true}
	alertDatasource = map[string]bool{"prometheus": true, "loki": true, "todayz-postgres": true, "__expr__": true}
	metricRef       = regexp.MustCompile(`\btz_[a-z0-9_]+`)
	metricLiteral   = regexp.MustCompile(`"(tz_[a-z0-9_]+)"`)
	metricPrefixed  = regexp.MustCompile(`MetricPrefix \+ "([a-z0-9_]+)"`)
)

// registeredMetrics scans the backend's Go code for the tz_ names it
// registers, written either as a literal or as MetricPrefix + "…".
func registeredMetrics(t *testing.T) map[string]bool {
	t.Helper()
	known := map[string]bool{}
	err := filepath.WalkDir(filepath.Join("..", ".."), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range metricLiteral.FindAllSubmatch(b, -1) {
			known[string(m[1])] = true
		}
		for _, m := range metricPrefixed.FindAllSubmatch(b, -1) {
			known["tz_"+string(m[1])] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tz_http_server_requests_total", "tz_job_lateness_seconds", "tz_ledger_reconcile_drift"} {
		if !known[name] {
			t.Fatalf("the scan did not find %s; it is broken", name)
		}
	}
	return known
}

// TestAlertRulesLint checks every W3-07 rule: a unique uid, area and severity
// labels, a runbook that exists, a dashboard that exists, only provisioned
// datasources, a threshold as the condition, tz_ metrics the code registers,
// and read-only SQL that never selects a person.
func TestAlertRulesLint(t *testing.T) {
	own, all := loadAlerts(t)
	uids := map[string]string{}
	for _, a := range all {
		for _, g := range a.Groups {
			for _, r := range g.Rules {
				if prev, ok := uids[r.UID]; ok {
					t.Errorf("rule uid %s is in %s and %s", r.UID, prev, a.file)
				}
				uids[r.UID] = a.file
			}
		}
	}
	dashUIDs := map[string]bool{}
	dashFiles, _ := filepath.Glob(filepath.Join(repoRoot(), "infra", "grafana", "dashboards", "*.json"))
	for _, f := range dashFiles {
		var d struct{ UID string }
		if b, err := os.ReadFile(f); err == nil && json.Unmarshal(b, &d) == nil {
			dashUIDs[d.UID] = true
		}
	}
	known := registeredMetrics(t)
	rules := 0
	for _, a := range own {
		if len(a.Groups) != 1 || a.Groups[0].Folder != "TodayZ" || a.Groups[0].Interval != "1m" {
			t.Errorf("%s: want one group in folder TodayZ at interval 1m", a.file)
		}
		for _, g := range a.Groups {
			for _, r := range g.Rules {
				rules++
				where := a.file + ": " + r.UID
				if !strings.HasPrefix(r.UID, "tz-") || r.Title == "" || r.IsPaused || r.NoDataState != "OK" {
					t.Errorf("%s: want a tz- uid, a title, not paused, noDataState OK", where)
				}
				if _, err := time.ParseDuration(r.For); err != nil {
					t.Errorf("%s: for %q: %v", where, r.For, err)
				}
				if r.Labels["area"] == "" || !alertSeverities[r.Labels["severity"]] {
					t.Errorf("%s: labels %v need an area and a severity in %v", where, r.Labels, alertSeverities)
				}
				for _, k := range []string{"summary", "description", "runbook_url", "__dashboardUid__"} {
					if r.Annotations[k] == "" {
						t.Errorf("%s: no %s annotation", where, k)
					}
				}
				rb := r.Annotations["runbook_url"]
				if !strings.HasPrefix(rb, "docs/runbooks/") {
					t.Errorf("%s: runbook %q is not under docs/runbooks", where, rb)
				} else if _, err := os.Stat(filepath.Join(repoRoot(), rb)); err != nil {
					t.Errorf("%s: runbook %s does not exist", where, rb)
				}
				if d := r.Annotations["__dashboardUid__"]; !dashUIDs[d] {
					t.Errorf("%s: dashboard %s does not exist", where, d)
				}
				if len(r.Data) < 2 || r.Data[len(r.Data)-1].RefID != r.Condition {
					t.Errorf("%s: the condition must be the last step", where)
					continue
				}
				if c := r.Data[len(r.Data)-1]; c.DatasourceUID != "__expr__" || c.Model["type"] != "threshold" {
					t.Errorf("%s: the condition must be a threshold expression", where)
				}
				for _, d := range r.Data {
					if !alertDatasource[d.DatasourceUID] {
						t.Errorf("%s %s: datasource %s is not provisioned", where, d.RefID, d.DatasourceUID)
					}
					if d.Model["refId"] != d.RefID {
						t.Errorf("%s %s: model refId %v differs", where, d.RefID, d.Model["refId"])
					}
					expr, _ := d.Model["expr"].(string)
					sql, _ := d.Model["rawSql"].(string)
					switch d.DatasourceUID {
					case "prometheus", "loki":
						if expr == "" || !balanced(expr) {
							t.Errorf("%s %s: empty or unbalanced expression %q", where, d.RefID, expr)
						}
					case "todayz-postgres":
						if sql == "" || forbiddenSQL.MatchString(sql) || personalSQL.MatchString(sql) {
							t.Errorf("%s %s: SQL must be present, read only and select no person", where, d.RefID)
						}
					}
					if d.DatasourceUID == "prometheus" {
						for _, m := range metricRef.FindAllString(expr, -1) {
							if !known[m] {
								t.Errorf("%s %s: metric %s is not registered in the code", where, d.RefID, m)
							}
						}
					}
				}
			}
		}
	}
	if rules != 24 {
		t.Errorf("found %d W3-07 rules, want the 24 of 05 §5", rules)
	}
}

// balanced reports whether (), [] and {} pair up outside quotes.
func balanced(s string) bool {
	var stack []rune
	pair := map[rune]rune{')': '(', ']': '[', '}': '{'}
	inQuote := false
	for i, c := range s {
		switch {
		case c == '"' && (i == 0 || s[i-1] != '\\'):
			inQuote = !inQuote
		case inQuote:
		case c == '(' || c == '[' || c == '{':
			stack = append(stack, c)
		case pair[c] != 0:
			if len(stack) == 0 || stack[len(stack)-1] != pair[c] {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack) == 0 && !inQuote
}

// alertNow is the evaluation time the firing test pins now() to: a weekday
// noon in IST, inside the business-hours windows.
var alertNow = testNow

// alertFixtures make each Postgres rule fire. They run in the rule's
// transaction, with $1 the pinned now, and are rolled back after it.
func alertFixtures(f fixture) map[string]func(e *env, tx pgx.Tx) {
	return map[string]func(e *env, tx pgx.Tx){
		"tz-db-missing-partition": func(e *env, tx pgx.Tx) {
			e.txExec(tx, `INSERT INTO partition_policies (parent, granularity, ahead) VALUES ('alert_test_missing', 'month', 3)`)
		},
		"tz-auth-otp-spike":       func(e *env, tx pgx.Tx) { e.otps(tx, 25, false) },
		"tz-auth-otp-one-address": func(e *env, tx pgx.Tx) { e.otps(tx, 51, true) },
		"tz-auth-refresh-reuse": func(e *env, tx pgx.Tx) {
			e.txExec(tx, `INSERT INTO sessions (id, user_id, app, family_id, refresh_hash, expires_at, revoked_at, revoked_reason, last_used_at, created_at)
				SELECT gen_random_uuid(), $2, 'customer', gen_random_uuid(), sha256(n::text::bytea), $1::timestamptz + interval '30 days',
				       $1::timestamptz - interval '2 minutes', 'reuse', $1::timestamptz - interval '1 hour', $1::timestamptz - interval '1 day'
				FROM generate_series(1, 6) n`, alertNow, ids.UUID(f.c1))
		},
		"tz-store-not-accepting": func(e *env, tx pgx.Tx) {
			for i := 0; i < 3; i++ {
				placed := alertNow.Add(-time.Duration(30+i) * time.Minute)
				id := ids.New(placed)
				e.txExec(tx, `INSERT INTO orders (id, customer_id, store_id, fulfilment, subtotal_paise,
					  items_after_offers_paise, item_subtotal_paise, total_paise, code_enc, code_hmac, placed_at, created_at, updated_at)
					VALUES ($2, $3, $4, 'pickup', 100, 100, 100, 100, '\x00', sha256(($2::uuid)::text::bytea), $1, $1, $1)`,
					placed, ids.UUID(id), ids.UUID(f.c2), ids.UUID(f.storeA))
				e.txExec(tx, `UPDATE orders SET status = 'expired', status_reason = 'not_accepted' WHERE id = $1`, ids.UUID(id))
			}
		},
		"tz-search-zero-result-spike": func(e *env, tx pgx.Tx) {
			e.txExec(tx, `INSERT INTO search_log (id, created_at, query_norm, results_count, store_count, radius_m, geo_cell)
				SELECT gen_random_uuid(), $1::timestamptz - n * interval '30 seconds', 'kokum', CASE WHEN n <= 25 THEN 0 ELSE 3 END, 0, 2000, 613196575014322175
				FROM generate_series(1, 60) n`, alertNow)
		},
		"tz-risk-store-strikes": func(e *env, tx pgx.Tx) {
			e.txExec(tx, `INSERT INTO risk_flags (id, subject_type, subject_id, rule, created_at, updated_at)
				VALUES (gen_random_uuid(), 'store', $2, 'store_strikes', $1::timestamptz - interval '5 minutes', $1)`, alertNow, ids.UUID(f.storeA))
		},
		"tz-risk-unconfirmed-returns": func(e *env, tx pgx.Tx) {
			e.txExec(tx, `INSERT INTO risk_flags (id, subject_type, subject_id, rule, created_at, updated_at)
				VALUES (gen_random_uuid(), 'order', gen_random_uuid(), 'return_unconfirmed', $1::timestamptz - interval '5 minutes', $1)`, alertNow)
		},
		"tz-pos-sync-lag": func(e *env, tx pgx.Tx) {
			e.txExec(tx, `INSERT INTO sync_cursors (device_id, store_id, last_synced_at)
				SELECT gen_random_uuid(), $2, $1::timestamptz - interval '2 hours' FROM generate_series(1, 5)`, alertNow, ids.UUID(f.storeA))
		},
	}
}

func (e *env) txExec(tx pgx.Tx, sql string, args ...any) {
	e.t.Helper()
	if _, err := tx.Exec(e.ctx, sql, args...); err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

// otps inserts n OTP requests over the last 9 minutes, from one hashed
// address or from n different ones.
func (e *env) otps(tx pgx.Tx, n int, oneAddress bool) {
	e.t.Helper()
	ip := `sha256(('198.51.100.' || g)::bytea)`
	if oneAddress {
		ip = `sha256('198.51.100.7'::bytea)`
	}
	e.txExec(tx, `INSERT INTO otp_requests (id, phone_hash, phone_enc, purpose, app, code_hash, expires_at, ip_hash, created_at)
		SELECT gen_random_uuid(), sha256(('p' || g)::bytea), '\x00', 'login', 'customer', sha256(('c' || g)::bytea),
		       $1::timestamptz + interval '5 minutes', `+ip+`, $1::timestamptz - g * interval '10 seconds'
		FROM generate_series(1, $2::int) g`, alertNow, n)
}

var sqlNow = regexp.MustCompile(`\bnow\(\)`)

// TestAlertRulesFire evaluates every Postgres alert query on a migrated
// database with the rollup fixture, now() pinned: each rule stays quiet on the
// clean data and fires once its fixture is added. A rule that also needs
// Prometheus or Loki only has to run here; promtool is not in the toolchain,
// so PromQL and LogQL get TestAlertRulesLint's static checks only.
func TestAlertRulesFire(t *testing.T) {
	own, _ := loadAlerts(t)
	e := newEnv(t)
	f := e.seed()
	fixtures := alertFixtures(f)
	evaluated := 0
	for _, a := range own {
		for _, r := range a.Groups[0].Rules {
			var pg []int
			for i, d := range r.Data {
				if d.DatasourceUID == "todayz-postgres" {
					pg = append(pg, i)
				}
			}
			if len(pg) == 0 {
				continue
			}
			cond := r.Data[len(r.Data)-1].Model
			pgOnly := len(pg) == len(r.Data)-1 && cond["expression"] == r.Data[pg[0]].RefID
			fixture, ok := fixtures[r.UID]
			if pgOnly && !ok {
				t.Errorf("%s: a Postgres rule without a firing fixture", r.UID)
				continue
			}
			t.Run(r.UID, func(t *testing.T) {
				tx, err := e.tdb.OwnerPool.Begin(e.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = tx.Rollback(e.ctx) }()
				sql := sqlNow.ReplaceAllString(r.Data[pg[0]].Model["rawSql"].(string), "$$1::timestamptz")
				if !pgOnly {
					// Part of a ratio or a union with another datasource:
					// it has to run and return a value.
					if vals := alertValues(t, e, tx, sql); len(vals) == 0 {
						t.Fatalf("returned no rows\n%s", sql)
					}
					return
				}
				evaluated++
				ev := cond["conditions"].([]any)[0].(map[string]any)["evaluator"].(map[string]any)
				op, limit := ev["type"].(string), toFloat(ev["params"].([]any)[0])
				fires := func() bool {
					for _, v := range alertValues(t, e, tx, sql) {
						if (op == "gt" && v > limit) || (op == "lt" && v < limit) {
							return true
						}
					}
					return false
				}
				if fires() {
					t.Fatalf("fires on the clean fixture\n%s", sql)
				}
				fixture(e, tx)
				if !fires() {
					t.Fatalf("does not fire with its fixture\n%s", sql)
				}
			})
		}
	}
	if evaluated != 9 {
		t.Errorf("evaluated %d Postgres-only rules, want 9", evaluated)
	}
}

// alertValues runs an alert query the way Grafana reads a table result: the
// one numeric column of each row is its value, string columns are labels.
func alertValues(t *testing.T, e *env, tx pgx.Tx, sql string) []float64 {
	t.Helper()
	rows, err := tx.Query(e.ctx, sql, alertNow)
	if err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, v := range vals {
			if _, isLabel := v.(string); isLabel {
				continue
			}
			out = append(out, toFloat(v))
			n++
		}
		if n != 1 {
			t.Fatalf("row %v has %d numeric columns, want 1\n%s", vals, n, sql)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
	return out
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case float64:
		return x
	case pgtype.Numeric:
		f, err := x.Float64Value()
		if err != nil || !f.Valid {
			return math.NaN()
		}
		return f.Float64
	}
	return math.NaN()
}

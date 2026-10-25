package rollups

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// W3-07's dashboards (05 §4): 00 and 05–13. 01–04 and 15 belong to W1-07 and
// W1-11 and have their own checks.
var ownDashboard = regexp.MustCompile(`^todayz-(00|0[5-9]|1[0-3])$`)

type dashboard struct {
	file       string
	UID        string                          `json:"uid"`
	Title      string                          `json:"title"`
	Tags       []string                        `json:"tags"`
	Timezone   string                          `json:"timezone"`
	Editable   bool                            `json:"editable"`
	Templating struct{ List []map[string]any } `json:"templating"`
	Panels     []panel                         `json:"panels"`
}

type panel struct {
	ID         int                      `json:"id"`
	Type       string                   `json:"type"`
	Title      string                   `json:"title"`
	Datasource *datasource              `json:"datasource"`
	GridPos    struct{ X, Y, W, H int } `json:"gridPos"`
	Targets    []target                 `json:"targets"`
	Options    map[string]any           `json:"options"`
}

type datasource struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

type target struct {
	RefID      string      `json:"refId"`
	Datasource *datasource `json:"datasource"`
	RawSQL     string      `json:"rawSql"`
	Expr       string      `json:"expr"`
}

// The datasource uids provisioned in infra/local/lgtm (and in Wave 5's cloud
// Grafana): Prometheus, Loki and the read-only app database.
var datasourceUIDs = map[string]string{
	"prometheus": "prometheus", "loki": "loki", "todayz-postgres": "grafana-postgresql-datasource",
}

func loadDashboards(t *testing.T) []dashboard {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "infra", "grafana", "dashboards")
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) == 0 {
		t.Skip("infra/grafana/dashboards not present (backend built on its own)")
	}
	var out []dashboard
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var d dashboard
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
		d.file = filepath.Base(f)
		if ownDashboard.MatchString(d.UID) {
			out = append(out, d)
		}
	}
	if len(out) != 10 {
		t.Fatalf("found %d of W3-07's 10 dashboards (00, 05–13)", len(out))
	}
	return out
}

// TestDashboardsLint checks the shape every W3-07 dashboard keeps: a uid and
// title matching its number, IST, read-only, a "How to read this" panel first,
// unique panel ids, a grid inside 24 columns and only provisioned datasources.
func TestDashboardsLint(t *testing.T) {
	for _, d := range loadDashboards(t) {
		num := strings.TrimPrefix(d.UID, "todayz-")
		if !strings.HasPrefix(d.file, num+"-") || !strings.HasPrefix(d.Title, num+" · ") {
			t.Errorf("%s: uid %s, title %q and file name disagree", d.file, d.UID, d.Title)
		}
		if d.Timezone != "Asia/Kolkata" || d.Editable || !contains(d.Tags, "todayz") {
			t.Errorf("%s: want timezone Asia/Kolkata, editable false, tag todayz", d.file)
		}
		if len(d.Panels) == 0 || d.Panels[0].Type != "text" || d.Panels[0].Title != "How to read this" {
			t.Errorf("%s: the first panel must be the \"How to read this\" text", d.file)
		}
		ids := map[int]bool{}
		for _, p := range d.Panels {
			where := d.file + ": panel " + p.Title
			if ids[p.ID] || p.ID == 0 {
				t.Errorf("%s: id %d is zero or repeated", where, p.ID)
			}
			ids[p.ID] = true
			g := p.GridPos
			if g.W <= 0 || g.H <= 0 || g.X < 0 || g.X+g.W > 24 {
				t.Errorf("%s: gridPos %+v is outside the 24-column grid", where, g)
			}
			if p.Type == "row" {
				continue
			}
			if p.Type == "text" {
				if s, _ := p.Options["content"].(string); s == "" {
					t.Errorf("%s: empty text panel", where)
				}
				continue
			}
			if p.Title == "" || len(p.Targets) == 0 {
				t.Errorf("%s: a data panel needs a title and a target", where)
			}
			checkDS(t, where, p.Datasource)
			for _, tg := range p.Targets {
				checkDS(t, where+" "+tg.RefID, tg.Datasource)
				if tg.Datasource != nil && p.Datasource != nil && tg.Datasource.UID != p.Datasource.UID {
					t.Errorf("%s %s: target datasource %s differs from the panel's %s", where, tg.RefID, tg.Datasource.UID, p.Datasource.UID)
				}
				if tg.RawSQL == "" && tg.Expr == "" {
					t.Errorf("%s %s: target has no query", where, tg.RefID)
				}
				if tg.RawSQL != "" && forbiddenSQL.MatchString(tg.RawSQL) {
					t.Errorf("%s %s: a dashboard query must only read", where, tg.RefID)
				}
				if tg.RawSQL != "" && personalSQL.MatchString(tg.RawSQL) {
					t.Errorf("%s %s: a dashboard never selects a phone, name of a person, address or OTP", where, tg.RefID)
				}
			}
		}
	}
}

var (
	forbiddenSQL = regexp.MustCompile(`(?i)\b(insert|update|delete|truncate|drop|alter|grant|create)\b\s`)
	// Columns that would put a person on a dashboard (05 §4: counts only).
	personalSQL = regexp.MustCompile(`(?i)\b(phone|phone_enc|to_enc|address|otp_hash|code_hash|full_name)\b`)
)

func checkDS(t *testing.T, where string, ds *datasource) {
	t.Helper()
	if ds == nil {
		t.Errorf("%s: no datasource", where)
		return
	}
	if want, ok := datasourceUIDs[ds.UID]; !ok || want != ds.Type {
		t.Errorf("%s: datasource %s/%s is not provisioned", where, ds.Type, ds.UID)
	}
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// Grafana's Postgres macros, expanded the way the datasource does for a
// 7-day range ending on the test clock.
var (
	macroTimeFilter = regexp.MustCompile(`\$__timeFilter\(([^)]+)\)`)
	macroGroupAlias = regexp.MustCompile(`\$__timeGroupAlias\(([^,]+),\s*'([0-9]+)([mhd])'\)`)
)

func expandMacros(sql string) string {
	const from, to = "'2026-10-15T06:30:00Z'", "'2026-10-22T06:30:00Z'"
	sql = macroTimeFilter.ReplaceAllString(sql, "$1 BETWEEN "+from+" AND "+to)
	sql = macroGroupAlias.ReplaceAllStringFunc(sql, func(m string) string {
		p := macroGroupAlias.FindStringSubmatch(m)
		unit := map[string]string{"m": "minute", "h": "hour", "d": "day"}[p[3]]
		return "date_trunc('" + unit + "', " + p[1] + ") AS time"
	})
	sql = strings.NewReplacer("$__timeFrom()", from, "$__timeTo()", to, "${city}", "__all").Replace(sql)
	return sql
}

// TestDashboardSQLRuns runs every Postgres query of dashboards 00 and 05–13,
// macros expanded, against a migrated database with the rollup fixture, in a
// read-only transaction as Grafana's role would. A renamed column or a typo
// fails here rather than as an empty panel.
func TestDashboardSQLRuns(t *testing.T) {
	ds := loadDashboards(t)
	e := newEnv(t)
	e.seed()
	if _, err := e.svc.Backfill(e.ctx, seedDay.Add(-1), seedDay); err != nil {
		t.Fatal(err)
	}
	queries := 0
	run := func(where, sql string) {
		queries++
		sql = expandMacros(sql)
		if strings.Contains(sql, "$__") || strings.Contains(sql, "${") {
			t.Errorf("%s: unexpanded macro in\n%s", where, sql)
			return
		}
		err := readOnly(e.ctx, e, func(tx pgx.Tx) error {
			rows, err := tx.Query(e.ctx, sql)
			if err != nil {
				return err
			}
			rows.Close()
			return rows.Err()
		})
		if err != nil && strings.Contains(err.Error(), "shared_preload_libraries") {
			// pg_stat_statements is created but not preloaded in the test
			// container; the statement still has to plan.
			err = readOnly(e.ctx, e, func(tx pgx.Tx) error {
				_, err := tx.Exec(e.ctx, "EXPLAIN "+sql)
				return err
			})
		}
		if err != nil {
			t.Errorf("%s: %v\n%s", where, err, sql)
		}
	}
	for _, d := range ds {
		for _, v := range d.Templating.List {
			if q, ok := v["query"].(string); ok && v["type"] == "query" {
				run(d.file+": variable "+v["name"].(string), q)
			}
		}
		for _, p := range d.Panels {
			for _, tg := range p.Targets {
				if tg.RawSQL != "" {
					run(d.file+": "+p.Title+" "+tg.RefID, tg.RawSQL)
				}
			}
		}
	}
	if queries < 100 {
		t.Fatalf("ran only %d dashboard queries; the walk is broken", queries)
	}
}

func readOnly(ctx context.Context, e *env, fn func(pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, e.tdb.OwnerPool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, fn)
}

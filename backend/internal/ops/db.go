package ops

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	api "github.com/todayz/backend/gen/api/admin-system"
	"github.com/todayz/backend/internal/platform/telemetry"
)

const (
	// maxStatementText is where a shown statement's text is cut (characters).
	maxStatementText = 500
	// readStatementText is how much of a statement is read before its
	// comments are taken out and it is cut to maxStatementText.
	readStatementText = 2000 //settings:ok characters read from pg_stat_statements, not a product rule
)

type dbSnapshot struct {
	at       time.Time
	database api.AdminSystemDatabase
	river    api.AdminSystemRiver
}

// dbPanels returns the database and River panels, read again once the last
// read is DBCacheTTL old.
func (s *Service) dbPanels(ctx context.Context) *dbSnapshot {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	now := s.d.Clock.Now() // under the lock, as in awsPanels
	if s.db != nil && fresh(now, s.db.at, DBCacheTTL) {
		return s.db
	}
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
	defer cancel()
	s.db = &dbSnapshot{at: now, database: s.readDatabase(c, now), river: s.readRiver(c, now)}
	return s.db
}

// readDatabase reads server connections, the database size and the top 10
// statements. multiAz is filled by System.
func (s *Service) readDatabase(ctx context.Context, now time.Time) api.AdminSystemDatabase {
	p := api.AdminSystemDatabase{
		Status:           api.AdminSystemPanelStatusOk,
		FetchedAt:        now,
		StatementsStatus: api.AdminSystemStatementsStatusError,
		Statements:       []api.AdminSystemStatement{},
	}
	if s.d.DB == nil {
		return failDatabase(p, errNoDB)
	}
	var conns, maxConns int
	var size int64
	err := s.d.DB.QueryRow(ctx, `SELECT
  (SELECT count(*) FROM pg_stat_activity WHERE backend_type = 'client backend')::int,
  current_setting('max_connections')::int,
  pg_database_size(current_database())`).Scan(&conns, &maxConns, &size)
	if err != nil {
		return failDatabase(p, err)
	}
	p.Connections, p.MaxConnections, p.SizeBytes = &conns, &maxConns, &size
	p.StatementsStatus, p.Statements = s.readStatements(ctx)
	return p
}

var errNoDB = errors.New("ops: no database")

func failDatabase(p api.AdminSystemDatabase, err error) api.AdminSystemDatabase {
	p.Status, p.ErrorClass = api.AdminSystemPanelStatusError, errClassPtr(err)
	return p
}

// statementsSQL lists the most expensive statements of this database from
// pg_stat_statements (in schema %s). Only data and DML statements are kept:
// their text is normalised (every literal is $n), while a utility statement
// (SET, CREATE, …) is stored as written and may hold a value. Statements the
// role may not see (another role's, without pg_read_all_stats) come back with
// a null queryid and no text; they compete for the top 10 too, so the caller
// can tell the list is incomplete.
const statementsSQL = `SELECT queryid, left(query, $1), calls, mean_exec_time, total_exec_time, rows
FROM %s.pg_stat_statements
WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
  AND toplevel
  AND (queryid IS NULL
    OR query ~* '^(\s*--[^\n]*\n)*\s*\(*\s*(select|insert|update|delete|with|merge|values)\M')
ORDER BY total_exec_time DESC
LIMIT 10`

// readStatements reads the top 10; an absent or not-preloaded extension, or a
// role that cannot see every statement, is a status, not a failure of the panel.
func (s *Service) readStatements(ctx context.Context) (api.AdminSystemStatementsStatus, []api.AdminSystemStatement) {
	none := []api.AdminSystemStatement{}
	var schema string
	err := s.d.DB.QueryRow(ctx, `SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace
WHERE e.extname = 'pg_stat_statements'`).Scan(&schema)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminSystemStatementsStatusNotInstalled, none
	}
	if err != nil {
		return api.AdminSystemStatementsStatusError, none
	}
	rows, err := s.d.DB.Query(ctx, sprintfSchema(statementsSQL, schema), readStatementText)
	if err != nil {
		return statementsErr(err), none
	}
	type row struct {
		queryID *int64
		st      api.AdminSystemStatement
	}
	got, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		var text *string
		err := r.Scan(&x.queryID, &text, &x.st.Calls, &x.st.MeanMs, &x.st.TotalMs, &x.st.Rows)
		if text != nil {
			x.st.Query = *text
		}
		return x, err
	})
	if err != nil {
		return statementsErr(err), none
	}
	status, out := api.AdminSystemStatementsStatusOk, none
	for _, x := range got {
		if x.queryID == nil {
			status = api.AdminSystemStatementsStatusNeedsPrivilege
			continue
		}
		if x.st.Query = shownStatement(x.st.Query); x.st.Query != "" {
			out = append(out, x.st)
		}
	}
	return status, out
}

func statementsErr(err error) api.AdminSystemStatementsStatus {
	var pe *pgconn.PgError
	// 55000 object_not_in_prerequisite_state: not in shared_preload_libraries.
	if errors.As(err, &pe) && pe.Code == "55000" {
		return api.AdminSystemStatementsStatusNotPreloaded
	}
	return api.AdminSystemStatementsStatusError
}

// reSqlcName is sqlc's leading query name line, the one comment kept.
var reSqlcName = regexp.MustCompile(`^-- name: [A-Za-z0-9_]+ :[a-z]+\n`)

// shownStatement takes the comments out of a statement (pg_stat_statements
// keeps them as written, and a tag such as /* user=… */ can carry a value),
// except a leading sqlc `-- name:` line, and cuts it to maxStatementText.
func shownStatement(q string) string {
	q = strings.TrimLeft(q, " \t\r\n")
	name := reSqlcName.FindString(q)
	q = strings.TrimSpace(stripComments(q[len(name):]))
	if q == "" {
		return ""
	}
	q = name + q
	if r := []rune(q); len(r) > maxStatementText {
		q = string(r[:maxStatementText])
	}
	return q
}

// stripComments replaces each -- and /* */ comment (nested, as Postgres
// allows) with a space. Quoted identifiers and strings are passed over. A
// comment left open (the text was cut) runs to the end.
func stripComments(q string) string {
	var b strings.Builder
	for i := 0; i < len(q); {
		switch {
		case q[i] == '\'' || q[i] == '"':
			j := i + 1
			for j < len(q) {
				if q[j] == q[i] {
					if j+1 < len(q) && q[j+1] == q[i] { // a doubled quote
						j += 2
						continue
					}
					break
				}
				j++
			}
			j = min(j+1, len(q))
			b.WriteString(q[i:j])
			i = j
		case strings.HasPrefix(q[i:], "--"):
			j := strings.IndexByte(q[i:], '\n')
			if j < 0 {
				return b.String()
			}
			b.WriteByte(' ')
			i += j
		case strings.HasPrefix(q[i:], "/*"):
			depth, j := 1, i+2
			for j < len(q) && depth > 0 {
				switch {
				case strings.HasPrefix(q[j:], "/*"):
					depth, j = depth+1, j+2
				case strings.HasPrefix(q[j:], "*/"):
					depth, j = depth-1, j+2
				default:
					j++
				}
			}
			if depth > 0 {
				return b.String()
			}
			b.WriteByte(' ')
			i = j
		default:
			b.WriteByte(q[i])
			i++
		}
	}
	return b.String()
}

func sprintfSchema(q, schema string) string {
	return strings.Replace(q, "%s", pgx.Identifier{schema}.Sanitize(), 1)
}

// readRiver reads the running count and each queue's due depth and oldest age.
func (s *Service) readRiver(ctx context.Context, now time.Time) api.AdminSystemRiver {
	p := api.AdminSystemRiver{Status: api.AdminSystemPanelStatusOk, FetchedAt: now, Queues: []api.AdminSystemQueue{}}
	if s.d.DB == nil {
		p.Status, p.ErrorClass = api.AdminSystemPanelStatusError, errClassPtr(errNoDB)
		return p
	}
	var running int64
	if err := s.d.DB.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE state = 'running'`).Scan(&running); err != nil {
		p.Status, p.ErrorClass = api.AdminSystemPanelStatusError, errClassPtr(err)
		return p
	}
	stats, err := telemetry.QueueStats(ctx, s.d.DB, now)
	if err != nil {
		p.Status, p.ErrorClass = api.AdminSystemPanelStatusError, errClassPtr(err)
		return p
	}
	p.Running = &running
	for _, q := range stats {
		p.Queues = append(p.Queues, api.AdminSystemQueue{Queue: q.Queue, Depth: q.Depth, OldestAgeSeconds: int(max(q.OldestAge, 0) / time.Second)})
	}
	sort.Slice(p.Queues, func(i, j int) bool {
		if p.Queues[i].Depth != p.Queues[j].Depth {
			return p.Queues[i].Depth > p.Queues[j].Depth
		}
		return p.Queues[i].Queue < p.Queues[j].Queue
	})
	return p
}

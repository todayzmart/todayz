package ops

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	api "github.com/todayz/backend/gen/api/admin-system"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// maxStatementText is where a statement's text is cut.
const maxStatementText = 500

type dbSnapshot struct {
	at       time.Time
	database api.AdminSystemDatabase
	river    api.AdminSystemRiver
}

// dbPanels returns the database and River panels, read again once the last
// read is DBCacheTTL old.
func (s *Service) dbPanels(ctx context.Context, now time.Time) *dbSnapshot {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	if s.db != nil && now.Sub(s.db.at) < DBCacheTTL && !now.Before(s.db.at) {
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
// (SET, CREATE, …) is stored as written and may hold a value. A leading
// `-- name:` comment (sqlc) is kept, since it names the query.
const statementsSQL = `SELECT left(query, $1), calls, mean_exec_time, total_exec_time, rows
FROM %s.pg_stat_statements
WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
  AND toplevel
  AND queryid IS NOT NULL
  AND query ~* '^(\s*--[^\n]*\n)*\s*\(*\s*(select|insert|update|delete|with|merge|values)\M'
ORDER BY total_exec_time DESC
LIMIT 10`

// readStatements reads the top 10; an absent or not-preloaded extension is a
// status, not a failure of the panel.
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
	rows, err := s.d.DB.Query(ctx, sprintfSchema(statementsSQL, schema), maxStatementText)
	if err != nil {
		var pe *pgconn.PgError
		// 55000 object_not_in_prerequisite_state: not in shared_preload_libraries.
		if errors.As(err, &pe) && pe.Code == "55000" {
			return api.AdminSystemStatementsStatusNotPreloaded, none
		}
		return api.AdminSystemStatementsStatusError, none
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.AdminSystemStatement, error) {
		var st api.AdminSystemStatement
		err := r.Scan(&st.Query, &st.Calls, &st.MeanMs, &st.TotalMs, &st.Rows)
		return st, err
	})
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "55000" {
			return api.AdminSystemStatementsStatusNotPreloaded, none
		}
		return api.AdminSystemStatementsStatusError, none
	}
	return api.AdminSystemStatementsStatusOk, out
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

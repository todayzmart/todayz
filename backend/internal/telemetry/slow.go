package telemetry

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/todayz/backend/gen/sqlc/telemetrydb"
	"github.com/todayz/backend/internal/platform/httpx/problem"
)

// maxSlowMs caps /v1/test/slow and /v1/test/slow-sql (04 §2.9, D-93).
const maxSlowMs = 30000

// slowMs reads ?ms=. The request validator already enforces 0..30000; this is the handler's own guard.
func slowMs(w http.ResponseWriter, r *http.Request) (int, bool) {
	ms, err := strconv.Atoi(r.URL.Query().Get("ms"))
	if err != nil || ms < 0 || ms > maxSlowMs {
		problem.Write(w, r, problem.New("validation_failed", "").WithField("query.ms", "an integer from 0 to 30000"))
		return 0, false
	}
	return ms, true
}

func writeSlept(w http.ResponseWriter, ms int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]int{"sleptMs": ms})
}

// getSlow answers after ms milliseconds, or as soon as the caller goes away.
func (s *Service) getSlow(w http.ResponseWriter, r *http.Request) {
	ms, ok := slowMs(w, r)
	if !ok {
		return
	}
	t := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer t.Stop()
	select {
	case <-t.C:
		writeSlept(w, ms)
	case <-r.Context().Done():
	}
}

// getSlowSQL runs pg_sleep in a query, so the trace shows a slow named query span.
func (s *Service) getSlowSQL(w http.ResponseWriter, r *http.Request) {
	ms, ok := slowMs(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if _, err := telemetrydb.New(s.d.DB.Q(ctx)).SlowQuery(ctx, float64(ms)/1000); err != nil {
		if ctx.Err() != nil {
			return
		}
		problem.Write(w, r, problem.Wrap("internal", err))
		return
	}
	writeSlept(w, ms)
}

// Package telemetry is the telemetry domain (W1-07, 04 §2.9, 05 §2.5–2.6): the browser's RUM batches
// (POST /v1/rum), client product events (POST /v1/events) and the slow test routes
// (GET /v1/test/slow, /v1/test/slow-sql), which share the /v1/test/clock gate. The plumbing it uses
// (metrics, redaction, the event catalogue) lives in internal/platform.
package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/gen/events"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	ptel "github.com/todayz/backend/internal/platform/telemetry"
	"github.com/todayz/backend/internal/platform/testclock"
)

// Deps is what the domain needs from the binary.
type Deps struct {
	DB      *db.DB
	Clock   clock.Clock
	Metrics *ptel.Metrics // nil: no RUM histograms (tests)
	Env     config.Env
	APIAddr string
	// TestRoutes is true when the test clock is mounted (d.TestClock != nil); the slow routes
	// use exactly the /v1/test/clock gate: Env.AllowsTestRoutes, a loopback API_ADDR and this.
	TestRoutes bool
	// UserID returns the signed-in user's id (a ULID) or "". nil means every caller is anonymous.
	// TODO(W1-01): pass the authenticated principal here once the auth middleware lands.
	UserID func(ctx context.Context) string
}

// Service holds the handlers' state.
type Service struct {
	d       Deps
	cat     *eventcat.Catalogue
	samples atomic.Uint64 // for the 1% raw RUM sample
}

// New loads the event catalogue and returns the service.
func New(d Deps) (*Service, error) {
	cat, err := eventcat.Load(events.FS)
	if err != nil {
		return nil, err
	}
	return &Service{d: d, cat: cat}, nil
}

// Register mounts the routes on r and reports whether the gated slow routes were mounted.
func Register(r chi.Router, d Deps) (*Service, bool, error) {
	s, err := New(d)
	if err != nil {
		return nil, false, err
	}
	r.Post("/v1/rum", s.postRum)
	r.Post("/v1/events", s.postEvents)
	gated := d.Env.AllowsTestRoutes() && d.TestRoutes && testclock.Loopback(d.APIAddr)
	if gated {
		r.Get("/v1/test/slow", s.getSlow)
		r.Get("/v1/test/slow-sql", s.getSlowSQL)
	}
	return s, gated, nil
}

// decode reads a JSON object body. The request validator has already checked the shape against the
// contract; this only guards against a body it let through that Go cannot read.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		problem.Write(w, r, problem.Wrap("bad_request", err).WithDetail("the body must be a JSON object"))
		return false
	}
	return true
}

// Package testclock serves /v1/test/clock (D-80, 04 §5, 08 §Test clock).
//
// In APP_ENV local and test the api and the worker run on a clock.Offset. The
// offset lives in the one-row test_clock table, so both processes share it:
// POST updates the row, a trigger NOTIFYs "test_clock", and every process's
// listener moves its Offset. After a move, POST waits (bounded) until River
// has worked the jobs that became due, so a tester who advances 31 minutes
// sees the expiry already applied.
//
// Register mounts nothing in staging and prod, nor when the API listens on
// anything but loopback, so the route answers 404 there; the process also
// leaves it out when the database does not say local or test (dbenv).
//
// Contract (W0-04 mirrors it in the OpenAPI document):
//
//	GET  /v1/test/clock                    → 200 {now, offsetMs}
//	POST /v1/test/clock {"advance":"31m"}  → 200 {now, offsetMs, dueJobs}
//	POST /v1/test/clock {"set":"<RFC 3339>"}
//	POST /v1/test/clock {"reset":true}
//
// POST needs Content-Type: application/json (415 otherwise). Exactly one of
// advance, set and reset. advance is a Go duration > 0, added in the database
// in one statement, so concurrent advances add up. An optional "wait" (Go
// duration, default 10s, at most 50s so it ends inside the server's 60 s
// write timeout, "0s" to skip) bounds the wait for due jobs; dueJobs is how
// many were still due when it returned.
package testclock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/pgnotify"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Path is the route.
const Path = "/v1/test/clock"

// Channel is the NOTIFY channel the test_clock trigger uses.
const Channel = "test_clock"

const (
	defaultWait = 10 * time.Second
	maxWait     = 50 * time.Second
	pollEvery   = 100 * time.Millisecond
)

// Store persists the shared offset and reports due jobs.
type Store interface {
	Offset(ctx context.Context) (time.Duration, error)
	SetOffset(ctx context.Context, d time.Duration) error
	// AdvanceOffset adds d to the shared offset atomically and returns the
	// new offset.
	AdvanceOffset(ctx context.Context, d time.Duration) (time.Duration, error)
	// DueJobs counts River jobs that are due at now and not yet finished.
	DueJobs(ctx context.Context, now time.Time) (int, error)
}

// PGStore is the Postgres Store.
type PGStore struct{ Pool *pgxpool.Pool }

// Offset implements Store.
func (s PGStore) Offset(ctx context.Context) (time.Duration, error) {
	var ms int64
	if err := s.Pool.QueryRow(ctx, `SELECT offset_ms FROM test_clock WHERE id`).Scan(&ms); err != nil {
		return 0, fmt.Errorf("testclock: read offset: %w", err)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// SetOffset implements Store.
func (s PGStore) SetOffset(ctx context.Context, d time.Duration) error {
	if _, err := s.Pool.Exec(ctx, `UPDATE test_clock SET offset_ms = $1 WHERE id`, d.Milliseconds()); err != nil {
		return fmt.Errorf("testclock: set offset: %w", err)
	}
	return nil
}

// AdvanceOffset implements Store.
func (s PGStore) AdvanceOffset(ctx context.Context, d time.Duration) (time.Duration, error) {
	var ms int64
	if err := s.Pool.QueryRow(ctx, `UPDATE test_clock SET offset_ms = offset_ms + $1 WHERE id RETURNING offset_ms`,
		d.Milliseconds()).Scan(&ms); err != nil {
		return 0, fmt.Errorf("testclock: advance offset: %w", err)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// DueJobs implements Store.
func (s PGStore) DueJobs(ctx context.Context, now time.Time) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job
		WHERE state IN ('available', 'scheduled', 'retryable', 'running') AND scheduled_at <= $1`, now).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("testclock: due jobs: %w", err)
	}
	return n, nil
}

// Service owns the process's Offset clock.
type Service struct {
	Clock *clock.Offset
	Store Store
	Log   *slog.Logger
}

// New returns a Service.
func New(c *clock.Offset, s Store, log *slog.Logger) *Service {
	if log == nil {
		log = telemetry.Default()
	}
	return &Service{Clock: c, Store: s, Log: log}
}

// Load reads the shared offset into the clock.
func (s *Service) Load(ctx context.Context) error {
	d, err := s.Store.Offset(ctx)
	if err != nil {
		return err
	}
	s.Clock.SetOffset(d)
	return nil
}

// Attach follows the shared offset: NOTIFY moves the clock, and every
// (re)connect reloads it.
func (s *Service) Attach(l *pgnotify.Listener) {
	l.Handle(Channel, func(ctx context.Context, payload string) {
		ms, err := strconv.ParseInt(payload, 10, 64)
		if err != nil {
			s.Log.WarnContext(ctx, "testclock: bad notify payload")
			return
		}
		s.Clock.SetOffset(time.Duration(ms) * time.Millisecond)
	})
	l.OnConnect(func(ctx context.Context) {
		if err := s.Load(ctx); err != nil {
			s.Log.WarnContext(ctx, "testclock: reload failed", slog.String("err", err.Error()))
		}
	})
}

// Register mounts the route when env allows test routes (local, test) and
// apiAddr (API_ADDR) is a loopback address. It reports whether it did.
func Register(r chi.Router, env config.Env, apiAddr string, s *Service) bool {
	if !env.AllowsTestRoutes() || s == nil || !Loopback(apiAddr) {
		return false
	}
	r.Get(Path, s.get)
	r.Post(Path, s.post)
	return true
}

// Loopback reports whether addr (host:port) listens only on loopback:
// localhost, 127.0.0.0/8 or ::1. ":8180" (every interface) is not.
func Loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// State is the response body.
type State struct {
	Now      time.Time `json:"now"`
	OffsetMs int64     `json:"offsetMs"`
	DueJobs  *int      `json:"dueJobs,omitempty"`
}

// Request is the POST body.
type Request struct {
	Advance string `json:"advance,omitempty"`
	Set     string `json:"set,omitempty"`
	Reset   bool   `json:"reset,omitempty"`
	Wait    string `json:"wait,omitempty"`
}

func (s *Service) state() State {
	return State{Now: s.Clock.Now().UTC(), OffsetMs: s.Clock.Offset().Milliseconds()}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Service) get(w http.ResponseWriter, _ *http.Request) { writeJSON(w, s.state()) }

// parse validates a request and returns the new offset (or, for advance,
// the amount to add) and wait.
func (s *Service) parse(req Request) (time.Duration, time.Duration, *problem.Error) {
	n := 0
	for _, set := range []bool{req.Advance != "", req.Set != "", req.Reset} {
		if set {
			n++
		}
	}
	if n != 1 {
		return 0, 0, problem.New("validation_failed", "send exactly one of advance, set and reset").
			WithField("body", "exactly one of advance, set, reset")
	}
	wait := defaultWait
	if req.Wait != "" {
		d, err := time.ParseDuration(req.Wait)
		if err != nil || d < 0 || d > maxWait {
			return 0, 0, problem.New("validation_failed", "").WithField("body.wait", "a duration from 0s to 50s")
		}
		wait = d
	}
	switch {
	case req.Reset:
		return 0, wait, nil
	case req.Advance != "":
		d, err := time.ParseDuration(req.Advance)
		if err != nil || d <= 0 {
			return 0, 0, problem.New("validation_failed", "").WithField("body.advance", "a positive duration such as 31m or 2h")
		}
		return d, wait, nil
	default:
		t, err := time.Parse(time.RFC3339, req.Set)
		if err != nil {
			return 0, 0, problem.New("validation_failed", "").WithField("body.set", "an RFC 3339 time")
		}
		// The offset that makes Now() == t at this instant.
		return t.Sub(s.Clock.Now()) + s.Clock.Offset(), wait, nil
	}
}

func (s *Service) post(w http.ResponseWriter, r *http.Request) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		problem.Write(w, r, problem.New("unsupported_media_type", "send Content-Type: application/json"))
		return
	}
	var req Request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		problem.Write(w, r, problem.Wrap("bad_request", err).WithDetail("the body must be a JSON object"))
		return
	}
	offset, wait, perr := s.parse(req)
	if perr != nil {
		problem.Write(w, r, perr)
		return
	}
	ctx := r.Context()
	if req.Advance != "" {
		next, err := s.Store.AdvanceOffset(ctx, offset)
		if err != nil {
			problem.Write(w, r, problem.Wrap("service_unavailable", err))
			return
		}
		offset = next
	} else if err := s.Store.SetOffset(ctx, offset); err != nil {
		problem.Write(w, r, problem.Wrap("service_unavailable", err))
		return
	}
	s.Clock.SetOffset(offset) // NOTIFY will say the same; don't wait for it here
	s.Log.InfoContext(ctx, "test clock moved", slog.Int64("offset_ms", offset.Milliseconds()))

	due, err := s.waitDue(ctx, wait)
	if err != nil {
		problem.Write(w, r, problem.Wrap("service_unavailable", err))
		return
	}
	st := s.state()
	st.DueJobs = &due
	writeJSON(w, st)
}

// waitDue polls until no job is due at the shifted now, or wait passes.
func (s *Service) waitDue(ctx context.Context, wait time.Duration) (int, error) {
	deadline := time.Now().Add(wait) //nolint:forbidigo // the wait is wall time; the test clock is what moves
	for {
		n, err := s.Store.DueJobs(ctx, s.Clock.Now())
		if err != nil || n == 0 || !time.Now().Before(deadline) { //nolint:forbidigo // wall-time deadline
			return n, err
		}
		select {
		case <-ctx.Done():
			return n, nil
		case <-time.After(pollEvery):
		}
	}
}

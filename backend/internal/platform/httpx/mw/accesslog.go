package mw

// Slot 6 · accesslog · owner W1-07 (adds RED metrics and the full log schema
// of 05). W0-03 ships this minimal access log.

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/platform/telemetry"
)

// AccessLog logs one line per request with the route template, never the raw
// path or query (they can carry codes or phone numbers).
func AccessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			level := slog.LevelInfo
			switch {
			case sw.status >= 500:
				level = slog.LevelError
			case route == "/healthz" || route == "/readyz":
				level = slog.LevelDebug
			}
			telemetry.LoggerOr(r.Context(), log).Log(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status", sw.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.Int("bytes", sw.bytes),
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
	bytes  int
}

func (s *statusWriter) WriteHeader(code int) {
	if !s.wrote {
		s.status, s.wrote = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	s.wrote = true
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

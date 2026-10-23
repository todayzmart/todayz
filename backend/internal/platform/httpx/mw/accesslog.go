package mw

// Slot 6 · accesslog · owner W1-07. One summary line per request with the
// 05 §2.2 fields, and the RED metrics (05 §2.1) recorded with the route
// template, never the raw path.

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// SampleRPS is the request rate above which 2xx summaries are sampled
// (05 §2.2); 4xx and 5xx are always logged.
const SampleRPS = 50

// SampleEvery logs one 2xx summary in this many above SampleRPS (10%).
const SampleEvery = 10

// AccessLog logs one line per request with the route template, never the raw
// path or query (they can carry codes or phone numbers), and records the RED
// metrics on m (nil records none).
func AccessLog(log *slog.Logger, m *telemetry.Metrics) func(http.Handler) http.Handler {
	smp := &sampler{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx, notes := telemetry.WithReqNotes(r.Context())
			r = r.WithContext(ctx)
			done := m.RequestStart(ctx)
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			dur := time.Since(start)
			code := notes.Code()
			app := telemetry.AppFromOrigin(r.Header.Get("Origin"))
			done(route, r.Method, sw.status, code, app, dur.Seconds(), sw.bytes)

			level := slog.LevelInfo
			switch {
			case sw.status >= 500:
				level = slog.LevelError
			case route == "/healthz" || route == "/readyz":
				level = slog.LevelDebug
			}
			if level == slog.LevelInfo && sw.status < 400 && !smp.keep(start) {
				return // 2xx/3xx summaries are sampled under load
			}
			kind, role, store := notes.Principal()
			if kind == "" {
				kind = reqctx.PrincipalFrom(ctx).Kind
			}
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status", sw.status),
				slog.Int64("duration_ms", dur.Milliseconds()),
				slog.Int("bytes", sw.bytes),
				slog.String("user_kind", kind),
			}
			if role != "" {
				attrs = append(attrs, slog.String("role", role))
			}
			if store != "" {
				attrs = append(attrs, slog.String("store_id", store))
			}
			if code != "" && sw.status >= 400 {
				attrs = append(attrs, slog.String("err_code", code))
			}
			if sw.status >= 500 {
				attrs = append(attrs, slog.String("fingerprint", Fingerprint(r.Method, route, code, sw.status)))
			}
			telemetry.LoggerOr(ctx, log).LogAttrs(ctx, level, "http request", attrs...)
		})
	}
}

// Fingerprint groups the same failure: a short hash of method, route template,
// problem code and status. It holds no request data.
func Fingerprint(method, route, code string, status int) string {
	h := sha256.Sum256([]byte(method + " " + route + " " + code + " " + strconv.Itoa(status/100)))
	return hex.EncodeToString(h[:6])
}

// sampler counts requests per second and keeps one in SampleEvery above
// SampleRPS.
type sampler struct {
	mu     sync.Mutex
	sec    int64
	n      int
	passed int
}

func (s *sampler) keep(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sec := now.Unix(); sec != s.sec {
		s.sec, s.n, s.passed = sec, 0, 0
	}
	s.n++
	if s.n <= SampleRPS {
		return true
	}
	s.passed++
	return s.passed%SampleEvery == 0
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

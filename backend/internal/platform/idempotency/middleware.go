package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/httpx/route"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Header names.
const (
	HeaderKey      = "Idempotency-Key"
	HeaderReplayed = "Idempotent-Replayed"
)

// MaxStoredBody is the largest response body kept for replay. A larger
// response is sent but the key is released, so a retry runs again.
const MaxStoredBody = 1 << 20

// storedHeaders are the response headers a replay repeats.
var storedHeaders = []string{"Content-Type", "Content-Language", "Location", "ETag", "Cache-Control", "Retry-After"}

// Options configure the middleware.
type Options struct {
	Store Store
	Clock clock.Clock
	Lease time.Duration
	// Log overrides the request logger (telemetry.Logger) when set.
	Log *slog.Logger
}

var (
	completeFailures atomic.Int64
	failureCounter   metric.Int64Counter
	counterOnce      sync.Once
)

// CompleteFailures counts responses that were sent but whose record could
// not be stored (also the OTel counter tz_idempotency_complete_failures).
func CompleteFailures() int64 { return completeFailures.Load() }

func countCompleteFailure(ctx context.Context) {
	completeFailures.Add(1)
	counterOnce.Do(func() {
		failureCounter, _ = otel.Meter("todayz/idempotency").Int64Counter("tz_idempotency_complete_failures",
			metric.WithDescription("Responses sent whose idempotency record could not be stored"))
	})
	if failureCounter != nil {
		failureCounter.Add(ctx, 1)
	}
}

// Middleware runs on unsafe requests to routes that declare idempotency
// (route.Policy; 02 §2.4 step 10). On every other route the header is
// ignored. A route with IdempotencyRequired refuses a request without the
// header (400 idempotency_key_required); IdempotencyOptional honours it when
// present.
func Middleware(o Options) func(http.Handler) http.Handler {
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.Lease <= 0 {
		o.Lease = DefaultLease
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pol := route.FromContext(r.Context())
			if pol.Idempotency == route.IdempotencyNone || !unsafe(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			key := r.Header.Get(HeaderKey)
			if key == "" {
				if pol.Idempotency == route.IdempotencyRequired {
					problem.Write(w, r, problem.New("idempotency_key_required", "This request needs an Idempotency-Key header (a ULID)"))
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if !ids.Valid(key) {
				problem.Write(w, r, problem.New("idempotency_key_invalid", "Idempotency-Key must be a ULID").WithField("header."+HeaderKey, "not a ULID"))
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				// bodylimit's MaxBytesReader reports an over-size body here.
				var mbe *http.MaxBytesError
				if errors.As(err, &mbe) {
					problem.Write(w, r, problem.New("payload_too_large", ""))
					return
				}
				problem.Write(w, r, problem.Wrap("bad_request", err))
				return
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))

			ctx := r.Context()
			log := o.Log
			if log == nil {
				log = telemetry.Logger(ctx)
			}
			scope := Scope{Principal: principal(ctx), Route: r.Method + " " + r.URL.Path, Key: key}
			hash := requestHash(r, body)
			begin, err := o.Store.Begin(ctx, scope, hash, o.Clock.Now(), o.Lease)
			if err != nil {
				problem.Write(w, r, problem.Wrap("service_unavailable", err))
				return
			}
			switch begin.Outcome {
			case Mismatch:
				problem.Write(w, r, problem.New("idempotency_mismatch", "This Idempotency-Key was already used with a different request body"))
				return
			case InProgress:
				problem.Write(w, r, problem.New("conflict", "A request with this Idempotency-Key is still in progress; retry shortly").WithHeader("Retry-After", "1"))
				return
			case Replay:
				replay(w, begin.Response)
				return
			}

			rec := &recorder{ResponseWriter: w, status: http.StatusOK}
			keep := false // true: leave the key alone (stored, or held until its lease ends)
			defer func() {
				if keep {
					return
				}
				// A panic (recover sits outside), a 5xx, an over-size or a
				// no-store response: forget the key so a retry runs.
				if err := o.Store.Release(context.WithoutCancel(ctx), scope, begin.Claim); err != nil {
					log.ErrorContext(ctx, "idempotency release failed", slog.String("err", err.Error()))
				}
			}()
			next.ServeHTTP(rec, r)

			if !rec.wrote {
				rec.capture()
			}
			if rec.status >= 500 || rec.overflow || rec.noStore {
				return
			}
			resp := Response{Status: rec.status, Header: http.Header{}, Body: rec.body.Bytes()}
			for _, h := range storedHeaders {
				if v := rec.snapshot.Values(h); len(v) > 0 {
					resp.Header[h] = v
				}
			}
			keep = true
			if err := o.Store.Complete(context.WithoutCancel(ctx), scope, begin.Claim, resp, o.Clock.Now()); err != nil {
				// The response is sent and its effects are committed. Releasing
				// would let a retry run the request twice, so the key stays in
				// flight until its lease ends: a retry meanwhile gets 409, and
				// one after the lease runs again (rare: alert on the metric).
				countCompleteFailure(ctx)
				log.ErrorContext(ctx, "idempotency complete failed; key held until its lease ends",
					slog.String("route", scope.Route), slog.String("err", err.Error()))
			}
		})
	}
}

func unsafe(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func principal(ctx context.Context) string {
	if p := reqctx.PrincipalFrom(ctx); !p.IsAnonymous() {
		return "user:" + p.UserID
	}
	return "ip:" + reqctx.ClientIP(ctx)
}

// requestHash covers the raw query string and the raw body bytes: "the same
// request" is the same bytes (see the package doc).
func requestHash(r *http.Request, body []byte) []byte {
	h := sha256.New()
	h.Write([]byte(r.URL.RawQuery))
	h.Write([]byte{0})
	h.Write(body)
	return h.Sum(nil)
}

func replay(w http.ResponseWriter, resp *Response) {
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.Header().Set(HeaderReplayed, "true")
	w.WriteHeader(resp.Status)
	_, _ = w.Write(resp.Body)
}

// recorder passes the response through and keeps a copy. The handler's
// headers are captured at its first write, before an outer middleware adds
// its own (secheaders adds the default Cache-Control).
type recorder struct {
	http.ResponseWriter
	status   int
	wrote    bool
	body     bytes.Buffer
	overflow bool
	noStore  bool
	snapshot http.Header
}

func (r *recorder) capture() {
	r.snapshot = r.Header().Clone()
	for _, v := range r.snapshot.Values("Cache-Control") {
		for d := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(d), "no-store") {
				r.noStore = true
			}
		}
	}
}

func (r *recorder) WriteHeader(code int) {
	if !r.wrote {
		r.status, r.wrote = code, true
		r.capture()
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.wrote = true
		r.capture()
	}
	if !r.overflow {
		if r.body.Len()+len(b) > MaxStoredBody {
			r.overflow = true
			r.body.Reset()
		} else {
			r.body.Write(b)
		}
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

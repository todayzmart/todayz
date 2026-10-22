package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/httpx/mw"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/httpx/route"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
)

const appOrigin = "https://app.todayz.localhost"

func newTestRouter(t *testing.T, logs *bytes.Buffer, v mw.Validator) *chi.Mux {
	t.Helper()
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := NewRouter(Options{
		Log:         log,
		CORSOrigins: []string{appOrigin},
		Idempotency: idempotency.Options{Store: idempotency.NewMemoryStore()},
		Validator:   v,
	})
	r.Get("/v1/echo/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"requestId": reqctx.RequestID(r.Context()),
			"ip":        reqctx.ClientIP(r.Context()),
		})
	})
	echo := func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			problem.Write(w, r, problem.Wrap("payload_too_large", err))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(b)
	}
	var plainCalls int
	r.Method(http.MethodPost, "/v1/echo", route.HandleFunc(route.Policy{Idempotency: route.IdempotencyOptional}, echo))
	r.Method(http.MethodPost, "/v1/upload", route.HandleFunc(route.Policy{Upload: true}, echo))
	r.Post("/v1/plain", func(w http.ResponseWriter, _ *http.Request) {
		plainCalls++
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, plainCalls)
	})
	r.Method(http.MethodPost, "/v1/must", route.HandleFunc(route.Policy{Idempotency: route.IdempotencyRequired},
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }))
	r.Get("/v1/panic", func(http.ResponseWriter, *http.Request) { panic("kaboom") })
	r.Get("/v1/cached", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write([]byte("{}"))
	})
	r.Get("/outside", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	return r
}

func do(h http.Handler, method, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, body)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func code(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	require.Equal(t, problem.ContentType, w.Header().Get("Content-Type"))
	var b problem.Body
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &b))
	return b.Code
}

func TestRequestIDAndAccessLog(t *testing.T) {
	var logs bytes.Buffer
	r := newTestRouter(t, &logs, nil)
	w := do(r, "GET", "/v1/echo/9876543210?phone=9876543210", nil, nil)
	require.Equal(t, 200, w.Code)
	id := w.Header().Get(mw.HeaderRequestID)
	require.True(t, ids.Valid(id), "a ULID is generated")
	require.Contains(t, w.Body.String(), id)

	require.Contains(t, logs.String(), `"route":"/v1/echo/{id}"`)
	require.NotContains(t, logs.String(), "9876543210", "the access log has the route template, not the raw path or query")
}

// TestRequestIDIsServerMade (T7): the id is always the server's ULID; the
// client's X-Request-Id or Cf-Ray is only logged, as client_request_id, when
// it is short and safe, and redacted.
func TestRequestIDIsServerMade(t *testing.T) {
	var logs bytes.Buffer
	r := newTestRouter(t, &logs, nil)
	sent := func(hdr map[string]string) (string, string) {
		logs.Reset()
		w := do(r, "GET", "/v1/echo/1", nil, hdr)
		id := w.Header().Get(mw.HeaderRequestID)
		require.True(t, ids.Valid(id), "a server ULID")
		require.Contains(t, w.Body.String(), id)
		return id, logs.String()
	}
	id, l := sent(map[string]string{"X-Request-Id": "abcdef12-cf"})
	require.NotEqual(t, "abcdef12-cf", id, "never the client's id")
	require.Contains(t, l, `"client_request_id":"abcdef12-cf"`)

	_, l = sent(map[string]string{"Cf-Ray": "8a1b2c3d4e5f6789-BOM"})
	require.Contains(t, l, `"client_request_id":"8a1b2c3d4e5f6789-BOM"`)

	_, l = sent(map[string]string{"X-Request-Id": "user_9876543210"})
	require.Contains(t, l, "client_request_id")
	require.NotContains(t, l, "9876543210", "redacted")

	for _, bad := range []string{"bad id\nwith newline", strings.Repeat("a", 65), "x;drop", "a/b"} {
		_, l = sent(map[string]string{"X-Request-Id": bad})
		require.NotContains(t, l, "client_request_id", bad)
	}
}

// TestSecHeaders (T5): every response carries nosniff, no-referrer and the
// deny-all CSP; /v1 answers default to Cache-Control: no-store unless the
// handler chose its own.
func TestSecHeaders(t *testing.T) {
	r := newTestRouter(t, &bytes.Buffer{}, nil)
	check := func(path, cache string) {
		t.Helper()
		w := do(r, "GET", path, nil, nil)
		require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"), path)
		require.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"), path)
		require.Equal(t, "default-src 'none'; frame-ancestors 'none'", w.Header().Get("Content-Security-Policy"), path)
		require.Equal(t, cache, w.Header().Get("Cache-Control"), path)
	}
	check("/v1/echo/1", "no-store")
	check("/v1/nope", "no-store") // the 404 too
	check("/v1/panic", "no-store")
	check("/v1/cached", "public, max-age=60")
	check("/outside", "")
	check("/nope", "")
}

// TestLeaseOutlivesWriteTimeout (I1): a claim outlives the longest a
// handler can run, with slack, so a live request is never taken over.
func TestLeaseOutlivesWriteTimeout(t *testing.T) {
	require.Greater(t, idempotency.DefaultLease, WriteTimeout+30*time.Second)
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	r := newTestRouter(t, &bytes.Buffer{}, nil)
	w := do(r, "GET", "/v1/nope", nil, nil)
	require.Equal(t, 404, w.Code)
	require.Equal(t, "not_found", code(t, w))
	w = do(r, "DELETE", "/v1/echo/1", nil, nil)
	require.Equal(t, 405, w.Code)
	require.Equal(t, "method_not_allowed", code(t, w))
}

func TestRecover(t *testing.T) {
	var logs bytes.Buffer
	r := newTestRouter(t, &logs, nil)
	w := do(r, "GET", "/v1/panic", nil, nil)
	require.Equal(t, 500, w.Code)
	require.Equal(t, "internal", code(t, w))
	require.NotContains(t, w.Body.String(), "kaboom", "the panic value stays in the log")
	require.NotEmpty(t, w.Header().Get(mw.HeaderRequestID), "request id sits outside recover, so the 500 still carries the id")
	require.Contains(t, logs.String(), "kaboom")
}

func TestCORS(t *testing.T) {
	r := newTestRouter(t, &bytes.Buffer{}, nil)
	w := do(r, "GET", "/v1/echo/1", nil, map[string]string{"Origin": appOrigin})
	require.Equal(t, appOrigin, w.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	require.Contains(t, w.Header().Get("Access-Control-Expose-Headers"), "X-Request-Id")

	w = do(r, "GET", "/v1/echo/1", nil, map[string]string{"Origin": "https://evil.example"})
	require.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))

	w = do(r, "OPTIONS", "/v1/echo", nil, map[string]string{"Origin": appOrigin, "Access-Control-Request-Method": "POST"})
	require.Equal(t, 204, w.Code)
	require.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "Idempotency-Key")
	w = do(r, "OPTIONS", "/v1/echo", nil, map[string]string{"Origin": "https://app.todayz.in.evil.example", "Access-Control-Request-Method": "POST"})
	require.Equal(t, 204, w.Code)
	require.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestBodyLimit(t *testing.T) {
	r := newTestRouter(t, &bytes.Buffer{}, nil)
	big := strings.Repeat("x", mw.DefaultBodyLimit+1)
	w := do(r, "POST", "/v1/echo", strings.NewReader(big), map[string]string{"Content-Type": "application/json"})
	require.Equal(t, 413, w.Code)
	require.Equal(t, "payload_too_large", code(t, w))

	// Without Content-Length the reader stops at the limit.
	req := httptest.NewRequest(http.MethodPost, "/v1/echo", io.NopCloser(strings.NewReader(big)))
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, 413, rec.Code)

	// Through the idempotency middleware too.
	w = do(r, "POST", "/v1/echo", io.NopCloser(strings.NewReader(big)), map[string]string{idempotency.HeaderKey: ids.New(timeZero()).String()})
	require.Equal(t, 413, w.Code)

	// T8: the Content-Type does not raise the limit; only an upload route does.
	w = do(r, "POST", "/v1/echo", strings.NewReader(big), map[string]string{"Content-Type": "multipart/form-data; boundary=x"})
	require.Equal(t, 413, w.Code)
	w = do(r, "POST", "/v1/upload", strings.NewReader(big), map[string]string{"Content-Type": "multipart/form-data; boundary=x"})
	require.Equal(t, 201, w.Code)
	huge := strings.Repeat("x", mw.UploadBodyLimit+1)
	w = do(r, "POST", "/v1/upload", io.NopCloser(strings.NewReader(huge)), nil)
	require.Equal(t, 413, w.Code)
}

func TestIdempotencyInPipeline(t *testing.T) {
	r := newTestRouter(t, &bytes.Buffer{}, nil)
	w := do(r, "POST", "/v1/must", nil, nil)
	require.Equal(t, 400, w.Code)
	require.Equal(t, "idempotency_key_required", code(t, w))
	key := ids.New(timeZero()).String()
	require.Equal(t, 201, do(r, "POST", "/v1/must", strings.NewReader("{}"), map[string]string{idempotency.HeaderKey: key}).Code)
	w = do(r, "POST", "/v1/must", strings.NewReader("{}"), map[string]string{idempotency.HeaderKey: key})
	require.Equal(t, "true", w.Header().Get(idempotency.HeaderReplayed))
	w = do(r, "POST", "/v1/must", strings.NewReader(`{"x":1}`), map[string]string{idempotency.HeaderKey: key})
	require.Equal(t, 422, w.Code)
	require.Equal(t, "idempotency_mismatch", code(t, w))

	// I3: a route that did not declare idempotency ignores the header.
	plainKey := ids.New(timeZero()).String()
	require.Equal(t, "1", do(r, "POST", "/v1/plain", nil, map[string]string{idempotency.HeaderKey: plainKey}).Body.String())
	w = do(r, "POST", "/v1/plain", nil, map[string]string{idempotency.HeaderKey: plainKey})
	require.Equal(t, "2", w.Body.String(), "runs again")
	require.Empty(t, w.Header().Get(idempotency.HeaderReplayed))
	require.Equal(t, 201, do(r, "POST", "/v1/plain", nil, map[string]string{idempotency.HeaderKey: "not-a-ulid"}).Code)
}

func TestValidateHook(t *testing.T) {
	v := mw.ValidatorFunc(func(r *http.Request) error {
		if r.URL.Query().Get("bad") != "" {
			return problem.New("validation_failed", "").WithField("query.bad", "not allowed")
		}
		return nil
	})
	r := newTestRouter(t, &bytes.Buffer{}, v)
	require.Equal(t, 200, do(r, "GET", "/v1/echo/1", nil, nil).Code)
	w := do(r, "GET", "/v1/echo/1?bad=1", nil, nil)
	require.Equal(t, 400, w.Code)
	require.Equal(t, "validation_failed", code(t, w))
}

func TestClientIP(t *testing.T) {
	type tc struct {
		name   string
		remote string
		hdr    map[string]string
		trust  bool
		want   string
	}
	for _, c := range []tc{
		{"peer", "198.51.100.1:5000", nil, false, "198.51.100.1"},
		{"spoofed header from a public peer is ignored", "198.51.100.1:5000", map[string]string{"CF-Connecting-IP": "1.2.3.4"}, false, "198.51.100.1"},
		{"loopback proxy (Caddy) XFF", "127.0.0.1:5000", map[string]string{"X-Forwarded-For": "9.9.9.9, 203.0.113.5"}, false, "203.0.113.5"},
		{"cloudflare trusted when deployed", "10.0.0.5:5000", map[string]string{"CF-Connecting-IP": "203.0.113.9"}, true, "203.0.113.9"},
		{"garbage header falls back", "10.0.0.5:5000", map[string]string{"CF-Connecting-IP": "nope"}, true, "10.0.0.5"},
		{"v4-mapped v6 is unmapped", "[::ffff:198.51.100.2]:1", nil, false, "198.51.100.2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var got string
			h := mw.ClientIP(mw.ClientIPOptions{TrustCloudflare: c.trust})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = reqctx.ClientIP(r.Context())
			}))
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = c.remote
			for k, v := range c.hdr {
				r.Header.Set(k, v)
			}
			h.ServeHTTP(httptest.NewRecorder(), r)
			require.Equal(t, c.want, got)
		})
	}
}

func TestOutboundClientGuard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()

	blocked := NewOutboundClient(OutboundOptions{NoEgress: true})
	_, err := blocked.Get("https://example.com/")
	require.ErrorIs(t, err, ErrEgressBlocked)
	resp, err := blocked.Get(srv.URL) // 127.0.0.1
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)

	open := NewOutboundClient(OutboundOptions{})
	require.NotNil(t, open.Transport)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err = open.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
}

func timeZero() time.Time { return time.Now() }

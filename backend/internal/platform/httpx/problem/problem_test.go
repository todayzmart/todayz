package problem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/platform/telemetry"
)

func do(t *testing.T, err error) (*httptest.ResponseRecorder, Body) {
	t.Helper()
	return doLog(t, err, nil)
}

// doLog writes err with log as the request logger.
func doLog(t *testing.T, err error, log *slog.Logger) (*httptest.ResponseRecorder, Body) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	ctx := reqctx.WithRequestID(req.Context(), "01JREQ")
	if log != nil {
		ctx = telemetry.WithLogger(ctx, log)
	}
	req = req.WithContext(ctx)
	Write(rec, req, err)
	var b Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	return rec, b
}

func TestWriteRegistered(t *testing.T) {
	rec, b := do(t, New(genproblems.InvalidTransition, "from accepted to placed").WithField("status", "not allowed"))
	require.Equal(t, 409, rec.Code)
	require.Equal(t, ContentType, rec.Header().Get("Content-Type"))
	require.Equal(t, Body{
		Type: "https://todayz.in/errors/invalid_transition", Title: "This change is not allowed in the current state",
		Status: 409, Code: "invalid_transition", Detail: "from accepted to placed", RequestID: "01JREQ",
		Fields: map[string]string{"status": "not allowed"},
	}, b)

	rec, b = do(t, New(genproblems.IdempotencyMismatch, ""))
	require.Equal(t, 422, rec.Code)
	require.Equal(t, "idempotency_mismatch", b.Code)

	rec, _ = do(t, New(genproblems.RateLimited, "").WithHeader("Retry-After", "30"))
	require.Equal(t, 429, rec.Code)
	require.Equal(t, "30", rec.Header().Get("Retry-After"))
}

func TestWriteHidesInternals(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))

	rec, b := doLog(t, errors.New("pq: connection refused to 10.0.0.1"), log)
	require.Equal(t, 500, rec.Code)
	require.Equal(t, "internal", b.Code)
	require.Empty(t, b.Detail)
	require.NotContains(t, rec.Body.String(), "10.0.0.1")
	require.Contains(t, logBuf.String(), "connection refused", "the cause is logged")

	_, b = do(t, Wrap(genproblems.ServiceUnavailable, errors.New("db down")).WithDetail("db host x"))
	require.Equal(t, 503, b.Status)
	require.Empty(t, b.Detail, "5xx never sends detail")
}

func TestClientClosedIsNot500(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))
	ctx, cancel := context.WithCancel(telemetry.WithLogger(context.Background(), log))
	cancel()
	rec := httptest.NewRecorder()
	Write(rec, httptest.NewRequest(http.MethodPost, "/v1/x", nil).WithContext(ctx), fmt.Errorf("internal: %w", context.Canceled))
	require.Equal(t, StatusClientClosed, rec.Code)
	require.Empty(t, rec.Body.String())
	require.Contains(t, logBuf.String(), `"level":"INFO"`)
	require.NotContains(t, logBuf.String(), "request failed")

	// A problem that wraps the cancel (the SSE replay's 503) is a hang-up too.
	rec = httptest.NewRecorder()
	Write(rec, httptest.NewRequest(http.MethodGet, "/v1/me/stream", nil).WithContext(ctx), Wrap(genproblems.ServiceUnavailable, context.Canceled))
	require.Equal(t, StatusClientClosed, rec.Code)

	// A canceled context of our own making while the client is still there is a real failure.
	rec, b := doLog(t, fmt.Errorf("query: %w", context.Canceled), log)
	require.Equal(t, 500, rec.Code)
	require.Equal(t, "internal", b.Code)
}

func TestUnregisteredCodeBecomes500(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))

	before := UnregisteredCount()
	rec, b := doLog(t, New("made_up_code", "x"), log)
	require.Equal(t, 500, rec.Code)
	require.Equal(t, "internal", b.Code)
	require.Equal(t, before+1, UnregisteredCount())
	require.Contains(t, logBuf.String(), "unregistered problem code")
	require.Contains(t, logBuf.String(), "made_up_code")
	require.False(t, Registered("made_up_code"))
	require.True(t, Registered("not_found"))
}

func TestErrorHelpers(t *testing.T) {
	cause := errors.New("boom")
	e := Wrap(genproblems.Conflict, cause)
	require.ErrorIs(t, e, cause)
	require.ErrorIs(t, e, New(genproblems.Conflict, ""))
	require.NotErrorIs(t, e, New(genproblems.NotFound, ""))
	require.Equal(t, "conflict", CodeOf(e))
	require.Equal(t, "", CodeOf(cause))
	require.Equal(t, "conflict: boom", e.Error())
	require.Equal(t, "not_found: x 1", Newf(genproblems.NotFound, "x %d", 1).Error())
	require.Equal(t, 409, Status(e))
	require.Equal(t, 500, Status(cause))
}

func TestHandlerAndFallbacks(t *testing.T) {
	h := Handler(func(http.ResponseWriter, *http.Request) error { return New(genproblems.Forbidden, "") })
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(context.Background()))
	require.Equal(t, 403, rec.Code)

	rec = httptest.NewRecorder()
	NotFound(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	require.Equal(t, 404, rec.Code)
	rec = httptest.NewRecorder()
	MethodNotAllowed(rec, httptest.NewRequest(http.MethodPut, "/healthz", nil))
	require.Equal(t, 405, rec.Code)
}

func TestEveryGeneratedConstantIsRegistered(t *testing.T) {
	for _, c := range Registry().Codes() {
		require.True(t, Registered(c))
	}
	require.Len(t, Registry().Codes(), len(Registry().Codes()))
}

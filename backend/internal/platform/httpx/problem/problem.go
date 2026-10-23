// Package problem writes RFC 9457 problem responses (04 §1.4, D-24).
//
// A handler returns a *problem.Error built with New or Wrap and a registered
// code (constants in github.com/todayz/backend/gen/problems). Write turns any
// error into problem JSON:
//
//	{"type":"https://todayz.in/errors/<code>","title":…,"status":…,"code":…,
//	 "detail":…,"requestId":…,"fields":{…}}
//
// A code that is not in backend/api/problems/*.yaml is never sent: Write logs
// it at error level, counts it, and answers 500 internal. TestNoUnregisteredCodes
// fails the build when a literal code in the source is not registered.
package problem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/httpx/problem/registry"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// StatusClientClosed is the access-log status of a request whose client
// went away before the answer (nginx's 499).
const StatusClientClosed = 499

// TypeBase prefixes the problem type URI.
const TypeBase = "https://todayz.in/errors/"

// ContentType is the media type of a problem body.
const ContentType = "application/problem+json"

// Error is a problem a handler returns. Detail is English for logs and
// developers; never put a phone, code, token or address in it.
type Error struct {
	Code   string
	Detail string
	// Fields maps a JSON path to a short reason, e.g. "items[2].quantity": "max 3".
	Fields map[string]string
	// Header is set on the response (e.g. Retry-After for rate_limited).
	Header http.Header
	// Extensions are extra members of the body (04 §1.4 "extensions"), e.g.
	// fieldsTruncated: true when Fields was capped.
	Extensions map[string]any
	cause      error
}

// New returns a problem with a registered code and an English detail.
func New(code, detail string) *Error { return &Error{Code: code, Detail: detail} }

// Newf is New with a formatted detail.
func Newf(code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// Wrap attaches a cause. The cause is logged, never sent to the client.
func Wrap(code string, cause error) *Error { return &Error{Code: code, cause: cause} }

// WithField adds a field reason.
func (e *Error) WithField(path, reason string) *Error {
	if e.Fields == nil {
		e.Fields = map[string]string{}
	}
	e.Fields[path] = reason
	return e
}

// WithExtension sets an extension member. Never put a client value in it.
func (e *Error) WithExtension(key string, v any) *Error {
	if e.Extensions == nil {
		e.Extensions = map[string]any{}
	}
	e.Extensions[key] = v
	return e
}

// WithDetail sets the detail.
func (e *Error) WithDetail(detail string) *Error { e.Detail = detail; return e }

// WithHeader sets a response header.
func (e *Error) WithHeader(k, v string) *Error {
	if e.Header == nil {
		e.Header = http.Header{}
	}
	e.Header.Set(k, v)
	return e
}

func (e *Error) Error() string {
	switch {
	case e.cause != nil && e.Detail != "":
		return e.Code + ": " + e.Detail + ": " + e.cause.Error()
	case e.cause != nil:
		return e.Code + ": " + e.cause.Error()
	case e.Detail != "":
		return e.Code + ": " + e.Detail
	default:
		return e.Code
	}
}

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.cause }

// Is matches another *Error by code, so errors.Is(err, problem.New(code, "")) works.
func (e *Error) Is(target error) bool {
	var t *Error
	return errors.As(target, &t) && t.Code == e.Code
}

// CodeOf returns the problem code of err, or "" if it is not a problem.
func CodeOf(err error) string {
	var p *Error
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

// Body is the wire shape.
type Body struct {
	Type       string            `json:"type"`
	Title      string            `json:"title"`
	Status     int               `json:"status"`
	Code       string            `json:"code"`
	Detail     string            `json:"detail,omitempty"`
	RequestID  string            `json:"requestId,omitempty"`
	Fields     map[string]string `json:"fields,omitempty"`
	Extensions map[string]any    `json:"extensions,omitempty"`
}

var (
	regOnce      sync.Once
	reg          *registry.Registry
	unregistered atomic.Int64
)

// Registry returns the embedded registry. make gen validated it, so a load
// error here means the generated package is stale; it panics at start-up.
func Registry() *registry.Registry {
	regOnce.Do(func() {
		r, err := registry.Load(genproblems.FS)
		if err != nil {
			panic("problem registry: " + err.Error() + " (run make gen)")
		}
		reg = r
	})
	return reg
}

// Registered reports whether code is in the registry.
func Registered(code string) bool {
	_, ok := Registry().Lookup(code)
	return ok
}

// UnregisteredCount is how many responses tried to use an unregistered code.
func UnregisteredCount() int64 { return unregistered.Load() }

// Status returns the HTTP status for err as Write would send it.
func Status(err error) int {
	b, _ := resolve(err)
	return b.Status
}

// resolve maps err to a body. ok is false when err carried an unregistered code.
func resolve(err error) (Body, bool) {
	r := Registry()
	code, detail, fields, ext := genproblems.Internal, "", map[string]string(nil), map[string]any(nil)
	var p *Error
	ok := true
	if errors.As(err, &p) {
		if _, found := r.Lookup(p.Code); found {
			code, detail, fields, ext = p.Code, p.Detail, p.Fields, p.Extensions
		} else {
			ok = false
		}
	}
	e, _ := r.Lookup(code)
	if e.Status >= 500 {
		// Never send internals to the client; the cause is in the log.
		detail, fields, ext = "", nil, nil
	}
	return Body{Type: TypeBase + code, Title: e.Title, Status: e.Status, Code: code, Detail: detail, Fields: fields, Extensions: ext}, ok
}

// Write sends err as problem JSON and logs it: 5xx at error with the cause,
// 4xx at debug, through the request logger (telemetry.Logger). An unregistered
// code is logged at error and sent as 500. Cache-Control comes from the
// secheaders middleware (no-store on /v1).
func Write(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		// The client hung up first: not a server fault and nobody reads the
		// answer. 499 keeps it out of the 5xx error rate.
		telemetry.Logger(ctx).InfoContext(ctx, "client closed the request", slog.String("route", reqctx.Route(ctx)))
		w.WriteHeader(StatusClientClosed)
		return
	}
	body, ok := resolve(err)
	body.RequestID = reqctx.RequestID(ctx)
	telemetry.NoteErrCode(ctx, body.Code) // the access log's `code` label (W1-07)
	log := telemetry.Logger(ctx)
	if !ok {
		unregistered.Add(1)
		log.ErrorContext(ctx, "unregistered problem code", slog.String("err_code", CodeOf(err)), slog.String("route", reqctx.Route(ctx)))
	}
	if body.Status >= 500 {
		log.ErrorContext(ctx, "request failed", slog.String("err_code", body.Code), slog.Any("err", err))
	} else {
		log.DebugContext(ctx, "request refused", slog.String("err_code", body.Code), slog.Int("status", body.Status))
	}
	var p *Error
	if errors.As(err, &p) && ok {
		for k, vs := range p.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
	}
	w.Header().Set("Content-Type", ContentType)
	w.WriteHeader(body.Status)
	_ = json.NewEncoder(w).Encode(body)
}

// Handler adapts a handler that returns an error: a non-nil error is written
// with Write. Domain handlers use it so error paths always go through the registry.
func Handler(fn func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			Write(w, r, err)
		}
	}
}

// NotFound and MethodNotAllowed are chi's fallbacks.
func NotFound(w http.ResponseWriter, r *http.Request) {
	Write(w, r, New(genproblems.NotFound, ""))
}

// MethodNotAllowed answers 405 with a problem body.
func MethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	Write(w, r, New(genproblems.MethodNotAllowed, ""))
}

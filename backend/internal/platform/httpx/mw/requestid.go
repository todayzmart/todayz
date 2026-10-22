package mw

// Slot 1 · requestid · owner W0-03. Outermost, so every later slot (recover
// included) logs with the request id and the request logger.

import (
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// HeaderRequestID carries the request id back to the client.
const HeaderRequestID = "X-Request-Id"

var reClientRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// ClientRequestID returns the id the client (or Cloudflare) sent in
// X-Request-Id or Cf-Ray, when it is at most 64 characters of
// [A-Za-z0-9._-], passed through the redactor; else "".
func ClientRequestID(r *http.Request) string {
	for _, h := range []string{HeaderRequestID, "Cf-Ray"} {
		if v := r.Header.Get(h); reClientRequestID.MatchString(v) {
			return telemetry.RedactString(v)
		}
	}
	return ""
}

// RequestID gives every request a server-made ULID (never the client's: a
// client could otherwise forge or collide ids in the logs), echoes it in
// X-Request-Id, and stores the request logger (log + client_request_id when
// the client sent a valid one) for telemetry.Logger.
func RequestID(log *slog.Logger) func(http.Handler) http.Handler {
	base := telemetry.OrDefault(log)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := ids.New(time.Now()).String()
			w.Header().Set(HeaderRequestID, id)
			l := base
			if c := ClientRequestID(r); c != "" {
				l = base.With(slog.String("client_request_id", c))
			}
			ctx := reqctx.WithRequestID(r.Context(), id)
			ctx = telemetry.WithLogger(ctx, l)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

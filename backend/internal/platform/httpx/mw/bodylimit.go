package mw

// Slot 10 · bodylimit · owner W0-03.

import (
	"net/http"

	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/httpx/route"
)

// Body limits (02 §2.4 step 10).
const (
	DefaultBodyLimit = 1 << 20  // 1 MB, every route
	UploadBodyLimit  = 10 << 20 // 10 MB, only routes registered with route.Policy{Upload: true}
)

// BodyLimit refuses a body over the limit with 413 payload_too_large: at once
// when Content-Length says so, otherwise when a reader passes the limit
// (http.MaxBytesError; idempotency and the handlers map it to 413). The limit
// comes from the route's policy (routepolicy runs first), never from the
// request's Content-Type.
func BodyLimit() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := int64(DefaultBodyLimit)
			if route.FromContext(r.Context()).Upload {
				limit = UploadBodyLimit
			}
			if r.ContentLength > limit {
				problem.Write(w, r, problem.New("payload_too_large", ""))
				return
			}
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

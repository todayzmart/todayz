package mw

// Slot 12 · ratelimit · owner W1-01 (02 §2.3, §2.4; 04 §1). One token bucket
// per principal (signed in) or client IP (anonymous), per route group, per
// instance. Every limited response carries RateLimit-Limit, -Remaining and
// -Reset; a refusal is 429 rate_limited with Retry-After (whole seconds).

import (
	"math"
	"net/http"
	"strconv"
	"time"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ratelimit"
	"github.com/todayz/backend/internal/platform/reqctx"
)

// Limiter decides one request (ratelimit.Limiter implements it).
type Limiter interface {
	Allow(group, key string) ratelimit.Result
}

// RateLimit limits requests per principal or IP per route group. A nil
// limiter limits nothing (the contract smoke and local runs).
func RateLimit(l Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if l == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := "ip:" + reqctx.ClientIP(r.Context())
			if p, ok := authn.FromContext(r.Context()); ok {
				key = "u:" + p.UserID.String() + ":" + string(p.App)
			}
			res := l.Allow(groupOf(r.URL.Path), key)
			if res.Limit > 0 {
				h := w.Header()
				h.Set("RateLimit-Limit", strconv.Itoa(res.Limit))
				h.Set("RateLimit-Remaining", strconv.Itoa(res.Remaining))
				h.Set("RateLimit-Reset", seconds(res.Reset))
			}
			if !res.Allowed {
				problem.Write(w, r, problem.New(genproblems.RateLimited, "").WithHeader("Retry-After", seconds(res.RetryAfter)))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func groupOf(path string) string {
	switch {
	case under(path, "/v1/auth"):
		return ratelimit.GroupAuth
	case under(path, "/v1/me"):
		return ratelimit.GroupMe
	case under(path, "/v1/biz"):
		return ratelimit.GroupBiz
	case under(path, "/v1/admin"):
		return ratelimit.GroupAdmin
	}
	return ratelimit.GroupPublic
}

// seconds rounds d up to whole seconds, at least 1.
func seconds(d time.Duration) string {
	s := int(math.Ceil(d.Seconds()))
	if s < 1 {
		s = 1
	}
	return strconv.Itoa(s)
}

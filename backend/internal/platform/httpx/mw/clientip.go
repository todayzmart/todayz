package mw

// Slot 5 · clientip · owner W0-03.

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/todayz/backend/internal/platform/reqctx"
)

// ClientIPOptions say which forwarding headers to believe.
type ClientIPOptions struct {
	// TrustCloudflare believes CF-Connecting-IP from any peer. Only for staging
	// and prod, where the ALB accepts Cloudflare's ranges only (02 §2.4 step 4).
	TrustCloudflare bool
}

// ClientIP stores the real client IP in the context. Without TrustCloudflare
// (local, test) it believes CF-Connecting-IP or the last X-Forwarded-For hop
// only from a loopback peer (Caddy locally); otherwise it uses the peer.
func ClientIP(o ClientIPOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(reqctx.WithClientIP(r.Context(), realIP(r, o))))
		})
	}
}

func realIP(r *http.Request, o ClientIPOptions) string {
	peer := r.RemoteAddr
	if h, _, err := net.SplitHostPort(peer); err == nil {
		peer = h
	}
	peerAddr, err := netip.ParseAddr(peer)
	if o.TrustCloudflare || (err == nil && peerAddr.IsLoopback()) {
		if v, ok := parseIP(r.Header.Get("CF-Connecting-IP")); ok {
			return v
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if v, ok := parseIP(parts[len(parts)-1]); ok {
				return v
			}
		}
	}
	if err == nil {
		return peerAddr.Unmap().String()
	}
	return peer
}

func parseIP(s string) (string, bool) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return "", false
	}
	return a.Unmap().String(), true
}

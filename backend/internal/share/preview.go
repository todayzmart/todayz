package share

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/netip"
	"strings"

	shareapi "github.com/todayz/backend/gen/api/share"
	"github.com/todayz/backend/internal/platform/reqctx"
)

// The customer Pages Function reads a link preview from Cloudflare, so the
// API sees Cloudflare's IP: every preview would share a few read buckets and
// share_opened would count one open per Cloudflare egress IP. The Function
// therefore sends the visitor's CF-Connecting-IP in HeaderPreviewIP with
// SHARE_PREVIEW_KEY in HeaderPreviewKey.
const (
	HeaderPreviewKey = "X-TZ-Preview-Key"
	HeaderPreviewIP  = "X-TZ-Preview-IP"
)

type previewIPKey struct{}

// previewIP believes the forwarded visitor IP on GetShareCard only, only when
// the key matches SHARE_PREVIEW_KEY (compared in constant time) and the IP
// parses. Anything else is ignored, and the read is counted against the
// caller's own IP as before. Neither header is ever logged.
func (s *Service) previewIP(f shareapi.StrictHandlerFunc, op string) shareapi.StrictHandlerFunc {
	if op != "GetShareCard" || len(s.previewKey) == 0 {
		return f
	}
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		if ip, ok := s.forwardedIP(r.Header); ok {
			ctx = context.WithValue(ctx, previewIPKey{}, ip)
		}
		return f(ctx, w, r, req)
	}
}

func (s *Service) forwardedIP(h http.Header) (string, bool) {
	got := h.Get(HeaderPreviewKey)
	if got == "" || subtle.ConstantTimeCompare([]byte(got), s.previewKey) != 1 {
		return "", false
	}
	a, err := netip.ParseAddr(strings.TrimSpace(h.Get(HeaderPreviewIP)))
	if err != nil {
		return "", false
	}
	return a.Unmap().String(), true
}

// visitorIP is the forwarded preview IP previewIP believed, else the
// request's client IP.
func visitorIP(ctx context.Context) string {
	if ip, ok := ctx.Value(previewIPKey{}).(string); ok {
		return ip
	}
	return reqctx.ClientIP(ctx)
}

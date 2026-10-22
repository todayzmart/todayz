// Package reqctx carries per-request values through context: the request id,
// the real client IP and the principal. Middleware sets them; handlers, the
// logger and the idempotency store read them.
package reqctx

import "context"

type key int

const (
	keyRequestID key = iota
	keyClientIP
	keyPrincipal
	keyRoute
)

// Principal is who is calling. authn (W1-01) sets it; until then every
// request is anonymous.
type Principal struct {
	// Kind is "anonymous", "customer", "staff" or "admin".
	Kind string
	// UserID is the user's ULID string; empty when anonymous.
	UserID string
}

// Anonymous is the principal of an unauthenticated request.
var Anonymous = Principal{Kind: "anonymous"}

// IsAnonymous reports whether p has no user.
func (p Principal) IsAnonymous() bool { return p.UserID == "" }

// WithRequestID stores the request id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyRequestID, id)
}

// RequestID returns the request id, or "".
func RequestID(ctx context.Context) string {
	s, _ := ctx.Value(keyRequestID).(string)
	return s
}

// WithClientIP stores the real client IP.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, keyClientIP, ip)
}

// ClientIP returns the real client IP, or "".
func ClientIP(ctx context.Context) string {
	s, _ := ctx.Value(keyClientIP).(string)
	return s
}

// WithPrincipal stores the principal.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, keyPrincipal, p)
}

// PrincipalFrom returns the principal; Anonymous when none is set.
func PrincipalFrom(ctx context.Context) Principal {
	if p, ok := ctx.Value(keyPrincipal).(Principal); ok {
		return p
	}
	return Anonymous
}

// WithRoute stores the matched route template (for example "/v1/me/orders/{id}").
func WithRoute(ctx context.Context, route string) context.Context {
	return context.WithValue(ctx, keyRoute, route)
}

// Route returns the route template, or "".
func Route(ctx context.Context) string {
	s, _ := ctx.Value(keyRoute).(string)
	return s
}

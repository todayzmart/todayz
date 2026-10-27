// Package authn is who is calling (02 §3, 04 §1.3, D-14): the principal type
// every domain reads, the context helpers, the EdDSA access tokens with key
// rotation by kid, and the Authenticator behind middleware slot 11.
//
// Stable interface (W1-01 phase A; W1-04, W1-08 and later domains build on it):
//
//   - Principal, App, AMR and their methods
//   - WithPrincipal / FromContext / MustFromContext
//   - Tokens.Issue / Tokens.Verify, KeySet
//   - Authenticator.Authenticate and the SessionChecker it calls
//
// Refresh tokens, sessions and OTPs live in internal/identity; this package
// never touches the database itself.
package authn

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
)

// App is the app a session belongs to (D-14). Each has its own session family,
// refresh cookie and token audience; one app's token never works for another.
type App string

// The three apps.
const (
	AppCustomer App = "customer"
	AppBusiness App = "business"
	AppAdmin    App = "admin"
)

// Apps lists every app.
var Apps = []App{AppCustomer, AppBusiness, AppAdmin}

// ParseApp reads an app name.
func ParseApp(s string) (App, bool) {
	switch App(s) {
	case AppCustomer, AppBusiness, AppAdmin:
		return App(s), true
	}
	return "", false
}

// Valid reports whether a is one of the three apps.
func (a App) Valid() bool { _, ok := ParseApp(string(a)); return ok }

// CookieName is the app's refresh cookie (D-14).
func (a App) CookieName() string {
	switch a {
	case AppCustomer:
		return "__Secure-tz_rt_c"
	case AppBusiness:
		return "__Secure-tz_rt_b"
	case AppAdmin:
		return "__Secure-tz_rt_a"
	}
	return ""
}

// Kind is the reqctx.Principal kind for the app: customer, staff or admin.
func (a App) Kind() string {
	switch a {
	case AppCustomer:
		return "customer"
	case AppBusiness:
		return "staff"
	case AppAdmin:
		return "admin"
	}
	return "anonymous"
}

// Origin host prefix of the app (app., business., admin.), used by the
// refresh CSRF check against config.CORSOrigins.
func (a App) hostPrefix() string {
	switch a {
	case AppCustomer:
		return "app."
	case AppBusiness:
		return "business."
	case AppAdmin:
		return "admin."
	}
	return "\x00"
}

// Session lifetimes (01 §2). Fixed product constants, not settings.
const (
	AccessTTL         = 15 * time.Minute
	RefreshTTL        = 30 * 24 * time.Hour // customer and business
	AdminRefreshTTL   = 12 * time.Hour
	StepUpWindow      = 10 * time.Minute // admin SMS step-up for † actions (02 §3.3, D-264)
	RefreshCookiePath = "/v1/auth"
)

// RefreshTTL returns the refresh-token lifetime of the app.
func (a App) RefreshTTL() time.Duration {
	if a == AppAdmin {
		return AdminRefreshTTL
	}
	return RefreshTTL
}

// AMR is how the session was authenticated (RFC 8176 style).
type AMR string

// The authentication method. Every app signs in with the SMS OTP alone; the
// admin authenticator (amr otp+totp) is gone (D-264), so a token carrying it
// no longer verifies and the client refreshes into an otp one.
const (
	AMROTP AMR = "otp" // SMS OTP
)

// Principal is an authenticated caller. The zero value is anonymous.
type Principal struct {
	UserID    ids.ID
	App       App
	SessionID ids.ID
	// Roles are the admin roles (D-64) of an admin principal, read live from
	// admin_users on every request; empty for the other apps. Business roles
	// are per business and come from the authz StaffResolver, never from here.
	Roles []string
	AMR   AMR
	// IssuedAt and ExpiresAt are the access token's.
	IssuedAt  time.Time
	ExpiresAt time.Time
	// StepUpAt is when the admin last passed an SMS step-up in this sign-in
	// (zero if never, D-264). authz compares it with StepUpWindow for †
	// actions.
	StepUpAt time.Time
}

// IsAnonymous reports whether p has no user.
func (p Principal) IsAnonymous() bool { return p.UserID == ids.Zero }

// HasRole reports whether an admin principal has role.
func (p Principal) HasRole(role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

type ctxKey struct{}

// WithPrincipal stores p in ctx and mirrors it into reqctx (kind and user id),
// which the logger and the idempotency store read.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	ctx = context.WithValue(ctx, ctxKey{}, p)
	if p.IsAnonymous() {
		return reqctx.WithPrincipal(ctx, reqctx.Anonymous)
	}
	return reqctx.WithPrincipal(ctx, reqctx.Principal{Kind: p.App.Kind(), UserID: p.UserID.String()})
}

// FromContext returns the principal and whether one is signed in.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	if !ok || p.IsAnonymous() {
		return Principal{}, false
	}
	return p, true
}

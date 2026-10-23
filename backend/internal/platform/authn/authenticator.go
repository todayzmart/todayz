package authn

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// SessionState is what the session store says about a token's session right
// now. It lets a revoked session, a blocked user or a disabled admin stop at
// once, instead of when the 15-minute access token expires.
type SessionState struct {
	// Active is false for a revoked, expired or unknown session, or one of
	// another user or app.
	Active bool
	// Blocked is true when the user is blocked or deleted (AU-17).
	Blocked bool
	// Roles are the admin's live roles (admin app only); an admin whose
	// admin_users row is disabled has an inactive session instead.
	Roles []string
	// StepUpAt is the session's last TOTP verification (admin app; zero if none).
	StepUpAt time.Time
}

// SessionChecker looks a session up by primary key. internal/identity
// implements it; one indexed read per authenticated request.
type SessionChecker interface {
	CheckSession(ctx context.Context, userID, sessionID ids.ID, app App) (SessionState, error)
}

// Authenticator turns a bearer token into a principal (slot 11).
type Authenticator struct {
	tokens   *Tokens
	sessions SessionChecker
}

// NewAuthenticator returns an authenticator. sessions may be nil (no database,
// as in the route-only contract test): then a verified token is trusted until
// it expires.
func NewAuthenticator(t *Tokens, sessions SessionChecker) *Authenticator {
	return &Authenticator{tokens: t, sessions: sessions}
}

// Tokens returns the token service (identity issues tokens with it).
func (a *Authenticator) Tokens() *Tokens { return a.tokens }

// Authenticate verifies a bearer token. Errors are problems: 401
// unauthorized for any bad, expired or revoked token; 403 account_blocked for
// a blocked user; 500 internal when the session store fails.
func (a *Authenticator) Authenticate(ctx context.Context, raw string) (Principal, error) {
	p, err := a.tokens.Verify(raw)
	if err != nil {
		return Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	if a.sessions == nil {
		return p, nil
	}
	st, err := a.sessions.CheckSession(ctx, p.UserID, p.SessionID, p.App)
	if err != nil {
		return Principal{}, problem.Wrap(genproblems.Internal, err)
	}
	switch {
	case st.Blocked:
		return Principal{}, problem.New(genproblems.AccountBlocked, "")
	case !st.Active:
		return Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	if p.App == AppAdmin {
		p.Roles = slices.Clone(st.Roles)
		p.StepUpAt = st.StepUpAt
		// The token's amr is a claim; the session row is the truth. An
		// otp+totp token whose sign-in holds no TOTP verification (it was
		// reset) is only an OTP session.
		if p.AMR == AMROTPTOTP && st.StepUpAt.IsZero() {
			p.AMR = AMROTP
		}
	}
	return p, nil
}

// ErrBadOrigin is returned by CheckRefreshOrigin.
var ErrBadOrigin = errors.New("authn: refresh from a foreign origin or without X-TZ-Client")

// CheckRefreshOrigin is the CSRF rule for /v1/auth/refresh and /logout
// (AU-16): Origin must be one of allowed (config.CORSOrigins) and be app's own
// origin (app., business. or admin.), and X-TZ-Client must be present.
func CheckRefreshOrigin(app App, origin, client string, allowed []string) error {
	if strings.TrimSpace(client) == "" || origin == "" {
		return ErrBadOrigin
	}
	if !slices.Contains(allowed, origin) {
		return ErrBadOrigin
	}
	u, err := url.Parse(origin)
	if err != nil || !strings.HasPrefix(u.Host, app.hostPrefix()) {
		return ErrBadOrigin
	}
	return nil
}

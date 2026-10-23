package mw

// Slot 11 · authn · owner W1-01 (02 §2.4, §3; 04 §1.3; D-14, D-100).
//
// Contract for every later slot and handler:
//
//   - A request without Authorization is anonymous: authn.FromContext reports
//     false and reqctx.PrincipalFrom is reqctx.Anonymous.
//   - "Authorization: Bearer <jwt>" with a valid, unexpired token whose session
//     is live sets the principal (authn.WithPrincipal). Anything else in
//     Authorization (another scheme, two headers, a bad, expired or revoked
//     token) is 401 unauthorized on every path, public ones included, so a
//     client always learns to refresh. A blocked user is 403 account_blocked.
//   - Except on the cookie routes of sign-in (cookieAuthPaths: OTP send and
//     verify, refresh, logout): there Authorization is ignored and the caller
//     is anonymous. They authenticate by the refresh cookie or not at all, and
//     a client that refreshes because its bearer expired often still sends
//     that bearer; a 401 there would stop it from ever refreshing.
//   - Namespaces, before chi and the validator: /v1/me, /v1/biz and /v1/admin
//     refuse an anonymous caller with 401. /v1/biz needs a business-app token
//     and /v1/admin an admin-app token with amr otp+totp (AU-11), else 403
//     forbidden. An admin-app token without TOTP reaches /v1/auth/* only.
//   - Per-action permission is slot 15 (authz), not here.

import (
	"context"
	"net/http"
	"strings"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/httpx/problem"
)

// Authenticator verifies a bearer token (authn.Authenticator implements it).
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (authn.Principal, error)
}

// Authn resolves the caller. a nil means no tokens can be verified: every
// request is anonymous and any Authorization header is 401.
func Authn(a Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			p := authn.Principal{}
			if vals := r.Header.Values("Authorization"); len(vals) > 0 && !cookieAuthPaths[r.URL.Path] {
				tok, ok := bearer(vals)
				if !ok || a == nil {
					problem.Write(w, r, problem.New(genproblems.Unauthorized, ""))
					return
				}
				got, err := a.Authenticate(ctx, tok)
				if err != nil {
					problem.Write(w, r, err)
					return
				}
				p = got
			}
			if err := guardNamespace(r.URL.Path, p); err != nil {
				problem.Write(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(authn.WithPrincipal(ctx, p)))
		})
	}
}

// cookieAuthPaths are the routes that never read a bearer token. Matched
// exactly: slot 8 (pathguard) has refused any path two routers could read
// differently, and a lookalike (a trailing slash) keeps the bearer check.
// /v1/auth/* routes that need the bearer (admin TOTP, phase B) are not here.
var cookieAuthPaths = map[string]bool{
	"/v1/auth/otp/send":   true,
	"/v1/auth/otp/verify": true,
	"/v1/auth/refresh":    true,
	"/v1/auth/logout":     true,
}

// bearer extracts the token from exactly one "Bearer <token>" header.
func bearer(vals []string) (string, bool) {
	if len(vals) != 1 {
		return "", false
	}
	scheme, tok, ok := strings.Cut(vals[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	tok = strings.TrimSpace(tok)
	if tok == "" || len(tok) > authn.MaxTokenLen {
		return "", false
	}
	return tok, true
}

func under(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// guardNamespace applies the path-level rules of the slot contract above.
func guardNamespace(path string, p authn.Principal) error {
	signedIn := !p.IsAnonymous()
	needs := under(path, "/v1/me") || under(path, "/v1/biz") || under(path, "/v1/admin")
	switch {
	case needs && !signedIn:
		return problem.New(genproblems.Unauthorized, "")
	case !signedIn:
		return nil
	case p.App == authn.AppAdmin && p.AMR != authn.AMROTPTOTP && !under(path, "/v1/auth"):
		// An admin who has not passed TOTP may only finish signing in (AU-11).
		return problem.New(genproblems.Forbidden, "")
	case under(path, "/v1/biz") && p.App != authn.AppBusiness:
		return problem.New(genproblems.Forbidden, "")
	case under(path, "/v1/admin") && (p.App != authn.AppAdmin || p.AMR != authn.AMROTPTOTP):
		return problem.New(genproblems.Forbidden, "")
	}
	return nil
}

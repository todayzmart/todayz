package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/identitydb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Revocation reasons (ck_sessions_reason).
const (
	reasonRotated = "rotated"
	reasonReuse   = "reuse"
	reasonLogout  = "logout"
	reasonAdmin   = "admin"
)

// maxUserAgent is sessions.user_agent's limit in characters.
const maxUserAgent = 256

// refreshTokenB64Len is the length of a refresh token in a cookie
// (32 bytes, base64url without padding).
var refreshTokenB64Len = base64.RawURLEncoding.EncodedLen(refreshTokenLen)

// newRefreshToken returns a random refresh token and its SHA-256. The token
// is 256 random bits, so a plain hash is enough to store it (no key needed).
func newRefreshToken() (token string, hash []byte, err error) {
	b := make([]byte, refreshTokenLen)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("identity: refresh token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashRefresh(token), nil
}

func hashRefresh(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// validRefreshToken reports whether s has the shape of a refresh token, so a
// junk cookie never reaches the database.
func validRefreshToken(s string) bool {
	if len(s) != refreshTokenB64Len {
		return false
	}
	_, err := base64.RawURLEncoding.Strict().DecodeString(s)
	return err == nil
}

// cleanUserAgent keeps at most 256 valid UTF-8 characters; empty is NULL.
func cleanUserAgent(ua string) *string {
	ua = strings.TrimSpace(strings.ToValidUTF8(ua, ""))
	if ua == "" {
		return nil
	}
	if utf8.RuneCountInString(ua) > maxUserAgent {
		ua = string([]rune(ua)[:maxUserAgent])
	}
	return &ua
}

// auditEntry is an identity audit row with the actor kind of the app.
func auditEntry(app authn.App, userID ids.ID, action string, after any) audit.Entry {
	kind := audit.ActorUser
	switch app {
	case authn.AppBusiness:
		kind = audit.ActorStaff
	case authn.AppAdmin:
		kind = audit.ActorAdmin
	}
	return audit.Entry{ActorKind: kind, ActorID: userID, Action: action, SubjectType: "user", SubjectID: userID.String(), After: after}
}

// RefreshInput is one refresh or logout call.
type RefreshInput struct {
	App       authn.App
	Token     string // the app's refresh cookie; empty when absent
	UserAgent string
}

// Refresh rotates a refresh token (AU-07): the presented row is revoked as
// rotated and a new row joins the same family. Presenting a token that was
// already rotated revokes the whole family (reuse detection) and answers 401.
//
// Customer and business sessions slide (each refresh lasts the full TTL
// again); an admin session keeps its first expiry (12 h absolute).
// TODO(lead): 01 §2 does not say whether the 30-day customer/business
// lifetime slides; sliding is assumed, admin is absolute (the conservative
// reading for the privileged app).
func (s *Service) Refresh(ctx context.Context, in RefreshInput) (SignIn, error) {
	if err := s.readyTokens(); err != nil {
		return SignIn{}, unavailable(ctx, err)
	}
	if !validRefreshToken(in.Token) {
		return SignIn{}, problem.New(genproblems.Unauthorized, "")
	}
	var (
		out     SignIn
		outcome error
		family  ids.ID
		roles   []string
		amr     = authn.AMROTP
	)
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		now := s.clock.Now().UTC()
		row, err := q.GetSessionByRefreshHashForUpdate(ctx, hashRefresh(in.Token))
		switch {
		case db.IsNoRows(err):
			outcome = problem.New(genproblems.Unauthorized, "")
			return nil
		case err != nil:
			return err
		}
		userID, _ := ids.FromUUID(row.UserID)
		fam, _ := ids.FromUUID(row.FamilyID)
		if row.App != string(in.App) {
			outcome = problem.New(genproblems.Unauthorized, "")
			return nil
		}
		if row.RevokedAt != nil {
			if row.RevokedReason != nil && *row.RevokedReason == reasonRotated {
				// Reuse: someone holds an old token. End the sign-in everywhere.
				n, err := q.RevokeFamily(ctx, identitydb.RevokeFamilyParams{Now: now, Reason: reasonReuse, FamilyID: row.FamilyID})
				if err != nil {
					return err
				}
				if n > 0 {
					telemetry.Logger(ctx).WarnContext(ctx, "identity: refresh token reuse; session family revoked", "app", row.App)
					if _, err := s.audit.Write(ctx, auditEntry(in.App, userID, "identity.refresh_reuse", map[string]any{
						"app": row.App, "sessionId": fam.String(), "revoked": n,
					})); err != nil {
						return err
					}
				}
			}
			outcome = problem.New(genproblems.Unauthorized, "")
			return nil
		}
		if !now.Before(row.ExpiresAt) {
			outcome = problem.New(genproblems.Unauthorized, "")
			return nil
		}
		if refusal := statusRefusal(row.UserStatus); refusal != nil {
			outcome = refusal
			return nil //nolint:nilerr // the refusal answers after the transaction
		}
		if in.App == authn.AppAdmin {
			a, err := q.GetAdminUser(ctx, row.UserID)
			if db.IsNoRows(err) || (err == nil && a.DisabledAt != nil) {
				if _, err := q.RevokeFamily(ctx, identitydb.RevokeFamilyParams{Now: now, Reason: reasonAdmin, FamilyID: row.FamilyID}); err != nil {
					return err
				}
				outcome = problem.New(genproblems.Forbidden, "")
				return nil
			}
			if err != nil {
				return err
			}
			roles = a.Roles
			if row.TotpVerifiedAt != nil {
				amr = authn.AMROTPTOTP
			}
		}
		if err := q.RevokeSession(ctx, identitydb.RevokeSessionParams{Now: now, Reason: reasonRotated, ID: row.ID}); err != nil {
			return err
		}
		refresh, refreshHash, err := newRefreshToken()
		if err != nil {
			return err
		}
		exp := now.Add(in.App.RefreshTTL())
		if in.App == authn.AppAdmin {
			exp = row.ExpiresAt.UTC() // absolute: the family's first expiry
		}
		if err := q.InsertSession(ctx, identitydb.InsertSessionParams{
			ID: ids.UUID(ids.New(now)), UserID: row.UserID, App: row.App, FamilyID: row.FamilyID,
			RefreshHash: refreshHash, ExpiresAt: exp, DeviceID: row.DeviceID,
			UserAgent: cleanUserAgent(in.UserAgent), IpHash: s.hash.IP(reqctx.ClientIP(ctx)), Now: now,
			TotpVerifiedAt: row.TotpVerifiedAt,
		}); err != nil {
			return err
		}
		out = SignIn{UserID: userID, RefreshToken: refresh, RefreshExpiresAt: exp}
		family = fam
		return nil
	})
	if err != nil {
		return SignIn{}, asProblem(err)
	}
	if outcome != nil {
		return SignIn{}, outcome
	}
	tok, exp, err := s.tokens.Issue(authn.Principal{UserID: out.UserID, App: in.App, SessionID: family, Roles: roles, AMR: amr})
	if err != nil {
		return SignIn{}, problem.Wrap(genproblems.Internal, err)
	}
	out.AccessToken, out.ExpiresAt = tok, exp
	return out, nil
}

// Logout ends the sign-in (session family) of the presented refresh token.
// An unknown or already revoked token is still a successful logout: the
// cookie is cleared either way and nothing is revealed.
func (s *Service) Logout(ctx context.Context, in RefreshInput) error {
	if err := s.ready(); err != nil {
		return unavailable(ctx, err)
	}
	if in.Token == "" {
		return problem.New(genproblems.Unauthorized, "")
	}
	if !validRefreshToken(in.Token) {
		return nil
	}
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		now := s.clock.Now().UTC()
		row, err := q.GetSessionByRefreshHashForUpdate(ctx, hashRefresh(in.Token))
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.App != string(in.App) {
			return nil
		}
		n, err := q.RevokeFamily(ctx, identitydb.RevokeFamilyParams{Now: now, Reason: reasonLogout, FamilyID: row.FamilyID})
		if err != nil || n == 0 {
			return err
		}
		userID, _ := ids.FromUUID(row.UserID)
		fam, _ := ids.FromUUID(row.FamilyID)
		_, err = s.audit.Write(ctx, auditEntry(in.App, userID, "identity.sign_out", map[string]any{"app": row.App, "sessionId": fam.String()}))
		return err
	})
	if err != nil {
		return asProblem(err)
	}
	return nil
}

// CheckSession implements authn.SessionChecker: is the sign-in (family) of an
// access token still live, and may its user still act? One indexed read
// (two for an admin).
func (s *Service) CheckSession(ctx context.Context, userID, sessionID ids.ID, app authn.App) (authn.SessionState, error) {
	if s.db == nil {
		return authn.SessionState{}, errUnavailable
	}
	q := identitydb.New(s.db.Q(ctx))
	now := s.clock.Now().UTC()
	st, err := q.GetFamilyState(ctx, identitydb.GetFamilyStateParams{
		Now: now, FamilyID: ids.UUID(sessionID), UserID: ids.UUID(userID), App: string(app),
	})
	if db.IsNoRows(err) {
		return authn.SessionState{}, nil
	}
	if err != nil {
		return authn.SessionState{}, fmt.Errorf("identity: session check: %w", err)
	}
	if st.Status != "active" {
		return authn.SessionState{Blocked: true}, nil
	}
	if !st.Live {
		return authn.SessionState{}, nil
	}
	out := authn.SessionState{Active: true}
	if app == authn.AppAdmin {
		a, err := q.GetAdminUser(ctx, ids.UUID(userID))
		if db.IsNoRows(err) || (err == nil && a.DisabledAt != nil) {
			return authn.SessionState{}, nil
		}
		if err != nil {
			return authn.SessionState{}, fmt.Errorf("identity: admin check: %w", err)
		}
		out.Roles = a.Roles
		if st.HasTotp {
			out.StepUpAt = st.TotpVerifiedAt
		}
	}
	return out, nil
}

// Sessions is the session checker alone, for the authenticator that the
// router builds before the identity routes mount.
type Sessions struct{ s *Service }

// NewSessions returns a checker on d. A nil d answers every check with an
// error (the authenticator then answers 500), so build it only with a database.
func NewSessions(d *db.DB, clk clock.Clock) *Sessions {
	return &Sessions{s: &Service{db: d, clock: clk}}
}

// CheckSession implements authn.SessionChecker.
func (c *Sessions) CheckSession(ctx context.Context, userID, sessionID ids.ID, app authn.App) (authn.SessionState, error) {
	return c.s.CheckSession(ctx, userID, sessionID, app)
}

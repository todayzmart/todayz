package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
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

// RotationGrace is how long after its rotation a refresh token still gets
// the lost-response treatment instead of reuse detection. A refresh whose
// response was lost (a reload or navigation mid-request, a dropped mobile
// connection) leaves the client holding the token the server just rotated.
//
// Within the window (measured from that rotation, never extended), the retry
// is answered with the very response it lost (D-239): the rotation recorded
// its successor's row and a salt, the successor's token is derived from the
// presented token (successorToken), and if that successor has never been
// presented it is returned again, with a fresh access token, and nothing is
// written or audited. Every retry gets the same token, so two tabs or two
// concurrent retries all converge on one cookie. When the successor has been
// used (or revoked, or the row predates D-239 or was rotated by the TOTP
// flows, which record no successor), the retry is a plain 401 that keeps the
// cookie (graceRetry), as before.
//
// Past the window, presenting a rotated token is reuse: the family is
// revoked and audited ("identity.refresh_reuse").
//
// The costs: a thief who rotates a stolen token first and is followed by the
// owner within this window is not detected at once (the owner gets the
// thief's successor, or a 401 once the thief has used it). And a thief
// holding a copy who presents it within this window of the owner's rotation,
// before the owner's client uses the successor, receives the successor too.
// Detection is delayed, not lost: the next time either party presents a
// token the other has rotated, past the window, the family is revoked. The
// window is kept short for that reason. A security constant, not a product
// rule (01 §17 does not cover it), so it is code and not a platform setting.
const RotationGrace = 30 * time.Second

// graceRetry is the 401 for a token rotated RotationGrace ago or less. The
// handler answers it without clearing the cookie: another tab's refresh may
// already have set the newer one, and clearing it would sign that tab out.
type graceRetry struct{ p *problem.Error }

func (g graceRetry) Error() string { return g.p.Error() }
func (g graceRetry) Unwrap() error { return g.p }

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

// successorSaltLen is sessions.successor_salt's length (ck_sessions_successor_salt).
const successorSaltLen = 32

// successorToken derives the refresh token that a rotation of parent issues:
// HMAC-SHA256 under PII_HASH_KEY of the parent token and a salt the rotation
// draws and stores (D-239). Only someone who presents parent can recompute
// it, which is what lets a lost refresh response be replayed without storing
// any token: the database keeps the salt and, as for every token, only the
// successor's SHA-256. The output is 32 bytes, so the token has the shape of
// a random one (validRefreshToken). A parent is fixed-length base64url (no
// ':'), so the input is unambiguous.
func (s *Service) successorToken(parent string, salt []byte) string {
	return base64.RawURLEncoding.EncodeToString(
		s.hash.Sum("refresh-successor:v1:" + parent + ":" + base64.RawURLEncoding.EncodeToString(salt)))
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
// rotated, recording the row that replaces it and the salt that row's token
// was derived with (successorToken), and the new row joins the same family.
// Presenting a token that was already rotated revokes the whole family (reuse
// detection) and answers 401, unless it was rotated RotationGrace ago or
// less: then the lost response is replayed if its successor is still unused
// (replaySuccessor), and otherwise it is only a 401 (graceRetry).
//
// Customer and business sessions slide (each refresh lasts the full TTL
// again); an admin session keeps its first expiry (12 h absolute).
// TODO(lead): 01 §2 does not say whether the 30-day customer/business
// lifetime slides; sliding is assumed, admin is absolute (the conservative
// reading for the privileged app).
//
// Locks: the users row FOR NO KEY UPDATE first (lockSessionUser), then the
// session row, then (replay only) its successor's row. Every path that
// revokes sessions in bulk (logout, logout-all, this function's reuse branch,
// the TOTP lockout, deletion) and every path that rotates (refresh, TOTP
// elevation) holds that lock, so a rotation either commits before a
// revocation starts (and its new row is revoked with the rest) or runs after
// it (and finds its token revoked). Without that, a revocation's UPDATE,
// whose snapshot predates the rotation's insert, would miss the new row. The
// same lock serialises a replay against a refresh of the successor: the
// replay sees the successor either live (and returns it) or rotated (and
// answers graceRetry), never half-rotated. Refreshes of one user therefore
// run one at a time; the lock is held only for a few indexed statements and
// the audit insert (the access token is signed after commit).
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
		if found, err := lockSessionUser(ctx, q, in.Token); err != nil || !found {
			if err == nil {
				outcome = problem.New(genproblems.Unauthorized, "")
			}
			return err
		}
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
		// The live row the checks below apply to: the presented one or, on a
		// replay, its successor.
		var (
			replay         *replayed
			expiresAt      = row.ExpiresAt
			totpVerifiedAt = row.TotpVerifiedAt
		)
		rotated := row.RevokedAt != nil && row.RevokedReason != nil && *row.RevokedReason == reasonRotated
		if rotated && now.Sub(*row.RevokedAt) <= RotationGrace {
			r, err := s.replaySuccessor(ctx, q, row, in.Token)
			if err != nil {
				return err
			}
			if r == nil {
				// A retry of a refresh whose successor is already in use
				// (another tab's refresh set the newer cookie): refuse it,
				// end nothing (RotationGrace).
				telemetry.Logger(ctx).InfoContext(ctx, "identity: rotated refresh token presented within the grace window", "app", row.App)
				outcome = graceRetry{problem.New(genproblems.Unauthorized, "")}
				return nil
			}
			replay, expiresAt, totpVerifiedAt = r, r.expiresAt, r.totpVerifiedAt
		} else if row.RevokedAt != nil {
			if rotated {
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
		if !now.Before(expiresAt) {
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
			if totpCurrent(a.TotpEnabledAt, totpVerifiedAt) {
				amr = authn.AMROTPTOTP
			}
		}
		family = fam
		if replay != nil {
			// The lost response again: the same refresh token and expiry, a
			// new access token. Nothing is written, so any number of retries
			// inside the window converge on this one token.
			telemetry.Logger(ctx).InfoContext(ctx, "identity: lost refresh response replayed within the grace window", "app", row.App)
			out = SignIn{UserID: userID, RefreshToken: replay.token, RefreshExpiresAt: expiresAt.UTC()}
			return nil
		}
		salt := make([]byte, successorSaltLen)
		if _, err := rand.Read(salt); err != nil {
			return fmt.Errorf("identity: refresh salt: %w", err)
		}
		next := ids.UUID(ids.New(now))
		n, err := q.RotateSession(ctx, identitydb.RotateSessionParams{Now: now, SuccessorID: next, SuccessorSalt: salt, ID: row.ID})
		if err != nil {
			return err
		}
		if n != 1 {
			// The row is locked and was live, so this cannot happen.
			return fmt.Errorf("identity: rotate session: %d rows", n)
		}
		refresh := s.successorToken(in.Token, salt)
		exp := now.Add(in.App.RefreshTTL())
		if in.App == authn.AppAdmin {
			exp = row.ExpiresAt.UTC() // absolute: the family's first expiry
		}
		if err := q.InsertSession(ctx, identitydb.InsertSessionParams{
			ID: next, UserID: row.UserID, App: row.App, FamilyID: row.FamilyID,
			RefreshHash: hashRefresh(refresh), ExpiresAt: exp, DeviceID: row.DeviceID,
			UserAgent: cleanUserAgent(in.UserAgent), IpHash: s.hash.IP(reqctx.ClientIP(ctx)), Now: now,
			TotpVerifiedAt: row.TotpVerifiedAt,
		}); err != nil {
			return err
		}
		out = SignIn{UserID: userID, RefreshToken: refresh, RefreshExpiresAt: exp}
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

// replayed is a rotated token's still-unused successor (replaySuccessor).
type replayed struct {
	token          string
	expiresAt      time.Time
	totpVerifiedAt *time.Time
}

// replaySuccessor returns the successor of the rotated row presented with
// token when the lost response can be replayed (D-239): the rotation
// recorded a successor, that row still exists and is live (never presented,
// never revoked), it belongs to the same family, user and app, and its hash
// matches the token derived from the presented one. Otherwise it returns nil
// and the caller answers graceRetry. It locks the successor's row (after the
// users row and the presented row) and writes nothing.
func (s *Service) replaySuccessor(ctx context.Context, q *identitydb.Queries, row identitydb.GetSessionByRefreshHashForUpdateRow, token string) (*replayed, error) {
	if !row.SuccessorID.Valid || len(row.SuccessorSalt) != successorSaltLen {
		return nil, nil // rotated before D-239, or by the TOTP flows
	}
	succ, err := q.GetSessionForUpdate(ctx, row.SuccessorID)
	switch {
	case db.IsNoRows(err):
		return nil, nil // purged
	case err != nil:
		return nil, err
	}
	if succ.RevokedAt != nil || succ.FamilyID != row.FamilyID || succ.UserID != row.UserID || succ.App != row.App {
		return nil, nil
	}
	derived := s.successorToken(token, row.SuccessorSalt)
	if subtle.ConstantTimeCompare(hashRefresh(derived), succ.RefreshHash) != 1 {
		return nil, nil // PII_HASH_KEY changed since the rotation
	}
	return &replayed{token: derived, expiresAt: succ.ExpiresAt, totpVerifiedAt: succ.TotpVerifiedAt}, nil
}

// lockSessionUser locks the users row of a refresh token's session (LockUser,
// FOR NO KEY UPDATE) before the session row is locked, for refresh, logout
// and logout-all. found is false for an unknown token or user. The first read
// takes no lock; a session's user never changes, so it is safe. One lock mode
// for all of them: a transaction that took FOR SHARE and then upgraded would
// deadlock with another doing the same.
func lockSessionUser(ctx context.Context, q *identitydb.Queries, token string) (found bool, err error) {
	uid, err := q.GetSessionUserByRefreshHash(ctx, hashRefresh(token))
	switch {
	case db.IsNoRows(err):
		return false, nil
	case err != nil:
		return false, err
	}
	_, err = q.LockUser(ctx, uid)
	switch {
	case db.IsNoRows(err):
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

// totpCurrent reports whether a sign-in's TOTP verification at verifiedAt
// still counts: the admin is enrolled now (totp_enabled_at) and verified at
// or after that enrolment. A reset (W3-05) clears totp_enabled_at, so no
// verification survives it, nor a later re-enrolment.
func totpCurrent(enabledAt, verifiedAt *time.Time) bool {
	return enabledAt != nil && verifiedAt != nil && !verifiedAt.Before(*enabledAt)
}

// Logout ends the sign-in (session family) of the presented refresh token.
// An unknown or already revoked token is still a successful logout: the
// cookie is cleared either way and nothing is revealed. It locks the users
// row first (lockSessionUser), so a concurrent refresh of the family cannot
// insert a row its revocation misses.
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
		if found, err := lockSessionUser(ctx, q, in.Token); err != nil || !found {
			return err
		}
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
		if st.HasTotp && totpCurrent(a.TotpEnabledAt, &st.TotpVerifiedAt) {
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

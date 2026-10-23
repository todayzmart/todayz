package identity

import (
	"context"
	"crypto/hmac"
	"errors"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/identitydb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// More revocation reasons (ck_sessions_reason).
const (
	reasonLogoutAll = "logout_all"
	reasonDeleted   = "deleted"
)

// LogoutAll ends every sign-in of the token's user in the token's app (04
// §2.1, AU-13); the user's sign-ins in the other apps stay. It authenticates
// by the refresh cookie like refresh and logout, and refuses a token that is
// not live: unlike a plain logout, signing everyone out needs proof of a
// current sign-in, so an old stolen token cannot sign the owner out
// everywhere. It returns how many session rows it revoked.
func (s *Service) LogoutAll(ctx context.Context, in RefreshInput) (int64, error) {
	if err := s.ready(); err != nil {
		return 0, unavailable(ctx, err)
	}
	if !validRefreshToken(in.Token) {
		return 0, problem.New(genproblems.Unauthorized, "")
	}
	var (
		revoked int64
		outcome error
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
		if row.App != string(in.App) {
			outcome = problem.New(genproblems.Unauthorized, "")
			return nil
		}
		if row.RevokedAt != nil {
			rotated := row.RevokedReason != nil && *row.RevokedReason == reasonRotated
			if rotated && now.Sub(*row.RevokedAt) <= RotationGrace {
				// Another tab rotated it a moment ago; keep that tab's cookie.
				outcome = graceRetry{problem.New(genproblems.Unauthorized, "")}
				return nil
			}
			outcome = problem.New(genproblems.Unauthorized, "")
			return nil
		}
		if !now.Before(row.ExpiresAt) {
			outcome = problem.New(genproblems.Unauthorized, "")
			return nil
		}
		revoked, err = q.RevokeUserApp(ctx, identitydb.RevokeUserAppParams{
			Now: now, Reason: reasonLogoutAll, UserID: row.UserID, App: row.App,
		})
		if err != nil {
			return err
		}
		userID, _ := ids.FromUUID(row.UserID)
		_, err = s.audit.Write(ctx, auditEntry(in.App, userID, "identity.sign_out_all", map[string]any{
			"app": row.App, "revoked": revoked,
		}))
		return err
	})
	if err != nil {
		return 0, asProblem(err)
	}
	if outcome != nil {
		return 0, outcome
	}
	return revoked, nil
}

// DeleteInput is one DELETE /v1/me call: the step_up OTP the user asked for
// with POST /v1/auth/otp/send {purpose: step_up}.
type DeleteInput struct {
	RequestID string
	Code      string
}

// DeleteAccount deletes the principal's customer account (01 §2, D-31,
// AU-18), confirmed with a fresh SMS OTP to the account's own number:
//
//   - the code is checked like a sign-in code (five tries, five minutes), but
//     must be a step_up code of the customer app sent to this user's phone;
//     a wrong code is 422 code_invalid with attemptsLeft, and the attempt
//     count commits;
//   - every CanDeleteAccount runs in a savepoint; a refusal answers as is,
//     changes nothing and leaves the code unused;
//   - then, in the same transaction, the code is used, every OnAccountDelete
//     runs, and identity anonymises the user, deletes the phone identity and
//     addresses, scrubs devices and revokes every session of every app.
//
// The users row stays (status deleted, no name), so the orders, ledger and
// audit rows that point at it keep their history without PII; the phone is
// free to sign up again as a new user.
func (s *Service) DeleteAccount(ctx context.Context, p authn.Principal, in DeleteInput) error {
	if err := s.ready(); err != nil {
		return unavailable(ctx, err)
	}
	if p.IsAnonymous() {
		return problem.New(genproblems.Unauthorized, "")
	}
	if p.App != authn.AppCustomer {
		return problem.New(genproblems.Forbidden, "")
	}
	reqID, err := ids.Parse(in.RequestID)
	if err != nil {
		return problem.New(genproblems.OtpExpired, "")
	}
	hooks := s.deletionHooks()
	var (
		outcome error
		revoked int64
	)
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		now := s.clock.Now().UTC()
		uid := ids.UUID(p.UserID)

		status, err := q.LockUser(ctx, uid)
		switch {
		case db.IsNoRows(err):
			return problem.New(genproblems.Unauthorized, "")
		case err != nil:
			return err
		}
		if refusal := statusRefusal(status); refusal != nil {
			return refusal
		}
		phoneHash, err := q.GetPhoneHashByUser(ctx, uid)
		switch {
		case db.IsNoRows(err):
			return problem.New(genproblems.Unauthorized, "") // no number to confirm with
		case err != nil:
			return err
		}

		row, err := q.GetOtpRequestForUpdate(ctx, ids.UUID(reqID))
		switch {
		case db.IsNoRows(err):
			return problem.New(genproblems.OtpExpired, "")
		case err != nil:
			return err
		}
		if row.ConsumedAt != nil || row.Purpose != PurposeStepUp || row.App != string(authn.AppCustomer) ||
			!hmac.Equal(row.PhoneHash, phoneHash) {
			return problem.New(genproblems.OtpExpired, "")
		}
		if row.Attempts >= OTPMaxAttempts {
			return problem.New(genproblems.OtpLocked, "")
		}
		if !now.Before(row.ExpiresAt) {
			return problem.New(genproblems.OtpExpired, "")
		}
		attempts, err := q.RecordOtpAttempt(ctx, row.ID)
		if err != nil {
			return err
		}
		match := hmac.Equal(s.codeHash(reqID, in.Code), row.CodeHash)
		if !match && s.allowTestCode {
			plain, err := s.seal.Open("phone", row.PhoneEnc)
			if err != nil {
				return err
			}
			match = pii.IsReservedTestPhone(string(plain)) && hmac.Equal([]byte(in.Code), []byte(testCode))
		}
		if !match {
			outcome = problem.New(genproblems.CodeInvalid, "").WithExtension("attemptsLeft", max(OTPMaxAttempts-int(attempts), 0))
			return nil
		}

		// TODO(lead): deleting an admin's or a business owner's account is not
		// specified; an active admin keeps their account (a super_admin
		// disables them first). Business owners: W1-04 registers a check.
		if a, err := q.GetAdminUser(ctx, uid); err == nil && a.DisabledAt == nil {
			outcome = problem.New(genproblems.Conflict, "")
			return nil
		} else if err != nil && !db.IsNoRows(err) {
			return err
		}

		// The checks: a refusal rolls back their savepoint only, so the
		// attempt count above still commits and the code stays unused.
		var refusal error
		if err := s.db.InTx(ctx, func(ctx context.Context) error {
			for _, c := range hooks.Can {
				if err := c.CanDeleteAccount(ctx, p.UserID); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			var pe *problem.Error
			if !errors.As(err, &pe) {
				return err // a failed check is a 500, not a refusal
			}
			refusal = pe
		}
		if refusal != nil {
			outcome = refusal
			return nil //nolint:nilerr // the refusal answers after the transaction, so the attempt count commits
		}

		if err := q.ConsumeOtpRequest(ctx, identitydb.ConsumeOtpRequestParams{Now: now, ID: row.ID}); err != nil {
			return err
		}
		for _, h := range hooks.On {
			if err := h.OnAccountDelete(ctx, p.UserID); err != nil {
				return err
			}
		}
		if err := q.AnonymiseUser(ctx, identitydb.AnonymiseUserParams{Now: now, ID: uid}); err != nil {
			return err
		}
		if err := q.DeletePhoneIdentity(ctx, uid); err != nil {
			return err
		}
		if err := q.DeleteAddresses(ctx, uid); err != nil {
			return err
		}
		if err := q.ScrubDevices(ctx, uid); err != nil {
			return err
		}
		revoked, err = q.RevokeUserSessions(ctx, identitydb.RevokeUserSessionsParams{Now: now, Reason: reasonDeleted, UserID: uid})
		if err != nil {
			return err
		}
		_, err = s.audit.Write(ctx, auditEntry(authn.AppCustomer, p.UserID, "identity.account_deleted", map[string]any{
			"revoked": revoked, "checks": len(hooks.Can), "hooks": len(hooks.On),
		}))
		return err
	})
	if err != nil {
		return asProblem(err)
	}
	return outcome
}

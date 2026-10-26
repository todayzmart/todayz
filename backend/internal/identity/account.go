package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

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
//
// It locks the users row (LockUser) before the session row, like every mass
// revocation, so a concurrent refresh of another device of the user cannot
// slip a new live row past the revocation (see Refresh).
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
//   - every CanDeleteAccount runs in a savepoint (identity's own first: no
//     deletion while no-show strikes limit the account, D-106); a refusal
//     answers as is, changes nothing, leaves the code unused and does not
//     count as an attempt (the code was right);
//   - then, in the same transaction, the code is used, every OnAccountDelete
//     runs, and identity anonymises the user, deletes the phone identity and
//     addresses, scrubs devices (push keys, and a new device id in place of
//     the browser's), clears the user agent and IP hash of every session,
//     revokes every session of every app and, for a disabled admin, drops
//     the TOTP secret and recovery codes.
//
// otp_requests.phone_enc is NOT NULL (the verify step needs it), so the
// sealed phone of this user's codes stays until the 24-hour OTP purge.
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

		// Only a step_up request is locked: a sign-in verifying a login
		// request holds that row and then waits for this user's row, which
		// this transaction already holds, so locking it here would deadlock.
		row, err := q.GetStepUpOtpRequestForUpdate(ctx, ids.UUID(reqID))
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
		match := hmac.Equal(s.codeHash(reqID, in.Code), row.CodeHash)
		if !match && s.allowTestCode {
			plain, err := s.seal.Open("phone", row.PhoneEnc)
			if err != nil {
				return err
			}
			match = pii.IsReservedTestPhone(string(plain)) && hmac.Equal([]byte(in.Code), []byte(testCode))
		}
		if !match {
			// Only a wrong code uses up a try; a right one refused below
			// does not, so a refusal never locks the owner out of the code.
			attempts, err := q.RecordOtpAttempt(ctx, row.ID)
			if err != nil {
				return err
			}
			outcome = problem.New(genproblems.CodeInvalid, "").WithExtension("attemptsLeft", max(OTPMaxAttempts-int(attempts), 0))
			return nil
		}

		// Business owners: W1-04 registers a check; an active admin keeps
		// their account (a super_admin disables them first; lead: 409, Q2).
		disabledAdmin := false
		if a, err := q.GetAdminUser(ctx, uid); err == nil {
			if a.DisabledAt == nil {
				return problem.New(genproblems.Conflict, "")
			}
			disabledAdmin = true
		} else if !db.IsNoRows(err) {
			return err
		}

		// The checks, in a savepoint: a refusal answers as is and nothing of
		// the deletion is written.
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
			return refusal
		}

		if err := q.ConsumeOtpRequest(ctx, identitydb.ConsumeOtpRequestParams{Now: now, ID: row.ID}); err != nil {
			return err
		}
		for _, h := range hooks.On {
			if err := h.OnAccountDelete(ctx, p.UserID); err != nil {
				return err
			}
		}
		if err := s.removePhotos(ctx, q, p.UserID); err != nil {
			return err
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
		if err := s.scrubDevices(ctx, q, uid); err != nil {
			return err
		}
		revoked, err = q.RevokeUserSessions(ctx, identitydb.RevokeUserSessionsParams{Now: now, Reason: reasonDeleted, UserID: uid})
		if err != nil {
			return err
		}
		if err := q.ScrubSessionPII(ctx, uid); err != nil {
			return err
		}
		if disabledAdmin {
			if err := q.ClearAdminTotp(ctx, uid); err != nil {
				return err
			}
			if err := q.DeleteRecoveryCodes(ctx, uid); err != nil {
				return err
			}
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

// scrubDevices drops the push keys and counter secrets of the user's devices
// and replaces each device_key: it is the browser's own id (localStorage), so
// a new account signed up in that browser would otherwise share it with the
// deleted one. The replacement is 128 random bits in the ULID form
// ck_devices_key accepts.
func (s *Service) scrubDevices(ctx context.Context, q *identitydb.Queries, uid pgtype.UUID) error {
	if err := q.ScrubDevices(ctx, uid); err != nil {
		return err
	}
	devs, err := q.ListDeviceIDs(ctx, uid)
	if err != nil {
		return err
	}
	for _, id := range devs {
		var key ids.ID
		if _, err := rand.Read(key[:]); err != nil {
			return fmt.Errorf("identity: device key: %w", err)
		}
		if err := q.SetDeviceKey(ctx, identitydb.SetDeviceKeyParams{DeviceKey: key.String(), ID: id}); err != nil {
			return err
		}
	}
	return nil
}

// canDeleteWhileLimited is identity's own CanDeleteAccount, registered before
// every other (D-106, the conservative reading): while no-show strikes limit
// the account (customer_profiles.limited_until is in the future), deleting it
// would shed the limit, so the answer is 409 conflict naming the date after
// which it can be deleted (extensions.limitedUntil).
func (s *Service) canDeleteWhileLimited(ctx context.Context, userID ids.ID) error {
	cp, err := identitydb.New(s.db.Q(ctx)).GetCustomerProfile(ctx, ids.UUID(userID))
	switch {
	case db.IsNoRows(err):
		return nil
	case err != nil:
		return err
	}
	if cp.LimitedUntil == nil || !cp.LimitedUntil.After(s.clock.Now()) {
		return nil
	}
	until := cp.LimitedUntil.UTC().Format(time.RFC3339)
	return problem.New(genproblems.Conflict, "This account is limited after missed pickups; it can be deleted after "+until+".").
		WithExtension("limitedUntil", until)
}

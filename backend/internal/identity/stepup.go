package identity

import (
	"context"
	"crypto/hmac"
	"time"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/identitydb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// The admin SMS step-up (D-264; 04 §2.1, 02 §3.3).
//
// An admin signs in with the SMS OTP alone and gets an access token with amr
// `otp`, which reaches every /v1/admin route their roles allow. A † action
// (authz.Rule.StepUp, authz.AdminGate.Require with stepUp) also needs a
// step-up younger than authn.StepUpWindow, else 403 step_up_required:
//
//   - POST /v1/auth/step-up/send (SendStepUp) sends a fresh code to the
//     admin's own number, the one in phone_identities, never one the client
//     names. It shares SendOTP's machinery (issueCode) and the same 5-minute
//     life, but has its own bucket (founder, 27 Oct): the resend gap between
//     step-up codes only, and admin_step_up_sends_per_hour per number, apart
//     from the sign-in limits, so a step-up straight after sign-in is never
//     refused and step-ups never use up sign-in sends.
//   - POST /v1/auth/step-up/verify (VerifyStepUp) checks the code (a step_up
//     request of the admin app for this admin's number, unused, unexpired,
//     5 tries) and stamps the sign-in's live session row with step_up_at.
//     Refresh copies it to the next row, and every request reads the family's
//     latest one as the principal's StepUpAt (CheckSession).
//
// The fifth wrong code of a request ends every admin session of the admin
// (as the old authenticator lockout did, D-106) and answers 429 otp_locked:
// they sign in again with a new SMS OTP, whose own limits (and the admin
// sign-in lock after admin_signin_max_failures wrong codes, signin_limits.go)
// then bound the guessing. Nothing here logs a phone or a code.

// adminPrincipal is the caller of a step-up route: an admin-app sign-in.
func adminPrincipal(p authn.Principal) error {
	if p.IsAnonymous() {
		return problem.New(genproblems.Unauthorized, "")
	}
	if p.App != authn.AppAdmin || p.SessionID == ids.Zero {
		return problem.New(genproblems.Forbidden, "")
	}
	return nil
}

// SendStepUp sends the signed-in admin a step-up code (see above).
func (s *Service) SendStepUp(ctx context.Context, p authn.Principal) (SendResult, error) {
	if err := s.ready(); err != nil {
		return SendResult{}, unavailable(ctx, err)
	}
	if err := adminPrincipal(p); err != nil {
		return SendResult{}, err
	}
	q := identitydb.New(s.db.Q(ctx))
	uid := ids.UUID(p.UserID)
	a, err := q.GetAdminUser(ctx, uid)
	switch {
	case db.IsNoRows(err) || (err == nil && a.DisabledAt != nil):
		return SendResult{}, problem.New(genproblems.Forbidden, "")
	case err != nil:
		return SendResult{}, problem.Wrap(genproblems.Internal, err)
	}
	ph, err := q.GetPhoneByUser(ctx, uid)
	switch {
	case db.IsNoRows(err):
		return SendResult{}, problem.New(genproblems.Forbidden, "") // no number to send to
	case err != nil:
		return SendResult{}, problem.Wrap(genproblems.Internal, err)
	}
	plain, err := s.seal.Open("phone", ph.PhoneEnc)
	if err != nil {
		return SendResult{}, problem.Wrap(genproblems.Internal, err)
	}
	return s.issueCode(ctx, authn.AppAdmin, string(plain), PurposeStepUp)
}

// VerifyStepUp checks a step-up code and stamps the caller's sign-in. It
// answers when the step-up stops counting (now + authn.StepUpWindow).
//
// Locks: the users row (LockUser, as every path that revokes sessions takes
// it), then the step_up request row, then the session rows. Only a step_up
// request is locked (GetStepUpOtpRequestForUpdate): a sign-in holds its login
// row and then waits for the users row.
func (s *Service) VerifyStepUp(ctx context.Context, p authn.Principal, requestID, code string) (time.Time, error) {
	if err := s.ready(); err != nil {
		return time.Time{}, unavailable(ctx, err)
	}
	if err := adminPrincipal(p); err != nil {
		return time.Time{}, err
	}
	reqID, err := ids.Parse(requestID)
	if err != nil {
		return time.Time{}, problem.New(genproblems.OtpExpired, "")
	}
	var (
		until   time.Time
		outcome error // a refusal whose attempt count (and lockout) must commit
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
		a, err := q.GetAdminUser(ctx, uid)
		switch {
		case db.IsNoRows(err) || (err == nil && a.DisabledAt != nil):
			return problem.New(genproblems.Forbidden, "")
		case err != nil:
			return err
		}
		phoneHash, err := q.GetPhoneHashByUser(ctx, uid)
		switch {
		case db.IsNoRows(err):
			return problem.New(genproblems.Forbidden, "")
		case err != nil:
			return err
		}

		row, err := q.GetStepUpOtpRequestForUpdate(ctx, ids.UUID(reqID))
		switch {
		case db.IsNoRows(err):
			return problem.New(genproblems.OtpExpired, "")
		case err != nil:
			return err
		}
		// Another admin's code, a customer's step-up code or a used one all
		// read as unknown.
		if row.ConsumedAt != nil || row.Purpose != PurposeStepUp || row.App != string(authn.AppAdmin) ||
			!hmac.Equal(row.PhoneHash, phoneHash) {
			return problem.New(genproblems.OtpExpired, "")
		}
		if row.Attempts >= OTPMaxAttempts {
			return problem.New(genproblems.OtpLocked, "")
		}
		if !now.Before(row.ExpiresAt) {
			return problem.New(genproblems.OtpExpired, "")
		}
		match := hmac.Equal(s.codeHash(reqID, code), row.CodeHash)
		if !match && s.allowTestCode {
			plain, err := s.seal.Open("phone", row.PhoneEnc)
			if err != nil {
				return err
			}
			match = pii.IsReservedTestPhone(string(plain)) && hmac.Equal([]byte(code), []byte(testCode))
		}
		if !match {
			attempts, err := q.RecordOtpAttempt(ctx, row.ID)
			if err != nil {
				return err
			}
			if attempts < OTPMaxAttempts {
				outcome = problem.New(genproblems.CodeInvalid, "").WithExtension("attemptsLeft", OTPMaxAttempts-int(attempts))
				return nil
			}
			revoked, err := q.RevokeUserApp(ctx, identitydb.RevokeUserAppParams{
				Now: now, Reason: reasonAdmin, UserID: uid, App: string(authn.AppAdmin),
			})
			if err != nil {
				return err
			}
			telemetry.Logger(ctx).WarnContext(ctx, "identity: admin step-up tries used up; admin sessions revoked", "revoked", revoked)
			if _, err := s.audit.Write(ctx, auditEntry(authn.AppAdmin, p.UserID, "identity.step_up_locked", map[string]any{
				"sessionId": p.SessionID.String(), "revoked": revoked,
			})); err != nil {
				return err
			}
			outcome = problem.New(genproblems.OtpLocked, "")
			return nil
		}

		if err := q.ConsumeOtpRequest(ctx, identitydb.ConsumeOtpRequestParams{Now: now, ID: row.ID}); err != nil {
			return err
		}
		n, err := q.SetFamilyStepUp(ctx, identitydb.SetFamilyStepUpParams{
			Now: now, FamilyID: ids.UUID(p.SessionID), UserID: uid,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			// The sign-in ended meanwhile (logout, reuse, its expiry).
			return problem.New(genproblems.Unauthorized, "")
		}
		if _, err := s.audit.Write(ctx, auditEntry(authn.AppAdmin, p.UserID, "identity.step_up_verified", map[string]any{
			"sessionId": p.SessionID.String(),
		})); err != nil {
			return err
		}
		until = now.Add(authn.StepUpWindow)
		return nil
	})
	if err != nil {
		return time.Time{}, asProblem(err)
	}
	if outcome != nil {
		return time.Time{}, outcome
	}
	return until, nil
}

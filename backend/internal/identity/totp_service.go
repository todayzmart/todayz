package identity

import (
	"context"
	"fmt"
	"strings"
	"time"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/identitydb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Admin TOTP flows (04 §2.1, 06 A-01, D-14, D-60, 08 AU-11/AU-12).
//
// An admin signs in with SMS OTP and gets an access token with amr `otp`,
// which reaches /v1/auth/* only (slot 11). Then:
//
//   - not enrolled: POST /v1/auth/totp/enrol returns a fresh secret and 10
//     recovery codes (shown once), and POST /v1/auth/totp/verify
//     {purpose: login} with a code from the app confirms the enrolment;
//   - enrolled: POST /v1/auth/totp/verify {purpose: login}, or
//     POST /v1/auth/totp/recover with a recovery code.
//
// Either marks the sign-in (session family) TOTP-verified now and returns an
// access token with amr `otp+totp`; refresh keeps the amr. Every request reads
// the family's last verification as the principal's StepUpAt (CheckSession),
// and a † action needs it younger than authn.StepUpWindow: past it the admin
// calls verify {purpose: step_up} and retries.
//
// Wrong TOTP and recovery codes count in admin_users.totp_failures; at
// TOTPMaxFailures every admin session of that admin ends (they sign in again
// with a new SMS OTP, whose own limits then bound the guessing).

// recoveryCodeCount is how many recovery codes an enrolment gets (01 §2, D-60).
const recoveryCodeCount = 10

// recoveryCodeLen is the length of a recovery code without its dash, from
// referralAlphabet (31 symbols, so ~49 bits each); shown as XXXXX-XXXXX.
const recoveryCodeLen = 10

// TOTP verify purposes (TotpVerifyRequest.purpose).
const (
	TOTPPurposeLogin  = "login"
	TOTPPurposeStepUp = "step_up"
)

// Enrolment is what POST /v1/auth/totp/enrol answers, once.
type Enrolment struct {
	Secret        string // base32, no padding
	URI           string // otpauth://totp/...
	RecoveryCodes []string
}

// TOTPResult is a successful TOTP verify or recovery.
type TOTPResult struct {
	AccessToken string
	ExpiresAt   time.Time
	// RecoveryCodesLeft: recovery only.
	RecoveryCodesLeft int
}

// adminPrincipal is the admin-app caller of a TOTP route.
func adminPrincipal(p authn.Principal) error {
	if p.IsAnonymous() {
		return problem.New(genproblems.Unauthorized, "")
	}
	if p.App != authn.AppAdmin || p.SessionID == ids.Zero {
		return problem.New(genproblems.Forbidden, "")
	}
	return nil
}

// lockAdmin reads the caller's admin row under a row lock: every TOTP flow of
// one admin runs one at a time, so the failure count and the last step are
// exact. A missing or disabled admin is 403.
func lockAdmin(ctx context.Context, q *identitydb.Queries, userID ids.ID) (identitydb.GetAdminForUpdateRow, error) {
	a, err := q.GetAdminForUpdate(ctx, ids.UUID(userID))
	switch {
	case db.IsNoRows(err):
		return a, problem.New(genproblems.Forbidden, "")
	case err != nil:
		return a, err
	case a.DisabledAt != nil:
		return a, problem.New(genproblems.Forbidden, "")
	}
	return a, nil
}

// EnrolTOTP starts (or restarts) an enrolment: a new secret, pending until a
// code verifies it, and 10 new recovery codes. An enrolled admin gets 409
// conflict: replacing a working authenticator is a super_admin reset
// (POST /v1/admin/users/{adminId}/reset-totp, W3-05), never self-service from
// an OTP-only session.
func (s *Service) EnrolTOTP(ctx context.Context, p authn.Principal) (Enrolment, error) {
	if err := s.readyTOTP(); err != nil {
		return Enrolment{}, unavailable(ctx, err)
	}
	if err := adminPrincipal(p); err != nil {
		return Enrolment{}, err
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return Enrolment{}, problem.Wrap(genproblems.Internal, err)
	}
	sealed, err := s.totpSeal.Seal(totpSealField, secret)
	if err != nil {
		return Enrolment{}, problem.Wrap(genproblems.Internal, err)
	}
	codes := make([]string, recoveryCodeCount)
	for i := range codes {
		c, err := randomString(referralAlphabet, recoveryCodeLen)
		if err != nil {
			return Enrolment{}, problem.Wrap(genproblems.Internal, err)
		}
		codes[i] = c[:recoveryCodeLen/2] + "-" + c[recoveryCodeLen/2:]
	}
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		now := s.clock.Now().UTC()
		a, err := lockAdmin(ctx, q, p.UserID)
		if err != nil {
			return err
		}
		if a.TotpEnabledAt != nil {
			return problem.New(genproblems.Conflict, "")
		}
		if err := q.SetPendingTotp(ctx, identitydb.SetPendingTotpParams{SecretEnc: sealed, UserID: a.UserID}); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, a.UserID); err != nil {
			return err
		}
		for _, c := range codes {
			if err := q.InsertRecoveryCode(ctx, identitydb.InsertRecoveryCodeParams{
				ID: ids.UUID(ids.New(now)), AdminID: a.UserID, CodeHash: s.recoveryHash(p.UserID, c), Now: now,
			}); err != nil {
				return err
			}
		}
		_, err = s.audit.Write(ctx, auditEntry(authn.AppAdmin, p.UserID, "identity.totp_enrol_started", map[string]any{
			"sessionId": p.SessionID.String(), "recoveryCodes": recoveryCodeCount,
		}))
		return err
	})
	if err != nil {
		return Enrolment{}, asProblem(err)
	}
	return Enrolment{
		Secret: EncodeTOTPSecret(secret), URI: totpURI(secret, p.UserID.String()), RecoveryCodes: codes,
	}, nil
}

// VerifyTOTP checks a code from the authenticator app. purpose login is the
// second factor after OTP (it also confirms a pending enrolment); step_up
// re-confirms an otp+totp session for a † action. Both mark the sign-in
// verified now and return a fresh otp+totp access token.
func (s *Service) VerifyTOTP(ctx context.Context, p authn.Principal, code, purpose string) (TOTPResult, error) {
	if err := s.readyTOTP(); err != nil {
		return TOTPResult{}, unavailable(ctx, err)
	}
	if err := adminPrincipal(p); err != nil {
		return TOTPResult{}, err
	}
	if purpose != TOTPPurposeLogin && purpose != TOTPPurposeStepUp {
		return TOTPResult{}, problem.New(genproblems.ValidationFailed, "").WithField("body.purpose", "must be login or step_up")
	}
	var (
		roles   []string
		outcome error // a refusal whose failure count must commit
	)
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		now := s.clock.Now().UTC()
		a, err := lockAdmin(ctx, q, p.UserID)
		if err != nil {
			return err
		}
		if purpose == TOTPPurposeStepUp && (a.TotpEnabledAt == nil || p.AMR != authn.AMROTPTOTP) {
			// Step-up re-confirms a finished sign-in; it never finishes one.
			return problem.New(genproblems.Forbidden, "")
		}
		if a.TotpSecretEnc == nil {
			return problem.New(genproblems.Conflict, "") // enrol first
		}
		secret, err := s.totpSeal.Open(totpSealField, a.TotpSecretEnc)
		if err != nil {
			return fmt.Errorf("identity: open totp secret: %w", err)
		}
		step, ok := verifyTOTP(secret, code, now, a.TotpLastStep)
		if !ok {
			refusal, err := s.totpFailure(ctx, q, now, p.UserID)
			if err != nil {
				return err
			}
			outcome = refusal
			return nil
		}
		enrolledNow := a.TotpEnabledAt == nil
		if err := q.RecordTotpSuccess(ctx, identitydb.RecordTotpSuccessParams{Now: now, Step: &step, UserID: a.UserID}); err != nil {
			return err
		}
		if err := s.markVerified(ctx, q, now, p); err != nil {
			return err
		}
		action := "identity.totp_verified"
		if enrolledNow {
			action = "identity.totp_enrolled"
		}
		if _, err := s.audit.Write(ctx, auditEntry(authn.AppAdmin, p.UserID, action, map[string]any{
			"purpose": purpose, "sessionId": p.SessionID.String(),
		})); err != nil {
			return err
		}
		roles = a.Roles
		return nil
	})
	if err != nil {
		return TOTPResult{}, asProblem(err)
	}
	if outcome != nil {
		return TOTPResult{}, outcome
	}
	return s.issueTOTP(p, roles)
}

// RecoverTOTP signs in with a recovery code instead of a TOTP code. Each code
// works once (admin_recovery_codes.used_at).
func (s *Service) RecoverTOTP(ctx context.Context, p authn.Principal, recoveryCode string) (TOTPResult, error) {
	if err := s.readyTOTP(); err != nil {
		return TOTPResult{}, unavailable(ctx, err)
	}
	if err := adminPrincipal(p); err != nil {
		return TOTPResult{}, err
	}
	norm, wellFormed := normalizeRecoveryCode(recoveryCode)
	var (
		roles   []string
		left    int
		outcome error
	)
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		now := s.clock.Now().UTC()
		a, err := lockAdmin(ctx, q, p.UserID)
		if err != nil {
			return err
		}
		if a.TotpEnabledAt == nil {
			// Recovery codes stand in for a working authenticator; an
			// unconfirmed enrolment has none to recover.
			return problem.New(genproblems.Conflict, "")
		}
		used := false
		if wellFormed {
			_, err := q.UseRecoveryCode(ctx, identitydb.UseRecoveryCodeParams{
				Now: now, AdminID: a.UserID, CodeHash: s.recoveryHash(p.UserID, norm),
			})
			switch {
			case db.IsNoRows(err):
			case err != nil:
				return err
			default:
				used = true
			}
		}
		if !used {
			refusal, err := s.totpFailure(ctx, q, now, p.UserID)
			if err != nil {
				return err
			}
			outcome = refusal
			return nil
		}
		if err := q.RecordTotpSuccess(ctx, identitydb.RecordTotpSuccessParams{Now: now, UserID: a.UserID}); err != nil {
			return err
		}
		if err := s.markVerified(ctx, q, now, p); err != nil {
			return err
		}
		n, err := q.CountUnusedRecoveryCodes(ctx, a.UserID)
		if err != nil {
			return err
		}
		left = int(n)
		if _, err := s.audit.Write(ctx, auditEntry(authn.AppAdmin, p.UserID, "identity.totp_recovered", map[string]any{
			"sessionId": p.SessionID.String(), "recoveryCodesLeft": left,
		})); err != nil {
			return err
		}
		roles = a.Roles
		return nil
	})
	if err != nil {
		return TOTPResult{}, asProblem(err)
	}
	if outcome != nil {
		return TOTPResult{}, outcome
	}
	out, err := s.issueTOTP(p, roles)
	out.RecoveryCodesLeft = left
	return out, err
}

// markVerified stamps the caller's live sign-in as TOTP-verified now. No live
// row means the sign-in ended meanwhile (logout, reuse, an expiry): 401.
func (s *Service) markVerified(ctx context.Context, q *identitydb.Queries, now time.Time, p authn.Principal) error {
	n, err := q.SetFamilyTotpVerified(ctx, identitydb.SetFamilyTotpVerifiedParams{
		Now: now, FamilyID: ids.UUID(p.SessionID), UserID: ids.UUID(p.UserID),
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return problem.New(genproblems.Unauthorized, "")
	}
	return nil
}

// totpFailure counts a wrong TOTP or recovery code. Below the limit it answers
// 422 code_invalid with attemptsLeft; at the limit it ends every admin
// session of the admin, resets the count and answers 429 otp_locked. The
// caller commits either way (the answer is returned as the outcome).
func (s *Service) totpFailure(ctx context.Context, q *identitydb.Queries, now time.Time, userID ids.ID) (*problem.Error, error) {
	n, err := q.RecordTotpFailure(ctx, ids.UUID(userID))
	if err != nil {
		return nil, err
	}
	if int(n) < TOTPMaxFailures {
		return problem.New(genproblems.CodeInvalid, "").WithExtension("attemptsLeft", TOTPMaxFailures-int(n)), nil
	}
	revoked, err := q.RevokeUserApp(ctx, identitydb.RevokeUserAppParams{
		Now: now, Reason: reasonAdmin, UserID: ids.UUID(userID), App: string(authn.AppAdmin),
	})
	if err != nil {
		return nil, err
	}
	if err := q.ResetTotpFailures(ctx, ids.UUID(userID)); err != nil {
		return nil, err
	}
	telemetry.Logger(ctx).WarnContext(ctx, "identity: admin TOTP failure limit reached; admin sessions revoked", "revoked", revoked)
	if _, err := s.audit.Write(ctx, auditEntry(authn.AppAdmin, userID, "identity.totp_locked", map[string]any{
		"failures": int(n), "revoked": revoked,
	})); err != nil {
		return nil, err
	}
	return problem.New(genproblems.OtpLocked, ""), nil
}

// issueTOTP signs the otp+totp access token of a verified admin sign-in.
func (s *Service) issueTOTP(p authn.Principal, roles []string) (TOTPResult, error) {
	tok, exp, err := s.tokens.Issue(authn.Principal{
		UserID: p.UserID, App: authn.AppAdmin, SessionID: p.SessionID, Roles: roles, AMR: authn.AMROTPTOTP,
	})
	if err != nil {
		return TOTPResult{}, problem.Wrap(genproblems.Internal, err)
	}
	return TOTPResult{AccessToken: tok, ExpiresAt: exp}, nil
}

// recoveryHash binds a recovery code to its admin:
// HMAC(PII_HASH_KEY, "recovery:<admin id>:<code without dash>").
func (s *Service) recoveryHash(adminID ids.ID, code string) []byte {
	norm, _ := normalizeRecoveryCode(code)
	return s.hash.Sum("recovery:" + adminID.String() + ":" + norm)
}

// normalizeRecoveryCode upper-cases a typed code and drops dashes and spaces;
// ok is false unless exactly recoveryCodeLen symbols of the alphabet remain.
func normalizeRecoveryCode(code string) (string, bool) {
	norm := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
	if len(norm) != recoveryCodeLen {
		return norm, false
	}
	for _, r := range norm {
		if !strings.ContainsRune(referralAlphabet, r) {
			return norm, false
		}
	}
	return norm, true
}

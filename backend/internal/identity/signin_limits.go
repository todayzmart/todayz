package identity

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/identitydb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// The admin's SMS code limits (D-264 review, founder 27 Oct; 01 §2).
//
//   - "Lock after 10 wrong": wrong admin sign-in codes are counted per number
//     across OTP requests (admin_signin_failures). The one that brings the
//     count within admin_signin_failure_window_hours to
//     admin_signin_max_failures locks the number's admin sign-in for
//     admin_signin_lock_hours (admin_signin_locks); while it holds, sends and
//     verifies of an admin sign-in code answer 429 signin_locked. A matching
//     code clears the count; another super_admin can lift the lock on A-18
//     (Accounts.UnlockAdminSignIn).
//   - The count is kept for every number an admin-app sign-in is tried with,
//     admin or not, so the lock answers alike for both and tells a caller no
//     more than sign-in already does (AU-06).
//   - "Own quota of 10/hour": admin step-up sends have their own bucket per
//     number (admin_step_up_sends_per_hour, and OTPResendGap between step-up
//     codes only), apart from the sign-in limits of SendOTP.
//
// Lock order: the otp_requests row (VerifyOTP), then adminSigninKey, then
// identity:phone and users (signIn). The unlock takes the A-18 advisory lock,
// then adminSigninKey, and no users row in between.

// adminSigninKey is the advisory lock of one number's failure count and lock.
func adminSigninKey(phoneHash []byte) string {
	return "identity:admin-signin:" + hex.EncodeToString(phoneHash)
}

// signinLocked is 429 signin_locked with Retry-After until the lock ends.
func signinLocked(until, now time.Time) error {
	wait := max(until.Sub(now), time.Second)
	secs := int((wait + time.Second - 1) / time.Second)
	return problem.New(genproblems.SigninLocked, "").WithHeader("Retry-After", strconv.Itoa(secs))
}

// adminSigninLock reports whether the number's admin sign-in is locked at
// now, and until when. A lock that has run out is ignored (the purge deletes
// it). The caller answers signinLocked(until, now).
func adminSigninLock(ctx context.Context, q *identitydb.Queries, phoneHash []byte, now time.Time) (time.Time, bool, error) {
	until, err := q.AdminSigninLockedUntil(ctx, phoneHash)
	switch {
	case db.IsNoRows(err):
		return time.Time{}, false, nil
	case err != nil:
		return time.Time{}, false, err
	}
	return until, now.Before(until), nil
}

// adminSigninFailed records a wrong admin sign-in code for phoneHash under
// adminSigninKey (held by the caller). It returns signin_locked when this
// failure trips the lock: the lock is written and the count starts again
// from zero after it.
//
// The failure is recorded before the limits are read. When they cannot be
// read (no settings cache, or a read that fails), the answer is 503 as a
// refusal, never as an error: VerifyOTP's transaction still commits, so the
// spent try and the counted failure stand, and the next wrong code checks
// the lock again.
func (s *Service) adminSigninFailed(ctx context.Context, q *identitydb.Queries, phoneHash []byte, now time.Time) (error, error) {
	if err := q.InsertAdminSigninFailure(ctx, identitydb.InsertAdminSigninFailureParams{
		ID: ids.UUID(ids.New(now)), PhoneHash: phoneHash, FailedAt: now,
	}); err != nil {
		return nil, err
	}
	maxFailures, window, lockFor, err := s.adminSigninLimits(ctx)
	if err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "identity: the admin sign-in limits cannot be read; the wrong code is counted and answered 503",
			slog.String("error", err.Error()))
		return problem.Wrap(genproblems.ServiceUnavailable, err), nil
	}
	n, err := q.CountAdminSigninFailuresSince(ctx, identitydb.CountAdminSigninFailuresSinceParams{
		PhoneHash: phoneHash, Since: now.Add(-window),
	})
	if err != nil {
		return nil, err
	}
	if int64(n) < maxFailures {
		return nil, nil
	}
	until := now.Add(lockFor)
	if err := q.LockAdminSignin(ctx, identitydb.LockAdminSigninParams{PhoneHash: phoneHash, Now: now, LockedUntil: until}); err != nil {
		return nil, err
	}
	if err := q.ClearAdminSigninFailures(ctx, phoneHash); err != nil {
		return nil, err
	}
	return signinLocked(until, now), nil
}

// adminSigninLimits reads the three lock settings (01 §17).
func (s *Service) adminSigninLimits(ctx context.Context) (maxFailures int64, window, lockFor time.Duration, err error) {
	if s.settings == nil {
		return 0, 0, 0, errors.New("identity: admin sign-in limits need platform_settings")
	}
	if maxFailures, err = s.settings.Int(ctx, settings.AdminSigninMaxFailures); err != nil {
		return 0, 0, 0, fmt.Errorf("identity: %s: %w", settings.AdminSigninMaxFailures, err)
	}
	if window, err = s.settings.Duration(ctx, settings.AdminSigninFailureWindowHours); err != nil {
		return 0, 0, 0, fmt.Errorf("identity: %s: %w", settings.AdminSigninFailureWindowHours, err)
	}
	if lockFor, err = s.settings.Duration(ctx, settings.AdminSigninLockHours); err != nil {
		return 0, 0, 0, fmt.Errorf("identity: %s: %w", settings.AdminSigninLockHours, err)
	}
	return maxFailures, window, lockFor, nil
}

// adminStepUpLimit checks the admin step-up bucket of one number (inside
// issueCode's transaction, under its phone lock): OTPResendGap since the last
// step-up code, and admin_step_up_sends_per_hour within OTPWindow.
func (s *Service) adminStepUpLimit(ctx context.Context, q *identitydb.Queries, phoneHash []byte, now time.Time, perHour int64) error {
	st, err := q.AdminStepUpSendsSince(ctx, identitydb.AdminStepUpSendsSinceParams{Since: now.Add(-OTPWindow), PhoneHash: phoneHash})
	if err != nil {
		return err
	}
	if st.Sends > 0 && now.Sub(st.Latest) < OTPResendGap {
		return limited(st.Latest.Add(OTPResendGap).Sub(now), OTPResendGap)
	}
	if int64(st.Sends) >= perHour {
		return limited(st.Oldest.Add(OTPWindow).Sub(now), OTPWindow)
	}
	return nil
}

// UnlockAdminSignIn lifts the admin sign-in lock of a user's number (A-18,
// D-264) and clears its failure count, in the caller's transaction. It
// returns when the lock would have ended, or nil when the number had no lock
// that still held at now (nothing is changed then).
func (a *Accounts) UnlockAdminSignIn(ctx context.Context, userID ids.ID, now time.Time) (*time.Time, error) {
	q := identitydb.New(a.s.db.Q(ctx))
	hash, err := q.GetPhoneHashByUser(ctx, ids.UUID(userID))
	switch {
	case db.IsNoRows(err):
		return nil, nil
	case err != nil:
		return nil, err
	}
	if err := q.LockKey(ctx, adminSigninKey(hash)); err != nil {
		return nil, err
	}
	until, err := q.AdminSigninLockedUntil(ctx, hash)
	switch {
	case db.IsNoRows(err):
		return nil, nil
	case err != nil:
		return nil, err
	}
	if !now.Before(until) {
		return nil, nil
	}
	if _, err := q.DeleteAdminSigninLock(ctx, hash); err != nil {
		return nil, err
	}
	if err := q.ClearAdminSigninFailures(ctx, hash); err != nil {
		return nil, err
	}
	return &until, nil
}

// Break-glass unlock errors (`todayz admin unlock-sign-in`). None of them
// names the number.
var (
	ErrBreakGlassPhone    = errors.New("the number must be one Indian mobile in E.164 (+91 and 10 digits)")
	ErrBreakGlassReason   = errors.New("--reason must be 1 to 500 characters and hold no phone number")
	ErrBreakGlassNotAdmin = errors.New("that number belongs to no admin account; nothing was changed")
)

// BreakGlassResult is what a break-glass unlock changed. It never carries the
// number.
type BreakGlassResult struct {
	UserID ids.ID
	// LockedUntil is when the lifted lock would have ended; nil when the
	// number had no lock that still held (the failure count is cleared all
	// the same).
	LockedUntil *time.Time
}

// maxBreakGlassReason matches the A-18 reason limit (04 §2.7).
const maxBreakGlassReason = 500 //settings:ok the API's reason length, not a product number

// BreakGlassUnlockAdminSignIn is the operator's way back in when every super
// admin is locked out, so nobody is left to unlock on A-18 (D-264 review). It
// lifts the admin sign-in lock of an admin's number and clears its failure
// count, and writes an audit row with the system actor and the operator's
// reason, the same action as the A-18 unlock. It works on a disabled admin
// too: lifting the lock lets nobody in (sign-in refuses a disabled admin),
// and an admin enabled again later finds no stale lock. A number that
// belongs to no admin is refused, with nothing changed.
func (s *Service) BreakGlassUnlockAdminSignIn(ctx context.Context, phone, reason string) (BreakGlassResult, error) {
	if s.db == nil || s.hash == nil || s.audit == nil {
		return BreakGlassResult{}, errors.New("identity: the unlock needs DATABASE_URL and PII_HASH_KEY")
	}
	e164, ok := pii.NormalizePhone(strings.TrimSpace(phone))
	if !ok {
		return BreakGlassResult{}, ErrBreakGlassPhone
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > maxBreakGlassReason || pii.ContainsPhone(reason) {
		return BreakGlassResult{}, ErrBreakGlassReason
	}
	hash := s.hash.Phone(e164)
	var out BreakGlassResult
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		u, err := q.GetUserByPhoneHash(ctx, hash)
		if db.IsNoRows(err) {
			return ErrBreakGlassNotAdmin
		} else if err != nil {
			return err
		}
		if _, err := q.GetAdminUser(ctx, u.ID); db.IsNoRows(err) {
			return ErrBreakGlassNotAdmin
		} else if err != nil {
			return err
		}
		userID, ok := ids.FromUUID(u.ID)
		if !ok {
			return errors.New("identity: an admin user without an id")
		}
		// The same per-number lock a sign-in and the A-18 unlock take, so the
		// unlock never interleaves with a verify of that number.
		if err := q.LockKey(ctx, adminSigninKey(hash)); err != nil {
			return err
		}
		now := s.clock.Now().UTC()
		var before any
		until, err := q.DeleteAdminSigninLock(ctx, hash)
		switch {
		case db.IsNoRows(err):
		case err != nil:
			return err
		case now.Before(until):
			t := until.UTC()
			out.LockedUntil, before = &t, t
		}
		if err := q.ClearAdminSigninFailures(ctx, hash); err != nil {
			return err
		}
		out.UserID = userID
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorSystem, Action: "admin_user.unlock_sign_in",
			SubjectType: "admin_user", SubjectID: userID.String(),
			Before: map[string]any{"signInLockedUntil": before},
			After:  map[string]any{"signInLockedUntil": nil, "reason": reason, "via": "break_glass"},
		})
		return err
	})
	if err != nil {
		return BreakGlassResult{}, err
	}
	return out, nil
}

package business

// The owner's confirmation (A-04 step 5): the agent sends a code to the
// owner's number; the owner reads it out; the right code makes the number's
// account the business's owner and submits the business. Only a keyed hash
// of the code is stored and it is never logged or returned.

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	adminfieldapi "github.com/todayz/backend/gen/api/admin-field"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// businessSubmitted is a business waiting for verification.
const businessSubmitted = "submitted"

// ownerTestCode is accepted for the reserved test numbers when the sign-in
// code is (local and test only).
const ownerTestCode = "000000"

// userActive is users.status of an account that may sign in.
const userActive = "active"

// ownerCodeAgentWindow is the rolling window of the per-agent send cap
// (field_owner_codes_per_agent_hour).
const ownerCodeAgentWindow = time.Hour

// ownerCodeHash is the keyed hash of a code, bound to its row.
func (s *Service) ownerCodeHash(id ids.ID, code string) []byte {
	return s.hash.Sum("field-owner-code:" + id.String() + ":" + code)
}

// rateLimited is 429 with Retry-After, as sign-in answers.
func rateLimited(wait, ceiling time.Duration) error {
	wait = min(max(wait, time.Second), ceiling)
	secs := int((wait + time.Second - 1) / time.Second)
	return problem.New(genproblems.RateLimited, "").WithHeader("Retry-After", strconv.Itoa(secs))
}

// ownerOf is the owner row of a business with its number, locked.
func (f *Field) ownerOf(ctx context.Context, b businessdb.Business) (businessdb.Staff, string, error) {
	owner, err := f.s.q(ctx).FieldOwnerForUpdate(ctx, b.ID)
	if db.IsNoRows(err) {
		return businessdb.Staff{}, "", problem.New(genproblems.InvalidTransition, "this business has no owner to confirm")
	}
	if err != nil {
		return businessdb.Staff{}, "", fmt.Errorf("business: owner row: %w", err)
	}
	if owner.Status != staffInvited || len(owner.InvitePhoneEnc) == 0 {
		return businessdb.Staff{}, "", problem.New(genproblems.InvalidTransition, "the owner has already confirmed")
	}
	sid, _ := ids.FromUUID(owner.ID)
	plain, err := f.s.seal.Open(SealField(FieldPhone, sid), owner.InvitePhoneEnc)
	if err != nil {
		return businessdb.Staff{}, "", fmt.Errorf("business: open owner phone: %w", err)
	}
	return owner, string(plain), nil
}

// sendCode sends the owner a code. The business must be ready to submit.
func (f *Field) sendCode(ctx context.Context, bidStr string) (adminfieldapi.FieldOwnerCode, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldOwnerCode{}, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return adminfieldapi.FieldOwnerCode{}, err
	}
	rules := f.d.OTP
	var phone, code string
	var expires time.Time
	err = f.s.db.InTx(ctx, func(ctx context.Context) error {
		q := f.s.q(ctx)
		b, err := f.business(ctx, c, bid)
		if err != nil {
			return err
		}
		if b.Status != businessDraft && b.Status != businessNeedsInfo {
			return problem.New(genproblems.InvalidTransition, "")
		}
		if err := submitReady(ctx, q, b); err != nil {
			return err
		}
		_, e164, err := f.ownerOf(ctx, b)
		if err != nil {
			return err
		}
		// The account may have been blocked since the invite.
		if err := f.ownerAccountOpen(ctx, e164); err != nil {
			return err
		}
		// One agent at a time, then one number at a time (always in this
		// order), so two sends cannot both pass the limits.
		if err := q.LockKey(ctx, "field-owner-code-agent:"+c.p.UserID.String()); err != nil {
			return err
		}
		now := f.s.clock.Now().UTC()
		perAgent, err := f.d.Settings.Int(ctx, settings.FieldOwnerCodesPerAgentHour)
		if err != nil {
			return err
		}
		since := now.Add(-ownerCodeAgentWindow)
		mine, err := q.OwnerCodeSendsByAgent(ctx, businessdb.OwnerCodeSendsByAgentParams{SentBy: ids.UUID(c.p.UserID), Since: since})
		if err != nil {
			return err
		}
		if int64(mine) >= perAgent {
			// The next send is allowed once the (sends − cap + 1)-th oldest
			// leaves the window (a lowered cap can leave several over it).
			at, err := q.OwnerCodeAgentSendAt(ctx, businessdb.OwnerCodeAgentSendAtParams{
				SentBy: ids.UUID(c.p.UserID), Since: since, Skip: int32(int64(mine) - perAgent), //nolint:gosec // both are small counts
			})
			if err != nil {
				return err
			}
			return rateLimited(at.Add(ownerCodeAgentWindow).Sub(now), ownerCodeAgentWindow)
		}
		hash := f.s.hash.Phone(e164)
		if err := q.LockKey(ctx, "field-owner-code:"+hex.EncodeToString(hash)); err != nil {
			return err
		}
		// Sign-in codes to the number count too (01 §2 is per number).
		phoneSince := now.Add(-rules.Window)
		sends, err := q.OwnerCodeSendsByPhone(ctx, businessdb.OwnerCodeSendsByPhoneParams{PhoneHash: hash, Since: phoneSince})
		if err != nil {
			return err
		}
		if sends.Sends > 0 && now.Sub(sends.Latest) < rules.ResendGap {
			return rateLimited(sends.Latest.Add(rules.ResendGap).Sub(now), rules.ResendGap)
		}
		if int(sends.Sends) >= rules.PerPhone {
			at, err := q.OwnerCodePhoneSendAt(ctx, businessdb.OwnerCodePhoneSendAtParams{
				PhoneHash: hash, Since: phoneSince, Skip: sends.Sends - int32(rules.PerPhone), //nolint:gosec // a small count
			})
			if err != nil {
				return err
			}
			return rateLimited(at.Add(rules.Window).Sub(now), rules.Window)
		}
		code, err = randomString("0123456789", rules.Digits)
		if err != nil {
			return err
		}
		id := ids.New(now)
		expires = now.Add(rules.TTL)
		if err := q.InsertOwnerCode(ctx, businessdb.InsertOwnerCodeParams{
			ID: ids.UUID(id), BusinessID: b.ID, PhoneHash: hash, CodeHash: f.s.ownerCodeHash(id, code),
			ExpiresAt: expires, SentBy: ids.UUID(c.p.UserID), Now: now,
		}); err != nil {
			return err
		}
		phone = e164
		return f.s.writeAuditAs(ctx, audit.ActorAdmin, c.p.UserID, "business.owner_code_send", "business", bid.String(), nil,
			map[string]any{"expiresAt": expires})
	})
	if err != nil {
		return adminfieldapi.FieldOwnerCode{}, asProblem(err)
	}
	// After the commit, as sign-in does: a code the database does not hold is
	// never sent.
	if err := f.d.SMS.SendOTP(ctx, phone, code); err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "field owner code not sent", "error", err.Error(), "business_id", bid.String())
		return adminfieldapi.FieldOwnerCode{}, problem.Wrap(genproblems.ServiceUnavailable, fmt.Errorf("business: send owner code: %w", err))
	}
	return adminfieldapi.FieldOwnerCode{
		ExpiresAt: expires, PhoneMasked: pii.MaskPhone(phone), ResendAfterSeconds: int(rules.ResendGap / time.Second),
	}, nil
}

// verifyCode checks the owner's code. A wrong code counts an attempt that is
// kept (the transaction commits) and answers code_invalid after it.
func (f *Field) verifyCode(ctx context.Context, bidStr string, body *adminfieldapi.FieldOwnerVerify) (adminfieldapi.FieldBusiness, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	if body == nil {
		return adminfieldapi.FieldBusiness{}, problem.New(genproblems.BadRequest, "")
	}
	rules := f.d.OTP
	var outcome error
	var failure string // the metric outcome, counted once the transaction commits
	var b businessdb.Business
	err = f.s.db.InTx(ctx, func(ctx context.Context) error {
		q := f.s.q(ctx)
		cur, err := f.business(ctx, c, bid)
		if err != nil {
			return err
		}
		if cur.Status != businessDraft && cur.Status != businessNeedsInfo {
			return problem.New(genproblems.InvalidTransition, "")
		}
		owner, e164, err := f.ownerOf(ctx, cur)
		if err != nil {
			return err
		}
		now := f.s.clock.Now().UTC()
		row, err := q.LatestOwnerCodeForUpdate(ctx, cur.ID)
		switch {
		case db.IsNoRows(err):
			outcome = problem.New(genproblems.OtpExpired, "")
			return nil
		case err != nil:
			return err
		}
		// A code sent to an earlier owner number does not confirm this one.
		if !hmac.Equal(row.PhoneHash, f.s.hash.Phone(e164)) {
			outcome = problem.New(genproblems.OtpExpired, "")
			failure = ownerCodeExpired
			return f.ownerCodeExpiredOnce(ctx, c, bid, row.ID, now, 0)
		}
		if int(row.Attempts) >= rules.MaxAttempts {
			outcome = problem.New(genproblems.OtpLocked, "")
			failure = ownerCodeLocked
			// The first try on a locked code is audited and marked (one more
			// attempt); later ones are only counted.
			if int(row.Attempts) > rules.MaxAttempts {
				return nil
			}
			if _, err := q.RecordOwnerCodeAttempt(ctx, row.ID); err != nil {
				return err
			}
			return f.ownerCodeFailed(ctx, c, bid, ownerCodeLocked, 0)
		}
		if !now.Before(row.ExpiresAt) {
			outcome = problem.New(genproblems.OtpExpired, "")
			failure = ownerCodeExpired
			return f.ownerCodeExpiredOnce(ctx, c, bid, row.ID, now, max(rules.MaxAttempts-int(row.Attempts), 0))
		}
		attempts, err := q.RecordOwnerCodeAttempt(ctx, row.ID)
		if err != nil {
			return err
		}
		rid, _ := ids.FromUUID(row.ID)
		match := hmac.Equal(f.s.ownerCodeHash(rid, body.Code), row.CodeHash)
		if !match && f.d.AllowTestCode && pii.IsReservedTestPhone(e164) {
			match = hmac.Equal([]byte(body.Code), []byte(ownerTestCode))
		}
		if !match {
			left := max(rules.MaxAttempts-int(attempts), 0)
			outcome = problem.New(genproblems.CodeInvalid, "").WithExtension("attemptsLeft", left)
			failure = ownerCodeWrong
			if left == 0 {
				failure = ownerCodeLockedOut
			}
			return f.ownerCodeFailed(ctx, c, bid, failure, left)
		}
		// The business may have changed since the code went out.
		if err := submitReady(ctx, q, cur); err != nil {
			return err
		}
		if err := q.ConsumeOwnerCode(ctx, businessdb.ConsumeOwnerCodeParams{Now: now, ID: row.ID}); err != nil {
			return err
		}
		acct, err := f.d.Accounts.FindOrCreateByPhone(ctx, e164, now)
		if err != nil {
			return fmt.Errorf("business: owner account: %w", err)
		}
		if acct.Status != userActive {
			return errOwnerNumber()
		}
		if acct.UserID == c.p.UserID {
			return problem.New(genproblems.Conflict, "the owner's number is yours")
		}
		_, err = q.ActivateOwner(ctx, businessdb.ActivateOwnerParams{UserID: ids.UUID(acct.UserID), ID: owner.ID})
		if db.IsUniqueViolation(err, "ux_staff_business_user") {
			return problem.New(genproblems.Conflict, "the owner's number is already staff of this business")
		}
		if err != nil {
			return fmt.Errorf("business: activate owner: %w", err)
		}
		b, err = q.SubmitBusiness(ctx, businessdb.SubmitBusinessParams{Now: now, ID: cur.ID})
		if db.IsNoRows(err) {
			return problem.New(genproblems.InvalidTransition, "")
		}
		if err != nil {
			return err
		}
		sid, _ := ids.FromUUID(owner.ID)
		if err := f.s.writeAuditAs(ctx, audit.ActorAdmin, c.p.UserID, "staff.owner_confirm", "staff", sid.String(),
			map[string]any{"status": owner.Status}, map[string]any{"status": staffActive, "userId": acct.UserID.String()}); err != nil {
			return err
		}
		return f.s.writeAuditAs(ctx, audit.ActorAdmin, c.p.UserID, "business.submit", "business", bid.String(),
			map[string]any{"status": cur.Status}, map[string]any{"status": b.Status})
	})
	if err != nil {
		return adminfieldapi.FieldBusiness{}, asProblem(err)
	}
	if failure != "" {
		countOwnerCodeFailure(ctx, failure)
	}
	if outcome != nil {
		return adminfieldapi.FieldBusiness{}, outcome
	}
	return f.view(ctx, c, b)
}

// ownerCodeExpiredOnce audits the first expired try on a code (lapsed, or
// sent to an earlier owner number) and marks it; later tries on that code are
// only counted in the metric.
func (f *Field) ownerCodeExpiredOnce(ctx context.Context, c fieldCall, bid ids.ID, code pgtype.UUID, now time.Time, left int) error {
	n, err := f.s.q(ctx).MarkOwnerCodeExpiredAudited(ctx, businessdb.MarkOwnerCodeExpiredAuditedParams{Now: now, ID: code})
	if err != nil || n == 0 {
		return err
	}
	return f.ownerCodeFailed(ctx, c, bid, ownerCodeExpired, left)
}

// ownerCodeFailed audits a failed owner-code try (kept: the transaction
// commits). The row carries what happened and the tries left, never the
// number or a code.
func (f *Field) ownerCodeFailed(ctx context.Context, c fieldCall, bid ids.ID, what string, left int) error {
	return f.s.writeAuditAs(ctx, audit.ActorAdmin, c.p.UserID, "business.owner_code_fail", "business", bid.String(), nil,
		map[string]any{"outcome": what, "attemptsLeft": left})
}

package referrals

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/referralsdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// ClaimInput is a claim: a typed code (As picks which party it is when it
// could be more than one) and the attribution cookie.
type ClaimInput struct {
	Code   string
	As     string // partyCustomer, partyStore, partyBusiness or ""
	Cookie string
}

// ClaimResult is the recorded referral.
type ClaimResult struct {
	ReferralID    ids.ID
	Kind          string
	Status        string // the public status
	RewardCredits int    // what the referee gets on unlock; 0 when rejected
	Created       bool   // false: the same claim again
}

func refusal(code string) *problem.Error { return problem.New(code, "") }

// pick chooses the referrer: a valid click (first touch) beats a typed code.
// TODO(lead): 01 §8 says first touch wins; a typed code that names someone
// else than the click is ignored rather than refused.
func (s *Service) pick(ctx context.Context, in ClaimInput, joined time.Time, win time.Duration, allowed []string, refused string) (referrer, string, error) {
	stale := false
	if in.Cookie != "" {
		r, at, ok, err := s.clickReferrer(ctx, in.Cookie)
		if err != nil {
			return referrer{}, "", err
		}
		if ok {
			if slices.Contains(allowed, r.Type) && !at.After(joined) && joined.Sub(at) <= win {
				live, ok, err := s.referrerLive(ctx, r)
				if err != nil {
					return referrer{}, "", err
				}
				if ok {
					src := sourceLink
					if live.StandeeID != ids.Zero {
						src = sourceStandee
					}
					return live, src, nil
				}
			}
			stale = true
		}
	}
	if in.Code != "" {
		code := normCode(in.Code)
		if code == "" {
			return referrer{}, "", fieldErr("body.code", "pattern")
		}
		order := append(slices.Clone(allowed), refused)
		if in.As != "" {
			if in.As == refused || !slices.Contains(allowed, in.As) {
				return referrer{}, "", refusal(genproblems.ReferralNotEligible)
			}
			order = []string{in.As}
		}
		r, ok, err := s.resolve(ctx, code, order...)
		if err != nil {
			return referrer{}, "", err
		}
		if !ok {
			return referrer{}, "", refusal(genproblems.ReferralCodeUnknown)
		}
		if r.Type == refused {
			return referrer{}, "", refusal(genproblems.ReferralNotEligible)
		}
		return r, sourceCode, nil
	}
	if stale {
		return referrer{}, "", refusal(genproblems.ReferralNotEligible)
	}
	return referrer{}, "", fieldErr("body.code", "required")
}

// referrerLive re-resolves a click's referrer: it must still be a live code
// of the same party (an active customer, a business not rejected or
// suspended, a live or paused store).
func (s *Service) referrerLive(ctx context.Context, r referrer) (referrer, bool, error) {
	now, ok, err := s.resolve(ctx, r.Code, r.Type)
	if err != nil || !ok || now.ID != r.ID {
		return referrer{}, false, err
	}
	now.ClickID, now.StandeeID = r.ClickID, r.StandeeID
	return now, true, nil
}

// referrerUsers are the people behind a referrer, for the same-device rule.
func (s *Service) referrerUsers(ctx context.Context, r referrer) ([]pgtype.UUID, error) {
	switch r.Type {
	case partyCustomer:
		return []pgtype.UUID{ids.UUID(r.ID)}, nil
	case partyStore:
		return s.q(ctx).StaffUserIDs(ctx, ids.UUID(r.BusinessID))
	default:
		return s.q(ctx).StaffUserIDs(ctx, ids.UUID(r.ID))
	}
}

// deviceCheck is the D-13 result for a claim.
type deviceCheck struct {
	Hash     []byte // the device's hash; nil when the session has no device
	Reason   string // "" or why the claim is rejected_same_device
	Accounts int32
}

// checkDevice applies D-13 to the caller's device. TODO(lead): a session
// without a device row (an old session) is not checked.
func (s *Service) checkDevice(ctx context.Context, sessionID, userID ids.ID, refUsers []pgtype.UUID, now time.Time) (deviceCheck, error) {
	q := s.q(ctx)
	key, err := q.SessionDeviceKey(ctx, referralsdb.SessionDeviceKeyParams{FamilyID: ids.UUID(sessionID), UserID: ids.UUID(userID)})
	if err != nil {
		if db.IsNoRows(err) {
			return deviceCheck{}, nil
		}
		return deviceCheck{}, err
	}
	dc := deviceCheck{Hash: s.hasher.Sum("device:" + key)}
	if dc.Accounts, err = q.DeviceAccountCount(ctx, referralsdb.DeviceAccountCountParams{DeviceKey: key, DeviceHash: dc.Hash}); err != nil {
		return deviceCheck{}, err
	}
	tied, err := q.DeviceTiedToUsers(ctx, referralsdb.DeviceTiedToUsersParams{DeviceKey: key, UserIds: refUsers})
	if err != nil {
		return deviceCheck{}, err
	}
	if tied {
		dc.Reason = "same_device_referrer"
		return dc, nil
	}
	seen, err := q.DeviceOtherAccounts(ctx, referralsdb.DeviceOtherAccountsParams{
		DeviceKey: key, UserID: ids.UUID(userID), Since: now.Add(-sameDeviceWindow), DeviceHash: dc.Hash, RefereeID: ids.UUID(userID),
	})
	if err != nil {
		return deviceCheck{}, err
	}
	if seen {
		dc.Reason = "same_device_recent_account"
	}
	return dc, nil
}

// record inserts the referral (rejected_same_device when the device check
// failed) and raises the device-accounts risk flag.
func (s *Service) record(ctx context.Context, p referralsdb.InsertReferralParams, dev deviceCheck) (referralsdb.Referral, error) {
	q := s.q(ctx)
	p.DeviceHash = dev.Hash
	if dev.Reason != "" {
		p.Status = statusRejectedSameDevice
		p.RejectReason = &dev.Reason
	}
	row, err := q.InsertReferral(ctx, p)
	if err != nil {
		if db.IsNoRows(err) {
			return referralsdb.Referral{}, refusal(genproblems.ReferralAlreadyClaimed)
		}
		return referralsdb.Referral{}, err
	}
	if dev.Accounts+1 >= deviceAccountsFlag {
		details, _ := json.Marshal(map[string]any{"accounts": dev.Accounts + 1, "kind": p.Kind})
		if err := q.InsertRiskFlag(ctx, referralsdb.InsertRiskFlagParams{
			ID: ids.UUID(s.ids.New(p.Now)), SubjectType: "referral", SubjectID: row.ID,
			Rule: "referral.device_accounts", Score: 50, Details: details, Now: p.Now,
		}); err != nil {
			return referralsdb.Referral{}, err
		}
	}
	return row, nil
}

func result(row referralsdb.Referral, created bool) ClaimResult {
	out := ClaimResult{ReferralID: idOf(row.ID), Kind: row.Kind, Status: publicStatus(row.Status), Created: created}
	if row.Status != statusRejected && row.Status != statusRejectedSameDevice {
		out.RewardCredits = int(row.RefereeCredits)
	}
	return out
}

// again answers a claim when the account already has a referral: the same
// claim (the same typed code, or the same click) is 200; anything else 409.
func again(row referralsdb.Referral, in ClaimInput) (ClaimResult, error) {
	if c := normCode(in.Code); c != "" {
		if c == row.Code {
			return result(row, false), nil
		}
	} else if in.Cookie != "" {
		if id, err := ids.Parse(in.Cookie); err == nil && row.ClickID.Valid && idOf(row.ClickID) == id {
			return result(row, false), nil
		}
	}
	return ClaimResult{}, refusal(genproblems.ReferralAlreadyClaimed)
}

// phoneHash is the referee's phone hash; 409 when a referral of this referee
// type was already claimed with the same phone (a deleted account signing up
// again, D-13).
func (s *Service) phoneHash(ctx context.Context, userID ids.ID, refereeType string) ([]byte, error) {
	q := s.q(ctx)
	h, err := q.UserPhoneHash(ctx, ids.UUID(userID))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	taken, err := q.PhoneAlreadyReferred(ctx, referralsdb.PhoneAlreadyReferredParams{PhoneHash: h, RefereeType: refereeType})
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, refusal(genproblems.ReferralAlreadyClaimed)
	}
	return h, nil
}

func (s *Service) insertParams(kind string, r referrer, src, refereeType string, refereeID ids.ID, phone []byte, credits [2]int32, now time.Time) referralsdb.InsertReferralParams {
	p := referralsdb.InsertReferralParams{
		ID: ids.UUID(s.ids.New(now)), Kind: kind, ReferrerType: r.Type, ReferrerID: ids.UUID(r.ID),
		RefereeType: refereeType, RefereeID: ids.UUID(refereeID), Code: r.Code, Source: src,
		Status: statusPending, RefereePhoneHash: phone,
		ReferrerCredits: credits[0], RefereeCredits: credits[1], Now: now,
	}
	if src != sourceCode {
		p.ClickID = optUUID(r.ClickID)
	}
	if src == sourceStandee {
		p.StandeeID = optUUID(r.StandeeID)
	}
	return p
}

// ClaimCustomer records the referral a new customer joined through (C-16):
// once, within the window of sign-up, before the first completed order.
func (s *Service) ClaimCustomer(ctx context.Context, userID, sessionID ids.ID, in ClaimInput) (ClaimResult, error) {
	if err := s.ready(ctx); err != nil {
		return ClaimResult{}, err
	}
	var out ClaimResult
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		now := s.now()
		win, err := s.window(ctx)
		if err != nil {
			return err
		}
		me, err := q.CustomerForClaim(ctx, ids.UUID(userID))
		if err != nil {
			if db.IsNoRows(err) {
				return refusal(genproblems.ReferralNotEligible)
			}
			return err
		}
		existing, err := q.RefereeReferral(ctx, referralsdb.RefereeReferralParams{RefereeType: partyCustomer, RefereeID: ids.UUID(userID)})
		switch {
		case err == nil:
			out, err = again(existing, in)
			return err
		case !db.IsNoRows(err):
			return err
		}
		if me.ReferredByReferralID.Valid {
			return refusal(genproblems.ReferralAlreadyClaimed)
		}
		if me.Status != "active" || now.Sub(me.CreatedAt) > win {
			return refusal(genproblems.ReferralNotEligible)
		}
		// TODO(lead): a verified POS sale before the claim is not checked
		// here (POS is W2-04); it cannot qualify a claim made after it.
		done, err := q.CustomerHasCompletedOrder(ctx, ids.UUID(userID))
		if err != nil {
			return err
		}
		if done {
			return refusal(genproblems.ReferralNotEligible)
		}
		r, src, err := s.pick(ctx, in, me.CreatedAt, win, []string{partyCustomer, partyStore}, partyBusiness)
		if err != nil {
			return err
		}
		kind := KindCustomerCustomer
		switch r.Type {
		case partyCustomer:
			if r.ID == userID {
				return refusal(genproblems.ReferralSelf)
			}
		case partyStore:
			kind = KindRetailerCustomer
			staff, err := q.IsStaffOf(ctx, referralsdb.IsStaffOfParams{BusinessID: ids.UUID(r.BusinessID), UserID: ids.UUID(userID)})
			if err != nil {
				return err
			}
			if staff {
				return refusal(genproblems.ReferralSelf)
			}
		}
		phone, err := s.phoneHash(ctx, userID, partyCustomer)
		if err != nil {
			return err
		}
		refUsers, err := s.referrerUsers(ctx, r)
		if err != nil {
			return err
		}
		dev, err := s.checkDevice(ctx, sessionID, userID, refUsers, now)
		if err != nil {
			return err
		}
		a, b, err := s.rewards(ctx, kind)
		if err != nil {
			return err
		}
		row, err := s.record(ctx, s.insertParams(kind, r, src, partyCustomer, userID, phone, [2]int32{a, b}, now), dev)
		if err != nil {
			return err
		}
		if err := q.SetCustomerReferredBy(ctx, referralsdb.SetCustomerReferredByParams{ReferralID: row.ID, Now: now, UserID: ids.UUID(userID)}); err != nil {
			return err
		}
		out = result(row, true)
		return nil
	})
	if err != nil {
		return ClaimResult{}, asProblem(err)
	}
	return out, nil
}

// claimableBusiness are the statuses a new business can still claim in
// (before verification).
var claimableBusiness = []string{"draft", "submitted", "needs_info"}

// ClaimBusiness records the referral a new business joined through (B-10):
// once, by its owner, before verification and within the window of the
// business's creation (D-160).
func (s *Service) ClaimBusiness(ctx context.Context, businessID, ownerID, sessionID ids.ID, in ClaimInput) (ClaimResult, error) {
	if err := s.ready(ctx); err != nil {
		return ClaimResult{}, err
	}
	var out ClaimResult
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		now := s.now()
		win, err := s.window(ctx)
		if err != nil {
			return err
		}
		biz, err := q.BusinessForClaim(ctx, ids.UUID(businessID))
		if err != nil {
			if db.IsNoRows(err) {
				return notFound()
			}
			return err
		}
		existing, err := q.RefereeReferral(ctx, referralsdb.RefereeReferralParams{RefereeType: partyBusiness, RefereeID: ids.UUID(businessID)})
		switch {
		case err == nil:
			out, err = again(existing, in)
			return err
		case !db.IsNoRows(err):
			return err
		}
		if biz.ReferredByReferralID.Valid {
			return refusal(genproblems.ReferralAlreadyClaimed)
		}
		if !slices.Contains(claimableBusiness, biz.Status) || now.Sub(biz.CreatedAt) > win {
			return refusal(genproblems.ReferralNotEligible)
		}
		r, src, err := s.pick(ctx, in, biz.CreatedAt, win, []string{partyCustomer, partyBusiness}, partyStore)
		if err != nil {
			return err
		}
		switch r.Type {
		case partyBusiness:
			if r.ID == businessID {
				return refusal(genproblems.ReferralSelf)
			}
			shared, err := q.SharesBusinessStaff(ctx, referralsdb.SharesBusinessStaffParams{A: ids.UUID(r.ID), B: ids.UUID(businessID)})
			if err != nil {
				return err
			}
			if shared {
				return refusal(genproblems.ReferralSelf)
			}
		case partyCustomer:
			staff, err := q.IsStaffOf(ctx, referralsdb.IsStaffOfParams{BusinessID: ids.UUID(businessID), UserID: ids.UUID(r.ID)})
			if err != nil {
				return err
			}
			if staff || r.ID == ownerID {
				return refusal(genproblems.ReferralSelf)
			}
		}
		phone, err := s.phoneHash(ctx, ownerID, partyBusiness)
		if err != nil {
			return err
		}
		refUsers, err := s.referrerUsers(ctx, r)
		if err != nil {
			return err
		}
		dev, err := s.checkDevice(ctx, sessionID, ownerID, refUsers, now)
		if err != nil {
			return err
		}
		a, b, err := s.rewards(ctx, KindAnyShop)
		if err != nil {
			return err
		}
		row, err := s.record(ctx, s.insertParams(KindAnyShop, r, src, partyBusiness, businessID, phone, [2]int32{a, b}, now), dev)
		if err != nil {
			return err
		}
		if err := q.SetBusinessReferredBy(ctx, referralsdb.SetBusinessReferredByParams{ReferralID: row.ID, Now: now, ID: ids.UUID(businessID)}); err != nil {
			return err
		}
		out = result(row, true)
		return nil
	})
	if err != nil {
		return ClaimResult{}, asProblem(err)
	}
	return out, nil
}

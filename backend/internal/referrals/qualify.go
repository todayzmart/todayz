package referrals

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/referralsdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// OnPurchaseCompleted is the hook orders and POS call inside the transaction
// that records a completed purchase (01 §8). It qualifies the buyer's pending
// customer referral and counts the sale toward the selling business's shop
// referral, posting rewards that are due. A database error is returned (the
// caller rolls back); a ledger or notification failure is logged and left to
// the sweep.
func (s *Service) OnPurchaseCompleted(ctx context.Context, o Outcome) error {
	if s.db == nil || s.settings == nil {
		return nil
	}
	if o.RefType == RefPOSSale && o.LinkMethod != LinkBillQR && o.LinkMethod != LinkCounterCode {
		return nil // an unlinked or phone_unverified sale never qualifies (D-07)
	}
	if o.CustomerID == ids.Zero {
		return nil
	}
	return s.db.InTx(ctx, func(ctx context.Context) error {
		if err := s.qualifyCustomer(ctx, o); err != nil {
			return err
		}
		return s.countShop(ctx, o)
	})
}

// qualifyCustomer: the buyer's first purchase with qualifying amount ≥
// ref_qualifying_min_order_paise unlocks their pending referral. A smaller
// purchase leaves it pending (D-166).
func (s *Service) qualifyCustomer(ctx context.Context, o Outcome) error {
	q := s.q(ctx)
	row, err := q.LockRefereeReferral(ctx, referralsdb.LockRefereeReferralParams{RefereeType: partyCustomer, RefereeID: ids.UUID(o.CustomerID)})
	if err != nil {
		if db.IsNoRows(err) {
			return nil
		}
		return err
	}
	if row.Status != statusPending {
		return nil
	}
	minPaise, err := s.settings.Paise(ctx, settings.RefQualifyingMinOrderPaise)
	if err != nil {
		return fmt.Errorf("referrals: %s: %w", settings.RefQualifyingMinOrderPaise, err)
	}
	if o.EligiblePaise-int64(o.CreditsUsed)*paisePerCredit < int64(minPaise) {
		return nil
	}
	row, err = q.SetQualified(ctx, referralsdb.SetQualifiedParams{RefType: o.RefType, RefID: ids.UUID(o.RefID), Now: s.now(), ID: row.ID})
	if err != nil {
		return err
	}
	return s.reward(ctx, row)
}

// countShop counts the sale toward the selling business's pending shop
// referral, then qualifies it when due. A purchase by the business's own
// staff, by the referrer, or by the referrer business's staff is not counted
// (D-167).
func (s *Service) countShop(ctx context.Context, o Outcome) error {
	q := s.q(ctx)
	biz, err := q.StoreBusiness(ctx, ids.UUID(o.StoreID))
	if err != nil {
		if db.IsNoRows(err) {
			return nil
		}
		return err
	}
	row, err := q.LockRefereeReferral(ctx, referralsdb.LockRefereeReferralParams{RefereeType: partyBusiness, RefereeID: biz})
	if err != nil {
		if db.IsNoRows(err) {
			return nil
		}
		return err
	}
	if row.Status != statusPending {
		return nil
	}
	buyer := ids.UUID(o.CustomerID)
	staff, err := q.IsStaffOf(ctx, referralsdb.IsStaffOfParams{BusinessID: biz, UserID: buyer})
	if err != nil || staff {
		return err
	}
	switch row.ReferrerType {
	case partyCustomer:
		if idOf(row.ReferrerID) == o.CustomerID {
			return nil
		}
	case partyBusiness:
		staff, err := q.IsStaffOf(ctx, referralsdb.IsStaffOfParams{BusinessID: row.ReferrerID, UserID: buyer})
		if err != nil || staff {
			return err
		}
	}
	if err := q.CountShopSale(ctx, referralsdb.CountShopSaleParams{
		ReferralID: row.ID, RefType: o.RefType, RefID: ids.UUID(o.RefID), StoreID: ids.UUID(o.StoreID),
		CustomerID: ids.UUID(o.CustomerID), Now: s.now(),
	}); err != nil {
		return err
	}
	return s.tryQualifyShop(ctx, row)
}

// tryQualifyShop qualifies a locked pending shop referral once the business
// is verified and ref_shop_qualifying_orders different customers bought
// (D-168); the last counted sale is the qualifying one.
func (s *Service) tryQualifyShop(ctx context.Context, row referralsdb.Referral) error {
	if row.Status != statusPending || row.Kind != KindAnyShop {
		return nil
	}
	q := s.q(ctx)
	verified, err := q.BusinessVerified(ctx, row.RefereeID)
	if err != nil || !verified {
		if db.IsNoRows(err) {
			return nil
		}
		return err
	}
	need, err := s.settings.Int(ctx, settings.RefShopQualifyingOrders)
	if err != nil {
		return fmt.Errorf("referrals: %s: %w", settings.RefShopQualifyingOrders, err)
	}
	n, err := q.ShopSaleCount(ctx, row.ID)
	if err != nil {
		return err
	}
	if int64(n) < max(need, 1) {
		return nil
	}
	last, err := q.LastShopSale(ctx, row.ID)
	if err != nil {
		return err
	}
	row, err = q.SetQualified(ctx, referralsdb.SetQualifiedParams{RefType: last.RefType, RefID: last.RefID, Now: s.now(), ID: row.ID})
	if err != nil {
		return err
	}
	return s.reward(ctx, row)
}

// beneficiary states.
const (
	payPost = iota // post to the account
	payWait        // not payable yet (no live store, a blocked referrer): try again later
	payVoid        // nobody to pay (a deleted customer): mark done unposted
)

// beneficiary is the account one side's reward goes to: a customer's wallet,
// or the primary store of a business (a store referrer's business too,
// 01 §8, D-72). A deleted customer is void; a blocked customer referrer
// waits until unblocked (D-168).
func (s *Service) beneficiary(ctx context.Context, side, partyType string, partyID pgtype.UUID) (Beneficiary, int, error) {
	q := s.q(ctx)
	switch partyType {
	case partyCustomer:
		status, err := q.UserStatus(ctx, partyID)
		switch {
		case db.IsNoRows(err):
			return Beneficiary{}, payVoid, nil
		case err != nil:
			return Beneficiary{}, 0, err
		}
		switch {
		case status == "deleted":
			return Beneficiary{}, payVoid, nil
		case status == "blocked" && side == SideReferrer:
			return Beneficiary{}, payWait, nil
		}
		return Beneficiary{Kind: BeneficiaryCustomer, ID: idOf(partyID)}, payPost, nil
	case partyStore, partyBusiness:
		biz := partyID
		if partyType == partyStore {
			b, err := q.StoreBusiness(ctx, partyID)
			if err != nil {
				return Beneficiary{}, 0, err
			}
			biz = b
		}
		st, err := q.PrimaryStore(ctx, biz)
		if err != nil {
			if db.IsNoRows(err) {
				return Beneficiary{}, payWait, nil
			}
			return Beneficiary{}, 0, err
		}
		return Beneficiary{Kind: BeneficiaryStore, ID: idOf(st)}, payPost, nil
	}
	return Beneficiary{}, 0, fmt.Errorf("referrals: unknown party %q", partyType)
}

// reward posts each side of a locked qualified referral not yet posted. The
// posting and the notification each run in a savepoint: a failure leaves the
// side for the sweep and never aborts the caller.
func (s *Service) reward(ctx context.Context, row referralsdb.Referral) error {
	sides := []struct {
		name    string
		done    bool
		typ     string
		id      pgtype.UUID
		credits int32
	}{
		{SideReferrer, row.ReferrerRewardedAt != nil, row.ReferrerType, row.ReferrerID, row.ReferrerCredits},
		{SideReferee, row.RefereeRewardedAt != nil, row.RefereeType, row.RefereeID, row.RefereeCredits},
	}
	q := s.q(ctx)
	log := telemetry.Logger(ctx)
	for _, side := range sides {
		if side.done || row.Status != statusQualified {
			continue
		}
		to, state, err := s.beneficiary(ctx, side.name, side.typ, side.id)
		if err != nil {
			return err
		}
		if state == payWait {
			continue
		}
		var txID ids.ID
		if state == payPost && side.credits > 0 {
			posted := false
			err := s.db.InTx(ctx, func(ctx context.Context) error {
				var err error
				txID, posted, err = s.ledger.PostReward(ctx, Reward{
					ReferralID: idOf(row.ID), Side: side.name, To: to, Credits: int(side.credits),
					IdempotencyKey: "referral:" + idOf(row.ID).String() + ":" + side.name,
				})
				return err
			})
			if err != nil {
				log.WarnContext(ctx, "referrals: reward not posted", "referral", idOf(row.ID).String(), "side", side.name, "err", err)
				continue
			}
			if !posted {
				continue
			}
		}
		row, err = q.SetSideRewarded(ctx, referralsdb.SetSideRewardedParams{Side: side.name, TransactionID: optUUID(txID), Now: s.now(), ID: row.ID})
		if err != nil {
			return err
		}
		if txID != ids.Zero {
			s.notify(ctx, row, to, int(side.credits))
		}
	}
	return nil
}

// notify sends "referral unlocked" to whoever was paid (01 §15: in-app +
// push). It runs in a savepoint and only logs a failure.
func (s *Service) notify(ctx context.Context, row referralsdb.Referral, to Beneficiary, credits int) {
	if s.notices == nil {
		return
	}
	n := notice.Notification{
		Params: map[string]string{"credits": strconv.Itoa(credits)},
		Ref:    &notice.Ref{Type: "referral", ID: idOf(row.ID)},
	}
	switch to.Kind {
	case BeneficiaryCustomer:
		n.Kind, n.UserID = notice.KindReferralUnlocked, to.ID
	case BeneficiaryStore:
		biz, err := s.q(ctx).StoreBusiness(ctx, ids.UUID(to.ID))
		if err != nil {
			telemetry.Logger(ctx).WarnContext(ctx, "referrals: notify: store business", "err", err)
			return
		}
		n.Kind, n.BusinessID, n.StoreID = notice.KindShopReferralUnlocked, idOf(biz), to.ID
	}
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		tx, _ := db.TxFrom(ctx)
		_, err := s.notices.Send(ctx, tx, n)
		return err
	})
	if err != nil {
		telemetry.Logger(ctx).WarnContext(ctx, "referrals: notify", "referral", idOf(row.ID).String(), "err", err)
	}
}

// OnPurchaseReversed is the hook for a reversed purchase (a POS refund or
// void, a reversed order; 01 §7, D-08), inside the reversal's transaction. A
// qualification whose rewards have not posted goes back to pending; one with
// a posted side keeps its rewards and raises referral.reversed_after_reward.
// A counted shop sale of a still-pending referral is uncounted.
func (s *Service) OnPurchaseReversed(ctx context.Context, refType string, refID ids.ID) error {
	if s.db == nil {
		return nil
	}
	return s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		now := s.now()
		rows, err := q.LockQualifyingReferrals(ctx, referralsdb.LockQualifyingReferralsParams{RefType: refType, RefID: ids.UUID(refID)})
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.ReferrerRewardedAt == nil && row.RefereeRewardedAt == nil {
				if err := q.RevertQualified(ctx, referralsdb.RevertQualifiedParams{Now: now, ID: row.ID}); err != nil {
					return err
				}
				continue
			}
			details, _ := json.Marshal(map[string]any{"ref_type": refType, "kind": row.Kind})
			if err := q.InsertRiskFlag(ctx, referralsdb.InsertRiskFlagParams{
				ID: ids.UUID(s.ids.New(now)), SubjectType: "referral", SubjectID: row.ID,
				Rule: "referral.reversed_after_reward", Score: 40, Details: details, Now: now, //settings:ok a risk score, not ask_daily_limit_store
			}); err != nil {
				return err
			}
		}
		// A sale counted toward a shop that is (again) pending no longer counts.
		_, err = q.UncountShopSale(ctx, referralsdb.UncountShopSaleParams{RefType: refType, RefID: ids.UUID(refID)})
		return err
	})
}

// Sweep posts rewards still due (a ledger that was down, a shop that had no
// live store) and qualifies shops whose verification came after their last
// counted sale. Each referral runs in its own transaction.
func (s *Service) Sweep(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{"rewarded": 0, "shops_qualified": 0}
	if s.db == nil || s.settings == nil {
		return out, errUnavailable
	}
	qualified, err := s.q(ctx).QualifiedReferrals(ctx, sweepBatch)
	if err != nil {
		return out, err
	}
	for _, id := range qualified {
		var done bool
		err := s.db.InTx(ctx, func(ctx context.Context) error {
			row, err := s.q(ctx).LockReferral(ctx, id)
			if err != nil {
				return err
			}
			if err := s.reward(ctx, row); err != nil {
				return err
			}
			after, err := s.q(ctx).LockReferral(ctx, id)
			done = err == nil && after.Status == statusRewarded
			return err
		})
		if err != nil {
			return out, err
		}
		if done {
			out["rewarded"]++
		}
	}
	pending, err := s.q(ctx).PendingShopReferrals(ctx, sweepBatch)
	if err != nil {
		return out, err
	}
	for _, id := range pending {
		var qualifiedNow bool
		err := s.db.InTx(ctx, func(ctx context.Context) error {
			row, err := s.q(ctx).LockReferral(ctx, id)
			if err != nil {
				return err
			}
			if err := s.tryQualifyShop(ctx, row); err != nil {
				return err
			}
			after, err := s.q(ctx).LockReferral(ctx, id)
			qualifiedNow = err == nil && after.Status != statusPending
			return err
		})
		if err != nil {
			return out, err
		}
		if qualifiedNow {
			out["shops_qualified"]++
		}
	}
	return out, nil
}

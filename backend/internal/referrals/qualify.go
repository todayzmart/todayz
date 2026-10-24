package referrals

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/todayz/backend/gen/sqlc/referralsdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// OnPurchaseCompleted is the hook orders and POS call inside the transaction
// that records a completed purchase (01 §8), after their own credit postings.
// It qualifies the buyer's pending customer referral and counts the sale
// toward the selling business's shop referral. It never posts a reward: a
// referral it qualifies gets a referral_reward job in the same transaction,
// which posts each side after the purchase commits (D-169), so a reward can
// never lock ledger accounts inside a purchase or fail one. A database error
// is returned (the caller rolls back).
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
	s.enqueueReward(ctx, row.ID)
	return nil
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
	s.enqueueReward(ctx, row.ID)
	return nil
}

// OnPurchaseReversed is the hook for a reversed purchase (a POS refund or
// void, a reversed order; 01 §7, D-08), inside the reversal's transaction and
// after its credit postings: ledger accounts first, the referral last, the
// order a reward posting takes them in (D-169). A
// qualification whose rewards have not posted goes back to pending, and a
// side marked done without a posting (void, zero credits) is cleared with it:
// posted means a ledger transaction. One with a posted side keeps its rewards
// and raises referral.reversed_after_reward; a side still unposted then is
// paid later all the same (01 §7: a reward stays once earned).
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
			// Posted means a ledger transaction, not a side marked done
			// without one (a deleted beneficiary, zero credits): 01 §7 keeps
			// only a reward that posted (D-169).
			if !row.ReferrerTransactionID.Valid && !row.RefereeTransactionID.Valid {
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

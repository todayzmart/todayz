package referrals

import (
	"context"
	"net/url"

	"github.com/jackc/pgx/v5/pgtype"

	referralsapi "github.com/todayz/backend/gen/api/referrals"
	"github.com/todayz/backend/gen/sqlc/referralsdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// link is the share link for a code; kind is the ?k= hint ("" for none).
func (s *Service) link(code, kind string) string {
	l := s.publicURL + "/v1/public/ref/" + url.PathEscape(code)
	if kind != "" {
		l += "?k=" + kind
	}
	return l
}

func (s *Service) amounts(ctx context.Context, kind string) (referralsapi.RewardAmounts, error) {
	a, b, err := s.rewards(ctx, kind)
	if err != nil {
		return referralsapi.RewardAmounts{}, err
	}
	return referralsapi.RewardAmounts{ReferrerCredits: int(a), RefereeCredits: int(b)}, nil
}

// stats adds up the referrals and clicks of these referrers.
func (s *Service) stats(ctx context.Context, sets map[string][]pgtype.UUID) (referralsapi.ReferralStats, error) {
	var out referralsapi.ReferralStats
	q := s.q(ctx)
	for typ, idsOf := range sets {
		if len(idsOf) == 0 {
			continue
		}
		st, err := q.ReferrerStats(ctx, referralsdb.ReferrerStatsParams{ReferrerType: typ, ReferrerIds: idsOf})
		if err != nil {
			return out, err
		}
		n, err := q.ClickCount(ctx, referralsdb.ClickCountParams{ReferrerType: typ, ReferrerIds: idsOf})
		if err != nil {
			return out, err
		}
		out.Pending += int(st.Pending)
		out.Rewarded += int(st.Rewarded)
		out.CreditsEarned += int(st.CreditsEarned)
		out.Clicks += int(n)
	}
	return out, nil
}

// Me is GET /v1/me/referrals (C-16).
func (s *Service) Me(ctx context.Context, userID ids.ID) (referralsapi.MyReferrals, error) {
	if err := s.ready(ctx); err != nil {
		return referralsapi.MyReferrals{}, err
	}
	q := s.q(ctx)
	code, err := q.CustomerCode(ctx, ids.UUID(userID))
	if err != nil {
		if db.IsNoRows(err) {
			return referralsapi.MyReferrals{}, notFound()
		}
		return referralsapi.MyReferrals{}, asProblem(err)
	}
	out := referralsapi.MyReferrals{Code: code, Link: s.link(code, "c")}
	if out.Rewards, err = s.amounts(ctx, KindCustomerCustomer); err != nil {
		return out, asProblem(err)
	}
	if out.Stats, err = s.stats(ctx, map[string][]pgtype.UUID{partyCustomer: {ids.UUID(userID)}}); err != nil {
		return out, asProblem(err)
	}
	row, err := q.RefereeReferral(ctx, referralsdb.RefereeReferralParams{RefereeType: partyCustomer, RefereeID: ids.UUID(userID)})
	switch {
	case err == nil:
		out.ReferredBy = &referralsapi.MyReferredBy{Kind: referralsapi.ReferralKind(row.Kind), Status: referralsapi.ReferralClaimStatus(publicStatus(row.Status))}
	case !db.IsNoRows(err):
		return out, asProblem(err)
	}
	return out, nil
}

// Business is GET /v1/biz/businesses/{bid}/referrals (B-10).
func (s *Service) Business(ctx context.Context, businessID ids.ID) (referralsapi.BusinessReferrals, error) {
	if err := s.ready(ctx); err != nil {
		return referralsapi.BusinessReferrals{}, err
	}
	q := s.q(ctx)
	bid := ids.UUID(businessID)
	code, err := q.BusinessCode(ctx, bid)
	if err != nil {
		if db.IsNoRows(err) {
			return referralsapi.BusinessReferrals{}, notFound()
		}
		return referralsapi.BusinessReferrals{}, asProblem(err)
	}
	out := referralsapi.BusinessReferrals{Code: code, Link: s.link(code, "b")}
	if out.Rewards, err = s.amounts(ctx, KindAnyShop); err != nil {
		return out, asProblem(err)
	}
	stores, err := q.BusinessStoreIDs(ctx, bid)
	if err != nil {
		return out, asProblem(err)
	}
	if out.Stats, err = s.stats(ctx, map[string][]pgtype.UUID{partyBusiness: {bid}, partyStore: stores}); err != nil {
		return out, asProblem(err)
	}
	row, err := q.RefereeReferral(ctx, referralsdb.RefereeReferralParams{RefereeType: partyBusiness, RefereeID: bid})
	switch {
	case err == nil:
		n, err := q.ShopSaleCount(ctx, row.ID)
		if err != nil {
			return out, asProblem(err)
		}
		need, err := s.settings.Int(ctx, settings.RefShopQualifyingOrders)
		if err != nil {
			return out, asProblem(err)
		}
		verified, err := q.BusinessVerified(ctx, bid)
		if err != nil {
			return out, asProblem(err)
		}
		out.ReferredBy = &referralsapi.ShopReferredBy{
			Status: referralsapi.ReferralClaimStatus(publicStatus(row.Status)), OrdersCounted: int(n),
			OrdersNeeded: int(max(need, 1)), Verified: verified,
		}
	case !db.IsNoRows(err):
		return out, asProblem(err)
	}
	return out, nil
}

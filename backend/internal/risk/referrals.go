package risk

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	refapi "github.com/todayz/backend/gen/api/admin-referrals"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/riskdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// A-10 Referrals: the list with each referral's flags, and the admin reject.

const maxRejectReason = 300 //settings:ok contract bound (AdminReferralRejectRequest.reason maxLength)

func party(typ string, id pgtype.UUID, label string) refapi.AdminReferralParty {
	p := refapi.AdminReferralParty{Type: refapi.AdminReferralPartyType(typ), Id: mustID(id).String()}
	if label != "" {
		p.Label = ptr(cut(label, maxLabel))
	}
	return p
}

func referralOf(r riskdb.ListReferralsRow) refapi.AdminReferral {
	out := refapi.AdminReferral{
		Id: mustID(r.ID).String(), Kind: refapi.AdminReferralKind(r.Kind), Status: refapi.AdminReferralStatus(r.Status),
		Source: refapi.AdminReferralSource(r.Source), Code: r.Code,
		Referrer:        party(r.ReferrerType, r.ReferrerID, r.ReferrerLabel),
		Referee:         party(r.RefereeType, r.RefereeID, r.RefereeLabel),
		ReferrerCredits: int(r.ReferrerCredits), RefereeCredits: int(r.RefereeCredits),
		RejectReason: r.RejectReason, QualifiedAt: utcPtr(r.QualifiedAt), CreatedAt: r.CreatedAt.UTC(),
		FlagRules: r.FlagRules, OpenFlags: int(r.OpenFlags),
	}
	if out.FlagRules == nil {
		out.FlagRules = []string{}
	}
	// rewardedAt is when the last side was paid: set only once both are.
	if r.ReferrerRewardedAt != nil && r.RefereeRewardedAt != nil {
		t := *r.ReferrerRewardedAt
		if r.RefereeRewardedAt.After(t) {
			t = *r.RefereeRewardedAt
		}
		out.RewardedAt = ptr(t.UTC())
	}
	return out
}

func (s *Service) listReferrals(ctx context.Context, p refapi.ListAdminReferralsParams) (refapi.AdminReferralList, error) {
	if err := s.ready(ctx); err != nil {
		return refapi.AdminReferralList{}, err
	}
	lim, at, id, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return refapi.AdminReferralList{}, err
	}
	arg := riskdb.ListReferralsParams{
		BeforeAt: at, BeforeID: id,
		Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	}
	if p.Status != nil {
		arg.Status = ptr(string(*p.Status))
	}
	if p.Kind != nil {
		arg.Kind = ptr(string(*p.Kind))
	}
	if p.Flagged != nil {
		arg.Flagged = *p.Flagged
	}
	rows, err := s.q(ctx).ListReferrals(ctx, arg)
	if err != nil {
		return refapi.AdminReferralList{}, err
	}
	out := refapi.AdminReferralList{Items: make([]refapi.AdminReferral, 0, min(len(rows), lim)), ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			break
		}
		out.Items = append(out.Items, referralOf(r))
	}
	if len(rows) > lim {
		last := rows[lim-1]
		out.NextCursor = ptr(encodeCursor(last.CreatedAt, mustID(last.ID)))
	}
	return out, nil
}

// RejectReferral stops a referral before any reward posts: from pending, or
// from qualified while neither side has been paid. It runs under the referral's
// row lock, the same lock the reward sweep takes, so a reward cannot post
// half-way through. Once any side is paid it is 409; a reversal is a credits
// action, not this one.
func (s *Service) RejectReferral(ctx context.Context, actor, refID ids.ID, reason string) (refapi.AdminReferral, error) {
	if err := s.auditReady(ctx); err != nil {
		return refapi.AdminReferral{}, err
	}
	reason, err := cleanReason(reason, maxRejectReason)
	if err != nil {
		return refapi.AdminReferral{}, err
	}
	var out refapi.AdminReferral
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		l, err := q.LockReferral(ctx, ids.UUID(refID))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		unpaid := !l.ReferrerTransactionID.Valid && !l.RefereeTransactionID.Valid &&
			l.ReferrerRewardedAt == nil && l.RefereeRewardedAt == nil
		if l.Status != "pending" && (l.Status != "qualified" || !unpaid) {
			return problem.New(genproblems.InvalidTransition, "")
		}
		if err := q.RejectReferral(ctx, riskdb.RejectReferralParams{Now: s.now(), ID: l.ID}); err != nil {
			return err
		}
		if _, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "referral.reject",
			SubjectType: "referral", SubjectID: refID.String(),
			Before: map[string]any{"status": l.Status},
			After:  map[string]any{"status": "rejected", "rejectReason": "admin_rejected", "reason": reason},
		}); err != nil {
			return err
		}
		rows, err := q.ListReferrals(ctx, riskdb.ListReferralsParams{OnlyID: l.ID, Lim: 1})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return notFound()
		}
		out = referralOf(rows[0])
		return nil
	})
	return out, err
}

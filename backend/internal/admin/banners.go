package admin

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	api "github.com/todayz/backend/gen/api/admin-banners"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// A-20 Banner moderation (D-46, D-47; 01 §12). An uploaded banner the
// automatic check did not clear waits as pending_review/needs_review; a person
// clears it (here) or takes it down. Any live banner can be taken down. The
// takedown is the banners service's Remove, called inside this package's
// transaction after the row lock, so two moderators cannot both act on one
// banner. Every change is audited.

// BannerRemover takes a banner down with a reason the shop sees
// (internal/banners implements it and writes its own audit row and event).
type BannerRemover interface {
	Remove(ctx context.Context, bannerID, adminID ids.ID, reason string) error
}

const (
	bannerPending   = "pending_review"
	bannerLive      = "live"
	bannerScheduled = "scheduled"
	bannerEnded     = "ended"
	bannerRemoved   = "removed"
	modNeedsReview  = "needs_review"
)

func bannerOut(r admindb.ListAdminBannersRow) api.AdminBanner {
	id, _ := ids.FromUUID(r.ID)
	store, _ := ids.FromUUID(r.StoreID)
	b := api.AdminBanner{
		Id: id.String(), StoreId: store.String(), StoreName: r.StoreName, StoreCity: r.StoreCity,
		Headline: r.Headline, ImageKey: r.ImageKey, TargetType: api.AdminBannerTargetType(r.TargetType),
		TargetItemCount: int(r.TargetItemCount), StartsAt: r.StartsAt, EndsAt: r.EndsAt,
		Status: api.AdminBannerStatus(r.Status), Moderation: api.AdminBannerModeration(r.Moderation),
		RemoveReason: r.RemoveReason, RemovedAt: r.RemovedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.TargetLabel != "" {
		b.TargetLabel = &r.TargetLabel
	}
	if len(r.Template) > 0 {
		var t api.AdminBannerTemplate
		if json.Unmarshal(r.Template, &t) == nil {
			b.Template = &t
		}
	}
	return b
}

func (s *Service) listBanners(ctx context.Context, p api.ListAdminBannersParams) (api.AdminBannerList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminBannerList{}, err
	}
	lim, at, after, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return api.AdminBannerList{}, err
	}
	state := api.AdminBannerStateReview
	if p.State != nil {
		state = *p.State
	}
	arg := admindb.ListAdminBannersParams{
		State: string(state), AfterAt: at, AfterID: after, Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	}
	if p.StoreId != nil {
		id, err := ids.Parse(*p.StoreId)
		if err != nil {
			return api.AdminBannerList{}, fieldErr("query.storeId", "not an id")
		}
		arg.StoreID = ids.UUID(id)
	}
	rows, err := s.q(ctx).ListAdminBanners(ctx, arg)
	if err != nil {
		return api.AdminBannerList{}, err
	}
	out := api.AdminBannerList{Items: []api.AdminBanner{}, ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			last := rows[lim-1]
			lid, _ := ids.FromUUID(last.ID)
			c := encodeCursor(last.CreatedAt, lid)
			out.NextCursor = &c
			break
		}
		out.Items = append(out.Items, bannerOut(r))
	}
	return out, nil
}

func (s *Service) banner(ctx context.Context, id ids.ID) (api.AdminBanner, error) {
	r, err := s.q(ctx).GetAdminBanner(ctx, ids.UUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminBanner{}, notFound()
	}
	if err != nil {
		return api.AdminBanner{}, err
	}
	return bannerOut(admindb.ListAdminBannersRow(r)), nil
}

// bannerChange runs change on the locked banner inside one transaction.
func (s *Service) bannerChange(ctx context.Context, id ids.ID, change func(ctx context.Context, q *admindb.Queries, cur admindb.LockBannerRow) error) (api.AdminBanner, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminBanner{}, err
	}
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		cur, err := q.LockBanner(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		return change(ctx, q, cur)
	})
	if err != nil {
		return api.AdminBanner{}, err
	}
	return s.banner(ctx, id)
}

// approveBanner clears a held upload. It shows at once, or from its start;
// one whose end has passed while it waited is ended.
func (s *Service) approveBanner(ctx context.Context, actor, id ids.ID, body api.AdminBannerApprove) (api.AdminBanner, error) {
	reason, err := optReason(body.Reason)
	if err != nil {
		return api.AdminBanner{}, err
	}
	return s.bannerChange(ctx, id, func(ctx context.Context, q *admindb.Queries, cur admindb.LockBannerRow) error {
		if cur.Status != bannerPending || cur.Moderation != modNeedsReview {
			return invalidTransition("the banner is not waiting for review")
		}
		now := s.now()
		to := bannerLive
		switch {
		case cur.EndsAt != nil && !cur.EndsAt.After(now):
			to = bannerEnded
		case cur.StartsAt.After(now):
			to = bannerScheduled
		}
		n, err := q.ClearBanner(ctx, admindb.ClearBannerParams{ID: cur.ID, Status: to, Now: now})
		if err != nil {
			return err
		}
		if n != 1 {
			return invalidTransition("the banner is not waiting for review")
		}
		after := map[string]any{"status": to, "moderation": "auto_ok"}
		if reason != nil {
			after["reason"] = *reason
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "banners.approve", SubjectType: "banner", SubjectID: id.String(),
			Before: map[string]any{"status": cur.Status, "moderation": cur.Moderation}, After: after,
		})
		return err
	})
}

// takedownBanner removes a banner for moderation; the reason is shown to the shop.
func (s *Service) takedownBanner(ctx context.Context, actor, id ids.ID, body api.AdminBannerTakedown) (api.AdminBanner, error) {
	reason, err := cleanReason(body.Reason)
	if err != nil {
		return api.AdminBanner{}, err
	}
	if s.banners == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "admin: the banners service is not configured")
		return api.AdminBanner{}, problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return s.bannerChange(ctx, id, func(ctx context.Context, _ *admindb.Queries, cur admindb.LockBannerRow) error {
		if cur.Status == bannerRemoved || cur.Status == bannerEnded {
			return invalidTransition("the banner is already off")
		}
		return s.banners.Remove(ctx, id, actor, reason)
	})
}

package banners

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	bannersapi "github.com/todayz/backend/gen/api/banners"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/bannersdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// maxTargetItems is the most items one banner may point at (ck_store_banners_target).
const maxTargetItems = 20

// --- conversions ----------------------------------------------------------------

func templateOf(raw []byte) *bannersapi.BannerTemplate {
	if len(raw) == 0 {
		return nil
	}
	var t bannersapi.BannerTemplate
	if json.Unmarshal(raw, &t) != nil {
		return nil
	}
	return &t
}

func (s *Service) toAPI(r bannersdb.StoreBanner, st bannersdb.BannerStatsRow) bannersapi.Banner {
	b := bannersapi.Banner{
		Id: ulidOf(r.ID), StoreId: ulidOf(r.StoreID), Headline: r.Headline, ImageKey: r.ImageKey,
		Template: templateOf(r.Template), TargetType: bannersapi.BannerTargetType(r.TargetType),
		TargetItemIds: make([]string, 0, len(r.TargetItemIds)), StartsAt: r.StartsAt, EndsAt: r.EndsAt,
		Status: bannersapi.BannerStatus(r.Status), Moderation: bannersapi.BannerModeration(r.Moderation),
		RemoveReason: r.RemoveReason, Impressions: int(st.Impressions), Clicks: int(st.Clicks),
		AttributedOrders: int(st.AttributedOrders), Version: int(r.Version), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.TargetID.Valid {
		b.TargetId = ptr(ulidOf(r.TargetID))
	}
	for _, id := range r.TargetItemIds {
		b.TargetItemIds = append(b.TargetItemIds, ulidOf(id))
	}
	return b
}

// stats is the roll-up numbers of the banners, by id.
func (s *Service) stats(ctx context.Context, q *bannersdb.Queries, rows []bannersdb.StoreBanner) (map[ids.ID]bannersdb.BannerStatsRow, error) {
	out := map[ids.ID]bannersdb.BannerStatsRow{}
	if len(rows) == 0 {
		return out, nil
	}
	list := make([]pgtype.UUID, len(rows))
	for i, r := range rows {
		list[i] = r.ID
	}
	st, err := q.BannerStats(ctx, list)
	if err != nil {
		return nil, fmt.Errorf("banners: stats: %w", err)
	}
	for _, r := range st {
		out[idOf(r.BannerID)] = r
	}
	return out, nil
}

// --- reads ----------------------------------------------------------------------

func (s *Service) getBanner(ctx context.Context, sid, bannerID string) (bannersapi.Banner, int32, error) {
	if err := s.ready(ctx); err != nil {
		return bannersapi.Banner{}, 0, err
	}
	store, err := pathID(sid)
	if err != nil {
		return bannersapi.Banner{}, 0, err
	}
	id, err := pathID(bannerID)
	if err != nil {
		return bannersapi.Banner{}, 0, err
	}
	q := s.q(ctx)
	r, err := q.GetBanner(ctx, bannersdb.GetBannerParams{ID: ids.UUID(id), StoreID: ids.UUID(store)})
	if db.IsNoRows(err) {
		return bannersapi.Banner{}, 0, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return bannersapi.Banner{}, 0, fmt.Errorf("banners: get banner: %w", err)
	}
	st, err := s.stats(ctx, q, []bannersdb.StoreBanner{r})
	if err != nil {
		return bannersapi.Banner{}, 0, err
	}
	return s.toAPI(r, st[id]), r.Version, nil
}

func (s *Service) listBanners(ctx context.Context, sid string, p bannersapi.ListBannersParams) (bannersapi.BannerList, error) {
	if err := s.ready(ctx); err != nil {
		return bannersapi.BannerList{}, err
	}
	store, err := pathID(sid)
	if err != nil {
		return bannersapi.BannerList{}, err
	}
	lim := int32(20)
	if p.Limit != nil {
		lim = int32(min(max(*p.Limit, 1), 100)) //nolint:gosec // clamped
	}
	arg := bannersdb.ListBannersParams{StoreID: ids.UUID(store), Lim: lim + 1}
	if p.Status != nil {
		arg.Status = ptr(string(*p.Status))
	}
	if p.Cursor != nil {
		raw, derr := base64.RawURLEncoding.DecodeString(*p.Cursor)
		after, perr := ids.Parse(string(raw))
		if derr != nil || perr != nil {
			return bannersapi.BannerList{}, fieldErr("query.cursor", "not a cursor from this list")
		}
		arg.AfterID = ids.UUID(after)
	}
	q := s.q(ctx)
	rows, err := q.ListBanners(ctx, arg)
	if err != nil {
		return bannersapi.BannerList{}, fmt.Errorf("banners: list banners: %w", err)
	}
	out := bannersapi.BannerList{Items: make([]bannersapi.Banner, 0, min(len(rows), int(lim)))}
	if len(rows) > int(lim) {
		out.NextCursor = ptr(base64.RawURLEncoding.EncodeToString([]byte(ulidOf(rows[lim-1].ID))))
		rows = rows[:lim]
	}
	st, err := s.stats(ctx, q, rows)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		out.Items = append(out.Items, s.toAPI(r, st[idOf(r.ID)]))
	}
	return out, nil
}

// --- create ---------------------------------------------------------------------

// limitReached is the 422 for a shop already at store_banners_max.
func limitReached(max int) error {
	return problem.New(genproblems.BannerLimitReached, fmt.Sprintf("A shop can have %d live or scheduled banners.", max))
}

// slots checks, under the store's row lock, that the shop has a free banner
// slot. The lock is the whole point: it makes the count and the write one step.
func (s *Service) slots(ctx context.Context, q *bannersdb.Queries, store ids.ID) error {
	if _, err := q.LockStore(ctx, ids.UUID(store)); err != nil {
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		return fmt.Errorf("banners: lock store: %w", err)
	}
	max, err := s.settings.Int(ctx, settings.StoreBannersMax)
	if err != nil {
		return fmt.Errorf("banners: store_banners_max: %w", err)
	}
	n, err := q.CountActiveBanners(ctx, ids.UUID(store))
	if err != nil {
		return fmt.Errorf("banners: count banners: %w", err)
	}
	if int(n) >= int(max) {
		return limitReached(int(max))
	}
	return nil
}

func (s *Service) createBanner(ctx context.Context, sid string, b *bannersapi.BannerCreate) (bannersapi.Banner, int32, error) {
	if err := s.ready(ctx); err != nil {
		return bannersapi.Banner{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return bannersapi.Banner{}, 0, err
	}
	store, err := pathID(sid)
	if err != nil {
		return bannersapi.Banner{}, 0, err
	}
	if b == nil {
		return bannersapi.Banner{}, 0, problem.New(genproblems.BadRequest, "")
	}
	if on, err := s.enabled(ctx); err != nil {
		return bannersapi.Banner{}, 0, err
	} else if !on {
		return bannersapi.Banner{}, 0, problem.New(genproblems.ServiceUnavailable, "Banners are switched off.")
	}
	now := s.now()

	var v invalid
	v.text("body.headline", b.Headline)
	if (b.ImageKey == nil) == (b.Template == nil) {
		v.add("body", "send exactly one of imageKey and template")
	}
	if b.ImageKey != nil && strings.TrimSpace(*b.ImageKey) == "" {
		v.add("body.imageKey", "must not be blank")
	}
	var tmpl []byte
	if b.Template != nil {
		if b.Template.Subline != nil {
			v.text("body.template.subline", *b.Template.Subline)
		}
		if tmpl, err = json.Marshal(b.Template); err != nil {
			return bannersapi.Banner{}, 0, fmt.Errorf("banners: template: %w", err)
		}
	}
	start := now
	if b.StartsAt != nil && b.StartsAt.After(now) {
		start = *b.StartsAt
	}
	if b.EndsAt != nil && !b.EndsAt.After(start) {
		v.add("body.endsAt", "must be after the start")
	}
	if b.EndsAt != nil && !b.EndsAt.After(now) {
		v.add("body.endsAt", "must be in the future")
	}
	var targetItems []ids.ID
	var targetID ids.ID
	switch string(b.TargetType) {
	case "store":
		if b.TargetId != nil {
			v.add("body.targetId", "does not apply to a shop banner")
		}
		if b.TargetItemIds != nil {
			v.add("body.targetItemIds", "does not apply to a shop banner")
		}
	case "offer", "category":
		if b.TargetItemIds != nil {
			v.add("body.targetItemIds", "does not apply to this target")
		}
		if b.TargetId == nil {
			v.add("body.targetId", "required")
		} else if targetID, err = ids.Parse(*b.TargetId); err != nil {
			v.add("body.targetId", "not an id")
		}
	case "items":
		if b.TargetId != nil {
			v.add("body.targetId", "does not apply to an items banner")
		}
		if b.TargetItemIds == nil || len(*b.TargetItemIds) == 0 {
			v.add("body.targetItemIds", "needs at least one item")
		} else if len(*b.TargetItemIds) > maxTargetItems {
			v.add("body.targetItemIds", fmt.Sprintf("at most %d items", maxTargetItems))
		} else {
			for i, raw := range *b.TargetItemIds {
				id, err := ids.Parse(raw)
				if err != nil {
					v.add(fmt.Sprintf("body.targetItemIds[%d]", i), "not an item id")
					continue
				}
				if !slices.Contains(targetItems, id) {
					targetItems = append(targetItems, id)
				}
			}
		}
	}
	if err := v.err(); err != nil {
		return bannersapi.Banner{}, 0, err
	}

	// An uploaded image is held for review; a template is ours and shows now.
	origin, moderation, status := "template", modAuto, statusLive
	if b.ImageKey != nil {
		origin, moderation, status = "upload", modNeeds, statusPending
	} else if start.After(now) {
		status = statusScheduled
	}

	var created bannersdb.StoreBanner
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		// The target must be the shop's own.
		switch string(b.TargetType) {
		case "offer":
			if _, err := q.OfferOfStore(ctx, bannersdb.OfferOfStoreParams{ID: ids.UUID(targetID), StoreID: ids.UUID(store)}); err != nil {
				if db.IsNoRows(err) {
					return fieldErr("body.targetId", "not an open offer of this shop")
				}
				return fmt.Errorf("banners: offer of store: %w", err)
			}
		case "category":
			ok, err := q.CategoryExists(ctx, ids.UUID(targetID))
			if err != nil {
				return fmt.Errorf("banners: category: %w", err)
			}
			if !ok {
				return fieldErr("body.targetId", "no such category")
			}
		case "items":
			have, err := q.ItemsOfStore(ctx, bannersdb.ItemsOfStoreParams{StoreID: ids.UUID(store), Ids: uuids(targetItems)})
			if err != nil {
				return fmt.Errorf("banners: items of store: %w", err)
			}
			if len(have) != len(targetItems) {
				return fieldErr("body.targetItemIds", "an item is not of this shop")
			}
		}
		if err := s.slots(ctx, q, store); err != nil {
			return err
		}
		id := s.ids.New(now)
		arg := bannersdb.InsertBannerParams{
			ID: ids.UUID(id), StoreID: ids.UUID(store), ImageKey: b.ImageKey, Template: tmpl,
			Headline: strings.TrimSpace(b.Headline), TargetType: string(b.TargetType), TargetItemIds: uuids(targetItems),
			StartsAt: start, EndsAt: b.EndsAt, Status: status, Moderation: moderation,
			CreatedBy: ids.UUID(p.UserID), Now: now,
		}
		if targetID != ids.Zero {
			arg.TargetID = ids.UUID(targetID)
		}
		if created, err = q.InsertBanner(ctx, arg); err != nil {
			return fmt.Errorf("banners: insert banner: %w", err)
		}
		if _, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorUser, ActorID: p.UserID, Action: "banners.create", SubjectType: "banner",
			SubjectID: id.String(), After: auditView(created),
		}); err != nil {
			return err
		}
		s.event(ctx, q, ids.UUID(p.UserID), "banner_created", map[string]any{
			"banner_id": id.String(), "target": string(b.TargetType), "origin": origin, "moderation": moderation,
		})
		return nil
	})
	if err != nil {
		return bannersapi.Banner{}, 0, err
	}
	return s.toAPI(created, bannersdb.BannerStatsRow{}), created.Version, nil
}

// auditView is the part of a banner the audit log keeps.
func auditView(r bannersdb.StoreBanner) map[string]any {
	return map[string]any{
		"status": r.Status, "moderation": r.Moderation, "target_type": r.TargetType,
		"starts_at": r.StartsAt, "ends_at": r.EndsAt,
	}
}

// --- update ---------------------------------------------------------------------

func (s *Service) updateBanner(ctx context.Context, sid, bannerID string, ifm *string, b *bannersapi.BannerUpdate, sent func(string) bool) (bannersapi.Banner, int32, error) {
	if err := s.ready(ctx); err != nil {
		return bannersapi.Banner{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return bannersapi.Banner{}, 0, err
	}
	store, err := pathID(sid)
	if err != nil {
		return bannersapi.Banner{}, 0, err
	}
	id, err := pathID(bannerID)
	if err != nil {
		return bannersapi.Banner{}, 0, err
	}
	version, err := ifMatch(ifm)
	if err != nil {
		return bannersapi.Banner{}, 0, err
	}
	if b == nil {
		return bannersapi.Banner{}, 0, problem.New(genproblems.BadRequest, "")
	}
	var v invalid
	if sent("status") && b.Status == nil {
		v.add("body.status", "must not be null")
	}
	if sent("startsAt") && b.StartsAt == nil {
		v.add("body.startsAt", "must not be null")
	}
	if err := v.err(); err != nil {
		return bannersapi.Banner{}, 0, err
	}
	now := s.now()
	var out bannersdb.StoreBanner
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		// Lock order: the store first (the slot count), then the banner.
		if _, err := q.LockStore(ctx, ids.UUID(store)); err != nil {
			if db.IsNoRows(err) {
				return problem.New(genproblems.NotFound, "")
			}
			return fmt.Errorf("banners: lock store: %w", err)
		}
		cur, err := q.LockBanner(ctx, bannersdb.LockBannerParams{ID: ids.UUID(id), StoreID: ids.UUID(store)})
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return fmt.Errorf("banners: lock banner: %w", err)
		}
		if cur.Version != version {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		if cur.Status == statusEnded || cur.Status == statusRemoved {
			return problem.New(genproblems.InvalidTransition, "an ended banner cannot change")
		}
		arg, resume, err := nextState(cur, b, sent, now)
		if err != nil {
			return err
		}
		if resume {
			// Back from paused takes a slot again.
			max, err := s.settings.Int(ctx, settings.StoreBannersMax)
			if err != nil {
				return fmt.Errorf("banners: store_banners_max: %w", err)
			}
			n, err := q.CountActiveBanners(ctx, ids.UUID(store))
			if err != nil {
				return fmt.Errorf("banners: count banners: %w", err)
			}
			if int(n) >= int(max) {
				return limitReached(int(max))
			}
		}
		if out, err = q.UpdateBanner(ctx, arg); err != nil {
			return fmt.Errorf("banners: update banner: %w", err)
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorUser, ActorID: p.UserID, Action: "banners.update", SubjectType: "banner",
			SubjectID: id.String(), Before: auditView(cur), After: auditView(out),
		})
		return err
	})
	if err != nil {
		return bannersapi.Banner{}, 0, err
	}
	st, err := s.stats(ctx, s.q(ctx), []bannersdb.StoreBanner{out})
	if err != nil {
		return bannersapi.Banner{}, 0, err
	}
	return s.toAPI(out, st[id]), out.Version, nil
}

// nextState applies the sent fields to the locked row. resume reports a move
// from paused back to live or scheduled, which needs a free slot. A banner
// held for review can only be ended; the start moves only before it starts.
func nextState(cur bannersdb.StoreBanner, b *bannersapi.BannerUpdate, sent func(string) bool, now time.Time) (bannersdb.UpdateBannerParams, bool, error) {
	arg := bannersdb.UpdateBannerParams{
		ID: cur.ID, StoreID: cur.StoreID, Headline: cur.Headline, Template: cur.Template, ImageKey: cur.ImageKey,
		TargetType: cur.TargetType, TargetID: cur.TargetID, TargetItemIds: cur.TargetItemIds,
		StartsAt: cur.StartsAt, EndsAt: cur.EndsAt, Status: cur.Status, Moderation: cur.Moderation, Now: now,
	}
	var v invalid
	if sent("startsAt") && b.StartsAt != nil {
		if !cur.StartsAt.After(now) {
			v.add("body.startsAt", "a banner that has started keeps its start")
		} else {
			arg.StartsAt = *b.StartsAt
			if arg.StartsAt.Before(now) {
				arg.StartsAt = now
			}
		}
	}
	if sent("endsAt") {
		switch {
		case b.EndsAt == nil:
			arg.EndsAt = nil
		case !b.EndsAt.After(now):
			v.add("body.endsAt", "must be in the future")
		default:
			e := *b.EndsAt
			arg.EndsAt = &e
		}
	}
	if arg.EndsAt != nil && !arg.EndsAt.After(arg.StartsAt) {
		v.add("body.endsAt", "must be after the start")
	}
	if err := v.err(); err != nil {
		return arg, false, err
	}

	want := cur.Status
	resume := false
	if sent("status") && b.Status != nil {
		switch *b.Status {
		case bannersapi.BannerUpdateStatusEnded:
			want = statusEnded
		case bannersapi.BannerUpdateStatusPaused:
			if cur.Status == statusPending {
				return arg, false, problem.New(genproblems.InvalidTransition, "a banner in review cannot be paused")
			}
			want = statusPaused
		case bannersapi.BannerUpdateStatusLive:
			switch cur.Status {
			case statusPaused:
				if cur.Moderation != modAuto {
					return arg, false, problem.New(genproblems.InvalidTransition, "the banner is not cleared yet")
				}
				if arg.EndsAt != nil && !arg.EndsAt.After(now) {
					return arg, false, problem.New(genproblems.InvalidTransition, "the banner's end has passed")
				}
				want, resume = statusLive, true
			case statusPending:
				return arg, false, problem.New(genproblems.InvalidTransition, "the banner is not cleared yet")
			}
		}
	}
	// Live and scheduled follow the dates.
	if want == statusLive || want == statusScheduled {
		want = statusLive
		if arg.StartsAt.After(now) {
			want = statusScheduled
		}
	}
	arg.Status = want
	return arg, resume, nil
}

// --- tap ------------------------------------------------------------------------

// tap records a signed-in customer's tap on a live banner. The last tap per
// (customer, banner) wins; the order attribution reads it (D-77).
func (s *Service) tap(ctx context.Context, bannerID string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	id, err := pathID(bannerID)
	if err != nil {
		return err
	}
	now := s.now()
	q := s.q(ctx)
	b, err := q.PublicBanner(ctx, bannersdb.PublicBannerParams{ID: ids.UUID(id), At: now})
	if db.IsNoRows(err) {
		return problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return fmt.Errorf("banners: public banner: %w", err)
	}
	if err := q.RecordTap(ctx, bannersdb.RecordTapParams{
		CustomerID: ids.UUID(p.UserID), BannerID: b.ID, StoreID: b.StoreID, TappedAt: now,
	}); err != nil {
		return fmt.Errorf("banners: record tap: %w", err)
	}
	s.event(ctx, q, ids.UUID(p.UserID), "banner_tapped", map[string]any{"banner_id": id.String(), "target": b.TargetType})
	return nil
}

// --- other domains ---------------------------------------------------------------

// LiveBanners is the banners that show at time t in these stores (the feed's
// BannerSource, adapted in cmd/todayz). With feature_banners_enabled off it
// returns none.
func (s *Service) LiveBanners(ctx context.Context, storeIDs []ids.ID, t time.Time) ([]LiveBanner, error) {
	if len(storeIDs) == 0 {
		return nil, nil
	}
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if on, err := s.enabled(ctx); err != nil || !on {
		return nil, err
	}
	rows, err := s.q(ctx).LiveBannersForStores(ctx, bannersdb.LiveBannersForStoresParams{StoreIds: uuids(storeIDs), At: t})
	if err != nil {
		return nil, fmt.Errorf("banners: live banners: %w", err)
	}
	out := make([]LiveBanner, 0, len(rows))
	for _, r := range rows {
		lb := LiveBanner{
			ID: idOf(r.ID), StoreID: idOf(r.StoreID), Headline: r.Headline, ImageKey: r.ImageKey,
			TargetType: r.TargetType, TargetItemIDs: idList(r.TargetItemIds),
		}
		if t := templateMap(r.Template); t != nil {
			lb.Template = t
		}
		if id, ok := ids.FromUUID(r.TargetID); ok {
			lb.TargetID = &id
		}
		out = append(out, lb)
	}
	return out, nil
}

func templateMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// Remove takes a banner down for moderation (W3-05 calls it). It is final and
// frees the shop's slot. The reason is shown to the shop.
func (s *Service) Remove(ctx context.Context, bannerID, adminID ids.ID, reason string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fieldErr("reason", "required")
	}
	now := s.now()
	return s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		r, err := q.RemoveBanner(ctx, bannersdb.RemoveBannerParams{
			RemovedBy: ids.UUID(adminID), Now: &now, Reason: &reason, ID: ids.UUID(bannerID),
		})
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return fmt.Errorf("banners: remove banner: %w", err)
		}
		if _, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: adminID, Action: "banners.remove", SubjectType: "banner",
			SubjectID: bannerID.String(), After: auditView(r),
		}); err != nil {
			return err
		}
		s.event(ctx, q, ids.UUID(adminID), "banner_removed", map[string]any{"banner_id": bannerID.String(), "by": "moderation"})
		return nil
	})
}

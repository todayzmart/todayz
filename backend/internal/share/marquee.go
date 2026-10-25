package share

// The site's "What people near you are searching for" strip (01 §11.3,
// S-07): built nightly as a proposal per city, shown only once an admin has
// approved it (A-23 Site strip).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	admindemandapi "github.com/todayz/backend/gen/api/admin-demand"
	shareapi "github.com/todayz/backend/gen/api/share"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/asksdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/geo/h3"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

const day = 24 * time.Hour

// Build proposes each city's strip for the IST day of at: master products
// searched in the site_marquee_window_days full days before it. A city whose
// list for that day is already decided keeps it; a rerun replaces its own
// proposal. It returns the number of cities proposed.
func (s *Service) Build(ctx context.Context, at time.Time) (int64, error) {
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	days, err := s.settings.Int(ctx, settings.SiteMarqueeWindowDays)
	if err != nil {
		return 0, err
	}
	minSearchers, err := s.floorSetting(ctx, settings.SiteMarqueeMinSearchers)
	if err != nil {
		return 0, err
	}
	maxItems, err := s.settings.Int(ctx, settings.SiteMarqueeMaxItems)
	if err != nil {
		return 0, err
	}
	until := clock.StartOfDayIST(at)
	since := until.Add(-time.Duration(days) * day)
	builtOn := istDate(at)

	var cities int64
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := s.fillCells(ctx, q, since, until); err != nil {
			return err
		}
		if err := q.DeleteProposedBuild(ctx, builtOn); err != nil {
			return fmt.Errorf("share: delete proposed build: %w", err)
		}
		decided, err := q.DecidedCities(ctx, builtOn)
		if err != nil {
			return fmt.Errorf("share: decided cities: %w", err)
		}
		rows, err := q.MarqueeCandidates(ctx, asksdb.MarqueeCandidatesParams{
			Since: since, Until: until, Denylist: Denylist,
			MinSearchers: int32(minSearchers), MaxItems: int32(maxItems), // #nosec G115 -- small settings
		})
		if err != nil {
			return fmt.Errorf("share: marquee candidates: %w", err)
		}
		seen := map[pgtype.UUID]bool{}
		now := s.now()
		for _, r := range rows {
			if slices.Contains(decided, r.CityPlaceID) {
				continue
			}
			if err := q.InsertMarquee(ctx, asksdb.InsertMarqueeParams{
				ID: ids.UUID(s.ids.New(now)), CityPlaceID: r.CityPlaceID, ProductID: r.ProductID,
				Searchers: r.Searchers, SearchesTotal: r.SearchesTotal, Rank: r.Rank, BuiltOn: builtOn,
			}); err != nil {
				return fmt.Errorf("share: insert marquee: %w", err)
			}
			seen[r.CityPlaceID] = true
		}
		cities = int64(len(seen))
		return nil
	})
	return cities, err
}

// fillCells gives every H3 cell searched in the window a centre, so the
// candidates query can place it in a city.
func (s *Service) fillCells(ctx context.Context, q *asksdb.Queries, since, until time.Time) error {
	cells, err := q.MarqueeCellsMissing(ctx, asksdb.MarqueeCellsMissingParams{Since: since, Until: until})
	if err != nil {
		return fmt.Errorf("share: missing cells: %w", err)
	}
	for _, c := range cells {
		lat, lng, err := h3.CellToLatLng(h3.FromInt64(c))
		if err != nil {
			telemetry.Logger(ctx).WarnContext(ctx, "share: bad search cell", slog.String("error", err.Error()))
			continue
		}
		if err := q.InsertGeoCell(ctx, asksdb.InsertGeoCellParams{GeoCell: c, Lat: lat, Lng: lng}); err != nil {
			return fmt.Errorf("share: insert cell: %w", err)
		}
	}
	return nil
}

// siteMarquee is the public strip of a city: its latest approved list, or
// fallback when there is none.
func (s *Service) siteMarquee(ctx context.Context, city string) (shareapi.SiteMarquee, error) {
	if err := s.ready(ctx); err != nil {
		return shareapi.SiteMarquee{}, err
	}
	names, err := s.q(ctx).ApprovedMarquee(ctx, city)
	if err != nil {
		return shareapi.SiteMarquee{}, fmt.Errorf("share: approved marquee: %w", err)
	}
	items := make([]shareapi.SiteMarqueeItem, 0, len(names))
	for _, n := range names {
		items = append(items, shareapi.SiteMarqueeItem{ProductName: n})
	}
	return shareapi.SiteMarquee{Fallback: len(items) == 0, Items: items}, nil
}

// adminMarquee lists each city's latest build and latest approved build.
func (s *Service) adminMarquee(ctx context.Context, params admindemandapi.ListSiteMarqueeParams) (admindemandapi.MarqueeCityList, error) {
	if err := s.ready(ctx); err != nil {
		return admindemandapi.MarqueeCityList{}, err
	}
	var city pgtype.UUID
	if params.City != nil {
		id, err := ids.Parse(*params.City)
		if err != nil {
			return admindemandapi.MarqueeCityList{}, fieldErr("query.city", "invalid")
		}
		city = ids.UUID(id)
	}
	minSearchers, err := s.floorSetting(ctx, settings.SiteMarqueeMinSearchers)
	if err != nil {
		return admindemandapi.MarqueeCityList{}, err
	}
	cities, err := s.marqueeCities(ctx, city)
	if err != nil {
		return admindemandapi.MarqueeCityList{}, err
	}
	return admindemandapi.MarqueeCityList{Cities: cities, MinSearchers: minSearchers, ServerNow: s.now()}, nil
}

func (s *Service) marqueeCities(ctx context.Context, city pgtype.UUID) ([]admindemandapi.MarqueeCity, error) {
	rows, err := s.q(ctx).MarqueeAdminRows(ctx, city)
	if err != nil {
		return nil, fmt.Errorf("share: marquee rows: %w", err)
	}
	out := []admindemandapi.MarqueeCity{}
	for _, r := range rows {
		if len(out) == 0 || out[len(out)-1].CityId != idOf(r.CityPlaceID).String() {
			out = append(out, admindemandapi.MarqueeCity{
				CityId: idOf(r.CityPlaceID).String(), CityName: r.CityName, CitySlug: r.CitySlug,
			})
		}
		c := &out[len(out)-1]
		item := admindemandapi.MarqueeItem{
			ProductId: idOf(r.ProductID).String(), ProductName: r.ProductName, Category: strPtr(r.CategoryName),
			Rank: int(r.Rank), Searchers: int(r.Searchers), SearchesTotal: int(r.SearchesTotal),
			Status: admindemandapi.MarqueeStatus(r.Status),
		}
		if r.IsLatest {
			c.Latest = addItem(c.Latest, r, item)
		}
		if r.IsApproved {
			c.Approved = addItem(c.Approved, r, item)
		}
	}
	for i := range out {
		settle(out[i].Latest)
		settle(out[i].Approved)
	}
	return out, nil
}

func addItem(b *admindemandapi.MarqueeBuild, r asksdb.MarqueeAdminRowsRow, item admindemandapi.MarqueeItem) *admindemandapi.MarqueeBuild {
	if b == nil {
		b = &admindemandapi.MarqueeBuild{BuiltOn: openapi_types.Date{Time: r.BuiltOn.Time}, Items: []admindemandapi.MarqueeItem{}}
	}
	if r.DecidedAt != nil && (b.DecidedAt == nil || r.DecidedAt.After(*b.DecidedAt)) {
		t := r.DecidedAt.UTC()
		b.DecidedAt = &t
	}
	b.Items = append(b.Items, item)
	return b
}

// settle gives a build its status: proposed while any row is, else approved
// when any row was approved, else rejected.
func settle(b *admindemandapi.MarqueeBuild) {
	if b == nil {
		return
	}
	b.Status = admindemandapi.Rejected
	for _, it := range b.Items {
		switch it.Status {
		case admindemandapi.Proposed:
			b.Status = admindemandapi.Proposed
			return
		case admindemandapi.Approved:
			b.Status = admindemandapi.Approved
		}
	}
}

// decide approves a subset of a city's latest proposed build (the rest are
// rejected) or rejects all of it, audited.
func (s *Service) decide(ctx context.Context, cityID string, body *admindemandapi.MarqueeDecision) (admindemandapi.MarqueeCity, error) {
	if err := s.ready(ctx); err != nil {
		return admindemandapi.MarqueeCity{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return admindemandapi.MarqueeCity{}, err
	}
	cid, err := ids.Parse(cityID)
	if err != nil {
		return admindemandapi.MarqueeCity{}, problem.New(genproblems.NotFound, "")
	}
	var want []string
	if body.ProductIds != nil {
		want = *body.ProductIds
	}
	switch body.Decision {
	case admindemandapi.Approve:
		if len(want) == 0 {
			return admindemandapi.MarqueeCity{}, fieldErr("body.productIds", "required")
		}
	case admindemandapi.Reject:
		if len(want) != 0 {
			return admindemandapi.MarqueeCity{}, fieldErr("body.productIds", "must be empty")
		}
	default:
		return admindemandapi.MarqueeCity{}, fieldErr("body.decision", "invalid")
	}
	city := ids.UUID(cid)
	builtOn := pgtype.Date{Time: body.BuiltOn.Time, Valid: true}

	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		latest, err := q.LatestBuildOn(ctx, city)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("share: latest build: %w", err)
		}
		if !latest.Valid {
			return problem.New(genproblems.NotFound, "")
		}
		if !sameDate(latest, builtOn) {
			return problem.New(genproblems.InvalidTransition, "Only the latest build can be decided.")
		}
		rows, err := q.LockBuild(ctx, asksdb.LockBuildParams{CityPlaceID: city, BuiltOn: builtOn})
		if err != nil {
			return fmt.Errorf("share: lock build: %w", err)
		}
		proposed := map[string]bool{}
		for _, r := range rows {
			if r.Status != string(admindemandapi.Proposed) {
				return problem.New(genproblems.InvalidTransition, "This build is already decided.")
			}
			proposed[idOf(r.ProductID).String()] = true
		}
		approve := make([]pgtype.UUID, 0, len(want))
		for i, w := range want {
			id, err := ids.Parse(w)
			if err != nil || !proposed[id.String()] {
				return fieldErr(fmt.Sprintf("body.productIds[%d]", i), "not in this build")
			}
			approve = append(approve, ids.UUID(id))
		}
		now := s.now()
		if _, err := q.DecideBuild(ctx, asksdb.DecideBuildParams{
			ApproveIds: approve, DecidedBy: ids.UUID(p.UserID), Now: &now, CityPlaceID: city, BuiltOn: builtOn,
		}); err != nil {
			return fmt.Errorf("share: decide build: %w", err)
		}
		if _, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: p.UserID, Action: "marquee.decide",
			SubjectType: "site_marquee", SubjectID: cid.String(),
			Before: map[string]any{"builtOn": body.BuiltOn.String(), "status": "proposed"},
			After:  map[string]any{"builtOn": body.BuiltOn.String(), "decision": string(body.Decision), "productIds": want},
		}); err != nil {
			return fmt.Errorf("share: audit: %w", err)
		}
		return nil
	})
	if err != nil {
		return admindemandapi.MarqueeCity{}, err
	}
	cities, err := s.marqueeCities(ctx, city)
	if err != nil {
		return admindemandapi.MarqueeCity{}, err
	}
	if len(cities) == 0 {
		return admindemandapi.MarqueeCity{}, problem.New(genproblems.NotFound, "")
	}
	return cities[0], nil
}

// istDate is the IST calendar date of t.
func istDate(t time.Time) pgtype.Date {
	return pgDate(t.In(clock.IST))
}

func sameDate(a, b pgtype.Date) bool {
	ay, am, ad := a.Time.Date()
	by, bm, bd := b.Time.Date()
	return ay == by && am == bm && ad == bd
}

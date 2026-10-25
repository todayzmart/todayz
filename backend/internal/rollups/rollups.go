// Package rollups computes the analytics rollups (W3-07, 05 §3) and serves
// the admin Pulse (A-02).
//
// The metric definitions are SQL views (v_metric_*, migration 30701) that
// dashboards 07–11 read too, so Grafana and Pulse agree. The rollup jobs
// upsert one row per IST day (platform, store, search demand, banners), a
// weekly cohort grid and a store's item pairs; every run recomputes its
// whole period, so a retry, an overlap or a backfill gives the same rows.
//
// Days are IST calendar days. An order counts on the day it was placed, a POS
// sale on the day it was sold. "Now" for a past day is the end of that day, so
// staleness and the live-store test read the way they did at midnight.
package rollups

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/rollupsdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

const (
	// cohortWeeks is how many weekly cohorts rollup_cohorts recomputes:
	// week 0 to week 12 (05 §3: W0–W12), so 13 weeks.
	cohortWeeks = 13 //settings:ok fixed by the plan (05 §3, W0–W12)
	// freshWindow is the "updated within 24 h" freshness line (05 §3); the
	// other line is stock_stale_after_hours.
	freshWindow = 24 * time.Hour
)

// Deps are the service's collaborators.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
}

// Service computes rollups and answers Pulse.
type Service struct {
	db       *db.DB
	clock    clock.Clock
	settings *settings.Cache
}

// New builds the service.
func New(d Deps) *Service {
	return &Service{db: d.DB, clock: d.Clock, settings: d.Settings}
}

func (s *Service) q(ctx context.Context) *rollupsdb.Queries { return rollupsdb.New(s.db.Q(ctx)) }

// Day is an IST calendar day, held as midnight UTC of that date.
type Day time.Time

// DayOf returns the IST day holding t.
func DayOf(t time.Time) Day {
	y, m, d := t.In(clock.IST).Date()
	return Day(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
}

// ParseDay reads "2006-01-02".
func ParseDay(s string) (Day, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Day{}, fmt.Errorf("rollups: bad day %q", s)
	}
	return Day(t), nil
}

// Start is the day's first instant, in UTC.
func (d Day) Start() time.Time {
	t := time.Time(d)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, clock.IST).UTC()
}

// End is the next day's first instant, in UTC.
func (d Day) End() time.Time { return d.Add(1).Start() }

// Add moves n days.
func (d Day) Add(n int) Day { return Day(time.Time(d).AddDate(0, 0, n)) }

// Before reports whether d is earlier than e.
func (d Day) Before(e Day) bool { return time.Time(d).Before(time.Time(e)) }

// Monday is the Monday of d's ISO week.
func (d Day) Monday() Day {
	wd := int(time.Time(d).Weekday()+6) % 7 // Monday 0 … Sunday 6
	return d.Add(-wd)
}

func (d Day) String() string { return time.Time(d).Format(time.DateOnly) }

func (d Day) pg() pgtype.Date { return pgtype.Date{Time: time.Time(d), Valid: true} }

func dayFromPG(p pgtype.Date) Day { return Day(p.Time) }

// asOf is what "now" means for day d: now itself while the day runs, the
// day's end once it is over.
func asOf(d Day, now time.Time) time.Time {
	if end := d.End(); now.After(end) {
		return end
	}
	return now
}

// DayResult counts what one day's computation wrote.
type DayResult struct {
	Stores int64
}

// ComputeDay recomputes the platform row and the store rows for day d in one
// transaction. The row is final once the day is over.
func (s *Service) ComputeDay(ctx context.Context, d Day) (DayResult, error) {
	now := s.clock.Now()
	stale, err := s.settings.Duration(ctx, settings.StockStaleAfterHours)
	if err != nil {
		return DayResult{}, err
	}
	at := asOf(d, now)
	var res DayResult
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := q.UpsertPlatformDay(ctx, rollupsdb.UpsertPlatformDayParams{
			OnDate: d.pg(), Final: !now.Before(d.End()), ComputedAt: now,
			DayStart: d.Start(), DayEnd: d.End(),
			WeekFrom: d.Add(-6).Start(), MonthFrom: d.Add(-29).Start(),
			D1Day: d.Add(-1).pg(), D7Day: d.Add(-7).pg(), D30Day: d.Add(-30).pg(),
			StaleBefore: at.Add(-stale),
		}); err != nil {
			return fmt.Errorf("rollups: platform day %s: %w", d, err)
		}
		n, err := q.UpsertStoreDays(ctx, rollupsdb.UpsertStoreDaysParams{
			OnDate: d.pg(), ComputedAt: now, DayStart: d.Start(), DayEnd: d.End(),
			StaleBefore: at.Add(-stale), FreshAfter: at.Add(-freshWindow),
		})
		if err != nil {
			return fmt.Errorf("rollups: store days %s: %w", d, err)
		}
		res.Stores = n
		return nil
	})
	return res, err
}

// ComputeDemand recomputes day d's search demand (05 §3, rollup_demand).
func (s *Service) ComputeDemand(ctx context.Context, d Day) (int64, error) {
	n, err := s.q(ctx).UpsertDemandDay(ctx, rollupsdb.UpsertDemandDayParams{
		OnDate: d.pg(), DayStart: d.Start(), DayEnd: d.End(),
	})
	if err != nil {
		return 0, fmt.Errorf("rollups: demand %s: %w", d, err)
	}
	return n, nil
}

// ComputeCohorts recomputes the customer and store cohorts of the last 13
// weeks up to day d (05 §3, rollup_cohorts).
func (s *Service) ComputeCohorts(ctx context.Context, d Day) (int64, error) {
	n, err := s.q(ctx).UpsertCohorts(ctx, rollupsdb.UpsertCohortsParams{
		ComputedAt: s.clock.Now(), FromWeek: d.Monday().Add(-7 * (cohortWeeks - 1)).pg(), OnDate: d.pg(),
	})
	if err != nil {
		return 0, fmt.Errorf("rollups: cohorts: %w", err)
	}
	return n, nil
}

// ComputeItemPairs replaces every store's item pairs with the window of
// insight_pair_window_days whole IST days ending the day before d (05 §3,
// rollup_item_pairs; W3-03 reads them).
func (s *Service) ComputeItemPairs(ctx context.Context, d Day) (int64, error) {
	days, err := s.settings.Int(ctx, settings.InsightPairWindowDays)
	if err != nil {
		return 0, err
	}
	minBills, err := s.settings.Int(ctx, settings.InsightPairMinBills)
	if err != nil {
		return 0, err
	}
	var n int64
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if _, err := q.DeleteItemPairs(ctx); err != nil {
			return err
		}
		var err error
		n, err = q.InsertItemPairs(ctx, rollupsdb.InsertItemPairsParams{
			ComputedAt: s.clock.Now(), WindowDays: int32(days), //nolint:gosec // a settings value in days
			WindowStart: d.Add(-int(days)).Start(), WindowEnd: d.Start(),
			MinBills: int32(minBills), //nolint:gosec // a settings count
		})
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("rollups: item pairs: %w", err)
	}
	return n, nil
}

// ComputeBanners recomputes banner_daily for day d: impressions and taps from
// the banner events, attributed orders by D-77's last tap (05 §3).
func (s *Service) ComputeBanners(ctx context.Context, d Day) (int64, error) {
	q := s.q(ctx)
	rows, err := q.BannerEventCounts(ctx, rollupsdb.BannerEventCountsParams{DayStart: d.Start(), DayEnd: d.End()})
	if err != nil {
		return 0, fmt.Errorf("rollups: banner events %s: %w", d, err)
	}
	p := rollupsdb.UpsertBannerDaysParams{OnDate: d.pg(), DayStart: d.Start(), DayEnd: d.End(),
		BannerIds: []pgtype.UUID{}, Impressions: []int64{}, Clicks: []int64{}}
	skipped := 0
	for _, r := range rows {
		id, err := ids.Parse(r.Banner)
		if err != nil {
			skipped++ // a client sent a banner_id that is not a ULID; the event catalogue says ULID
			continue
		}
		p.BannerIds = append(p.BannerIds, ids.UUID(id))
		p.Impressions = append(p.Impressions, r.Impressions)
		p.Clicks = append(p.Clicks, r.Clicks)
	}
	if skipped > 0 {
		telemetry.Logger(ctx).WarnContext(ctx, "rollups: banner events with a bad banner_id", "day", d.String(), "groups", skipped)
	}
	n, err := q.UpsertBannerDays(ctx, p)
	if err != nil {
		return 0, fmt.Errorf("rollups: banner days %s: %w", d, err)
	}
	return n, nil
}

// Today is the IST day the clock is in.
func (s *Service) Today() Day { return DayOf(s.clock.Now()) }

// BackfillResult counts a backfill.
type BackfillResult struct {
	Days, Stores, Demand, Banners, Cohorts, Pairs int64
}

// Backfill recomputes every daily rollup for the days from..to (inclusive),
// then the cohorts and item pairs as of to's next day (todayz rollup).
// banner_taps keeps each customer's last tap per banner only, so an old day's
// attributed orders can come out lower than the live job counted.
func (s *Service) Backfill(ctx context.Context, from, to Day) (BackfillResult, error) {
	var r BackfillResult
	if to.Before(from) {
		return r, fmt.Errorf("rollups: --to %s is before --from %s", to, from)
	}
	for d := from; !to.Before(d); d = d.Add(1) {
		res, err := s.ComputeDay(ctx, d)
		if err != nil {
			return r, err
		}
		n, err := s.ComputeDemand(ctx, d)
		if err != nil {
			return r, err
		}
		b, err := s.ComputeBanners(ctx, d)
		if err != nil {
			return r, err
		}
		r.Days++
		r.Stores += res.Stores
		r.Demand += n
		r.Banners += b
	}
	var err error
	if r.Cohorts, err = s.ComputeCohorts(ctx, to); err != nil {
		return r, err
	}
	if r.Pairs, err = s.ComputeItemPairs(ctx, to.Add(1)); err != nil {
		return r, err
	}
	return r, nil
}

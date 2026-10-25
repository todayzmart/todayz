package rollups

import (
	"context"
	"fmt"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	api "github.com/todayz/backend/gen/api/admin-pulse"
	"github.com/todayz/backend/gen/sqlc/rollupsdb"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

const (
	// kFloor is the privacy floor (01 §16, fixed): a demand cell or a
	// zero-result query appears only with at least this many searches.
	kFloor = 10 //settings:ok 01 §16 fixes k ≥ 10
	// strikeWindow and notAcceptingWindow are the fixed windows of the
	// "stores needing attention" list (05 §3, the Store not accepting alert).
	strikeWindow       = 30 * 24 * time.Hour
	notAcceptingWindow = 2 * time.Hour
	bpScale            = 10000 //settings:ok basis points per whole (100 %), not a setting
)

// rangeDays is how many IST days a Pulse range covers, today included.
func rangeDays(r api.AdminPulseRange) (int, error) {
	switch r {
	case api.Today:
		return 1, nil
	case api.N7d:
		return 7, nil
	case api.N30d:
		return 30, nil //settings:ok the 30d Pulse range is a contract enum
	case api.N90d:
		return 90, nil //settings:ok the 90d Pulse range is a contract enum
	}
	return 0, fmt.Errorf("rollups: bad range %q", r)
}

// period is the days a range covers and the same-length period before it.
type period struct {
	rng            api.AdminPulseRange
	from, to       Day
	prevFrom, prev Day // prev is the previous period's last day
}

func (s *Service) period(r *api.AdminPulseRange) (period, error) {
	rng := api.N7d
	if r != nil {
		rng = *r
	}
	n, err := rangeDays(rng)
	if err != nil {
		return period{}, err
	}
	to := s.Today()
	from := to.Add(1 - n)
	return period{rng: rng, from: from, to: to, prevFrom: from.Add(-n), prev: from.Add(-1)}, nil
}

func apiDate(d Day) openapi_types.Date { return openapi_types.Date{Time: time.Time(d)} }

// bp is num/den in basis points, nil when den is 0.
func bp(num, den int64) *int {
	if den <= 0 {
		return nil
	}
	v := int(num * bpScale / den)
	return &v
}

// sums adds up the platform rows of one period.
type sums struct {
	gmv, pos                                         int64
	placed, accepted, completed                      int64
	searches, zero, storeViews                       int64
	newStores, newCustomers                          int64
	credIssued, credSpent, credExpired               int64
	d1Base, d1Back, d7Base, d7Back, d30Base, d30Back int64
	liveStores                                       int64 // the period's last day
	days                                             int
}

func sum(rows []rollupsdb.RollupDailyPlatform, from, to Day) sums {
	var s sums
	for _, r := range rows {
		d := dayFromPG(r.Date)
		if d.Before(from) || to.Before(d) {
			continue
		}
		s.days++
		s.gmv += r.GmvPaise
		s.pos += r.PosGmvPaise
		s.placed += int64(r.OrdersPlaced)
		s.accepted += int64(r.OrdersAccepted)
		s.completed += int64(r.OrdersCompleted)
		s.searches += int64(r.Searches)
		s.zero += int64(r.SearchesZero)
		s.storeViews += int64(r.StoreViews)
		s.newStores += int64(r.NewStores)
		s.newCustomers += int64(r.NewCustomers)
		s.credIssued += r.CreditsIssued
		s.credSpent += r.CreditsSpent
		s.credExpired += r.CreditsExpired
		s.d1Base += int64(r.RetD1Base)
		s.d1Back += int64(r.RetD1Back)
		s.d7Base += int64(r.RetD7Base)
		s.d7Back += int64(r.RetD7Back)
		s.d30Base += int64(r.RetD30Base)
		s.d30Back += int64(r.RetD30Back)
		s.liveStores = int64(r.LiveStores) // rows are in date order
	}
	return s
}

// kpi pairs a value with the previous period's; previous is null when the
// previous period has no rollup rows at all.
func kpi(v, p int64, prevRows bool) api.AdminPulseKpi {
	k := api.AdminPulseKpi{Value: int(v)}
	if prevRows {
		pv := int(p)
		k.Previous = &pv
	}
	return k
}

func rateKpi(v, p *int) api.AdminPulseRateKpi { return api.AdminPulseRateKpi{Value: v, Previous: p} }

// Pulse answers GET /v1/admin/pulse (A-02).
func (s *Service) Pulse(ctx context.Context, r *api.AdminPulseRange) (api.AdminPulse, error) {
	p, err := s.period(r)
	if err != nil {
		return api.AdminPulse{}, err
	}
	now := s.clock.Now()
	q := s.q(ctx)
	rows, err := q.PlatformDays(ctx, rollupsdb.PlatformDaysParams{FromDay: p.prevFrom.pg(), ToDay: p.to.pg()})
	if err != nil {
		return api.AdminPulse{}, fmt.Errorf("rollups: pulse days: %w", err)
	}
	cur, prev := sum(rows, p.from, p.to), sum(rows, p.prevFrom, p.prev)
	hasPrev := prev.days > 0

	distinct := func(f func(context.Context, time.Time, time.Time) (int32, error)) (int64, int64, error) {
		c, err := f(ctx, p.from.Start(), p.to.End())
		if err != nil {
			return 0, 0, err
		}
		pv, err := f(ctx, p.prevFrom.Start(), p.prev.End())
		return int64(c), int64(pv), err
	}
	active, activePrev, err := distinct(func(ctx context.Context, a, b time.Time) (int32, error) {
		return q.ActiveCustomersBetween(ctx, rollupsdb.ActiveCustomersBetweenParams{StartAt: a, EndAt: b})
	})
	if err != nil {
		return api.AdminPulse{}, fmt.Errorf("rollups: active customers: %w", err)
	}
	trans, transPrev, err := distinct(func(ctx context.Context, a, b time.Time) (int32, error) {
		return q.TransactingCustomersBetween(ctx, rollupsdb.TransactingCustomersBetweenParams{StartAt: a, EndAt: b})
	})
	if err != nil {
		return api.AdminPulse{}, fmt.Errorf("rollups: transacting customers: %w", err)
	}
	stores, err := q.ActiveStoresBetween(ctx, rollupsdb.ActiveStoresBetweenParams{FromDay: p.from.pg(), ToDay: p.to.pg()})
	if err != nil {
		return api.AdminPulse{}, fmt.Errorf("rollups: active stores: %w", err)
	}
	storesPrev, err := q.ActiveStoresBetween(ctx, rollupsdb.ActiveStoresBetweenParams{FromDay: p.prevFrom.pg(), ToDay: p.prev.pg()})
	if err != nil {
		return api.AdminPulse{}, fmt.Errorf("rollups: active stores: %w", err)
	}

	out := api.AdminPulse{
		Range: p.rng, From: apiDate(p.from), To: apiDate(p.to), ServerNow: now,
		Kpis: api.AdminPulseKpis{
			GmvPaise:             kpi(cur.gmv+cur.pos, prev.gmv+prev.pos, hasPrev),
			OnlineGmvPaise:       kpi(cur.gmv, prev.gmv, hasPrev),
			PosGmvPaise:          kpi(cur.pos, prev.pos, hasPrev),
			Orders:               kpi(cur.placed, prev.placed, hasPrev),
			CompletedOrders:      kpi(cur.completed, prev.completed, hasPrev),
			SuccessRateBp:        rateKpi(bp(cur.completed, cur.placed), bp(prev.completed, prev.placed)),
			ActiveCustomers:      kpi(active, activePrev, hasPrev),
			TransactingCustomers: kpi(trans, transPrev, hasPrev),
			LiveStores:           kpi(cur.liveStores, prev.liveStores, hasPrev),
			ActiveStores:         kpi(int64(stores), int64(storesPrev), hasPrev),
			NewStores:            kpi(cur.newStores, prev.newStores, hasPrev),
			Searches:             kpi(cur.searches, prev.searches, hasPrev),
			SearchSuccessRateBp:  rateKpi(bp(cur.searches-cur.zero, cur.searches), bp(prev.searches-prev.zero, prev.searches)),
			D7RetentionBp:        rateKpi(bp(cur.d7Back, cur.d7Base), bp(prev.d7Back, prev.d7Base)),
		},
		Funnel: api.AdminPulseFunnel{
			Searches: int(cur.searches), SearchesWithResults: int(cur.searches - cur.zero),
			StoreViews: int(cur.storeViews), OrdersPlaced: int(cur.placed),
			OrdersAccepted: int(cur.accepted), OrdersCompleted: int(cur.completed),
		},
		Credits: api.AdminPulseCredits{Issued: int(cur.credIssued), Spent: int(cur.credSpent), Expired: int(cur.credExpired)},
		Trend:   []api.AdminPulseDay{},
		Leaks:   []api.AdminPulseLeak{},
		Retention: api.AdminPulseRetention{
			D1Bp: bp(cur.d1Back, cur.d1Base), D7Bp: bp(cur.d7Back, cur.d7Base), D30Bp: bp(cur.d30Back, cur.d30Base),
			Cohorts: []api.AdminPulseCohort{},
		},
		ZeroResultSearches:     []api.AdminPulseZeroResult{},
		StoresNeedingAttention: []api.AdminPulseAttention{},
	}
	for _, r := range rows {
		d := dayFromPG(r.Date)
		if d.Before(p.from) {
			continue
		}
		out.Trend = append(out.Trend, api.AdminPulseDay{
			Date: apiDate(d), GmvPaise: int(r.GmvPaise), PosGmvPaise: int(r.PosGmvPaise),
			Orders: int(r.OrdersPlaced), CompletedOrders: int(r.OrdersCompleted),
			ActiveCustomers: int(r.Dau), NewCustomers: int(r.NewCustomers), LiveStores: int(r.LiveStores),
			Searches: int(r.Searches), ZeroResultSearches: int(r.SearchesZero),
		})
		if d == p.to {
			at := r.ComputedAt
			out.AsOf = &at // today's row: when rollup_today last ran
		}
	}

	leaks, err := q.OrderLeaks(ctx, rollupsdb.OrderLeaksParams{StartAt: p.from.Start(), EndAt: p.to.End()})
	if err != nil {
		return api.AdminPulse{}, fmt.Errorf("rollups: leaks: %w", err)
	}
	for _, l := range leaks {
		out.Leaks = append(out.Leaks, api.AdminPulseLeak{Status: api.AdminPulseLeakStatus(l.Status), Reason: l.StatusReason, Count: int(l.N)})
	}

	if out.Retention.Cohorts, err = s.cohorts(ctx, q, p.to); err != nil {
		return api.AdminPulse{}, err
	}

	zr, err := q.ZeroResultQueries(ctx, rollupsdb.ZeroResultQueriesParams{FromDay: p.from.pg(), ToDay: p.to.pg(), K: kFloor})
	if err != nil {
		return api.AdminPulse{}, fmt.Errorf("rollups: zero-result queries: %w", err)
	}
	for _, z := range zr {
		out.ZeroResultSearches = append(out.ZeroResultSearches, api.AdminPulseZeroResult{QueryNorm: z.QueryNorm, Searches: int(z.N)})
	}

	if out.StoresNeedingAttention, err = s.attention(ctx, q, p, now); err != nil {
		return api.AdminPulse{}, err
	}
	return out, nil
}

// cohorts is the customer retention grid: each cohort's share still ordering
// in week n, in basis points.
func (s *Service) cohorts(ctx context.Context, q *rollupsdb.Queries, to Day) ([]api.AdminPulseCohort, error) {
	rows, err := q.CustomerCohorts(ctx, to.pg())
	if err != nil {
		return nil, fmt.Errorf("rollups: cohorts: %w", err)
	}
	out := []api.AdminPulseCohort{}
	for _, r := range rows {
		wk := apiDate(dayFromPG(r.CohortWeek))
		if len(out) == 0 || out[len(out)-1].CohortWeek != wk {
			out = append(out, api.AdminPulseCohort{CohortWeek: wk, Size: int(r.CohortSize), WeeksBp: []int{}})
		}
		c := &out[len(out)-1]
		for len(c.WeeksBp) < int(r.WeekN) {
			c.WeeksBp = append(c.WeeksBp, 0) // a week with no row: nobody ordered
		}
		v := 0
		if b := bp(int64(r.OrderingCount), int64(r.CohortSize)); b != nil {
			v = *b
		}
		c.WeeksBp = append(c.WeeksBp, v)
	}
	return out, nil
}

func (s *Service) attention(ctx context.Context, q *rollupsdb.Queries, p period, now time.Time) ([]api.AdminPulseAttention, error) {
	threshold, err := s.settings.Int(ctx, settings.StoreStrikeRiskThreshold)
	if err != nil {
		return nil, err
	}
	stale, err := s.settings.Duration(ctx, settings.StockStaleAfterHours)
	if err != nil {
		return nil, err
	}
	rows, err := q.StoresNeedingAttention(ctx, rollupsdb.StoresNeedingAttentionParams{
		RecentFrom: now.Add(-notAcceptingWindow), StartAt: p.from.Start(), EndAt: p.to.End(),
		StrikesFrom: now.Add(-strikeWindow), StrikeThreshold: int32(threshold), // #nosec G115 -- a settings count
		StaleBefore: now.Add(-stale),
	})
	if err != nil {
		return nil, fmt.Errorf("rollups: stores needing attention: %w", err)
	}
	out := make([]api.AdminPulseAttention, 0, len(rows))
	for _, r := range rows {
		id, ok := ids.FromUUID(r.ID)
		if !ok {
			continue
		}
		a := api.AdminPulseAttention{
			StoreId: id.String(), Name: r.Name, City: r.City, Reason: api.AdminPulseAttentionReason(r.Reason),
			ExpiredUnaccepted: int(r.ExpiredUnaccepted), Strikes30d: int(r.Strikes30d),
		}
		if r.HasAcceptRate {
			v := int(r.AcceptRateBp)
			a.AcceptRateBp = &v
		}
		if t, ok := r.StockUpdatedAt.(time.Time); ok {
			a.StockUpdatedAt = &t
		}
		out = append(out, a)
	}
	return out, nil
}

// Map answers GET /v1/admin/pulse/map (A-02): every store pin and the demand
// cells with at least kFloor searches in the period.
func (s *Service) Map(ctx context.Context, r *api.AdminPulseRange) (api.AdminPulseMap, error) {
	p, err := s.period(r)
	if err != nil {
		return api.AdminPulseMap{}, err
	}
	q := s.q(ctx)
	out := api.AdminPulseMap{Range: p.rng, From: apiDate(p.from), To: apiDate(p.to), ServerNow: s.clock.Now(),
		Stores: []api.AdminPulseMapStore{}, Demand: []api.AdminPulseDemandCell{}}
	stores, err := q.MapStores(ctx, p.to.pg())
	if err != nil {
		return out, fmt.Errorf("rollups: map stores: %w", err)
	}
	for _, st := range stores {
		id, ok := ids.FromUUID(st.ID)
		if !ok {
			continue
		}
		out.Stores = append(out.Stores, api.AdminPulseMapStore{
			StoreId: id.String(), Name: st.Name, City: st.City, Status: api.AdminPulseMapStoreStatus(st.Status),
			Lat: float32(st.Lat), Lng: float32(st.Lng), Live: st.Live,
		})
	}
	cells, err := q.MapDemand(ctx, rollupsdb.MapDemandParams{FromDay: p.from.pg(), ToDay: p.to.pg(), K: kFloor})
	if err != nil {
		return out, fmt.Errorf("rollups: map demand: %w", err)
	}
	for _, c := range cells {
		out.Demand = append(out.Demand, api.AdminPulseDemandCell{
			Cell: fmt.Sprintf("%015x", uint64(c.GeoCell)), Searches: int(c.Searches), ZeroResultSearches: int(c.ZeroResults), // #nosec G115 -- an H3 index is a uint64 held in a bigint
		})
	}
	return out, nil
}

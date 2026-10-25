package detectors

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/todayz/backend/gen/sqlc/coachdb"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// All is every detector, in the 11 §2.1 order: the Demand insights kinds
// first, then the coach's own.
func All() []Detector {
	return []Detector{
		fromFacts("top_seller_low_days_of_cover", facts.SellingFast, Action, TopicStock, Daily, sellingFast),
		fromFacts("slow_mover_21d", facts.SittingOnShelf, Action, TopicOffers, Daily, sittingOnShelf),
		fromFacts("expiring_batch_value", facts.GoingToWaste, Action, TopicStock, Daily, goingToWaste),
		fromFacts("wanted_nearby", facts.WantedNearby, Tip, TopicDemand, Daily, wantedNearby),
		fromFacts("missed_sales", facts.MissedSales, Low, TopicDemand, Daily, missedSales),
		fromFacts("expired_on_shelf", facts.ExpiredOnShelf, Low, TopicStock, Daily, expiredOnShelf),
		fromFacts("selling_below_cost", facts.SellingBelowCost, Low, TopicSales, Daily, sellingBelowCost),
		fromFacts("stale_stock_share", facts.StockCheckDue, Action, TopicStock, Daily, stockCheckDue),
		fromFacts("bought_together", facts.BoughtTogether, Tip, TopicOffers, Weekly, boughtTogether),
		fromFacts("regulars_slipping", facts.RegularsSlipping, Tip, TopicSales, Weekly, regularsSlipping),
		{Name: "missed_orders_peak_hour", Jobs: []Job{Daily}, Run: missedOrdersPeakHour},
		{Name: "best_day_record", Jobs: []Job{Daily}, Run: bestDayRecord},
		{Name: "reviews_unanswered", Jobs: []Job{Daily}, Run: reviewsUnanswered},
		{Name: "weekly_summary", Jobs: []Job{Weekly}, Run: weeklySummary},
		{Name: "rating_drop", Jobs: []Job{Weekly}, Run: ratingDrop},
		{Name: "accept_time_vs_benchmark", Jobs: []Job{Weekly}, Run: acceptTimeVsBenchmark},
		{Name: "setup_incomplete", Jobs: []Job{Weekly}, Run: setupIncomplete},
		{Name: "expiry_penalty", Jobs: []Job{Realtime}, Run: expiryPenalty},
	}
}

// --- Demand insights kinds (D-99) -------------------------------------------

// rowFn turns one Demand insights row into the card's numbers and variant.
type rowFn func(r facts.Row, st facts.Settings) (nums map[string]any, variant string)

func fromFacts(name string, k facts.Kind, kind CardKind, topic string, j Job, fn rowFn) Detector {
	return Detector{Name: name, Jobs: []Job{j}, FromFacts: k, Run: func(_ context.Context, in *Input) ([]Fact, error) {
		if !in.FactsLoaded {
			return nil, fmt.Errorf("detectors: %s needs the Demand insights", name)
		}
		var out []Fact
		for _, r := range in.Facts.Rows {
			if r.Kind != k {
				continue
			}
			nums, variant := fn(r, in.FactsCfg)
			f := Fact{
				Detector: name, Subject: r.SubjectRef, Kind: kind, Topic: topic,
				Severity: SeverityOf(r.Severity), Stake: r.Stake, Numbers: nums, Variant: variant,
				Action: facts.Action{Type: r.Action.Type, Payload: r.Action.Payload},
			}
			if k == facts.WantedNearby || k == facts.MissedSales {
				// Counts of searches by other people: facts already applied the
				// k ≥ 10 floor; the coach checks it again (D-71).
				f.AboutOthers, f.Others = true, i64(r.Facts["searches"])
			}
			out = append(out, f)
		}
		return out, nil
	}}
}

func itemOf(f map[string]any) string { return ItemName(str(f["item"]), str(f["size"])) }

func sellingFast(r facts.Row, _ facts.Settings) (map[string]any, string) {
	return map[string]any{
		"item": itemOf(r.Facts), "days": r.Facts["daysOfStock"], "qty": r.Action.Payload["qty"],
		"sold": r.Facts["soldQty"], "periodDays": r.Facts["periodDays"],
	}, ""
}

func sittingOnShelf(r facts.Row, _ facts.Settings) (map[string]any, string) {
	n := map[string]any{"item": itemOf(r.Facts), "days": r.Facts["daysNoSale"], "onHand": r.Facts["onHand"]}
	if pct, ok := r.Action.Payload["pct"]; ok {
		n["pct"] = pct
		return n, "pct"
	}
	return n, ""
}

func goingToWaste(r facts.Row, _ facts.Settings) (map[string]any, string) {
	names, _ := r.Facts["names"].([]string)
	n := map[string]any{"count": r.Facts["expiringCount"], "withinDays": r.Facts["withinDays"]}
	if len(names) > 0 {
		n["item"] = ItemName(names[0], "")
	}
	if i64(r.Facts["expiringCount"]) > 1 {
		return n, "many"
	}
	return n, ""
}

func km(radiusM int64) string {
	if radiusM%1000 == 0 {
		return fmt.Sprintf("%d km", radiusM/1000)
	}
	return fmt.Sprintf("%.1f km", float64(radiusM)/1000)
}

func wantedNearby(r facts.Row, st facts.Settings) (map[string]any, string) {
	n := map[string]any{"item": itemOf(r.Facts), "searches": r.Facts["searches"], "km": km(st.RadiusM),
		"periodDays": r.Facts["periodDays"]}
	if b, _ := r.Facts["noShopNearby"].(bool); b {
		return n, "none_nearby"
	}
	return n, ""
}

func missedSales(r facts.Row, _ facts.Settings) (map[string]any, string) {
	return map[string]any{"item": itemOf(r.Facts), "searches": r.Facts["searches"], "zeroDays": r.Facts["zeroDays"]}, ""
}

func expiredOnShelf(r facts.Row, _ facts.Settings) (map[string]any, string) {
	return map[string]any{"item": itemOf(r.Facts), "qty": r.Facts["qty"], "daysAgo": r.Facts["expiredDaysAgo"]}, ""
}

func sellingBelowCost(r facts.Row, _ facts.Settings) (map[string]any, string) {
	n := map[string]any{"item": itemOf(r.Facts), "price": Rupees(i64(r.Facts["pricePaise"])),
		"cost": Rupees(i64(r.Facts["costPaise"]))}
	if b, _ := r.Facts["checkCost"].(bool); b {
		return n, "check_cost"
	}
	return n, ""
}

func stockCheckDue(r facts.Row, _ facts.Settings) (map[string]any, string) {
	return map[string]any{"count": r.Facts["staleCount"], "days": i64(r.Facts["staleAfterHours"]) / 24}, ""
}

func boughtTogether(r facts.Row, _ facts.Settings) (map[string]any, string) {
	return map[string]any{
		"itemA": ItemName(str(r.Facts["itemA"]), str(r.Facts["sizeA"])),
		"itemB": ItemName(str(r.Facts["itemB"]), str(r.Facts["sizeB"])), "oneIn": r.Facts["oneIn"],
	}, ""
}

func regularsSlipping(r facts.Row, _ facts.Settings) (map[string]any, string) {
	return map[string]any{"count": r.Facts["count"], "lapseDays": r.Facts["lapseDays"]}, ""
}

// --- The coach's own detectors ----------------------------------------------

// missedOrdersPeakHour: orders that expired yesterday because nobody accepted
// within order_accept_timeout_min, with the IST hours most of them came in.
// It offers auto-accept only while it is off.
// TODO(lead): no minimum count in 01; conservative reading, one expiry fires.
func missedOrdersPeakHour(ctx context.Context, in *Input) ([]Fact, error) {
	y := in.Today.Add(-1)
	rows, err := in.Q.UnacceptedExpiriesByHour(ctx, coachdb.UnacceptedExpiriesByHourParams{
		StoreID: ids.UUID(in.Store.ID), FromAt: y.Start(), ToAt: y.End()})
	if err != nil {
		return nil, fmt.Errorf("detectors: unaccepted expiries: %w", err)
	}
	var total int64
	for _, r := range rows {
		total += r.N
	}
	if total == 0 {
		return nil, nil
	}
	mins, err := in.Settings.Int(ctx, settings.OrderAcceptTimeoutMin)
	if err != nil {
		return nil, fmt.Errorf("detectors: %s: %w", settings.OrderAcceptTimeoutMin, err)
	}
	// The busiest run of consecutive hours holding the expiries: start at the
	// hour with the most and widen while a neighbour also had one.
	by := map[int]int64{}
	peak := int(rows[0].Hour)
	for _, r := range rows {
		by[int(r.Hour)] = r.N
		if r.N > by[peak] {
			peak = int(r.Hour)
		}
	}
	from, to := peak, peak
	for by[from-1] > 0 {
		from--
	}
	for by[to+1] > 0 {
		to++
	}
	act := facts.Action{Type: ActNone, Payload: map[string]any{}}
	variant := "auto_on"
	if !in.Store.AutoAccept {
		act = facts.Action{Type: ActTurnOnAutoAccept, Payload: map[string]any{"setting": "auto_accept"}}
		variant = ""
	}
	return []Fact{{
		Detector: "missed_orders_peak_hour", Subject: facts.StoreSubject, Kind: Low, Topic: TopicOrders,
		Severity: 2, Stake: total, Variant: variant, Action: act,
		Numbers: map[string]any{"count": total, "minutes": mins, "window": HourRange(from, to)},
	}}, nil
}

// salesOn maps an IST day to its sales (paise) and bills.
func salesOn(ctx context.Context, in *Input, from, to facts.Day) (map[facts.Day]coachdb.DailySalesRow, error) {
	rows, err := in.Q.DailySales(ctx, coachdb.DailySalesParams{StoreID: ids.UUID(in.Store.ID), FromDay: from.PG(), ToDay: to.PG()})
	if err != nil {
		return nil, fmt.Errorf("detectors: daily sales: %w", err)
	}
	out := make(map[facts.Day]coachdb.DailySalesRow, len(rows))
	for _, r := range rows {
		out[facts.DayFromPG(r.Date)] = r
	}
	return out, nil
}

// pctAbove is how much a is above b, in whole percent.
func pctAbove(a, b int64) int64 {
	if b <= 0 {
		return 0
	}
	return int64(math.Round(float64(a-b) * 100 / float64(b)))
}

// bestDayRecord: yesterday's sales beat every one of the recordWeeks earlier
// same weekdays, with at least recordMinWeeks of them on record (a High card,
// naming the items that led).
func bestDayRecord(ctx context.Context, in *Input) ([]Fact, error) {
	y := in.Today.Add(-1)
	by, err := salesOn(ctx, in, y.Add(-7*recordWeeks), y)
	if err != nil {
		return nil, err
	}
	me, ok := by[y]
	if !ok || me.SalesPaise <= 0 {
		return nil, nil
	}
	var n, sum int64
	for w := 1; w <= recordWeeks; w++ {
		r, ok := by[y.Add(-7*w)]
		if !ok || r.SalesPaise <= 0 {
			continue
		}
		if r.SalesPaise >= me.SalesPaise {
			return nil, nil
		}
		n++
		sum += r.SalesPaise
	}
	if n < recordMinWeeks {
		return nil, nil
	}
	usual := sum / n
	top, err := in.Q.TopItemsOn(ctx, coachdb.TopItemsOnParams{StoreID: ids.UUID(in.Store.ID), FromAt: y.Start(), ToAt: y.End(), Lim: topLeaders})
	if err != nil {
		return nil, fmt.Errorf("detectors: top items: %w", err)
	}
	nums := map[string]any{
		"weekday": time.Time(y).Weekday().String(), "weeks": int64(recordWeeks), "sales": Rupees(me.SalesPaise),
		"pct": pctAbove(me.SalesPaise, usual),
	}
	variant := ""
	if len(top) > 0 {
		names := make([]string, len(top))
		for i, t := range top {
			names[i] = ItemName(t.Name, "")
		}
		nums["leaders"] = strings.Join(names, " · ")
		variant = "leaders"
	}
	return []Fact{{
		Detector: "best_day_record", Subject: y.String(), Kind: High, Topic: TopicSales, Severity: 3,
		Stake: me.SalesPaise, Numbers: nums, Variant: variant,
		Action: facts.Action{Type: ActNone, Payload: map[string]any{}},
	}}, nil
}

// reviewsUnanswered: visible reviews with text in the last insight_period_days
// that have no reply yet.
func reviewsUnanswered(ctx context.Context, in *Input) ([]Fact, error) {
	days, err := in.Settings.Int(ctx, settings.InsightPeriodDays)
	if err != nil {
		return nil, fmt.Errorf("detectors: %s: %w", settings.InsightPeriodDays, err)
	}
	days = max(days, 1)
	r, err := in.Q.UnansweredReviews(ctx, coachdb.UnansweredReviewsParams{
		StoreID: ids.UUID(in.Store.ID), FromAt: in.Today.Add(-int(days)).Start(), ToAt: in.Today.Start()})
	if err != nil {
		return nil, fmt.Errorf("detectors: unanswered reviews: %w", err)
	}
	if r.N == 0 {
		return nil, nil
	}
	sev := 1
	if r.Lowest > 0 && r.Lowest <= 2 { //settings:ok a 1- or 2-star review is a low review (01 §11)
		sev = 2
	}
	return []Fact{{
		Detector: "reviews_unanswered", Subject: facts.StoreSubject, Kind: Action, Topic: TopicReviews,
		Severity: sev, Stake: r.N, Numbers: map[string]any{"count": r.N, "days": days},
		Action: facts.Action{Type: ActReplyReviews, Payload: map[string]any{"filter": "unanswered"}},
	}}, nil
}

// weeklySummary: last week (Mon–Sun) against the average of the summaryWeeks
// before it. Fires only with all of them on record.
func weeklySummary(ctx context.Context, in *Input) ([]Fact, error) {
	// The week that ended yesterday when the job runs on Monday; any other day
	// it is the last whole week before today.
	end := in.Today
	for time.Time(end).Weekday() != time.Monday {
		end = end.Add(-1)
	}
	start := end.Add(-7 * (summaryWeeks + 1))
	by, err := salesOn(ctx, in, start, end.Add(-1))
	if err != nil {
		return nil, err
	}
	weeks := make([]int64, summaryWeeks+1)
	var bills int64
	have := make([]bool, summaryWeeks+1)
	for d, r := range by {
		w := summaryWeeks - d.Sub(start)/7
		if w < 0 || w > summaryWeeks {
			continue
		}
		weeks[w] += r.SalesPaise
		if r.SalesPaise > 0 {
			have[w] = true
		}
		if w == 0 {
			bills += r.Bills
		}
	}
	for _, h := range have {
		if !h {
			return nil, nil
		}
	}
	var sum int64
	for _, s := range weeks[1:] {
		sum += s
	}
	usual := sum / summaryWeeks
	pct := pctAbove(weeks[0], usual)
	variant := "up"
	if pct < 0 {
		variant, pct = "down", -pct
	} else if pct == 0 {
		variant = "flat"
	}
	return []Fact{{
		Detector: "weekly_summary", Subject: end.String(), Kind: Tip, Topic: TopicSales, Severity: 1,
		Stake: weeks[0], Variant: variant,
		Numbers: map[string]any{"sales": Rupees(weeks[0]), "bills": bills, "pct": pct, "weeks": int64(summaryWeeks)},
		Action:  facts.Action{Type: ActNone, Payload: map[string]any{}},
	}}, nil
}

func stars(sum, n int64) int64 { return int64(math.Round(float64(sum) * 10 / float64(n))) }

func tenths(t int64) string { return fmt.Sprintf("%d.%d", t/10, t%10) }

// ratingDrop: the average stars of the last ratingRecentDays fell at least
// ratingDropTenths below the ratingEarlierDays before, with
// insight_min_events_for_percent reviews in each window.
func ratingDrop(ctx context.Context, in *Input) ([]Fact, error) {
	minN, err := in.Settings.Int(ctx, settings.InsightMinEventsForPercent)
	if err != nil {
		return nil, fmt.Errorf("detectors: %s: %w", settings.InsightMinEventsForPercent, err)
	}
	to := in.Today.Start()
	recent := in.Today.Add(-ratingRecentDays).Start()
	r, err := in.Q.RatingWindows(ctx, coachdb.RatingWindowsParams{
		StoreID: ids.UUID(in.Store.ID), RecentFrom: recent, EarlierFrom: in.Today.Add(-ratingRecentDays - ratingEarlierDays).Start(), ToAt: to})
	if err != nil {
		return nil, fmt.Errorf("detectors: rating windows: %w", err)
	}
	if r.RecentN < max(minN, 1) || r.EarlierN < max(minN, 1) {
		return nil, nil
	}
	now, before := stars(r.RecentStars, r.RecentN), stars(r.EarlierStars, r.EarlierN)
	if before-now < ratingDropTenths {
		return nil, nil
	}
	return []Fact{{
		Detector: "rating_drop", Subject: facts.StoreSubject, Kind: Low, Topic: TopicReviews, Severity: 2,
		Stake: before - now, Numbers: map[string]any{"now": tenths(now), "before": tenths(before), "count": r.RecentN, "days": int64(ratingRecentDays)},
		Action: facts.Action{Type: ActReplyReviews, Payload: map[string]any{"filter": "low"}},
	}}, nil
}

// acceptTimeVsBenchmark: yesterday's median time to accept against the median
// of nearby shops (insight_wanted_nearby_radius_m), only with at least KFloor
// other businesses behind the benchmark (D-71).
func acceptTimeVsBenchmark(ctx context.Context, in *Input) ([]Fact, error) {
	y := in.Today.Add(-1)
	by, err := salesOn(ctx, in, y, y)
	if err != nil {
		return nil, err
	}
	me, ok := by[y]
	if !ok || me.P50AcceptSeconds == nil {
		return nil, nil
	}
	radius, err := in.Settings.Int(ctx, settings.InsightWantedNearbyRadiusM)
	if err != nil {
		return nil, fmt.Errorf("detectors: %s: %w", settings.InsightWantedNearbyRadiusM, err)
	}
	b, err := in.Q.NearbyAcceptMedian(ctx, coachdb.NearbyAcceptMedianParams{StoreID: ids.UUID(in.Store.ID), RadiusM: float64(radius), Day: y.PG()})
	if err != nil {
		return nil, fmt.Errorf("detectors: nearby accept median: %w", err)
	}
	if b.Businesses < KFloor || b.MedianSeconds <= 0 {
		return nil, nil
	}
	mine := int64(*me.P50AcceptSeconds)
	// TODO(lead): no 01 key for "slow"; conservative reading, twice the median.
	if mine <= slowFactor*b.MedianSeconds {
		return nil, nil
	}
	minutes := func(s int64) int64 { return max((s+30)/60, 1) }
	if minutes(mine) <= minutes(b.MedianSeconds) {
		return nil, nil
	}
	act := facts.Action{Type: ActNone, Payload: map[string]any{}}
	if !in.Store.AutoAccept {
		act = facts.Action{Type: ActTurnOnAutoAccept, Payload: map[string]any{"setting": "auto_accept"}}
	}
	return []Fact{{
		Detector: "accept_time_vs_benchmark", Subject: facts.StoreSubject, Kind: Low, Topic: TopicOrders, Severity: 1,
		Stake: mine - b.MedianSeconds, AboutOthers: true, Others: b.Businesses, Action: act,
		Numbers: map[string]any{"mine": minutes(mine), "nearby": minutes(b.MedianSeconds)},
	}}, nil
}

// setupIncomplete: the shop has no opening hours, no map pin or no items.
func setupIncomplete(_ context.Context, in *Input) ([]Fact, error) {
	var variant string
	switch {
	case in.Store.Items == 0:
		variant = "items"
	case !in.Store.HasHours:
		variant = "hours"
	case !in.Store.HasLocation:
		variant = "location"
	default:
		return nil, nil
	}
	act := facts.Action{Type: ActNone, Payload: map[string]any{"step": variant}}
	if variant == "items" {
		act.Type = facts.ActAddProduct
	}
	return []Fact{{
		Detector: "setup_incomplete", Subject: variant, Kind: Tip, Topic: TopicSetup, Severity: 1,
		Variant: variant, Numbers: map[string]any{}, Action: act,
	}}, nil
}

// expiryPenalty: the 30-day unaccepted-expiry rate the ranking uses is above
// shop_expiry_penalty_threshold_bp, so the shop shows lower in search (D-46).
func expiryPenalty(ctx context.Context, in *Input) ([]Fact, error) {
	if !in.Store.UnacceptedExpiryRate30d.Valid {
		return nil, nil
	}
	f, err := in.Store.UnacceptedExpiryRate30d.Float64Value()
	if err != nil || !f.Valid {
		return nil, nil
	}
	rateBp := int64(math.Round(f.Float64 * bpScale))
	threshold, err := in.Settings.BP(ctx, settings.ShopExpiryPenaltyThresholdBp)
	if err != nil {
		return nil, fmt.Errorf("detectors: %s: %w", settings.ShopExpiryPenaltyThresholdBp, err)
	}
	if rateBp <= int64(threshold) {
		return nil, nil
	}
	// The month's counts behind the rate, for the sentence.
	o, err := in.Q.OrderOutcomes(ctx, coachdb.OrderOutcomesParams{StoreID: ids.UUID(in.Store.ID),
		FromAt: in.Today.Add(-penaltyDays).Start(), ToAt: in.Now})
	if err != nil {
		return nil, fmt.Errorf("detectors: order outcomes: %w", err)
	}
	nums := map[string]any{"pct": (rateBp + 50) / 100, "threshold": (int64(threshold) + 50) / 100, "days": int64(penaltyDays)}
	variant := ""
	if o.Orders > 0 {
		nums["expired"], nums["orders"] = o.Unaccepted, o.Orders
		variant = "counts"
	}
	return []Fact{{
		Detector: "expiry_penalty", Subject: facts.StoreSubject, Kind: High, Topic: TopicReliability, Severity: 3,
		Stake: rateBp, Variant: variant, Numbers: nums,
		Action: facts.Action{Type: ActTurnOnAutoAccept, Payload: map[string]any{"setting": "auto_accept", "also": "order_sound"}},
	}}, nil
}

// SortByStake orders facts of equal score: larger stake first, then name and
// subject, so a run is deterministic.
func SortByStake(fs []Fact) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Stake != fs[j].Stake {
			return fs[i].Stake > fs[j].Stake
		}
		if fs[i].Detector != fs[j].Detector {
			return fs[i].Detector < fs[j].Detector
		}
		return fs[i].Subject < fs[j].Subject
	})
}

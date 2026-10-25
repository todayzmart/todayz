package facts

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/insightsdb"
	"github.com/todayz/backend/internal/platform/ids"
)

// calc gathers the rows and needs-data lines of one Compute.
type calc struct {
	in    *input
	st    Settings
	quiet Quiet
	rows  []Row
	needs []Need
}

func (c *calc) add(r Row) {
	if len(r.Quiets) == 0 {
		r.Quiets = []string{r.SubjectRef}
	}
	c.rows = append(c.rows, r)
}

func (c *calc) need(k Kind, params map[string]any) {
	c.needs = append(c.needs, Need{Kind: k, Key: "insights.needs." + string(k), Params: params})
}

func itemFacts(it *item) map[string]any {
	return map[string]any{"item": it.name, "size": it.size, "storeItemId": it.id.String()}
}

// sellingFast: at least insight_min_units_selling_fast units sold in the
// period and available ÷ pace below insight_cover_days.
func (c *calc) sellingFast(context.Context) error {
	in, st := c.in, c.st
	evidence := false
	for _, it := range in.items {
		sold := in.sold[it.id]
		if sold < st.MinUnitsSellingFast || sold <= 0 {
			continue
		}
		evidence = true
		avail := it.available()
		// days of stock = avail ÷ (sold ÷ period); fires below cover days.
		if avail*st.PeriodDays >= st.CoverDays*sold {
			continue
		}
		sub := it.id.String()
		if c.quiet.Has(SellingFast, sub) {
			continue
		}
		sev := Medium
		if avail*st.PeriodDays <= highRunOutDays*sold {
			sev = High
		}
		days := float64(max(avail, 0)) * float64(st.PeriodDays) / float64(sold)
		runOut := DayOf(in.now.Add(time.Duration(days * float64(24*time.Hour))))
		f := itemFacts(it)
		f["soldQty"] = sold
		f["periodDays"] = st.PeriodDays
		f["available"] = avail
		f["daysOfStock"] = math.Round(days*10) / 10
		f["runOutOn"] = runOut.String()
		f["runOutInDays"] = runOut.Sub(in.today)
		qty := restockQty(sold, st.CoverDays, st.PeriodDays, avail)
		c.add(Row{
			Kind: SellingFast, SubjectRef: sub, Severity: sev, Stake: sold, Facts: f,
			Action: Action{Type: ActRestock, Payload: map[string]any{"storeItemId": sub, "qty": qty}},
		})
	}
	if !evidence {
		c.need(SellingFast, map[string]any{"minUnits": st.MinUnitsSellingFast})
	}
	return nil
}

// offerPctFor is the suggested % off: offerPct, cut so the price stays at or
// above the latest cost, never above maxOfferPct. 0 means no safe %.
func offerPctFor(price int64, cost *int64) int64 {
	safe := int64(maxOfferPct)
	if cost != nil && price > 0 {
		safe = min(safe, (price-*cost)*100/price)
	}
	if safe <= 0 {
		return 0
	}
	return min(int64(offerPct), safe)
}

// sittingOnShelf: listed, in stock, listed at least insight_min_days_listed
// days, no sale in insight_no_sale_days.
func (c *calc) sittingOnShelf(context.Context) error {
	in, st := c.in, c.st
	evidence := false
	for _, it := range in.items {
		if !it.listed || it.onHand <= 0 {
			continue
		}
		if int64(in.today.Sub(DayOf(it.createdAt))) < st.MinDaysListed {
			continue
		}
		evidence = true
		ref := it.createdAt
		if t, ok := in.last[it.id]; ok {
			ref = t
		}
		days := int64(in.today.Sub(DayOf(ref)))
		if days < st.NoSaleDays {
			continue
		}
		sub := it.id.String()
		if c.quiet.Has(SittingOnShelf, sub) {
			continue
		}
		f := itemFacts(it)
		f["onHand"] = it.onHand
		f["daysNoSale"] = days
		unit := it.price
		var cost *int64
		if b := in.latestCosted(it.id); b != nil {
			cost = b.cost
			unit = *b.cost
			f["valueAtCostPaise"] = it.onHand * *b.cost
		}
		payload := map[string]any{"storeItemId": sub}
		if pct := offerPctFor(it.price, cost); pct > 0 {
			payload["pct"] = pct
		}
		c.add(Row{
			Kind: SittingOnShelf, SubjectRef: sub, Severity: Low, Stake: it.onHand * unit, Facts: f,
			Action: Action{Type: ActCreateOffer, Payload: payload},
		})
	}
	if !evidence {
		c.need(SittingOnShelf, map[string]any{"minDaysListed": st.MinDaysListed})
	}
	return nil
}

// goingToWaste: one row per store with every batch (remaining above 0) that
// expires within insight_expiring_within_days, today included, not yet
// expired. Quieted batches are left out.
func (c *calc) goingToWaste(context.Context) error {
	in, st := c.in, c.st
	last := in.today.Add(int(st.ExpiringWithinDays) - 1)
	var bs []batch
	for _, b := range in.batches {
		if b.expires == nil || b.expires.Before(in.today) || last.Before(*b.expires) {
			continue
		}
		if c.quiet.Has(GoingToWaste, b.id.String()) {
			continue
		}
		bs = append(bs, b)
	}
	if len(bs) == 0 {
		return nil
	}
	var names []string
	seen := map[ids.ID]bool{}
	batchIDs := make([]string, 0, len(bs))
	var stake int64
	for _, b := range bs {
		batchIDs = append(batchIDs, b.id.String())
		it := in.byID[b.item]
		if it == nil {
			continue
		}
		unit := it.price
		if b.cost != nil {
			unit = *b.cost
		}
		stake += b.qty * unit
		if !seen[b.item] {
			seen[b.item] = true
			names = append(names, it.name)
		}
	}
	shown := names
	if len(shown) > 3 { //settings:ok 01 §12 shows up to 3 names
		shown = shown[:3]
	}
	c.add(Row{
		Kind: GoingToWaste, SubjectRef: batchIDs[0], Severity: High, Stake: stake,
		Facts: map[string]any{
			"expiringCount": len(names), "withinDays": st.ExpiringWithinDays, "names": shown,
			"moreCount": len(names) - len(shown), "earliestOn": bs[0].expires.String(),
		},
		Action: Action{Type: ActFlashOffer, Payload: map[string]any{"batchIds": batchIDs}},
		Quiets: batchIDs,
	})
	return nil
}

// wantedNearby: searches (and unanswered asks) within the radius in the period
// for a product the store has no item for, or a query no product matched,
// at least insight_min_searches (never below 10).
func (c *calc) wantedNearby(ctx context.Context) error {
	in, st := c.in, c.st
	type cand struct {
		key     string
		product ids.ID
		hasProd bool
		query   string
		n       int64
	}
	byKey := map[string]*cand{}
	for _, d := range in.demand {
		var key string
		if d.hasProd {
			if in.listed[d.product] {
				continue
			}
			key = d.product.String()
		} else {
			if d.query == "" {
				continue
			}
			key = "q:" + d.query
		}
		x := byKey[key]
		if x == nil {
			x = &cand{key: key, product: d.product, hasProd: d.hasProd, query: d.query}
			byKey[key] = x
		}
		x.n += d.n
	}
	var fire []*cand
	fired := false
	for _, x := range byKey {
		if x.n < st.MinSearches {
			continue
		}
		fired = true
		if c.quiet.Has(WantedNearby, x.key) {
			continue
		}
		fire = append(fire, x)
	}
	if !fired {
		c.need(WantedNearby, map[string]any{"minSearches": st.MinSearches, "radiusM": st.RadiusM})
		return nil
	}
	if len(fire) == 0 {
		return nil
	}
	var pids []pgtype.UUID
	for _, x := range fire {
		if x.hasProd {
			pids = append(pids, ids.UUID(x.product))
		}
	}
	names := map[ids.ID][2]string{}
	stocked := map[ids.ID]bool{}
	k := int32(0)
	if len(pids) > 0 {
		pn, err := in.q.ProductNames(ctx, pids)
		if err != nil {
			return fmt.Errorf("facts: product names: %w", err)
		}
		for _, r := range pn {
			id, _ := ids.FromUUID(r.ID)
			names[id] = [2]string{r.Name, r.Size}
		}
		if k, err = in.q.NearbyBusinesses(ctx, insightsdb.NearbyBusinessesParams{StoreID: in.store, RadiusM: float64(st.RadiusM)}); err != nil {
			return fmt.Errorf("facts: nearby businesses: %w", err)
		}
		if k >= kFloor {
			ss, err := in.q.NearbyStocked(ctx, insightsdb.NearbyStockedParams{StoreID: in.store, RadiusM: float64(st.RadiusM), ProductIds: pids})
			if err != nil {
				return fmt.Errorf("facts: nearby stocked: %w", err)
			}
			for _, u := range ss {
				id, _ := ids.FromUUID(u)
				stocked[id] = true
			}
		}
	}
	for _, x := range fire {
		f := map[string]any{"searches": x.n, "radiusM": st.RadiusM, "periodDays": st.PeriodDays}
		payload := map[string]any{}
		if x.hasProd {
			nm := names[x.product]
			f["item"], f["size"] = nm[0], nm[1]
			f["productId"] = x.product.String()
			f["noShopNearby"] = k >= kFloor && !stocked[x.product]
			payload["productId"] = x.product.String()
		} else {
			f["item"], f["size"] = x.query, ""
			f["query"] = x.query
			f["noShopNearby"] = false
			payload["query"] = x.query
		}
		c.add(Row{
			Kind: WantedNearby, SubjectRef: x.key, Severity: Low, Stake: x.n, Facts: f,
			Action: Action{Type: ActAddProduct, Payload: payload},
		})
	}
	return nil
}

// missedSales: a listed item at closing stock 0 or below on at least
// insight_missed_sales_min_zero_days days of the period, with at least
// insight_missed_sales_min_searches nearby searches matching its product on
// those days. Searches with no product never count.
func (c *calc) missedSales(context.Context) error {
	in, st := c.in, c.st
	byProd := map[ids.ID]map[Day]int64{}
	for _, d := range in.demand {
		if !d.hasProd {
			continue
		}
		if byProd[d.product] == nil {
			byProd[d.product] = map[Day]int64{}
		}
		byProd[d.product][d.day] += d.n
	}
	candidate, fired := false, false
	for _, it := range in.items {
		if !it.listed {
			continue
		}
		zero := in.closingZeroDays(it)
		if int64(len(zero)) < st.MissedMinZeroDays || len(zero) == 0 {
			continue
		}
		candidate = true
		var n int64
		for d := range zero {
			n += byProd[it.productID][d]
		}
		if n < st.MissedMinSearches {
			continue
		}
		fired = true
		sub := it.id.String()
		if c.quiet.Has(MissedSales, sub) {
			continue
		}
		sold, avail := in.sold[it.id], it.available()
		f := itemFacts(it)
		f["zeroDays"] = len(zero)
		f["searches"] = n
		f["soldQty"] = sold
		f["available"] = avail
		c.add(Row{
			Kind: MissedSales, SubjectRef: sub, Severity: Medium, Stake: n, Facts: f,
			Action: Action{Type: ActRestock, Payload: map[string]any{
				"storeItemId": sub, "qty": restockQty(sold, st.CoverDays, st.PeriodDays, avail),
			}},
		})
	}
	if candidate && !fired {
		c.need(MissedSales, map[string]any{"minSearches": st.MissedMinSearches})
	}
	return nil
}

// expiredOnShelf: one row per batch with remaining above 0 and expires_on
// before today. It cannot be dismissed.
func (c *calc) expiredOnShelf(context.Context) error {
	in := c.in
	// The item's expired batches, earliest expiry first, each at
	// min(remaining, available left).
	type pick struct {
		ids     []string
		batches []map[string]any
	}
	picks := map[ids.ID]*pick{}
	left := map[ids.ID]int64{}
	for _, it := range in.items {
		left[it.id] = max(it.available(), 0)
	}
	var expired []batch
	for _, b := range in.batches {
		if b.expires == nil || !b.expires.Before(in.today) {
			continue
		}
		expired = append(expired, b)
		p := picks[b.item]
		if p == nil {
			p = &pick{}
			picks[b.item] = p
		}
		q := min(b.qty, left[b.item])
		left[b.item] -= q
		p.ids = append(p.ids, b.id.String())
		p.batches = append(p.batches, map[string]any{"batchId": b.id.String(), "qty": q})
	}
	for _, b := range expired {
		it := in.byID[b.item]
		if it == nil {
			continue
		}
		f := itemFacts(it)
		f["qty"] = b.qty
		f["expiredOn"] = b.expires.String()
		f["expiredDaysAgo"] = in.today.Sub(*b.expires)
		p := picks[b.item]
		c.add(Row{
			Kind: ExpiredOnShelf, SubjectRef: b.id.String(), Severity: High, Stake: b.qty * it.price, Facts: f,
			Action: Action{Type: ActMarkExpired, Payload: map[string]any{
				"storeItemId": it.id.String(), "batchIds": p.ids, "batches": p.batches,
			}},
		})
	}
	return nil
}

// sellingBelowCost: the price is below the cost of the latest batch with
// remaining above 0 that has a cost. A cost at or above the MRP is probably a
// case price: that row asks to check the cost entry and ranks last.
func (c *calc) sellingBelowCost(context.Context) error {
	in := c.in
	for _, it := range in.items {
		b := in.latestCosted(it.id)
		if b == nil {
			continue
		}
		cost := *b.cost
		if it.price >= cost {
			continue
		}
		sub := it.id.String()
		if c.quiet.Has(SellingBelowCost, sub) {
			continue
		}
		f := itemFacts(it)
		f["pricePaise"] = it.price
		f["costPaise"] = cost
		if it.mrp > 0 && cost >= it.mrp {
			f["checkCost"] = true
			f["mrpPaise"] = it.mrp
			c.add(Row{
				Kind: SellingBelowCost, SubjectRef: sub, Severity: High, Stake: cost - it.price, Last: true, Facts: f,
				Action: Action{Type: ActCheckCost, Payload: map[string]any{"storeItemId": sub, "batchId": b.id.String()}},
			})
			continue
		}
		f["checkCost"] = false
		ref := ceilDiv(cost, 100) * 100 //settings:ok paise per rupee
		if it.mrp > 0 {
			ref = min(ref, it.mrp)
		}
		c.add(Row{
			Kind: SellingBelowCost, SubjectRef: sub, Severity: High, Stake: (cost - it.price) * max(it.onHand, 1), Facts: f,
			Action: Action{Type: ActEditPrice, Payload: map[string]any{"storeItemId": sub, "refPaise": ref}},
		})
	}
	return nil
}

// Stale is the customer-facing "Stock not confirmed recently" test (§4), the
// same as internal/catalogue's.
func Stale(t *time.Time, now time.Time, after time.Duration) bool {
	return t == nil || now.Sub(*t) > after
}

// stockCheckDue: one store row for the listed items on hand above 0 whose
// stock was confirmed longer ago than stock_stale_after_hours.
func (c *calc) stockCheckDue(context.Context) error {
	in, st := c.in, c.st
	var stale []*item
	for _, it := range in.items {
		if it.listed && it.onHand > 0 && Stale(it.updatedAt, in.now, st.StaleAfter) {
			stale = append(stale, it)
		}
	}
	if len(stale) == 0 || c.quiet.Has(StockCheckDue, StoreSubject) {
		return nil
	}
	// Stalest first; the count list is cut to the first 100 (06).
	sort.SliceStable(stale, func(i, j int) bool {
		a, b := stale[i].updatedAt, stale[j].updatedAt
		if a == nil || b == nil {
			return a == nil && b != nil
		}
		return a.Before(*b)
	})
	idList := make([]string, 0, min(len(stale), maxCountIDs))
	for _, it := range stale[:min(len(stale), maxCountIDs)] {
		idList = append(idList, it.id.String())
	}
	c.add(Row{
		Kind: StockCheckDue, SubjectRef: StoreSubject, Severity: Medium, Stake: int64(len(stale)),
		Facts:  map[string]any{"staleCount": len(stale), "staleAfterHours": int64(st.StaleAfter / time.Hour)},
		Action: Action{Type: ActCountStock, Payload: map[string]any{"storeItemIds": idList}},
	})
	return nil
}

// PairSubject is the subjectRef of a bought-together pair, item A first.
func PairSubject(a, b ids.ID) string { return a.String() + ":" + b.String() }

// boughtTogether: the pairs of the nightly rollup that pass the share and
// lift tests with the current settings (01 §12 pair rule), ranked by lift.
func (c *calc) boughtTogether(ctx context.Context) error {
	in, st := c.in, c.st
	pairs, err := in.q.StorePairs(ctx, in.store)
	if err != nil {
		return fmt.Errorf("facts: pairs: %w", err)
	}
	if len(pairs) == 0 {
		c.need(BoughtTogether, map[string]any{"minBills": st.PairMinBills})
		return nil
	}
	for _, p := range pairs {
		a, _ := ids.FromUUID(p.StoreItemA)
		b, _ := ids.FromUUID(p.StoreItemB)
		ia, ib := in.byID[a], in.byID[b]
		if ia == nil || ib == nil || p.BillsA <= 0 || p.BillsBoth <= 0 {
			continue
		}
		both, billsA := int64(p.BillsBoth), int64(p.BillsA)
		if both*bpScale/billsA < st.PairMinShareBp || p.LiftBp < st.PairMinLiftPct*minPairLiftScale {
			continue
		}
		sub := PairSubject(a, b)
		if c.quiet.Has(BoughtTogether, sub) {
			continue
		}
		f := map[string]any{
			"itemA": ia.name, "sizeA": ia.size, "itemB": ib.name, "sizeB": ib.size,
			"storeItemIdA": a.String(), "storeItemIdB": b.String(),
			"billsA": billsA, "billsBoth": both, "oneIn": (2*billsA + both) / (2 * both), "liftBp": p.LiftBp,
		}
		if billsA >= st.MinEventsForPercent {
			f["sharePct"] = (both*100*2 + billsA) / (2 * billsA)
		}
		c.add(Row{
			Kind: BoughtTogether, SubjectRef: sub, Severity: Low, Stake: p.LiftBp, Facts: f,
			Action: Action{Type: ActCreateOffer, Payload: map[string]any{"storeItemIds": []string{a.String(), b.String()}}},
		})
	}
	return nil
}

// Regulars is the "Regulars slipping away" computation, shared by the kind and
// GET {S}/customers?segment=slipping so both return the same customers.
type Regulars struct {
	// Linked is the phone-linked customers who bought in the window.
	Linked int
	// Slipping is the customers with at least insight_regular_min_visits
	// visits whose last visit is at least insight_regular_lapse_days ago.
	Slipping []insightsdb.CustomerVisitsRow
	// All is every linked customer of the window (the repeat customers list).
	All []insightsdb.CustomerVisitsRow
}

// LoadRegulars reads the regular window's customers. The window is the
// insight_regular_window_days whole days before today; purchases made today
// still end a lapse, so a customer who came back today is not slipping.
func LoadRegulars(ctx context.Context, q *insightsdb.Queries, st Settings, storeID ids.ID, now time.Time) (Regulars, error) {
	today := DayOf(now)
	rows, err := q.CustomerVisits(ctx, insightsdb.CustomerVisitsParams{
		StoreID: ids.UUID(storeID), FromAt: today.Add(-int(st.RegularWindowDays)).Start(), ToAt: now,
	})
	if err != nil {
		return Regulars{}, fmt.Errorf("facts: customer visits: %w", err)
	}
	out := Regulars{Linked: len(rows), All: rows}
	lapse := today.Add(-int(st.RegularLapseDays))
	for _, r := range rows {
		if int64(r.Visits) >= st.RegularMinVisits && !lapse.Before(DayFromPG(r.LastDay)) {
			out.Slipping = append(out.Slipping, r)
		}
	}
	sort.SliceStable(out.Slipping, func(i, j int) bool {
		a, b := out.Slipping[i], out.Slipping[j]
		if a.SpendPaise != b.SpendPaise {
			return a.SpendPaise > b.SpendPaise
		}
		return a.Ckey < b.Ckey
	})
	return out, nil
}

// Enough reports whether the window has enough linked customers for the
// kind to show anything.
func (r Regulars) Enough(st Settings) bool { return int64(r.Linked) >= st.RegularMinLinked }

func (c *calc) regularsSlipping(ctx context.Context) error {
	in, st := c.in, c.st
	reg, err := LoadRegulars(ctx, in.q, st, in.storeID, in.now)
	if err != nil {
		return err
	}
	if !reg.Enough(st) {
		c.need(RegularsSlipping, map[string]any{"minLinked": st.RegularMinLinked})
		return nil
	}
	if len(reg.Slipping) == 0 || c.quiet.Has(RegularsSlipping, StoreSubject) {
		return nil
	}
	c.add(Row{
		Kind: RegularsSlipping, SubjectRef: StoreSubject, Severity: Medium, Stake: int64(len(reg.Slipping)),
		Facts:  map[string]any{"count": len(reg.Slipping), "lapseDays": st.RegularLapseDays, "minVisits": st.RegularMinVisits},
		Action: Action{Type: ActSeeCustomers, Payload: map[string]any{"segment": "slipping"}},
	})
	return nil
}

// pricedAboveNearby: only while insight_price_compare_enabled; the price is
// above the median listed price across at least 10 other live verified
// stores of distinct businesses within the radius (D-71). Only the median
// is shown.
func (c *calc) pricedAboveNearby(ctx context.Context) error {
	in, st := c.in, c.st
	if !st.PriceCompare {
		return nil
	}
	med, err := in.q.NearbyMedianPrices(ctx, insightsdb.NearbyMedianPricesParams{
		StoreID: in.store, RadiusM: float64(st.RadiusM), MinBusinesses: kFloor,
	})
	if err != nil {
		return fmt.Errorf("facts: nearby prices: %w", err)
	}
	if len(med) == 0 {
		c.need(PricedAboveNearby, map[string]any{"minStores": kFloor})
		return nil
	}
	byProd := map[ids.ID]int64{}
	for _, m := range med {
		id, _ := ids.FromUUID(m.ProductID)
		byProd[id] = m.MedianPaise
	}
	for _, it := range in.items {
		m, ok := byProd[it.productID]
		if !ok || !it.listed || it.price <= m {
			continue
		}
		sub := it.id.String()
		if c.quiet.Has(PricedAboveNearby, sub) {
			continue
		}
		f := itemFacts(it)
		f["pricePaise"] = it.price
		f["typicalPaise"] = m
		c.add(Row{
			Kind: PricedAboveNearby, SubjectRef: sub, Severity: Low, Stake: it.price - m, Facts: f,
			Action: Action{Type: ActEditPrice, Payload: map[string]any{"storeItemId": sub, "refPaise": m}},
		})
	}
	return nil
}

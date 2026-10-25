// Package facts computes the Demand insights of a store (01 §12, D-99): the
// eleven kinds, their numbers, severity and evidence state, in plain SQL and
// Go with no model involved. The insights API (internal/insights) and the shop
// coach (W3-11) both import it, so a fact is computed once (IN-22).
//
// Every window is whole IST days ending yesterday; "now" is the caller's
// injected clock. Every threshold is read from platform_settings (01 §17).
// Nothing here names another shop, counts competing shops or shows a phone.
package facts

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/insightsdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// Kind is one Demand insights kind, in the order of the 01 §12 table.
type Kind string

const (
	SellingFast       Kind = "selling_fast"
	SittingOnShelf    Kind = "sitting_on_shelf"
	GoingToWaste      Kind = "going_to_waste"
	WantedNearby      Kind = "wanted_nearby"
	MissedSales       Kind = "missed_sales"
	ExpiredOnShelf    Kind = "expired_on_shelf"
	SellingBelowCost  Kind = "selling_below_cost"
	StockCheckDue     Kind = "stock_check_due"
	BoughtTogether    Kind = "bought_together"
	RegularsSlipping  Kind = "regulars_slipping"
	PricedAboveNearby Kind = "priced_above_nearby"
)

// Kinds lists every kind in the 01 §12 table order.
var Kinds = []Kind{
	SellingFast, SittingOnShelf, GoingToWaste, WantedNearby, MissedSales, ExpiredOnShelf,
	SellingBelowCost, StockCheckDue, BoughtTogether, RegularsSlipping, PricedAboveNearby,
}

// KindOf parses a kind name.
func KindOf(s string) (Kind, bool) {
	for _, k := range Kinds {
		if string(k) == s {
			return k, true
		}
	}
	return "", false
}

// Order is the kind's position in the 01 §12 table.
func (k Kind) Order() int {
	for i, x := range Kinds {
		if x == k {
			return i
		}
	}
	return len(Kinds)
}

// Dismissible reports whether "Not useful" applies: never to expired on shelf,
// a food-safety row (D-99).
func (k Kind) Dismissible() bool { return k != ExpiredOnShelf }

// StoreWide reports whether the kind's subject is the whole store.
func (k Kind) StoreWide() bool { return k == StockCheckDue || k == RegularsSlipping }

// Severity is high, medium or low (01 §12 severity ladder).
type Severity string

const (
	High   Severity = "high"
	Medium Severity = "medium"
	Low    Severity = "low"
)

func (s Severity) rank() int {
	switch s {
	case High:
		return 0
	case Medium:
		return 1
	default:
		return 2
	}
}

// StoreSubject is the subjectRef of the store-wide kinds.
const StoreSubject = "store"

// Action is what a row's button opens, prefilled. The API applies nothing.
type Action struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

// Action types (04 §2.6).
const (
	ActRestock      = "restock"
	ActCreateOffer  = "create_offer"
	ActFlashOffer   = "flash_offer"
	ActAddProduct   = "add_product"
	ActMarkExpired  = "mark_expired"
	ActEditPrice    = "edit_price"
	ActCheckCost    = "check_cost"
	ActCountStock   = "count_stock"
	ActSeeCustomers = "see_customers"
)

// Row is one firing insight.
type Row struct {
	Kind       Kind
	SubjectRef string
	Severity   Severity
	// Stake is the money (paise) or units the row is about; rows of one kind
	// go larger first.
	Stake int64
	// Last ranks the row after every other row of its kind (the "Check the
	// cost entry" row of selling below cost).
	Last   bool
	Facts  map[string]any
	Action Action
	// Quiets is every subject "Not useful" writes for this row: the subject
	// itself, or each batch of a going-to-waste row.
	Quiets []string
}

// Need is the "what data it needs" line of a kind with no row that is below
// its minimum evidence (the B-12 tab only, never the card).
type Need struct {
	Kind   Kind
	Key    string
	Params map[string]any
}

// Result is a store's insights at one instant.
type Result struct {
	Rows  []Row
	Needs []Need
	// PeriodDays is insight_period_days, for the "Last N days" tag.
	PeriodDays int64
}

// Quiet is the set of subjects "Not useful" has quieted, per kind.
type Quiet map[Kind]map[string]bool

// Has reports whether the subject of kind k is quiet.
func (q Quiet) Has(k Kind, subject string) bool { return q[k][subject] }

// Add marks a subject quiet.
func (q Quiet) Add(k Kind, subject string) {
	if q[k] == nil {
		q[k] = map[string]bool{}
	}
	q[k][subject] = true
}

// AskCount is unanswered asks for a product (or a normalised query when no
// product matched) near the store on one IST day (W3-16, D-101).
type AskCount struct {
	Day       Day
	ProductID ids.ID
	HasProd   bool
	QueryNorm string
	Count     int64
}

// AskDemand adds the unanswered asks near a store to the searches of Wanted
// nearby and Missed sales before the k ≥ 10 test (D-101). W3-16's asks.Demand
// implements it; until it lands, NoAsks returns nothing.
type AskDemand interface {
	Asks(ctx context.Context, storeID ids.ID, from, to Day, radiusM int64) ([]AskCount, error)
}

// NoAsks is the AskDemand that returns zero asks.
type NoAsks struct{}

// Asks returns nothing.
func (NoAsks) Asks(context.Context, ids.ID, Day, Day, int64) ([]AskCount, error) { return nil, nil }

// Source is what Compute reads.
type Source struct {
	Q        *insightsdb.Queries
	Settings *settings.Cache
	Asks     AskDemand
}

// Fixed constants of 01 §12 and §16 (not settings).
const (
	// kFloor is the k ≥ 10 floor on any count of people or searches shown
	// (01 §16, D-81) and on the stores behind a nearby comparison (D-71).
	kFloor = 10 //settings:ok 01 §16 fixes k ≥ 10
	// highRunOutDays: a selling-fast row is high when stock lasts this long or less.
	highRunOutDays = 2 //settings:ok fixed constant in 01 §12 severity ladder
	// offerPct is the suggested % of a sitting-on-the-shelf offer, cut to the
	// item's margin (AI-R4 may change only the %).
	offerPct = 10 //settings:ok suggestion inside the safe limits, not a rule
	// maxOfferPct caps any suggested % off.
	maxOfferPct = 90 //settings:ok safe-limit cap of a suggested % off
	// maxCountIDs is the count-mode list limit (06 Insight deep links).
	maxCountIDs = 100   //settings:ok 06 caps the count list at 100 ids
	bpScale     = 10000 //settings:ok basis points per whole
	// minPairLiftScale turns insight_pair_min_lift_pct into basis points.
	minPairLiftScale = 100 //settings:ok percent to basis points
)

// Settings are the thresholds Compute applies, read from platform_settings.
type Settings struct {
	PeriodDays          int64
	CoverDays           int64
	MinUnitsSellingFast int64
	NoSaleDays          int64
	MinDaysListed       int64
	ExpiringWithinDays  int64
	RadiusM             int64
	MinSearches         int64
	MissedMinZeroDays   int64
	MissedMinSearches   int64
	PairMinBills        int64
	PairMinShareBp      int64
	PairMinLiftPct      int64
	MinEventsForPercent int64
	RegularMinVisits    int64
	RegularWindowDays   int64
	RegularLapseDays    int64
	RegularMinLinked    int64
	StaleAfter          time.Duration
	PriceCompare        bool
}

// LoadSettings reads every threshold.
func LoadSettings(ctx context.Context, c *settings.Cache) (Settings, error) {
	var s Settings
	ints := []struct {
		k settings.Key
		v *int64
	}{
		{settings.InsightPeriodDays, &s.PeriodDays},
		{settings.InsightCoverDays, &s.CoverDays},
		{settings.InsightMinUnitsSellingFast, &s.MinUnitsSellingFast},
		{settings.InsightNoSaleDays, &s.NoSaleDays},
		{settings.InsightMinDaysListed, &s.MinDaysListed},
		{settings.InsightExpiringWithinDays, &s.ExpiringWithinDays},
		{settings.InsightWantedNearbyRadiusM, &s.RadiusM},
		{settings.InsightMinSearches, &s.MinSearches},
		{settings.InsightMissedSalesMinZeroDays, &s.MissedMinZeroDays},
		{settings.InsightMissedSalesMinSearches, &s.MissedMinSearches},
		{settings.InsightPairMinBills, &s.PairMinBills},
		{settings.InsightPairMinLiftPct, &s.PairMinLiftPct},
		{settings.InsightMinEventsForPercent, &s.MinEventsForPercent},
		{settings.InsightRegularMinVisits, &s.RegularMinVisits},
		{settings.InsightRegularWindowDays, &s.RegularWindowDays},
		{settings.InsightRegularLapseDays, &s.RegularLapseDays},
		{settings.InsightRegularMinLinkedCustomers, &s.RegularMinLinked},
	}
	for _, x := range ints {
		n, err := c.Int(ctx, x.k)
		if err != nil {
			return Settings{}, fmt.Errorf("facts: %s: %w", x.k, err)
		}
		*x.v = n
	}
	bp, err := c.BP(ctx, settings.InsightPairMinShareBp)
	if err != nil {
		return Settings{}, fmt.Errorf("facts: %s: %w", settings.InsightPairMinShareBp, err)
	}
	s.PairMinShareBp = int64(bp)
	if s.StaleAfter, err = c.Duration(ctx, settings.StockStaleAfterHours); err != nil {
		return Settings{}, fmt.Errorf("facts: %s: %w", settings.StockStaleAfterHours, err)
	}
	if s.PriceCompare, err = c.Bool(ctx, settings.InsightPriceCompareEnabled); err != nil {
		return Settings{}, fmt.Errorf("facts: %s: %w", settings.InsightPriceCompareEnabled, err)
	}
	// The k floors can never go lower than 10, whatever the table says (01 §16).
	s.MinSearches = max(s.MinSearches, kFloor)
	s.MissedMinSearches = max(s.MissedMinSearches, kFloor)
	s.PeriodDays = max(s.PeriodDays, 1)
	return s, nil
}

// Compute returns the store's firing rows (quiet subjects left out) and the
// needs-data lines, at now.
func Compute(ctx context.Context, src Source, storeID ids.ID, now time.Time, quiet Quiet) (Result, error) {
	st, err := LoadSettings(ctx, src.Settings)
	if err != nil {
		return Result{}, err
	}
	return ComputeWith(ctx, src, st, storeID, now, quiet)
}

// ComputeWith is Compute with the thresholds already read.
func ComputeWith(ctx context.Context, src Source, st Settings, storeID ids.ID, now time.Time, quiet Quiet) (Result, error) {
	if quiet == nil {
		quiet = Quiet{}
	}
	asks := src.Asks
	if asks == nil {
		asks = NoAsks{}
	}
	in, err := load(ctx, src.Q, asks, st, storeID, now)
	if err != nil {
		return Result{}, err
	}
	c := &calc{in: in, st: st, quiet: quiet}
	for _, f := range []func(context.Context) error{
		c.sellingFast, c.sittingOnShelf, c.goingToWaste, c.wantedNearby, c.missedSales,
		c.expiredOnShelf, c.sellingBelowCost, c.stockCheckDue, c.boughtTogether, c.regularsSlipping,
		c.pricedAboveNearby,
	} {
		if err := f(ctx); err != nil {
			return Result{}, err
		}
	}
	Sort(c.rows)
	return Result{Rows: c.rows, Needs: c.needs, PeriodDays: st.PeriodDays}, nil
}

// Sort orders rows by severity, then kind in table order, then the "last"
// rows of a kind after the others, then stake (larger first).
func Sort(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Severity.rank() != b.Severity.rank() {
			return a.Severity.rank() < b.Severity.rank()
		}
		if a.Kind.Order() != b.Kind.Order() {
			return a.Kind.Order() < b.Kind.Order()
		}
		if a.Last != b.Last {
			return !a.Last
		}
		if a.Stake != b.Stake {
			return a.Stake > b.Stake
		}
		return a.SubjectRef < b.SubjectRef
	})
}

// Card picks the B-03 card: the top n rows by severity, at most one per kind.
func Card(rows []Row, n int) []Row {
	seen := map[Kind]bool{}
	out := make([]Row, 0, n)
	for _, r := range rows {
		if len(out) == n {
			break
		}
		if seen[r.Kind] {
			continue
		}
		seen[r.Kind] = true
		out = append(out, r)
	}
	return out
}

// --- IST days ----------------------------------------------------------------

// Day is an IST calendar day, held as midnight UTC of that date (the same
// convention as internal/rollups, which this package does not import).
type Day time.Time

// DayOf returns the IST day holding t.
func DayOf(t time.Time) Day {
	y, m, d := t.In(clock.IST).Date()
	return Day(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
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

// Sub is the number of days from e to d.
func (d Day) Sub(e Day) int { return int(time.Time(d).Sub(time.Time(e)).Hours() / 24) }

func (d Day) String() string { return time.Time(d).Format(time.DateOnly) }

// PG is the day as a database date.
func (d Day) PG() pgtype.Date { return pgtype.Date{Time: time.Time(d), Valid: true} }

// DayFromPG reads a database date.
func DayFromPG(p pgtype.Date) Day { return Day(p.Time) }

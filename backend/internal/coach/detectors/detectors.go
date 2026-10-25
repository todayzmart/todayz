// Package detectors is the shop coach's fact library (11 §2.1 step 1, D-48):
// each detector reads a store's own numbers in plain SQL and Go, with no model,
// and returns candidate facts with the numbers a card may use, a severity and
// the action the card's button opens prefilled.
//
// Where a detector covers a Demand insights kind it takes the row from
// internal/insights/facts (W3-03) and adds only severity, recency and the card
// type, so the card and the coach never compute a number twice (D-99). The
// coach does not repeat "priced above nearby" while
// insight_price_compare_enabled is false.
//
// A statement about other people (searches near the shop, a benchmark against
// nearby shops) needs at least KFloor people or shops behind it, always
// (01 §16, D-71); below that the detector does not fire.
package detectors

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/todayz/backend/gen/sqlc/coachdb"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// Job is the coach job a detector runs in (11 §2.1 table).
type Job string

const (
	Daily    Job = "coach_daily"
	Weekly   Job = "coach_weekly"
	Realtime Job = "coach_realtime_rules"
)

// CardKind is the card's type on B-19: High, Low, Do this or Tip.
type CardKind string

const (
	High   CardKind = "high"
	Low    CardKind = "low"
	Action CardKind = "action"
	Tip    CardKind = "tip"
)

// Topics of a card (the coach_suggestions.topic check).
const (
	TopicSales       = "sales"
	TopicStock       = "stock"
	TopicOrders      = "orders"
	TopicReliability = "reliability"
	TopicOffers      = "offers"
	TopicReviews     = "reviews"
	TopicDemand      = "demand"
	TopicSetup       = "setup"
)

// Coach-only action types (the Demand insights ones are facts.Act*).
const (
	ActReplyReviews     = "reply_reviews"
	ActTurnOnAutoAccept = "turn_on_auto_accept"
	ActUpdateStock      = "update_stock"
	ActEnableDelivery   = "enable_delivery"
	ActNone             = "none"
)

// Fixed numbers of the plan (not settings).
const (
	// KFloor is the k ≥ 10 floor on any statement about other people or shops
	// (01 §16, D-43, D-71). It never goes lower, whatever a setting says.
	KFloor = 10 //settings:ok 01 §16 fixes k ≥ 10
	// recordWeeks is how many earlier same weekdays a record day beats
	// ("Best Thursday in 6 weeks", 11 §2.1).
	recordWeeks = 6 //settings:ok the window named in the 11 §2.1 example card
	// recordMinWeeks is the earlier same weekdays with sales a record needs, so
	// a shop's second week is not a "record" (minimum evidence, 11 §2.1).
	// TODO(lead): no 01 key; conservative reading, at least 4 of the 6 weeks.
	recordMinWeeks = 4 //settings:ok minimum evidence of best_day_record, see TODO(lead)
	// summaryWeeks is the earlier weeks the weekly summary compares against
	// ("week vs previous 4 weeks", 11 §2.1).
	summaryWeeks = 4 //settings:ok the window named in the 11 §2.1 table
	// ratingRecentDays and ratingEarlierDays are the rating trend windows:
	// the last 30 days against the 60 before them.
	ratingRecentDays  = 30 //settings:ok window of the weekly rating trend (11 §2.1 table)
	ratingEarlierDays = 60 //settings:ok window of the weekly rating trend (11 §2.1 table)
	// ratingDropTenths is the fall in average stars (in tenths) that makes a
	// rating drop. TODO(lead): no 01 key; conservative reading, half a star.
	ratingDropTenths = 5 //settings:ok threshold of rating_drop, see TODO(lead)
	// topLeaders is how many items a record-day card names.
	topLeaders = 2 //settings:ok "Maggi and Amul milk led" names two (11 §2.1)
	// itemNameMax keeps a product name short enough for a 200-character body.
	itemNameMax = 48    //settings:ok display limit of a name inside a card body
	bpScale     = 10000 //settings:ok basis points per whole
	// penaltyDays is the window of the stored unaccepted-expiry rate (D-46).
	penaltyDays = 30 //settings:ok the 30-day rate of 01 §5 and D-46
	// slowFactor: accepting is slow at this many times the nearby median.
	slowFactor = 2 //settings:ok see TODO(lead) in acceptTimeVsBenchmark
)

// Fact is one candidate card.
type Fact struct {
	Detector string
	// Subject is what the card is about: a store item, a batch, a pair a:b or
	// "store". Not useful quiets this subject of this detector, never the kind.
	Subject string
	Kind    CardKind
	Topic   string
	// Severity is 3 (high), 2 (medium) or 1 (low).
	Severity int
	// Stake breaks ties inside a severity: money (paise) or units.
	Stake int64
	// AgeDays is how old the evidence is (0: today or yesterday).
	AgeDays int
	// Numbers is every value the card's text may use, display-ready ("₹18,240",
	// "Amul butter 100 g", 3). The grounding check reads only these.
	Numbers map[string]any
	// Variant picks a template variant (selling_below_cost check_cost …).
	Variant string
	Action  facts.Action
	// AboutOthers marks a statement about other people or shops (k ≥ 10).
	AboutOthers bool
	// Others is the count of people or shops behind an AboutOthers fact.
	Others int64
}

// Store is the store a run is for.
type Store struct {
	ID ids.ID
	coachdb.StoreProfileRow
}

// Input is what a detector reads.
type Input struct {
	Q        *coachdb.Queries
	Settings *settings.Cache
	Store    Store
	Now      time.Time
	// Today is the IST day of Now; windows end yesterday.
	Today facts.Day
	// Facts is the store's Demand insights at Now (computed once per run, quiet
	// subjects of insight_feedback and of the coach left out).
	Facts       facts.Result
	FactsLoaded bool
	FactsCfg    facts.Settings
}

// Detector computes one kind of fact.
type Detector struct {
	Name string
	Jobs []Job
	// FromFacts is the Demand insights kind the detector maps (empty: own SQL).
	FromFacts facts.Kind
	Run       func(ctx context.Context, in *Input) ([]Fact, error)
}

// In reports whether the detector runs in job j.
func (d Detector) In(j Job) bool {
	for _, x := range d.Jobs {
		if x == j {
			return true
		}
	}
	return false
}

// For returns the detectors of job j, in table order.
func For(j Job) []Detector {
	var out []Detector
	for _, d := range All() {
		if d.In(j) {
			out = append(out, d)
		}
	}
	return out
}

// Names lists every detector name (the coach_weights rows, dashboard 14).
func Names() []string {
	all := All()
	out := make([]string, len(all))
	for i, d := range all {
		out[i] = d.Name
	}
	return out
}

// NeedsFacts reports whether any detector of j reads the Demand insights.
func NeedsFacts(j Job) bool {
	for _, d := range For(j) {
		if d.FromFacts != "" {
			return true
		}
	}
	return false
}

// SeverityOf maps a Demand insights severity.
func SeverityOf(s facts.Severity) int {
	switch s {
	case facts.High:
		return 3
	case facts.Medium:
		return 2
	default:
		return 1
	}
}

// Rupees formats paise as whole rupees with Indian grouping ("₹1,23,456").
func Rupees(paise int64) string {
	neg := paise < 0
	if neg {
		paise = -paise
	}
	r := (paise + 50) / 100
	s := strconv.FormatInt(r, 10)
	if len(s) > 3 {
		head, tail := s[:len(s)-3], s[len(s)-3:]
		var parts []string
		for len(head) > 2 {
			parts = append([]string{head[len(head)-2:]}, parts...)
			head = head[:len(head)-2]
		}
		if head != "" {
			parts = append([]string{head}, parts...)
		}
		s = strings.Join(parts, ",") + "," + tail
	}
	if neg {
		return "-₹" + s
	}
	return "₹" + s
}

// ItemName joins a name and size and keeps it short ("Amul butter 100 g").
func ItemName(name, size string) string {
	s := strings.TrimSpace(name)
	if size = strings.TrimSpace(size); size != "" && !strings.HasSuffix(s, size) {
		s += " " + size
	}
	r := []rune(s)
	if len(r) > itemNameMax {
		s = strings.TrimSpace(string(r[:itemNameMax-1])) + "…"
	}
	return s
}

// HourRange is "7–9 pm" for the IST hours [from, to] (to inclusive).
func HourRange(from, to int) string {
	h := func(x int) (int, string) {
		x %= 24
		suf := "am"
		if x >= 12 {
			suf = "pm"
		}
		x %= 12
		if x == 0 {
			x = 12
		}
		return x, suf
	}
	a, sa := h(from)
	b, sb := h(to + 1)
	if sa == sb {
		return fmt.Sprintf("%d–%d %s", a, b, sb)
	}
	return fmt.Sprintf("%d %s–%d %s", a, sa, b, sb)
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

func i64(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	default:
		return 0
	}
}

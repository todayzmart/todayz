package offers

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/todayz/backend/internal/platform/ids"
)

// Kind is an offer's kind (03 §9, offers.kind).
type Kind string

// The offer kinds.
const (
	KindPercent Kind = "percent"  // value is whole percent of the line
	KindFlat    Kind = "flat"     // value is paise off each unit
	KindBXGY    Kind = "bxgy"     // buy buy_qty, get get_qty of the same item free
	KindBillMin Kind = "bill_min" // value is paise off a bill of min_bill_paise or more
)

// Scope is what an offer covers (offers.scope).
type Scope string

// The scopes.
const (
	ScopeStore    Scope = "store"
	ScopeItems    Scope = "items"
	ScopeCategory Scope = "category"
)

// Channels is where an offer applies (offers.channels).
type Channels string

// The channel sets.
const (
	ChannelsOnline Channels = "online"
	ChannelsPOS    Channels = "pos"
	ChannelsBoth   Channels = "both"
)

// Status is an offer's state (offers.status).
type Status string

// The statuses.
const (
	StatusScheduled Status = "scheduled"
	StatusLive      Status = "live"
	StatusPaused    Status = "paused"
	StatusEnded     Status = "ended"
)

// MaxPercent is the largest percent an offer may take off. TODO(lead): a safe
// limit the plan does not number; 90 keeps a typo from giving an item away.
const MaxPercent = 90

// minTotalPaise is the floor of a bill: never below ₹1 (01 §12).
const minTotalPaise = 100

// Offer is one offer as the pricing engine reads it.
type Offer struct {
	ID    ids.ID
	Title string // "" derives it from the terms (see TitleOf)
	Kind  Kind
	// Value: percent for KindPercent, paise per unit for KindFlat, paise off
	// the bill for KindBillMin.
	Value        int64
	BuyQty       int
	GetQty       int
	MinBillPaise int64
	Scope        Scope
	StoreItemIDs []ids.ID
	CategoryID   ids.ID
	Channels     Channels
	// PerCustomerLimit and MaxRedemptions are 0 for "no limit".
	PerCustomerLimit int
	MaxRedemptions   int
	Redemptions      int
	StartsAt         time.Time
	EndsAt           time.Time // zero: no end
	Status           Status
}

// Live reports whether the offer can apply at t on channel ch: scheduled or
// live (the scheduling job only tidies the status), inside its dates by the
// given time, on the channel, and with redemptions left.
func (o Offer) Live(t time.Time, ch Channel) bool {
	if o.Status != StatusLive && o.Status != StatusScheduled {
		return false
	}
	if t.Before(o.StartsAt) || (!o.EndsAt.IsZero() && !t.Before(o.EndsAt)) {
		return false
	}
	switch o.Channels {
	case ChannelsOnline:
		if ch != ChannelOnline {
			return false
		}
	case ChannelsPOS:
		if ch != ChannelPOS {
			return false
		}
	}
	return o.MaxRedemptions == 0 || o.Redemptions < o.MaxRedemptions
}

// input is everything compute reads: no clock, no database.
type input struct {
	cart   Cart
	now    time.Time
	offers []Offer
	// cats[i] is line i's category and its ancestors.
	cats [][]ids.ID
	// used counts the customer's unreleased redemptions by offer.
	used map[ids.ID]int
}

func (in input) eligible(o Offer) bool {
	if !o.Live(in.now, in.cart.Channel) {
		return false
	}
	if o.PerCustomerLimit > 0 {
		if in.cart.CustomerID == ids.Zero || in.used[o.ID] >= o.PerCustomerLimit {
			return false
		}
	}
	return true
}

func (o Offer) covers(item ids.ID, cats []ids.ID) bool {
	switch o.Scope {
	case ScopeItems:
		return slices.Contains(o.StoreItemIDs, item)
	case ScopeCategory:
		return o.CategoryID != ids.Zero && slices.Contains(cats, o.CategoryID)
	default:
		return true
	}
}

// itemDiscount is what an item-level offer takes off a line of qty units at
// unit paise, and how many units it gives free. The amounts are floored paise.
func (o Offer) itemDiscount(unit int64, qty int) (paise int64, free int) {
	gross := unit * int64(qty)
	switch o.Kind {
	case KindPercent:
		return gross * o.Value / 100, 0
	case KindFlat:
		return min(o.Value, unit) * int64(qty), 0
	case KindBXGY:
		group := o.BuyQty + o.GetQty
		if o.BuyQty < 1 || o.GetQty < 1 || group < 1 {
			return 0, 0
		}
		free = qty / group * o.GetQty
		return unit * int64(free), free
	}
	return 0, 0
}

// older reports whether a was made before b (ULIDs sort by creation time).
func older(a, b ids.ID) bool { return bytes.Compare(a[:], b[:]) < 0 }

// compute prices the cart. It never fails: a nonsense cart prices to what it
// can (negative quantities and prices count as 0).
func compute(in input) Quote {
	n := len(in.cart.Lines)
	q := Quote{Lines: make([]LineQuote, n)}
	item := map[ids.ID]*Applied{}
	var order []ids.ID

	for i, l := range in.cart.Lines {
		unit, qty := max(l.UnitPricePaise, 0), max(l.Quantity, 0)
		lq := &q.Lines[i]
		lq.GrossPaise = unit * int64(qty)
		q.GrossPaise += lq.GrossPaise
		if l.Fixed || lq.GrossPaise == 0 {
			continue
		}
		var cats []ids.ID
		if i < len(in.cats) {
			cats = in.cats[i]
		}
		var best Offer
		var bestPaise int64
		var bestFree int
		for _, o := range in.offers {
			if o.Kind == KindBillMin || !in.eligible(o) || !o.covers(l.StoreItemID, cats) {
				continue
			}
			d, free := o.itemDiscount(unit, qty)
			d = min(d, lq.GrossPaise)
			if d > bestPaise || (d == bestPaise && d > 0 && older(o.ID, best.ID)) {
				best, bestPaise, bestFree = o, d, free
			}
		}
		if bestPaise == 0 {
			continue
		}
		lq.DiscountPaise, lq.OfferID, lq.FreeUnits = bestPaise, best.ID, bestFree
		if _, ok := item[best.ID]; !ok {
			item[best.ID] = &Applied{OfferID: best.ID, Title: TitleOf(best), Kind: best.Kind}
			order = append(order, best.ID)
		}
		item[best.ID].DiscountPaise += bestPaise
	}

	// The bill offer, compared with the item subtotal (D-94).
	var itemDisc int64
	for _, lq := range q.Lines {
		itemDisc += lq.DiscountPaise
	}
	itemSubtotal := q.GrossPaise - itemDisc
	var bill *Applied
	var billPaise int64
	if itemSubtotal > 0 {
		var best Offer
		for _, o := range in.offers {
			if o.Kind != KindBillMin || !in.eligible(o) || itemSubtotal < o.MinBillPaise {
				continue
			}
			d := min(max(o.Value, 0), itemSubtotal)
			if d > billPaise || (d == billPaise && d > 0 && older(o.ID, best.ID)) {
				best, billPaise = o, d
			}
		}
		if billPaise > 0 {
			bill = &Applied{OfferID: best.ID, Title: TitleOf(best), Kind: best.Kind, DiscountPaise: billPaise}
		}
	}

	// Never below ₹1: the bill discount gives way first, then the line
	// discounts from the last line backwards.
	over := itemDisc + billPaise - max(q.GrossPaise-minTotalPaise, 0)
	if over > 0 {
		cut := min(over, billPaise)
		billPaise -= cut
		over -= cut
		for i := n - 1; i >= 0 && over > 0; i-- {
			lq := &q.Lines[i]
			c := min(over, lq.DiscountPaise)
			if c == 0 {
				continue
			}
			lq.DiscountPaise -= c
			over -= c
			item[lq.OfferID].DiscountPaise -= c
			itemDisc -= c
			if lq.DiscountPaise == 0 {
				lq.OfferID, lq.FreeUnits = ids.Zero, 0
			}
		}
		if bill != nil {
			bill.DiscountPaise = billPaise
			if billPaise == 0 {
				bill = nil
			}
		}
	}

	for _, id := range order {
		if a := item[id]; a.DiscountPaise > 0 {
			q.Applied = append(q.Applied, *a)
		}
	}
	q.ItemDiscountPaise = itemDisc
	q.ItemSubtotalPaise = q.GrossPaise - itemDisc
	q.BillDiscountPaise, q.BillOffer = billPaise, bill
	if bill != nil {
		q.Applied = append(q.Applied, *bill)
	}
	q.TotalPaise = q.ItemSubtotalPaise - billPaise
	return q
}

// TitleOf is the offer's customer-facing name: its own title, or one made from
// its terms ("20% off", "₹50 off", "Buy 2 get 1 free", "₹50 off on ₹500+").
func TitleOf(o Offer) string {
	if o.Title != "" {
		return o.Title
	}
	switch o.Kind {
	case KindPercent:
		return strconv.FormatInt(o.Value, 10) + "% off"
	case KindFlat:
		return rupees(o.Value) + " off"
	case KindBXGY:
		return fmt.Sprintf("Buy %d get %d free", o.BuyQty, o.GetQty)
	case KindBillMin:
		return rupees(o.Value) + " off on " + rupees(o.MinBillPaise) + "+"
	}
	return "Offer"
}

// rupees is "₹50" or "₹49.50" for paise.
func rupees(p int64) string {
	if p%100 == 0 {
		return "₹" + strconv.FormatInt(p/100, 10)
	}
	return fmt.Sprintf("₹%d.%02d", p/100, p%100)
}

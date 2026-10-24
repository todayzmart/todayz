package offers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

var t0 = time.Date(2026, 10, 26, 4, 30, 0, 0, time.UTC) // 10:00 IST

// id makes a fixed id; a bigger n is a newer offer.
func id(n int) ids.ID {
	var x ids.ID
	x[5], x[15] = byte(n>>8), byte(n)
	return x
}

var (
	milk, rice, soap  = id(1), id(2), id(3)
	catFood, catDairy = id(10), id(11)
	cust, other       = id(20), id(21)
)

func line(item ids.ID, unit int64, qty int) Line {
	return Line{StoreItemID: item, UnitPricePaise: unit, Quantity: qty}
}

func live(o Offer) Offer {
	if o.Status == "" {
		o.Status = StatusLive
	}
	if o.StartsAt.IsZero() {
		o.StartsAt = t0.Add(-time.Hour)
	}
	if o.Channels == "" {
		o.Channels = ChannelsBoth
	}
	if o.Scope == "" {
		o.Scope = ScopeStore
	}
	return o
}

func pct(n int, v int64, items ...ids.ID) Offer {
	o := live(Offer{ID: id(n), Kind: KindPercent, Value: v})
	if len(items) > 0 {
		o.Scope, o.StoreItemIDs = ScopeItems, items
	}
	return o
}

func flat(n int, paise int64, items ...ids.ID) Offer {
	o := live(Offer{ID: id(n), Kind: KindFlat, Value: paise})
	if len(items) > 0 {
		o.Scope, o.StoreItemIDs = ScopeItems, items
	}
	return o
}

func bxgy(n, buy, get int, items ...ids.ID) Offer {
	return live(Offer{ID: id(n), Kind: KindBXGY, BuyQty: buy, GetQty: get, Scope: ScopeItems, StoreItemIDs: items})
}

func bill(n int, paise, min int64) Offer {
	return live(Offer{ID: id(n), Kind: KindBillMin, Value: paise, MinBillPaise: min})
}

func price(t *testing.T, cart Cart, offers ...Offer) Quote {
	t.Helper()
	f := NewFake(clock.NewFake(t0)).Add(offers...)
	f.Categorise(milk, catDairy, catFood).Categorise(rice, catFood)
	if cart.Channel == "" {
		cart.Channel = ChannelOnline
	}
	q, err := f.Price(context.Background(), cart)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestPriceTable(t *testing.T) {
	type want struct {
		line         []int64 // per-line discount
		bill         int64
		itemSubtotal int64
		total        int64
		applied      []ids.ID
	}
	tests := []struct {
		name   string
		cart   Cart
		offers []Offer
		want   want
	}{
		{"no offers", Cart{Lines: []Line{line(milk, 5000, 2)}}, nil,
			want{[]int64{0}, 0, 10000, 10000, nil}},
		{"percent", Cart{Lines: []Line{line(milk, 5000, 2)}}, []Offer{pct(30, 20)},
			want{[]int64{2000}, 0, 8000, 8000, []ids.ID{id(30)}}},
		{"percent floors to the paisa", Cart{Lines: []Line{line(milk, 333, 1)}}, []Offer{pct(30, 15)},
			want{[]int64{49}, 0, 284, 284, []ids.ID{id(30)}}}, // 49.95 → 49
		{"flat is per unit", Cart{Lines: []Line{line(milk, 5000, 3)}}, []Offer{flat(30, 500)},
			want{[]int64{1500}, 0, 13500, 13500, []ids.ID{id(30)}}},
		{"flat larger than the unit price gives the unit away, not more", Cart{Lines: []Line{line(rice, 300, 2), line(milk, 5000, 1)}},
			[]Offer{flat(30, 1000, rice)}, want{[]int64{600, 0}, 0, 5000, 5000, []ids.ID{id(30)}}},
		{"buy 2 get 1 with 3 units", Cart{Lines: []Line{line(milk, 5000, 3)}}, []Offer{bxgy(30, 2, 1, milk)},
			want{[]int64{5000}, 0, 10000, 10000, []ids.ID{id(30)}}},
		{"buy 2 get 1 with 2 units gives nothing", Cart{Lines: []Line{line(milk, 5000, 2)}}, []Offer{bxgy(30, 2, 1, milk)},
			want{[]int64{0}, 0, 10000, 10000, nil}},
		{"buy 2 get 1 with 7 units: two groups", Cart{Lines: []Line{line(milk, 1000, 7)}}, []Offer{bxgy(30, 2, 1, milk)},
			want{[]int64{2000}, 0, 5000, 5000, []ids.ID{id(30)}}},
		{"buy 1 get 1", Cart{Lines: []Line{line(milk, 1000, 5)}}, []Offer{bxgy(30, 1, 1, milk)},
			want{[]int64{2000}, 0, 3000, 3000, []ids.ID{id(30)}}},
		{"best of several item offers wins", Cart{Lines: []Line{line(milk, 5000, 2)}},
			[]Offer{pct(30, 10), flat(31, 1000), pct(32, 5)},
			want{[]int64{2000}, 0, 8000, 8000, []ids.ID{id(31)}}},
		{"a tie goes to the older offer", Cart{Lines: []Line{line(milk, 5000, 2)}},
			[]Offer{pct(32, 10), pct(31, 10)},
			want{[]int64{1000}, 0, 9000, 9000, []ids.ID{id(31)}}},
		{"each line has its own best offer", Cart{Lines: []Line{line(milk, 5000, 1), line(rice, 2000, 1)}},
			[]Offer{pct(30, 10, milk), flat(31, 500, rice)},
			want{[]int64{500, 500}, 0, 6000, 6000, []ids.ID{id(30), id(31)}}},
		{"one offer on two lines is one applied entry with the sum", Cart{Lines: []Line{line(milk, 5000, 1), line(rice, 2000, 1)}},
			[]Offer{pct(30, 10)},
			want{[]int64{500, 200}, 0, 6300, 6300, []ids.ID{id(30)}}},
		{"bill offer below the threshold does not apply", Cart{Lines: []Line{line(milk, 49999, 1)}}, []Offer{bill(30, 5000, 50000)},
			want{[]int64{0}, 0, 49999, 49999, nil}},
		{"bill offer at the threshold applies (inclusive, D-94)", Cart{Lines: []Line{line(milk, 50000, 1)}}, []Offer{bill(30, 5000, 50000)},
			want{[]int64{0}, 5000, 50000, 45000, []ids.ID{id(30)}}},
		{"bill offer is compared after item offers (01 §5.5 example)", Cart{Lines: []Line{line(milk, 52000, 1)}},
			[]Offer{flat(31, 3000), bill(30, 5000, 50000)},
			want{[]int64{3000}, 0, 49000, 49000, []ids.ID{id(31)}}},
		{"item and bill offers stack", Cart{Lines: []Line{line(milk, 60000, 1)}},
			[]Offer{pct(31, 10), bill(30, 5000, 50000)},
			want{[]int64{6000}, 5000, 54000, 49000, []ids.ID{id(31), id(30)}}},
		{"only the best bill offer applies", Cart{Lines: []Line{line(milk, 60000, 1)}},
			[]Offer{bill(30, 2000, 10000), bill(31, 5000, 50000), bill(32, 3000, 20000)},
			want{[]int64{0}, 5000, 60000, 55000, []ids.ID{id(31)}}},
		{"never below one rupee: the bill discount gives way first", Cart{Lines: []Line{line(milk, 6000, 1)}},
			[]Offer{bill(30, 5950, 5000)},
			want{[]int64{0}, 5900, 6000, 100, []ids.ID{id(30)}}},
		{"never below one rupee: then the last line's discount", Cart{Lines: []Line{line(milk, 500, 1), line(rice, 500, 1)}},
			[]Offer{flat(30, 500, milk, rice)},
			want{[]int64{500, 400}, 0, 100, 100, []ids.ID{id(30)}}},
		{"a bill of under one rupee gets no discount at all", Cart{Lines: []Line{line(milk, 80, 1)}}, []Offer{pct(30, 50)},
			want{[]int64{0}, 0, 80, 80, nil}},
		{"fixed-price line takes no item offer but counts for the bill offer", Cart{Lines: []Line{{StoreItemID: milk, UnitPricePaise: 30000, Quantity: 1, Fixed: true}, line(rice, 25000, 1)}},
			[]Offer{pct(31, 10), bill(30, 5000, 50000)},
			want{[]int64{0, 2500}, 5000, 52500, 47500, []ids.ID{id(31), id(30)}}},
		{"category scope covers the category and its descendants", Cart{Lines: []Line{line(milk, 5000, 1), line(rice, 2000, 1), line(soap, 1000, 1)}},
			[]Offer{live(Offer{ID: id(30), Kind: KindPercent, Value: 10, Scope: ScopeCategory, CategoryID: catFood})},
			want{[]int64{500, 200, 0}, 0, 7300, 7300, []ids.ID{id(30)}}},
		{"items scope covers only the listed items", Cart{Lines: []Line{line(milk, 5000, 1), line(rice, 2000, 1)}},
			[]Offer{pct(30, 10, rice)},
			want{[]int64{0, 200}, 0, 6800, 6800, []ids.ID{id(30)}}},
		{"empty cart", Cart{}, []Offer{pct(30, 10), bill(31, 100, 0)}, want{[]int64{}, 0, 0, 0, nil}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := price(t, tc.cart, tc.offers...)
			if len(q.Lines) != len(tc.want.line) {
				t.Fatalf("lines = %d, want %d", len(q.Lines), len(tc.want.line))
			}
			for i, w := range tc.want.line {
				if q.Lines[i].DiscountPaise != w {
					t.Errorf("line %d discount = %d, want %d", i, q.Lines[i].DiscountPaise, w)
				}
			}
			if q.BillDiscountPaise != tc.want.bill || q.ItemSubtotalPaise != tc.want.itemSubtotal || q.TotalPaise != tc.want.total {
				t.Errorf("bill/itemSubtotal/total = %d/%d/%d, want %d/%d/%d", q.BillDiscountPaise, q.ItemSubtotalPaise, q.TotalPaise,
					tc.want.bill, tc.want.itemSubtotal, tc.want.total)
			}
			var got []ids.ID
			var sum int64
			for _, a := range q.Applied {
				got = append(got, a.OfferID)
				sum += a.DiscountPaise
			}
			if len(got) != len(tc.want.applied) {
				t.Fatalf("applied = %v, want %v", got, tc.want.applied)
			}
			for i := range got {
				if got[i] != tc.want.applied[i] {
					t.Errorf("applied[%d] = %v, want %v", i, got[i], tc.want.applied[i])
				}
			}
			if sum != q.DiscountPaise() || q.GrossPaise-q.DiscountPaise() != q.TotalPaise {
				t.Errorf("applied sum %d, discount %d, gross %d, total %d do not add up", sum, q.DiscountPaise(), q.GrossPaise, q.TotalPaise)
			}
		})
	}
}

func TestFreeUnitsAndTitles(t *testing.T) {
	q := price(t, Cart{Lines: []Line{line(milk, 1000, 7)}}, bxgy(30, 2, 1, milk))
	if q.Lines[0].FreeUnits != 2 {
		t.Errorf("free units = %d, want 2", q.Lines[0].FreeUnits)
	}
	for _, tc := range []struct {
		o    Offer
		want string
	}{
		{pct(1, 20), "20% off"}, {flat(1, 5000), "₹50 off"}, {flat(1, 4950), "₹49.50 off"},
		{bxgy(1, 2, 1, milk), "Buy 2 get 1 free"}, {bill(1, 5000, 50000), "₹50 off on ₹500+"},
		{live(Offer{Kind: KindPercent, Value: 5, Title: "Diwali special"}), "Diwali special"},
	} {
		if got := TitleOf(tc.o); got != tc.want {
			t.Errorf("TitleOf = %q, want %q", got, tc.want)
		}
	}
}

// Expiry, scheduling and the channel read the injected clock, never the wall.
func TestPriceByTheInjectedClock(t *testing.T) {
	clk := clock.NewFake(t0)
	o := pct(30, 10)
	o.StartsAt, o.EndsAt = t0.Add(time.Hour), t0.Add(3*time.Hour)
	f := NewFake(clk).Add(o)
	cart := Cart{Channel: ChannelOnline, Lines: []Line{line(milk, 10000, 1)}}
	disc := func() int64 {
		q, _ := f.Price(context.Background(), cart)
		return q.DiscountPaise()
	}
	for _, step := range []struct {
		at   time.Duration
		want int64
		why  string
	}{
		{0, 0, "before it starts"},
		{time.Hour - time.Nanosecond, 0, "a hair before it starts"},
		{time.Hour, 1000, "at the start (inclusive)"},
		{3*time.Hour - time.Nanosecond, 1000, "a hair before the end"},
		{3 * time.Hour, 0, "at the end (exclusive)"},
		{10 * time.Hour, 0, "long after"},
	} {
		clk.Set(t0.Add(step.at))
		if got := disc(); got != step.want {
			t.Errorf("%s: discount = %d, want %d", step.why, got, step.want)
		}
	}
	// An explicit time on the cart wins (a POS sale priced at its own time).
	cart.At = t0.Add(2 * time.Hour)
	if got := disc(); got != 1000 {
		t.Errorf("cart.At inside the window: discount = %d, want 1000", got)
	}
}

func TestStatusAndChannel(t *testing.T) {
	cart := func(ch Channel) Cart { return Cart{Channel: ch, Lines: []Line{line(milk, 10000, 1)}} }
	for _, tc := range []struct {
		name string
		mod  func(*Offer)
		ch   Channel
		want int64
	}{
		{"live online", func(*Offer) {}, ChannelOnline, 1000},
		{"live at the pos", func(*Offer) {}, ChannelPOS, 1000},
		{"scheduled whose time has come still prices", func(o *Offer) { o.Status = StatusScheduled }, ChannelOnline, 1000},
		{"paused", func(o *Offer) { o.Status = StatusPaused }, ChannelOnline, 0},
		{"ended", func(o *Offer) { o.Status = StatusEnded }, ChannelOnline, 0},
		{"online only, at the pos", func(o *Offer) { o.Channels = ChannelsOnline }, ChannelPOS, 0},
		{"online only, online", func(o *Offer) { o.Channels = ChannelsOnline }, ChannelOnline, 1000},
		{"pos only, online", func(o *Offer) { o.Channels = ChannelsPOS }, ChannelOnline, 0},
		{"pos only, pos", func(o *Offer) { o.Channels = ChannelsPOS }, ChannelPOS, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := pct(30, 10)
			tc.mod(&o)
			if got := price(t, cart(tc.ch), o).DiscountPaise(); got != tc.want {
				t.Errorf("discount = %d, want %d", got, tc.want)
			}
		})
	}
}

// The same cart gets the same price in a quote and at the POS (SC-OF same
// price in quote and POS).
func TestQuoteAndPOSAgree(t *testing.T) {
	offers := []Offer{pct(30, 12, milk), flat(31, 700, rice), bxgy(32, 3, 1, soap), bill(33, 4000, 20000)}
	lines := []Line{line(milk, 4999, 3), line(rice, 6500, 2), line(soap, 3333, 5)}
	online := price(t, Cart{Channel: ChannelOnline, Lines: lines}, offers...)
	pos := price(t, Cart{Channel: ChannelPOS, Lines: lines}, offers...)
	if online.TotalPaise != pos.TotalPaise || online.DiscountPaise() != pos.DiscountPaise() {
		t.Errorf("online %+v differs from pos %+v", online, pos)
	}
}

func TestRedemptionLimits(t *testing.T) {
	clk := clock.NewFake(t0)
	ctx := context.Background()
	o := pct(30, 10)
	o.MaxRedemptions = 2
	f := NewFake(clk).Add(o)
	cart := func(c ids.ID) Cart {
		return Cart{CustomerID: c, Channel: ChannelOnline, Lines: []Line{line(milk, 10000, 1)}}
	}
	redeem := func(n int, c ids.ID) error {
		q, _ := f.Price(ctx, cart(c))
		return f.Redeem(ctx, Ref{OrderID: id(100 + n)}, c, q)
	}
	if err := redeem(1, cust); err != nil {
		t.Fatal(err)
	}
	if err := redeem(2, other); err != nil {
		t.Fatal(err)
	}
	if q, _ := f.Price(ctx, cart(cust)); q.DiscountPaise() != 0 {
		t.Errorf("after max_redemptions the offer still applies: %d", q.DiscountPaise())
	}
	// A quote taken before the last redemption lost the race.
	stale := price(t, cart(cust), pct(30, 10))
	if err := f.Redeem(ctx, Ref{OrderID: id(103)}, cust, stale); !errors.Is(err, ErrUnavailable) {
		t.Errorf("redeeming a spent offer = %v, want ErrUnavailable", err)
	}
	// Redeeming the same ref twice counts once; releasing gives the use back.
	if err := f.Release(ctx, Ref{OrderID: id(101)}); err != nil {
		t.Fatal(err)
	}
	if err := f.Release(ctx, Ref{OrderID: id(101)}); err != nil {
		t.Fatal(err)
	}
	if q, _ := f.Price(ctx, cart(cust)); q.DiscountPaise() != 1000 {
		t.Errorf("after a release the offer applies again: %d", q.DiscountPaise())
	}
}

func TestPerCustomerLimit(t *testing.T) {
	ctx := context.Background()
	o := pct(30, 10)
	o.PerCustomerLimit = 1
	f := NewFake(clock.NewFake(t0)).Add(o)
	cart := func(c ids.ID) Cart {
		return Cart{CustomerID: c, Channel: ChannelOnline, Lines: []Line{line(milk, 10000, 1)}}
	}

	if q, _ := f.Price(ctx, cart(ids.Zero)); q.DiscountPaise() != 0 {
		t.Errorf("a limited offer applied to nobody: %d", q.DiscountPaise())
	}
	q, _ := f.Price(ctx, cart(cust))
	if q.DiscountPaise() != 1000 {
		t.Fatalf("first use = %d, want 1000", q.DiscountPaise())
	}
	if err := f.Redeem(ctx, Ref{OrderID: id(101)}, cust, q); err != nil {
		t.Fatal(err)
	}
	if q, _ := f.Price(ctx, cart(cust)); q.DiscountPaise() != 0 {
		t.Errorf("second use by the same customer = %d, want 0", q.DiscountPaise())
	}
	if q, _ := f.Price(ctx, cart(other)); q.DiscountPaise() != 1000 {
		t.Errorf("another customer = %d, want 1000", q.DiscountPaise())
	}
	if err := f.Release(ctx, Ref{OrderID: id(101)}); err != nil {
		t.Fatal(err)
	}
	if q, _ := f.Price(ctx, cart(cust)); q.DiscountPaise() != 1000 {
		t.Errorf("after the release the customer may use it again: %d", q.DiscountPaise())
	}
}

// Whatever the offers, the bill never drops under ₹1 and the parts add up.
func TestPriceInvariants(t *testing.T) {
	offers := []Offer{pct(30, 90), flat(31, 99999), bxgy(32, 1, 3, milk, rice), bill(33, 1<<40, 0), bill(34, 500, 100)}
	for _, price1 := range []int64{1, 50, 99, 100, 101, 199, 1000, 123457} {
		for qty := 1; qty <= 9; qty++ {
			q := price(t, Cart{Lines: []Line{line(milk, price1, qty), line(rice, price1+7, qty)}}, offers...)
			if q.GrossPaise >= 100 && q.TotalPaise < 100 {
				t.Fatalf("price %d qty %d: total %d under ₹1", price1, qty, q.TotalPaise)
			}
			if q.TotalPaise < 0 || q.GrossPaise-q.DiscountPaise() != q.TotalPaise {
				t.Fatalf("price %d qty %d: %+v does not add up", price1, qty, q)
			}
			var sum int64
			for _, l := range q.Lines {
				if l.DiscountPaise < 0 || l.DiscountPaise > l.GrossPaise {
					t.Fatalf("price %d qty %d: line %+v out of range", price1, qty, l)
				}
				sum += l.DiscountPaise
			}
			if sum != q.ItemDiscountPaise {
				t.Fatalf("price %d qty %d: lines %d, item discount %d", price1, qty, sum, q.ItemDiscountPaise)
			}
		}
	}
}

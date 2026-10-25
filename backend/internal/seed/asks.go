package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/geo/h3"
	"github.com/todayz/backend/internal/platform/ids"
)

// seedasks writes the Ask shops nearby, Demand gaps and site strip fixtures
// (W3-16; 08 ASK, e2e customer/e2e/asks.spec.ts, business/e2e/asks.spec.ts,
// admin/e2e/demand.spec.ts) in every profile, at the QA point in Bengaluru:
//
//   - Asha's open ask for Toor dal 1 kg (×2, today): Sri Lakshmi and Sri
//     Lakshmi 2 have not replied yet (B-20 inbox), Annapurna said No.
//   - Ravi's ask for Amul Butter 100 g: Sri Lakshmi said Yes at ₹58, ready in
//     20 minutes, so C-26 shows a reply to book; Sri Lakshmi 2 has not replied.
//   - Asha's expired ask for "brass diya": nobody replied in the window.
//   - Nandi Hardware has asks muted, so no ask reaches it.
//   - The new customer's last asks (ask_low_yield_after_count of them) all got
//     no Yes, so their next ask is narrowed (low-yield).
//   - Demand gaps: ten shoppers asked for Kodo millet 1 kg with no Yes (shows
//     in A-23), nine for Foxtail millet 500 g (below the floor of ten, hidden).
//   - Site strip: yesterday's approved build (shown on todayz.in) and today's
//     proposed build waiting in A-23.
//
// The shoppers are bare users (no phone, no name), so nobody signs in as one.
// A booked ask needs an order, which only the orders service writes (it seals
// the order code); the e2e journey books Ravi's reply through the API.
//
// Fixed ids (prefixes MA asks, MC recipients, MD shoppers, ME strip rows), the
// seed clock, and ON CONFLICT DO NOTHING, so a second run changes nothing.
// Without PII_ENC_KEY and PII_HASH_KEY identity seeds no users, so there are no
// asks either.

const (
	askIDPrefix       = "01KE0S1W6W8ZV7T1C3A6MA"
	askRecipientIDPfx = "01KE0S1W6W8ZV7T1C3A6MC"
	askShopperIDPfx   = "01KE0S1W6W8ZV7T1C3A6MD"
	marqueeIDPrefix   = "01KE0S1W6W8ZV7T1C3A6ME"
)

// askReply is one store's row on a seeded ask; Answer empty is no reply yet.
type askReply struct {
	Store     string
	DistanceM int32
	Answer    string
	Price     int64  // paise, Yes only
	Item      string // store item key when the store stocks it
	ReadyIn   time.Duration
	RepliedAt time.Duration // before now
}

type seedAsk struct {
	Customer string // identityUserID key; empty: Shopper
	Shopper  int
	Product  string // fixture product name; empty: free text
	Text     string
	Qty      int32
	NeedBy   string
	Ago      time.Duration
	Closed   string // close reason; empty: open
	Replies  []askReply
}

func askBand(m int32) string {
	switch {
	case m < 1000: //settings:ok distance band edge, 01 §11.1
		return "lt_1km"
	case m < 3000: //settings:ok distance band edge, 01 §11.1
		return "1_3km"
	case m < 5000: //settings:ok distance band edge, 01 §11.1
		return "3_5km"
	}
	return "gt_5km"
}

func seedasks(ctx context.Context, d Deps, _ Profile) error {
	if !d.Keys.PIIEncKey.IsSet() || !d.Keys.PIIHashKey.IsSet() {
		return nil
	}
	q := d.DB.Q(ctx)
	now := d.Clock.Now().UTC()

	var window, lowAfter, priceValid int
	for key, dst := range map[string]*int{
		"ask_reply_window_min": &window, "ask_low_yield_after_count": &lowAfter, "ask_price_valid_min": &priceValid,
	} {
		if err := q.QueryRow(ctx, `SELECT (value #>> '{}')::int FROM platform_settings WHERE key = $1`, key).Scan(dst); err != nil {
			return fmt.Errorf("seed asks: setting %s: %w", key, err)
		}
	}
	reply := time.Duration(window) * time.Minute

	cell, err := h3.LatLngToCell(QAPoint.Lat, QAPoint.Lng, 8)
	if err != nil {
		return err
	}

	if _, err := q.Exec(ctx, `UPDATE stores SET asks_muted = true WHERE id = $1 AND NOT asks_muted`,
		ids.UUID(storeSeedID("NH"))); err != nil {
		return err
	}

	for i := 1; i <= 10; i++ {
		if _, err := q.Exec(ctx, `INSERT INTO users (id, created_at, updated_at) VALUES ($1, $2, $2)
			ON CONFLICT (id) DO NOTHING`, ids.UUID(seqID(askShopperIDPfx, i)), now.Add(-30*day)); err != nil {
			return err
		}
	}

	asks := []seedAsk{
		{Customer: "customer.asha", Product: "Toor dal 1 kg", Qty: 2, NeedBy: "today", Ago: 4 * time.Minute, Replies: []askReply{
			{Store: "SL", DistanceM: 500, Item: "I7"}, {Store: "SL2", DistanceM: 2000},
			{Store: "AP", DistanceM: 1000, Answer: "no", RepliedAt: 2 * time.Minute},
		}},
		{Customer: "customer.ravi", Product: "Amul Butter 100 g", Qty: 1, NeedBy: "now", Ago: 10 * time.Minute, Replies: []askReply{
			{Store: "SL", DistanceM: 500, Answer: "yes", Price: 5800, Item: "I1", ReadyIn: 20 * time.Minute, RepliedAt: 6 * time.Minute},
			{Store: "SL2", DistanceM: 2000},
		}},
		{Customer: "customer.asha", Text: "brass diya", Qty: 1, Ago: 3 * time.Hour, Closed: "window_ended", Replies: []askReply{
			{Store: "SL", DistanceM: 500}, {Store: "AP", DistanceM: 1000},
		}},
	}
	for i := range lowAfter {
		asks = append(asks, seedAsk{Customer: "customer.new", Text: fmt.Sprintf("qa low yield %02d", i+1), Qty: 1,
			Ago: time.Duration(i+1) * 6 * time.Hour, Closed: "window_ended",
			Replies: []askReply{{Store: "SL2", DistanceM: 2000}}})
	}
	for i := 1; i <= 10; i++ {
		asks = append(asks, seedAsk{Shopper: i, Product: "Kodo millet 1 kg", Qty: 1, Ago: time.Duration(i) * 7 * time.Hour,
			Closed: "window_ended", Replies: []askReply{
				{Store: "SL", DistanceM: 500, Answer: "no", RepliedAt: time.Duration(i)*7*time.Hour - 5*time.Minute},
				{Store: "SL2", DistanceM: 2000},
			}})
		if i <= 9 {
			asks = append(asks, seedAsk{Shopper: i, Product: "Foxtail millet 500 g", Qty: 1, Ago: time.Duration(i)*7*time.Hour + time.Hour,
				Closed: "window_ended", Replies: []askReply{{Store: "SL2", DistanceM: 2000}}})
		}
	}

	r := 0
	for n, a := range asks {
		if err := insertSeedAsk(ctx, d, n+1, &r, a, now, reply, time.Duration(priceValid)*time.Minute, cell.Int64()); err != nil {
			return err
		}
	}
	return seedMarquee(ctx, d, now)
}

func insertSeedAsk(ctx context.Context, d Deps, n int, r *int, a seedAsk, now time.Time, reply, priceValid time.Duration, cell int64) error {
	q := d.DB.Q(ctx)
	at := now.Add(-a.Ago)
	customer := seqID(askShopperIDPfx, a.Shopper)
	if a.Customer != "" {
		customer = identityUserID(a.Customer)
	}
	var product, needBy, closedAt, reason any
	text := a.Text
	if a.Product != "" {
		product = ids.UUID(productSeedID(a.Product))
		text = lowerASCII(a.Product)
	}
	if a.NeedBy != "" {
		needBy = a.NeedBy
	}
	status := "open"
	if a.Closed != "" {
		status, reason, closedAt = "closed", a.Closed, at.Add(reply)
	}
	id := ids.UUID(seqID(askIDPrefix, n))
	if _, err := q.Exec(ctx, `INSERT INTO asks (id, customer_id, product_id, query_norm, quantity, need_by, geo_cell,
			city_place_id, radius_m, recipients_count, status, close_reason, created_at, closed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, md5('todayz:place:city:Bengaluru')::uuid,
			(SELECT (value #>> '{}')::int FROM platform_settings WHERE key = 'ask_radius_m'), $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO NOTHING`,
		id, ids.UUID(customer), product, text, a.Qty, needBy, cell, len(a.Replies), status, reason, at, closedAt); err != nil {
		return fmt.Errorf("seed asks: ask %d: %w", n, err)
	}
	for _, rep := range a.Replies {
		*r++
		var answer, price, item, readyBy, repliedAt, repliedBy, validUntil any
		if rep.Item != "" {
			item = ids.UUID(storeItemSeedID(rep.Item))
		}
		if rep.Answer != "" {
			t := now.Add(-rep.RepliedAt)
			answer, repliedAt = rep.Answer, t
			repliedBy = ids.UUID(identityUserID("staff.owner"))
			if rep.Answer == "yes" {
				price = rep.Price
				readyBy, validUntil = t.Add(rep.ReadyIn), t.Add(priceValid)
			}
		}
		if _, err := q.Exec(ctx, `INSERT INTO ask_recipients (id, ask_id, store_id, sent_at, distance_m, distance_band, reply_by,
				answer, price_paise, store_item_id, ready_by, replied_at, replied_by, price_valid_until)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) ON CONFLICT (id) DO NOTHING`,
			ids.UUID(seqID(askRecipientIDPfx, *r)), id, ids.UUID(storeSeedID(rep.Store)), at, rep.DistanceM, askBand(rep.DistanceM),
			at.Add(reply), answer, price, item, readyBy, repliedAt, repliedBy, validUntil); err != nil {
			return fmt.Errorf("seed asks: recipient %d: %w", *r, err)
		}
	}
	return nil
}

// seedMarquee writes Bengaluru's strip: yesterday's build approved, today's
// proposed. A strip row has no admin to name, so decided_by stays empty.
func seedMarquee(ctx context.Context, d Deps, now time.Time) error {
	q := d.DB.Q(ctx)
	today := clock.StartOfDayIST(now).In(clock.IST)
	builds := []struct {
		On       time.Time
		Status   string
		Products []string
	}{
		{today.AddDate(0, 0, -1), "approved", []string{"Toor dal 1 kg", "Sunflower oil 1 L", "Wheat atta 5 kg", "Amul Butter 100 g"}},
		{today, "proposed", []string{"Toor dal 1 kg", "Kodo millet 1 kg", "Sunflower oil 1 L", "Eggs tray 12", "Filter coffee powder 200 g"}},
	}
	n := 0
	for _, b := range builds {
		var decidedAt any
		if b.Status != "proposed" {
			decidedAt = now.Add(-20 * time.Hour)
		}
		for rank, p := range b.Products {
			n++
			searchers := 24 - 3*rank
			if _, err := q.Exec(ctx, `INSERT INTO site_demand_marquee (id, city_place_id, product_id, searchers, searches_total,
					rank, built_on, status, decided_at)
				VALUES ($1, md5('todayz:place:city:Bengaluru')::uuid, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING`,
				ids.UUID(seqID(marqueeIDPrefix, n)), ids.UUID(productSeedID(p)), searchers, searchers*2, rank+1,
				b.On.Format(time.DateOnly), b.Status, decidedAt); err != nil {
				return fmt.Errorf("seed asks: strip row %d: %w", n, err)
			}
		}
	}
	return nil
}

// lowerASCII is the stored query of a fixture product name (search.Normalize
// lower-cases; the fixture names are ASCII).
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

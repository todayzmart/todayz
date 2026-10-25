package seed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/rollups"
)

// Seeded insights ids: prefix + a fixed number (seqID).
const (
	posSaleIDPrefix     = "01KE0S1W6W8ZV7T1C3A6MX"
	posSaleLineIDPrefix = "01KE0S1W6W8ZV7T1C3A6MY"
	posDeviceIDPrefix   = "01KE0S1W6W8ZV7T1C3A6MZ"
)

// Search cells (D-81). These stay fixed placeholder cell numbers (not H3)
// with the centre the book gives each, so its distances hold exactly: W 1.2 km
// from both SL and SL2, S2 0.2 km from SL2 and 2.6 km from SL, M 0.5 km from
// the Chennai shop. Real searches log a real H3 cell (platform/geo/h3) and
// rollup_demand gives insight_geo_cells its centre.
var insightCells = []struct {
	Cell int64
	At   LatLng
}{
	{Cell: 0x0881_0000_0000_0001, At: QAPoint.offset(180, 0.6)},     // W
	{Cell: 0x0881_0000_0000_0002, At: QAPoint.offset(180, 2.0)},     // S2
	{Cell: 0x0881_0000_0000_0003, At: chennaiPoint.offset(90, 0.5)}, // M
}

// insightSearches are the nearby searches of 08 §2: cell, product, then the
// searches on each day before today (index 0 is yesterday).
var insightSearches = []struct {
	Cell    int
	Product string
	PerDay  []int32
}{
	{0, "Horlicks 500 g", []int32{2, 2, 2, 2, 2, 2}},    // 12 in W: a row at SL and SL2
	{0, "Bournvita 500 g", []int32{2, 2, 2, 1, 1, 1}},   // 9 in W: under the 10-search floor
	{1, "Boost 500 g", []int32{2, 2, 2, 2, 2, 1}},       // 11 in S2: SL2 only; AP stocks it
	{1, "Idli rice 1 kg", []int32{9, 9, 2, 2, 2}},       // 24 in S2, 18 on SL2's two days at 0
	{2, "Horlicks 500 g", []int32{2, 2, 2, 2, 2, 2, 2}}, // 14 in M: "No shop nearby has it."
}

// insightRegulars are the reserved test phones (+91 90000 000xx) of SL2's 11
// phone-linked customers; the manual book's own numbers stop at 00062.
var insightRegulars = []string{
	"+919000000063", "+919000000064", "+919000000065", "+919000000066", "+919000000067", "+919000000068",
	"+919000000069", "+919000000071", "+919000000072", "+919000000073", "+919000000074",
}

type seedLine struct {
	Key string // I1–I14, J1–J22
	Qty int32
}

type seedBill struct {
	Store string
	Ago   int // IST days before today; 0 is today
	At    time.Time
	Lines []seedLine
	Phone int // index into insightRegulars, -1 for a walk-in
}

// insightBills builds the bill history of <seed:insights-SL> and
// <seed:insights-SL2> (08 §2). SL2 has 10 bills a day (j 0–9) on each of the
// 60 days before today, so the pair rollup sees 600 bills:
//
//	milk J20        j 0–7, every day                 480 bills, 56 in 7 days
//	Parle-G J21     j 4–8, every day                 300 bills, 240 with milk (lift 1.0)
//	Bread J18       j 9 days 8–60, j 0 days 8–38     84 bills
//	Eggs J19        j 9 days 8–28, j 8 days 8–37,    70 bills, 21 with Bread (lift 2.14)
//	                j 1 days 8–26
//	Tata Tea J1     4 a bill: j 8–9 days 1–7, j 0 days 1–2   64 units in 7 days
//
// The other in-stock items sell once on day 8 so only Mango pickle (day 23)
// and Agarbatti (day 40) sit on the shelf; Idli rice is sold through the
// stock writer (see insightIdliRice). Customers: 8 regulars with visits on
// days 22, 36 and 50, one with 3 recent visits, one with 2 and one with 1.
// SL: Amul Butter sells 8 a day for 7 days and Toor dal last sold 26 days ago.
func insightBills(today time.Time, now time.Time) []seedBill {
	at := func(ago, j int) time.Time {
		h, m := 8+(j*13+ago*3)%14, (j*7+ago)%60
		return today.AddDate(0, 0, -ago).Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	}
	regular := map[[2]int]int{}
	for c := range 8 {
		for _, ago := range []int{22, 36, 50} {
			regular[[2]int{ago, c}] = c
		}
	}
	for _, ago := range []int{3, 9, 15} {
		regular[[2]int{ago, 0}] = 8
	}
	for _, ago := range []int{5, 13} {
		regular[[2]int{ago, 1}] = 9
	}
	regular[[2]int{12, 2}] = 10

	var out []seedBill
	for ago := 60; ago >= 1; ago-- {
		for j := range 10 {
			var ls []seedLine
			add := func(key string, qty int32) { ls = append(ls, seedLine{key, qty}) }
			if j <= 7 {
				add("J20", 1)
			}
			if j >= 4 && j <= 8 {
				add("J21", 1)
			}
			if (j == 9 && ago >= 8) || (j == 0 && ago >= 8 && ago <= 38) {
				add("J18", 1)
			}
			if (j == 9 && ago >= 8 && ago <= 28) || (j == 8 && ago >= 8 && ago <= 37) || (j == 1 && ago >= 8 && ago <= 26) {
				add("J19", 1)
			}
			if ago <= 7 && (j >= 8 || (j == 0 && ago <= 2)) {
				add("J1", 4)
			}
			if j == 9 {
				switch ago {
				case 8:
					for _, k := range []string{"J2", "J3", "J4", "J5", "J6", "J7", "J10", "J12", "J13", "J14", "J15", "J16", "J17", "J22"} {
						add(k, 1)
					}
				case 23:
					add("J8", 1)
				case 40:
					add("J9", 1)
				}
			}
			phone, ok := regular[[2]int{ago, j}]
			if !ok {
				phone = -1
			}
			out = append(out, seedBill{Store: "SL2", Ago: ago, At: at(ago, j), Lines: ls, Phone: phone})
		}
	}
	for ago := 7; ago >= 1; ago-- {
		for j := range 8 {
			out = append(out, seedBill{Store: "SL", Ago: ago, At: at(ago, j), Lines: []seedLine{{"I1", 1}}, Phone: -1})
		}
	}
	keep := []seedLine{}
	for _, k := range []string{"I2", "I3", "I4", "I5", "I6", "I8", "I9", "I10", "I11", "I12", "I13", "I14"} {
		keep = append(keep, seedLine{k, 1})
	}
	out = append(out,
		seedBill{Store: "SL", Ago: 8, At: at(8, 0), Lines: keep, Phone: -1},
		seedBill{Store: "SL", Ago: 26, At: at(26, 0), Lines: []seedLine{{"I7", 1}}, Phone: -1})
	// Today's walk-ins before the clock time, for the overview's KPIs.
	for _, w := range []struct {
		Store string
		Min   int
		Lines []seedLine
	}{
		{"SL2", 8*60 + 20, []seedLine{{"J20", 2}, {"J21", 1}}},
		{"SL2", 8*60 + 55, []seedLine{{"J20", 1}}},
		{"SL2", 9*60 + 35, []seedLine{{"J21", 2}}},
		{"SL", 8*60 + 40, []seedLine{{"I2", 2}}},
		{"SL", 9*60 + 25, []seedLine{{"I3", 1}}},
	} {
		t := today.Add(time.Duration(w.Min) * time.Minute)
		if !t.Before(now) {
			continue
		}
		out = append(out, seedBill{Store: w.Store, Ago: 0, At: t, Lines: w.Lines, Phone: -1})
	}
	return out
}

// insightIdliRice is SL2's Idli rice (J11): 4 in as opening stock 7 days ago,
// then one sold a day 6, 5, 4 and 2 days ago, so it closes at 0 on the last
// two whole days. It goes through the stock writer so the closing stock the
// missed-sales kind rebuilds from the movements is real.
var insightIdliRice = struct {
	InAgo    int
	SoldAgo  []int
	BillSlot int // the SL2 bill (j) of each sale day the line joins
}{InAgo: 7, SoldAgo: []int{6, 5, 4, 2}, BillSlot: 3}

// seedinsights writes <seed:insights-SL> and <seed:insights-SL2> (08 §2, D-99):
// the POS bill history, the phone-linked regulars, Idli rice's stock-out, the
// nearby searches in the placeholder cells, then W3-07's own pair rollup over
// it. Sales lines do not move stock (the catalogue seed's on-hand is what the
// book reads), except Idli rice's. A second run changes nothing. Without
// PII_ENC_KEY and PII_HASH_KEY there are no users to ring bills up, so it warns
// and seeds nothing.
func seedinsights(ctx context.Context, d Deps, _ Profile) error {
	if !d.Keys.PIIEncKey.IsSet() || !d.Keys.PIIHashKey.IsSet() {
		d.Log.WarnContext(ctx, "seed insights: PII_ENC_KEY or PII_HASH_KEY unset; no insights rows seeded (run make setup)")
		return nil
	}
	q := d.DB.Q(ctx)
	var one int
	switch err := q.QueryRow(ctx, `SELECT 1 FROM pos_sales WHERE id = $1`, ids.UUID(seqID(posSaleIDPrefix, 1))).Scan(&one); {
	case err == nil:
		return nil // seeded by an earlier run
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("insights seeded: %w", err)
	}
	seal, err := pii.NewSealer(d.Keys.PIIEncKey.Bytes())
	if err != nil {
		return fmt.Errorf("PII_ENC_KEY: %w", err)
	}
	hash, err := pii.NewHasher(d.Keys.PIIHashKey.Bytes())
	if err != nil {
		return fmt.Errorf("PII_HASH_KEY: %w", err)
	}
	now := d.Clock.Now().UTC()
	today := clock.StartOfDayIST(now)

	if err := seedInsightSearches(ctx, d, today); err != nil {
		return err
	}
	bills := insightBills(today, now)
	if err := seedInsightBills(ctx, d, seal, hash, bills); err != nil {
		return err
	}
	if err := seedInsightIdliRice(ctx, d, today, bills); err != nil {
		return err
	}

	r := rollups.New(rollups.Deps{DB: d.DB, Clock: d.Clock,
		Settings: settings.NewCache(settings.PGLoader(d.DB.Pool), settings.Options{Log: d.Log})})
	pairs, err := r.ComputeItemPairs(ctx, rollups.DayOf(now))
	if err != nil {
		return err
	}
	d.Log.DebugContext(ctx, "seed insights", slog.Int("bills", len(bills)), slog.Int64("pairs", pairs))
	return nil
}

func seedInsightSearches(ctx context.Context, d Deps, today time.Time) error {
	q := d.DB.Q(ctx)
	for _, c := range insightCells {
		if _, err := q.Exec(ctx, `INSERT INTO insight_geo_cells (geo_cell, centre)
			VALUES ($1, ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography) ON CONFLICT DO NOTHING`,
			c.Cell, c.At.Lng, c.At.Lat); err != nil {
			return fmt.Errorf("insight cell: %w", err)
		}
	}
	for _, s := range insightSearches {
		pid := productSeedID(s.Product)
		for i, n := range s.PerDay {
			day := pgtype.Date{Time: catalogue.Today(today.AddDate(0, 0, -(i + 1))), Valid: true}
			if _, err := q.Exec(ctx, `INSERT INTO rollup_daily_search_demand (date, geo_cell, query_norm, product_id, searches, zero_results)
				VALUES ($1, $2, lower($3), $4, $5, 0) ON CONFLICT DO NOTHING`,
				day, insightCells[s.Cell].Cell, s.Product, ids.UUID(pid), n); err != nil {
				return fmt.Errorf("search demand %s: %w", s.Product, err)
			}
		}
	}
	return nil
}

// seedInsightBills writes the bills as completed cash or UPI sales rung up by
// staff.owner on one fixed device, with no discounts.
func seedInsightBills(ctx context.Context, d Deps, seal *pii.Sealer, hash *pii.Hasher, bills []seedBill) error {
	q := d.DB.Q(ctx)
	prices := map[string]int64{}
	names := map[string]string{}
	for _, s := range seedStoreItems {
		for _, it := range s.Items {
			if it.Key != "" {
				prices[it.Key], names[it.Key] = it.Price, it.Product
			}
		}
	}
	type phone struct{ hash, enc []byte }
	phones := make([]phone, len(insightRegulars))
	for i, p := range insightRegulars {
		enc, err := seal.Seal("phone", []byte(p))
		if err != nil {
			return fmt.Errorf("seal: %w", err)
		}
		phones[i] = phone{hash.Phone(p), enc}
	}
	cashier := ids.UUID(identityUserID("staff.owner"))
	device := ids.UUID(seqID(posDeviceIDPrefix, 1))
	line := 0
	for i, b := range bills {
		saleID := ids.UUID(seqID(posSaleIDPrefix, i+1))
		var total int64
		for _, l := range b.Lines {
			total += prices[l.Key] * int64(l.Qty)
		}
		pay := "cash"
		if i%3 == 0 {
			pay = "upi"
		}
		var method, status *string
		var ph, enc []byte
		if b.Phone >= 0 {
			m, s := "phone_unverified", "pending_verification"
			method, status = &m, &s
			ph, enc = phones[b.Phone].hash, phones[b.Phone].enc
		}
		if _, err := q.Exec(ctx, `INSERT INTO pos_sales (id, store_id, client_id, device_id, cashier_user_id,
				link_method, link_status, link_phone_hash, link_phone_enc,
				item_subtotal_paise, subtotal_paise, total_paise, payment_method, status, sold_at, received_at, created_at, updated_at)
			VALUES ($1, $2, $1, $3, $4, $5, $6, $7, $8, $9, $9, $9, $10, 'completed', $11, $11, $11, $11)
			ON CONFLICT DO NOTHING`,
			saleID, ids.UUID(storeSeedID(b.Store)), device, cashier, method, status, ph, enc, total, pay, b.At); err != nil {
			return fmt.Errorf("pos sale %d: %w", i+1, err)
		}
		for _, l := range b.Lines {
			line++
			id := ids.UUID(seqID(posSaleLineIDPrefix, line))
			p := prices[l.Key]
			if _, err := q.Exec(ctx, `INSERT INTO pos_sale_lines (id, sale_id, client_id, store_item_id, name_snapshot,
					unit_price_paise, quantity, line_total_paise, created_at)
				VALUES ($1, $2, $1, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING`,
				id, saleID, ids.UUID(storeItemSeedID(l.Key)), names[l.Key], p, l.Qty, p*int64(l.Qty), b.At); err != nil {
				return fmt.Errorf("pos line %s: %w", l.Key, err)
			}
		}
	}
	return nil
}

// seedInsightIdliRice writes Idli rice's opening stock and its four sales
// through the stock writer, each sale a line on that day's SL2 bill j=3 with
// the sale's movement.
func seedInsightIdliRice(ctx context.Context, d Deps, today time.Time, bills []seedBill) error {
	q := d.DB.Q(ctx)
	r := insightIdliRice
	store, item := storeSeedID("SL2"), storeItemSeedID("J11")
	fake := clock.NewFake(today.AddDate(0, 0, -r.InAgo).Add(8 * time.Hour))
	w := catalogue.NewWriterWithIDs(fake, seedIDs())
	qty := int32(len(r.SoldAgo)) // #nosec G115 -- four sales
	if _, err := w.AddStock(ctx, q, catalogue.StockIn{StoreID: store, ItemID: item, Qty: qty, Source: catalogue.SourceOpening}); err != nil {
		return fmt.Errorf("idli rice stock: %w", err)
	}
	var price int64
	for _, s := range seedStoreItems {
		for _, it := range s.Items {
			if it.Key == "J11" {
				price = it.Price
			}
		}
	}
	for n, ago := range r.SoldAgo {
		bill := -1
		for i, b := range bills {
			if b.Store == "SL2" && b.Ago == ago {
				bill = i + r.BillSlot // SL2 writes its 10 bills of a day in slot order
				break
			}
		}
		if bill < 0 {
			return fmt.Errorf("idli rice: no SL2 bill %d days ago", ago)
		}
		saleID := seqID(posSaleIDPrefix, bill+1)
		fake.Set(bills[bill].At)
		mv, err := w.Sell(ctx, q, catalogue.Sale{StoreID: store, ItemID: item, Qty: 1, Source: catalogue.SourceSalePOS,
			RefType: "pos_sale", RefID: saleID})
		if err != nil {
			return fmt.Errorf("idli rice sale: %w", err)
		}
		id := ids.UUID(seqID(posSaleLineIDPrefix, 9000+n+1))
		if _, err := q.Exec(ctx, `INSERT INTO pos_sale_lines (id, sale_id, client_id, store_item_id, name_snapshot,
				unit_price_paise, quantity, line_total_paise, movement_id, created_at)
			VALUES ($1, $2, $1, $3, 'Idli rice 1 kg', $4, 1, $4, $5, $6) ON CONFLICT DO NOTHING`,
			id, ids.UUID(saleID), ids.UUID(item), price, ids.UUID(mv.ID), bills[bill].At); err != nil {
			return fmt.Errorf("idli rice line: %w", err)
		}
		if _, err := q.Exec(ctx, `UPDATE pos_sales SET item_subtotal_paise = item_subtotal_paise + $2,
				subtotal_paise = subtotal_paise + $2, total_paise = total_paise + $2 WHERE id = $1`,
			ids.UUID(saleID), price); err != nil {
			return fmt.Errorf("idli rice total: %w", err)
		}
	}
	return nil
}

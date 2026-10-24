package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/internal/platform/ids"
)

// Seeded offer ids: prefix + a fixed number (seqID).
const offerIDPrefix = "01KE0S1W6W8ZV7T1C3A6MV"

// seedoffers writes the offers the manual book names (docs/qa/manual/README.md
// §7, OFFER-offers-banners.md): <seed:offer-I13-21off>, a ₹21 flat offer on I13
// at SL, live from the seed clock with no end. The demo profile adds a few more
// shapes so the shop list, the nearby feed and the customer cards have something
// to show. Ids are fixed and times come from the seed clock; a second run
// changes nothing (ON CONFLICT DO NOTHING).
func seedoffers(ctx context.Context, d Deps, p Profile) error {
	now := d.Clock.Now().UTC()
	q := d.DB.Q(ctx)
	insert := func(n int, store string, kind string, value int64, buy, get *int, minBill *int64, scope string, items []ids.ID, title string, ends *time.Time) error {
		_, err := q.Exec(ctx, `INSERT INTO offers (id, store_id, title, kind, value, buy_qty, get_qty, min_bill_paise, scope, store_item_ids,
		    channels, starts_at, ends_at, status, source, created_at, updated_at)
		  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::uuid[], 'both', $11, $12, 'live', 'manual', $11, $11)
		  ON CONFLICT DO NOTHING`,
			ids.UUID(seqID(offerIDPrefix, n)), ids.UUID(storeSeedID(store)), title, kind, value, buy, get, minBill, scope,
			uuidSlice(items), now, ends)
		if err != nil {
			return fmt.Errorf("offer %d at %s: %w", n, store, err)
		}
		return nil
	}
	// <seed:offer-I13-21off>
	if err := insert(1, "SL", "flat", 2100, nil, nil, nil, "items", []ids.ID{storeItemSeedID("I13")}, "₹21 off", nil); err != nil {
		return err
	}
	if p != Demo {
		return nil
	}
	week := now.Add(7 * 24 * time.Hour)
	two, one := 2, 1
	bill := int64(50000)
	for _, o := range []struct {
		n       int
		kind    string
		value   int64
		buy     *int
		get     *int
		minBill *int64
		scope   string
		items   []ids.ID
		title   string
		ends    *time.Time
	}{
		{2, "percent", 10, nil, nil, nil, "items", []ids.ID{storeItemSeedID("I1")}, "10% off butter", &week},
		{3, "bxgy", 0, &two, &one, nil, "items", []ids.ID{storeItemSeedID("I2")}, "Buy 2 get 1", nil},
		{4, "bill_min", 5000, nil, nil, &bill, "store", nil, "₹50 off ₹500+", &week},
	} {
		if err := insert(o.n, "SL", o.kind, o.value, o.buy, o.get, o.minBill, o.scope, o.items, o.title, o.ends); err != nil {
			return err
		}
	}
	return nil
}

// uuidSlice is ids as the uuid[] the offers table takes (never nil).
func uuidSlice(in []ids.ID) []pgtype.UUID {
	out := make([]pgtype.UUID, len(in))
	for i, id := range in {
		out[i] = ids.UUID(id)
	}
	return out
}

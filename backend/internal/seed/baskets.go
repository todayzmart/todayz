package seed

import (
	"context"
	"log/slog"
	"time"

	"github.com/todayz/backend/internal/platform/ids"
)

// seedbaskets writes Asha's saved baskets (W2-14; 08 BSK, e2e
// customer/e2e/basket.stack.spec.ts) in every profile, at the QA point:
//
//   - "Weekly groceries": Sri Lakshmi has every line (Has everything).
//   - "Sunday breakfast": no store has everything; Sri Lakshmi has the butter
//     and the noodles, Sri Lakshmi 2 the bread and the eggs, so search
//     suggests that split. The noodles line is free text, stored normalised.
//
// Fixed ids (prefixes MK baskets, MN lines), the seed clock, and ON CONFLICT
// DO NOTHING, so a second run changes nothing. Without PII_ENC_KEY and
// PII_HASH_KEY identity seeds no users, so there are no baskets either.

const (
	basketIDPrefix     = "01KE0S1W6W8ZV7T1C3A6MK"
	basketLineIDPrefix = "01KE0S1W6W8ZV7T1C3A6MN"
)

type seedBasketLine struct {
	Product string // fixture product name; empty for free text
	Text    string // normalised as search.Normalize would
	Qty     int32
}

var seedBaskets = []struct {
	Name  string
	Lines []seedBasketLine
}{
	{Name: "Weekly groceries", Lines: []seedBasketLine{
		{Product: "Amul Butter 100 g", Qty: 1},
		{Product: "Nandini Toned Milk 500 ml", Qty: 2},
		{Product: "Wheat atta 5 kg", Qty: 1},
		{Product: "Curd 400 g", Qty: 1},
	}},
	{Name: "Sunday breakfast", Lines: []seedBasketLine{
		{Product: "Bread 400 g", Qty: 1},
		{Product: "Eggs tray 12", Qty: 1},
		{Product: "Amul Butter 100 g", Qty: 1},
		{Text: "maggi noodles", Qty: 2},
	}},
}

func seedbaskets(ctx context.Context, d Deps, _ Profile) error {
	if !d.Keys.PIIEncKey.IsSet() || !d.Keys.PIIHashKey.IsSet() {
		return nil
	}
	q := d.DB.Q(ctx)
	owner := ids.UUID(identityUserID("customer.asha"))
	line := 0
	for i, b := range seedBaskets {
		at := d.Clock.Now().UTC().Add(-time.Duration(len(seedBaskets)-i) * time.Hour)
		bid := ids.UUID(seqID(basketIDPrefix, i+1))
		if _, err := q.Exec(ctx, `INSERT INTO baskets (id, customer_id, name, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $4) ON CONFLICT (id) DO NOTHING`, bid, owner, b.Name, at); err != nil {
			return err
		}
		for pos, l := range b.Lines {
			line++
			var product, text any
			if l.Product != "" {
				product = ids.UUID(productSeedID(l.Product))
			} else {
				text = l.Text
			}
			if _, err := q.Exec(ctx, `INSERT INTO basket_lines (id, basket_id, product_id, text, quantity, position, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (id) DO NOTHING`,
				ids.UUID(seqID(basketLineIDPrefix, line)), bid, product, text, l.Qty, pos, at); err != nil {
				return err
			}
		}
	}
	d.Log.DebugContext(ctx, "seed baskets", slog.Int("baskets", len(seedBaskets)), slog.Int("lines", line))
	return nil
}

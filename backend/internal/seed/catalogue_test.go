package seed

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/testkit"
)

var brandSlugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// TestCatalogueSeedFixedValues checks the lists against the schema's checks
// and each other without a database, so a bad entry fails with its name.
func TestCatalogueSeedFixedValues(t *testing.T) {
	products, pids := masterProducts()
	require.Len(t, pids, len(products))
	t.Logf("%d master products", len(products))
	require.GreaterOrEqual(t, len(products), 2900, "about 3,000 master products")
	require.LessOrEqual(t, len(products), 3100, "about 3,000 master products")

	units := map[string]bool{"g": true, "kg": true, "ml": true, "l": true, "pc": true, "pack": true}
	seen := map[string]string{}
	unique := func(kind, v, who string) {
		t.Helper()
		prev, dup := seen[kind+":"+v]
		require.False(t, dup, "%s reuses the %s of %s", who, kind, prev)
		seen[kind+":"+v] = who
	}
	for i, p := range products {
		unique("product id", pids[i].String(), p.Name)
		unique("name", p.Name, p.Name)
		require.True(t, len([]rune(p.Name)) >= 1 && len([]rune(p.Name)) <= 120, p.Name)
		require.True(t, units[p.Unit], p.Name)
		require.Positive(t, p.Pack, p.Name)
		require.Positive(t, p.MRP, p.Name)
		categoryID(p.Category) // panics on an unknown category
		if p.Brand != "" {
			require.LessOrEqual(t, len([]rune(p.Brand)), 80, p.Brand)
			require.Regexp(t, brandSlugRE, slugOf(p.Brand), p.Brand)
		}
		if p.Barcode != "" {
			unique("barcode", p.Barcode, p.Name)
			require.Len(t, p.Barcode, 13, p.Name)
			require.Equal(t, p.Barcode, eanWithCheck(p.Barcode[:12]), "%s: EAN-13 check digit", p.Name)
		}
	}
	// Two brands with one slug would make the brand insert skip one silently.
	slugs := map[string]string{}
	for _, p := range products {
		if p.Brand == "" {
			continue
		}
		s := slugOf(p.Brand)
		if prev, ok := slugs[s]; ok {
			require.Equal(t, prev, p.Brand, "brands %q and %q share the slug %s", prev, p.Brand, s)
		}
		slugs[s] = p.Brand
	}

	keys := map[string]bool{}
	for _, s := range seedStoreItems {
		storeSeedID(s.Store) // panics on an unknown store
		listed := map[string]bool{}
		for i, it := range s.Items {
			who := s.Store + "/" + it.Product
			require.False(t, listed[it.Product], "%s listed twice", who)
			listed[it.Product] = true
			unique("item id", seqID(storeItemIDPrefix, s.No*100+i+1).String(), who)
			productSeedID(it.Product) // panics on an unknown product
			require.Positive(t, it.Price, who)
			mrp := it.MRP
			if mrp == 0 {
				mrp = fixtureMRP(it.Product)
			}
			require.LessOrEqual(t, it.Price, mrp, "%s: price above the MRP", who)
			for _, in := range it.Stock {
				require.Positive(t, in.Qty, who)
			}
			if it.Key != "" {
				require.False(t, keys[it.Key], it.Key)
				keys[it.Key] = true
				require.Equal(t, seqID(storeItemIDPrefix, s.No*100+i+1), storeItemSeedID(it.Key))
			}
		}
	}
	for i := 1; i <= 14; i++ {
		require.True(t, keys["I"+strconv.Itoa(i)], "I%d", i)
	}
	for i := 1; i <= 22; i++ {
		require.True(t, keys["J"+strconv.Itoa(i)], "J%d", i)
	}
	require.Panics(t, func() { storeItemSeedID("Z9") })
	require.Panics(t, func() { productSeedID("No such thing") })

	// The deterministic id source is strictly increasing within one time.
	next := seedIDs()
	a, b := next(Epoch), next(Epoch)
	require.Negative(t, a.Compare(b))
	require.Equal(t, a, seedIDs()(Epoch), "the same run gives the same ids")
}

func eanWithCheck(s12 string) string {
	sum := 0
	for i, c := range s12 {
		d := int(c - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return s12 + strconv.Itoa((10-sum%10)%10)
}

func TestCatalogueSeed(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	d := Deps{DB: db.New(tdb.Pool), Keys: testKeys(t)}
	require.NoError(t, Run(ctx, config.EnvTest, Dev, d))

	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		require.NoError(t, tdb.Pool.QueryRow(ctx, sql, args...).Scan(&n))
		return n
	}
	products, _ := masterProducts()
	require.Equal(t, len(products), count(`SELECT count(*) FROM products WHERE status = 'active'`))
	items := 0
	for _, s := range seedStoreItems {
		items += len(s.Items)
	}
	require.Equal(t, items, count(`SELECT count(*) FROM store_items`))
	movements := count(`SELECT count(*) FROM stock_movements`)
	batches := count(`SELECT count(*) FROM stock_batches`)
	require.Positive(t, movements)
	require.Positive(t, batches)

	// Every live store has items; no store that is not live has any.
	require.Zero(t, count(`SELECT count(*) FROM stores s WHERE s.status = 'live'
		AND NOT EXISTS (SELECT 1 FROM store_items i WHERE i.store_id = s.id)`))
	require.Zero(t, count(`SELECT count(*) FROM store_items i JOIN stores s ON s.id = i.store_id WHERE s.status <> 'live'`))

	// The ledgers agree with the counters (03 stock rules 1 and 8).
	require.Zero(t, count(`SELECT count(*) FROM store_items i
		WHERE i.on_hand <> coalesce((SELECT sum(delta) FROM stock_movements m WHERE m.store_item_id = i.id), 0)`))
	require.Zero(t, count(`SELECT count(*) FROM stock_batches b
		WHERE b.quantity <> coalesce((SELECT sum(delta) FROM stock_movement_batches a WHERE a.batch_id = b.id), 0)`))
	require.Zero(t, count(`SELECT count(*) FROM store_items i
		WHERE (SELECT coalesce(sum(quantity), 0) FROM stock_batches b WHERE b.store_item_id = i.id) > greatest(i.on_hand, 0)`))
	require.Zero(t, count(`SELECT count(*) FROM store_items WHERE search = ''::tsvector`))

	type itemRow struct {
		Price, OnHand int64
		Updated       *time.Time
	}
	item := func(key string) itemRow {
		t.Helper()
		var r itemRow
		require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT price_paise, on_hand, stock_updated_at FROM store_items WHERE id = $1`,
			ids.UUID(storeItemSeedID(key))).Scan(&r.Price, &r.OnHand, &r.Updated))
		return r
	}
	today := catalogue.Today(Epoch)

	i1 := item("I1")
	require.Equal(t, itemRow{Price: 5800, OnHand: 10, Updated: i1.Updated}, i1)
	require.True(t, i1.Updated.Equal(Epoch))

	// I7: stale (84 h), latest batch cost ₹128.
	i7 := item("I7")
	require.EqualValues(t, 8, i7.OnHand)
	require.True(t, i7.Updated.Equal(Epoch.Add(-84*time.Hour)), "I7 stale: %s", i7.Updated)
	var cost int64
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT cost_paise FROM stock_batches WHERE store_item_id = $1 ORDER BY received_at DESC LIMIT 1`,
		ids.UUID(storeItemSeedID("I7"))).Scan(&cost))
	require.EqualValues(t, 12800, cost)

	// I8: 20 on hand, one batch of 10 expiring in three days.
	require.EqualValues(t, 20, item("I8").OnHand)
	var qty int
	var exp time.Time
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT quantity, expires_on FROM stock_batches WHERE store_item_id = $1`,
		ids.UUID(storeItemSeedID("I8"))).Scan(&qty, &exp))
	require.Equal(t, 10, qty)
	require.Equal(t, today.AddDate(0, 0, 3).Format(time.DateOnly), exp.Format(time.DateOnly))

	// J6: six expired yesterday still on the shelf, four expire in six days.
	rows, err := tdb.Pool.Query(ctx, `SELECT quantity, expires_on FROM stock_batches WHERE store_item_id = $1 ORDER BY expires_on`,
		ids.UUID(storeItemSeedID("J6")))
	require.NoError(t, err)
	var j6 []string
	for rows.Next() {
		require.NoError(t, rows.Scan(&qty, &exp))
		j6 = append(j6, strconv.Itoa(qty)+"@"+exp.Format(time.DateOnly))
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{
		"6@" + today.AddDate(0, 0, -1).Format(time.DateOnly),
		"4@" + today.AddDate(0, 0, 6).Format(time.DateOnly),
	}, j6)
	require.EqualValues(t, 10, item("J6").OnHand)

	// J11 has never had stock; J12–J17 were last stocked nine days ago; J20
	// is the same product as I3.
	j11 := item("J11")
	require.Zero(t, j11.OnHand)
	require.Nil(t, j11.Updated)
	for i := 12; i <= 17; i++ {
		r := item("J" + strconv.Itoa(i))
		require.True(t, r.Updated.Equal(Epoch.Add(-9*day)), "J%d", i)
	}
	require.Equal(t, 1, count(`SELECT count(DISTINCT product_id) FROM store_items WHERE id IN ($1, $2)`,
		ids.UUID(storeItemSeedID("I3")), ids.UUID(storeItemSeedID("J20"))))

	// The asks: Horlicks, Bournvita and the millets exist and no store lists
	// them; the bulb is only at AP, 2 on hand, five days old.
	for _, name := range []string{"Horlicks 500 g", "Bournvita 500 g", "Kodo millet 1 kg", "Foxtail millet 500 g"} {
		require.Equal(t, 1, count(`SELECT count(*) FROM products WHERE id = $1`, ids.UUID(productSeedID(name))), name)
		require.Zero(t, count(`SELECT count(*) FROM store_items WHERE product_id = $1`, ids.UUID(productSeedID(name))), name)
	}
	var bulbStore [16]byte
	var bulbOnHand int
	var bulbAt time.Time
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT store_id, on_hand, stock_updated_at FROM store_items WHERE product_id = $1`,
		ids.UUID(productSeedID("LED bulb 9 W"))).Scan(&bulbStore, &bulbOnHand, &bulbAt))
	require.Equal(t, storeSeedID("AP"), ids.ID(bulbStore))
	require.Equal(t, 2, bulbOnHand)
	require.True(t, bulbAt.Equal(Epoch.Add(-5*day)))
	for _, name := range []string{"Amul Butter 100 g", "Idli rice 1 kg"} {
		require.Zero(t, count(`SELECT count(*) FROM store_items WHERE store_id = $1 AND product_id = $2`,
			ids.UUID(storeSeedID("AP")), ids.UUID(productSeedID(name))), "AP must not list %s", name)
	}

	// <seed:price-compare>: twelve Chennai neighbours sell sugar (median ₹58),
	// nine sell jaggery (median ₹90).
	median := func(name string) (int, []int64) {
		t.Helper()
		rows, err := tdb.Pool.Query(ctx, `SELECT i.price_paise FROM store_items i JOIN stores s ON s.id = i.store_id
			WHERE i.product_id = $1 AND s.name LIKE 'Chennai Store %'`, ids.UUID(productSeedID(name)))
		require.NoError(t, err)
		defer rows.Close()
		var ps []int64
		for rows.Next() {
			var p int64
			require.NoError(t, rows.Scan(&p))
			ps = append(ps, p)
		}
		require.NoError(t, rows.Err())
		sort.Slice(ps, func(i, j int) bool { return ps[i] < ps[j] })
		return len(ps), ps
	}
	n, ps := median("Sugar 1 kg")
	require.Equal(t, 12, n)
	require.EqualValues(t, 5800, (ps[5]+ps[6])/2)
	n, ps = median("Jaggery 1 kg")
	require.Equal(t, 9, n)
	require.EqualValues(t, 9000, ps[4])

	// A second run (another profile) changes nothing.
	require.NoError(t, Run(ctx, config.EnvTest, Test, d))
	require.Equal(t, movements, count(`SELECT count(*) FROM stock_movements`))
	require.Equal(t, batches, count(`SELECT count(*) FROM stock_batches`))
	require.Equal(t, len(products), count(`SELECT count(*) FROM products`))
	require.Equal(t, items, count(`SELECT count(*) FROM store_items`))
}

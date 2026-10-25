package seed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/gen/sqlc/insightsdb"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

// The bill grid alone, without a database: the counts 08 §2 gives.
func TestInsightBillsGrid(t *testing.T) {
	today := clock.StartOfDayIST(Epoch)
	bills := insightBills(today, Epoch)
	in60 := map[string]int{}
	in7 := map[string]int{}
	both := map[[2]string]int{}
	n60, lines := 0, 0
	for _, b := range bills {
		lines += len(b.Lines)
		if b.Store != "SL2" || b.Ago < 1 {
			continue
		}
		n60++
		seen := map[string]bool{}
		for _, l := range b.Lines {
			seen[l.Key] = true
			if b.Ago <= 7 {
				in7[l.Key] += int(l.Qty)
			}
		}
		for k := range seen {
			in60[k]++
			for k2 := range seen {
				if k < k2 {
					both[[2]string{k, k2}]++
				}
			}
		}
	}
	require.Equal(t, 600, n60)
	require.Less(t, lines, 9000, "line ids 9001+ are Idli rice's")
	require.Equal(t, 480, in60["J20"])
	require.Equal(t, 300, in60["J21"])
	require.Equal(t, 240, both[[2]string{"J20", "J21"}])
	require.Equal(t, 84, in60["J18"])
	require.Equal(t, 70, in60["J19"])
	require.Equal(t, 21, both[[2]string{"J18", "J19"}])
	require.Equal(t, 64, in7["J1"])
	require.Equal(t, 56, in7["J20"])
	require.Equal(t, 35, in7["J21"])
	for k, n := range both {
		if k != [2]string{"J20", "J21"} && k != [2]string{"J18", "J19"} && n >= 20 {
			// Stored by the rollup but under the share floor: the A item's bills are 8+ times the pair's.
			require.Contains(t, [][2]string{{"J18", "J20"}, {"J19", "J21"}}, k, "unexpected pair %v with %d bills", k, n)
		}
	}
}

// After the dev seed every Demand insights kind but priced above nearby (off
// until Q-52) fires: SL2 has 13 rows in 10 kinds and SL the original kinds,
// with the cards 08 §2 lists. A second run changes nothing.
func TestInsightsSeed(t *testing.T) {
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	d := Deps{DB: db.New(tdb.Pool), Keys: testKeys(t)}
	for range 2 {
		require.NoError(t, Run(ctx, config.EnvTest, Dev, d))
	}
	var bills int
	require.NoError(t, tdb.OwnerPool.QueryRow(ctx, `SELECT count(*) FROM pos_sales`).Scan(&bills))
	src := facts.Source{
		Q:        insightsdb.New(tdb.Pool),
		Settings: settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{}),
		Asks:     facts.NoAsks{},
	}
	kinds := func(store string) (map[facts.Kind]int, []facts.Kind) {
		res, err := facts.Compute(ctx, src, storeSeedID(store), Epoch, nil)
		require.NoError(t, err)
		by := map[facts.Kind]int{}
		for _, r := range res.Rows {
			by[r.Kind]++
		}
		var card []facts.Kind
		for _, r := range facts.Card(res.Rows, 4) {
			card = append(card, r.Kind)
		}
		return by, card
	}

	sl2, card := kinds("SL2")
	require.Equal(t, map[facts.Kind]int{
		facts.SellingFast: 1, facts.SittingOnShelf: 2, facts.GoingToWaste: 1, facts.WantedNearby: 2,
		facts.MissedSales: 1, facts.ExpiredOnShelf: 1, facts.SellingBelowCost: 2, facts.StockCheckDue: 1,
		facts.BoughtTogether: 1, facts.RegularsSlipping: 1,
	}, sl2)
	require.Equal(t, []facts.Kind{facts.SellingFast, facts.GoingToWaste, facts.ExpiredOnShelf, facts.SellingBelowCost}, card)

	sl, card := kinds("SL")
	require.Equal(t, map[facts.Kind]int{
		facts.SellingFast: 1, facts.SittingOnShelf: 1, facts.GoingToWaste: 1, facts.WantedNearby: 1,
		facts.StockCheckDue: 1,
	}, sl)
	require.ElementsMatch(t, []facts.Kind{facts.SellingFast, facts.GoingToWaste, facts.StockCheckDue, facts.SittingOnShelf}, card)

	var again int
	require.NoError(t, Run(ctx, config.EnvTest, Dev, d))
	require.NoError(t, tdb.OwnerPool.QueryRow(ctx, `SELECT count(*) FROM pos_sales`).Scan(&again))
	require.Equal(t, bills, again)
}

package seed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/testkit"
)

// The asks fixtures (W3-16): open, answered and expired asks, a muted store, a
// low-yield customer, one gap at the floor of ten customers and one below it,
// and an approved and a proposed strip. A second run changes nothing.
func TestAsksSeed(t *testing.T) {
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	d := Deps{DB: db.New(tdb.Pool), Keys: testKeys(t)}
	count := func(sql string, args ...any) int {
		var n int
		require.NoError(t, tdb.OwnerPool.QueryRow(ctx, sql, args...).Scan(&n))
		return n
	}
	for range 2 {
		require.NoError(t, Run(ctx, config.EnvTest, Dev, d))
	}
	asha, ravi := ids.UUID(identityUserID("customer.asha")), ids.UUID(identityUserID("customer.ravi"))

	require.Equal(t, 1, count(`SELECT count(*) FROM asks a JOIN ask_recipients r ON r.ask_id = a.id
	  WHERE a.customer_id = $1 AND a.status = 'open' AND r.store_id = $2 AND r.answer IS NULL AND r.reply_by > $3`,
		asha, ids.UUID(storeSeedID("SL")), Epoch), "Asha's open ask waits in Sri Lakshmi's inbox")
	require.Equal(t, 1, count(`SELECT count(*) FROM asks a JOIN ask_recipients r ON r.ask_id = a.id
	  WHERE a.customer_id = $1 AND a.status = 'open' AND r.answer = 'yes' AND r.price_paise = 5800
	    AND r.store_item_id IS NOT NULL AND r.price_valid_until > $2`, ravi, Epoch), "Ravi has a Yes to book")
	require.Equal(t, 1, count(`SELECT count(*) FROM asks WHERE customer_id = $1 AND close_reason = 'window_ended'`, asha))
	require.Equal(t, 1, count(`SELECT count(*) FROM stores WHERE id = $1 AND asks_muted`, ids.UUID(storeSeedID("NH"))))
	require.Equal(t, 0, count(`SELECT count(*) FROM ask_recipients WHERE store_id = $1`, ids.UUID(storeSeedID("NH"))))

	var lowAfter int
	require.NoError(t, tdb.OwnerPool.QueryRow(ctx,
		`SELECT (value #>> '{}')::int FROM platform_settings WHERE key = 'ask_low_yield_after_count'`).Scan(&lowAfter))
	require.Equal(t, lowAfter, count(`SELECT count(*) FROM asks a WHERE a.customer_id = $1
	  AND NOT EXISTS (SELECT 1 FROM ask_recipients r WHERE r.ask_id = a.id AND r.answer = 'yes')`,
		ids.UUID(identityUserID("customer.new"))), "the new customer is low-yield")

	gap := func(product string) int {
		return count(`SELECT count(DISTINCT customer_id) FROM asks WHERE product_id = $1 AND status = 'closed' AND created_at >= $2`,
			ids.UUID(productSeedID(product)), Epoch.AddDate(0, 0, -7))
	}
	require.Equal(t, 10, gap("Kodo millet 1 kg"))
	require.Equal(t, 9, gap("Foxtail millet 500 g"))

	require.Equal(t, 4, count(`SELECT count(*) FROM site_demand_marquee WHERE status = 'approved'`))
	require.Equal(t, 5, count(`SELECT count(*) FROM site_demand_marquee WHERE status = 'proposed'
	  AND built_on = (SELECT max(built_on) FROM site_demand_marquee)`))
	require.Equal(t, 1, count(`SELECT count(DISTINCT built_on) FROM site_demand_marquee WHERE status = 'approved'
	  AND built_on < (SELECT max(built_on) FROM site_demand_marquee)`))
}

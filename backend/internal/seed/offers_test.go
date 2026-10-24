package seed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/testkit"
)

// <seed:offer-I13-21off> and <seed:banner-SL> (manual book §7) exist after the
// dev seed, the demo seed adds the other shapes, and a second run changes
// nothing.
func TestOffersAndBannersSeed(t *testing.T) {
	ctx := context.Background()
	count := func(tdb *testkit.DB, sql string, args ...any) int {
		var n int
		require.NoError(t, tdb.OwnerPool.QueryRow(ctx, sql, args...).Scan(&n))
		return n
	}
	for _, c := range []struct {
		p      Profile
		offers int
	}{{Dev, 1}, {Demo, 4}} {
		tdb := testkit.NewDB(t)
		for range 2 {
			require.NoError(t, Run(ctx, config.EnvTest, c.p, Deps{DB: db.New(tdb.Pool)}), c.p)
		}
		require.Equal(t, c.offers, count(tdb, `SELECT count(*) FROM offers`), c.p)
		require.Equal(t, 1, count(tdb, `SELECT count(*) FROM offers o JOIN store_items i ON i.id = ANY (o.store_item_ids)
		  WHERE o.kind = 'flat' AND o.value = 2100 AND o.status = 'live' AND o.ends_at IS NULL AND i.store_id = o.store_id
		    AND i.price_paise = 12000`), "the ₹21 offer on I13 (₹120) at SL")
		require.Equal(t, 1, count(tdb, `SELECT count(*) FROM store_banners b JOIN offers o ON o.id = b.target_id
		  WHERE b.headline = 'Fresh stock every morning' AND b.status = 'live' AND b.moderation = 'auto_ok'
		    AND b.target_type = 'offer' AND o.kind = 'flat'`), c.p)
	}
}

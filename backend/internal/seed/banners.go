package seed

import (
	"context"
	"fmt"

	"github.com/todayz/backend/internal/platform/ids"
)

// Seeded banner ids: prefix + a fixed number (seqID).
const bannerIDPrefix = "01KE0S1W6W8ZV7T1C3A6MW"

// seedbanners writes <seed:banner-SL> (docs/qa/manual/README.md §7): one live
// template banner at SL, headline "Fresh stock every morning", aimed at the
// I13 offer of seedoffers. The seed clock is its start; a second run changes
// nothing.
func seedbanners(ctx context.Context, d Deps, _ Profile) error {
	now := d.Clock.Now().UTC()
	_, err := d.DB.Q(ctx).Exec(ctx, `INSERT INTO store_banners (id, store_id, template, headline, target_type, target_id,
	    starts_at, status, moderation, created_at, updated_at)
	  VALUES ($1, $2, '{"tint":"green","subline":"Order now, pick up today"}'::jsonb, 'Fresh stock every morning', 'offer', $3,
	    $4, 'live', 'auto_ok', $4, $4)
	  ON CONFLICT DO NOTHING`,
		ids.UUID(seqID(bannerIDPrefix, 1)), ids.UUID(storeSeedID("SL")), ids.UUID(seqID(offerIDPrefix, 1)), now)
	if err != nil {
		return fmt.Errorf("banner at SL: %w", err)
	}
	return nil
}

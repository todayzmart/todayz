package catalogue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/todayz/backend/gen/sqlc/cataloguedb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
)

var (
	// ErrStale: the item changed between the read and the write.
	ErrStale = errors.New("catalogue: the item changed; read it again")
)

// SetPrice changes one item's selling price inside the caller's transaction
// q, as PATCH {S}/items/{itemId} with only pricePaise does: the catalogue
// change is recorded first (lock order: the stores row, then the item), and
// the write is guarded by the item's version read in the same transaction.
// The price may be above the MRP (D-245). It is the interface voice
// confirm (W3-04) uses; the caller has checked item.price.edit.
func SetPrice(ctx context.Context, q db.Querier, now time.Time, storeID, itemID ids.ID, pricePaise int64) error {
	if pricePaise <= 0 {
		return ErrQuantity
	}
	c := cataloguedb.New(q)
	if _, err := c.RecordCatalogueChange(ctx, cataloguedb.RecordCatalogueChangeParams{
		StoreID: ids.UUID(storeID), Entity: "store_item", EntityID: ids.UUID(itemID), Op: "upsert", Now: now,
	}); err != nil {
		return fmt.Errorf("catalogue: catalogue change: %w", err)
	}
	cur, err := c.GetStoreItem(ctx, cataloguedb.GetStoreItemParams{ID: ids.UUID(itemID), StoreID: ids.UUID(storeID)})
	if db.IsNoRows(err) {
		return ErrItemNotFound
	}
	if err != nil {
		return fmt.Errorf("catalogue: item: %w", err)
	}
	if _, err := c.UpdateStoreItem(ctx, cataloguedb.UpdateStoreItemParams{
		SetPrice: true, PricePaise: pricePaise,
		ID: ids.UUID(itemID), StoreID: ids.UUID(storeID), Version: cur.Version,
	}); err != nil {
		if db.IsNoRows(err) {
			return ErrStale
		}
		return fmt.Errorf("catalogue: update item: %w", err)
	}
	return nil
}

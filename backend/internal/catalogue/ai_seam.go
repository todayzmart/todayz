package catalogue

import (
	"context"

	catalogueapi "github.com/todayz/backend/gen/api/catalogue"
)

// CreateItem lists one product in the caller's store, exactly as POST .../items does (the
// caller needs item.price.edit too, the price is set on create). The shelf photo draft
// (W3-11) calls it from its confirm, in a savepoint of the draft's transaction, so a refused
// line rolls back alone and the others are listed. Field errors carry the items route's paths
// (body.pricePaise …).
func (s *Service) CreateItem(ctx context.Context, bid, sid string, b *catalogueapi.StoreItemCreate) (catalogueapi.StoreItem, error) {
	it, _, err := s.createItem(ctx, bid, sid, b)
	return it, err
}

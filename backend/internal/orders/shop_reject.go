package orders

import (
	"context"
	"fmt"

	"github.com/todayz/backend/internal/platform/ids"
)

// CancelShopOrders cancels every open online order of the business's stores
// when a field agent or admin rejects the shop (D-274): the reason is the
// system's shop_rejected, nobody gets a strike, held stock, credits and
// offers go back (end), and each customer is told. It runs in the caller's
// transaction, so the reject and the cancels commit together, and answers
// how many orders it cancelled. Counter sales are not orders and stay.
func (s *Service) CancelShopOrders(ctx context.Context, businessID ids.ID) (int, error) {
	if err := s.readyToWrite(ctx); err != nil {
		return 0, err
	}
	// Locks every open order first, in id order (see the query).
	rows, err := s.q(ctx).OpenOrdersOfBusiness(ctx, ids.UUID(businessID))
	if err != nil {
		return 0, fmt.Errorf("orders: open orders of business: %w", err)
	}
	now := s.now()
	n := 0
	for _, id := range rows {
		o, err := s.lockOrder(ctx, mustID(id), anyOrder)
		if err != nil {
			return n, err
		}
		// Ended since the list was read (a timer or the customer got there
		// first): nothing to cancel.
		if !CanMove(o.Status, StatusCancelled, o.Fulfilment) {
			continue
		}
		if err := s.end(ctx, o, StatusCancelled, reasonShopRejected, nil, system, variant(reasonShopRejected), now); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

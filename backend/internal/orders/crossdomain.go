package orders

import (
	"context"
	"fmt"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
)

// What other domains call through their own interfaces.

var _ notice.UnseenOrders = (*Service)(nil)

// Unseen implements notice.UnseenOrders (W2-05's "new order not seen"
// escalation): placed orders no staff member has listed or opened.
func (s *Service) Unseen(ctx context.Context, q notice.UnseenQuery) ([]notice.UnseenOrder, error) {
	if s.db == nil {
		return nil, errUnavailable
	}
	p := ordersdb.UnseenOrdersParams{CreatedBefore: q.CreatedBefore, Lim: i32(max(q.Limit, 0))}
	if q.After != nil {
		at := q.After.CreatedAt
		p.AfterAt, p.AfterID = &at, ids.UUID(q.After.OrderID)
	}
	rows, err := s.q(ctx).UnseenOrders(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("orders: unseen: %w", err)
	}
	out := make([]notice.UnseenOrder, len(rows))
	for i, r := range rows {
		out[i] = notice.UnseenOrder{OrderID: mustID(r.ID), StoreID: mustID(r.StoreID), CreatedAt: r.PlacedAt}
	}
	return out, nil
}

// CanDeleteAccount refuses an account deletion while the customer has an
// open order (BK-44): 409 account_has_active_order. Identity runs it inside
// the deletion's transaction, which holds the users row, and create holds
// the same row, so an order cannot slip in between.
func (s *Service) CanDeleteAccount(ctx context.Context, userID ids.ID) error {
	if s.db == nil {
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	n, err := s.q(ctx).ActiveOrderCount(ctx, ids.UUID(userID))
	if err != nil {
		return fmt.Errorf("orders: active orders: %w", err)
	}
	if n > 0 {
		return problem.New(genproblems.AccountHasActiveOrder, "")
	}
	return nil
}

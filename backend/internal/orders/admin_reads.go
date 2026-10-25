package orders

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	adminordersapi "github.com/todayz/backend/gen/api/admin-orders"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// The admin order search and detail (A-07, W3-05). They sit beside the admin
// cancel because one generated admin-orders server serves all three. Neither
// reads the pickup code or the address.

// ListAdminOrders implements GET /v1/admin/orders.
func (h adminHandlers) ListAdminOrders(ctx context.Context, req adminordersapi.ListAdminOrdersRequestObject) (adminordersapi.ListAdminOrdersResponseObject, error) {
	out, err := h.s.adminOrders(ctx, req.Params)
	if err != nil {
		return nil, err
	}
	return adminordersapi.ListAdminOrders200JSONResponse(out), nil
}

// GetAdminOrder implements GET /v1/admin/orders/{orderId}.
func (h adminHandlers) GetAdminOrder(ctx context.Context, req adminordersapi.GetAdminOrderRequestObject) (adminordersapi.GetAdminOrderResponseObject, error) {
	id, err := pathID(req.OrderId)
	if err != nil {
		return nil, err
	}
	out, err := h.s.adminOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	return adminordersapi.GetAdminOrder200JSONResponse(out), nil
}

// orderNumber reads TZ-024817 or 24817 as the order's sequence number.
func orderNumber(s string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimPrefix(s, "TZ-"), 10, 64)
	return n, err == nil && n > 0
}

// optUUID is a filter id: unset when nil.
func optUUID(s *string, field string) (pgtype.UUID, error) {
	if s == nil {
		return pgtype.UUID{}, nil
	}
	id, err := ids.Parse(*s)
	if err != nil {
		return pgtype.UUID{}, fieldErr(field, "not an id")
	}
	return ids.UUID(id), nil
}

func (s *Service) adminOrders(ctx context.Context, p adminordersapi.ListAdminOrdersParams) (adminordersapi.AdminOrderList, error) {
	if err := s.ready(ctx); err != nil {
		return adminordersapi.AdminOrderList{}, err
	}
	lim, at, before, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return adminordersapi.AdminOrderList{}, err
	}
	args := ordersdb.AdminListOrdersParams{FromAt: p.From, ToAt: p.To, BeforeAt: at, BeforeID: before, Lim: i32(lim + 1)}
	if p.Number != nil {
		n, ok := orderNumber(*p.Number)
		if !ok {
			return adminordersapi.AdminOrderList{}, fieldErr("query.number", "not an order number")
		}
		args.Number = &n
	}
	if args.StoreID, err = optUUID(p.StoreId, "query.storeId"); err != nil {
		return adminordersapi.AdminOrderList{}, err
	}
	if args.CustomerID, err = optUUID(p.CustomerId, "query.customerId"); err != nil {
		return adminordersapi.AdminOrderList{}, err
	}
	if p.Status != nil {
		args.Status = ptr(string(*p.Status))
	}
	rows, err := s.q(ctx).AdminListOrders(ctx, args)
	if err != nil {
		return adminordersapi.AdminOrderList{}, problem.Wrap(genproblems.Internal, err)
	}
	out := adminordersapi.AdminOrderList{Items: []adminordersapi.AdminOrderSummary{}, ServerNow: s.now()}
	more := len(rows) > lim
	if more {
		rows = rows[:lim]
	}
	for _, r := range rows {
		out.Items = append(out.Items, adminordersapi.AdminOrderSummary{
			Id: mustID(r.ID).String(), Number: number(r.DisplayNumber, r.ID),
			StoreId: mustID(r.StoreID).String(), StoreName: r.StoreName,
			CustomerId: mustID(r.CustomerID).String(), CustomerName: r.CustomerName,
			Fulfilment: adminordersapi.AdminOrderSummaryFulfilment(r.Fulfilment), Status: adminordersapi.AdminOrderStatus(r.Status),
			StatusReason: r.StatusReason, TotalPaise: int(r.TotalPaise), PlacedAt: r.PlacedAt.UTC(),
		})
	}
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		out.NextCursor = ptr(encodeCursor(last.PlacedAt, mustID(last.ID)))
	}
	return out, nil
}

func (s *Service) adminOrder(ctx context.Context, orderID ids.ID) (adminordersapi.AdminOrder, error) {
	if err := s.ready(ctx); err != nil {
		return adminordersapi.AdminOrder{}, err
	}
	q := s.q(ctx)
	o, err := q.GetOrder(ctx, ids.UUID(orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return adminordersapi.AdminOrder{}, notFound()
	}
	if err != nil {
		return adminordersapi.AdminOrder{}, problem.Wrap(genproblems.Internal, err)
	}
	names, err := q.AdminOrderNames(ctx, ordersdb.AdminOrderNamesParams{StoreID: o.StoreID, CustomerID: o.CustomerID})
	if err != nil {
		return adminordersapi.AdminOrder{}, problem.Wrap(genproblems.Internal, err)
	}
	lines, err := q.AdminOrderLines(ctx, o.ID)
	if err != nil {
		return adminordersapi.AdminOrder{}, problem.Wrap(genproblems.Internal, err)
	}
	events, err := q.AdminOrderEvents(ctx, o.ID)
	if err != nil {
		return adminordersapi.AdminOrder{}, problem.Wrap(genproblems.Internal, err)
	}
	notes, err := q.AdminOrderNotifications(ctx, o.ID)
	if err != nil {
		return adminordersapi.AdminOrder{}, problem.Wrap(genproblems.Internal, err)
	}
	holds, err := q.AdminOrderCreditHolds(ctx, o.ID)
	if err != nil {
		return adminordersapi.AdminOrder{}, problem.Wrap(genproblems.Internal, err)
	}
	out := adminordersapi.AdminOrder{
		Id: orderID.String(), Number: displayNumber(o), StoreId: mustID(o.StoreID).String(), StoreName: names.StoreName,
		CustomerId: mustID(o.CustomerID).String(), CustomerName: names.CustomerName,
		Fulfilment: adminordersapi.AdminOrderFulfilment(o.Fulfilment), Status: adminordersapi.AdminOrderStatus(o.Status),
		StatusReason: o.StatusReason, StatusNote: o.StatusNote,
		SubtotalPaise: int(o.SubtotalPaise), DiscountPaise: int(o.DiscountPaise), CreditsUsed: int(o.CreditsUsed),
		DeliveryFeePaise: int(o.DeliveryFeePaise), TotalPaise: int(o.TotalPaise), CashbackCredits: int(o.CashbackCredits),
		PaymentStatus: o.PaymentStatus, CodeAttempts: int(o.CodeAttempts), CodeLocked: o.CodeLockedAt != nil,
		AcceptBy: utc(o.AcceptBy), HoldUntil: utc(o.HoldUntil), DispatchBy: utc(o.DispatchBy),
		PlacedAt: o.PlacedAt.UTC(), Version: int(o.Version), ServerNow: s.now(),
		Items:         make([]adminordersapi.AdminOrderLine, 0, len(lines)),
		Events:        make([]adminordersapi.AdminOrderEvent, 0, len(events)),
		Notifications: make([]adminordersapi.AdminOrderNotification, 0, len(notes)),
		CreditHolds:   make([]adminordersapi.AdminOrderCreditHold, 0, len(holds)),
	}
	for _, l := range lines {
		sn := readSnapshot(l.ProductSnapshot)
		out.Items = append(out.Items, adminordersapi.AdminOrderLine{
			Id: mustID(l.ID).String(), Name: sn.Name, Variant: sn.Variant, UnitPricePaise: int(l.UnitPricePaise),
			QuantityRequested: int(l.QuantityRequested), QuantityAccepted: intPtr(l.QuantityAccepted),
			LineTotalPaise: int(l.LineTotalPaise),
		})
	}
	for _, e := range events {
		ev := adminordersapi.AdminOrderEvent{
			Id: mustID(e.ID).String(), ToStatus: adminordersapi.AdminOrderStatus(e.ToStatus),
			ActorKind: adminordersapi.AdminOrderEventActorKind(e.ActorKind), Reason: e.Reason, CreatedAt: e.CreatedAt.UTC(),
		}
		if e.FromStatus != nil {
			ev.FromStatus = ptr(adminordersapi.AdminOrderStatus(*e.FromStatus))
		}
		if e.ActorID.Valid {
			ev.ActorId = ptr(mustID(e.ActorID).String())
		}
		out.Events = append(out.Events, ev)
	}
	for _, n := range notes {
		out.Notifications = append(out.Notifications, adminordersapi.AdminOrderNotification{
			Id: mustID(n.ID).String(), App: adminordersapi.AdminOrderNotificationApp(n.App), Kind: n.Kind,
			CreatedAt: n.CreatedAt.UTC(), ReadAt: utc(n.ReadAt),
		})
	}
	for _, c := range holds {
		out.CreditHolds = append(out.CreditHolds, adminordersapi.AdminOrderCreditHold{
			Id: mustID(c.ID).String(), Amount: int(c.Amount), Captured: int(c.Captured), Released: int(c.Released),
			Status: c.Status, CreatedAt: c.CreatedAt.UTC(),
		})
	}
	return out, nil
}

package orders

import (
	"context"
	"errors"

	"github.com/go-chi/chi/v5"

	adminordersapi "github.com/todayz/backend/gen/api/admin-orders"
	ordersapi "github.com/todayz/backend/gen/api/orders"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Ops maps the orders domain's operations to their authz actions, keyed as
// the generated strict server names them (the Go method name).
var Ops = map[string]string{
	"QuoteCheckout":       policy.MeOrdersPlace,
	"CreateOrder":         policy.MeOrdersPlace,
	"ListMyOrders":        policy.MeOrdersView,
	"GetMyOrder":          policy.MeOrdersView,
	"CancelOrder":         policy.MeOrdersManage,
	"ConfirmPartialOrder": policy.MeOrdersManage,
	"ExtendOrder":         policy.MeOrdersManage,
	"NewOrderCode":        policy.MeOrdersManage,

	"ListStoreOrders":    policy.OrderView,
	"GetStoreOrder":      policy.OrderView,
	"AcceptOrder":        policy.OrderAccept,
	"RejectOrder":        policy.OrderReject,
	"ReadyOrder":         policy.OrderReady,
	"DispatchOrder":      policy.OrderDispatch,
	"VerifyOrderCode":    policy.OrderHandover,
	"FailOrderDelivery":  policy.OrderFailDelivery,
	"ConfirmOrderReturn": policy.OrderConfirmReturn,
	"CancelStoreOrder":   policy.OrderCancel,
}

// Register mounts the orders operations behind slot 15.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	ordersapi.Register(r, handlers{s},
		ordersapi.StrictMiddlewareFunc(authz.Strict[ordersapi.StrictHandlerFunc](az, Ops)))
	adminordersapi.Register(r, adminHandlers{s},
		adminordersapi.StrictMiddlewareFunc(authz.Strict[adminordersapi.StrictHandlerFunc](az, AdminOps)))
}

type handlers struct{ s *Service }

var _ ordersapi.StrictServerInterface = handlers{}

// staffScope is the staff member and the store the route's check passed.
func staffScope(ctx context.Context) (ids.ID, ids.ID, error) {
	p, err := principal(ctx)
	if err != nil {
		return ids.Zero, ids.Zero, err
	}
	d, ok := authz.DecisionFrom(ctx)
	if !ok || d.Scope.StoreID == ids.Zero {
		return ids.Zero, ids.Zero, problem.Wrap(genproblems.Internal, errors.New("orders: no store in the authz decision"))
	}
	return p.UserID, d.Scope.StoreID, nil
}

// staffAction runs one staff transition and answers the order as it is now.
func (h handlers) staffAction(ctx context.Context, orderID string, act func(by, storeID, orderID ids.ID) error) (ordersapi.StoreOrder, error) {
	by, storeID, err := staffScope(ctx)
	if err != nil {
		return ordersapi.StoreOrder{}, err
	}
	id, err := pathID(orderID)
	if err != nil {
		return ordersapi.StoreOrder{}, err
	}
	if err := act(by, storeID, id); err != nil {
		return ordersapi.StoreOrder{}, err
	}
	return h.s.storeOrderAfter(ctx, id)
}

// customerAction runs one customer transition and answers the order (with
// no code: an action's response may be stored for an idempotent replay).
func (h handlers) customerAction(ctx context.Context, orderID string, act func(userID, orderID ids.ID) error) (ordersapi.Order, error) {
	p, err := principal(ctx)
	if err != nil {
		return ordersapi.Order{}, err
	}
	id, err := pathID(orderID)
	if err != nil {
		return ordersapi.Order{}, err
	}
	if err := act(p.UserID, id); err != nil {
		return ordersapi.Order{}, err
	}
	return h.s.customerOrder(ctx, p.UserID, id, false)
}

// --- customer ---------------------------------------------------------------------

// QuoteCheckout implements POST /v1/me/checkout/quote.
func (h handlers) QuoteCheckout(ctx context.Context, req ordersapi.QuoteCheckoutRequestObject) (ordersapi.QuoteCheckoutResponseObject, error) {
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	b := req.Body
	q, err := h.s.quoteCart(ctx, cart{
		storeID: b.StoreId, fulfilment: string(b.Fulfilment), items: b.Items, askReplyID: b.AskReplyId, credits: b.CreditsToUse,
	})
	if err != nil {
		return nil, err
	}
	return ordersapi.QuoteCheckout200JSONResponse(q), nil
}

// CreateOrder implements POST /v1/me/orders.
func (h handlers) CreateOrder(ctx context.Context, req ordersapi.CreateOrderRequestObject) (ordersapi.CreateOrderResponseObject, error) {
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	b := req.Body
	id, err := h.s.create(ctx, cart{
		storeID: b.StoreId, fulfilment: string(b.Fulfilment), items: b.Items, askReplyID: b.AskReplyId, credits: b.CreditsToUse,
	}, b.ExpectedTotalPaise)
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	o, err := h.s.customerOrder(ctx, p.UserID, id, false)
	if err != nil {
		return nil, err
	}
	return ordersapi.CreateOrder201JSONResponse(o), nil
}

// ListMyOrders implements GET /v1/me/orders.
func (h handlers) ListMyOrders(ctx context.Context, req ordersapi.ListMyOrdersRequestObject) (ordersapi.ListMyOrdersResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	l, err := h.s.customerOrders(ctx, p.UserID, req.Params)
	if err != nil {
		return nil, err
	}
	return ordersapi.ListMyOrders200JSONResponse(l), nil
}

// GetMyOrder implements GET /v1/me/orders/{orderId}: the only response that
// carries the pickup code.
func (h handlers) GetMyOrder(ctx context.Context, req ordersapi.GetMyOrderRequestObject) (ordersapi.GetMyOrderResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := pathID(req.OrderId)
	if err != nil {
		return nil, err
	}
	o, err := h.s.customerOrder(ctx, p.UserID, id, true)
	if err != nil {
		return nil, err
	}
	return ordersapi.GetMyOrder200JSONResponse(o), nil
}

// CancelOrder implements POST /v1/me/orders/{orderId}/cancel.
func (h handlers) CancelOrder(ctx context.Context, req ordersapi.CancelOrderRequestObject) (ordersapi.CancelOrderResponseObject, error) {
	o, err := h.customerAction(ctx, req.OrderId, func(u, id ids.ID) error { return h.s.customerCancel(ctx, u, id, req.Body) })
	if err != nil {
		return nil, err
	}
	return ordersapi.CancelOrder200JSONResponse(o), nil
}

// ConfirmPartialOrder implements POST /v1/me/orders/{orderId}/confirm-partial
// (BK-09, BK-10).
func (h handlers) ConfirmPartialOrder(ctx context.Context, req ordersapi.ConfirmPartialOrderRequestObject) (ordersapi.ConfirmPartialOrderResponseObject, error) {
	o, err := h.customerAction(ctx, req.OrderId, func(u, id ids.ID) error { return h.s.confirmPartial(ctx, u, id, req.Body) })
	if err != nil {
		return nil, err
	}
	return ordersapi.ConfirmPartialOrder200JSONResponse(o), nil
}

// ExtendOrder implements POST /v1/me/orders/{orderId}/extend.
func (h handlers) ExtendOrder(ctx context.Context, req ordersapi.ExtendOrderRequestObject) (ordersapi.ExtendOrderResponseObject, error) {
	o, err := h.customerAction(ctx, req.OrderId, func(u, id ids.ID) error { return h.s.extend(ctx, u, id) })
	if err != nil {
		return nil, err
	}
	return ordersapi.ExtendOrder200JSONResponse(o), nil
}

// NewOrderCode implements POST /v1/me/orders/{orderId}/new-code. The new
// code is read with GET /v1/me/orders/{orderId}.
func (h handlers) NewOrderCode(ctx context.Context, req ordersapi.NewOrderCodeRequestObject) (ordersapi.NewOrderCodeResponseObject, error) {
	o, err := h.customerAction(ctx, req.OrderId, func(u, id ids.ID) error { return h.s.newCode(ctx, u, id) })
	if err != nil {
		return nil, err
	}
	return ordersapi.NewOrderCode200JSONResponse(o), nil
}

// --- staff ------------------------------------------------------------------------

// ListStoreOrders implements GET {S}/orders.
func (h handlers) ListStoreOrders(ctx context.Context, req ordersapi.ListStoreOrdersRequestObject) (ordersapi.ListStoreOrdersResponseObject, error) {
	_, storeID, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	l, err := h.s.storeOrders(ctx, storeID, req.Params)
	if err != nil {
		return nil, err
	}
	return ordersapi.ListStoreOrders200JSONResponse(l), nil
}

// GetStoreOrder implements GET {S}/orders/{orderId}.
func (h handlers) GetStoreOrder(ctx context.Context, req ordersapi.GetStoreOrderRequestObject) (ordersapi.GetStoreOrderResponseObject, error) {
	_, storeID, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	id, err := pathID(req.OrderId)
	if err != nil {
		return nil, err
	}
	o, err := h.s.storeOrder(ctx, storeID, id)
	if err != nil {
		return nil, err
	}
	return ordersapi.GetStoreOrder200JSONResponse(o), nil
}

// AcceptOrder implements POST {S}/orders/{orderId}/accept.
func (h handlers) AcceptOrder(ctx context.Context, req ordersapi.AcceptOrderRequestObject) (ordersapi.AcceptOrderResponseObject, error) {
	o, err := h.staffAction(ctx, req.OrderId, func(by, sid, id ids.ID) error { return h.s.accept(ctx, by, sid, id, req.Body) })
	if err != nil {
		return nil, err
	}
	return ordersapi.AcceptOrder200JSONResponse(o), nil
}

// RejectOrder implements POST {S}/orders/{orderId}/reject.
func (h handlers) RejectOrder(ctx context.Context, req ordersapi.RejectOrderRequestObject) (ordersapi.RejectOrderResponseObject, error) {
	o, err := h.staffAction(ctx, req.OrderId, func(by, sid, id ids.ID) error { return h.s.reject(ctx, by, sid, id, req.Body) })
	if err != nil {
		return nil, err
	}
	return ordersapi.RejectOrder200JSONResponse(o), nil
}

// ReadyOrder implements POST {S}/orders/{orderId}/ready.
func (h handlers) ReadyOrder(ctx context.Context, req ordersapi.ReadyOrderRequestObject) (ordersapi.ReadyOrderResponseObject, error) {
	o, err := h.staffAction(ctx, req.OrderId, func(by, sid, id ids.ID) error { return h.s.markReady(ctx, by, sid, id) })
	if err != nil {
		return nil, err
	}
	return ordersapi.ReadyOrder200JSONResponse(o), nil
}

// VerifyOrderCode implements POST {S}/orders/{orderId}/verify-code.
func (h handlers) VerifyOrderCode(ctx context.Context, req ordersapi.VerifyOrderCodeRequestObject) (ordersapi.VerifyOrderCodeResponseObject, error) {
	o, err := h.staffAction(ctx, req.OrderId, func(by, sid, id ids.ID) error { return h.s.verify(ctx, by, sid, id, req.Body) })
	if err != nil {
		return nil, err
	}
	return ordersapi.VerifyOrderCode200JSONResponse(o), nil
}

// CancelStoreOrder implements POST {S}/orders/{orderId}/cancel.
func (h handlers) CancelStoreOrder(ctx context.Context, req ordersapi.CancelStoreOrderRequestObject) (ordersapi.CancelStoreOrderResponseObject, error) {
	o, err := h.staffAction(ctx, req.OrderId, func(by, sid, id ids.ID) error { return h.s.storeCancel(ctx, by, sid, id, req.Body) })
	if err != nil {
		return nil, err
	}
	return ordersapi.CancelStoreOrder200JSONResponse(o), nil
}

// DispatchOrder implements POST {S}/orders/{orderId}/dispatch. Delivery is
// not built yet (BK-23–25): no order is a delivery order, so it is 409.
func (h handlers) DispatchOrder(ctx context.Context, req ordersapi.DispatchOrderRequestObject) (ordersapi.DispatchOrderResponseObject, error) {
	_, err := h.staffAction(ctx, req.OrderId, func(_, sid, id ids.ID) error { return h.s.notBuiltStore(ctx, sid, id) })
	return nil, err
}

// FailOrderDelivery implements POST {S}/orders/{orderId}/fail-delivery (409
// until delivery is built).
func (h handlers) FailOrderDelivery(ctx context.Context, req ordersapi.FailOrderDeliveryRequestObject) (ordersapi.FailOrderDeliveryResponseObject, error) {
	_, err := h.staffAction(ctx, req.OrderId, func(_, sid, id ids.ID) error { return h.s.notBuiltStore(ctx, sid, id) })
	return nil, err
}

// ConfirmOrderReturn implements POST {S}/orders/{orderId}/confirm-return
// (409 until delivery and D-69 returns are built).
func (h handlers) ConfirmOrderReturn(ctx context.Context, req ordersapi.ConfirmOrderReturnRequestObject) (ordersapi.ConfirmOrderReturnResponseObject, error) {
	_, err := h.staffAction(ctx, req.OrderId, func(_, sid, id ids.ID) error { return h.s.notBuiltStore(ctx, sid, id) })
	return nil, err
}

// --- not built yet ------------------------------------------------------------

// notBuiltStore answers a delivery or return action: 404 for an order the
// store cannot see, else 409 invalid_transition (no order can be in the
// state the action needs yet).
func (s *Service) notBuiltStore(ctx context.Context, storeID, orderID ids.ID) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	o, err := s.q(ctx).GetOrder(ctx, ids.UUID(orderID))
	if db.IsNoRows(err) || (err == nil && mustID(o.StoreID) != storeID) {
		return notFound()
	}
	if err != nil {
		return problem.Wrap(genproblems.Internal, err)
	}
	return invalidTransition()
}

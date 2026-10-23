package policy

import "github.com/todayz/backend/internal/platform/authz"

// Orders actions (W2-02, 02 §3.3 "orders" rows). The store stream keeps its
// own action (store.stream, notify.go) with the same grants as order.view:
// the lead kept them separate. The admin rows (admin.order.view,
// admin.order.cancel) arrive with the admin console's order screens.
const (
	// KindCustomer: the signed-in customer (/v1/me), on their own orders.
	MeOrdersView   = "me.orders.view"   // list, detail
	MeOrdersPlace  = "me.orders.place"  // quote, place
	MeOrdersManage = "me.orders.manage" // cancel, extend, new code, confirm partial

	// KindBusiness: by role in {bid} with access to {sid}.
	OrderView          = "order.view"
	OrderAccept        = "order.accept"
	OrderReject        = "order.reject"
	OrderReady         = "order.ready"
	OrderDispatch      = "order.dispatch"
	OrderHandover      = "order.handover"
	OrderFailDelivery  = "order.fail_delivery"
	OrderConfirmReturn = "order.confirm_return"
	OrderCancel        = "order.cancel"
)

func init() {
	staff := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow, authz.Cashier: authz.Allow}
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	Register(authz.Table{Domain: "orders", Actions: []authz.Action{
		{Name: MeOrdersView, Kind: authz.KindCustomer, Doc: "GET /v1/me/orders, /v1/me/orders/{orderId} (C-12, C-13): own orders only"},
		{Name: MeOrdersPlace, Kind: authz.KindCustomer, Doc: "POST /v1/me/checkout/quote, POST /v1/me/orders (C-11)"},
		{Name: MeOrdersManage, Kind: authz.KindCustomer, Doc: "POST /v1/me/orders/{orderId}/cancel|extend|new-code|confirm-partial: own orders only"},

		{Name: OrderView, Kind: authz.KindBusiness, Grants: staff, Doc: "GET {S}/orders, {S}/orders/{orderId} (B-04)"},
		{Name: OrderAccept, Kind: authz.KindBusiness, Grants: staff, Doc: "POST {S}/orders/{orderId}/accept, full or partial"},
		{Name: OrderReject, Kind: authz.KindBusiness, Grants: staff, Doc: "POST {S}/orders/{orderId}/reject with a RejectReason"},
		{Name: OrderReady, Kind: authz.KindBusiness, Grants: staff, Doc: "POST {S}/orders/{orderId}/ready (pickup)"},
		{Name: OrderDispatch, Kind: authz.KindBusiness, Grants: staff, Doc: "POST {S}/orders/{orderId}/dispatch (delivery)"},
		{Name: OrderHandover, Kind: authz.KindBusiness, Grants: staff, Doc: "POST {S}/orders/{orderId}/verify-code: pickup handover or delivery"},
		{Name: OrderFailDelivery, Kind: authz.KindBusiness, Grants: staff, Doc: "POST {S}/orders/{orderId}/fail-delivery with a reason"},
		{Name: OrderConfirmReturn, Kind: authz.KindBusiness, Grants: staff, Doc: "POST {S}/orders/{orderId}/confirm-return (D-69); damage needs stock.damage"},
		{Name: OrderCancel, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "POST {S}/orders/{orderId}/cancel with a ShopCancelReason (D-68)"},
	}})
}

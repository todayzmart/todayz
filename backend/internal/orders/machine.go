package orders

// The order statuses (03 §4.1).
const (
	StatusPlaced           = "placed"
	StatusAwaitingCustomer = "awaiting_customer"
	StatusAccepted         = "accepted"
	StatusReady            = "ready"
	StatusOutForDelivery   = "out_for_delivery"
	StatusCollected        = "collected"
	StatusDelivered        = "delivered"
	StatusRejected         = "rejected"
	StatusExpired          = "expired"
	StatusCancelled        = "cancelled"
	StatusDeliveryFailed   = "delivery_failed"
)

// Fulfilments.
const (
	Pickup   = "pickup"
	Delivery = "delivery"
)

// Statuses lists every status, in the 03 §4.1 order.
func Statuses() []string {
	return []string{
		StatusPlaced, StatusAwaitingCustomer, StatusAccepted, StatusReady, StatusOutForDelivery,
		StatusCollected, StatusDelivered, StatusRejected, StatusExpired, StatusCancelled, StatusDeliveryFailed,
	}
}

// openStatuses are the statuses of an order that is not finished.
var openStatuses = []string{StatusPlaced, StatusAwaitingCustomer, StatusAccepted, StatusReady, StatusOutForDelivery}

// pastStatuses are the finished ones.
var pastStatuses = []string{StatusCollected, StatusDelivered, StatusRejected, StatusExpired, StatusCancelled, StatusDeliveryFailed}

// next is the transition table, the same as trg_orders_guard's.
var next = map[string]map[string]bool{
	StatusPlaced:           {StatusAccepted: true, StatusAwaitingCustomer: true, StatusRejected: true, StatusExpired: true, StatusCancelled: true},
	StatusAwaitingCustomer: {StatusAccepted: true, StatusCancelled: true},
	StatusAccepted:         {StatusReady: true, StatusCollected: true, StatusExpired: true, StatusCancelled: true, StatusOutForDelivery: true},
	StatusReady:            {StatusCollected: true, StatusExpired: true, StatusCancelled: true},
	StatusOutForDelivery:   {StatusDelivered: true, StatusDeliveryFailed: true, StatusCancelled: true},
}

// CanMove reports whether an order of fulfilment may go from one status to
// another (03 §4.1). The database trigger trg_orders_guard enforces the same
// table; TestMachineMatchesTheGuard checks the two agree on every pair.
func CanMove(from, to, fulfilment string) bool {
	if fulfilment != Pickup && fulfilment != Delivery {
		return false
	}
	if !next[from][to] {
		return false
	}
	switch to {
	case StatusReady, StatusCollected:
		if fulfilment != Pickup {
			return false
		}
	case StatusOutForDelivery, StatusDelivered:
		if fulfilment != Delivery {
			return false
		}
	}
	// Expiry from ready is a pickup no-show only (D-21); ready is pickup
	// only anyway, so this never refuses a reachable pair.
	return from != StatusReady || to != StatusExpired || fulfilment == Pickup
}

// handable reports whether a pickup order can be handed over now.
func handable(status string) bool { return status == StatusAccepted || status == StatusReady }

// codeShown reports whether the customer is shown the hand-over code: while
// a pickup order can be collected, or a delivery order is accepted or on its
// way (03 §4.1).
func codeShown(status string) bool { return handable(status) || status == StatusOutForDelivery }

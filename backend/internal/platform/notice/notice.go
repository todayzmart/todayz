// Package notice is the cross-domain seam of the notify domain (W2-05): the
// types a domain hands to the notification sender and the interfaces the
// notify domain needs from others. Domain packages never import each other
// (backend/CLAUDE.md), so the payload and the small interfaces live here and
// cmd/todayz wires the implementations.
//
//   - A domain that notifies (orders W2-02, asks W2-09, reviews, payouts)
//     takes a Sender and calls Send inside its own transaction: the inbox row,
//     its stream event and any SMS job commit or roll back with the change.
//   - internal/notify implements Sender and runs the "new order not seen" SMS
//     (01 §15) through UnseenOrders (W2-02 implements it) and StoreContacts
//     (W1-04 implements it).
package notice

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/todayz/backend/internal/platform/ids"
)

// Channel is an outside channel. The in-app inbox is always written and has
// no switch (01 §15).
type Channel string

// The outside channels (notify_<channel>_enabled).
const (
	Push     Channel = "push"
	SMS      Channel = "sms"
	WhatsApp Channel = "whatsapp"
)

// Channels lists the outside channels.
func Channels() []Channel { return []Channel{Push, SMS, WhatsApp} }

// Kinds of notification (01 §15). The notify domain's catalogue holds each
// kind's app, switchable channels and text per language; a kind not in the
// catalogue is refused by Send.
const (
	// Business inbox.
	KindNewOrder    = "new_order"    // a new order for the store (push + in-app)
	KindOrderUnseen = "order_unseen" // the new order was not seen in order_unseen_sms_after_min (SMS)
	KindNewAsk      = "new_ask"      // a customer ask for the store (push + in-app; D-101)
	// Customer inbox.
	KindOrderUpdate = "order_update" // accepted, partial, rejected, expired, ready, out for delivery, delivered
	KindAskReply    = "ask_reply"    // a shop replied Yes to the customer's ask (D-101)
)

// Ref is what a notification opens.
type Ref struct {
	Type string // "order", "ask", "review", …: ^[a-z][a-z_]{0,30}$
	ID   ids.ID
}

// Outside is one message on an outside channel. Phone is E.164; it is sealed
// into the delivery row and never logged, put in a metric or returned in an
// error.
type Outside struct {
	Channel Channel
	Phone   string
	// Lang is the recipient's language (en, hi, kn, te, ta); anything else is
	// English.
	Lang string
}

// Notification is one inbox row and its outside messages. Exactly one of
// UserID (the customer inbox, C-20) and BusinessID (the business inbox,
// B-16) is set; StoreID, with BusinessID, says which store the row is about.
type Notification struct {
	Kind       string
	UserID     ids.ID
	BusinessID ids.ID
	StoreID    ids.ID
	// Params fill the kind's text ({store}, {minutes}). They are stored and
	// rendered on read, so they never hold a phone, an order or pickup code,
	// an address or a token.
	Params map[string]string
	Ref    *Ref
	// Outside messages. Each is sent only when its channel's setting is on,
	// its provider key is set, the kind allows the channel and PrefUser has
	// not switched it off.
	Outside []Outside
	// PrefUser is whose switches gate Outside: UserID by default; for a
	// business row, the owner (Contact.OwnerUserID).
	PrefUser ids.ID
}

// Sender writes a notification inside the caller's transaction and returns
// its id.
type Sender interface {
	Send(ctx context.Context, tx pgx.Tx, n Notification) (ids.ID, error)
}

// UnseenOrder is a new order no staff device has opened yet.
type UnseenOrder struct {
	OrderID   ids.ID
	StoreID   ids.ID
	CreatedAt time.Time
}

// UnseenQuery is one page of UnseenOrders.Unseen.
type UnseenQuery struct {
	// CreatedBefore: only orders created at or before it.
	CreatedBefore time.Time
	// After is the last order of the previous page (keyset on CreatedAt,
	// then OrderID); nil for the first page. Paging matters: an order that
	// stays unseen after its SMS keeps matching until it leaves the
	// "waiting for the store" state, and without a cursor a backlog of such
	// orders would hide every newer one behind the page limit.
	After *UnseenOrder
	Limit int
}

// UnseenOrders lists new orders not seen by the store (W2-02 implements it).
type UnseenOrders interface {
	// Unseen returns orders still waiting for the store (status placed, not
	// expired or cancelled) that no staff device has seen, matching q,
	// ordered by (CreatedAt, OrderID), at most q.Limit.
	Unseen(ctx context.Context, q UnseenQuery) ([]UnseenOrder, error)
}

// Contact is who the "not seen" SMS goes to (01 §15).
type Contact struct {
	BusinessID ids.ID
	// Phone is the store's contact phone, else the owner's login phone
	// (E.164). Empty when neither exists: the inbox row is still written.
	Phone string
	// Lang is the business's notification language; "" means English.
	Lang string
	// OwnerUserID is whose switches gate the SMS (zero: no switch applies).
	OwnerUserID ids.ID
	// StoreName fills {store}; it is the shop's public name, not PII.
	StoreName string
}

// StoreContacts finds a store's escalation contact (W1-04 implements it).
type StoreContacts interface {
	// Contact returns the store's contact. ok is false for an unknown store.
	Contact(ctx context.Context, storeID ids.ID) (c Contact, ok bool, err error)
}

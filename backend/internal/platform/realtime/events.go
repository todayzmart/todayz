// Package realtime is the SSE hub of 02 §5 (W2-05): a domain publishes a
// small event inside its own transaction, Postgres LISTEN/NOTIFY fans it out
// to every API instance, and each instance writes it to the open streams of
// that channel. Clients refetch through their queries; an event says what
// changed ({type, id, version}), never the data.
//
// Delivery is at least once per connection and gap-free across reconnects:
// the stream's `id` is a cursor into outbox_events (see cursor.go), and a
// client that resumes with Last-Event-ID gets every event it may have missed,
// or `event: reset` (refetch everything) when the cursor is older than the
// 24 h retention.
package realtime

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/todayz/backend/internal/platform/ids"
)

// ChannelKind is who a channel belongs to.
type ChannelKind string

// The three channel kinds of 02 §5.
const (
	KindStore ChannelKind = "store" // a store's staff: the business app (B-04, B-20)
	KindUser  ChannelKind = "user"  // one customer: the customer app (C-12, C-25)
	KindAdmin ChannelKind = "admin" // the admin console (ops live counters)
)

// Channel is a logical stream: "store:<ulid>", "user:<ulid>" or "admin".
type Channel string

// StoreChannel is a store's channel.
func StoreChannel(id ids.ID) Channel { return Channel("store:" + id.String()) }

// UserChannel is a customer's channel.
func UserChannel(id ids.ID) Channel { return Channel("user:" + id.String()) }

// AdminChannel is the one admin channel.
const AdminChannel Channel = "admin"

// Kind reports the channel's kind and whether the channel is well formed
// (the same shape outbox_events' CHECK constraint accepts).
func (c Channel) Kind() (ChannelKind, bool) {
	if c == AdminChannel {
		return KindAdmin, true
	}
	k, id, ok := strings.Cut(string(c), ":")
	if !ok || (k != string(KindStore) && k != string(KindUser)) {
		return "", false
	}
	if p, err := ids.Parse(id); err != nil || p.String() != id || p == ids.Zero {
		return "", false
	}
	return ChannelKind(k), true
}

// Type is an event type: "<noun>.<verb>", lower case.
type Type string

// The event types every app knows. A domain that needs another registers it
// with Register in an init function of its own package.
const (
	// OrderCreated: a new order for the store (B-04 toast and sound). Store.
	OrderCreated Type = "order.created"
	// OrderUpdated: an order's status or lines changed (any transition, a
	// partial accept, an extension). Customer and store.
	OrderUpdated Type = "order.updated"
	// AskUpdated: a shop replied to, or the customer's ask closed (D-101).
	// Customer, data {askId, version} through Event.ID and Event.Version.
	AskUpdated Type = "ask.updated"
	// AskCreated: a new ask for the store's inbox (B-20, D-101). Store.
	AskCreated Type = "ask.created"
	// AskClosed: an ask the store saw is closed (D-101). Store.
	AskClosed Type = "ask.closed"
	// InboxUpdated: a row was added to the user's or the business's in-app
	// inbox (C-20, B-16); the client refetches the list and unread count.
	InboxUpdated Type = "inbox.updated"
	// PulseUpdated: the admin live counters changed (A-01 pulse). Admin.
	PulseUpdated Type = "pulse.updated"
)

var reType = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}\.[a-z][a-z0-9_]{0,30}$`)

// reserved are the SSE event names the stream itself uses.
var reserved = map[Type]bool{"ready": true, "reset": true, "reconnect": true}

var (
	regMu    sync.RWMutex
	registry = map[Type]map[ChannelKind]bool{}
)

func init() {
	Register(OrderCreated, KindStore)
	Register(OrderUpdated, KindUser, KindStore)
	Register(AskUpdated, KindUser)
	Register(AskCreated, KindStore)
	Register(AskClosed, KindStore)
	Register(InboxUpdated, KindUser, KindStore)
	Register(PulseUpdated, KindAdmin)
}

// Register adds an event type and the channel kinds it may be published on.
// It panics on a bad name, a reserved name, no kinds or a second registration
// (two domains must never define the same type).
func Register(t Type, kinds ...ChannelKind) {
	if !reType.MatchString(string(t)) || reserved[t] {
		panic(fmt.Sprintf("realtime: bad event type %q", t))
	}
	if len(kinds) == 0 {
		panic(fmt.Sprintf("realtime: event type %q has no channel kind", t))
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[t]; dup {
		panic(fmt.Sprintf("realtime: event type %q registered twice", t))
	}
	m := map[ChannelKind]bool{}
	for _, k := range kinds {
		if k != KindStore && k != KindUser && k != KindAdmin {
			panic(fmt.Sprintf("realtime: event type %q: bad channel kind %q", t, k))
		}
		m[k] = true
	}
	registry[t] = m
}

// Types lists the registered event types and their channel kinds, sorted
// (for the frontend registry test and the docs).
func Types() map[Type][]ChannelKind {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make(map[Type][]ChannelKind, len(registry))
	for t, ks := range registry {
		l := make([]ChannelKind, 0, len(ks))
		for k := range ks {
			l = append(l, k)
		}
		sort.Slice(l, func(i, j int) bool { return l[i] < l[j] })
		out[t] = l
	}
	return out
}

// Event is what a stream carries: which entity changed and its new version.
// It is never the entity's data (02 §5); the client refetches.
type Event struct {
	Type    Type   `json:"type"`
	ID      ids.ID `json:"id"`
	Version int64  `json:"version"`
}

// Errors from Publish's validation.
var (
	ErrUnknownType = errors.New("realtime: unknown event type")
	ErrBadChannel  = errors.New("realtime: bad channel")
	ErrWrongKind   = errors.New("realtime: event type not allowed on this channel kind")
	ErrBadEvent    = errors.New("realtime: event needs an id and a version >= 0")
)

// Validate checks ev against the registry for channel c.
func Validate(c Channel, ev Event) error {
	kind, ok := c.Kind()
	if !ok {
		return ErrBadChannel
	}
	regMu.RLock()
	kinds, known := registry[ev.Type]
	regMu.RUnlock()
	if !known {
		return fmt.Errorf("%w: %q", ErrUnknownType, ev.Type)
	}
	if !kinds[kind] {
		return fmt.Errorf("%w: %q on %s", ErrWrongKind, ev.Type, kind)
	}
	if ev.ID == ids.Zero || ev.Version < 0 {
		return ErrBadEvent
	}
	return nil
}

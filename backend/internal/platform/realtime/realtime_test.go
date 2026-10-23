package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

func newID() ids.ID { return ids.New(clock.Real{}.Now()) }

func TestParseCursor(t *testing.T) {
	for _, tc := range []struct {
		in string
		v  uint64
		ok bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"1", 1, true},
		{"48213", 48213, true},
		{"18446744073709551615", 18446744073709551615, true},
		{"18446744073709551616", 0, false},  // overflows uint64
		{"123456789012345678901", 0, false}, // longer than an xid8
		{"-1", 0, false},
		{"+1", 0, false},
		{" 1", 0, false},
		{"1e3", 0, false},
		{"abc", 0, false},
	} {
		v, ok := parseCursor(tc.in)
		require.Equal(t, tc.ok, ok, tc.in)
		require.Equal(t, tc.v, v, tc.in)
	}
	require.Equal(t, "48213", formatCursor(48213))
}

func TestChannelKind(t *testing.T) {
	id := newID()
	k, ok := UserChannel(id).Kind()
	require.True(t, ok)
	require.Equal(t, KindUser, k)
	k, ok = StoreChannel(id).Kind()
	require.True(t, ok)
	require.Equal(t, KindStore, k)
	k, ok = AdminChannel.Kind()
	require.True(t, ok)
	require.Equal(t, KindAdmin, k)

	for _, bad := range []Channel{"", "admin:1", "store:", "store:" + Channel(strings.ToLower(id.String())),
		"user:" + Channel(ids.Zero.String()), "order:" + Channel(id.String()), "user:" + Channel(id.String()) + "x"} {
		_, ok := bad.Kind()
		require.False(t, ok, string(bad))
	}
}

func TestRegistry(t *testing.T) {
	types := Types()
	// The event registry of the weekend slice (W2-02, W2-09, W2-10 publish these).
	require.Equal(t, []ChannelKind{KindStore}, types[OrderCreated])
	require.Equal(t, []ChannelKind{KindStore, KindUser}, types[OrderUpdated])
	require.Equal(t, []ChannelKind{KindUser}, types[AskUpdated])
	require.Equal(t, []ChannelKind{KindStore}, types[AskCreated])
	require.Equal(t, []ChannelKind{KindStore}, types[AskClosed])
	require.Equal(t, []ChannelKind{KindStore, KindUser}, types[InboxUpdated])
	require.Equal(t, []ChannelKind{KindAdmin}, types[PulseUpdated])

	require.Panics(t, func() { Register(OrderCreated, KindStore) }, "a second registration")
	require.Panics(t, func() { Register("ready", KindStore) }, "a stream event name")
	require.Panics(t, func() { Register("Order.Created", KindStore) }, "upper case")
	require.Panics(t, func() { Register("nodot", KindStore) })
	require.Panics(t, func() { Register("test.nokind") })
	require.Panics(t, func() { Register("test.badkind", "orders") })
	Register("test.registered", KindUser)
	require.Equal(t, []ChannelKind{KindUser}, Types()["test.registered"])
}

func TestValidate(t *testing.T) {
	id := newID()
	u, s := UserChannel(id), StoreChannel(id)
	require.NoError(t, Validate(u, Event{Type: OrderUpdated, ID: id, Version: 1}))
	require.NoError(t, Validate(s, Event{Type: OrderCreated, ID: id, Version: 0}))
	require.ErrorIs(t, Validate("store:x", Event{Type: OrderCreated, ID: id}), ErrBadChannel)
	require.ErrorIs(t, Validate(u, Event{Type: "order.nope", ID: id}), ErrUnknownType)
	require.ErrorIs(t, Validate(u, Event{Type: OrderCreated, ID: id}), ErrWrongKind, "order.created is for the store")
	require.ErrorIs(t, Validate(s, Event{Type: AskUpdated, ID: id}), ErrWrongKind, "ask.updated is for the customer")
	require.ErrorIs(t, Validate(AdminChannel, Event{Type: OrderUpdated, ID: id}), ErrWrongKind)
	require.ErrorIs(t, Validate(u, Event{Type: OrderUpdated}), ErrBadEvent)
	require.ErrorIs(t, Validate(u, Event{Type: OrderUpdated, ID: id, Version: -1}), ErrBadEvent)
	require.ErrorIs(t, Publish(context.Background(), nil, u, Event{Type: OrderUpdated, ID: id}), ErrNoTx)
	require.ErrorIs(t, PublishCtx(context.Background(), u, Event{Type: OrderUpdated, ID: id}), ErrNoTx)
}

// notifyPayload is what publishSQL sends for ev on c with snapshot xmin x.
func notifyPayload(t *testing.T, seq int64, c Channel, ev Event, x uint64) string {
	t.Helper()
	e, err := json.Marshal(ev)
	require.NoError(t, err)
	b, err := json.Marshal(notification{Seq: seq, Channel: c, Event: e, X: fmt.Sprint(x), T: 1})
	require.NoError(t, err)
	return string(b)
}

func TestHubFansOutToItsChannelOnly(t *testing.T) {
	h := NewHub(Options{})
	a, b := StoreChannel(newID()), StoreChannel(newID())
	sa, err := h.Subscribe(a)
	require.NoError(t, err)
	sb, err := h.Subscribe(b)
	require.NoError(t, err)
	require.Equal(t, 2, h.Len())

	ev := Event{Type: OrderCreated, ID: newID(), Version: 1}
	h.Notify(context.Background(), notifyPayload(t, 1, a, ev, 900))
	require.Len(t, sa.q, 1)
	require.Empty(t, sb.q)
	d := <-sa.q
	require.Equal(t, uint64(900), d.x)
	require.Equal(t, `event: order.created`+"\n"+`data: {"type":"order.created","id":"`+ev.ID.String()+`","version":1}`+"\n", string(d.frame))
	require.Equal(t, uint64(900), h.Mark())

	// An unusable event still moves the mark; a lower x never lowers it.
	h.Notify(context.Background(), notifyPayload(t, 2, "bogus", ev, 950))
	require.Equal(t, uint64(950), h.Mark())
	h.Notify(context.Background(), notifyPayload(t, 3, a, ev, 10))
	require.Equal(t, uint64(950), h.Mark())
	// Garbage and a payload without a cursor are ignored.
	h.Notify(context.Background(), "{")
	h.Notify(context.Background(), `{"s":4,"c":"admin","e":{},"x":"nope"}`)
	require.Equal(t, uint64(950), h.Mark())

	h.Unsubscribe(sa)
	require.Equal(t, ReasonCancelled, sa.Reason())
	require.Equal(t, 1, h.Len())
	h.Unsubscribe(sa) // twice is harmless
	require.Equal(t, 1, h.Len())
}

func TestHubDropsSlowConsumer(t *testing.T) {
	h := NewHub(Options{Buffer: 2})
	c := UserChannel(newID())
	slow, err := h.Subscribe(c)
	require.NoError(t, err)
	fast, err := h.Subscribe(c)
	require.NoError(t, err)
	ev := Event{Type: OrderUpdated, ID: newID(), Version: 1}
	for i := 0; i < 3; i++ {
		h.Notify(context.Background(), notifyPayload(t, int64(i), c, ev, uint64(100+i)))
		if len(fast.q) > 0 {
			<-fast.q // the fast reader keeps up
		}
	}
	select {
	case <-slow.Done():
	default:
		t.Fatal("the slow subscriber was not dropped")
	}
	require.Equal(t, ReasonOverflow, slow.Reason())
	select {
	case <-fast.Done():
		t.Fatal("the fast subscriber was dropped too")
	default:
	}
	require.Equal(t, 1, h.Len())
}

func TestHubLimitsAndShutdown(t *testing.T) {
	h := NewHub(Options{MaxPerChannel: 2, MaxStreams: 3})
	c1, c2 := UserChannel(newID()), UserChannel(newID())
	_, err := h.Subscribe("nope")
	require.ErrorIs(t, err, ErrBadChannel)
	s1, err := h.Subscribe(c1)
	require.NoError(t, err)
	_, err = h.Subscribe(c1)
	require.NoError(t, err)
	_, err = h.Subscribe(c1)
	require.ErrorIs(t, err, ErrTooMany, "per channel")
	s3, err := h.Subscribe(c2)
	require.NoError(t, err)
	_, err = h.Subscribe(c2)
	require.ErrorIs(t, err, ErrTooMany, "per instance")

	require.NoError(t, h.Shutdown())
	for _, s := range []*Subscriber{s1, s3} {
		<-s.Done()
		require.Equal(t, ReasonShutdown, s.Reason())
	}
	require.Zero(t, h.Len())
	_, err = h.Subscribe(c1)
	require.ErrorIs(t, err, ErrShuttingDown)
}

func TestReconnectFrame(t *testing.T) {
	require.Equal(t, "event: reconnect\nretry: 1000\nid: 42\ndata: {\"reason\":\"shutdown\"}\n\n", string(reconnectFrame(42, ReasonShutdown)))
	require.Equal(t, "event: reconnect\nretry: 1000\ndata: {\"reason\":\"revoked\"}\n\n", string(reconnectFrame(0, ReasonRevoked)))
}

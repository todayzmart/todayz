package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/pgnotify"
	"github.com/todayz/backend/internal/platform/realtime"
	"github.com/todayz/backend/internal/platform/testclock"
	"github.com/todayz/backend/internal/testkit"
)

// streamAPI is bizAPI with the realtime hub on its own LISTEN connection and
// the router behind a real HTTP server, so a stream runs through the whole
// middleware pipeline (flush, write deadlines, authn, authz) as in the API.
type streamAPI struct {
	*bizAPI
	d   deps
	srv *httptest.Server
	hub *realtime.Hub
}

// streamTestHTTP reaches the loopback test server only; a stream stays open
// past the default request timeout.
var streamTestHTTP = httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true, Timeout: time.Hour})

func newStreamAPI(t *testing.T, recheck time.Duration) *streamAPI {
	t.Helper()
	tdb := testkit.NewDB(t)
	e := localEnv(tdb)
	cfg, err := config.Load(env(e), nil)
	require.NoError(t, err)
	clk := clock.NewFake(time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	l := pgnotify.New(tdb.Pool, log)
	hub := realtime.NewHub(realtime.Options{Log: log})
	hub.Attach(l)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = l.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	select {
	case <-l.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("no LISTEN connection")
	}

	old := storeStreamRecheck
	storeStreamRecheck = recheck
	t.Cleanup(func() { storeStreamRecheck = old })

	d := deps{
		Config: cfg, Log: log, Clock: clk,
		DB: db.New(tdb.Pool), Ready: &health.Readiness{},
		TestClock: testclock.New(clock.NewOffset(nil), nopStore{}, log),
		Realtime:  hub,
	}
	h, err := buildRouter(d, idempotency.NewMemoryStore(), contractValidator(t))
	require.NoError(t, err)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	// The hub ends every stream first, so srv.Close does not wait on them.
	t.Cleanup(func() { _ = hub.Shutdown() })
	return &streamAPI{bizAPI: &bizAPI{t: t, h: h, clk: clk, tdb: tdb}, d: d, srv: srv, hub: hub}
}

// sseBlock is one block of an event stream (comments are skipped).
type sseBlock struct{ Event, ID, Data, Retry string }

type sseStream struct {
	code   int
	raw    string // the body of a refused request
	blocks chan sseBlock
	done   chan struct{}
}

// open opens path as token's stream. A refused request (not 200) comes back
// with its body read and no blocks.
func (a *streamAPI) open(token, path string) *sseStream {
	a.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, a.srv.URL+path, nil)
	require.NoError(a.t, err)
	req.Host = "api.todayz.localhost"
	req.Header.Set("Accept", "text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := streamTestHTTP.Do(req)
	require.NoError(a.t, err)
	s := &sseStream{code: res.StatusCode, blocks: make(chan sseBlock, 64), done: make(chan struct{})}
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		s.raw = string(b)
		close(s.done)
		return s
	}
	require.Equal(a.t, "text/event-stream", res.Header.Get("Content-Type"))
	a.t.Cleanup(func() { _ = res.Body.Close() })
	go func() {
		defer close(s.done)
		br := bufio.NewReader(res.Body)
		var b sseBlock
		filled := false
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSuffix(line, "\n")
			switch {
			case line == "":
				if filled {
					s.blocks <- b
				}
				b, filled = sseBlock{}, false
				continue
			case strings.HasPrefix(line, ":"):
				continue
			case strings.HasPrefix(line, "event: "):
				b.Event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "id: "):
				b.ID = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "data: "):
				b.Data = strings.TrimPrefix(line, "data: ")
			case strings.HasPrefix(line, "retry: "):
				b.Retry = strings.TrimPrefix(line, "retry: ")
			}
			filled = true
		}
	}()
	return s
}

// next returns the next block with an event name (id-only heartbeat blocks
// are skipped).
func (s *sseStream) next(t *testing.T) sseBlock {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case b := <-s.blocks:
			if b.Event == "" && b.Retry == "" {
				continue
			}
			return b
		case <-s.done:
			select {
			case b := <-s.blocks:
				return b
			default:
			}
			t.Fatal("stream ended")
		case <-deadline:
			t.Fatal("no block in time")
		}
	}
}

func (s *sseStream) ready(t *testing.T) {
	t.Helper()
	require.Equal(t, http.StatusOK, s.code, s.raw)
	require.Equal(t, "3000", s.next(t).Retry)
	b := s.next(t)
	require.Equal(t, "ready", b.Event)
	require.JSONEq(t, `{"resumed":false}`, b.Data)
}

// event waits for an event of type typ and returns its payload.
func (s *sseStream) event(t *testing.T, typ realtime.Type) realtime.Event {
	t.Helper()
	b := s.next(t)
	require.Equal(t, string(typ), b.Event, b.Data)
	require.NotEmpty(t, b.ID, "a live event carries the cursor")
	var ev realtime.Event
	require.NoError(t, json.Unmarshal([]byte(b.Data), &ev))
	return ev
}

func (a *streamAPI) publish(c realtime.Channel, ev realtime.Event) {
	a.t.Helper()
	ctx := context.Background()
	tx, err := a.tdb.Pool.Begin(ctx)
	require.NoError(a.t, err)
	require.NoError(a.t, realtime.Publish(ctx, tx, c, ev))
	require.NoError(a.t, tx.Commit(ctx))
}

func (a *streamAPI) send(n notice.Notification) ids.ID {
	a.t.Helper()
	ctx := context.Background()
	tx, err := a.tdb.Pool.Begin(ctx)
	require.NoError(a.t, err)
	id, err := newNotifyService(&a.d).Send(ctx, tx, n)
	require.NoError(a.t, err)
	require.NoError(a.t, tx.Commit(ctx))
	return id
}

// TestStoreStreamHTTP: the store stream through the real pipeline (W2-05,
// 02 §5): the owner and a member with access open it and get the store's
// events; another business's cashier, a customer token and nobody are
// refused; a removed member's open stream is cut at the next permission
// check and cannot be reopened (D-107).
func TestStoreStreamHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	a := newStreamAPI(t, 300*time.Millisecond)
	owner := a.signIn(ownerPhone, "business")
	bx, sx := a.newBusiness(owner, "Lakshmi Traders")
	X := "/v1/biz/businesses/" + bx
	SX := X + "/stores/" + sx + "/stream"

	// A cashier of X with access to the store.
	r := a.call(owner, http.MethodPost, X+"/staff", map[string]any{"phone": cashierPhone, "role": "cashier", "storeIds": []string{sx}}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	cashStaff := r.body["id"].(string)
	cashier := a.signIn(cashierPhone, "business")
	r = a.call(cashier, http.MethodPost, X+"/staff/"+cashStaff+"/accept", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// Another business Y with its own cashier.
	ownerY := a.signIn(otherOwnerPhone, "business")
	by, sy := a.newBusiness(ownerY, "Ganesh Stores")
	Y := "/v1/biz/businesses/" + by
	r = a.call(ownerY, http.MethodPost, Y+"/staff", map[string]any{"phone": s2CashierPhone, "role": "cashier", "storeIds": []string{sy}}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	yStaff := r.body["id"].(string)
	cashierY := a.signIn(s2CashierPhone, "business")
	r = a.call(cashierY, http.MethodPost, Y+"/staff/"+yStaff+"/accept", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// --- refused -------------------------------------------------------------
	for _, c := range []struct {
		name, token, path string
		codes             []int
	}{
		{"nobody", "", SX, []int{http.StatusUnauthorized}},
		{"a customer token", a.signIn(strangerPhone, "customer"), SX, []int{http.StatusForbidden}},
		{"a business user of no business", a.signIn(strangerPhone, "business"), SX, []int{http.StatusForbidden, http.StatusNotFound}},
		{"Y's cashier on X's store", cashierY, SX, []int{http.StatusForbidden, http.StatusNotFound}},
		{"Y's cashier with X's store under Y", cashierY, Y + "/stores/" + sx + "/stream", []int{http.StatusForbidden, http.StatusNotFound}},
		{"X's owner on Y's store", owner, Y + "/stores/" + sy + "/stream", []int{http.StatusForbidden, http.StatusNotFound}},
		{"X's owner with Y's store under X", owner, X + "/stores/" + sy + "/stream", []int{http.StatusForbidden, http.StatusNotFound}},
	} {
		s := a.open(c.token, c.path)
		require.Contains(t, c.codes, s.code, "%s: %s", c.name, s.raw)
		require.Contains(t, s.raw, `"code"`, "%s: a problem body", c.name)
	}

	// --- the owner and the cashier get the store's events --------------------
	so := a.open(owner, SX)
	so.ready(t)
	sc := a.open(cashier, SX)
	sc.ready(t)
	sYs := a.open(cashierY, Y+"/stores/"+sy+"/stream")
	sYs.ready(t)

	order := ids.New(a.clk.Now())
	a.publish(realtime.StoreChannel(mustID(t, sx)), realtime.Event{Type: realtime.OrderCreated, ID: order, Version: 1})
	require.Equal(t, order, so.event(t, realtime.OrderCreated).ID)
	require.Equal(t, order, sc.event(t, realtime.OrderCreated).ID)

	// A new-order inbox row for the store reaches its stream as inbox.updated,
	// and the business inbox lists it.
	nid := a.send(notice.Notification{
		Kind: notice.KindNewOrder, BusinessID: mustID(t, bx), StoreID: mustID(t, sx),
		Params: map[string]string{"store": "Lakshmi Traders"}, Ref: &notice.Ref{Type: "order", ID: order},
	})
	require.Equal(t, nid, so.event(t, realtime.InboxUpdated).ID)
	require.Equal(t, nid, sc.event(t, realtime.InboxUpdated).ID)
	r = a.call(cashier, http.MethodGet, X+"/inbox", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	require.EqualValues(t, 1, r.body["unreadCount"])
	r = a.call(cashierY, http.MethodGet, X+"/inbox", nil, nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, r.code, "another business's inbox: %s", r.raw)
	r = a.call(owner, http.MethodPost, X+"/inbox/read", map[string]any{"all": true}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 0, r.body["unreadCount"])

	// Y's stream saw none of X's events: its next event is its own.
	other := ids.New(a.clk.Now())
	a.publish(realtime.StoreChannel(mustID(t, sy)), realtime.Event{Type: realtime.OrderCreated, ID: other, Version: 1})
	require.Equal(t, other, sYs.event(t, realtime.OrderCreated).ID)

	// --- D-107: a removed member is cut off and cannot reopen ----------------
	r = a.call(owner, http.MethodDelete, X+"/staff/"+cashStaff, nil, nil)
	require.Equal(t, http.StatusNoContent, r.code, r.raw)
	b := sc.next(t)
	require.Equal(t, "reconnect", b.Event)
	require.Equal(t, "1000", b.Retry)
	require.JSONEq(t, `{"reason":"revoked"}`, b.Data)
	select {
	case <-sc.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the removed member's stream is still open")
	}
	again := a.open(cashier, SX)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, again.code, "a removed member reopening: %s", again.raw)
	again = a.open(cashier, SX)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, again.code, again.raw)

	// The owner's stream is untouched by the recheck and still live.
	next := ids.New(a.clk.Now())
	a.publish(realtime.StoreChannel(mustID(t, sx)), realtime.Event{Type: realtime.OrderUpdated, ID: next, Version: 2})
	ev := so.event(t, realtime.OrderUpdated)
	require.Equal(t, next, ev.ID)
	require.Equal(t, int64(2), ev.Version)
}

// TestMeStreamAndInboxHTTP: the customer's stream and inbox through the real
// pipeline: a notification for the user reaches /v1/me/stream and the list,
// mark-read moves the unread count, and the switches are per kind and
// channel of the caller's own app.
func TestMeStreamAndInboxHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	a := newStreamAPI(t, time.Minute)
	cust := a.signIn(strangerPhone, "customer")
	r := a.call(cust, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	uid := mustID(t, r.body["id"].(string))

	biz := a.signIn(ownerPhone, "business")
	s := a.open(biz, "/v1/me/stream")
	require.Equal(t, http.StatusForbidden, s.code, "the customer stream is for the customer app: %s", s.raw)
	s = a.open(cust, "/v1/admin/stream")
	require.Equal(t, http.StatusForbidden, s.code, "the admin stream: %s", s.raw)

	s = a.open(cust, "/v1/me/stream")
	s.ready(t)
	r = a.call(cust, http.MethodGet, "/v1/me/notifications", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Empty(t, r.body["items"])
	require.EqualValues(t, 0, r.body["unreadCount"])

	order := ids.New(a.clk.Now())
	nid := a.send(notice.Notification{
		Kind: notice.KindOrderUpdate, UserID: uid,
		Params: map[string]string{"store": "Lakshmi Traders", "variant": "ready"}, Ref: &notice.Ref{Type: "order", ID: order},
	})
	require.Equal(t, nid, s.event(t, realtime.InboxUpdated).ID)
	a.publish(realtime.UserChannel(uid), realtime.Event{Type: realtime.OrderUpdated, ID: order, Version: 3})
	require.Equal(t, order, s.event(t, realtime.OrderUpdated).ID)

	r = a.call(cust, http.MethodGet, "/v1/me/notifications", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	items := r.body["items"].([]any)
	require.Len(t, items, 1)
	it := items[0].(map[string]any)
	require.Equal(t, nid.String(), it["id"])
	require.Equal(t, "order_update", it["kind"])
	require.Contains(t, it["title"].(string)+" "+it["body"].(string), "Lakshmi Traders")
	require.Nil(t, it["readAt"])
	require.EqualValues(t, 1, r.body["unreadCount"])

	r = a.call(cust, http.MethodPost, "/v1/me/notifications/read", map[string]any{"ids": []string{nid.String()}}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 0, r.body["unreadCount"])

	// Switches: the customer app's own kinds and channels only.
	r = a.call(cust, http.MethodPatch, "/v1/me/notification-prefs", map[string]any{
		"items": []map[string]any{{"kind": "order_update", "channel": "push", "enabled": false}},
	}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	off := false
	for _, x := range r.body["items"].([]any) {
		m := x.(map[string]any)
		if m["kind"] == "order_update" && m["channel"] == "push" {
			off = m["enabled"] == false
		}
	}
	require.True(t, off, "order_update push is off: %s", r.raw)
	for _, bad := range []map[string]any{
		{"kind": "new_order", "channel": "push", "enabled": false},   // a business kind
		{"kind": "order_update", "channel": "sms", "enabled": false}, // not a channel of the kind
		{"kind": "nope", "channel": "push", "enabled": false},        // unknown
	} {
		r = a.call(cust, http.MethodPatch, "/v1/me/notification-prefs", map[string]any{"items": []map[string]any{bad}}, nil)
		require.Equal(t, http.StatusUnprocessableEntity, r.code, "%v: %s", bad, r.raw)
	}
	r = a.call(cust, http.MethodGet, "/v1/me/notification-prefs", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
}

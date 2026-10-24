package realtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/lifecycle"
	"github.com/todayz/backend/internal/platform/pgnotify"
	"github.com/todayz/backend/internal/testkit"
)

// Cursors are snapshot xmins, which are cluster-wide: keep a server of our own.
func TestMain(m *testing.M) { testkit.MainOwnServer(m) }

const waitFor = 10 * time.Second

// testHTTP reaches the loopback test servers only; streams stay open past
// the default 10 s request timeout.
var testHTTP = httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true, Timeout: time.Hour})

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// startHub runs a hub on its own LISTEN connection, as one API instance does.
func startHub(t testing.TB, pool *pgxpool.Pool, o Options) *Hub {
	t.Helper()
	l := pgnotify.New(pool, quietLog())
	h := NewHub(o)
	h.Attach(l)
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
	case <-time.After(waitFor):
		t.Fatal("no LISTEN connection")
	}
	return h
}

// streamServer serves s for channel c, with principal p in the context when set.
func streamServer(t testing.TB, s *Stream, c Channel, p *authn.Principal) *httptest.Server {
	t.Helper()
	if s.Channel == nil {
		s.Channel = func(*http.Request) (Channel, error) { return c, nil }
	}
	if s.Clock == nil {
		s.Clock = clock.Real{}
	}
	if s.Log == nil {
		s.Log = quietLog()
	}
	var h http.Handler = s
	if p != nil {
		inner := h
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			inner.ServeHTTP(w, r.WithContext(authn.WithPrincipal(r.Context(), *p)))
		})
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// sseFrame is one block of an event stream.
type sseFrame struct {
	Event, ID, Data, Retry string
	Comment                bool // a block of comments only (": hb")
}

type sseConn struct {
	resp   *http.Response
	frames chan sseFrame
	done   chan struct{} // closed at EOF
}

func dial(t testing.TB, url, lastID string) *sseConn {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := testHTTP.Do(req)
	require.NoError(t, err)
	c := &sseConn{resp: resp, frames: make(chan sseFrame, 64), done: make(chan struct{})}
	t.Cleanup(func() { _ = resp.Body.Close() })
	go c.read()
	return c
}

func (c *sseConn) read() {
	defer close(c.done)
	br := bufio.NewReader(c.resp.Body)
	var f sseFrame
	any := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSuffix(line, "\n")
		switch {
		case line == "":
			if any {
				c.frames <- f
			}
			f, any = sseFrame{}, false
			continue
		case strings.HasPrefix(line, ":"):
			if !any {
				f.Comment = true
			}
		case strings.HasPrefix(line, "event: "):
			f.Event, f.Comment = strings.TrimPrefix(line, "event: "), false
		case strings.HasPrefix(line, "id: "):
			f.ID, f.Comment = strings.TrimPrefix(line, "id: "), false
		case strings.HasPrefix(line, "data: "):
			f.Data, f.Comment = strings.TrimPrefix(line, "data: "), false
		case strings.HasPrefix(line, "retry: "):
			f.Retry, f.Comment = strings.TrimPrefix(line, "retry: "), false
		}
		any = true
	}
}

// next returns the next block that is not a heartbeat.
func (c *sseConn) next(t testing.TB) sseFrame {
	t.Helper()
	deadline := time.After(waitFor)
	for {
		select {
		case f := <-c.frames:
			if f.Comment && f.Event == "" && f.Data == "" {
				continue
			}
			return f
		case <-c.done:
			select {
			case f := <-c.frames:
				return f
			default:
			}
			t.Fatal("stream ended")
		case <-deadline:
			t.Fatal("no frame in time")
		}
	}
}

// ready reads the opening blocks up to `ready` and returns them.
func (c *sseConn) ready(t testing.TB) (before []sseFrame, ready sseFrame) {
	t.Helper()
	require.Equal(t, http.StatusOK, c.resp.StatusCode)
	require.Equal(t, "text/event-stream", c.resp.Header.Get("Content-Type"))
	first := c.next(t)
	require.Equal(t, "3000", first.Retry, "a stream opens with retry: 3000")
	for {
		f := c.next(t)
		if f.Event == "ready" {
			require.NotEmpty(t, f.ID)
			return before, f
		}
		before = append(before, f)
	}
}

func (c *sseConn) ended(t testing.TB) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(waitFor):
		t.Fatal("stream still open")
	}
}

func publish(t testing.TB, pool *pgxpool.Pool, c Channel, ev Event) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, Publish(ctx, tx, c, ev))
	require.NoError(t, tx.Commit(ctx))
}

func eventOf(t testing.TB, f sseFrame) Event {
	t.Helper()
	var ev Event
	require.NoError(t, json.Unmarshal([]byte(f.Data), &ev))
	require.Equal(t, string(ev.Type), f.Event)
	return ev
}

func TestStreamLiveThenReplayAfterReconnect(t *testing.T) {
	db := testkit.NewDB(t)
	h := startHub(t, db.Pool, Options{})
	c := StoreChannel(newID())
	srv := streamServer(t, &Stream{Hub: h, DB: db.Pool, App: "business"}, c, nil)

	conn := dial(t, srv.URL, "")
	before, rd := conn.ready(t)
	require.Empty(t, before, "a fresh connect has no reset and no replay")
	require.Equal(t, `{"resumed":false}`, rd.Data)

	e1 := Event{Type: OrderCreated, ID: newID(), Version: 1}
	publish(t, db.Pool, c, e1)
	f := conn.next(t)
	require.Equal(t, e1, eventOf(t, f))
	require.NotEmpty(t, f.ID)
	last := f.ID
	_ = conn.resp.Body.Close()
	conn.ended(t)

	// Missed while away: two events here, one on another store.
	e2 := Event{Type: OrderUpdated, ID: e1.ID, Version: 2}
	e3 := Event{Type: AskCreated, ID: newID(), Version: 1}
	publish(t, db.Pool, c, e2)
	publish(t, db.Pool, StoreChannel(newID()), Event{Type: OrderCreated, ID: newID(), Version: 1})
	publish(t, db.Pool, c, e3)

	again := dial(t, srv.URL, last)
	replayed, rd := again.ready(t)
	require.Equal(t, `{"resumed":true}`, rd.Data)
	var got []Event
	for _, f := range replayed {
		got = append(got, eventOf(t, f))
	}
	// The cursor is a lower bound: e1 may come again, nothing may be missing.
	if len(got) == 3 {
		require.Equal(t, e1, got[0])
		got = got[1:]
	}
	require.Equal(t, []Event{e2, e3}, got)

	// e3's NOTIFY reaching the hub only now (it was committed before the
	// replay snapshot, after Subscribe) is not sent a second time.
	var seq3 int64
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT seq FROM outbox_events WHERE channel = $1 AND payload->>'type' = $2`, string(c), string(AskCreated)).Scan(&seq3))
	h.Notify(context.Background(), notifyPayload(t, seq3, c, e3, mustCursor(t, rd.ID)))

	// Live again after the replay, with a cursor that moved on.
	e4 := Event{Type: AskClosed, ID: e3.ID, Version: 2}
	publish(t, db.Pool, c, e4)
	f = again.next(t)
	require.Equal(t, e4, eventOf(t, f))
	require.GreaterOrEqual(t, mustCursor(t, f.ID), mustCursor(t, rd.ID))
}

func mustCursor(t testing.TB, s string) uint64 {
	t.Helper()
	v, ok := parseCursor(s)
	require.True(t, ok, s)
	return v
}

func TestRolledBackPublishDeliversNothing(t *testing.T) {
	db := testkit.NewDB(t)
	h := startHub(t, db.Pool, Options{})
	c := UserChannel(newID())
	srv := streamServer(t, &Stream{Hub: h, DB: db.Pool, App: "customer"}, c, nil)
	conn := dial(t, srv.URL, "")
	_, rd := conn.ready(t)

	ctx := context.Background()
	tx, err := db.Pool.Begin(ctx)
	require.NoError(t, err)
	gone := Event{Type: OrderUpdated, ID: newID(), Version: 9}
	require.NoError(t, Publish(ctx, tx, c, gone))
	require.NoError(t, tx.Rollback(ctx))

	kept := Event{Type: OrderUpdated, ID: newID(), Version: 1}
	publish(t, db.Pool, c, kept)
	require.Equal(t, kept, eventOf(t, conn.next(t)), "the rolled-back event never arrives")

	var n int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE channel = $1`, string(c)).Scan(&n))
	require.Equal(t, 1, n, "nor is it in the replay log")

	// A replay from the first cursor has the kept event only.
	replay := dial(t, srv.URL, rd.ID)
	frames, _ := replay.ready(t)
	require.Len(t, frames, 1)
	require.Equal(t, kept, eventOf(t, frames[0]))
}

func TestCrossInstanceDelivery(t *testing.T) {
	db := testkit.NewDB(t)
	a := startHub(t, db.Pool, Options{})
	b := startHub(t, db.Pool, Options{})
	c := StoreChannel(newID())
	srvA := streamServer(t, &Stream{Hub: a, DB: db.Pool, App: "business"}, c, nil)
	srvB := streamServer(t, &Stream{Hub: b, DB: db.Pool, App: "business"}, c, nil)
	ca, cb := dial(t, srvA.URL, ""), dial(t, srvB.URL, "")
	ca.ready(t)
	cb.ready(t)

	// Published through neither hub: any transaction on the database.
	ev := Event{Type: OrderCreated, ID: newID(), Version: 1}
	publish(t, db.Pool, c, ev)
	require.Equal(t, ev, eventOf(t, ca.next(t)))
	fb := cb.next(t)
	require.Equal(t, ev, eventOf(t, fb))

	// A cursor from instance B resumes on instance A (xids are the database's).
	_ = cb.resp.Body.Close()
	ev2 := Event{Type: OrderUpdated, ID: ev.ID, Version: 2}
	publish(t, db.Pool, c, ev2)
	moved := dial(t, srvA.URL, fb.ID)
	frames, _ := moved.ready(t)
	require.NotEmpty(t, frames)
	require.Equal(t, ev2, eventOf(t, frames[len(frames)-1]))
	require.Equal(t, ev2, eventOf(t, ca.next(t)))
}

func TestResetWhenCursorCannotBeHonoured(t *testing.T) {
	db := testkit.NewDB(t)
	ctx := context.Background()
	h := startHub(t, db.Pool, Options{})
	c := UserChannel(newID())
	srv := streamServer(t, &Stream{Hub: h, DB: db.Pool, App: "customer", ReplayLimit: 3}, c, nil)

	expectReset := func(lastID string) sseFrame {
		t.Helper()
		conn := dial(t, srv.URL, lastID)
		before, rd := conn.ready(t)
		require.Len(t, before, 1, lastID)
		require.Equal(t, "reset", before[0].Event, lastID)
		require.Equal(t, `{"resumed":false}`, rd.Data)
		_ = conn.resp.Body.Close()
		return rd
	}
	expectReset("not-a-cursor")
	expectReset("0")
	expectReset("99999999999") // never handed out: beyond the snapshot's xmax

	// More missed events than the replay limit: refetch everything.
	conn := dial(t, srv.URL, "")
	_, rd := conn.ready(t)
	_ = conn.resp.Body.Close()
	for i := 0; i < 4; i++ {
		publish(t, db.Pool, c, Event{Type: OrderUpdated, ID: newID(), Version: int64(i)})
	}
	expectReset(rd.ID)

	// Retention: the cleanup deletes rows older than 24 h and raises the
	// watermark; a cursor at or below it may have lost rows.
	now := clock.Real{}.Now()
	_, err := db.Pool.Exec(ctx, `UPDATE outbox_events SET created_at = $1 WHERE channel = $2`, now.Add(-25*time.Hour), string(c))
	require.NoError(t, err)
	fresh := Event{Type: OrderUpdated, ID: newID(), Version: 1}
	publish(t, db.Pool, c, fresh)
	res, err := Cleanup(ctx, db.Pool, now, 10)
	require.NoError(t, err)
	require.Equal(t, int64(4), res.Deleted)
	var wm, oldest string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT deleted_through_xid::text FROM outbox_watermark`).Scan(&wm))
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT min(xid::text::numeric)::text FROM outbox_events WHERE channel = $1`, string(c)).Scan(&oldest))
	require.Greater(t, mustCursor(t, wm), uint64(0))
	require.Less(t, mustCursor(t, wm), mustCursor(t, oldest), "the watermark is below what is kept")
	expectReset(rd.ID)

	// A cursor above the watermark still replays.
	conn = dial(t, srv.URL, formatCursor(mustCursor(t, wm)+1))
	frames, rd2 := conn.ready(t)
	require.Equal(t, `{"resumed":true}`, rd2.Data)
	require.Len(t, frames, 1)
	require.Equal(t, fresh, eventOf(t, frames[0]))

	// A second cleanup with nothing old leaves the watermark alone.
	res, err = Cleanup(ctx, db.Pool, now, 10)
	require.NoError(t, err)
	require.Zero(t, res.Deleted)
	var wm2 string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT deleted_through_xid::text FROM outbox_watermark`).Scan(&wm2))
	require.Equal(t, wm, wm2)
}

func TestHeartbeatMovesTheCursor(t *testing.T) {
	db := testkit.NewDB(t)
	h := startHub(t, db.Pool, Options{})
	c := UserChannel(newID())
	srv := streamServer(t, &Stream{Hub: h, DB: db.Pool, App: "customer", Heartbeat: 50 * time.Millisecond}, c, nil)
	conn := dial(t, srv.URL, "")
	_, rd := conn.ready(t)

	// Traffic on other channels moves this idle stream's cursor on the
	// next heartbeat (an id-only block), so a resume replays less.
	publish(t, db.Pool, UserChannel(newID()), Event{Type: OrderUpdated, ID: newID(), Version: 1})
	publish(t, db.Pool, UserChannel(newID()), Event{Type: OrderUpdated, ID: newID(), Version: 1})
	deadline := time.After(waitFor)
	for {
		select {
		case f := <-conn.frames:
			require.True(t, f.Comment || f.Event == "", "only heartbeats here: %+v", f)
			if f.ID != "" && mustCursor(t, f.ID) > mustCursor(t, rd.ID) {
				return
			}
		case <-deadline:
			t.Fatal("the heartbeat never moved the cursor")
		}
	}
}

func TestRecheckEndsTheStream(t *testing.T) {
	db := testkit.NewDB(t)
	h := startHub(t, db.Pool, Options{})
	for _, tc := range []struct {
		err  error
		want Reason
	}{
		{problem.New(genproblems.Forbidden, ""), ReasonRevoked},
		{problem.New(genproblems.NotFound, ""), ReasonRevoked},
		{errors.New("database down"), ReasonRecheck},
	} {
		var calls atomic.Int32
		c := StoreChannel(newID())
		srv := streamServer(t, &Stream{Hub: h, DB: db.Pool, App: "business", RecheckEvery: 30 * time.Millisecond,
			Recheck: func(*http.Request) error {
				if calls.Add(1) < 3 {
					return nil
				}
				return tc.err
			}}, c, nil)
		conn := dial(t, srv.URL, "")
		conn.ready(t)
		f := conn.next(t)
		require.Equal(t, "reconnect", f.Event)
		require.Equal(t, "1000", f.Retry)
		require.Equal(t, `{"reason":"`+string(tc.want)+`"}`, f.Data)
		conn.ended(t)
		require.Eventually(t, func() bool { return h.Len() == 0 }, waitFor, 10*time.Millisecond, "the subscription is gone")
	}
}

func TestTokenExpiryEndsTheStream(t *testing.T) {
	db := testkit.NewDB(t)
	h := startHub(t, db.Pool, Options{})
	uid := newID()
	p := authn.Principal{UserID: uid, App: authn.AppCustomer, ExpiresAt: clock.Real{}.Now().Add(300 * time.Millisecond)}
	srv := streamServer(t, &Stream{Hub: h, DB: db.Pool, App: "customer", Channel: MeChannel}, "", &p)
	conn := dial(t, srv.URL, "")
	conn.ready(t)
	f := conn.next(t)
	require.Equal(t, "reconnect", f.Event)
	require.Equal(t, `{"reason":"expired"}`, f.Data)
	conn.ended(t)
}

func TestSlowConsumerIsDroppedNotWaitedFor(t *testing.T) {
	db := testkit.NewDB(t)
	h := startHub(t, db.Pool, Options{Buffer: 1})
	c := UserChannel(newID())
	s, err := h.Subscribe(c) // never reads
	require.NoError(t, err)
	srv := streamServer(t, &Stream{Hub: h, DB: db.Pool, App: "customer"}, c, nil)
	conn := dial(t, srv.URL, "")
	conn.ready(t)
	for i := 0; i < 3; i++ {
		publish(t, db.Pool, c, Event{Type: OrderUpdated, ID: newID(), Version: int64(i)})
	}
	<-s.Done()
	require.Equal(t, ReasonOverflow, s.Reason())
	// The reading stream still got events (it may itself overflow a
	// one-slot buffer, in which case it is told to reconnect, never stalled).
	f := conn.next(t)
	require.Contains(t, []string{"order.updated", "reconnect"}, f.Event)
}

// TestSIGTERMSendsReconnect is SC-GS-07 (D-98): on SIGTERM the server keeps
// serving through the pre-stop, then every open stream gets `event:
// reconnect` with `retry: 1000` and its cursor, the streams end, and the
// drain finishes at once instead of waiting out its budget.
func TestSIGTERMSendsReconnect(t *testing.T) {
	db := testkit.NewDB(t)
	h := startHub(t, db.Pool, Options{})
	c := StoreChannel(newID())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	st := &Stream{Hub: h, DB: db.Pool, App: "business", Clock: clock.Real{}, Log: quietLog(),
		Channel: func(*http.Request) (Channel, error) { return c, nil }}
	srv := &lifecycle.Server{Name: "api", Srv: httpx.NewServer(ln.Addr().String(), st), Listener: ln,
		Ready: noDrainer{}, PreStop: 100 * time.Millisecond, Drain: 30 * time.Second, Before: h.Shutdown, Log: quietLog()}
	sig, term := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run(sig) }()

	url := "http://" + ln.Addr().String()
	conns := []*sseConn{dial(t, url, ""), dial(t, url, "")}
	for _, cn := range conns {
		cn.ready(t)
	}
	ev := Event{Type: OrderCreated, ID: newID(), Version: 1}
	publish(t, db.Pool, c, ev)
	var lastIDs []string
	for _, cn := range conns {
		f := cn.next(t)
		require.Equal(t, ev, eventOf(t, f))
		lastIDs = append(lastIDs, f.ID)
	}

	start := clock.Real{}.Now()
	term() // SIGTERM
	for i, cn := range conns {
		f := cn.next(t)
		require.Equal(t, "reconnect", f.Event)
		require.Equal(t, "1000", f.Retry)
		require.Equal(t, `{"reason":"shutdown"}`, f.Data)
		require.GreaterOrEqual(t, mustCursor(t, f.ID), mustCursor(t, lastIDs[i]), "the client resumes from where it was")
		cn.ended(t)
	}
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(waitFor):
		t.Fatal("the server did not finish its drain")
	}
	require.Less(t, clock.Real{}.Now().Sub(start), 5*time.Second, "streams do not hold the drain")

	// The resumed cursor replays nothing lost on the next instance.
	other := startHub(t, db.Pool, Options{})
	next := streamServer(t, &Stream{Hub: other, DB: db.Pool, App: "business"}, c, nil)
	resumed := dial(t, next.URL, lastIDs[0])
	_, rd := resumed.ready(t)
	require.Equal(t, `{"resumed":true}`, rd.Data)

	// A stream opened on a hub that is shutting down is told to go elsewhere.
	late := streamServer(t, &Stream{Hub: h, DB: db.Pool, App: "business"}, c, nil)
	lc := dial(t, late.URL, "")
	f := lc.next(t)
	require.Equal(t, "reconnect", f.Event)
	require.Equal(t, `{"reason":"shutdown"}`, f.Data)
	lc.ended(t)
}

type noDrainer struct{}

func (noDrainer) SetDraining() {}

func TestPublishOutsideTransactionIsRefused(t *testing.T) {
	require.ErrorIs(t, PublishCtx(context.Background(), AdminChannel, Event{Type: PulseUpdated, ID: newID()}), ErrNoTx)
}

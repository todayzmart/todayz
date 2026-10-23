package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/todayz/backend/internal/platform/pgnotify"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Defaults for Options.
const (
	DefaultBuffer        = 64
	DefaultMaxPerChannel = 16
	DefaultMaxStreams    = 20000
)

// Reason says why the hub ended a subscription. Every reason makes the
// handler write `event: reconnect` and close: the client resumes with its
// Last-Event-ID, on this instance or another, and loses nothing.
type Reason string

// The reasons.
const (
	ReasonShutdown  Reason = "shutdown"  // SIGTERM (D-98)
	ReasonOverflow  Reason = "overflow"  // a slow consumer filled its buffer
	ReasonListener  Reason = "listener"  // the LISTEN connection was re-established; notifications may have been lost in between
	ReasonExpired   Reason = "expired"   // the access token the stream was opened with expired
	ReasonCancelled Reason = "cancelled" // the subscriber unsubscribed itself
	ReasonRevoked   Reason = "revoked"   // the periodic permission check refused (D-107); the reconnect is refused too
	ReasonRecheck   Reason = "recheck"   // the periodic permission check could not run; the reconnect checks again
)

// Errors from Subscribe.
var (
	ErrShuttingDown = errors.New("realtime: hub is shutting down")
	ErrTooMany      = errors.New("realtime: too many streams")
)

// Options configure a Hub.
type Options struct {
	Log     *slog.Logger
	Metrics *telemetry.Metrics
	// Buffer is each subscriber's queue (default 64). A subscriber whose
	// queue is full when an event arrives is dropped (ReasonOverflow), never
	// waited for: one slow phone must not delay every other stream.
	Buffer int
	// MaxPerChannel caps streams per channel on this instance (default 16):
	// a store's devices, a customer's tabs. Past it Subscribe answers ErrTooMany.
	MaxPerChannel int
	// MaxStreams caps all streams on this instance (default 20,000).
	MaxStreams int
}

// delivery is one event as the hub received it.
type delivery struct {
	frame []byte    // "event: <type>\ndata: {…}\n", rendered once per notification
	x     uint64    // the publisher's snapshot xmin: a valid cursor once written
	seq   int64     // the outbox row, so a stream can skip what its replay already sent
	at    time.Time // publish time (Unix ms from the database), for the lag metric
}

// Subscriber is one open stream's registration.
type Subscriber struct {
	ch     Channel
	q      chan delivery
	done   chan struct{}
	once   sync.Once
	reason atomic.Value // Reason
}

// Done is closed when the hub ends the subscription; Reason says why.
func (s *Subscriber) Done() <-chan struct{} { return s.done }

// Reason is why the subscription ended ("" while open).
func (s *Subscriber) Reason() Reason {
	r, _ := s.reason.Load().(Reason)
	return r
}

func (s *Subscriber) end(r Reason) {
	s.once.Do(func() {
		s.reason.Store(r)
		close(s.done)
	})
}

// Hub fans notifications from one LISTEN connection out to this instance's
// subscribers. Its handler runs inside pgnotify's loop, so it never blocks:
// a full queue ends that subscriber instead.
type Hub struct {
	log     *slog.Logger
	metrics *telemetry.Metrics
	buffer  int
	perChan int
	max     int

	mu     sync.Mutex
	subs   map[Channel]map[*Subscriber]struct{}
	n      int
	closed bool

	// mark is the largest publisher xmin the hub has fanned out (cursor.go).
	mark atomic.Uint64
}

func init() {
	telemetry.RegisterLabelValues("channel", string(KindStore), string(KindUser), string(KindAdmin))
}

// NewHub returns a hub; Attach it to the API's pgnotify listener.
func NewHub(o Options) *Hub {
	h := &Hub{
		log: telemetry.OrDefault(o.Log), metrics: o.Metrics,
		buffer: o.Buffer, perChan: o.MaxPerChannel, max: o.MaxStreams,
		subs: map[Channel]map[*Subscriber]struct{}{},
	}
	if h.buffer <= 0 {
		h.buffer = DefaultBuffer
	}
	if h.perChan <= 0 {
		h.perChan = DefaultMaxPerChannel
	}
	if h.max <= 0 {
		h.max = DefaultMaxStreams
	}
	return h
}

// Attach registers the hub on l. Call it before l runs (startListener).
// After every (re)connect of l, every open stream is told to reconnect: a
// notification sent while the connection was down is lost, and only a replay
// from the client's cursor recovers it.
func (h *Hub) Attach(l *pgnotify.Listener) {
	l.Handle(PGChannel, h.Notify)
	l.OnConnect(func(context.Context) { h.endAll(ReasonListener) })
}

// Subscribe registers a stream on c. Register before reading the replay, so
// an event that commits after the replay's snapshot is queued here.
func (h *Hub) Subscribe(c Channel) (*Subscriber, error) {
	if _, ok := c.Kind(); !ok {
		return nil, ErrBadChannel
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrShuttingDown
	}
	if h.n >= h.max || len(h.subs[c]) >= h.perChan {
		return nil, ErrTooMany
	}
	s := &Subscriber{ch: c, q: make(chan delivery, h.buffer), done: make(chan struct{})}
	m := h.subs[c]
	if m == nil {
		m = map[*Subscriber]struct{}{}
		h.subs[c] = m
	}
	m[s] = struct{}{}
	h.n++
	return s, nil
}

// Unsubscribe removes s; the handler calls it when the stream closes.
func (h *Hub) Unsubscribe(s *Subscriber) {
	h.mu.Lock()
	h.remove(s)
	h.mu.Unlock()
	s.end(ReasonCancelled)
}

// remove deletes s; h.mu is held.
func (h *Hub) remove(s *Subscriber) {
	m := h.subs[s.ch]
	if _, ok := m[s]; !ok {
		return
	}
	delete(m, s)
	h.n--
	if len(m) == 0 {
		delete(h.subs, s.ch)
	}
}

// Mark is the hub's current mark (cursor.go): valid for a subscriber whose
// queue is empty. Read it before checking the queue.
func (h *Hub) Mark() uint64 { return h.mark.Load() }

// Len is the number of open streams on this instance.
func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n
}

// notification is the NOTIFY payload publishSQL builds.
type notification struct {
	Seq     int64           `json:"s"`
	Channel Channel         `json:"c"`
	Event   json.RawMessage `json:"e"`
	X       string          `json:"x"`
	T       int64           `json:"t"`
}

// Notify handles one NOTIFY payload (pgnotify.Handler).
func (h *Hub) Notify(ctx context.Context, payload string) {
	var n notification
	if err := json.Unmarshal([]byte(payload), &n); err != nil {
		h.log.WarnContext(ctx, "realtime: unreadable notification", slog.String("err", err.Error()))
		return
	}
	x, err := strconv.ParseUint(n.X, 10, 64)
	if err != nil {
		h.log.WarnContext(ctx, "realtime: notification without a cursor", slog.Int64("seq", n.Seq))
		return
	}
	kind, ok := n.Channel.Kind()
	var ev Event
	if ok {
		ok = json.Unmarshal(n.Event, &ev) == nil
	}
	if ok {
		// Rendered once here, not per subscriber.
		data, _ := json.Marshal(ev)
		d := delivery{
			frame: append(append(append(append([]byte("event: "), ev.Type...), "\ndata: "...), data...), '\n'),
			x:     x,
			seq:   n.Seq,
			at:    time.UnixMilli(n.T),
		}
		h.metrics.SSEPublished(ctx, string(kind))
		h.fanOut(n.Channel, d)
	} else {
		h.log.WarnContext(ctx, "realtime: bad event in notification", slog.Int64("seq", n.Seq))
	}
	// Even an event this instance cannot use moves the mark: it proves every
	// transaction below x has been delivered.
	h.raiseMark(x)
}

func (h *Hub) fanOut(c Channel, d delivery) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[c] {
		select {
		case s.q <- d:
		default:
			h.remove(s)
			s.end(ReasonOverflow)
		}
	}
}

func (h *Hub) raiseMark(x uint64) {
	for {
		cur := h.mark.Load()
		if x <= cur || h.mark.CompareAndSwap(cur, x) {
			return
		}
	}
}

// Shutdown ends every stream with ReasonShutdown and refuses new ones (D-98).
// lifecycle.Server runs it after the pre-stop and before srv.Shutdown, so the
// load balancer has already stopped sending new streams here and the
// handlers' `reconnect` lets srv.Shutdown finish instead of waiting out the
// drain. It never fails; the error is for lifecycle.Server.Before.
func (h *Hub) Shutdown() error {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	h.endAll(ReasonShutdown)
	return nil
}

func (h *Hub) endAll(r Reason) {
	h.mu.Lock()
	all := make([]*Subscriber, 0, h.n)
	for _, m := range h.subs {
		for s := range m {
			all = append(all, s)
		}
	}
	h.subs = map[Channel]map[*Subscriber]struct{}{}
	h.n = 0
	h.mu.Unlock()
	for _, s := range all {
		s.end(r)
	}
}

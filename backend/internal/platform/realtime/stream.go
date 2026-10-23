package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Stream defaults.
const (
	// DefaultHeartbeat is the comment interval of 02 §5: Cloudflare closes a
	// stream idle for 100 s.
	DefaultHeartbeat = 25 * time.Second
	// DefaultReplayLimit caps a Last-Event-ID replay; past it the client gets
	// `reset` and refetches, which is cheaper than streaming a day of events.
	DefaultReplayLimit = 500
	// ReconnectRetry is the `retry:` sent with `event: reconnect` (D-98), ms.
	ReconnectRetry = 1000
	// InitialRetry is the `retry:` sent when a stream opens, ms.
	InitialRetry = 3000
	// DefaultRecheck is how often an open stream re-runs its permission check.
	DefaultRecheck = time.Minute
	// writeTimeout bounds one write to the client (wall clock): a client that
	// stops reading is cut instead of holding a goroutine and a buffer.
	writeTimeout = 10 * time.Second
)

// TxBeginner is the pool (or a test double) the replay reads through.
type TxBeginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// ChannelFunc picks the stream's channel from an authenticated request. It
// returns a problem error (404, 401) when the request has none; the route's
// authz middleware has already decided whether the caller may read it.
type ChannelFunc func(r *http.Request) (Channel, error)

// Stream is the SSE handler for one kind of stream.
type Stream struct {
	Hub     *Hub
	DB      TxBeginner
	Clock   clock.Clock // the app clock: token expiry
	Channel ChannelFunc
	// App is the `app` metric label: customer, business or admin.
	App     string
	Metrics *telemetry.Metrics
	Log     *slog.Logger
	// Heartbeat and ReplayLimit default to DefaultHeartbeat and DefaultReplayLimit.
	Heartbeat   time.Duration
	ReplayLimit int
	// Recheck, when set, runs the route's permission check again every
	// RecheckEvery (default DefaultRecheck) while the stream is open. A
	// refusal ends the stream with `reconnect` (reason "revoked"), and the
	// reconnect is refused by the route's authz: a member removed from the
	// store or the business (D-107) stops receiving within a minute instead
	// of at token expiry. Any other error (a failed lookup) also ends the
	// stream, with reason "recheck": the reconnect re-runs the full check.
	Recheck      func(r *http.Request) error
	RecheckEvery time.Duration
}

// MeChannel is the customer's own channel (GET /v1/me/stream).
func MeChannel(r *http.Request) (Channel, error) {
	p, ok := authn.FromContext(r.Context())
	if !ok || p.IsAnonymous() {
		return "", problem.New(genproblems.Unauthorized, "")
	}
	return UserChannel(p.UserID), nil
}

// AdminChannelFunc is the admin channel (GET /v1/admin/stream).
func AdminChannelFunc(*http.Request) (Channel, error) { return AdminChannel, nil }

// StoreChannelFromPath is the store in the path (GET {S}/stream). The route's
// authz middleware has checked that the store belongs to the business in the
// path and that the caller is its staff (authz.StaffResolver).
func StoreChannelFromPath(r *http.Request) (Channel, error) {
	sc, err := authz.ScopeFrom(r)
	if err != nil {
		return "", err
	}
	if sc.StoreID == ids.Zero {
		return "", problem.New(genproblems.NotFound, "")
	}
	return StoreChannel(sc.StoreID), nil
}

func (s *Stream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := telemetry.LoggerOr(ctx, s.Log)
	rc := http.NewResponseController(w)
	// httpx.NewServer's ReadTimeout (30 s) would otherwise end the request's
	// background read and cancel ctx half a minute into every stream.
	_ = rc.SetReadDeadline(time.Time{})

	ch, err := s.Channel(r)
	if err != nil {
		problem.Write(w, r, err)
		return
	}
	sub, err := s.Hub.Subscribe(ch)
	switch {
	case errors.Is(err, ErrShuttingDown):
		// This instance is draining: tell the client to go elsewhere now.
		s.headers(w)
		_ = s.write(rc, w, reconnectFrame(0, ReasonShutdown))
		return
	case errors.Is(err, ErrTooMany):
		problem.Write(w, r, problem.New(genproblems.RateLimited, "Too many open streams").WithHeader("Retry-After", "15"))
		return
	case err != nil:
		problem.Write(w, r, err)
		return
	}
	defer s.Hub.Unsubscribe(sub)

	lastID := r.Header.Get("Last-Event-ID")
	resumed := lastID != ""
	if resumed {
		s.Metrics.SSEReconnect(ctx, s.App)
	}
	from, valid := parseCursor(lastID)
	rp, err := s.replay(ctx, ch, from, resumed && valid)
	if err != nil {
		log.WarnContext(ctx, "realtime: replay failed", slog.String("err", err.Error()))
		problem.Write(w, r, problem.New(genproblems.ServiceUnavailable, "").WithHeader("Retry-After", "5"))
		return
	}

	s.headers(w)
	s.Metrics.SSEOpened(ctx, s.App)
	defer s.Metrics.SSEClosed(ctx, s.App)

	buf := []byte("retry: " + strconv.Itoa(InitialRetry) + "\n\n")
	cur := rp.cursor
	if resumed && rp.reset {
		buf = append(buf, frame("reset", cur, `{}`)...)
	}
	for _, f := range rp.frames {
		buf = append(buf, f...)
		buf = append(buf, '\n')
	}
	buf = append(buf, frame("ready", cur, `{"resumed":`+strconv.FormatBool(resumed && !rp.reset)+`}`)...)
	if err := s.write(rc, w, buf); err != nil {
		return
	}
	sent := cur

	hbEvery := s.Heartbeat
	if hbEvery <= 0 {
		hbEvery = DefaultHeartbeat
	}
	hb := time.NewTicker(hbEvery)
	defer hb.Stop()
	expiry, stopExpiry := s.expiryTimer(ctx)
	defer stopExpiry()
	var recheck <-chan time.Time
	if s.Recheck != nil {
		every := s.RecheckEvery
		if every <= 0 {
			every = DefaultRecheck
		}
		t := time.NewTicker(every)
		defer t.Stop()
		recheck = t.C
	}

	for {
		select {
		case d := <-sub.q:
			// Subscribe comes before the replay snapshot (no gap), so a row
			// committed just before the snapshot can be replayed and then
			// arrive live too: send it once. Each seq is notified once.
			if _, dup := rp.seqs[d.seq]; dup {
				delete(rp.seqs, d.seq)
				continue
			}
			mark := s.Hub.Mark() // before the queue check (cursor.go)
			cur = maxU(cur, d.x)
			if len(sub.q) == 0 {
				cur = maxU(cur, mark)
			}
			out := append(append(append(append([]byte{}, d.frame...), "id: "...), formatCursor(cur)...), '\n', '\n')
			if err := s.write(rc, w, out); err != nil {
				return
			}
			sent = cur
			if !d.at.IsZero() {
				s.Metrics.SSEDeliveryLag(ctx, s.App, clock.Real{}.Now().Sub(d.at).Seconds())
			}
		case <-hb.C:
			if s.expired(ctx) {
				_ = s.write(rc, w, reconnectFrame(cur, ReasonExpired))
				return
			}
			mark := s.Hub.Mark()
			if len(sub.q) == 0 {
				cur = maxU(cur, mark)
			}
			out := []byte(": hb\n")
			if cur != sent {
				// An id-only block moves the client's Last-Event-ID without an event.
				out = append(append(append(out, "id: "...), formatCursor(cur)...), '\n')
				sent = cur
			}
			if err := s.write(rc, w, append(out, '\n')); err != nil {
				return
			}
		case <-expiry:
			_ = s.write(rc, w, reconnectFrame(cur, ReasonExpired))
			return
		case <-recheck:
			if err := s.Recheck(r); err != nil {
				reason := ReasonRecheck
				if st := problem.Status(err); st == http.StatusUnauthorized || st == http.StatusForbidden || st == http.StatusNotFound {
					reason = ReasonRevoked
				} else {
					log.WarnContext(ctx, "realtime: permission recheck failed", slog.String("err", err.Error()))
				}
				_ = s.write(rc, w, reconnectFrame(cur, reason))
				return
			}
		case <-sub.Done():
			_ = s.write(rc, w, reconnectFrame(cur, sub.Reason()))
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *Stream) headers(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

// write sends b and flushes, under a wall-clock write deadline.
func (s *Stream) write(rc *http.ResponseController, w http.ResponseWriter, b []byte) error {
	_ = rc.SetWriteDeadline(clock.Real{}.Now().Add(writeTimeout))
	if _, err := w.Write(b); err != nil {
		return err
	}
	return rc.Flush()
}

// expiryTimer fires when the access token the stream was opened with
// expires: the stream then asks the client to reconnect, which re-checks the
// session (a revoked session or a removed role ends there).
func (s *Stream) expiryTimer(ctx context.Context) (<-chan time.Time, func()) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.ExpiresAt.IsZero() || s.Clock == nil {
		return nil, func() {}
	}
	d := p.ExpiresAt.Sub(s.Clock.Now())
	if d < 0 {
		d = 0
	}
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}

// expired checks the token expiry against the app clock on each heartbeat:
// the test clock can jump past it, which the wall-clock timer would not see.
func (s *Stream) expired(ctx context.Context) bool {
	p, ok := authn.FromContext(ctx)
	if !ok || p.ExpiresAt.IsZero() || s.Clock == nil {
		return false
	}
	return !s.Clock.Now().Before(p.ExpiresAt)
}

// replayResult is what a connect sends before going live.
type replayResult struct {
	cursor uint64             // the stream's first cursor
	frames [][]byte           // "event: …\ndata: …\n" per replayed event
	seqs   map[int64]struct{} // the outbox rows replayed (at most ReplayLimit)
	reset  bool               // the client's cursor could not be honoured
}

// replay reads, in one repeatable-read snapshot taken after Subscribe, the
// snapshot's xmin (the new cursor), the cleanup watermark and the events at
// or after the client's cursor.
func (s *Stream) replay(ctx context.Context, ch Channel, from uint64, resume bool) (replayResult, error) {
	var rp replayResult
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return rp, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var xmin, xmax, wm string
	if err := tx.QueryRow(ctx, `SELECT pg_snapshot_xmin(pg_current_snapshot())::text,
		pg_snapshot_xmax(pg_current_snapshot())::text,
		(SELECT deleted_through_xid::text FROM outbox_watermark WHERE id = 1)`).Scan(&xmin, &xmax, &wm); err != nil {
		return rp, err
	}
	x0, err1 := strconv.ParseUint(xmin, 10, 64)
	next, err2 := strconv.ParseUint(xmax, 10, 64)
	deleted, err3 := strconv.ParseUint(wm, 10, 64)
	if err := errors.Join(err1, err2, err3); err != nil {
		return rp, fmt.Errorf("realtime: snapshot: %w", err)
	}
	rp.cursor = x0
	if !resume {
		rp.reset = true
		return rp, nil
	}
	// A cursor from the future was never handed out; one at or below the
	// watermark may have lost rows to retention.
	if from > next || from <= deleted {
		rp.reset = true
		return rp, nil
	}
	limit := s.ReplayLimit
	if limit <= 0 {
		limit = DefaultReplayLimit
	}
	rows, err := tx.Query(ctx, `SELECT seq, payload::text FROM outbox_events
		WHERE channel = $1 AND xid >= $2::text::xid8 ORDER BY seq LIMIT $3`,
		string(ch), formatCursor(from), limit+1)
	if err != nil {
		return rp, err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		var p string
		if err := rows.Scan(&seq, &p); err != nil {
			return rp, err
		}
		var ev Event
		if json.Unmarshal([]byte(p), &ev) != nil {
			continue
		}
		data, _ := json.Marshal(ev)
		rp.frames = append(rp.frames, []byte("event: "+string(ev.Type)+"\ndata: "+string(data)+"\n"))
		if rp.seqs == nil {
			rp.seqs = map[int64]struct{}{}
		}
		rp.seqs[seq] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return rp, err
	}
	if len(rp.frames) > limit {
		return replayResult{cursor: x0, reset: true}, nil
	}
	rp.cursor = maxU(from, x0)
	return rp, nil
}

func frame(event string, id uint64, data string) []byte {
	return []byte("event: " + event + "\nid: " + formatCursor(id) + "\ndata: " + data + "\n\n")
}

// reconnectFrame is D-98's last message: reconnect in 1 s, resuming from id.
func reconnectFrame(id uint64, r Reason) []byte {
	b := []byte("event: reconnect\nretry: " + strconv.Itoa(ReconnectRetry) + "\n")
	if id != 0 {
		b = append(append(append(b, "id: "...), formatCursor(id)...), '\n')
	}
	return append(b, `data: {"reason":"`+string(r)+`"}`+"\n\n"...)
}

func maxU(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

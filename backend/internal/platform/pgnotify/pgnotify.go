// Package pgnotify listens to Postgres NOTIFY channels on one dedicated
// connection and calls handlers. It reconnects with backoff; after every
// (re)connect it calls OnConnect handlers so caches reload anything they may
// have missed while disconnected.
package pgnotify

import (
	"context"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/todayz/backend/internal/platform/telemetry"
)

var reChannel = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Handler receives a notification payload.
type Handler func(ctx context.Context, payload string)

// Listener multiplexes channels over one connection.
type Listener struct {
	pool *pgxpool.Pool
	log  *slog.Logger

	mu        sync.Mutex
	handlers  map[string][]Handler
	onConnect []func(ctx context.Context)
	ready     chan struct{}
	readyOnce sync.Once
}

// New returns a listener; call Handle before Run.
func New(pool *pgxpool.Pool, log *slog.Logger) *Listener {
	if log == nil {
		log = telemetry.Default()
	}
	return &Listener{pool: pool, log: log, handlers: map[string][]Handler{}, ready: make(chan struct{})}
}

// Handle registers h for channel. It panics on a bad channel name.
func (l *Listener) Handle(channel string, h Handler) {
	if !reChannel.MatchString(channel) {
		panic("pgnotify: bad channel name " + channel)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.handlers[channel] = append(l.handlers[channel], h)
}

// OnConnect registers f to run after every successful LISTEN setup.
func (l *Listener) OnConnect(f func(ctx context.Context)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onConnect = append(l.onConnect, f)
}

// Ready is closed after the first successful LISTEN.
func (l *Listener) Ready() <-chan struct{} { return l.ready }

// Run listens until ctx is done.
func (l *Listener) Run(ctx context.Context) error {
	backoff := 100 * time.Millisecond
	for {
		err := l.once(ctx)
		if ctx.Err() != nil {
			return nil //nolint:nilerr // ctx done is a normal stop, not a failure
		}
		l.log.WarnContext(ctx, "pgnotify: listener lost its connection; reconnecting", slog.String("err", errText(err)))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < 5*time.Second {
			backoff *= 2
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (l *Listener) once(ctx context.Context) error {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// The connection had session state (LISTEN); never return it to the pool.
	pc := conn.Hijack()
	defer func() { _ = pc.Close(context.WithoutCancel(ctx)) }()

	l.mu.Lock()
	channels := make([]string, 0, len(l.handlers))
	for c := range l.handlers {
		channels = append(channels, c)
	}
	onConnect := append([]func(context.Context){}, l.onConnect...)
	l.mu.Unlock()

	for _, c := range channels {
		if _, err := pc.Exec(ctx, "LISTEN "+pgx.Identifier{c}.Sanitize()); err != nil {
			return err
		}
	}
	for _, f := range onConnect {
		f(ctx)
	}
	l.readyOnce.Do(func() { close(l.ready) })

	for {
		n, err := pc.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		l.mu.Lock()
		hs := append([]Handler{}, l.handlers[n.Channel]...)
		l.mu.Unlock()
		for _, h := range hs {
			h(ctx, n.Payload)
		}
	}
}

// Package settings reads platform_settings (01 §17) through an in-process
// cache with typed getters. Domain code reads every configurable number here
// and never hard-codes it (root CLAUDE.md; SC-PL-06).
//
// The cache reloads after a short TTL and at once when Postgres sends NOTIFY
// platform_settings (a trigger on the table), so a change made in the admin
// console reaches every API and worker instance within moments.
//
// Reloads are single-flight (W3): one caller reloads, every other caller is
// served the values already held without waiting (only the very first load,
// with nothing to serve, makes callers wait for it). A failed reload keeps
// the last good values and backs off (1 s, doubling up to the TTL) before
// the next attempt, so a database outage costs one attempt per backoff step,
// not one per request.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/todayz/backend/internal/platform/money"
	"github.com/todayz/backend/internal/platform/pgnotify"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Key is a platform_settings key (constants in keys.go).
type Key string

// Unit is a setting's unit (03 platform_settings.unit).
type Unit string

// Units.
const (
	UnitPaise   Unit = "paise"
	UnitBP      Unit = "bp"
	UnitCredits Unit = "credits"
	UnitMinutes Unit = "minutes"
	UnitHours   Unit = "hours"
	UnitDays    Unit = "days"
	UnitCount   Unit = "count"
	UnitMeters  Unit = "meters"
	UnitBool    Unit = "bool"
	UnitText    Unit = "text"
)

// Channel is the NOTIFY channel the table trigger uses.
const Channel = "platform_settings"

// DefaultTTL bounds how stale a value can be if a NOTIFY is missed.
const DefaultTTL = 30 * time.Second

// MinBackoff is the wait after a first failed reload; it doubles per
// failure up to the TTL. LoadTimeout bounds one load.
const (
	MinBackoff  = time.Second
	LoadTimeout = 5 * time.Second
)

// Errors.
var (
	ErrUnknownKey = errors.New("settings: unknown key")
	ErrWrongUnit  = errors.New("settings: wrong unit for getter")
	ErrBadValue   = errors.New("settings: value does not match unit")
)

// Value is one row.
type Value struct {
	Key  Key
	Unit Unit
	Raw  json.RawMessage
}

// Loader reads every row.
type Loader func(ctx context.Context) (map[Key]Value, error)

// PGLoader reads platform_settings from pool.
func PGLoader(pool *pgxpool.Pool) Loader {
	return func(ctx context.Context) (map[Key]Value, error) {
		rows, err := pool.Query(ctx, `SELECT key, unit, value FROM platform_settings`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[Key]Value{}
		for rows.Next() {
			var k, u string
			var raw []byte
			if err := rows.Scan(&k, &u, &raw); err != nil {
				return nil, err
			}
			out[Key(k)] = Value{Key: Key(k), Unit: Unit(u), Raw: raw}
		}
		return out, rows.Err()
	}
}

// Options configure a Cache.
type Options struct {
	TTL time.Duration
	// Now is the cache's own wall clock (TTL only); defaults to time.Now.
	Now func() time.Time
	Log *slog.Logger
}

// Cache holds every setting in memory.
type Cache struct {
	load Loader
	ttl  time.Duration
	now  func() time.Time
	log  *slog.Logger

	mu       sync.Mutex
	vals     map[Key]Value // replaced on reload, never mutated
	loadedAt time.Time
	stale    bool
	gen      uint64 // bumped by Invalidate; a load started before it leaves the cache stale
	reloads  int64

	inflight  chan struct{} // closed when the running load ends; nil when none runs
	backoff   time.Duration
	nextRetry time.Time
	lastErr   error
}

// NewCache returns an empty cache; the first read loads it.
func NewCache(load Loader, o Options) *Cache {
	if o.TTL <= 0 {
		o.TTL = DefaultTTL
	}
	if o.Now == nil {
		o.Now = time.Now //nolint:forbidigo // the cache TTL is wall time, not business time
	}
	if o.Log == nil {
		o.Log = telemetry.Default()
	}
	return &Cache{load: load, ttl: o.TTL, now: o.Now, log: o.Log, stale: true}
}

// Attach subscribes the cache to NOTIFY platform_settings on l. Every
// notification, and every listener reconnect, marks the cache stale.
func (c *Cache) Attach(l *pgnotify.Listener) {
	l.Handle(Channel, func(ctx context.Context, key string) {
		c.log.DebugContext(ctx, "settings changed", slog.String("setting", key))
		c.Invalidate()
	})
	l.OnConnect(func(context.Context) { c.Invalidate() })
}

// Invalidate makes the next read reload. It also ends a backoff: a NOTIFY or
// a listener reconnect means the database answers again.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.stale = true
	c.gen++
	c.nextRetry = time.Time{}
	c.mu.Unlock()
}

// Reloads is how many times the cache has loaded (for tests and metrics).
func (c *Cache) Reloads() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reloads
}

// Load reloads now (waiting for a load already running, then loading again),
// ignoring any backoff.
func (c *Cache) Load(ctx context.Context) error {
	for {
		c.mu.Lock()
		if c.inflight == nil {
			c.inflight = make(chan struct{})
			c.mu.Unlock()
			return c.reload(ctx)
		}
		ch := c.inflight
		c.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// reload runs one load; the caller has set c.inflight.
func (c *Cache) reload(ctx context.Context) error {
	c.mu.Lock()
	gen := c.gen
	c.mu.Unlock()
	// A caller that gives up (client gone) must not turn into a backoff.
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), LoadTimeout)
	defer cancel()
	vals, err := c.load(lctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	close(c.inflight)
	c.inflight = nil
	if err != nil {
		c.backoff = min(max(2*c.backoff, MinBackoff), c.ttl)
		c.nextRetry = c.now().Add(c.backoff)
		c.lastErr = fmt.Errorf("settings: load: %w", err)
		return c.lastErr
	}
	c.vals, c.loadedAt, c.stale = vals, c.now(), c.gen != gen
	c.backoff, c.nextRetry, c.lastErr = 0, time.Time{}, nil
	c.reloads++
	return nil
}

// current returns the values to read from, reloading when they are stale and
// no other caller is already reloading and no backoff is running.
func (c *Cache) current(ctx context.Context) (map[Key]Value, error) {
	c.mu.Lock()
	if c.vals != nil && !c.stale && c.now().Sub(c.loadedAt) < c.ttl {
		v := c.vals
		c.mu.Unlock()
		return v, nil
	}
	if c.inflight != nil || c.now().Before(c.nextRetry) {
		// Another caller is reloading, or the last attempt failed recently:
		// serve what we hold without waiting.
		if c.vals != nil {
			v := c.vals
			c.mu.Unlock()
			return v, nil
		}
		if c.inflight == nil {
			err := c.lastErr
			c.mu.Unlock()
			return nil, err
		}
		// Nothing to serve yet: wait for the first load.
		ch := c.inflight
		c.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		c.mu.Lock()
		v, err := c.vals, c.lastErr
		c.mu.Unlock()
		if v == nil {
			return nil, err
		}
		return v, nil
	}
	c.inflight = make(chan struct{})
	c.mu.Unlock()

	err := c.reload(ctx)
	c.mu.Lock()
	v := c.vals
	c.mu.Unlock()
	if err != nil {
		if v == nil {
			return nil, err
		}
		// Serve the last good copy; the DB blip is logged once per attempt.
		c.log.WarnContext(ctx, "settings reload failed; serving cached values", slog.String("err", err.Error()))
	}
	return v, nil
}

// Get returns the raw value of key.
func (c *Cache) Get(ctx context.Context, key Key) (Value, error) {
	vals, err := c.current(ctx)
	if err != nil {
		return Value{}, err
	}
	v, ok := vals[key]
	if !ok {
		return Value{}, fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	return v, nil
}

func (c *Cache) number(ctx context.Context, key Key, units ...Unit) (int64, Unit, error) {
	v, err := c.Get(ctx, key)
	if err != nil {
		return 0, "", err
	}
	if !unitIn(v.Unit, units) {
		return 0, v.Unit, fmt.Errorf("%w: %s is %s, want %v", ErrWrongUnit, key, v.Unit, units)
	}
	var n json.Number
	if err := json.Unmarshal(v.Raw, &n); err != nil {
		return 0, v.Unit, fmt.Errorf("%w: %s", ErrBadValue, key)
	}
	i, err := n.Int64()
	if err != nil {
		return 0, v.Unit, fmt.Errorf("%w: %s", ErrBadValue, key)
	}
	return i, v.Unit, nil
}

func unitIn(u Unit, us []Unit) bool {
	for _, x := range us {
		if x == u {
			return true
		}
	}
	return false
}

// Int reads a count, credits, meters, minutes, hours or days setting.
func (c *Cache) Int(ctx context.Context, key Key) (int64, error) {
	n, _, err := c.number(ctx, key, UnitCount, UnitCredits, UnitMeters, UnitMinutes, UnitHours, UnitDays)
	return n, err
}

// BP reads a basis-points setting.
func (c *Cache) BP(ctx context.Context, key Key) (money.BP, error) {
	n, _, err := c.number(ctx, key, UnitBP)
	return money.BP(n), err
}

// Paise reads a money setting in paise.
func (c *Cache) Paise(ctx context.Context, key Key) (money.Paise, error) {
	n, _, err := c.number(ctx, key, UnitPaise)
	return money.Paise(n), err
}

// Duration reads a minutes, hours or days setting as a time.Duration.
// A day is 24 h; IST calendar-day rules use clock.StartOfDayIST instead.
func (c *Cache) Duration(ctx context.Context, key Key) (time.Duration, error) {
	n, u, err := c.number(ctx, key, UnitMinutes, UnitHours, UnitDays)
	if err != nil {
		return 0, err
	}
	switch u {
	case UnitMinutes:
		return time.Duration(n) * time.Minute, nil
	case UnitHours:
		return time.Duration(n) * time.Hour, nil
	default:
		return time.Duration(n) * 24 * time.Hour, nil
	}
}

// Bool reads an on/off flag.
func (c *Cache) Bool(ctx context.Context, key Key) (bool, error) {
	v, err := c.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if v.Unit != UnitBool {
		return false, fmt.Errorf("%w: %s is %s, want bool", ErrWrongUnit, key, v.Unit)
	}
	var b bool
	if err := json.Unmarshal(v.Raw, &b); err != nil {
		return false, fmt.Errorf("%w: %s", ErrBadValue, key)
	}
	return b, nil
}

// String reads a text setting.
func (c *Cache) String(ctx context.Context, key Key) (string, error) {
	v, err := c.Get(ctx, key)
	if err != nil {
		return "", err
	}
	if v.Unit != UnitText {
		return "", fmt.Errorf("%w: %s is %s, want text", ErrWrongUnit, key, v.Unit)
	}
	var s string
	if err := json.Unmarshal(v.Raw, &s); err != nil {
		return "", fmt.Errorf("%w: %s", ErrBadValue, key)
	}
	return s, nil
}

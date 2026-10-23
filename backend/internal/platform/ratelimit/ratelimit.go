// Package ratelimit is the in-process token bucket behind middleware slot 12
// (02 §2.3, §2.4): per principal or IP, per route group, per instance. It is
// a coarse guard behind Cloudflare's edge limits. The exact limits (OTP sends
// and attempts) are Postgres-backed in internal/identity, not here.
package ratelimit

import (
	"math"
	"sync"
	"time"

	"github.com/todayz/backend/internal/platform/clock"
)

// Rule is one bucket shape: Burst tokens, refilled at PerMinute a minute.
type Rule struct {
	PerMinute int
	Burst     int
}

// Groups are the route groups of the API (mw.RateLimit picks one per path).
const (
	GroupAuth   = "auth"   // /v1/auth/*
	GroupMe     = "me"     // /v1/me/*
	GroupBiz    = "biz"    // /v1/biz/*
	GroupAdmin  = "admin"  // /v1/admin/*
	GroupPublic = "public" // everything else
)

// DefaultRules are deliberately high: they stop a runaway client or script,
// never a person. TODO(lead): the plan gives no numbers for the generic
// limits (02 §2.3); confirm or replace these before Wave 5.
var DefaultRules = map[string]Rule{
	GroupAuth:   {PerMinute: 60, Burst: 60},
	GroupMe:     {PerMinute: 600, Burst: 300},
	GroupBiz:    {PerMinute: 1200, Burst: 600},
	GroupAdmin:  {PerMinute: 1200, Burst: 600},
	GroupPublic: {PerMinute: 600, Burst: 300},
}

// MaxKeys bounds memory: past it, buckets that have refilled completely (and
// so behave exactly like a new bucket) are dropped.
const MaxKeys = 100_000

// Sweeps walk every bucket under the lock, so they are rationed: every
// SweepEvery in any case, and past MaxKeys at most once per MinSweepGap.
// Without the gap, a flood of distinct keys that keeps the map above MaxKeys
// (buckets that are not yet full cannot be dropped) would make every request
// walk 100,000 buckets.
const (
	SweepEvery  = time.Minute
	MinSweepGap = time.Second
)

// Result is one decision. Limit and Remaining feed the RateLimit-* headers.
type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration // until one token is back (when refused)
	Reset      time.Duration // until the bucket is full again
}

type bucket struct {
	tokens float64
	at     time.Time
}

// Limiter holds the buckets. Safe for concurrent use.
type Limiter struct {
	clock clock.Clock
	rules map[string]Rule

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
	sweeps    int // for tests
}

// New returns a limiter with rules (a group without a rule is not limited).
func New(c clock.Clock, rules map[string]Rule) *Limiter {
	return &Limiter{clock: c, rules: rules, buckets: map[string]*bucket{}}
}

// Allow takes one token from the bucket of key in group.
func (l *Limiter) Allow(group, key string) Result {
	rule, ok := l.rules[group]
	if !ok || rule.PerMinute <= 0 || rule.Burst <= 0 {
		return Result{Allowed: true}
	}
	rate := float64(rule.PerMinute) / 60 // tokens per second
	now := l.clock.Now()

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sweepDue(now) {
		l.sweep(now)
	}
	k := group + "\x00" + key
	b, ok := l.buckets[k]
	if !ok {
		b = &bucket{tokens: float64(rule.Burst), at: now}
		l.buckets[k] = b
	}
	if el := now.Sub(b.at).Seconds(); el > 0 {
		b.tokens = math.Min(float64(rule.Burst), b.tokens+el*rate)
	}
	b.at = now
	res := Result{Limit: rule.Burst}
	if b.tokens >= 1 {
		b.tokens--
		res.Allowed = true
	} else {
		res.RetryAfter = time.Duration((1 - b.tokens) / rate * float64(time.Second))
	}
	res.Remaining = int(b.tokens)
	res.Reset = time.Duration((float64(rule.Burst) - b.tokens) / rate * float64(time.Second))
	return res
}

// sweepDue reports whether Allow should sweep now. A clock that moved back
// (the local test clock, D-80) counts as due, so sweeps never stall until it
// catches up. Caller holds mu.
func (l *Limiter) sweepDue(now time.Time) bool {
	since := now.Sub(l.lastSweep)
	switch {
	case since < 0 || since > SweepEvery:
		return true
	case len(l.buckets) >= MaxKeys:
		return since >= MinSweepGap
	}
	return false
}

// sweep drops buckets that are full by now: they are indistinguishable from
// absent ones. Caller holds mu.
func (l *Limiter) sweep(now time.Time) {
	l.lastSweep = now
	l.sweeps++
	for k, b := range l.buckets {
		group := k[:indexNUL(k)]
		rule := l.rules[group]
		rate := float64(rule.PerMinute) / 60
		if rate <= 0 || b.tokens+now.Sub(b.at).Seconds()*rate >= float64(rule.Burst) {
			delete(l.buckets, k)
		}
	}
}

func indexNUL(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return i
		}
	}
	return len(s)
}

// Len is the number of live buckets (tests and metrics).
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

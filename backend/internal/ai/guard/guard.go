// Package guard decides whether an AI call may run: the feature switches (11 §1 rule 2) and
// the daily caps (rule 7, D-70, D-101).
//
// Rules this package holds:
//   - A feature works only when every flag of its Spec is on and its provider keys are set; in
//     local and test the mock LLM and STT count as set (MOCK_LLM, W0-07). Otherwise the
//     feature is hidden: Require answers the common 404, never a broken screen.
//   - Reserve runs before every model or speech call, including the customer features that keep
//     no draft. It refuses when today's platform spend has reached ai_daily_cost_cap_paise, or
//     the store or customer has used ai_daily_calls_store / ai_daily_calls_customer calls
//     today (IST day). Background wording (Subject.Background: insight sentences, coach
//     cards) counts against ai_daily_calls_store_background under scope store_background
//     instead, never against the calls the owner starts (D-228). A refusal is 429 ai_daily_cap_reached with extensions.scope customer,
//     store or platform and Retry-After at the next IST midnight, and no call is made.
//   - A reserved call counts even when the provider then fails (it may have been billed).
//   - Settle adds the call's cost to the day. Today's spend over the cap is published as
//     tz_ai_cost_cap_used_ratio; the AI cost alert fires at 0.8 (infra/grafana/alerts/ai.yaml)
//     and one warning is logged per process and day when 80% is crossed.
//   - Every number comes from platform_settings at the moment of the call.
package guard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/aidb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Scopes of a cap (the problem's extensions.scope and the ai_usage_daily scope).
const (
	ScopeCustomer = "customer"
	ScopeStore    = "store"
	ScopePlatform = "platform"
	// ScopeStoreBackground counts a store's background wording (D-228). Its refusal is never
	// an answer: the caller keeps the template.
	ScopeStoreBackground = "store_background"
)

// AlertRatio is the share of the daily cost cap at which the warning fires (01 §17
// ai_daily_cost_cap_paise: "alert at 80%"; a fixed rule, not a setting).
const AlertRatio = 0.8

func init() {
	telemetry.RegisterLabelValues("reason", ScopeCustomer, ScopeStore, ScopePlatform, ScopeStoreBackground)
}

// ErrUnknownFeature is a feature name with no Spec (a programming error).
var ErrUnknownFeature = errors.New("guard: unknown feature")

// CapError is a refused call.
type CapError struct {
	Scope      string
	RetryAfter time.Duration
}

func (e *CapError) Error() string { return "guard: daily AI cap reached (" + e.Scope + ")" }

// Problem is the 429 the endpoint answers.
func (e *CapError) Problem() *problem.Error {
	secs := int64(math.Ceil(e.RetryAfter.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return problem.New(problems.AiDailyCapReached, "").
		WithExtension("scope", e.Scope).
		WithHeader("Retry-After", strconv.FormatInt(secs, 10))
}

// Providers says which provider keys count as set.
type Providers struct {
	LLM bool
	STT bool
}

// ProvidersFrom reads the config: a key set, or the mocks in local and test (D-17, D-19). A
// provider without a base URL is never configured.
func ProvidersFrom(c *config.Config) Providers {
	mock := c.Providers.MockLLM && c.Env.AllowsTestRoutes()
	return Providers{
		LLM: c.Providers.LLMAPIURL != "" && (c.Providers.AnthropicAPIKey.IsSet() || mock),
		STT: c.Providers.STTAPIURL != "" && (c.Providers.SarvamAPIKey.IsSet() || mock),
	}
}

// Subject is who a call is for. A business feature sets Store; a customer feature sets
// Customer; an admin feature sets neither and counts only against the platform cap.
type Subject struct {
	Store    ids.ID
	Customer ids.ID
	// Background marks wording nobody asked for (insight sentences, coach cards): Store's
	// call counts against ai_daily_calls_store_background, not ai_daily_calls_store.
	Background bool
}

// Guard checks switches and caps.
type Guard struct {
	db        *db.DB
	settings  *settings.Cache
	clock     clock.Clock
	providers Providers

	mu        sync.Mutex
	day       time.Time // IST start of the day spend and capPaise were read for
	spend     int64
	capPaise  int64
	warnedDay time.Time
}

// New returns a guard.
func New(d *db.DB, s *settings.Cache, c clock.Clock, p Providers) *Guard {
	g := &Guard{db: d, settings: s, clock: c, providers: p}
	registerGauge(g)
	return g
}

// Available reports whether feature is switched on and its providers are configured.
func (g *Guard) Available(ctx context.Context, feature string) (bool, error) {
	spec, ok := Features[feature]
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrUnknownFeature, feature)
	}
	if spec.Needs&LLM != 0 && !g.providers.LLM {
		return false, nil
	}
	if spec.Needs&STT != 0 && !g.providers.STT {
		return false, nil
	}
	for _, k := range spec.Flags {
		on, err := g.settings.Bool(ctx, k)
		if err != nil {
			return false, err
		}
		if !on {
			return false, nil
		}
	}
	return true, nil
}

// Require is Available as an endpoint answer: nil, or the common 404 for a hidden feature.
func (g *Guard) Require(ctx context.Context, feature string) error {
	ok, err := g.Available(ctx, feature)
	if err != nil {
		return err
	}
	if !ok {
		return problem.New(problems.NotFound, "")
	}
	return nil
}

func istDate(t time.Time) pgtype.Date {
	d := clock.StartOfDayIST(t).In(clock.IST)
	return pgtype.Date{Time: time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}

// retryAfter is the time to the next IST midnight.
func retryAfter(now time.Time) time.Duration {
	return clock.StartOfDayIST(now).Add(24 * time.Hour).Sub(now)
}

// Reserve counts one call for feature and subject, or refuses it with a *CapError. Call it
// right before the provider call and Settle after it.
func (g *Guard) Reserve(ctx context.Context, feature string, s Subject) error {
	now := g.clock.Now()
	day := istDate(now)
	refuse := func(scope string) error {
		client.RecordCall(ctx, feature, "", client.OutcomeCapped)
		recordCapped(ctx, feature, scope)
		return &CapError{Scope: scope, RetryAfter: retryAfter(now)}
	}

	capPaise, err := g.settings.Paise(ctx, settings.AiDailyCostCapPaise)
	if err != nil {
		return err
	}
	q := aidb.New(g.db.Q(ctx))
	spend, err := q.PlatformSpend(ctx, day)
	if err != nil {
		return fmt.Errorf("guard: read spend: %w", err)
	}
	g.observe(now, spend, int64(capPaise))
	if int64(capPaise) <= 0 || spend >= int64(capPaise) {
		return refuse(ScopePlatform)
	}

	var capped string
	err = g.db.InTx(ctx, func(ctx context.Context) error {
		q := aidb.New(g.db.Q(ctx))
		take := func(scope string, subject ids.ID, key settings.Key) error {
			if subject == ids.Zero {
				return nil
			}
			n, err := g.settings.Int(ctx, key)
			if err != nil {
				return err
			}
			if n < 1 {
				capped = scope
				return errCapped
			}
			_, err = q.ReserveCall(ctx, aidb.ReserveCallParams{Day: day, Scope: scope, Subject: ids.UUID(subject), Now: now, Cap: int32(min(n, math.MaxInt32))})
			if db.IsNoRows(err) {
				capped = scope
				return errCapped
			}
			return err
		}
		storeScope, storeKey := ScopeStore, settings.AiDailyCallsStore
		if s.Background {
			storeScope, storeKey = ScopeStoreBackground, settings.AiDailyCallsStoreBackground
		}
		if err := take(storeScope, s.Store, storeKey); err != nil {
			return err
		}
		if err := take(ScopeCustomer, s.Customer, settings.AiDailyCallsCustomer); err != nil {
			return err
		}
		// The platform row counts every call (no call cap; the cost cap is checked above).
		_, err := q.ReserveCall(ctx, aidb.ReserveCallParams{Day: day, Scope: ScopePlatform, Subject: ids.UUID(ids.Zero), Now: now, Cap: math.MaxInt32})
		return err
	})
	if errors.Is(err, errCapped) {
		return refuse(capped)
	}
	if err != nil {
		return fmt.Errorf("guard: reserve: %w", err)
	}
	return nil
}

var errCapped = errors.New("guard: capped")

// Settle adds a finished call's cost to today's platform spend.
func (g *Guard) Settle(ctx context.Context, costPaise int64) error {
	if costPaise <= 0 {
		return nil
	}
	now := g.clock.Now()
	total, err := aidb.New(g.db.Q(ctx)).AddPlatformCost(ctx, aidb.AddPlatformCostParams{Day: istDate(now), CostPaise: costPaise, Now: now})
	if err != nil {
		return fmt.Errorf("guard: settle: %w", err)
	}
	capPaise, err := g.settings.Paise(ctx, settings.AiDailyCostCapPaise)
	if err != nil {
		return err
	}
	if g.observe(now, total, int64(capPaise)) {
		telemetry.Logger(ctx).WarnContext(ctx, "AI spend passed 80% of the daily cap",
			slog.Int64("spend_paise", total), slog.Int64("cap_paise", int64(capPaise)))
	}
	return nil
}

// observe records today's spend for the gauge and reports whether this is the first time
// today this process sees it at or above AlertRatio of the cap.
func (g *Guard) observe(now time.Time, spend, capPaise int64) bool {
	day := clock.StartOfDayIST(now)
	g.mu.Lock()
	defer g.mu.Unlock()
	if !day.Before(g.day) {
		g.day, g.spend, g.capPaise = day, spend, capPaise
	}
	if capPaise > 0 && float64(spend) >= AlertRatio*float64(capPaise) && !g.warnedDay.Equal(day) {
		g.warnedDay = day
		return true
	}
	return false
}

// UsedRatio is today's spend over the cap as this process last saw it (0 on a new day, 1 when
// the cap is 0). It is the value of tz_ai_cost_cap_used_ratio.
func (g *Guard) UsedRatio() float64 {
	now := g.clock.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.day.Equal(clock.StartOfDayIST(now)) {
		return 0
	}
	if g.capPaise <= 0 {
		return 1
	}
	return float64(g.spend) / float64(g.capPaise)
}

var (
	gaugeOnce sync.Once
	gaugeMu   sync.Mutex
	gaugeOf   *Guard
	mCapped   metric.Int64Counter
)

// registerGauge points the process's gauge at the newest guard (one per process in practice).
func registerGauge(g *Guard) {
	gaugeMu.Lock()
	gaugeOf = g
	gaugeMu.Unlock()
	gaugeOnce.Do(func() {
		m := otel.Meter("todayz/ai")
		mCapped, _ = m.Int64Counter("tz_ai_capped_total",
			metric.WithDescription("AI calls refused by a daily cap, by feature and cap (reason customer, store or platform)"))
		_, _ = m.Float64ObservableGauge("tz_ai_cost_cap_used_ratio",
			metric.WithDescription("Today's AI spend (IST day) over ai_daily_cost_cap_paise; alert at 0.8, calls stop at 1"),
			metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
				gaugeMu.Lock()
				g := gaugeOf
				gaugeMu.Unlock()
				if g != nil {
					o.Observe(g.UsedRatio())
				}
				return nil
			}))
	})
}

func recordCapped(ctx context.Context, feature, scope string) {
	if mCapped == nil {
		return
	}
	mCapped.Add(ctx, 1, metric.WithAttributes(
		attributeString("feature", telemetry.LabelValue("feature", feature)),
		attributeString("reason", telemetry.LabelValue("reason", scope)),
	))
}

func attributeString(k, v string) attribute.KeyValue { return attribute.String(k, v) }

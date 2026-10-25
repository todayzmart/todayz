package guard

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

type env struct {
	t   *testing.T
	tdb *testkit.DB
	st  *settings.Cache
	clk *clock.Fake
	g   *Guard
}

// 10:00 IST on 25 Oct 2026; the next IST midnight is 14 hours away.
var morning = time.Date(2026, 10, 25, 4, 30, 0, 0, time.UTC)

func newEnv(t *testing.T, p Providers) *env {
	t.Helper()
	tdb := testkit.NewDB(t)
	st := settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	clk := clock.NewFake(morning)
	return &env{t: t, tdb: tdb, st: st, clk: clk, g: New(db.New(tdb.Pool), st, clk, p)}
}

func (e *env) set(k settings.Key, v string) {
	e.t.Helper()
	_, err := e.tdb.OwnerPool.Exec(context.Background(), `UPDATE platform_settings SET value = $2 WHERE key = $1`, string(k), v)
	require.NoError(e.t, err)
	e.st.Invalidate()
}

func capScope(t *testing.T, err error) string {
	t.Helper()
	var ce *CapError
	require.ErrorAs(t, err, &ce)
	return ce.Scope
}

// SC-AI-01: with ai_daily_calls_customer = 2 the third call of the day is refused with
// 429 scope customer and Retry-After at IST midnight; the next IST day starts again.
func TestCustomerCap(t *testing.T) {
	e := newEnv(t, Providers{LLM: true})
	e.set(settings.AiDailyCallsCustomer, "2")
	ctx := context.Background()
	who := Subject{Customer: ids.New(morning)}

	require.NoError(t, e.g.Reserve(ctx, "search_assist", who))
	require.NoError(t, e.g.Reserve(ctx, "search_assist", who))
	err := e.g.Reserve(ctx, "search_assist", who)
	assert.Equal(t, ScopeCustomer, capScope(t, err))

	var ce *CapError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, 14*time.Hour, ce.RetryAfter)
	p := ce.Problem()
	assert.Equal(t, problems.AiDailyCapReached, p.Code)
	assert.Equal(t, "customer", p.Extensions["scope"])
	assert.Equal(t, "50400", p.Header.Get("Retry-After"))

	// Another customer is not affected.
	require.NoError(t, e.g.Reserve(ctx, "search_assist", Subject{Customer: ids.New(morning)}))

	// 23:59 IST is still today; 00:00 IST is a new day.
	e.clk.Set(time.Date(2026, 10, 25, 18, 29, 0, 0, time.UTC))
	assert.Equal(t, ScopeCustomer, capScope(t, e.g.Reserve(ctx, "search_assist", who)))
	e.clk.Set(time.Date(2026, 10, 25, 18, 30, 0, 0, time.UTC))
	require.NoError(t, e.g.Reserve(ctx, "search_assist", who))
}

func TestStoreCapAndZero(t *testing.T) {
	e := newEnv(t, Providers{LLM: true})
	e.set(settings.AiDailyCallsStore, "1")
	ctx := context.Background()
	store := Subject{Store: ids.New(morning)}
	require.NoError(t, e.g.Reserve(ctx, "voice_stock", store))
	assert.Equal(t, ScopeStore, capScope(t, e.g.Reserve(ctx, "voice_stock", store)))

	// 0 switches the scope off without a first call.
	e.set(settings.AiDailyCallsCustomer, "0")
	assert.Equal(t, ScopeCustomer, capScope(t, e.g.Reserve(ctx, "list_parse", Subject{Customer: ids.New(morning)})))

	// The store row is checked before the customer row and a refused store call counts nothing
	// for the customer.
	var calls int
	require.NoError(t, e.tdb.OwnerPool.QueryRow(ctx, `SELECT calls FROM ai_usage_daily WHERE scope = 'store'`).Scan(&calls))
	assert.Equal(t, 1, calls)
}

// D-228: background wording has its own per-store budget. Spending it leaves the owner's
// calls untouched, and the owner's calls never spend it.
func TestStoreBackgroundBudget(t *testing.T) {
	e := newEnv(t, Providers{LLM: true})
	e.set(settings.AiDailyCallsStoreBackground, "1")
	e.set(settings.AiDailyCallsStore, "1")
	ctx := context.Background()
	store := ids.New(morning)
	bg := Subject{Store: store, Background: true}

	require.NoError(t, e.g.Reserve(ctx, "insight_text", bg))
	assert.Equal(t, ScopeStoreBackground, capScope(t, e.g.Reserve(ctx, "coach_card", bg)))
	require.NoError(t, e.g.Reserve(ctx, "supplier_bill", Subject{Store: store}))
	assert.Equal(t, ScopeStore, capScope(t, e.g.Reserve(ctx, "supplier_bill", Subject{Store: store})))

	count := func(scope string) (n int) {
		require.NoError(t, e.tdb.OwnerPool.QueryRow(ctx, `SELECT coalesce(sum(calls), 0) FROM ai_usage_daily WHERE scope = $1`, scope).Scan(&n))
		return n
	}
	assert.Equal(t, 1, count(ScopeStoreBackground))
	assert.Equal(t, 1, count(ScopeStore))
	assert.Equal(t, 2, count(ScopePlatform), "the platform row counts every call that ran")
}

// SC-AI-04: the cost cap stops calls at 100% and the warning fires at 80%, once a day.
func TestCostCap(t *testing.T) {
	e := newEnv(t, Providers{LLM: true})
	e.set(settings.AiDailyCostCapPaise, "1000")
	ctx := context.Background()
	admin := Subject{}

	require.NoError(t, e.g.Reserve(ctx, "catalogue_cleanup", admin))
	require.NoError(t, e.g.Settle(ctx, 700))
	assert.InDelta(t, 0.7, e.g.UsedRatio(), 1e-9)
	assert.False(t, e.g.observe(e.clk.Now(), 700, 1000))

	require.NoError(t, e.g.Reserve(ctx, "catalogue_cleanup", admin))
	require.NoError(t, e.g.Settle(ctx, 100))
	assert.InDelta(t, 0.8, e.g.UsedRatio(), 1e-9)
	// Settle already crossed 80% today, so a second crossing does not warn again.
	assert.False(t, e.g.observe(e.clk.Now(), 900, 1000))

	require.NoError(t, e.g.Reserve(ctx, "catalogue_cleanup", admin))
	require.NoError(t, e.g.Settle(ctx, 250)) // a call may overshoot; the next one is refused
	assert.Equal(t, ScopePlatform, capScope(t, e.g.Reserve(ctx, "catalogue_cleanup", admin)))
	assert.Equal(t, ScopePlatform, capScope(t, e.g.Reserve(ctx, "search_assist", Subject{Customer: ids.New(morning)})))
	assert.GreaterOrEqual(t, e.g.UsedRatio(), 1.0)

	// Raising the cap in platform_settings lets calls run again at once.
	e.set(settings.AiDailyCostCapPaise, "5000")
	require.NoError(t, e.g.Reserve(ctx, "catalogue_cleanup", admin))

	// A cap of 0 stops every call.
	e.set(settings.AiDailyCostCapPaise, "0")
	assert.Equal(t, ScopePlatform, capScope(t, e.g.Reserve(ctx, "catalogue_cleanup", admin)))

	// A new IST day starts at 0 spend and may warn again.
	e.set(settings.AiDailyCostCapPaise, "1000")
	e.clk.Advance(24 * time.Hour)
	assert.Zero(t, e.g.UsedRatio())
	require.NoError(t, e.g.Reserve(ctx, "catalogue_cleanup", admin))
	assert.True(t, e.g.observe(e.clk.Now(), 800, 1000))
}

func TestWarnOnceAt80(t *testing.T) {
	g := &Guard{}
	now := morning
	assert.False(t, g.observe(now, 799, 1000))
	assert.True(t, g.observe(now, 800, 1000))
	assert.False(t, g.observe(now, 950, 1000))
	assert.True(t, g.observe(now.Add(24*time.Hour), 900, 1000))
	assert.False(t, g.observe(now.Add(48*time.Hour), 900, 0), "a cap of 0 is a kill switch, not an alert")
}

// SC-AI-05: a feature works only with its flags on and its provider keys set.
func TestAvailable(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, Providers{LLM: true})
	ok, err := e.g.Available(ctx, "list_parse")
	require.NoError(t, err)
	assert.False(t, ok, "ai_listparse_enabled is off in the seed")
	err = e.g.Require(ctx, "list_parse")
	var pe *problem.Error
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, problems.NotFound, pe.Code)

	e.set(settings.AiListparseEnabled, "true")
	require.NoError(t, e.g.Require(ctx, "list_parse"))

	// voice_stock needs both its flags and both providers.
	e.set(settings.AiVoiceStockEnabled, "true")
	ok, _ = e.g.Available(ctx, "voice_stock")
	assert.False(t, ok, "no STT key")
	g2 := New(db.New(e.tdb.Pool), e.st, e.clk, Providers{LLM: true, STT: true})
	ok, _ = g2.Available(ctx, "voice_stock")
	assert.True(t, ok)
	e.set(settings.FeatureVoiceEnabled, "false")
	ok, _ = g2.Available(ctx, "voice_stock")
	assert.False(t, ok)

	// No LLM key hides the feature whatever the flag says.
	g3 := New(db.New(e.tdb.Pool), e.st, e.clk, Providers{})
	ok, _ = g3.Available(ctx, "list_parse")
	assert.False(t, ok)

	_, err = e.g.Available(ctx, "no_such_feature")
	assert.True(t, errors.Is(err, ErrUnknownFeature))
}

func TestProvidersFrom(t *testing.T) {
	c := &config.Config{Env: config.EnvLocal}
	assert.Equal(t, Providers{}, ProvidersFrom(c), "no base URL")
	c.Providers.LLMAPIURL = "https://mocks.todayz.localhost/anthropic"
	assert.Equal(t, Providers{}, ProvidersFrom(c), "no key and no mock")
	c.Providers.MockLLM = true
	assert.Equal(t, Providers{LLM: true}, ProvidersFrom(c), "the mock counts in local")
	c.Env = config.EnvProd
	assert.Equal(t, Providers{}, ProvidersFrom(c), "the mock never counts when deployed")
	c.Providers.AnthropicAPIKey = config.NewSecret([]byte("k"))
	assert.Equal(t, Providers{LLM: true}, ProvidersFrom(c))
}

// Every feature in the client list has a Spec, except the platform's own eval suite, and
// every Spec names a known feature.
func TestFeaturesMatchClient(t *testing.T) {
	for name := range Features {
		assert.True(t, client.KnownFeature(name), name)
	}
	for _, name := range client.Features {
		if name == "ai_selftest" {
			continue
		}
		_, ok := Features[name]
		assert.True(t, ok, name)
	}
	assert.False(t, slices.Contains(keys(), "ai_selftest"))
}

func keys() []string {
	out := make([]string, 0, len(Features))
	for k := range Features {
		out = append(out, k)
	}
	return out
}

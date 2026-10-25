// Package retailtest is the database test setup the retailer AI drafts share: a fresh
// database with the dev seed, the settings cache, the guard and a retail.Kit that runs the
// background work inline, plus a fake model that answers a fixed tool input.
package retailtest

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/seed"
	"github.com/todayz/backend/internal/testkit"
)

// The dev seed's Sri Lakshmi store and its owner (internal/seed/business.go).
var (
	Store = mustID("01KE0S1W6W8ZV7T1C3A6MS0001")
	Owner = mustID("01KE0S1W6W8ZV7T1C3A6M9N011")
)

func mustID(s string) ids.ID {
	id, err := ids.Parse(s)
	if err != nil {
		panic(err)
	}
	return id
}

// Env is one test's database and the retail kit over it.
type Env struct {
	Ctx      context.Context
	TDB      *testkit.DB
	DB       *db.DB
	Settings *settings.Cache
	Clock    *clock.Fake
	Guard    *guard.Guard
	Kit      retail.Kit
	LLM      *LLM
	Scope    authz.Scope
	Owner    authn.Principal
}

// New is a fresh seeded database at the seed's epoch, with the background work inline.
func New(t *testing.T) *Env {
	t.Helper()
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	enc, hash := make([]byte, 32), make([]byte, 32)
	_, err := rand.Read(enc)
	require.NoError(t, err)
	_, err = rand.Read(hash)
	require.NoError(t, err)
	keys := config.AppKeys{PIIEncKey: config.NewSecret(enc), PIIHashKey: config.NewSecret(hash)}
	d := db.New(tdb.Pool)
	require.NoError(t, seed.Run(ctx, config.EnvTest, seed.Dev, seed.Deps{DB: d, Keys: keys}))
	st := settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	clk := clock.NewFake(seed.Epoch)
	g := guard.New(d, st, clk, guard.Providers{LLM: true})
	e := &Env{Ctx: ctx, TDB: tdb, DB: d, Settings: st, Clock: clk, Guard: g, LLM: &LLM{},
		Kit:   retail.Kit{Clock: clk, Guard: g, Drafts: drafts.New(d, clk), Go: func(f func()) { f() }},
		Owner: authn.Principal{UserID: Owner, App: authn.AppBusiness}}
	var biz pgtype.UUID
	require.NoError(t, tdb.OwnerPool.QueryRow(ctx, `SELECT business_id FROM stores WHERE id = $1`, ids.UUID(Store)).Scan(&biz))
	bid, ok := ids.FromUUID(biz)
	require.True(t, ok)
	e.Scope = authz.Scope{BusinessID: bid, StoreID: Store}
	return e
}

// Set changes one platform setting.
func (e *Env) Set(t *testing.T, k settings.Key, v string) {
	t.Helper()
	_, err := e.TDB.OwnerPool.Exec(e.Ctx, `UPDATE platform_settings SET value = $2 WHERE key = $1`, string(k), v)
	require.NoError(t, err)
	e.Settings.Invalidate()
}

// Count runs a count(*) query as the owner.
func (e *Env) Count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, e.TDB.OwnerPool.QueryRow(e.Ctx, sql, args...).Scan(&n), sql)
	return n
}

// Draft is the stored draft.
func (e *Env) Draft(t *testing.T, feature string, id ids.ID) drafts.Draft {
	t.Helper()
	d, err := e.Kit.Drafts.Get(e.Ctx, feature, id)
	require.NoError(t, err)
	return d
}

// Photo is a tiny image; the fake model never looks at it.
func Photo() client.Image {
	return client.Image{MediaType: "image/jpeg", Data: []byte{0xff, 0xd8, 0xff, 0xd9}}
}

// LLM is a fake model: every call answers Reply's tool input (or Err).
type LLM struct {
	mu    sync.Mutex
	calls int
	Reply any
	Err   error
}

// Complete answers the fixed tool input.
func (f *LLM) Complete(_ context.Context, _ client.Request) (client.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.Err != nil {
		return client.Response{}, f.Err
	}
	b, err := json.Marshal(f.Reply)
	if err != nil {
		return client.Response{}, err
	}
	return client.Response{Model: "fake", ToolInput: b, StopReason: "tool_use", CostPaise: 1}, nil
}

// Calls is how many calls the model has had.
func (f *LLM) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Problem is err as a problem, failing the test when it is not one.
func Problem(t *testing.T, err error) *problem.Error {
	t.Helper()
	var pe *problem.Error
	require.ErrorAs(t, err, &pe)
	return pe
}

// Item is one of the store's items as a test names it.
type Item struct {
	ID       ids.ID
	Name     string
	OnHand   int
	MrpPaise int64
}

// Items is n items of the store that have an MRP, in id order.
func (e *Env) Items(t *testing.T, n int) []Item {
	t.Helper()
	rows, err := e.TDB.OwnerPool.Query(e.Ctx, `SELECT si.id, coalesce(si.local_name, p.name), si.on_hand,
		  coalesce(si.mrp_paise, p.mrp_paise)
		FROM store_items si JOIN products p ON p.id = si.product_id
		WHERE si.store_id = $1 AND coalesce(si.mrp_paise, p.mrp_paise) IS NOT NULL
		ORDER BY si.id LIMIT $2`, ids.UUID(Store), n)
	require.NoError(t, err)
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var u pgtype.UUID
		var it Item
		require.NoError(t, rows.Scan(&u, &it.Name, &it.OnHand, &it.MrpPaise))
		it.ID, _ = ids.FromUUID(u)
		out = append(out, it)
	}
	require.NoError(t, rows.Err())
	require.Len(t, out, n, "the dev seed has too few priced items")
	return out
}

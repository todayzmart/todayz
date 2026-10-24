package baskets

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	basketsapi "github.com/todayz/backend/gen/api/baskets"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

// Saved baskets (08 BSK-01, 02, 03, 12, 13).

func TestMain(m *testing.M) { testkit.Main(m) }

var testNow = time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)

const catDairy = "0190c47e-0000-7000-8000-000000000102" // seeded by 10501

// normalize stands in for search.Normalize (domains never import each other).
func normalize(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

type env struct {
	t     *testing.T
	tdb   *testkit.DB
	svc   *Service
	cache *settings.Cache
	ctx   context.Context
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	e := &env{t: t, tdb: tdb, ctx: context.Background()}
	e.cache = settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	var err error
	e.svc, err = New(Deps{DB: db.New(tdb.Pool), Clock: clock.NewFake(testNow), Settings: e.cache, Normalize: normalize})
	require.NoError(t, err)
	return e
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.tdb.OwnerPool.Exec(e.ctx, sql, args...)
	require.NoError(e.t, err, sql)
}

func (e *env) user() ids.ID {
	id := ids.New(testNow)
	e.exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(id))
	return id
}

func (e *env) product(name string) string {
	id := ids.New(testNow)
	e.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, barcodes) VALUES ($1, $2, $3, 100, 'g', '{}')`,
		ids.UUID(id), name, catDairy)
	return id.String()
}

func (e *env) setting(k settings.Key, v string) {
	e.exec(`UPDATE platform_settings SET value = $2 WHERE key = $1`, string(k), v)
	e.cache.Invalidate()
}

func text(s string, qty int) basketsapi.BasketLineInput {
	return basketsapi.BasketLineInput{Text: &s, Quantity: qty}
}

func prod(id string, qty int) basketsapi.BasketLineInput {
	return basketsapi.BasketLineInput{ProductId: &id, Quantity: qty}
}

func (e *env) create(u ids.ID, name string, lines ...basketsapi.BasketLineInput) basketsapi.Basket {
	e.t.Helper()
	if lines == nil {
		lines = []basketsapi.BasketLineInput{text("milk", 1)}
	}
	b, err := e.svc.create(e.ctx, u, basketsapi.BasketCreate{Name: name, Lines: lines})
	require.NoError(e.t, err)
	return b
}

func bid(t *testing.T, b basketsapi.Basket) ids.ID {
	id, err := ids.Parse(b.BasketId)
	require.NoError(t, err)
	return id
}

// requireProblem checks the problem code and, when path is set, the field.
func requireProblem(t *testing.T, err error, code, path string) {
	t.Helper()
	require.Error(t, err)
	var p *problem.Error
	require.ErrorAs(t, err, &p)
	require.Equal(t, code, p.Code, err.Error())
	if path != "" {
		require.Contains(t, p.Fields, path)
	}
}

func sp(s string) *string { return &s }

func TestSavedLimitBSK01(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	var first basketsapi.Basket
	for i := range 10 {
		b := e.create(u, "Basket "+string(rune('A'+i)))
		if i == 0 {
			first = b
		}
	}
	_, err := e.svc.create(e.ctx, u, basketsapi.BasketCreate{Name: "Eleventh", Lines: []basketsapi.BasketLineInput{text("milk", 1)}})
	requireProblem(t, err, genproblems.BasketLimitReached, "")
	assert.Equal(t, 422, problem.Status(err))

	require.NoError(t, e.svc.remove(e.ctx, u, bid(t, first)))
	var deletedAt *time.Time
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT deleted_at FROM baskets WHERE id = $1`, ids.UUID(bid(t, first))).Scan(&deletedAt))
	assert.NotNil(t, deletedAt, "a soft delete")
	list, err := e.svc.list(e.ctx, u)
	require.NoError(t, err)
	assert.Len(t, list.Baskets, 9)
	for _, s := range list.Baskets {
		assert.NotEqual(t, first.BasketId, s.BasketId)
	}
	e.create(u, "Eleventh")

	e.setting(settings.BasketMaxSaved, "3")
	_, err = e.svc.create(e.ctx, u, basketsapi.BasketCreate{Name: "More", Lines: []basketsapi.BasketLineInput{text("milk", 1)}})
	requireProblem(t, err, genproblems.BasketLimitReached, "")
	e.create(e.user(), "Another customer's first")
}

func TestLinesBSK02BSK03(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	butter := e.product("Amul Butter")

	many := make([]basketsapi.BasketLineInput, 31)
	for i := range many {
		many[i] = text("item "+string(rune('a'+i%26)), 1)
	}
	b := e.create(u, "Thirty", many[:30]...)
	assert.Len(t, b.Lines, 30)
	_, err := e.svc.create(e.ctx, u, basketsapi.BasketCreate{Name: "Too many", Lines: many})
	requireProblem(t, err, genproblems.ValidationFailed, "body.lines")
	_, err = e.svc.update(e.ctx, u, bid(t, b), sp(`"v1"`), basketsapi.BasketPatch{Lines: &many})
	requireProblem(t, err, genproblems.ValidationFailed, "body.lines")

	bad := func(path string, name string, lines ...basketsapi.BasketLineInput) {
		t.Helper()
		_, err := e.svc.create(e.ctx, u, basketsapi.BasketCreate{Name: name, Lines: lines})
		requireProblem(t, err, genproblems.ValidationFailed, path)
	}
	bad("body.lines[0].quantity", "Q", text("milk", 0))
	bad("body.lines[1].quantity", "Q", text("milk", 1), text("curd", 100))
	bad("body.name", strings.Repeat("n", 41), text("milk", 1))
	bad("body.name", "   ", text("milk", 1))
	bad("body.lines[0]", "Both", basketsapi.BasketLineInput{ProductId: &butter, Text: sp("butter"), Quantity: 1})
	bad("body.lines[0]", "Neither", basketsapi.BasketLineInput{Quantity: 1})
	bad("body.lines[0].text", "Empty", text(" !! ", 1))
	bad("body.lines[0].productId", "Unknown", prod(ids.New(testNow).String(), 1))
	e.create(u, strings.Repeat("n", 40), text("milk", 99))

	e.setting(settings.BasketMaxItems, "5")
	bad("body.lines", "Six", many[:6]...)

	// BSK-03: free text is stored normalised; a product line keeps its id.
	b = e.create(u, "Mixed", text("  Amull  BUTTR! ", 2), prod(butter, 1))
	require.Len(t, b.Lines, 2)
	require.NotNil(t, b.Lines[0].Text)
	assert.Equal(t, "amull buttr", *b.Lines[0].Text)
	assert.Nil(t, b.Lines[0].ProductId)
	assert.Equal(t, 2, b.Lines[0].Quantity)
	require.NotNil(t, b.Lines[1].ProductId)
	assert.Equal(t, butter, *b.Lines[1].ProductId)
	require.NotNil(t, b.Lines[1].ProductName)
	assert.Equal(t, "Amul Butter", *b.Lines[1].ProductName)
	assert.Equal(t, 1, b.Lines[1].Position)
}

func TestOwnershipBSK12(t *testing.T) {
	e := newEnv(t)
	a, other := e.user(), e.user()
	b := e.create(a, "A's")
	id := bid(t, b)

	_, err := e.svc.get(e.ctx, other, id)
	assert.Equal(t, 404, problem.Status(err))
	_, err = e.svc.update(e.ctx, other, id, sp(`"v1"`), basketsapi.BasketPatch{Name: sp("Mine now")})
	assert.Equal(t, 404, problem.Status(err))
	assert.Equal(t, 404, problem.Status(e.svc.remove(e.ctx, other, id)))
	list, err := e.svc.list(e.ctx, other)
	require.NoError(t, err)
	assert.Empty(t, list.Baskets)

	got, err := e.svc.get(e.ctx, a, id)
	require.NoError(t, err)
	assert.Equal(t, "A's", got.Name)

	gone := e.create(a, "Gone")
	require.NoError(t, e.svc.remove(e.ctx, a, bid(t, gone)))
	_, err = e.svc.get(e.ctx, a, bid(t, gone))
	assert.Equal(t, 404, problem.Status(err))
	assert.Equal(t, 404, problem.Status(e.svc.remove(e.ctx, a, bid(t, gone))))

	// Account deletion removes the customer's baskets and lines, and only theirs.
	e.create(other, "Other's")
	require.NoError(t, e.svc.OnAccountDelete(e.ctx, a))
	require.NoError(t, e.svc.OnAccountDelete(e.ctx, a), "runs twice without harm")
	var baskets, lines int
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT
	    (SELECT count(*) FROM baskets WHERE customer_id = $1),
	    (SELECT count(*) FROM basket_lines l JOIN baskets b ON b.id = l.basket_id WHERE b.customer_id = $1)`,
		ids.UUID(a)).Scan(&baskets, &lines))
	assert.Zero(t, baskets)
	assert.Zero(t, lines)
	list, err = e.svc.list(e.ctx, other)
	require.NoError(t, err)
	assert.Len(t, list.Baskets, 1)
	var orphan int
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT count(*) FROM basket_lines WHERE basket_id = $1`, ids.UUID(id)).Scan(&orphan))
	assert.Zero(t, orphan, "lines go by cascade")
}

func TestConcurrencyBSK13(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	b := e.create(u, "Weekly", text("milk", 1), text("curd", 2))
	id := bid(t, b)
	assert.Equal(t, 1, b.Version)

	_, err := e.svc.update(e.ctx, u, id, nil, basketsapi.BasketPatch{Name: sp("x")})
	requireProblem(t, err, genproblems.PreconditionRequired, "")
	assert.Equal(t, 428, problem.Status(err))

	lines := []basketsapi.BasketLineInput{text("atta", 1)}
	b, err = e.svc.update(e.ctx, u, id, sp(`"v1"`), basketsapi.BasketPatch{Lines: &lines})
	require.NoError(t, err)
	assert.Equal(t, 2, b.Version)
	require.Len(t, b.Lines, 1, "lines replace the whole set")
	assert.Equal(t, "atta", *b.Lines[0].Text)
	assert.Equal(t, "Weekly", b.Name, "name unchanged when not sent")

	stale := []basketsapi.BasketLineInput{text("ghee", 1)}
	_, err = e.svc.update(e.ctx, u, id, sp(`"v1"`), basketsapi.BasketPatch{Lines: &stale})
	requireProblem(t, err, genproblems.PreconditionFailed, "")
	assert.Equal(t, 412, problem.Status(err))
	_, err = e.svc.update(e.ctx, u, id, sp(`garbage`), basketsapi.BasketPatch{Lines: &stale})
	assert.Equal(t, 412, problem.Status(err))

	// A failing line leaves the old set.
	failing := []basketsapi.BasketLineInput{text("ghee", 1), text("milk", 0)}
	_, err = e.svc.update(e.ctx, u, id, sp(`"v2"`), basketsapi.BasketPatch{Lines: &failing})
	requireProblem(t, err, genproblems.ValidationFailed, "body.lines[1].quantity")
	got, err := e.svc.get(e.ctx, u, id)
	require.NoError(t, err)
	assert.Equal(t, 2, got.Version)
	require.Len(t, got.Lines, 1)
	assert.Equal(t, "atta", *got.Lines[0].Text)

	// lastSplit: kept until the basket is searched again; only its own lines.
	line := got.Lines[0].LineId
	store := ids.New(testNow).String()
	split := basketsapi.BasketSplit{
		SearchedAt: testNow,
		Stores: []basketsapi.BasketSplitStore{
			{StoreId: store, StoreSlug: "shop-a", StoreName: "Shop A", LineIds: []string{line}},
		},
		UncoveredLineIds: []string{},
	}
	got, err = e.svc.update(e.ctx, u, id, sp(`"v2"`), basketsapi.BasketPatch{LastSplit: &split})
	require.NoError(t, err)
	require.NotNil(t, got.LastSplit)
	assert.Equal(t, []string{line}, got.LastSplit.Stores[0].LineIds)
	assert.Equal(t, 3, got.Version)

	foreign := split
	foreign.Stores = []basketsapi.BasketSplitStore{{StoreId: store, StoreSlug: "shop-a", StoreName: "Shop A", LineIds: []string{ids.New(testNow).String()}}}
	_, err = e.svc.update(e.ctx, u, id, sp(`"v3"`), basketsapi.BasketPatch{LastSplit: &foreign})
	requireProblem(t, err, genproblems.ValidationFailed, "")
	_, err = e.svc.update(e.ctx, u, id, sp(`"v3"`), basketsapi.BasketPatch{LastSplit: &split, Lines: &lines})
	requireProblem(t, err, genproblems.ValidationFailed, "body.lastSplit")

	// New lines clear the split.
	got, err = e.svc.update(e.ctx, u, id, sp(`"v3"`), basketsapi.BasketPatch{Lines: &lines, Name: sp("Renamed")})
	require.NoError(t, err)
	assert.Nil(t, got.LastSplit)
	assert.Equal(t, "Renamed", got.Name)
	assert.Equal(t, 4, got.Version)
}

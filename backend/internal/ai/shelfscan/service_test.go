package shelfscan_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/ai/retail/retailtest"
	"github.com/todayz/backend/internal/ai/shelfscan"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// items records the store items a confirm asks the catalogue to create.
type items struct {
	mu   sync.Mutex
	adds []shelfscan.NewItem
	// refuse is a product the catalogue refuses for want of a permission.
	refuse ids.ID
}

func (f *items) Add(_ context.Context, _ authz.Scope, it shelfscan.NewItem) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if it.ProductID == f.refuse {
		return retail.RefuseNotPermitted
	}
	f.adds = append(f.adds, it)
	return nil
}

func (f *items) calls() []shelfscan.NewItem {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]shelfscan.NewItem(nil), f.adds...)
}

type product struct {
	ID       ids.ID
	Name     string
	MrpPaise int64
}

// products is n active master products with an MRP, unlisted (or listed) at the store.
func products(t *testing.T, e *retailtest.Env, n int, listed bool) []product {
	t.Helper()
	rows, err := e.TDB.OwnerPool.Query(e.Ctx, `SELECT DISTINCT ON (lower(p.name)) p.id, p.name, p.mrp_paise
		FROM products p
		WHERE p.status = 'active' AND p.mrp_paise IS NOT NULL
		  AND EXISTS (SELECT 1 FROM store_items si WHERE si.product_id = p.id AND si.store_id = $1) = $2
		  AND length(p.name) > 8
		ORDER BY lower(p.name), p.id LIMIT $3`, ids.UUID(retailtest.Store), listed, n)
	require.NoError(t, err)
	defer rows.Close()
	var out []product
	for rows.Next() {
		var u pgtype.UUID
		var p product
		require.NoError(t, rows.Scan(&u, &p.Name, &p.MrpPaise))
		p.ID, _ = ids.FromUUID(u)
		out = append(out, p)
	}
	require.NoError(t, rows.Err())
	require.Len(t, out, n, "the dev seed has too few products")
	return out
}

func newService(t *testing.T, e *retailtest.Env, it shelfscan.Items) *shelfscan.Service {
	t.Helper()
	e.Set(t, settings.AiShelfscanEnabled, "true")
	s, err := shelfscan.New(shelfscan.Deps{DB: e.DB, Clock: e.Clock, Settings: e.Settings, LLM: e.LLM, Kit: e.Kit, Items: it})
	require.NoError(t, err)
	return s
}

func shelf(names ...string) shelfscan.ToolOutput {
	out := shelfscan.ToolOutput{}
	for _, n := range names {
		out.Products = append(out.Products, shelfscan.ToolProduct{Name: n, Confidence: 0.9})
	}
	return out
}

// TestShelfDraftCreatesNothingUntilConfirm (SC-AI-16): a read shelf is a ready draft with
// matched products and no store item; the confirm creates only the accepted and edited
// lines, at the MRP or the edited price.
func TestShelfDraftCreatesNothingUntilConfirm(t *testing.T) {
	e := retailtest.New(t)
	f := &items{}
	s := newService(t, e, f)
	ps := products(t, e, 2, false)
	e.LLM.Reply = shelf(ps[0].Name, ps[1].Name, "Qzxv Wjk Plorb")
	before := e.Count(t, `SELECT count(*) FROM store_items WHERE store_id = $1`, ids.UUID(retailtest.Store))

	id, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	require.NoError(t, err)
	v, err := s.Get(e.Ctx, e.Scope, id)
	require.NoError(t, err)
	require.Equal(t, aiapi.AiDraftStatus("ready"), v.Status)
	require.Len(t, v.Lines, 3)
	require.NotNil(t, v.Lines[0].Product)
	require.Equal(t, ps[0].ID.String(), v.Lines[0].Product.ProductId)
	require.False(t, v.Lines[0].Listed)
	require.NotNil(t, v.Lines[1].Product)
	require.Nil(t, v.Lines[2].Product)
	require.Empty(t, f.calls(), "no item before the confirm")
	require.Equal(t, before, e.Count(t, `SELECT count(*) FROM store_items WHERE store_id = $1`, ids.UUID(retailtest.Store)))

	opening := 4
	res, err := s.Confirm(e.Ctx, e.Scope, e.Owner, id, []aiapi.ShelfPhotoDecision{
		{LineId: "l1", Outcome: aiapi.AiConfirmOutcomeAccepted},
		{LineId: "l2", Outcome: aiapi.AiConfirmOutcomeEdited, Value: &aiapi.ShelfPhotoLineValue{
			ProductId: ps[1].ID.String(), PricePaise: int(ps[1].MrpPaise - 100), OpeningStock: &opening}},
		{LineId: "l3", Outcome: aiapi.AiConfirmOutcomeDiscarded},
	})
	require.NoError(t, err)
	require.Equal(t, drafts.StatusConfirmed, res.Status)
	adds := f.calls()
	require.Equal(t, []shelfscan.NewItem{
		{ProductID: ps[0].ID, PricePaise: ps[0].MrpPaise},
		{ProductID: ps[1].ID, PricePaise: ps[1].MrpPaise - 100, OpeningStock: 4},
	}, adds)
}

// TestShelfConfirmRefuses (SC-AI-16): a product the store already sells, a price above MRP,
// an unmatched accepted line and a line the catalogue refuses for want of a permission
// (item.price.edit, W3-11 review) are refused alone with their reason; the good line among
// them is created, the draft stays ready, and the next confirm creates only the fixed line.
func TestShelfConfirmRefuses(t *testing.T) {
	e := retailtest.New(t)
	ps := products(t, e, 2, false)
	listed := products(t, e, 1, true)
	e.LLM.Reply = shelf(ps[0].Name, ps[1].Name, "Qzxv Wjk Plorb")
	accept := func(l string) aiapi.ShelfPhotoDecision {
		return aiapi.ShelfPhotoDecision{LineId: l, Outcome: aiapi.AiConfirmOutcomeAccepted}
	}
	edit := func(p product, price int64) aiapi.ShelfPhotoDecision {
		return aiapi.ShelfPhotoDecision{LineId: "l1", Outcome: aiapi.AiConfirmOutcomeEdited,
			Value: &aiapi.ShelfPhotoLineValue{ProductId: p.ID.String(), PricePaise: int(price)}}
	}
	for _, tc := range []struct {
		name   string
		l1     aiapi.ShelfPhotoDecision
		reason string
		refuse bool
	}{
		{"already listed", edit(listed[0], listed[0].MrpPaise), "invalid_listed", false},
		{"above mrp", edit(ps[0], ps[0].MrpPaise+1), "invalid_price_above_mrp", false},
		{"no price edit permission", accept("l1"), "not_permitted", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &items{}
			if tc.refuse {
				f.refuse = ps[0].ID
			}
			s := newService(t, e, f)
			id, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
			require.NoError(t, err)

			res, err := s.Confirm(e.Ctx, e.Scope, e.Owner, id, []aiapi.ShelfPhotoDecision{tc.l1, accept("l2"), accept("l3")})
			require.NoError(t, err)
			require.Equal(t, drafts.StatusReady, res.Status)
			require.Equal(t, []drafts.LineResult{
				{LineID: "l1", Outcome: drafts.NotPermitted, Reason: tc.reason},
				{LineID: "l2", Outcome: drafts.Accepted},
				{LineID: "l3", Outcome: drafts.NotPermitted, Reason: "invalid_product"},
			}, res.Lines)
			require.Equal(t, []shelfscan.NewItem{{ProductID: ps[1].ID, PricePaise: ps[1].MrpPaise}}, f.calls())
			require.Equal(t, drafts.StatusReady, e.Draft(t, shelfscan.Feature, id).Status)

			f.refuse = ids.Zero
			res, err = s.Confirm(e.Ctx, e.Scope, e.Owner, id, []aiapi.ShelfPhotoDecision{
				edit(ps[0], ps[0].MrpPaise), accept("l2"), {LineId: "l3", Outcome: aiapi.AiConfirmOutcomeDiscarded},
			})
			require.NoError(t, err)
			require.Equal(t, drafts.StatusConfirmed, res.Status)
			require.Equal(t, []shelfscan.NewItem{
				{ProductID: ps[1].ID, PricePaise: ps[1].MrpPaise}, {ProductID: ps[0].ID, PricePaise: ps[0].MrpPaise},
			}, f.calls(), "l2 is not created twice")
		})
	}
}

// TestShelfHiddenWhenOff (SC-AI-24): with ai_shelfscan_enabled off the create is the common
// 404 and the model is never called.
func TestShelfHiddenWhenOff(t *testing.T) {
	e := retailtest.New(t)
	s, err := shelfscan.New(shelfscan.Deps{DB: e.DB, Clock: e.Clock, Settings: e.Settings, LLM: e.LLM, Kit: e.Kit, Items: &items{}})
	require.NoError(t, err)
	_, err = s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	require.Equal(t, "not_found", retailtest.Problem(t, err).Code)
	require.Zero(t, e.LLM.Calls())
}

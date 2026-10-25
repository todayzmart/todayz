package billscan_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/require"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/ai/billscan"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/ai/retail/retailtest"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// stock records the stock ins a confirm asks for.
type stock struct {
	mu  sync.Mutex
	ins []billscan.StockIn
	// gone is an item the catalogue answers ErrItemGone for.
	gone ids.ID
}

func (s *stock) AddFromBill(_ context.Context, in billscan.StockIn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.ItemID == s.gone {
		return billscan.ErrItemGone
	}
	s.ins = append(s.ins, in)
	return nil
}

func (s *stock) calls() []billscan.StockIn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]billscan.StockIn(nil), s.ins...)
}

func newService(t *testing.T, e *retailtest.Env, st billscan.Stock) *billscan.Service {
	t.Helper()
	e.Set(t, settings.AiBillscanEnabled, "true")
	s, err := billscan.New(billscan.Deps{DB: e.DB, Clock: e.Clock, Settings: e.Settings, LLM: e.LLM, Kit: e.Kit, Stock: st})
	require.NoError(t, err)
	return s
}

func rupees(p int64) *float64 { r := float64(p) / 100; return &r }
func strp(s string) *string   { return &s }

// bill is a model answer of two lines naming the store's items and one naming nothing it sells.
func bill(a, b retailtest.Item) billscan.ToolOutput {
	return billscan.ToolOutput{Supplier: strp("Sri Venkateshwara Agencies"), Lines: []billscan.ToolLine{
		{Name: a.Name, Quantity: 6, CostRupees: rupees(a.MrpPaise - 100), MrpRupees: rupees(a.MrpPaise), Confidence: 0.94},
		{Name: b.Name, Quantity: 12, CostRupees: rupees(b.MrpPaise - 100), Confidence: 0.81},
		{Name: "Qzxv Wjk Plorb", Quantity: 2, Confidence: 0.4},
	}}
}

func onHand(t *testing.T, e *retailtest.Env, it retailtest.Item) int {
	t.Helper()
	return e.Count(t, `SELECT on_hand FROM store_items WHERE id = $1`, ids.UUID(it.ID))
}

// TestDraftWritesNothingUntilConfirm (SC-AI-11, SC-AI-13): a read bill is a ready draft with
// matched lines and no stock change; the confirm writes only the accepted and edited lines,
// and a repeat answers the first result without writing again.
func TestDraftWritesNothingUntilConfirm(t *testing.T) {
	e := retailtest.New(t)
	st := &stock{}
	s := newService(t, e, st)
	items := e.Items(t, 2)
	a, b := items[0], items[1]
	e.LLM.Reply = bill(a, b)
	before := e.Count(t, `SELECT count(*) FROM stock_movements WHERE store_id = $1`, ids.UUID(retailtest.Store))

	id, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	require.NoError(t, err)
	require.Equal(t, 1, e.LLM.Calls())
	v, err := s.Get(e.Ctx, e.Scope, id)
	require.NoError(t, err)
	require.Equal(t, aiapi.AiDraftStatus("ready"), v.Status)
	require.Len(t, v.Lines, 3)
	require.NotNil(t, v.Lines[0].Match)
	require.Equal(t, a.ID.String(), v.Lines[0].Match.StoreItemId)
	require.Equal(t, a.OnHand, v.Lines[0].Match.OnHand)
	require.NotNil(t, v.Lines[1].Match)
	require.Equal(t, b.ID.String(), v.Lines[1].Match.StoreItemId)
	require.Nil(t, v.Lines[2].Match, "a line the store does not sell has no match")

	require.Empty(t, st.calls(), "no stock in before the confirm")
	require.Equal(t, a.OnHand, onHand(t, e, a))
	require.Equal(t, before, e.Count(t, `SELECT count(*) FROM stock_movements WHERE store_id = $1`, ids.UUID(retailtest.Store)))

	edited := &aiapi.SupplierBillLineValue{StoreItemId: b.ID.String(), Quantity: 3}
	in := []aiapi.SupplierBillDecision{
		{LineId: "l1", Outcome: aiapi.AiConfirmOutcomeAccepted},
		{LineId: "l2", Outcome: aiapi.AiConfirmOutcomeEdited, Value: edited},
		{LineId: "l3", Outcome: aiapi.AiConfirmOutcomeDiscarded},
	}
	res, err := s.Confirm(e.Ctx, e.Scope, e.Owner, id, in)
	require.NoError(t, err)
	require.Equal(t, drafts.StatusConfirmed, res.Status)
	require.Equal(t, []drafts.LineResult{{LineID: "l1", Outcome: drafts.Accepted}, {LineID: "l2", Outcome: drafts.Edited},
		{LineID: "l3", Outcome: drafts.Discarded}}, res.Lines)
	ins := st.calls()
	require.Len(t, ins, 2)
	require.Equal(t, a.ID, ins[0].ItemID)
	require.Equal(t, 6, ins[0].Qty)
	require.Equal(t, a.MrpPaise-100, *ins[0].CostPaise)
	require.Equal(t, id, ins[0].DraftID)
	require.Equal(t, retailtest.Owner, ins[0].Who)
	require.Equal(t, b.ID, ins[1].ItemID)
	require.Equal(t, 3, ins[1].Qty, "the edited quantity, not the model's")
	require.Nil(t, ins[1].CostPaise, "an edit without a cost writes none")

	again, err := s.Confirm(e.Ctx, e.Scope, e.Owner, id, in)
	require.NoError(t, err)
	require.Equal(t, res.Lines, again.Lines)
	require.Len(t, st.calls(), 2, "a repeat writes nothing")
}

// TestConfirmRefuses (SC-AI-12): a line that does not fit (no match, a cost above the item's
// MRP, an expiry before today, an item the catalogue refuses) is refused alone with its
// reason; the good lines among it are written, the draft stays ready, and the next confirm
// writes only the corrected lines (the settled ones are never written twice).
func TestConfirmRefuses(t *testing.T) {
	e := retailtest.New(t)
	items := e.Items(t, 2)
	a, b := items[0], items[1]
	e.LLM.Reply = bill(a, b)
	over := int(a.MrpPaise + 1)
	yesterday := openapi_types.Date{Time: e.Clock.Now().Add(-48 * time.Hour)}
	accept := func(l string) aiapi.SupplierBillDecision {
		return aiapi.SupplierBillDecision{LineId: l, Outcome: aiapi.AiConfirmOutcomeAccepted}
	}
	edit := func(l string, v aiapi.SupplierBillLineValue) aiapi.SupplierBillDecision {
		return aiapi.SupplierBillDecision{LineId: l, Outcome: aiapi.AiConfirmOutcomeEdited, Value: &v}
	}
	for _, tc := range []struct {
		name   string
		l1     aiapi.SupplierBillDecision
		reason string
		gone   bool
	}{
		{"above mrp", edit("l1", aiapi.SupplierBillLineValue{StoreItemId: a.ID.String(), Quantity: 1, CostPaise: &over}), "invalid_cost_above_mrp", false},
		{"past expiry", edit("l1", aiapi.SupplierBillLineValue{StoreItemId: a.ID.String(), Quantity: 1, ExpiresOn: &yesterday}), "invalid_expiry", false},
		{"no quantity", edit("l1", aiapi.SupplierBillLineValue{StoreItemId: a.ID.String(), Quantity: 0}), "invalid_quantity", false},
		{"another store's item", edit("l1", aiapi.SupplierBillLineValue{StoreItemId: ids.New(e.Clock.Now()).String(), Quantity: 1}), "invalid_item", false},
		{"catalogue refuses", accept("l1"), "invalid_item", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &stock{}
			if tc.gone {
				st.gone = a.ID
			}
			s := newService(t, e, st)
			id, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
			require.NoError(t, err)

			// One bad line among good ones: l1 is refused, l2 is written, l3 (accepted with
			// no match) is refused.
			res, err := s.Confirm(e.Ctx, e.Scope, e.Owner, id, []aiapi.SupplierBillDecision{tc.l1, accept("l2"), accept("l3")})
			require.NoError(t, err)
			require.Equal(t, drafts.StatusReady, res.Status)
			require.Equal(t, []drafts.LineResult{
				{LineID: "l1", Outcome: drafts.NotPermitted, Reason: tc.reason},
				{LineID: "l2", Outcome: drafts.Accepted},
				{LineID: "l3", Outcome: drafts.NotPermitted, Reason: "invalid_item"},
			}, res.Lines)
			ins := st.calls()
			require.Len(t, ins, 1, "only the good line is written")
			require.Equal(t, b.ID, ins[0].ItemID)
			require.Equal(t, drafts.StatusReady, e.Draft(t, billscan.Feature, id).Status)

			// The person corrects l1 and drops l3; l2 is not written again.
			st.gone = ids.Zero
			res, err = s.Confirm(e.Ctx, e.Scope, e.Owner, id, []aiapi.SupplierBillDecision{
				edit("l1", aiapi.SupplierBillLineValue{StoreItemId: a.ID.String(), Quantity: 2}), accept("l2"),
				{LineId: "l3", Outcome: aiapi.AiConfirmOutcomeDiscarded},
			})
			require.NoError(t, err)
			require.Equal(t, drafts.StatusConfirmed, res.Status)
			require.Equal(t, []drafts.LineResult{
				{LineID: "l1", Outcome: drafts.Edited}, {LineID: "l2", Outcome: drafts.Accepted}, {LineID: "l3", Outcome: drafts.Discarded},
			}, res.Lines)
			ins = st.calls()
			require.Len(t, ins, 2)
			require.Equal(t, a.ID, ins[1].ItemID)
			require.Equal(t, 2, ins[1].Qty)
		})
	}
}

// TestGuardBalanced: every create reserves one call and settles its cost on every path (a
// read, a provider error, a schema reject, an empty bill), so the day's calls equal the
// creates and the cost booked equals what the model calls cost.
func TestGuardBalanced(t *testing.T) {
	e := retailtest.New(t)
	s := newService(t, e, &stock{})
	items := e.Items(t, 2)
	replies := []struct {
		reply any
		err   error
	}{
		{bill(items[0], items[1]), nil},
		{nil, errors.New("provider down")},
		{map[string]any{"lines": "not a list"}, nil},
		{billscan.ToolOutput{Lines: []billscan.ToolLine{}}, nil},
	}
	for _, r := range replies {
		e.LLM.Reply, e.LLM.Err = r.reply, r.err
		_, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
		require.NoError(t, err)
	}
	require.Equal(t, len(replies), e.LLM.Calls())
	require.Equal(t, len(replies), e.Count(t, `SELECT coalesce(sum(calls), 0)::int FROM ai_usage_daily WHERE scope = 'store'`))
	require.Equal(t, 0, e.Count(t, `SELECT count(*) FROM ai_drafts WHERE feature = $1 AND status = 'processing'`, billscan.Feature))
	require.Equal(t, 3, e.Count(t, `SELECT count(*) FROM ai_drafts WHERE feature = $1 AND status = 'failed'`, billscan.Feature))
	require.Equal(t,
		e.Count(t, `SELECT coalesce(sum(cost_paise), 0)::int FROM ai_drafts WHERE feature = $1`, billscan.Feature),
		e.Count(t, `SELECT coalesce(sum(cost_paise), 0)::int FROM ai_usage_daily WHERE scope = 'platform'`))
}

// TestScansInFlight: a store reads at most two photos at once (bill and shelf together); a
// third start is 429 rate_limited, takes nothing from the daily cap and calls no model, and
// a stale draft no longer counts.
func TestScansInFlight(t *testing.T) {
	e := retailtest.New(t)
	held := []func(){}
	e.Kit.Go = func(f func()) { held = append(held, f) }
	s := newService(t, e, &stock{})
	items := e.Items(t, 2)
	e.LLM.Reply = bill(items[0], items[1])
	for range retail.MaxScansInFlight {
		_, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
		require.NoError(t, err)
	}
	_, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	pe := retailtest.Problem(t, err)
	require.Equal(t, "rate_limited", pe.Code)
	require.NotEmpty(t, pe.Header.Get("Retry-After"))
	require.Equal(t, retail.MaxScansInFlight, e.Count(t, `SELECT coalesce(sum(calls), 0)::int FROM ai_usage_daily WHERE scope = 'store'`))

	// One finishes: the next start is taken.
	held[0]()
	_, err = s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	require.NoError(t, err)

	// Past StaleAfter the stuck ones no longer hold the store.
	e.Clock.Advance(retail.StaleAfter + time.Second)
	_, err = s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	require.NoError(t, err)
	require.Equal(t, 1, e.LLM.Calls(), "only the run that was let go called the model")
}

// TestHiddenWhenOff (SC-AI-24): with ai_billscan_enabled off every call is the common 404 and
// the model is never called.
func TestHiddenWhenOff(t *testing.T) {
	e := retailtest.New(t)
	s := newService(t, e, &stock{})
	items := e.Items(t, 2)
	e.LLM.Reply = bill(items[0], items[1])
	id, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	require.NoError(t, err)
	e.Set(t, settings.AiBillscanEnabled, "false")

	_, err = s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	require.Equal(t, "not_found", retailtest.Problem(t, err).Code)
	_, err = s.Get(e.Ctx, e.Scope, id)
	require.Equal(t, "not_found", retailtest.Problem(t, err).Code)
	_, err = s.Confirm(e.Ctx, e.Scope, e.Owner, id, []aiapi.SupplierBillDecision{{LineId: "l1", Outcome: aiapi.AiConfirmOutcomeAccepted}})
	require.Equal(t, "not_found", retailtest.Problem(t, err).Code)
	require.Equal(t, 1, e.LLM.Calls())
}

// TestStoreCap (SC-AI-25): past the store's daily AI calls the create is a 429 with no draft
// and no model call.
func TestStoreCap(t *testing.T) {
	e := retailtest.New(t)
	s := newService(t, e, &stock{})
	items := e.Items(t, 2)
	e.LLM.Reply = bill(items[0], items[1])
	e.Set(t, settings.AiDailyCallsStore, "1")
	_, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	require.NoError(t, err)

	_, err = s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	pe := retailtest.Problem(t, err)
	require.Equal(t, "ai_daily_cap_reached", pe.Code)
	require.NotEmpty(t, pe.Header.Get("Retry-After"))
	require.Equal(t, 1, e.LLM.Calls())
	require.Equal(t, 1, e.Count(t, `SELECT count(*) FROM ai_drafts WHERE feature = $1`, billscan.Feature))
}

// TestFailedRead: a model answer with no lines fails the draft (empty), and a failed draft
// cannot be confirmed.
func TestFailedRead(t *testing.T) {
	e := retailtest.New(t)
	st := &stock{}
	s := newService(t, e, st)
	e.LLM.Reply = billscan.ToolOutput{Lines: []billscan.ToolLine{}}
	id, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	require.NoError(t, err)
	v, err := s.Get(e.Ctx, e.Scope, id)
	require.NoError(t, err)
	require.Equal(t, aiapi.AiDraftStatus("failed"), v.Status)
	require.NotNil(t, v.Failure)
	_, err = s.Confirm(e.Ctx, e.Scope, e.Owner, id, []aiapi.SupplierBillDecision{{LineId: "l1", Outcome: aiapi.AiConfirmOutcomeAccepted}})
	require.Error(t, err)
	require.Empty(t, st.calls())
}

// TestTiedMatchIsLeftUnmatched (W3-11 review): when two of the store's items fit a line
// equally well the server does not pick one; the line has no match and the person chooses.
func TestTiedMatchIsLeftUnmatched(t *testing.T) {
	e := retailtest.New(t)
	s := newService(t, e, &stock{})
	items := e.Items(t, 2)
	a, b := items[0], items[1]
	_, err := e.TDB.OwnerPool.Exec(e.Ctx, `UPDATE store_items SET local_name = $1 WHERE id = $2`, a.Name, ids.UUID(b.ID))
	require.NoError(t, err)
	e.LLM.Reply = billscan.ToolOutput{Lines: []billscan.ToolLine{{Name: a.Name, Quantity: 1, Confidence: 0.9}}}
	id, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	require.NoError(t, err)
	v, err := s.Get(e.Ctx, e.Scope, id)
	require.NoError(t, err)
	require.Len(t, v.Lines, 1)
	require.Nil(t, v.Lines[0].Match, "a tie is not guessed")
}

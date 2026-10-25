package billscan_test

import (
	"context"
	"sync"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/require"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/ai/billscan"
	"github.com/todayz/backend/internal/ai/drafts"
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
}

func (s *stock) AddFromBill(_ context.Context, in billscan.StockIn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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

// TestConfirmRefuses (SC-AI-12): an accepted line with no match, a cost above the item's MRP
// and an expiry before today are 422s that write nothing and leave the draft ready.
func TestConfirmRefuses(t *testing.T) {
	e := retailtest.New(t)
	st := &stock{}
	s := newService(t, e, st)
	items := e.Items(t, 2)
	a, b := items[0], items[1]
	e.LLM.Reply = bill(a, b)
	id, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, retailtest.Photo())
	require.NoError(t, err)

	over := int(a.MrpPaise + 1)
	yesterday := openapi_types.Date{Time: e.Clock.Now().Add(-48 * time.Hour)}
	discard := aiapi.SupplierBillDecision{LineId: "l2", Outcome: aiapi.AiConfirmOutcomeDiscarded}
	for _, tc := range []struct {
		name   string
		l1, l3 aiapi.SupplierBillDecision
		field  string
		reason string
	}{
		{"unmatched accepted", aiapi.SupplierBillDecision{LineId: "l1", Outcome: aiapi.AiConfirmOutcomeDiscarded},
			aiapi.SupplierBillDecision{LineId: "l3", Outcome: aiapi.AiConfirmOutcomeAccepted},
			"body.lines[2].value.storeItemId", "required"},
		{"above mrp", aiapi.SupplierBillDecision{LineId: "l1", Outcome: aiapi.AiConfirmOutcomeEdited,
			Value: &aiapi.SupplierBillLineValue{StoreItemId: a.ID.String(), Quantity: 1, CostPaise: &over}},
			aiapi.SupplierBillDecision{LineId: "l3", Outcome: aiapi.AiConfirmOutcomeDiscarded},
			"body.lines[0].value.costPaise", "above_mrp"},
		{"past expiry", aiapi.SupplierBillDecision{LineId: "l1", Outcome: aiapi.AiConfirmOutcomeEdited,
			Value: &aiapi.SupplierBillLineValue{StoreItemId: a.ID.String(), Quantity: 1, ExpiresOn: &yesterday}},
			aiapi.SupplierBillDecision{LineId: "l3", Outcome: aiapi.AiConfirmOutcomeDiscarded},
			"body.lines[0].value.expiresOn", "past"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Confirm(e.Ctx, e.Scope, e.Owner, id, []aiapi.SupplierBillDecision{tc.l1, discard, tc.l3})
			pe := retailtest.Problem(t, err)
			require.Equal(t, tc.reason, pe.Fields[tc.field], pe.Fields)
			require.Empty(t, st.calls())
			require.Equal(t, drafts.StatusReady, e.Draft(t, billscan.Feature, id).Status)
		})
	}
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

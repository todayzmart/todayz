package bannercopy_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/ai/bannercopy"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/retail/retailtest"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func newService(t *testing.T, e *retailtest.Env) *bannercopy.Service {
	t.Helper()
	e.Set(t, settings.AiBannercopyEnabled, "true")
	s, err := bannercopy.New(bannercopy.Deps{DB: e.DB, Clock: e.Clock, Settings: e.Settings, LLM: e.LLM, Kit: e.Kit})
	require.NoError(t, err)
	return s
}

func one(n int) *int { return &n }

// TestBannerCopyWritesNoBanner (SC-AI-22): the options are a draft, an option with a number
// the facts do not hold is dropped, and the confirm answers the picked copy without writing
// a banner (the person saves the banner in the banner editor).
func TestBannerCopyWritesNoBanner(t *testing.T) {
	e := retailtest.New(t)
	s := newService(t, e)
	items := e.Items(t, 2)
	e.LLM.Reply = bannercopy.ToolOutput{Options: []bannercopy.ToolOption{
		{Headline: "Fresh stock is in", Subline: "Pick up today", HeadlineEn: "Fresh stock is in", SublineEn: "Pick up today", Tint: "green", Item: one(1)},
		// D-230: item 2 of the list is not a discount (only a percent fact grounds "N%").
		{Headline: "2% off everything", Subline: "", HeadlineEn: "2% off everything", SublineEn: "", Tint: "rose"},
		{Headline: "Your daily needs", Subline: "Near you", HeadlineEn: "Your daily needs", SublineEn: "Near you", Tint: "sky"},
	}}
	banners := func() int {
		return e.Count(t, `SELECT count(*) FROM store_banners WHERE store_id = $1`, ids.UUID(retailtest.Store))
	}
	before := banners()
	itemIDs := []string{items[0].ID.String(), items[1].ID.String()}

	id, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, aiapi.BannerCopyDraftRequest{ItemIds: &itemIDs})
	require.NoError(t, err)
	v, err := s.Get(e.Ctx, e.Scope, id)
	require.NoError(t, err)
	require.Equal(t, aiapi.AiDraftStatus("ready"), v.Status)
	require.Len(t, v.Lines, 2, "the option with an ungrounded number is dropped")
	require.Equal(t, "Fresh stock is in", v.Lines[0].Headline)
	require.NotNil(t, v.Lines[0].StoreItemId)
	require.Equal(t, items[0].ID.String(), *v.Lines[0].StoreItemId)

	_, err = s.Confirm(e.Ctx, e.Scope, e.Owner, id, []aiapi.BannerCopyDecision{
		{LineId: "l1", Outcome: aiapi.AiConfirmOutcomeAccepted}, {LineId: "l2", Outcome: aiapi.AiConfirmOutcomeAccepted}})
	require.Equal(t, "one_pick", retailtest.Problem(t, err).Fields["body.lines[1].outcome"])

	res, err := s.Confirm(e.Ctx, e.Scope, e.Owner, id, []aiapi.BannerCopyDecision{
		{LineId: "l1", Outcome: aiapi.AiConfirmOutcomeDiscarded},
		{LineId: "l2", Outcome: aiapi.AiConfirmOutcomeEdited, Value: &aiapi.BannerCopyValue{Headline: " Daily needs, nearby ", Subline: "", Tint: "amber"}},
	})
	require.NoError(t, err)
	require.Equal(t, aiapi.BannerCopyConfirmResultStatus(drafts.StatusConfirmed), res.Status)
	require.NotNil(t, res.Copy)
	require.Equal(t, "Daily needs, nearby", res.Copy.Headline)
	require.Equal(t, aiapi.BannerTint("amber"), res.Copy.Tint)
	require.Equal(t, before, banners(), "a confirm never writes a banner")
}

// TestBannerCopyRefuses: an item of another store is a field error before any model call; a
// draft whose every option is ungrounded fails (grounding).
func TestBannerCopyRefuses(t *testing.T) {
	e := retailtest.New(t)
	s := newService(t, e)
	stranger := []string{ids.New(e.Clock.Now()).String()}
	_, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, aiapi.BannerCopyDraftRequest{ItemIds: &stranger})
	require.Equal(t, "not_in_store", retailtest.Problem(t, err).Fields["body.itemIds"])
	require.Zero(t, e.LLM.Calls())

	items := e.Items(t, 1)
	mine := []string{items[0].ID.String()}
	e.LLM.Reply = bannercopy.ToolOutput{Options: []bannercopy.ToolOption{
		{Headline: "Only ₹4321 today", HeadlineEn: "Only ₹4321 today", Tint: "green"},
	}}
	id, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, aiapi.BannerCopyDraftRequest{ItemIds: &mine})
	require.NoError(t, err)
	v, err := s.Get(e.Ctx, e.Scope, id)
	require.NoError(t, err)
	require.Equal(t, aiapi.AiDraftStatus("failed"), v.Status)
	require.NotNil(t, v.Failure)
}

package replydraft_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	aiapi "github.com/todayz/backend/gen/api/ai"
	reviewsapi "github.com/todayz/backend/gen/api/reviews"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/replydraft"
	"github.com/todayz/backend/internal/ai/retail/retailtest"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

type posted struct {
	review ids.ID
	text   string
}

// replies records the replies a confirm asks the reviews domain to post.
type replies struct {
	mu   sync.Mutex
	sent []posted
}

func (f *replies) Reply(_ context.Context, _, _, reviewID ids.ID, text string) (reviewsapi.ReviewReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, posted{reviewID, text})
	return reviewsapi.ReviewReply{}, nil
}

func (f *replies) calls() []posted {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]posted(nil), f.sent...)
}

// review is a fresh four-star review of the store for a counter sale (the dev seed has none).
func review(t *testing.T, e *retailtest.Env) ids.ID {
	t.Helper()
	customer := e.TDB.InsertUser(t, ids.Zero)
	sale := e.TDB.InsertPosSale(t, ids.Zero, retailtest.Store, retailtest.Owner)
	id := ids.New(e.Clock.Now())
	_, err := e.TDB.OwnerPool.Exec(e.Ctx, `INSERT INTO reviews (id, store_id, customer_id, pos_sale_id, stars, text, tags, edited_until)
		VALUES ($1, $2, $3, $4, 4, 'Good stock, quick billing.', '{fresh_stock,quick_service}', now() + interval '1 day')`,
		ids.UUID(id), ids.UUID(retailtest.Store), ids.UUID(customer), ids.UUID(sale))
	require.NoError(t, err)
	return id
}

func newService(t *testing.T, e *retailtest.Env, r replydraft.Replies) *replydraft.Service {
	t.Helper()
	e.Set(t, settings.AiReplydraftEnabled, "true")
	s, err := replydraft.New(replydraft.Deps{DB: e.DB, Clock: e.Clock, Settings: e.Settings, LLM: e.LLM, Kit: e.Kit, Replies: r})
	require.NoError(t, err)
	return s
}

// TestReplyPostsOnlyOnConfirm (SC-AI-20): the drafted reply posts nothing until a person
// confirms; an edited reply posts the person's words, a discarded draft posts nothing.
func TestReplyPostsOnlyOnConfirm(t *testing.T) {
	e := retailtest.New(t)
	f := &replies{}
	s := newService(t, e, f)
	rv := review(t, e)
	e.LLM.Reply = replydraft.ToolOutput{Text: "Thank you for shopping with us. We will keep the shelves stocked."}

	id, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, rv)
	require.NoError(t, err)
	v, err := s.Get(e.Ctx, e.Scope, id)
	require.NoError(t, err)
	require.Equal(t, aiapi.AiDraftStatus("ready"), v.Status)
	require.Equal(t, rv.String(), v.ReviewId)
	require.Len(t, v.Lines, 1)
	require.Empty(t, f.calls(), "nothing posted before the confirm")

	res, err := s.Confirm(e.Ctx, e.Scope, e.Owner, id, []aiapi.ReviewReplyDecision{{LineId: "l1",
		Outcome: aiapi.AiConfirmOutcomeEdited, Value: &aiapi.ReviewReplyLineValue{Text: "  Thanks, see you soon!  "}}})
	require.NoError(t, err)
	require.Equal(t, drafts.StatusConfirmed, res.Status)
	require.Equal(t, []posted{{rv, "Thanks, see you soon!"}}, f.calls())

	other, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, rv)
	require.NoError(t, err)
	res, err = s.Confirm(e.Ctx, e.Scope, e.Owner, other, []aiapi.ReviewReplyDecision{{LineId: "l1", Outcome: aiapi.AiConfirmOutcomeDiscarded}})
	require.NoError(t, err)
	require.Equal(t, drafts.StatusDiscarded, res.Status)
	require.Len(t, f.calls(), 1, "a discarded draft posts nothing")
}

// TestReplyRefuses: a review of another store is a field error before any model call, and an
// empty edited reply is a 422 that posts nothing.
func TestReplyRefuses(t *testing.T) {
	e := retailtest.New(t)
	f := &replies{}
	s := newService(t, e, f)
	e.LLM.Reply = replydraft.ToolOutput{Text: "Thank you."}

	_, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, ids.New(e.Clock.Now()))
	require.Equal(t, "no_review", retailtest.Problem(t, err).Fields["body.reviewId"])
	require.Zero(t, e.LLM.Calls())

	id, err := s.Create(e.Ctx, e.Scope, retailtest.Owner, review(t, e))
	require.NoError(t, err)
	_, err = s.Confirm(e.Ctx, e.Scope, e.Owner, id, []aiapi.ReviewReplyDecision{{LineId: "l1",
		Outcome: aiapi.AiConfirmOutcomeEdited, Value: &aiapi.ReviewReplyLineValue{Text: "   "}}})
	require.Equal(t, "length", retailtest.Problem(t, err).Fields["body.lines[0].value.text"])
	require.Empty(t, f.calls())
	require.Equal(t, drafts.StatusReady, e.Draft(t, replydraft.Feature, id).Status)
}

package replydraft

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"unicode/utf8"

	aiapi "github.com/todayz/backend/gen/api/ai"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/airetaildb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

type inputMeta struct {
	Stars    int    `json:"stars"`
	Language string `json:"language"`
}

// Create checks the review (this store's, visible, without a reply) and drafts a reply in
// the background.
func (s *Service) Create(ctx context.Context, sc authz.Scope, actor, reviewID ids.ID) (ids.ID, error) {
	if err := s.d.Kit.Guard.Require(ctx, Feature); err != nil {
		return ids.Zero, err
	}
	q := airetaildb.New(s.d.DB.Q(ctx))
	rv, err := q.ReviewForReply(ctx, airetaildb.ReviewForReplyParams{ID: ids.UUID(reviewID), StoreID: ids.UUID(sc.StoreID)})
	if db.IsNoRows(err) {
		return ids.Zero, retail.FieldErr("body.reviewId", "no_review")
	}
	if err != nil {
		return ids.Zero, err
	}
	if rv.Replied {
		return ids.Zero, problem.New(genproblems.ReviewReplyExists, "")
	}
	lang := ""
	if rv.Text != nil {
		lang = LangOf(*rv.Text)
	}
	if lang == "" {
		if lang, err = q.StoreLang(ctx, ids.UUID(sc.StoreID)); err != nil {
			return ids.Zero, err
		}
	}
	return s.d.Kit.Start(ctx, retail.New{
		Feature: Feature, Store: sc.StoreID, Actor: actor, InputRef: reviewID.String(),
		PromptVersion: prompts.Version(Feature), Meta: inputMeta{Stars: int(rv.Stars), Language: lang},
	}, func(ctx context.Context, id ids.ID) { s.process(ctx, id, sc.StoreID, reviewID, lang) })
}

func (s *Service) process(ctx context.Context, id, storeID, reviewID ids.ID, lang string) {
	log := telemetry.Logger(ctx)
	fail := func(what string, err error) {
		log.ErrorContext(ctx, "replydraft: "+what, slog.String("draft_id", id.String()), slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, client.Response{})
	}
	model, err := s.d.Settings.String(ctx, settings.AiModelFast)
	if err != nil {
		fail("model setting", err)
		return
	}
	q := airetaildb.New(s.d.DB.Q(ctx))
	rv, err := q.ReviewForReply(ctx, airetaildb.ReviewForReplyParams{ID: ids.UUID(reviewID), StoreID: ids.UUID(storeID)})
	if err != nil {
		fail("review", err)
		return
	}
	shop, err := q.StoreName(ctx, ids.UUID(storeID))
	if err != nil {
		fail("store name", err)
		return
	}
	r, err := Write(ctx, s.d.LLM, model, id.String(), NewReview(int(rv.Stars), rv.Tags, rv.Text, lang, shop))
	s.d.Kit.Settle(ctx, Feature, r.Resp)
	switch {
	case err != nil:
		log.ErrorContext(ctx, "replydraft: model call", slog.String("draft_id", id.String()), slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, r.Resp)
	case r.Reject != "":
		s.d.Kit.Fail(ctx, Feature, id, r.Reject, r.Resp)
	default:
		s.d.Kit.Ready(ctx, Feature, id, r.Output, r.Resp)
	}
}

// Get answers one draft of the caller's store.
func (s *Service) Get(ctx context.Context, sc authz.Scope, id ids.ID) (aiapi.ReviewReplyDraft, error) {
	d, err := s.d.Kit.Load(ctx, Feature, sc.StoreID, id)
	if err != nil {
		return aiapi.ReviewReplyDraft{}, err
	}
	v := aiapi.ReviewReplyDraft{
		DraftId: d.ID.String(), Feature: aiapi.ReviewReplyDraftFeature(Feature), Status: aiapi.AiDraftStatus(d.Status),
		PromptVersion: d.PromptVersion, CreatedAt: d.CreatedAt.UTC(), Failure: retail.Failure(d), ReviewId: d.InputRef,
		Lines: []aiapi.ReviewReplyLine{}, Outcomes: retail.Outcomes(d.Lines),
	}
	if len(d.Output) == 0 {
		return v, nil
	}
	var out Output
	if err := d.Decode(&out); err != nil {
		return aiapi.ReviewReplyDraft{}, err
	}
	for _, l := range out.Lines {
		v.Lines = append(v.Lines, aiapi.ReviewReplyLine{LineId: l.LineID, Text: l.Text, Language: aiapi.AiLanguage(l.Language)})
	}
	return v, nil
}

// Confirm posts the accepted or edited reply; discarded posts nothing.
func (s *Service) Confirm(ctx context.Context, sc authz.Scope, p authn.Principal, draftID ids.ID, in []aiapi.ReviewReplyDecision) (drafts.Result, error) {
	if err := s.d.Kit.Guard.Require(ctx, Feature); err != nil {
		return drafts.Result{}, err
	}
	ds := make([]drafts.Decision, 0, len(in))
	for _, dc := range in {
		var v any
		if dc.Value != nil {
			v = dc.Value
		}
		d, err := retail.Decision(dc.LineId, dc.Outcome, v)
		if err != nil {
			return drafts.Result{}, err
		}
		ds = append(ds, d)
	}
	return s.d.Kit.Drafts.Confirm(ctx, drafts.Confirmation{
		Feature: Feature, DraftID: draftID, By: p.UserID, Decisions: ds, Check: retail.OwnStore(sc.StoreID),
		Apply: func(ctx context.Context, d drafts.Draft, ds []drafts.Decision) ([]drafts.LineResult, error) {
			return s.apply(ctx, sc, p, d, ds)
		},
	})
}

func (s *Service) apply(ctx context.Context, sc authz.Scope, p authn.Principal, d drafts.Draft, ds []drafts.Decision) ([]drafts.LineResult, error) {
	if len(ds) == 0 {
		return nil, nil
	}
	var out Output
	if err := d.Decode(&out); err != nil || len(out.Lines) != 1 {
		return nil, retail.NotFound()
	}
	reviewID, err := ids.Parse(d.InputRef)
	if err != nil {
		return nil, retail.NotFound()
	}
	dc := ds[0] // one line, so at most one decision
	text := out.Lines[0].Text
	if dc.Outcome == drafts.Edited {
		var v aiapi.ReviewReplyLineValue
		if err := json.Unmarshal(dc.Value, &v); err != nil {
			return nil, retail.FieldErr("body.lines[0].value", "invalid")
		}
		text = strings.TrimSpace(v.Text)
		if text == "" || utf8.RuneCountInString(text) > MaxEdited {
			return nil, retail.FieldErr("body.lines[0].value.text", "length")
		}
	}
	if _, err := s.d.Replies.Reply(ctx, p.UserID, sc.StoreID, reviewID, text); err != nil {
		return nil, err
	}
	return []drafts.LineResult{{LineID: dc.LineID, Outcome: dc.Outcome}}, nil
}

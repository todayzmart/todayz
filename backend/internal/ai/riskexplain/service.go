package riskexplain

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"unicode/utf8"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/gen/sqlc/aiadmindb"
	"github.com/todayz/backend/internal/ai/assist"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

type inputMeta struct {
	Rule  string `json:"rule"`
	Score int    `json:"score"`
}

// Create checks the flag and drafts its summary in the background.
func (s *Service) Create(ctx context.Context, p authn.Principal, flagID ids.ID) (ids.ID, error) {
	if err := s.d.Kit.Guard.Require(ctx, Feature); err != nil {
		return ids.Zero, err
	}
	f, err := aiadmindb.New(s.d.DB.Q(ctx)).FlagForSummary(ctx, ids.UUID(flagID))
	if db.IsNoRows(err) {
		return ids.Zero, retail.FieldErr("body.flagId", "no_flag")
	}
	if err != nil {
		return ids.Zero, err
	}
	return s.d.Kit.Start(ctx, retail.New{
		Feature: Feature, Store: ids.Zero, Actor: p.UserID, InputRef: flagID.String(),
		PromptVersion: prompts.Version(Feature), Meta: inputMeta{Rule: f.Rule, Score: int(f.Score)},
	}, func(ctx context.Context, id ids.ID) { s.process(ctx, id, flagID) })
}

// flag reads the model's view of one flag.
func (s *Service) flag(ctx context.Context, flagID ids.ID) (Flag, error) {
	q := aiadmindb.New(s.d.DB.Q(ctx))
	f, err := q.FlagForSummary(ctx, ids.UUID(flagID))
	if err != nil {
		return Flag{}, err
	}
	h, err := q.SubjectFlagCounts(ctx, aiadmindb.SubjectFlagCountsParams{SubjectType: f.SubjectType, SubjectID: f.SubjectID, ID: f.ID})
	if err != nil {
		return Flag{}, err
	}
	var facts []Fact
	if s.d.Facts != nil {
		facts = s.d.Facts(f.Details)
	}
	return NewFlag(f.Rule, f.SubjectType, int(f.Score), f.Status, facts,
		History{Open: int(h.Open), Confirmed: int(h.Confirmed), Cleared: int(h.Cleared)}), nil
}

func (s *Service) process(ctx context.Context, id, flagID ids.ID) {
	log := telemetry.Logger(ctx)
	fail := func(what string, err error) {
		log.ErrorContext(ctx, "riskexplain: "+what, slog.String("draft_id", id.String()), slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, client.Response{})
	}
	model, err := s.d.Settings.String(ctx, settings.AiModelFast)
	if err != nil {
		fail("model setting", err)
		return
	}
	f, err := s.flag(ctx, flagID)
	if err != nil {
		fail("flag", err)
		return
	}
	r, err := Write(ctx, s.d.LLM, model, id.String(), f)
	s.d.Kit.Settle(ctx, Feature, r.Resp)
	switch {
	case err != nil:
		log.ErrorContext(ctx, "riskexplain: model call", slog.String("draft_id", id.String()), slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, r.Resp)
	case r.Reject != "":
		s.d.Kit.Fail(ctx, Feature, id, r.Reject, r.Resp)
	default:
		s.d.Kit.Ready(ctx, Feature, id, r.Output, r.Resp)
	}
}

// Get answers one of the caller's own drafts.
func (s *Service) Get(ctx context.Context, p authn.Principal, id ids.ID) (aiapi.RiskSummaryDraft, error) {
	d, err := assist.Load(ctx, s.d.Kit, Feature, p.UserID, id)
	if err != nil {
		return aiapi.RiskSummaryDraft{}, err
	}
	v := aiapi.RiskSummaryDraft{
		DraftId: d.ID.String(), Feature: aiapi.RiskSummaryDraftFeature(Feature), Status: aiapi.AiDraftStatus(d.Status),
		PromptVersion: d.PromptVersion, CreatedAt: d.CreatedAt.UTC(), Failure: retail.Failure(d), FlagId: d.InputRef,
		Lines: []aiapi.RiskSummaryLine{}, Outcomes: retail.Outcomes(d.Lines),
	}
	if len(d.Output) == 0 {
		return v, nil
	}
	var out Output
	if err := d.Decode(&out); err != nil {
		return aiapi.RiskSummaryDraft{}, err
	}
	for _, l := range out.Lines {
		v.Lines = append(v.Lines, aiapi.RiskSummaryLine{LineId: l.LineID, Text: l.Text})
	}
	return v, nil
}

// Confirm attaches the accepted or edited summary to the flag in the audit log; discarded
// attaches nothing.
func (s *Service) Confirm(ctx context.Context, p authn.Principal, draftID ids.ID, in []aiapi.RiskSummaryDecision) (drafts.Result, error) {
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
		Feature: Feature, DraftID: draftID, By: p.UserID, Decisions: ds, Check: assist.Own(p.UserID),
		Apply: func(ctx context.Context, d drafts.Draft, ds []drafts.Decision) ([]drafts.LineResult, error) {
			return s.apply(ctx, p, d, ds)
		},
	})
}

func (s *Service) apply(ctx context.Context, p authn.Principal, d drafts.Draft, ds []drafts.Decision) ([]drafts.LineResult, error) {
	if len(ds) == 0 {
		return nil, nil
	}
	var out Output
	if err := d.Decode(&out); err != nil || len(out.Lines) != 1 {
		return nil, retail.NotFound()
	}
	flagID, err := ids.Parse(d.InputRef)
	if err != nil {
		return nil, retail.NotFound()
	}
	f, err := s.flag(ctx, flagID)
	if db.IsNoRows(err) {
		return nil, retail.NotFound()
	}
	if err != nil {
		return nil, err
	}
	dc := ds[0] // one line, so at most one decision
	text := out.Lines[0].Text
	if dc.Outcome == drafts.Edited {
		var v aiapi.RiskSummaryLineValue
		if err := json.Unmarshal(dc.Value, &v); err != nil {
			return nil, retail.FieldErr("body.lines[0].value", "invalid")
		}
		text = strings.TrimSpace(v.Text)
		if text == "" || utf8.RuneCountInString(text) > MaxSummary {
			return nil, retail.FieldErr("body.lines[0].value.text", "length")
		}
		if !f.Grounded(text) {
			return nil, retail.FieldErr("body.lines[0].value.text", "ungrounded")
		}
	}
	if _, err := s.d.Audit.Write(ctx, audit.Entry{
		ActorKind: audit.ActorAdmin, ActorID: p.UserID, Action: "risk.flag_summary",
		SubjectType: "risk_flag", SubjectID: flagID.String(),
		After: map[string]any{"draftId": d.ID.String(), "outcome": string(dc.Outcome), "text": text},
	}); err != nil {
		return nil, err
	}
	return []drafts.LineResult{{LineID: dc.LineID, Outcome: dc.Outcome}}, nil
}

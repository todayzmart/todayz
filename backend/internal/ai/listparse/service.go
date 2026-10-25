package listparse

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf8"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// inputMeta is all a draft keeps of the list: its length, never its words.
type inputMeta struct {
	Chars int `json:"chars"`
}

// Create starts a draft of the customer's list. The list is scrubbed, handed to the
// background parse and never stored.
func (s *Service) Create(ctx context.Context, customer ids.ID, text string) (ids.ID, error) {
	if err := s.d.Guard.Require(ctx, Feature); err != nil {
		return ids.Zero, err
	}
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n == 0 || n > MaxInput {
		return ids.Zero, retail.FieldErr("body.text", "length")
	}
	list := Clean(text)
	return s.start(ctx, customer, inputMeta{Chars: utf8.RuneCountInString(text)}, func(ctx context.Context, id ids.ID) {
		s.process(ctx, id, list)
	})
}

func (s *Service) process(ctx context.Context, id ids.ID, list string) {
	log := telemetry.Logger(ctx)
	model, err := s.d.Settings.String(ctx, settings.AiModelFast)
	if err != nil {
		log.ErrorContext(ctx, "listparse: model setting", slog.String("draft_id", id.String()), slog.Any("err", err))
		s.fail(ctx, id, client.OutcomeError, client.Response{})
		return
	}
	maxLines, err := s.d.Settings.Int(ctx, settings.BasketMaxItems)
	if err != nil {
		log.ErrorContext(ctx, "listparse: basket_max_items", slog.String("draft_id", id.String()), slog.Any("err", err))
		s.fail(ctx, id, client.OutcomeError, client.Response{})
		return
	}
	d, err := Parse(ctx, s.d.LLM, model, id.String(), list, int(min(maxLines, MaxLines)))
	s.settle(ctx, d.Resp)
	switch {
	case err != nil:
		// The error names the provider's status only; the list is never logged.
		log.ErrorContext(ctx, "listparse: model call", slog.String("draft_id", id.String()), slog.Any("err", err))
		s.fail(ctx, id, client.OutcomeError, d.Resp)
	case d.Reject != "":
		s.fail(ctx, id, d.Reject, d.Resp)
	default:
		s.ready(ctx, id, d.Output, d.Resp)
	}
}

// Get answers one of the customer's drafts.
func (s *Service) Get(ctx context.Context, customer, id ids.ID) (aiapi.ListParseDraft, error) {
	d, err := s.load(ctx, customer, id)
	if err != nil {
		return aiapi.ListParseDraft{}, err
	}
	v := aiapi.ListParseDraft{
		DraftId: d.ID.String(), Feature: aiapi.ListParseDraftFeature(Feature), Status: aiapi.AiDraftStatus(d.Status),
		PromptVersion: d.PromptVersion, CreatedAt: d.CreatedAt.UTC(), Failure: retail.Failure(d),
		Lines: []aiapi.ListParseLine{}, Outcomes: retail.Outcomes(d.Lines),
	}
	if len(d.Output) == 0 {
		return v, nil
	}
	var out Output
	if err := d.Decode(&out); err != nil {
		return aiapi.ListParseDraft{}, err
	}
	for _, l := range out.Lines {
		v.Lines = append(v.Lines, aiapi.ListParseLine{LineId: l.LineID, Text: l.Text, Quantity: l.Quantity})
	}
	return v, nil
}

// Confirm closes the draft with the customer's choices and answers the lines to add to the
// basket editor: the accepted lines as drafted, the edited ones as the customer wrote them.
// Nothing is saved but the draft's outcomes. An edited value is not stored, so a repeat
// answers the request's value for an edited line (the drafted one when the repeat has none).
func (s *Service) Confirm(ctx context.Context, customer, draftID ids.ID, in []aiapi.ListParseDecision) (aiapi.ListParseConfirmResult, error) {
	if err := s.d.Guard.Require(ctx, Feature); err != nil {
		return aiapi.ListParseConfirmResult{}, err
	}
	edited := map[string]aiapi.ListParseLineValue{}
	ds := make([]drafts.Decision, 0, len(in))
	for i, dc := range in {
		var v any
		if dc.Value != nil {
			if dc.Outcome != aiapi.AiConfirmOutcomeEdited {
				return aiapi.ListParseConfirmResult{}, retail.FieldErr(field(i, ""), "only_when_edited")
			}
			val, err := cleanValue(i, *dc.Value)
			if err != nil {
				return aiapi.ListParseConfirmResult{}, err
			}
			edited[dc.LineId], v = val, val
		} else if dc.Outcome == aiapi.AiConfirmOutcomeEdited {
			return aiapi.ListParseConfirmResult{}, retail.FieldErr(field(i, ""), "required")
		}
		d, err := retail.Decision(dc.LineId, dc.Outcome, v)
		if err != nil {
			return aiapi.ListParseConfirmResult{}, err
		}
		ds = append(ds, d)
	}
	var drafted Output
	res, err := s.d.Drafts.Confirm(ctx, drafts.Confirmation{
		Feature: Feature, DraftID: draftID, By: customer, Decisions: ds,
		Check: func(d drafts.Draft) error {
			if err := own(customer)(d); err != nil {
				return err
			}
			if len(d.Output) > 0 {
				return d.Decode(&drafted)
			}
			return nil
		},
		// Applying writes nothing: the lines go back to the app, which adds them to the
		// basket editor (BSK-15).
		Apply: func(_ context.Context, _ drafts.Draft, ds []drafts.Decision) ([]drafts.LineResult, error) {
			out := make([]drafts.LineResult, 0, len(ds))
			for _, dc := range ds {
				out = append(out, drafts.LineResult{LineID: dc.LineID, Outcome: dc.Outcome})
			}
			return out, nil
		},
	})
	if err != nil {
		return aiapi.ListParseConfirmResult{}, err
	}
	byID := make(map[string]Line, len(drafted.Lines))
	for _, l := range drafted.Lines {
		byID[l.LineID] = l
	}
	r := aiapi.ListParseConfirmResult{
		DraftId: res.DraftID.String(), Status: aiapi.ListParseConfirmResultStatus(res.Status),
		Lines: retail.Outcomes(res.Lines), BasketLines: []aiapi.ListParseLine{},
	}
	for _, l := range res.Lines {
		if l.Outcome != drafts.Accepted && l.Outcome != drafts.Edited {
			continue
		}
		line, ok := byID[l.LineID]
		if !ok {
			continue
		}
		bl := aiapi.ListParseLine{LineId: l.LineID, Text: line.Text, Quantity: line.Quantity}
		if v, ok := edited[l.LineID]; ok && l.Outcome == drafts.Edited {
			bl.Text, bl.Quantity = v.Text, v.Quantity
		}
		r.BasketLines = append(r.BasketLines, bl)
	}
	return r, nil
}

func field(i int, name string) string {
	f := "body.lines[" + itoa(i) + "].value"
	if name != "" {
		f += "." + name
	}
	return f
}

func itoa(i int) string { return strconv.Itoa(i) }

// cleanValue checks an edited line: text of 1 to MaxText characters (spaces collapsed), a
// quantity of 1 to MaxQuantity.
func cleanValue(i int, v aiapi.ListParseLineValue) (aiapi.ListParseLineValue, error) {
	v.Text = strings.Join(strings.Fields(v.Text), " ")
	if n := utf8.RuneCountInString(v.Text); n == 0 || n > MaxText {
		return v, retail.FieldErr(field(i, "text"), "length")
	}
	if v.Quantity < 1 || v.Quantity > MaxQuantity {
		return v, retail.FieldErr(field(i, "quantity"), "range")
	}
	return v, nil
}

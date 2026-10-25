package voice

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	voiceapi "github.com/todayz/backend/gen/api/voice"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Get answers one draft for its speaker or the store's owner and managers. A draft still
// processing StaleAfter after it was made is closed as failed (timeout) first: the
// background work that had it is gone (a restart, a crash).
func (s *Service) Get(ctx context.Context, sc authz.Scope, p authn.Principal, id ids.ID) (voiceapi.VoiceStockDraft, error) {
	if err := s.d.Guard.Require(ctx, Feature); err != nil {
		return voiceapi.VoiceStockDraft{}, err
	}
	d, err := s.d.Drafts.Get(ctx, Feature, id)
	if err != nil {
		return voiceapi.VoiceStockDraft{}, err
	}
	if err := s.owns(ctx, p, sc, d); err != nil {
		return voiceapi.VoiceStockDraft{}, err
	}
	if d.Status == drafts.StatusProcessing && s.d.Clock.Now().Sub(d.CreatedAt) > StaleAfter {
		if _, err := s.d.Drafts.MarkFailed(ctx, id, client.OutcomeTimeout, client.Response{}); err != nil {
			telemetry.Logger(ctx).ErrorContext(ctx, "voice: close stale draft", slog.String("draft_id", id.String()), slog.Any("err", err))
		} else if d, err = s.d.Drafts.Get(ctx, Feature, id); err != nil {
			return voiceapi.VoiceStockDraft{}, err
		}
	}
	return s.view(ctx, sc, p, d)
}

// view is the draft as the caller sees it: the stored lines with each item's price and
// stock now, and the lines the caller may not confirm marked ask_manager.
func (s *Service) view(ctx context.Context, sc authz.Scope, p authn.Principal, d drafts.Draft) (voiceapi.VoiceStockDraft, error) {
	v := voiceapi.VoiceStockDraft{
		DraftId: d.ID.String(), Feature: voiceapi.VoiceStock, Status: voiceapi.VoiceStockDraftStatus(d.Status),
		PromptVersion: d.PromptVersion, CreatedAt: d.CreatedAt.UTC(),
		Lines: []voiceapi.VoiceStockLine{}, Outcomes: []voiceapi.VoiceStockLineResult{},
	}
	if d.Failure != "" {
		f := d.Failure
		v.Failure = &f
	}
	for _, l := range d.Lines {
		v.Outcomes = append(v.Outcomes, voiceapi.VoiceStockLineResult{LineId: l.LineID, Outcome: voiceapi.VoiceStockLineResultOutcome(l.Outcome)})
	}
	if len(d.Output) == 0 {
		return v, nil
	}
	var out Output
	if err := d.Decode(&out); err != nil {
		return voiceapi.VoiceStockDraft{}, err
	}
	if out.Transcript != "" {
		t := out.Transcript
		v.Transcript = &t
	}
	if out.Language != "" {
		l := out.Language
		v.Language = &l
	}
	var want []pgtype.UUID
	for _, l := range out.Lines {
		if l.Matched() {
			if id, err := ids.Parse(*l.StoreItemID); err == nil {
				want = append(want, ids.UUID(id))
			}
		}
	}
	now, err := s.itemsByIDs(ctx, sc.StoreID, want)
	if err != nil {
		return voiceapi.VoiceStockDraft{}, err
	}
	may := s.permits(ctx, p, sc)
	for _, l := range out.Lines {
		line := voiceapi.VoiceStockLine{
			LineId: l.LineID, Kind: voiceapi.VoiceStockLineKind(l.Kind), StoreItemId: l.StoreItemID,
			ItemName: l.ItemName, Heard: l.Heard, Quantity: l.Quantity, Confidence: float32(l.Confidence),
			Matched: l.Matched(),
		}
		if l.PricePaise != nil {
			pp := int(*l.PricePaise)
			line.PricePaise = &pp
		}
		if l.Matched() {
			if id, err := ids.Parse(*l.StoreItemID); err == nil {
				if r, ok := now[id]; ok {
					price, onHand := int(r.PricePaise), int(r.OnHand)
					line.CurrentPricePaise, line.OnHand = &price, &onHand
				}
			}
		}
		ok, err := may(l.Kind)
		if err != nil {
			return voiceapi.VoiceStockDraft{}, err
		}
		if !ok {
			dis := voiceapi.AskManager
			line.Disabled = &dis
		}
		v.Lines = append(v.Lines, line)
	}
	return v, nil
}

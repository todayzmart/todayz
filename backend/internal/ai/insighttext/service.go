package insighttext

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/todayz/backend/gen/sqlc/airetaildb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

type pending struct {
	row  facts.Row
	hash string
}

// Texts returns the kept sentence of each row (nil for none), in rows' order.
// Rows without a current sentence are written in the background when the
// feature is on; this call never waits for the model. Any failure is logged
// and leaves the template.
func (s *Service) Texts(ctx context.Context, storeID ids.ID, rows []facts.Row) []*string {
	out := make([]*string, len(rows))
	if len(rows) == 0 || s.d.DB == nil || s.d.Guard == nil {
		return out
	}
	log := telemetry.Logger(ctx)
	if on, err := s.d.Guard.Available(ctx, Feature); err != nil || !on {
		return out
	}
	q := airetaildb.New(s.d.DB.Q(ctx))
	store := ids.UUID(storeID)
	lang, err := q.StoreLang(ctx, store)
	if err != nil {
		log.WarnContext(ctx, "insighttext: store language", slog.String("error", err.Error()))
		return out
	}
	kept, err := q.InsightTextsForStore(ctx, airetaildb.InsightTextsForStoreParams{StoreID: store, Lang: lang})
	if err != nil {
		log.WarnContext(ctx, "insighttext: kept texts", slog.String("error", err.Error()))
		return out
	}
	byKey := map[string]airetaildb.InsightTextsForStoreRow{}
	for _, k := range kept {
		byKey[k.Kind+"\x00"+k.SubjectRef] = k
	}
	// Without the misses the kept texts are still served, but nothing is asked
	// for: a failed read must not resend facts that already failed.
	misses, err := q.AiTextMisses(ctx, airetaildb.AiTextMissesParams{
		Feature: Feature, StoreID: store, Lang: lang, KeptAfter: s.d.Clock.Now().Add(-retail.MissTTL),
	})
	missesOK := err == nil
	if !missesOK {
		log.WarnContext(ctx, "insighttext: misses", slog.String("error", err.Error()))
	}
	missed := map[string]string{}
	for _, m := range misses {
		missed[m.Ref] = m.FactsHash
	}
	var missing []pending
	for i, r := range rows {
		h, err := Hash(r, lang)
		if err != nil {
			continue
		}
		if k, ok := byKey[string(r.Kind)+"\x00"+r.SubjectRef]; ok && k.FactsHash == h {
			t := k.Text
			out[i] = &t
			continue
		}
		if missed[missRef(r)] == h {
			continue // these facts failed before; the template shows until they change
		}
		missing = append(missing, pending{row: r, hash: h})
	}
	if len(missing) > 0 && missesOK && s.claim(storeID) {
		bg := context.WithoutCancel(ctx)
		s.d.Go(func() { s.run(bg, storeID, lang, missing) })
	}
	return out
}

// missRef is a row's ref in ai_text_misses.
func missRef(r facts.Row) string { return string(r.Kind) + "/" + r.SubjectRef }

// missReason is the ai_text_misses reason for a whole-call reject, or false
// for a transient one (an error or a timeout), which may ask again. A
// whole-call miss lasts retail.MissTTL; only an ungrounded row's holds until
// its facts change.
func missReason(reject string) (string, bool) {
	switch reject {
	case client.RejectSchema, client.OutcomeInvalid, client.OutcomeTruncated:
		return "schema", true
	case client.OutcomeRefused:
		return "refused", true
	}
	return "", false
}

// claim reports whether this store may start a run now (one per retryAfter).
func (s *Service) claim(storeID ids.ID) bool {
	now := s.d.Clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	k := storeID.String()
	if t, ok := s.tried[k]; ok && now.Sub(t) < retryAfter {
		return false
	}
	s.tried[k] = now
	for id, t := range s.tried { // keep the map small
		if now.Sub(t) >= retryAfter {
			delete(s.tried, id)
		}
	}
	return true
}

// run writes the missing sentences of one store in one call and keeps the
// grounded ones.
func (s *Service) run(ctx context.Context, storeID ids.ID, lang string, missing []pending) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	log := telemetry.Logger(ctx)
	if len(missing) > MaxRows {
		missing = missing[:MaxRows]
	}
	// The model is read first, so a failed read never holds a reservation.
	model, err := s.d.Settings.String(ctx, settings.AiModelFast)
	if err != nil {
		log.WarnContext(ctx, "insighttext: model setting", slog.String("error", err.Error()))
		return
	}
	// Background wording has its own budget (D-228); when it is spent the
	// templates show.
	if err := s.d.Guard.Reserve(ctx, Feature, guard.Subject{Store: storeID, Background: true}); err != nil {
		var ce *guard.CapError
		if !errors.As(err, &ce) {
			log.WarnContext(ctx, "insighttext: guard", slog.String("error", err.Error()))
		}
		return
	}
	in := make([]Row, len(missing))
	for i, m := range missing {
		in[i] = Row{Ref: "r" + strconv.Itoa(i+1), Kind: string(m.row.Kind), Facts: Readable(m.row)}
	}
	w, err := Write(ctx, s.d.LLM, model, storeID.String(), lang, in)
	if serr := s.d.Guard.Settle(ctx, w.Resp.CostPaise); serr != nil {
		log.WarnContext(ctx, "insighttext: settle", slog.String("error", serr.Error()))
	}
	if err != nil {
		log.WarnContext(ctx, "insighttext: model call", slog.String("error", err.Error()))
		return
	}
	q := airetaildb.New(s.d.DB.Q(ctx))
	now := s.d.Clock.Now().UTC()
	miss := func(p pending, reason string) bool {
		if err := q.UpsertAiTextMiss(ctx, airetaildb.UpsertAiTextMissParams{
			Feature: Feature, StoreID: ids.UUID(storeID), Ref: missRef(p.row), Lang: lang,
			FactsHash: p.hash, Reason: reason, Now: now,
		}); err != nil {
			log.WarnContext(ctx, "insighttext: keep miss", slog.String("error", err.Error()))
			return false
		}
		return true
	}
	if w.Reject != "" {
		if reason, ok := missReason(w.Reject); ok {
			for _, p := range missing {
				if !miss(p, reason) {
					return
				}
			}
		}
		return
	}
	m := w.Resp.Model
	if m == "" {
		m = model
	}
	for i, p := range missing {
		t, ok := w.Texts[in[i].Ref]
		if !ok {
			reason := "skipped"
			if w.Ungrounded[in[i].Ref] {
				reason = "grounding"
			}
			if !miss(p, reason) {
				return
			}
			continue
		}
		if err := q.UpsertInsightText(ctx, airetaildb.UpsertInsightTextParams{
			StoreID: ids.UUID(storeID), Kind: string(p.row.Kind), SubjectRef: p.row.SubjectRef, Lang: lang,
			FactsHash: p.hash, Text: t, PromptVersion: prompts.Version(Feature), Model: m, Now: now,
		}); err != nil {
			log.WarnContext(ctx, "insighttext: keep", slog.String("error", err.Error()))
			return
		}
	}
}

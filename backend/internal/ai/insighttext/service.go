package insighttext

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/todayz/backend/gen/sqlc/airetaildb"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/ai/prompts"
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
		missing = append(missing, pending{row: r, hash: h})
	}
	if len(missing) > 0 && s.claim(storeID) {
		bg := context.WithoutCancel(ctx)
		s.d.Go(func() { s.run(bg, storeID, lang, missing) })
	}
	return out
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
	// TODO(lead): a background run counts against the store's daily AI calls
	// cap like a call the owner started (conservative, as the coach does).
	if err := s.d.Guard.Reserve(ctx, Feature, guard.Subject{Store: storeID}); err != nil {
		var ce *guard.CapError
		if !errors.As(err, &ce) {
			log.WarnContext(ctx, "insighttext: guard", slog.String("error", err.Error()))
		}
		return
	}
	model, err := s.d.Settings.String(ctx, settings.AiModelFast)
	if err != nil {
		log.WarnContext(ctx, "insighttext: model setting", slog.String("error", err.Error()))
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
	if err != nil || w.Reject != "" {
		if err != nil {
			log.WarnContext(ctx, "insighttext: model call", slog.String("error", err.Error()))
		}
		return
	}
	m := w.Resp.Model
	if m == "" {
		m = model
	}
	q := airetaildb.New(s.d.DB.Q(ctx))
	now := s.d.Clock.Now().UTC()
	for i, p := range missing {
		t, ok := w.Texts[in[i].Ref]
		if !ok {
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

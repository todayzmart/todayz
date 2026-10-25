package voice

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/todayz/backend/gen/sqlc/voicedb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/scrub"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Recording is one uploaded recording.
type Recording struct {
	Data      []byte
	MediaType string
	Language  string // a VoiceStockLanguage, or "" to detect
	// Length is what the container says, or MaxSeconds when it does not (tooLong); the
	// speech cost is charged on it.
	Length time.Duration
	// Measured is whether Length came from the container.
	Measured bool
}

// inputMeta is the draft's input_meta: small facts, never the audio or its words.
type inputMeta struct {
	Language  string `json:"language,omitempty"`
	Bytes     int    `json:"bytes"`
	MediaType string `json:"mediaType"`
	// DurationMs is the length the container states (absent when it does not say).
	DurationMs int64 `json:"durationMs,omitempty"`
}

// Create checks the switches and the daily cap, stores a processing draft and starts the
// speech and model work in the background. It returns the draft id at once.
func (s *Service) Create(ctx context.Context, sc authz.Scope, actor ids.ID, rec Recording) (ids.ID, error) {
	if err := s.d.Guard.Require(ctx, Feature); err != nil {
		return ids.Zero, err
	}
	meta := inputMeta{Language: rec.Language, Bytes: len(rec.Data), MediaType: rec.MediaType}
	if rec.Measured {
		meta.DurationMs = rec.Length.Milliseconds()
	}
	// The call is counted and the draft stored together: a draft that cannot be stored does
	// not use up one of the store's calls for the day.
	var id ids.ID
	err := s.d.DB.InTx(ctx, func(ctx context.Context) error {
		if err := s.d.Guard.Reserve(ctx, Feature, guard.Subject{Store: sc.StoreID}); err != nil {
			return err
		}
		var err error
		id, err = s.d.Drafts.Create(ctx, drafts.NewDraft{
			Feature: Feature, Store: sc.StoreID, Actor: actor, PromptVersion: prompts.Version(Feature),
			InputMeta: meta,
		})
		return err
	})
	if err != nil {
		var ce *guard.CapError
		if errors.As(err, &ce) {
			return ids.Zero, ce.Problem()
		}
		return ids.Zero, err
	}
	// The work outlives the request: it keeps the request's values (logger, trace) but not
	// its cancel, and has its own deadline.
	bg := context.WithoutCancel(ctx)
	s.d.Go(func() {
		ctx, cancel := context.WithTimeout(bg, ProcessTimeout)
		defer cancel()
		s.process(ctx, id, sc.StoreID, rec)
	})
	return id, nil
}

// process turns one recording into draft lines, or fails the draft.
func (s *Service) process(ctx context.Context, id, storeID ids.ID, rec Recording) {
	start := s.d.Clock.Now()
	log := telemetry.Logger(ctx)
	fail := func(reason string, resp client.Response) {
		if _, err := s.d.Drafts.MarkFailed(ctx, id, reason, resp); err != nil {
			log.ErrorContext(ctx, "voice: mark failed", slog.String("draft_id", id.String()), slog.Any("err", err))
		}
		recordDraft(ctx, outcomeFailed, s.d.Clock.Now().Sub(start), -1, resp.CostPaise)
	}

	tr, err := s.d.STT.Transcribe(ctx, client.Audio{Data: rec.Data, MediaType: rec.MediaType, Filename: "voice" + extension(rec.MediaType), Language: rec.Language})
	// Speech is charged by length whenever the provider took the recording, answered or not
	// (a call that never left, not configured or refused here, costs nothing).
	speech := client.Response{}
	if !errors.Is(err, client.ErrNotConfigured) && !errors.Is(err, client.ErrBadRequest) {
		speech.CostPaise = client.SpeechCostPaise(rec.Length)
		if serr := s.d.Guard.Settle(ctx, speech.CostPaise); serr != nil {
			log.ErrorContext(ctx, "voice: settle speech", slog.Any("err", serr))
		}
	}
	if err != nil {
		fail(client.Outcome(err), speech)
		return
	}
	transcript := truncate(strings.TrimSpace(scrub.Text(tr.Text, scrub.Options{}).Text), MaxTranscript)
	if transcript == "" {
		fail(FailEmpty, speech)
		return
	}
	cands, err := s.candidates(ctx, storeID)
	if err != nil {
		log.ErrorContext(ctx, "voice: candidates", slog.String("draft_id", id.String()), slog.Any("err", err))
		fail(client.OutcomeError, speech)
		return
	}
	p, err := Parse(ctx, s.d.LLM, s.model(ctx), transcript, cands)
	if serr := s.d.Guard.Settle(ctx, p.Resp.CostPaise); serr != nil {
		log.ErrorContext(ctx, "voice: settle", slog.Any("err", serr))
	}
	// The draft's cost is both calls'.
	p.Resp.CostPaise += speech.CostPaise
	switch {
	case err != nil:
		log.ErrorContext(ctx, "voice: model call", slog.String("draft_id", id.String()), slog.Any("err", err))
		fail(client.OutcomeError, p.Resp)
		return
	case p.Reject != "":
		fail(p.Reject, p.Resp)
		return
	case len(p.Lines) == 0:
		fail(FailEmpty, p.Resp)
		return
	}
	lang := tr.Language
	if lang == "" {
		lang = rec.Language
	}
	out := Output{Transcript: transcript, Language: truncate(lang, 16), Lines: p.Lines}
	if _, err := s.d.Drafts.MarkReady(ctx, id, out, p.Resp); err != nil {
		log.ErrorContext(ctx, "voice: mark ready", slog.String("draft_id", id.String()), slog.Any("err", err))
		fail(client.OutcomeError, p.Resp)
		return
	}
	recordDraft(ctx, outcomeReady, s.d.Clock.Now().Sub(start), matchedRatio(p.Lines), p.Resp.CostPaise)
}

// candidates is the store's top items by units sold, as refs c1, c2, …
func (s *Service) candidates(ctx context.Context, storeID ids.ID) ([]Candidate, error) {
	rows, err := voicedb.New(s.d.DB.Q(ctx)).Candidates(ctx, voicedb.CandidatesParams{
		StoreID: ids.UUID(storeID), Since: s.d.Clock.Now().Add(-SalesWindow), MaxRows: MaxCandidates,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(rows))
	for i, r := range rows {
		id, ok := ids.FromUUID(r.ID)
		if !ok {
			return nil, errNullID
		}
		out = append(out, Candidate{Ref: "c" + strconv.Itoa(i+1), ID: id, Name: candidateName(r)})
	}
	return out, nil
}

// candidateName is the name the model matches against: name, variant and pack size.
func candidateName(r voicedb.CandidatesRow) string {
	parts := []string{strings.TrimSpace(r.Name)}
	if r.Variant != nil && strings.TrimSpace(*r.Variant) != "" && !strings.Contains(strings.ToLower(r.Name), strings.ToLower(*r.Variant)) {
		parts = append(parts, strings.TrimSpace(*r.Variant))
	}
	// The pack is added unless the name already says it ("Maggi 70 g" or "Maggi 70g").
	size, unit := packSize(r.PackSize), strings.TrimSpace(r.Unit)
	name := strings.ToLower(r.Name)
	if pack := strings.TrimSpace(size + " " + unit); pack != "" &&
		!strings.Contains(name, strings.ToLower(pack)) && !strings.Contains(name, strings.ToLower(size+unit)) {
		parts = append(parts, pack)
	}
	// The scrubber also runs on item names: a shop may have typed a phone into one.
	return scrub.Text(strings.Join(parts, " "), scrub.Options{}).Text
}

// packSize drops a numeric's trailing zeros ("100.000" is 100, "0.500" is 0.5).
func packSize(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// model reads the model id from ai_model_fast at the moment of the call.
func (s *Service) model(ctx context.Context) Model {
	return func(k settings.Key) string {
		v, err := s.d.Settings.String(ctx, k)
		if err != nil {
			return ""
		}
		return v
	}
}

func extension(mediaType string) string {
	switch mediaType {
	case "audio/ogg":
		return ".ogg"
	case "audio/wav":
		return ".wav"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4":
		return ".m4a"
	default:
		return ".webm"
	}
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

func matchedRatio(lines []Line) float64 {
	if len(lines) == 0 {
		return 0
	}
	n := 0
	for _, l := range lines {
		if l.Matched() {
			n++
		}
	}
	return float64(n) / float64(len(lines))
}

// DropTranscripts removes the transcript and the heard words from voice drafts made more
// than TranscriptRetention before now (02 §8 step 6). It returns how many drafts changed.
func DropTranscripts(ctx context.Context, q voicedb.DBTX, now time.Time) (int64, error) {
	return voicedb.New(q).DropOldTranscripts(ctx, voicedb.DropOldTranscriptsParams{Now: now, Before: now.Add(-TranscriptRetention)})
}

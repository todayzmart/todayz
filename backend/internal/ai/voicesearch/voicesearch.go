// Package voicesearch is voice search (AI-C5, feature voice_search, D-101): the customer
// speaks a search, the speech provider writes it down and the refine chips are read from the
// words (querynorm's deterministic parser, no model).
//
// Rules it holds:
//   - It never searches. The transcript fills the search box (C-05 <VoiceSearchSlot/>) and the
//     customer searches it (GET /v1/discover/search?source=voice).
//   - It keeps nothing: no draft, no bucket object, no audio, no transcript. Neither the audio
//     nor the words reach a log line, a trace attribute, an event or a metric label (ASK-16,
//     ASK-26); the provider's call is metered by outcome only.
//   - One recording of at most voice.MaxSeconds and voice.MaxAudioBytes, the voice stock
//     update's own limits and form reader.
//   - Each use is one call against the customer's daily cap (ai_daily_calls_customer, 429)
//     and the speech is charged by length whenever the provider took the recording.
//   - A speech provider failure is 503 (C-23 hides the mic for the session); the common 404
//     while ai_voicesearch_enabled is off or the speech key is missing. It needs no model key.
package voicesearch

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/todayz/backend/gen/sqlc/aiconsumerdb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/ai/querynorm"
	"github.com/todayz/backend/internal/ai/scrub"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
	"github.com/todayz/backend/internal/voice"
)

// Feature is the voice search feature name (client.Features).
const Feature = "voice_search"

// MaxTranscript bounds the transcript (VoiceSearchResult.transcript maxLength).
const MaxTranscript = 200

// Languages are the result's languages (AiLanguage); a transcript in another is en.
var Languages = []string{"en", "hi", "kn", "te", "ta"}

// Deps are the service's dependencies.
type Deps struct {
	DB    *db.DB
	Guard *guard.Guard
	STT   client.STT
}

// Service is voice search.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) (*Service, error) {
	if d.DB == nil || d.Guard == nil || d.STT == nil {
		return nil, errors.New("voicesearch: db, guard and stt are required")
	}
	return &Service{d: d}, nil
}

// Result is what the customer sees: the words for the search box, their language and chips.
type Result struct {
	Transcript string
	Language   string
	Chips      []querynorm.Chip
}

// ErrUnavailable is the speech provider failing (503).
var ErrUnavailable = errors.New("voicesearch: speech provider failed")

// Search transcribes one recording and reads its chips. It stores nothing.
func (s *Service) Search(ctx context.Context, customer ids.ID, rec voice.Recording) (Result, error) {
	if err := s.d.Guard.Reserve(ctx, Feature, guard.Subject{Customer: customer}); err != nil {
		var ce *guard.CapError
		if errors.As(err, &ce) {
			return Result{}, ce.Problem()
		}
		return Result{}, err
	}
	tr, err := s.d.STT.Transcribe(ctx, client.Audio{Data: rec.Data, MediaType: rec.MediaType, Filename: voice.Filename(rec.MediaType), Language: rec.Language})
	// Speech is charged by length whenever the provider took the recording, answered or not.
	if !errors.Is(err, client.ErrNotConfigured) && !errors.Is(err, client.ErrBadRequest) {
		if serr := s.d.Guard.Settle(ctx, client.SpeechCostPaise(rec.Length)); serr != nil {
			telemetry.Logger(ctx).ErrorContext(ctx, "voicesearch: settle", "err", serr)
		}
	}
	if err != nil {
		return Result{}, ErrUnavailable
	}
	text := Clean(tr.Text)
	var brands []querynorm.Brand
	if text != "" {
		rows, err := aiconsumerdb.New(s.d.DB.Q(ctx)).BrandsNamedIn(ctx, text)
		if err != nil {
			return Result{}, err
		}
		for _, r := range rows {
			brands = append(brands, querynorm.Brand{Name: r.Name, Slug: r.Slug})
		}
	}
	return Read(text, tr.Language, rec.Language, brands), nil
}

// Clean is the transcript as the customer may see it: phones, codes and ids scrubbed (the
// [bracketed] marks dropped), spaces collapsed, at most MaxTranscript characters.
func Clean(text string) string {
	t := scrub.Text(text, scrub.Options{}).Text
	t = strings.Join(strings.Fields(t), " ")
	if utf8.RuneCountInString(t) > MaxTranscript {
		t = strings.TrimSpace(string([]rune(t)[:MaxTranscript]))
	}
	return t
}

// Read is the result for a cleaned transcript: the language the provider heard (or the one
// the customer picked) and the chips of the brands named in it.
func Read(text, heard, picked string, brands []querynorm.Brand) Result {
	return Result{Transcript: text, Language: Language(heard, picked), Chips: querynorm.Parse(text, brands).Chips}
}

// Language maps a BCP-47 code (hi-IN, kn-IN, …) to an AiLanguage: the provider's, else the
// customer's pick, else en.
func Language(heard, picked string) string {
	for _, l := range []string{heard, picked} {
		code, _, _ := strings.Cut(strings.ToLower(l), "-")
		for _, k := range Languages {
			if code == k {
				return k
			}
		}
	}
	return "en"
}

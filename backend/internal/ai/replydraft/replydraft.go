// Package replydraft drafts a shop's reply to a customer review (AI-R6, B-17 reviews): the
// model writes one polite reply in the review's language; the owner edits it and posts it.
//
// Rules this package holds:
//   - Nothing is posted until a person confirms (11 §1 rule 1). Confirm posts through the
//     reviews domain's own reply (the same cleaning, one reply per review), in the draft's
//     transaction; discarded posts nothing.
//   - The model sees the stars, the tags and the review text scrubbed of phones, codes, ids and
//     the like; never the customer's name. The draft row keeps the review id, not the text.
//   - The reply may carry no number the review does not (schema.Ungrounded): a draft that
//     does fails as grounding, so a reply never invents a price, a time or an amount.
//   - Language: the script of the review text (Devanagari hi, Kannada kn, Telugu te, Tamil ta,
//     else en); a review with no text takes the shop's language.
//   - The common 404 while ai_replydraft_enabled is off or the key is missing; the route needs
//     review.reply (owner and manager).
package replydraft

import (
	"context"
	"errors"
	"unicode"

	reviewsapi "github.com/todayz/backend/gen/api/reviews"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// Feature is the drafts feature name.
const Feature = "review_reply"

// Fixed numbers (the contract's limits, not settings).
const (
	// MaxReply bounds the drafted reply (ReviewReplyLine.text maxLength).
	MaxReply = 500
	// MaxEdited bounds an edited reply (ReviewReplyLineValue.text maxLength, the reviews
	// domain's own reply limit).
	MaxEdited = 1000
)

// Replies posts the store's reply to a review through the reviews domain.
type Replies interface {
	Reply(ctx context.Context, userID, storeID, reviewID ids.ID, text string) (reviewsapi.ReviewReply, error)
}

// Deps are the service's dependencies.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	LLM      client.LLM
	Kit      retail.Kit
	Replies  Replies
}

// Service is the review reply draft.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("replydraft: a clock is required")
	}
	d.Kit.Clock = d.Clock
	return &Service{d: d}, nil
}

// Languages are the five the apps ship.
var Languages = []string{"en", "hi", "kn", "te", "ta"}

// LangOf is the language of a review by the script most of its letters are in, "" when it
// has no letters (the caller then uses the shop's language).
func LangOf(text string) string {
	counts := map[string]int{}
	letters := 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		switch {
		case unicode.Is(unicode.Devanagari, r):
			counts["hi"]++
		case unicode.Is(unicode.Kannada, r):
			counts["kn"]++
		case unicode.Is(unicode.Telugu, r):
			counts["te"]++
		case unicode.Is(unicode.Tamil, r):
			counts["ta"]++
		}
	}
	if letters == 0 {
		return ""
	}
	best, n := "en", 0
	for _, l := range Languages[1:] {
		if counts[l] > n {
			best, n = l, counts[l]
		}
	}
	if n*2 < letters { // mostly Latin: English (or Hinglish, answered in English)
		return "en"
	}
	return best
}

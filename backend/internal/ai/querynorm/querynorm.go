// Package querynorm is the customer's search helpers that read a query (01 §11, D-101): the
// deterministic refine-chip parser (chips.go: price caps, open now, pickup, delivery,
// offers, near me and master-catalogue brands in the five languages), and search assist
// (AI-C2, feature search_assist), the model's suggestions for a query search did not
// understand.
//
// Rules search assist holds:
//   - The model never runs inside search. The app calls it only after the results rendered,
//     only when the search answered refineHint and only when signed in; nothing applies
//     until the customer taps a suggestion (C-05, <SearchAssistSlot/>).
//   - One call has SearchAssistTimeout (01 §17 fixed constants). A timeout, a provider error,
//     output that fails validation or nothing to suggest is 204: the customer sees nothing
//     and the results stay as they were. The customer's daily cap is 429.
//   - It stores nothing: no ai_drafts row (D-101), no search_log row, and the query is never
//     logged. It reaches the model scrubbed of phones, codes and ids.
//   - A chip is only an allowed kind, at most one of each; a price must be a number of the
//     query, a brand a slug of the master catalogue, and its words the query's own.
//   - The common 404 while ai_querynorm_enabled is off or the model key is missing.
package querynorm

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/todayz/backend/gen/sqlc/aiconsumerdb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/ai/schema"
	"github.com/todayz/backend/internal/ai/scrub"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Feature is the search assist feature name (client.Features).
const Feature = "search_assist"

// Fixed numbers (01 §17 and the contract's limits, not settings).
const (
	// SearchAssistTimeout bounds one search assist call (01 §17 fixed constants).
	SearchAssistTimeout = 3 * time.Second
	// MaxQuery bounds the query and the suggested query (SearchAssistRequest.q,
	// SearchAssistResult.query maxLength).
	MaxQuery = 200
	// MaxChips bounds the suggestions (SearchAssistResult.chips maxItems).
	MaxChips = 6
)

// Deps are the service's dependencies.
type Deps struct {
	DB       *db.DB
	Settings *settings.Cache
	LLM      client.LLM
	Guard    *guard.Guard
}

// Service is search assist.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) (*Service, error) {
	if d.DB == nil || d.Settings == nil || d.Guard == nil {
		return nil, errors.New("querynorm: db, settings and guard are required")
	}
	return &Service{d: d}, nil
}

// Suggestion is search assist's answer: a cleaner query ("" when the same as q) and chips.
type Suggestion struct {
	Query string
	Chips []Chip
}

// Empty reports whether there is nothing to show.
func (s Suggestion) Empty() bool { return s.Query == "" && len(s.Chips) == 0 }

// Assist suggests a query and chips for q. Nothing to show (a timeout, a provider error,
// output that fails validation, no suggestion) is the empty Suggestion and no error; the
// switch is the common 404 and the customer's cap the 429.
func (s *Service) Assist(ctx context.Context, customer ids.ID, q string) (Suggestion, error) {
	if err := s.d.Guard.Require(ctx, Feature); err != nil {
		return Suggestion{}, err
	}
	q = strings.Join(strings.Fields(q), " ")
	if n := utf8.RuneCountInString(q); n == 0 || n > MaxQuery {
		return Suggestion{}, fieldErr("body.q", "length")
	}
	model, err := s.d.Settings.String(ctx, settings.AiModelFast)
	if err != nil {
		return Suggestion{}, err
	}
	if err := s.d.Guard.Reserve(ctx, Feature, guard.Subject{Customer: customer}); err != nil {
		var ce *guard.CapError
		if errors.As(err, &ce) {
			return Suggestion{}, ce.Problem()
		}
		return Suggestion{}, err
	}
	clean := strings.TrimSpace(scrub.Text(q, scrub.Options{}).Text)
	cctx, cancel := context.WithTimeout(ctx, SearchAssistTimeout)
	defer cancel()
	out, resp, reject := Suggest(cctx, s.d.LLM, model, "", clean)
	if err := s.d.Guard.Settle(ctx, resp.CostPaise); err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "querynorm: settle", "err", err)
	}
	if reject != "" {
		return Suggestion{}, nil
	}
	return s.check(ctx, clean, out)
}

// check keeps what may be shown of the model's answer.
func (s *Service) check(ctx context.Context, q string, out ToolOutput) (Suggestion, error) {
	var slugs []string
	for _, c := range out.Chips {
		if c.Kind == KindBrand && c.Value != nil {
			slugs = append(slugs, *c.Value)
		}
	}
	known := map[string]bool{}
	if len(slugs) > 0 {
		rows, err := aiconsumerdb.New(s.d.DB.Q(ctx)).BrandsBySlug(ctx, slugs)
		if err != nil {
			return Suggestion{}, err
		}
		for _, r := range rows {
			known[r.Slug] = true
		}
	}
	sg := Validate(q, out, func(slug string) bool { return known[slug] })
	if sg.Empty() {
		client.RecordRejected(ctx, Feature, client.RejectGrounding)
	}
	return sg, nil
}

// Validate keeps the suggested query and the chips that pass the rules (the package doc);
// brand says whether a slug is in the master catalogue.
func Validate(q string, out ToolOutput, brand func(slug string) bool) Suggestion {
	var sg Suggestion
	if out.Query != nil {
		v := strings.Join(strings.Fields(*out.Query), " ")
		if v != "" && !strings.EqualFold(v, q) && utf8.RuneCountInString(v) <= MaxQuery &&
			len(schema.Ungrounded(v, q)) == 0 && scrub.Clean(v) {
			sg.Query = v
		}
	}
	low := strings.ToLower(q)
	seen := map[string]bool{}
	for _, c := range out.Chips {
		text := strings.Join(strings.Fields(c.Text), " ")
		if seen[c.Kind] || order(c.Kind) == len(Kinds) || text == "" || utf8.RuneCountInString(text) > MaxChipText ||
			!strings.Contains(low, strings.ToLower(text)) || len(sg.Chips) >= MaxChips {
			continue
		}
		chip := Chip{Kind: c.Kind, Text: text}
		switch c.Kind {
		case KindMaxPrice:
			if c.Value == nil || !groundedPrice(*c.Value, q) {
				continue
			}
			chip.Value = *c.Value
		case KindBrand:
			if c.Value == nil || !brand(*c.Value) {
				continue
			}
			chip.Value = *c.Value
		default:
			if c.Value != nil && *c.Value != "" {
				continue
			}
		}
		seen[c.Kind] = true
		sg.Chips = append(sg.Chips, chip)
	}
	return sg
}

// groundedPrice reports whether v is a whole-rupee price in paise whose rupees are a number
// of q.
func groundedPrice(v, q string) bool {
	p, err := strconv.ParseInt(v, 10, 64)
	if err != nil || p <= 0 || p%100 != 0 || p/100 > MaxPriceRupees || strconv.FormatInt(p, 10) != v {
		return false
	}
	return len(schema.Ungrounded(strconv.FormatInt(p/100, 10), q)) == 0
}

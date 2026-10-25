// Package listparse reads a shopping list a customer typed, pasted or spoke (AI-C1, C-22
// basket editor): "1 kg atta, 2 Nandini milk, Dolo 650" in English, Hindi, Kannada, Telugu,
// Tamil or a mix becomes a draft of basket lines the customer confirms one by one.
//
// Rules this package holds:
//   - Parsing saves nothing and searches nothing (BSK-15): no basket, no search_log row, no
//     catalogue lookup. The draft keeps the lines only; the list itself is never stored and
//     reaches the model scrubbed of phones, codes and ids.
//   - Confirm writes nothing either: it answers the accepted and edited lines (basketLines),
//     which the app adds to the basket editor; the customer saves the basket themselves.
//   - A line may carry no number the list does not (a quantity written as a word in one of
//     the five languages counts): a line that does is dropped, and a draft with no line left
//     fails (grounding, or empty when the model found nothing).
//   - At most basket_max_items lines; the rest are dropped.
//   - The common 404 while ai_listparse_enabled is off or the model key is missing; the
//     routes need me.ai.use, and a draft is its customer's own (another's is the common 404).
package listparse

import (
	"context"
	"errors"
	"log/slog"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Feature is the drafts feature name.
const Feature = "list_parse"

// Fixed numbers (the contract's limits, not settings).
const (
	// MaxInput bounds the list (ListParseDraftRequest.text maxLength).
	MaxInput = 2000 //settings:ok the contract's maxLength, not a radius or share
	// MaxText bounds one line (ListParseLine.text, BasketLineInput.text maxLength).
	MaxText = 120
	// MaxQuantity bounds one line's count (BasketLineInput.quantity maximum).
	MaxQuantity = 99
	// MaxLines bounds the model's answer (ListParseDraft.lines maxItems); basket_max_items
	// cuts it further.
	MaxLines = 100
)

// Deps are the service's dependencies.
type Deps struct {
	Clock    clock.Clock
	Settings *settings.Cache
	LLM      client.LLM
	Guard    *guard.Guard
	Drafts   *drafts.Service
	// Go runs the background parse of one draft; nil starts a goroutine. Tests pass a
	// runner that waits.
	Go func(func())
}

// Service is the list parse draft.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil || d.Guard == nil || d.Drafts == nil || d.Settings == nil {
		return nil, errors.New("listparse: clock, settings, guard and drafts are required")
	}
	return &Service{d: d}, nil
}

// start checks the switch and the customer's daily cap, stores a processing draft and runs
// work in the background with retail.ProcessTimeout (the customer-scoped twin of
// retail.Kit.Start).
func (s *Service) start(ctx context.Context, customer ids.ID, meta any, work func(ctx context.Context, id ids.ID)) (ids.ID, error) {
	if err := s.d.Guard.Require(ctx, Feature); err != nil {
		return ids.Zero, err
	}
	if err := s.d.Guard.Reserve(ctx, Feature, guard.Subject{Customer: customer}); err != nil {
		var ce *guard.CapError
		if errors.As(err, &ce) {
			return ids.Zero, ce.Problem()
		}
		return ids.Zero, err
	}
	id, err := s.d.Drafts.Create(ctx, drafts.NewDraft{
		Feature: Feature, Actor: customer, InputMeta: meta, PromptVersion: promptVersion(),
	})
	if err != nil {
		return ids.Zero, err
	}
	bg := context.WithoutCancel(ctx)
	run := s.d.Go
	if run == nil {
		run = func(f func()) { go f() }
	}
	run(func() {
		ctx, cancel := context.WithTimeout(bg, retail.ProcessTimeout)
		defer cancel()
		work(ctx, id)
	})
	return id, nil
}

// load reads one of the customer's drafts (anyone else's is the common 404) and closes it as
// failed (timeout) first when it has been processing longer than retail.StaleAfter.
func (s *Service) load(ctx context.Context, customer, id ids.ID) (drafts.Draft, error) {
	if err := s.d.Guard.Require(ctx, Feature); err != nil {
		return drafts.Draft{}, err
	}
	d, err := s.d.Drafts.Get(ctx, Feature, id)
	if err != nil {
		return drafts.Draft{}, err
	}
	if err := own(customer)(d); err != nil {
		return drafts.Draft{}, err
	}
	if d.Status == drafts.StatusProcessing && s.d.Clock.Now().Sub(d.CreatedAt) > retail.StaleAfter {
		if _, err := s.d.Drafts.MarkFailed(ctx, id, client.OutcomeTimeout, client.Response{}); err != nil {
			telemetry.Logger(ctx).ErrorContext(ctx, "listparse: close stale draft", slog.String("draft_id", id.String()), slog.Any("err", err))
		} else if d, err = s.d.Drafts.Get(ctx, Feature, id); err != nil {
			return drafts.Draft{}, err
		}
	}
	return d, nil
}

// own is the Check of a read and a confirm: a store draft or another customer's is the
// common 404.
func own(customer ids.ID) func(drafts.Draft) error {
	return func(d drafts.Draft) error {
		if d.Store != ids.Zero || d.Actor != customer {
			return retail.NotFound()
		}
		return nil
	}
}

func (s *Service) settle(ctx context.Context, resp client.Response) {
	if err := s.d.Guard.Settle(ctx, resp.CostPaise); err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "listparse: settle", slog.Any("err", err))
	}
}

func (s *Service) fail(ctx context.Context, id ids.ID, reason string, resp client.Response) {
	if _, err := s.d.Drafts.MarkFailed(ctx, id, reason, resp); err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "listparse: mark failed", slog.String("draft_id", id.String()), slog.Any("err", err))
	}
}

func (s *Service) ready(ctx context.Context, id ids.ID, out Output, resp client.Response) {
	if _, err := s.d.Drafts.MarkReady(ctx, id, out, resp); err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "listparse: mark ready", slog.String("draft_id", id.String()), slog.Any("err", err))
		s.fail(ctx, id, client.OutcomeError, resp)
	}
}

package coach

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/schema"
	"github.com/todayz/backend/internal/coach/detectors"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Feature is the AI feature name of the coach's wording (11 §2.1 step 4).
const Feature = "coach_card"

//go:embed prompt.md
var promptFS embed.FS

func init() { prompts.MustRegister(Feature, promptFS) }

// Tool is the strict tool the model answers through.
var Tool = client.Tool{
	Name:        "write_coach_card",
	Description: "Write the coach card's title and body.",
	Schema: schema.Object("", map[string]schema.S{
		"title": schema.String("The card's title", titleMax),
		"body":  schema.String("The card's body", bodyMax),
	}),
}

// Output is the tool input.
type Output struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Validate is the bounds the strict schema only describes.
func (o *Output) Validate() error {
	t, b := strings.TrimSpace(o.Title), strings.TrimSpace(o.Body)
	switch {
	case t == "" || len([]rune(t)) > titleMax:
		return errors.New("title length")
	case b == "" || len([]rune(b)) > bodyMax:
		return errors.New("body length")
	}
	return nil
}

// cardMaxTokens bounds one reply (a title and a body).
const cardMaxTokens = 400 //settings:ok a provider request bound, not a product number

type generated struct {
	By            string
	PromptVersion *string
	Model         *string
}

// Input is what the model sees for one card: no store name, no person.
type Input struct {
	Lang    string          `json:"lang"`
	Kind    string          `json:"kind"`
	Title   string          `json:"title"`
	Body    string          `json:"body"`
	Numbers json.RawMessage `json:"numbers"`
}

// Rewrite asks the model for the card's wording and checks it. reject is ""
// when the output is used, else why it was not (a client outcome, schema or
// grounding); the caller then keeps the template.
func Rewrite(ctx context.Context, llm client.LLM, model, ref string, in Input) (out Output, resp client.Response, reject string, err error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Output{}, resp, "", err
	}
	msg, err := json.Marshal(in)
	if err != nil {
		return Output{}, resp, "", err
	}
	resp, err = llm.Complete(ctx, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys,
		Messages: []client.Message{client.UserText(string(msg))}, Tool: &Tool, MaxTokens: cardMaxTokens,
	})
	if err != nil {
		var pe *client.ProviderError
		if errors.As(err, &pe) && !pe.Retryable() {
			return Output{}, resp, "", err
		}
		return Output{}, resp, client.Outcome(err), nil
	}
	if schema.Decode(resp.ToolInput, &out) != nil {
		client.RecordRejected(ctx, Feature, client.RejectSchema)
		return Output{}, resp, client.RejectSchema, nil
	}
	out.Title, out.Body = strings.TrimSpace(out.Title), strings.TrimSpace(out.Body)
	// Every number in the card must be one of the fact's (11 §1 rule 6).
	if len(schema.Ungrounded(out.Title+"\n"+out.Body, string(in.Numbers))) > 0 {
		client.RecordRejected(ctx, Feature, client.RejectGrounding)
		return Output{}, resp, client.RejectGrounding, nil
	}
	return out, resp, "", nil
}

// writeAI returns the model's wording when the feature is on, the caps allow
// and the output passes; ok false keeps the template.
func (s *Service) writeAI(ctx context.Context, storeID ids.ID, f detectors.Fact, lang string, tpl Text, nums []byte) (Text, generated, bool) {
	if s.d.LLM == nil || s.d.Guard == nil {
		return Text{}, generated{}, false
	}
	log := telemetry.Logger(ctx)
	on, err := s.d.Guard.Available(ctx, Feature)
	if err != nil || !on {
		return Text{}, generated{}, false
	}
	// TODO(lead): a background card counts against the store's daily AI calls
	// cap (ai_daily_calls_store) like a call the owner started; conservative
	// reading, so a busy coach day cannot exceed it.
	if err := s.d.Guard.Reserve(ctx, Feature, guard.Subject{Store: storeID}); err != nil {
		var ce *guard.CapError
		if !errors.As(err, &ce) {
			log.WarnContext(ctx, "coach: guard", slog.String("error", err.Error()))
		}
		return Text{}, generated{}, false
	}
	model, err := s.d.Settings.String(ctx, settings.AiModelFast)
	if err != nil {
		return Text{}, generated{}, false
	}
	out, resp, reject, err := Rewrite(ctx, s.d.LLM, model, storeID.String(), Input{
		Lang: lang, Kind: string(f.Kind), Title: tpl.Title, Body: tpl.Body, Numbers: nums,
	})
	if serr := s.d.Guard.Settle(ctx, resp.CostPaise); serr != nil {
		log.WarnContext(ctx, "coach: settle", slog.String("error", serr.Error()))
	}
	if err != nil || reject != "" {
		if err != nil {
			log.WarnContext(ctx, "coach: model call", slog.String("error", err.Error()))
		}
		return Text{}, generated{}, false
	}
	v := prompts.Version(Feature)
	m := resp.Model
	if m == "" {
		m = model
	}
	return Text{Title: out.Title, Body: out.Body}, generated{By: "ai", PromptVersion: &v, Model: &m}, true
}


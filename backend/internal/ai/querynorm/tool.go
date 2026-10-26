package querynorm

import (
	"context"
	"embed"
	"fmt"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/ai/schema"
)

//go:embed prompt.md
var promptFS embed.FS

func init() { prompts.MustRegister(Feature, promptFS) }

// Tool is the strict tool the model answers with; kind is an enum of the allowed kinds, so
// a chip of any other kind is a schema break.
var Tool = client.Tool{
	Name:        "suggest_search",
	Description: "Record the product words to search for and the filters the sentence asks for.",
	Schema: schema.Object("", map[string]schema.S{
		"query": schema.Nullable(schema.String("The product to search for, or null", MaxQuery)),
		"chips": schema.Array("The filters the sentence asks for, at most one of each kind", schema.Object("", map[string]schema.S{
			"kind":  schema.Enum("The filter", Kinds...),
			"value": schema.Nullable(schema.String("Paise for maxPrice, the brand slug for brand, else null", MaxChipText)),
			"text":  schema.String("The sentence's words the filter came from, copied exactly", MaxChipText),
		}), MaxChips),
	}),
}

// ToolChip is one chip of the tool input.
type ToolChip struct {
	Kind  string  `json:"kind"`
	Value *string `json:"value"`
	Text  string  `json:"text"`
}

// ToolOutput is the tool input.
type ToolOutput struct {
	Query *string    `json:"query"`
	Chips []ToolChip `json:"chips"`
}

// Validate is the bounds the strict schema only describes.
func (o *ToolOutput) Validate() error {
	if len(o.Chips) > MaxChips {
		return fmt.Errorf("%d chips", len(o.Chips))
	}
	for i, c := range o.Chips {
		if order(c.Kind) == len(Kinds) {
			return fmt.Errorf("chip %d kind", i)
		}
	}
	return nil
}

// Suggest runs the model on one query (already scrubbed). reject is the reason nothing can
// be shown (a client outcome or schema); a provider error the request caused is a reject too:
// search assist answers 204 for every failure.
func Suggest(ctx context.Context, llm client.LLM, model, ref, q string) (ToolOutput, client.Response, string) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return ToolOutput{}, client.Response{}, client.OutcomeError
	}
	var out ToolOutput
	resp, reject, err := retail.Call(ctx, llm, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys, Tool: &Tool, MaxTokens: 512,
		Messages: []client.Message{{Role: client.RoleUser, Content: []client.Block{
			{Text: "The search sentence:\n" + q},
		}}},
	}, &out)
	if err != nil {
		return ToolOutput{}, resp, client.OutcomeError
	}
	return out, resp, reject
}

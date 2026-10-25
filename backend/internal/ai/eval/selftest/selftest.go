// Package selftest is the AI platform's own eval suite (feature ai_selftest): a toy stock
// note parser that runs every platform piece a real feature uses (prompt registry, PII
// scrubber, metered client with a strict tool, schema decode, number grounding) against
// recorded replies, so `make check` proves the pipeline before any feature suite exists.
// It is never served.
package selftest

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/schema"
	"github.com/todayz/backend/internal/ai/scrub"
	"github.com/todayz/backend/internal/platform/settings"
)

// Feature is the suite's feature name.
const Feature = "ai_selftest"

//go:embed prompt.md
var promptFS embed.FS

//go:embed fixtures/*.json
var fixturesFS embed.FS

func init() {
	prompts.MustRegister(Feature, promptFS)
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	eval.Register(eval.Suite{Feature: Feature, PassBar: 1, Fixtures: fx, Run: run})
}

var units = []string{"piece", "kg", "g", "l", "ml", "pack"}

// Tool is the suite's strict tool.
var Tool = client.Tool{
	Name:        "record_stock_lines",
	Description: "Record the items and quantities named in the shop owner's note.",
	Schema: schema.Object("", map[string]schema.S{
		"lines": schema.Array("Items in the note, in the order they appear", schema.Object("", map[string]schema.S{
			"name": schema.String("The item", 60),
			"qty":  schema.Number("Quantity as written in the note", 0, 100000),
			"unit": schema.Enum("Unit", units...),
		}), 20),
	}),
}

// Line is one parsed line.
type Line struct {
	Name string  `json:"name"`
	Qty  float64 `json:"qty"`
	Unit string  `json:"unit"`
}

// Output is the tool input.
type Output struct {
	Lines []Line `json:"lines"`
}

// Validate is the bounds the strict schema only describes.
func (o *Output) Validate() error {
	if len(o.Lines) > 20 {
		return errors.New("more than 20 lines")
	}
	for i, l := range o.Lines {
		if l.Name == "" || len([]rune(l.Name)) > 60 {
			return fmt.Errorf("line %d: name length", i)
		}
		if l.Qty <= 0 || l.Qty > 100000 {
			return fmt.Errorf("line %d: qty out of range", i)
		}
		if !slices.Contains(units, l.Unit) {
			return fmt.Errorf("line %d: unit", i)
		}
	}
	return nil
}

// Parse runs the toy feature: scrub, call, decode, ground. reject is "" or the reason the
// output was refused (a client outcome, schema or grounding).
func Parse(ctx context.Context, env eval.Env, note string) (out Output, reject string, err error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Output{}, "", err
	}
	clean := scrub.Text(note, scrub.Options{}).Text
	resp, err := env.LLM.Complete(ctx, client.Request{
		Feature: Feature, Model: env.Model(settings.AiModelFast), System: sys,
		Messages: []client.Message{client.UserText(clean)}, Tool: &Tool, MaxTokens: 1024,
	})
	if err != nil {
		var pe *client.ProviderError
		if errors.As(err, &pe) && !pe.Retryable() {
			return Output{}, "", err // a request the provider refused is a broken harness
		}
		return Output{}, client.Outcome(err), nil
	}
	if schema.Decode(resp.ToolInput, &out) != nil {
		client.RecordRejected(ctx, Feature, client.RejectSchema)
		return Output{}, client.RejectSchema, nil //nolint:nilerr // a rejected output is a scored outcome, not a harness error
	}
	// Every quantity must appear in the note the model saw (11 §1 rule 6).
	nums := make([]string, 0, len(out.Lines))
	for _, l := range out.Lines {
		nums = append(nums, strconv.FormatFloat(l.Qty, 'f', -1, 64))
	}
	if len(schema.Ungrounded(strings.Join(nums, " "), clean)) > 0 {
		client.RecordRejected(ctx, Feature, client.RejectGrounding)
		return Output{}, client.RejectGrounding, nil
	}
	return out, "", nil
}

type expect struct {
	Lines  []Line `json:"lines"`
	Reject string `json:"reject"`
}

func run(ctx context.Context, env eval.Env, c eval.Case) (eval.Verdict, error) {
	var in struct {
		Note string `json:"note"`
	}
	var want expect
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return eval.Verdict{}, err
	}
	if err := json.Unmarshal(c.Expect, &want); err != nil {
		return eval.Verdict{}, err
	}
	got, reject, err := Parse(ctx, env, in.Note)
	if err != nil {
		return eval.Verdict{}, err
	}
	if reject != want.Reject {
		return eval.Verdict{Note: fmt.Sprintf("rejected %q, want %q", reject, want.Reject)}, nil
	}
	if !slices.Equal(got.Lines, want.Lines) {
		return eval.Verdict{Note: fmt.Sprintf("%d lines, want %d, or a line differs", len(got.Lines), len(want.Lines))}, nil
	}
	return eval.Verdict{Pass: true}, nil
}

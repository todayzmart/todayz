package replydraft

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/ai/schema"
	"github.com/todayz/backend/internal/ai/scrub"
)

//go:embed prompt.md
var promptFS embed.FS

func init() { prompts.MustRegister(Feature, promptFS) }

// Tool is the strict tool the model answers with.
var Tool = client.Tool{
	Name:        "draft_review_reply",
	Description: "Record one drafted reply to the review.",
	Schema: schema.Object("", map[string]schema.S{
		"text": schema.String("The reply, in the language asked", MaxReply),
	}),
}

// ToolOutput is the tool input.
type ToolOutput struct {
	Text string `json:"text"`
}

// Validate is the bounds the strict schema only describes.
func (o *ToolOutput) Validate() error {
	if strings.TrimSpace(o.Text) == "" || utf8.RuneCountInString(o.Text) > MaxReply {
		return fmt.Errorf("text length")
	}
	return nil
}

// Review is what the model is shown of one review.
type Review struct {
	Stars    int      `json:"stars"`
	Tags     []string `json:"tags"`
	Text     string   `json:"text"` // scrubbed
	Language string   `json:"language"`
	Shop     string   `json:"shop"`
}

// NewReview is the model's view of a review: its text scrubbed and trimmed.
func NewReview(stars int, tags []string, text *string, lang, shop string) Review {
	r := Review{Stars: stars, Tags: tags, Language: lang, Shop: shop}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	if text != nil {
		r.Text = strings.TrimSpace(scrub.Text(*text, scrub.Options{}).Text)
	}
	return r
}

// Line is the one stored draft line.
type Line struct {
	LineID   string `json:"lineId"`
	Text     string `json:"text"`
	Language string `json:"language"`
}

// Output is a ready draft's output.
type Output struct {
	Lines []Line `json:"lines"`
}

// Draft is what the model made of one review.
type Draft struct {
	Output Output
	Resp   client.Response
	Reject string
}

// Write runs the model on one review. A reply with a number the review does not carry, or
// with a phone or code, is rejected as grounding.
func Write(ctx context.Context, llm client.LLM, model, ref string, rv Review) (Draft, error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Draft{}, err
	}
	facts, err := json.Marshal(rv)
	if err != nil {
		return Draft{}, err
	}
	var out ToolOutput
	resp, reject, err := retail.Call(ctx, llm, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys, Tool: &Tool, MaxTokens: 512,
		Messages: []client.Message{{Role: client.RoleUser, Content: []client.Block{
			{Text: "The review (JSON):\n" + string(facts)},
		}}},
	}, &out)
	if err != nil || reject != "" {
		return Draft{Resp: resp, Reject: reject}, err
	}
	text := strings.TrimSpace(out.Text)
	if bad := schema.Ungrounded(text, string(facts)); len(bad) > 0 || !scrub.Clean(text) {
		client.RecordRejected(ctx, Feature, client.RejectGrounding)
		return Draft{Resp: resp, Reject: client.RejectGrounding}, nil
	}
	return Draft{Output: Output{Lines: []Line{{LineID: "l1", Text: text, Language: rv.Language}}}, Resp: resp}, nil
}

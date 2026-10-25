// Package imagecheck is AI-R10 (11 §1, W3-11): a model looks at an uploaded
// banner image and answers ok, unsure or reject with reasons (explicit,
// violent, misleading, competitor brand, personal data, illegible text, not a
// banner). The answer is advice for the person who moderates: an uploaded
// banner still waits in pending_review (internal/banners), and a reject or
// unsure answer, or any failure, keeps it there.
//
// TODO(lead): this package is not wired yet. Whether an "ok" answer may clear
// a banner without a person (moderation auto_ok), and where the verdict is
// stored for the admin queue (W3-05's moderation screen), are lead decisions;
// until then the conservative reading is that every uploaded banner keeps
// waiting for a person, as today.
package imagecheck

import (
	"context"
	"embed"
	"fmt"
	"math"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/ai/schema"
)

// Feature is the AI feature name.
const Feature = "image_check"

//go:embed prompt.md
var promptFS embed.FS

func init() { prompts.MustRegister(Feature, promptFS) }

// Verdicts.
const (
	OK     = "ok"
	Unsure = "unsure"
	Reject = "reject"
)

// Reasons are the reasons a verdict may give.
var Reasons = []string{"explicit", "violent", "misleading", "competitor_brand", "personal_data", "illegible_text", "not_a_banner"}

// Tool is the strict tool the model answers with.
var Tool = client.Tool{
	Name:        "check_banner_image",
	Description: "Say whether this banner image may be shown to customers.",
	Schema: schema.Object("", map[string]schema.S{
		"verdict":    schema.Enum("ok, unsure or reject", OK, Unsure, Reject),
		"reasons":    schema.Array("Why it is unsure or rejected; empty when ok", schema.Enum("A reason", Reasons...), len(Reasons)),
		"confidence": schema.Number("How sure you are", 0, 1),
	}),
}

// Output is the tool input.
type Output struct {
	Verdict    string   `json:"verdict"`
	Reasons    []string `json:"reasons"`
	Confidence float64  `json:"confidence"`
}

// Validate is the bounds the strict schema only describes, plus the rule
// that ok has no reason and the others have at least one.
func (o *Output) Validate() error {
	switch o.Verdict {
	case OK:
		if len(o.Reasons) > 0 {
			return fmt.Errorf("ok with reasons")
		}
	case Unsure, Reject:
		if len(o.Reasons) == 0 {
			return fmt.Errorf("%s without a reason", o.Verdict)
		}
	default:
		return fmt.Errorf("verdict")
	}
	seen := map[string]bool{}
	for _, r := range o.Reasons {
		if !isReason(r) || seen[r] {
			return fmt.Errorf("reason")
		}
		seen[r] = true
	}
	if math.IsNaN(o.Confidence) || o.Confidence < 0 || o.Confidence > 1 {
		return fmt.Errorf("confidence")
	}
	return nil
}

func isReason(r string) bool {
	for _, x := range Reasons {
		if x == r {
			return true
		}
	}
	return false
}

// Result is the check of one image. Verdict is Unsure whenever the model
// could not answer (Reject then says why).
type Result struct {
	Output
	Resp   client.Response
	Reject string
}

// HoldsForReview reports whether a person must look before the banner shows.
// Today that is always true (see the package TODO); it is false only for an
// ok answer, which is what an auto-clear would use.
func (r Result) HoldsForReview() bool { return r.Reject != "" || r.Verdict != OK }

// Check runs the model on one banner image. ref is the banner id (a case name
// in the eval). A provider error the request caused is returned as an error;
// the caller keeps the banner for review either way.
func Check(ctx context.Context, llm client.LLM, model, ref string, img client.Image) (Result, error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Result{}, err
	}
	var out Output
	resp, reject, err := retail.Call(ctx, llm, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys, Tool: &Tool, MaxTokens: 256,
		Messages: []client.Message{{Role: client.RoleUser, Content: []client.Block{
			{Image: &img}, {Text: "Check this banner image."},
		}}},
	}, &out)
	if err != nil || reject != "" {
		return Result{Output: Output{Verdict: Unsure}, Resp: resp, Reject: reject}, err
	}
	if out.Reasons == nil {
		out.Reasons = []string{}
	}
	return Result{Output: out, Resp: resp}, nil
}

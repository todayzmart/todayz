package riskexplain

import (
	"context"
	"embed"
	"encoding/json"
	"regexp"
	"strings"

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
	Name:        "summarise_risk_flag",
	Description: "Record the short summary of the risk flag.",
	Schema: schema.Object("", map[string]schema.S{
		"text": schema.String("The summary, in English", MaxSummary),
	}),
}

// ToolOutput is the tool input.
type ToolOutput struct {
	Text string `json:"text"`
}

// History is the subject's other flags by status.
type History struct {
	Open      int `json:"open"`
	Confirmed int `json:"confirmed"`
	Cleared   int `json:"cleared"`
}

// Flag is what the model is shown of one flag.
type Flag struct {
	Rule        string            `json:"rule"`
	SubjectType string            `json:"subjectType"`
	Score       int               `json:"score"`
	Status      string            `json:"status"`
	Facts       map[string]string `json:"facts"`
	OtherFlags  History           `json:"otherFlags"`
}

// NewFlag is the model's view of a flag: ids left out, every value scrubbed.
func NewFlag(rule, subjectType string, score int, status string, facts []Fact, h History) Flag {
	f := Flag{Rule: rule, SubjectType: subjectType, Score: score, Status: status, Facts: map[string]string{}, OtherFlags: h}
	for _, x := range facts {
		if strings.HasSuffix(x.Key, "Id") || strings.HasSuffix(x.Key, "Ids") || strings.EqualFold(x.Key, "id") {
			continue
		}
		v := strings.TrimSpace(x.Value)
		if !bareNumber.MatchString(v) {
			v = strings.TrimSpace(scrub.Text(v, scrub.Options{}).Text)
		}
		f.Facts[x.Key] = v
	}
	return f
}

// bareNumber is a fact that is only a count or amount, shorter than a phone number: scrub's
// PIN rule would cut a six-digit one (125000 paise), and nothing in it can identify a person.
var bareNumber = regexp.MustCompile(`^-?[0-9]{1,9}(\.[0-9]+)?$`)

// JSON is the facts JSON the model is given and the summary is grounded against.
func (f Flag) JSON() []byte {
	b, _ := json.Marshal(f) // a struct of strings and ints always marshals
	return b
}

// Grounded reports whether text holds only the flag's numbers and nothing scrub would cut.
func (f Flag) Grounded(text string) bool {
	return len(schema.UngroundedFacts(text, f.JSON())) == 0 && scrub.Clean(text)
}

// Line is the one stored draft line.
type Line struct {
	LineID string `json:"lineId"`
	Text   string `json:"text"`
}

// Output is a ready draft's output.
type Output struct {
	Lines []Line `json:"lines"`
}

// Draft is what the model made of one flag.
type Draft struct {
	Output Output
	Resp   client.Response
	Reject string
}

// Write runs the model on one flag. A summary with a number the flag does not carry, or with
// a phone or code, is rejected as grounding.
func Write(ctx context.Context, llm client.LLM, model, ref string, f Flag) (Draft, error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Draft{}, err
	}
	var out ToolOutput
	resp, reject, err := retail.Call(ctx, llm, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys, Tool: &Tool, MaxTokens: 512,
		Messages: []client.Message{{Role: client.RoleUser, Content: []client.Block{
			{Text: "The flag (JSON):\n" + string(f.JSON())},
		}}},
	}, &out)
	if err != nil || reject != "" {
		return Draft{Resp: resp, Reject: reject}, err
	}
	text := strings.TrimSpace(out.Text)
	if text == "" {
		client.RecordRejected(ctx, Feature, client.RejectSchema)
		return Draft{Resp: resp, Reject: client.RejectSchema}, nil
	}
	if !f.Grounded(text) {
		client.RecordRejected(ctx, Feature, client.RejectGrounding)
		return Draft{Resp: resp, Reject: client.RejectGrounding}, nil
	}
	return Draft{Output: Output{Lines: []Line{{LineID: "l1", Text: text}}}, Resp: resp}, nil
}

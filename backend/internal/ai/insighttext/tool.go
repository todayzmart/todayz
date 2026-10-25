package insighttext

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/ai/schema"
	"github.com/todayz/backend/internal/ai/scrub"
	"github.com/todayz/backend/internal/insights/facts"
)

//go:embed prompt.md
var promptFS embed.FS

func init() { prompts.MustRegister(Feature, promptFS) }

const maxRef = 4

// Tool is the strict tool the model answers with.
var Tool = client.Tool{
	Name:        "write_insight_texts",
	Description: "Write one sentence for each Demand insights row.",
	Schema: schema.Object("", map[string]schema.S{
		"texts": schema.Array("One entry per row ref", schema.Object("", map[string]schema.S{
			"ref":  schema.String("The row's ref", maxRef),
			"text": schema.String("The row's sentence", MaxText),
		}), MaxRows),
	}),
}

// ToolText is one entry of the tool input.
type ToolText struct {
	Ref  string `json:"ref"`
	Text string `json:"text"`
}

// ToolOutput is the tool input.
type ToolOutput struct {
	Texts []ToolText `json:"texts"`
}

// Validate is the bounds the strict schema only describes.
func (o *ToolOutput) Validate() error {
	if len(o.Texts) > MaxRows {
		return fmt.Errorf("%d texts", len(o.Texts))
	}
	for i, t := range o.Texts {
		if t.Ref == "" || utf8.RuneCountInString(t.Ref) > maxRef {
			return fmt.Errorf("text %d: ref", i)
		}
		if utf8.RuneCountInString(t.Text) > MaxText {
			return fmt.Errorf("text %d: length", i)
		}
	}
	return nil
}

// Row is one row as the model reads it: no ids, money in rupees, the
// action's numbers under "action".
type Row struct {
	Ref   string         `json:"ref"`
	Kind  string         `json:"kind"`
	Facts map[string]any `json:"facts"`
}

// Input is the user message.
type Input struct {
	Language string `json:"language"`
	Rows     []Row  `json:"rows"`
}

// Readable is a row's facts as the model reads them (and as grounding checks
// them): keys naming ids are left out, a *Paise number becomes rupees
// ("valueAtCostPaise": 18240 → "valueAtCost": "182.40"), and the action
// payload's numbers join under "action".
func Readable(r facts.Row) map[string]any {
	out := readable(r.Facts)
	if a := readable(r.Action.Payload); len(a) > 0 {
		out["action"] = a
	}
	return out
}

func readable(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if idKey(k) {
			continue
		}
		if base, ok := strings.CutSuffix(k, "Paise"); ok {
			if p, ok := paise(v); ok {
				out[base] = rupees(p)
				continue
			}
		}
		if sub, ok := v.(map[string]any); ok {
			out[k] = readable(sub)
			continue
		}
		out[k] = v
	}
	return out
}

func idKey(k string) bool {
	return strings.HasSuffix(k, "Id") || strings.HasSuffix(k, "Ids") || k == "batches"
}

func paise(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case float64:
		return int64(n), n == float64(int64(n))
	}
	return 0, false
}

// rupees is paise as the model reads them: "120", or "99.50".
func rupees(p int64) string {
	sign := ""
	if p < 0 {
		sign, p = "-", -p
	}
	if p%100 == 0 {
		return sign + strconv.FormatInt(p/100, 10)
	}
	return fmt.Sprintf("%s%d.%02d", sign, p/100, p%100)
}

// Hash is the key a kept sentence is valid under: the row's kind, subject and
// readable facts, the language and the prompt version (a new prompt rewrites).
func Hash(r facts.Row, lang string) (string, error) {
	b, err := json.Marshal(struct {
		Kind    string         `json:"k"`
		Subject string         `json:"s"`
		Facts   map[string]any `json:"f"`
		Lang    string         `json:"l"`
		Prompt  string         `json:"p"`
	}{string(r.Kind), r.SubjectRef, Readable(r), lang, prompts.Version(Feature)})
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// Written is what one call made of the rows.
type Written struct {
	// Texts maps a row's ref to its grounded sentence.
	Texts map[string]string
	// Dropped counts the sentences that were ungrounded, unclean or empty.
	Dropped int
	Resp    client.Response
	Reject  string
}

// Write words rows in one call. ref is the store id (a case name in the
// eval). A provider error the request caused is returned as an error.
func Write(ctx context.Context, llm client.LLM, model, ref, lang string, rows []Row) (Written, error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Written{}, err
	}
	msg, err := json.Marshal(Input{Language: lang, Rows: rows})
	if err != nil {
		return Written{}, err
	}
	var out ToolOutput
	resp, reject, err := retail.Call(ctx, llm, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys, Tool: &Tool, MaxTokens: 2048,
		Messages: []client.Message{client.UserText(string(msg))},
	}, &out)
	if err != nil || reject != "" {
		return Written{Resp: resp, Reject: reject}, err
	}
	byRef := map[string]Row{}
	for _, r := range rows {
		byRef[r.Ref] = r
	}
	w := Written{Texts: map[string]string{}, Resp: resp}
	for _, t := range out.Texts {
		row, ok := byRef[t.Ref]
		if !ok {
			continue
		}
		if _, dup := w.Texts[t.Ref]; dup {
			continue
		}
		if !Grounded(t.Text, row) {
			w.Dropped++
			client.RecordRejected(ctx, Feature, client.RejectGrounding)
			continue
		}
		w.Texts[t.Ref] = strings.TrimSpace(t.Text)
	}
	return w, nil
}

// Grounded reports whether text is a usable sentence for row: not empty,
// every number from the row's readable facts (rule 6), and no phone, email
// or link the scrubber would remove.
func Grounded(text string, row Row) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	f, err := json.Marshal(row.Facts)
	if err != nil {
		return false
	}
	return len(schema.Ungrounded(text, string(f))) == 0 && scrub.Clean(text)
}

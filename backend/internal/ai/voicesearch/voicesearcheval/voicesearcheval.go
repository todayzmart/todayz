// Package voicesearcheval is voice search's eval suite (feature voice_search, W3-12 AI-C5):
// 100 recorded speech-to-text replies of spoken searches in English, Hindi, Kannada, Telugu,
// Tamil and mixed speech, each with the master-catalogue brands and the transcript, language
// and refine chips the customer should see. There is no model: a case runs the speech client
// against a loopback server that answers the recorded reply, then voicesearch.Clean and
// voicesearch.Read, the code the route uses, and passes only when all three are exact. The
// suite always replays (live speech evals are not part of W3-12) and writes nothing. Pass
// bar 90% (07 W3-12).
package voicesearcheval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/querynorm"
	"github.com/todayz/backend/internal/ai/voicesearch"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
)

// PassBar is the share of cases that must be exact.
const PassBar = 0.9

//go:embed fixtures/*.json
var fixturesFS embed.FS

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	eval.Register(eval.Suite{Feature: voicesearch.Feature, PassBar: PassBar, Fixtures: fx, Run: run})
}

type input struct {
	Language string            `json:"language"` // the customer's pick (en-IN, …) or ""
	Brands   []querynorm.Brand `json:"brands"`
}

// Chip is one expected chip; value is "" for the kinds without one.
type Chip struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Text  string `json:"text"`
}

type expect struct {
	Transcript string `json:"transcript"`
	Language   string `json:"language"`
	Chips      []Chip `json:"chips"`
}

func run(ctx context.Context, _ eval.Env, c eval.Case) (eval.Verdict, error) {
	var in input
	var want expect
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return eval.Verdict{}, err
	}
	if err := json.Unmarshal(c.Expect, &want); err != nil {
		return eval.Verdict{}, err
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/speech-to-text" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(c.Reply)
	}))
	defer srv.Close()
	stt := client.NewSarvam(srv.URL, config.Secret{}, httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true}))
	tr, err := stt.Transcribe(ctx, client.Audio{Data: []byte("recorded"), MediaType: "audio/webm", Filename: voicesearch.Feature + ".webm", Language: in.Language})
	if err != nil {
		return eval.Verdict{}, err
	}
	text := voicesearch.Clean(tr.Text)
	res := voicesearch.Read(text, tr.Language, in.Language, NamedIn(text, in.Brands))
	if res.Transcript != want.Transcript {
		return eval.Verdict{Note: "the transcript differs"}, nil
	}
	if res.Language != want.Language {
		return eval.Verdict{Note: fmt.Sprintf("language %q, want %q", res.Language, want.Language)}, nil
	}
	if len(res.Chips) != len(want.Chips) {
		return eval.Verdict{Note: fmt.Sprintf("%d chips, want %d", len(res.Chips), len(want.Chips))}, nil
	}
	for i, ch := range res.Chips {
		if ch.Kind != want.Chips[i].Kind || ch.Value != want.Chips[i].Value || ch.Text != want.Chips[i].Text {
			return eval.Verdict{Note: fmt.Sprintf("chip %d differs", i+1)}, nil
		}
	}
	return eval.Verdict{Pass: true}, nil
}

// NamedIn is the brands whose name is in text, longest first: what the aiconsumer
// BrandsNamedIn query answers from the master catalogue.
func NamedIn(text string, all []querynorm.Brand) []querynorm.Brand {
	low := strings.ToLower(text)
	var out []querynorm.Brand
	for _, b := range all {
		if len([]rune(b.Name)) >= 2 && strings.Contains(low, strings.ToLower(b.Name)) {
			out = append(out, b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := len([]rune(out[i].Name)), len([]rune(out[j].Name)); a != b {
			return a > b
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

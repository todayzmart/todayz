package fixtures

import (
	"strings"
	"testing"
	"testing/fstest"
)

const textResp = `"response":{"content":[{"type":"text","text":"ok"}]}`

func fx(match string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(`{"match":` + match + `,` + textResp + `}`)}
}

func TestMatchPrecedence(t *testing.T) {
	fsys := fstest.MapFS{
		// Folder name supplies the feature when match.feature is empty.
		"llm/voice_stock/a-generic.json":     fx(`{}`),
		"llm/voice_stock/b-atta.json":        fx(`{"contains":["atta"]}`),
		"llm/voice_stock/c-atta-tool.json":   fx(`{"contains":["atta"],"tool":"record_stock_lines"}`),
		"llm/voice_stock/d-atta-doodh.json":  fx(`{"contains":["atta","doodh"]}`),
		"llm/voice_stock/e-tie-later.json":   fx(`{"contains":["ATTA","doodh"]}`),
		"llm/coach_card/default.json":        fx(`{}`),
		"llm/_recorded/voice_stock-123.json": fx(`{"feature":"voice_stock","contains":["exact recorded prompt","x","y","z"]}`),
	}
	set, err := LoadLLM(fsys, LLMOptions{IncludeRecorded: true})
	if err != nil || len(set.Errors) != 0 {
		t.Fatal(err, set.Errors)
	}
	all := set.Fixtures
	tools := []string{"record_stock_lines"}
	cases := []struct {
		name string
		req  LLMRequest
		want string
	}{
		{"generic only", LLMRequest{Feature: "voice_stock", Text: "kuch nahi"}, "llm/voice_stock/a-generic.json"},
		{"one substring", LLMRequest{Feature: "voice_stock", Text: "do kilo Atta"}, "llm/voice_stock/b-atta.json"},
		{"tool counts as a rule; tie broken by path", LLMRequest{Feature: "voice_stock", Text: "do kilo atta aur doodh", Tools: tools}, "llm/voice_stock/c-atta-tool.json"},
		{"two substrings beat one", LLMRequest{Feature: "voice_stock", Text: "atta aur doodh"}, "llm/voice_stock/d-atta-doodh.json"},
		{"tool rule needs the tool", LLMRequest{Feature: "voice_stock", Text: "atta", Tools: []string{"other"}}, "llm/voice_stock/b-atta.json"},
		{"recorded fixture is most specific when it matches", LLMRequest{Feature: "voice_stock", Text: "exact recorded prompt x y z atta"}, "llm/_recorded/voice_stock-123.json"},
		{"other feature", LLMRequest{Feature: "coach_card", Text: "atta"}, "llm/coach_card/default.json"},
		{"unknown feature", LLMRequest{Feature: "nope", Text: "atta"}, ""},
		{"no feature", LLMRequest{Text: "atta"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MatchLLM(all, c.req)
			gotPath := ""
			if got != nil {
				gotPath = got.Path
			}
			if gotPath != c.want {
				t.Fatalf("got %q, want %q", gotPath, c.want)
			}
		})
	}
	// Deterministic: same answer on every call.
	for i := 0; i < 20; i++ {
		if got := MatchLLM(all, cases[2].req); got.Path != cases[2].want {
			t.Fatalf("run %d: got %s", i, got.Path)
		}
	}
}

func TestLoadLLMRejectsBadFixtures(t *testing.T) {
	bad := map[string]string{
		"no feature at top level":  `{"match":{},` + textResp + `}`,
		"both response and error":  `{"match":{"feature":"x"},` + textResp + `,"error":{"status":529,"type":"overloaded_error","message":"m"}}`,
		"neither":                  `{"match":{"feature":"x"}}`,
		"unknown field":            `{"match":{"feature":"x"},"respons":{}}`,
		"bad block":                `{"match":{"feature":"x"},"response":{"content":[{"type":"image"}]}}`,
		"tool_use without input":   `{"match":{"feature":"x"},"response":{"content":[{"type":"tool_use","name":"t"}]}}`,
		"bad stop reason":          `{"match":{"feature":"x"},"response":{"content":[{"type":"text","text":"a"}],"stop_reason":"done"}}`,
		"error status below 400":   `{"match":{"feature":"x"},"error":{"status":200,"type":"api_error"}}`,
		"feature with bad letters": `{"match":{"feature":"Voice-Stock"},` + textResp + `}`,
	}
	bad["stream error with an error fixture"] = `{"match":{"feature":"x"},"error":{"status":529,"type":"overloaded_error","message":"m"},"stream_error_after":2}`
	bad["thinking without text"] = `{"match":{"feature":"x"},"response":{"content":[{"type":"thinking"}]}}`
	bad["trailing data"] = `{"match":{"feature":"x"},` + textResp + `} {}`
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			p := "llm/f.json"
			if name == "no feature at top level" {
				p = "llm/top.json"
			}
			set, err := LoadLLM(fstest.MapFS{p: {Data: []byte(body)}, "llm/good/ok.json": fx(`{}`)}, LLMOptions{})
			if err != nil {
				t.Fatalf("a bad file must not fail the whole load: %v", err)
			}
			if len(set.Errors) != 1 || set.Errors[0].Path != p || !strings.Contains(set.Errors[0].Error(), p) {
				t.Fatalf("errors = %v", set.Errors)
			}
			// The good file still loads.
			if len(set.Fixtures) != 1 || set.Fixtures[0].Path != "llm/good/ok.json" {
				t.Fatalf("fixtures = %v", set.Fixtures)
			}
		})
	}
}

// A broken file is attributed to the feature it would have served: its
// match.feature when that decoded, else its folder, else unknown ("").
func TestLoadLLMAttributesBrokenFiles(t *testing.T) {
	set, err := LoadLLM(fstest.MapFS{
		"llm/voice_stock/bad-json.json":    {Data: []byte(`{not json`)},
		"llm/coach_card/bad-block.json":    {Data: []byte(`{"match":{},"response":{"content":[{"type":"image"}]}}`)},
		"llm/coach_card/other-feat.json":   {Data: []byte(`{"match":{"feature":"order_help"},"response":{"content":[]}}`)},
		"llm/top-level.json":               {Data: []byte(`nope`)},
		"llm/voice_stock/good.json":        fx(`{}`),
		"llm/_recorded/voice_stock-x.json": {Data: []byte(`nope`)},
	}, LLMOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range set.Errors {
		got[e.Path] = e.Feature
	}
	want := map[string]string{
		"llm/voice_stock/bad-json.json":  "voice_stock",
		"llm/coach_card/bad-block.json":  "coach_card",
		"llm/coach_card/other-feat.json": "order_help",
		"llm/top-level.json":             "",
	}
	if len(got) != len(want) {
		t.Fatalf("errors = %v (the _recorded file must be skipped, not reported)", got)
	}
	for p, f := range want {
		if g, ok := got[p]; !ok || g != f {
			t.Errorf("%s: feature %q (present %v), want %q", p, g, ok, f)
		}
	}
	if len(set.BrokenFor("voice_stock")) != 1 || len(set.BrokenFor("")) != 1 || len(set.BrokenFor("nope")) != 0 {
		t.Errorf("BrokenFor wrong: %v", set.Errors)
	}
	if len(set.Fixtures) != 1 {
		t.Errorf("fixtures = %v", set.Fixtures)
	}
}

// Recordings live in llm/_recorded/ and are ignored unless replay is asked
// for, so a recording can never change a curated fixture's answer.
func TestLoadLLMSkipsUnderscoreFolders(t *testing.T) {
	fsys := fstest.MapFS{
		"llm/voice_stock/default.json":     fx(`{}`),
		"llm/_recorded/voice_stock-1.json": fx(`{"feature":"voice_stock","contains":["atta"],"tool":"record_stock_lines"}`),
		"llm/_drafts/x.json":               {Data: []byte(`broken`)},
	}
	req := LLMRequest{Feature: "voice_stock", Text: "do kilo atta", Tools: []string{"record_stock_lines"}}

	off, err := LoadLLM(fsys, LLMOptions{})
	if err != nil || len(off.Errors) != 0 || len(off.Fixtures) != 1 {
		t.Fatalf("default load: %v %v %v", err, off.Errors, off.Fixtures)
	}
	if got := MatchLLM(off.Fixtures, req); got == nil || got.Path != "llm/voice_stock/default.json" {
		t.Fatalf("default: got %v, want the curated fixture", got)
	}

	on, err := LoadLLM(fsys, LLMOptions{IncludeRecorded: true})
	if err != nil || len(on.Fixtures) != 2 || len(on.Errors) != 1 {
		t.Fatalf("replay load: %v %v %v", err, on.Errors, on.Fixtures)
	}
	if got := MatchLLM(on.Fixtures, req); got == nil || got.Path != "llm/_recorded/voice_stock-1.json" {
		t.Fatalf("replay: got %v", got)
	}
}

func TestLoadLLMMissingDir(t *testing.T) {
	set, err := LoadLLM(fstest.MapFS{}, LLMOptions{})
	if err != nil || len(set.Fixtures) != 0 || len(set.Errors) != 0 {
		t.Fatalf("got %v %v", set, err)
	}
}

func TestLoadSTT(t *testing.T) {
	h := strings.Repeat("ab", 32)
	fsys := fstest.MapFS{
		"stt/a.json": {Data: []byte(`{"sha256":"` + strings.ToUpper(h) + `","language_code":"hi-IN","transcript":"t"}`)},
	}
	m, bad, err := LoadSTT(fsys)
	if err != nil || len(bad) != 0 {
		t.Fatal(err, bad)
	}
	if m[h].Transcript != "t" || m[h].Path != "stt/a.json" {
		t.Fatalf("got %+v", m)
	}
	// A duplicate hash and a bad file are skipped and reported; a.json still loads.
	fsys["stt/b.json"] = &fstest.MapFile{Data: []byte(`{"sha256":"` + h + `","language_code":"hi-IN","transcript":"u"}`)}
	fsys["stt/c.json"] = &fstest.MapFile{Data: []byte(`{"sha256":"xyz"}`)}
	m, bad, err = LoadSTT(fsys)
	if err != nil || len(bad) != 2 || m[h].Transcript != "t" {
		t.Fatalf("got %v %v %v", m, bad, err)
	}
	if bad[0].Path != "stt/b.json" || !strings.Contains(bad[0].Error(), "same sha256") || bad[1].Path != "stt/c.json" {
		t.Fatalf("bad = %v", bad)
	}
}

func TestLoadTemplates(t *testing.T) {
	m, err := LoadTemplates(fstest.MapFS{}, "sms/templates.json")
	if err != nil || len(m) != 0 {
		t.Fatalf("missing file: %v %v", m, err)
	}
	m, err = LoadTemplates(fstest.MapFS{"sms/templates.json": {Data: []byte(`{"a":"Hi ##NAME##"}`)}}, "sms/templates.json")
	if err != nil || m["a"] != "Hi ##NAME##" {
		t.Fatalf("got %v %v", m, err)
	}
}

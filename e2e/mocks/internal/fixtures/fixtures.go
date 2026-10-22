// Package fixtures loads the mocks' recorded answers: LLM responses, STT
// transcripts and message templates. Fixtures are read from an fs.FS on every
// call, so editing a file under e2e/mocks/fixtures takes effect without a
// restart when the server runs from the directory.
//
// One bad file never breaks the others: loaders skip it and report it as a
// FileError, which the server lists on /healthz.
package fixtures

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// LLMMatch selects the requests a fixture answers.
type LLMMatch struct {
	// Feature must equal the <feature> in metadata.user_id "tz:<feature>:...".
	// When empty it defaults to the fixture's folder name (llm/<feature>/x.json).
	Feature string `json:"feature"`
	// Contains lists substrings that must all appear (case-insensitive) in the
	// request text: system prompt, message text, tool results and tool inputs.
	Contains []string `json:"contains,omitempty"`
	// Tool, when set, must be the name of one of the request's tools.
	Tool string `json:"tool,omitempty"`
}

// LLMError makes a fixture answer with an Anthropic error instead of a message.
type LLMError struct {
	Status  int    `json:"status"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// LLMResponse is the part of a Messages API response a fixture provides. The
// mock fills id, type, role and model.
type LLMResponse struct {
	Content      []map[string]any `json:"content"`
	StopReason   string           `json:"stop_reason,omitempty"`
	StopSequence *string          `json:"stop_sequence,omitempty"`
	Usage        map[string]any   `json:"usage,omitempty"`
}

// LLMFixture is one file under fixtures/llm.
type LLMFixture struct {
	Path        string       `json:"-"` // e.g. llm/voice_stock/hinglish-sale.json
	Description string       `json:"description,omitempty"`
	Match       LLMMatch     `json:"match"`
	Response    *LLMResponse `json:"response,omitempty"`
	Error       *LLMError    `json:"error,omitempty"`
	// StreamErrorAfter, when > 0, makes a streaming request fail mid-stream:
	// after that many SSE events the mock sends "event: error"
	// (overloaded_error) and ends the stream. Non-streaming requests ignore it.
	StreamErrorAfter int `json:"stream_error_after,omitempty"`
}

// Specificity ranks fixtures: each contains rule and a tool rule count one.
func (f *LLMFixture) Specificity() int {
	n := len(f.Match.Contains)
	if f.Match.Tool != "" {
		n++
	}
	return n
}

var featureRe = regexp.MustCompile(`^[a-z0-9_]+$`)

// FileError is a fixture file that failed to load.
type FileError struct {
	Path string
	// Feature is the LLM feature the file would have served, "" when unknown.
	Feature string
	Err     error
}

func (e FileError) Error() string { return e.Path + ": " + e.Err.Error() }

// LLMOptions controls LoadLLM.
type LLMOptions struct {
	// IncludeRecorded also loads folders whose name starts with "_" (such as
	// llm/_recorded/). Off by default, so a recording never changes what a
	// curated fixture answers. MOCK_LLM_REPLAY_RECORDED=1 turns it on.
	IncludeRecorded bool
}

// LLMSet is the loaded LLM fixtures plus the files that failed.
type LLMSet struct {
	Fixtures []LLMFixture // sorted by path
	Errors   []FileError  // sorted by path
}

// BrokenFor returns the failed files that belong to feature. With feature ""
// it returns the failed files whose feature is unknown.
func (s LLMSet) BrokenFor(feature string) []FileError {
	var out []FileError
	for _, e := range s.Errors {
		if e.Feature == feature {
			out = append(out, e)
		}
	}
	return out
}

// LoadLLM reads every *.json under llm/ (recursively), sorted by path. Folders
// starting with "_" are skipped unless opt.IncludeRecorded. A file that does
// not parse or validate is skipped and listed in Errors; the error return is
// only for a tree that cannot be walked.
func LoadLLM(fsys fs.FS, opt LLMOptions) (LLMSet, error) {
	var set LLMSet
	err := fs.WalkDir(fsys, "llm", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == "llm" {
				return fs.SkipDir
			}
			return err
		}
		if d.IsDir() {
			if p != "llm" && strings.HasPrefix(d.Name(), "_") && !opt.IncludeRecorded {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".json") {
			return nil
		}
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			set.Errors = append(set.Errors, FileError{Path: p, Feature: folderFeature(p), Err: err})
			return nil
		}
		f, err := ParseLLM(p, raw)
		if err != nil {
			feature := f.Match.Feature
			if !featureRe.MatchString(feature) {
				feature = folderFeature(p)
			}
			set.Errors = append(set.Errors, FileError{Path: p, Feature: feature, Err: err})
			return nil
		}
		set.Fixtures = append(set.Fixtures, f)
		return nil
	})
	if err != nil {
		return LLMSet{}, err
	}
	sort.Slice(set.Fixtures, func(i, j int) bool { return set.Fixtures[i].Path < set.Fixtures[j].Path })
	sort.Slice(set.Errors, func(i, j int) bool { return set.Errors[i].Path < set.Errors[j].Path })
	return set, nil
}

// folderFeature is the feature named by the file's folder (llm/<feature>/x.json),
// or "" for a file directly under llm/ or in a "_" folder.
func folderFeature(p string) string {
	dir := path.Base(path.Dir(p))
	if dir == "llm" || strings.HasPrefix(dir, "_") || !featureRe.MatchString(dir) {
		return ""
	}
	return dir
}

// ParseLLM decodes and validates one LLM fixture file. The recorder uses it
// too, so a recording is written only when it would load. On error the
// returned fixture holds whatever decoded (Match.Feature may be set).
func ParseLLM(p string, raw []byte) (LLMFixture, error) {
	var f LLMFixture
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return f, err
	}
	if dec.More() {
		return f, errors.New("trailing data after the JSON object")
	}
	f.Path = p
	if f.Match.Feature == "" {
		f.Match.Feature = folderFeature(p)
	}
	return f, f.Validate()
}

// Validate checks a fixture's shape.
func (f *LLMFixture) Validate() error {
	if !featureRe.MatchString(f.Match.Feature) {
		return fmt.Errorf("match.feature %q must be set and match %s", f.Match.Feature, featureRe)
	}
	if (f.Response == nil) == (f.Error == nil) {
		return errors.New("exactly one of response or error is required")
	}
	if f.StreamErrorAfter < 0 {
		return errors.New("stream_error_after must be >= 0")
	}
	if f.Error != nil {
		if f.Error.Status < 400 || f.Error.Type == "" {
			return errors.New("error needs a status >= 400 and a type")
		}
		if f.StreamErrorAfter > 0 {
			return errors.New("stream_error_after needs a response, not an error")
		}
		return nil
	}
	if len(f.Response.Content) == 0 {
		return errors.New("response.content must not be empty")
	}
	for i, b := range f.Response.Content {
		switch b["type"] {
		case "text":
			if _, ok := b["text"].(string); !ok {
				return fmt.Errorf("content[%d]: text block needs a text string", i)
			}
		case "tool_use":
			if name, _ := b["name"].(string); name == "" {
				return fmt.Errorf("content[%d]: tool_use block needs a name", i)
			}
			if _, ok := b["input"].(map[string]any); !ok {
				return fmt.Errorf("content[%d]: tool_use block needs an input object", i)
			}
		case "thinking":
			if _, ok := b["thinking"].(string); !ok {
				return fmt.Errorf("content[%d]: thinking block needs a thinking string", i)
			}
		case "redacted_thinking":
		default:
			return fmt.Errorf("content[%d]: unsupported block type %v", i, b["type"])
		}
	}
	switch f.Response.StopReason {
	case "", "end_turn", "max_tokens", "stop_sequence", "tool_use", "pause_turn", "refusal":
	default:
		return fmt.Errorf("unknown stop_reason %q", f.Response.StopReason)
	}
	return nil
}

// LLMRequest is what matching looks at.
type LLMRequest struct {
	Feature string
	Text    string // all request text; compared case-insensitively
	Tools   []string
}

// MatchLLM returns the most specific fixture for req, or nil. Fixtures must be
// sorted by path (LoadLLM does this); among equally specific fixtures the
// first path wins, so the choice is deterministic.
func MatchLLM(all []LLMFixture, req LLMRequest) *LLMFixture {
	text := strings.ToLower(req.Text)
	var best *LLMFixture
	for i := range all {
		f := &all[i]
		if f.Match.Feature != req.Feature {
			continue
		}
		if f.Match.Tool != "" && !contains(req.Tools, f.Match.Tool) {
			continue
		}
		ok := true
		for _, s := range f.Match.Contains {
			if !strings.Contains(text, strings.ToLower(s)) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if best == nil || f.Specificity() > best.Specificity() {
			best = f
		}
	}
	return best
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// STTFixture maps one audio file (by sha256 of its bytes) to a transcript.
type STTFixture struct {
	Path         string `json:"-"`
	SHA256       string `json:"sha256"`
	LanguageCode string `json:"language_code"`
	Transcript   string `json:"transcript"`
	// Translation is returned by speech-to-text-translate; Transcript is used when empty.
	Translation string `json:"translation,omitempty"`
}

var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// LoadSTT reads stt/*.json keyed by lower-case sha256. A file that does not
// parse, or repeats an earlier file's sha256, is skipped and listed in the
// returned errors; the error return is only for a bad glob.
func LoadSTT(fsys fs.FS) (map[string]STTFixture, []FileError, error) {
	out := map[string]STTFixture{}
	var bad []FileError
	files, err := fs.Glob(fsys, "stt/*.json")
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)
	for _, p := range files {
		f, err := parseSTT(fsys, p)
		if err == nil {
			if prev, dup := out[f.SHA256]; dup {
				err = fmt.Errorf("same sha256 as %s", prev.Path)
			}
		}
		if err != nil {
			bad = append(bad, FileError{Path: p, Err: err})
			continue
		}
		out[f.SHA256] = f
	}
	return out, bad, nil
}

func parseSTT(fsys fs.FS, p string) (STTFixture, error) {
	var f STTFixture
	raw, err := fs.ReadFile(fsys, p)
	if err != nil {
		return f, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return f, err
	}
	f.Path = p
	f.SHA256 = strings.ToLower(f.SHA256)
	if !sha256Re.MatchString(f.SHA256) {
		return f, errors.New("sha256 must be 64 hex characters")
	}
	return f, nil
}

// LoadTemplates reads a JSON object of template id -> text. A missing file is
// an empty map.
func LoadTemplates(fsys fs.FS, name string) (map[string]string, error) {
	raw, err := fs.ReadFile(fsys, name)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

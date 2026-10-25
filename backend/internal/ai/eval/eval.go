// Package eval runs a feature's evaluation (11 §1 rule 8): a fixture set with a pass bar, run
// against recorded model replies in make check (no network, no key) and against the live
// model only under make ai-eval with the user's own key in .env.local (D-17).
//
// A feature keeps its fixtures in backend/internal/ai/<feature>/eval/ (embed.FS) and registers
// a Suite from that package's init; `todayz ai eval` imports the packages that register.
// A fixture is one JSON file:
//
//	{
//	  "name":   "hi_two_items",        // [a-z0-9_]+, unique in the suite
//	  "input":  {...},                 // the feature's own input shape
//	  "expect": {...},                 // the feature's own expected result
//	  "reply":  {...}                  // the recorded Messages API reply (recorded mode)
//	}
//
// In recorded mode the suite talks to the real Anthropic client, pointed at a loopback server
// that answers each case with its recorded reply, so the request building, reply parsing,
// schema and grounding checks all run exactly as in production. A prompt change must not drop
// the score: a recorded reply that no longer fits the prompt is re-recorded live.
package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/platform/settings"
)

// Case is one fixture.
type Case struct {
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input"`
	Expect json.RawMessage `json:"expect"`
	Reply  json.RawMessage `json:"reply"`
}

// Verdict is one case's result.
type Verdict struct {
	Pass bool
	Note string // why it failed: short, no model text beyond what the fixture already holds
}

// Suite is a feature's evaluation.
type Suite struct {
	Feature string
	// PassBar is the share of cases that must pass (0 < PassBar ≤ 1), from the feature's row
	// in 07-work-packages.md.
	PassBar float64
	// Fixtures holds the *.json cases at its root.
	Fixtures fs.FS
	// Run calls the feature with the case's input through env.LLM and judges the result. An
	// error is a broken harness, not a failed case.
	Run func(ctx context.Context, env Env, c Case) (Verdict, error)
}

// Env is what a suite calls the model with. Model gives the model for a settings key
// (ai_model_fast, ai_model_smart): the platform_settings value live, a placeholder when
// replaying (the recorded reply names the model it came from).
type Env struct {
	LLM   client.LLM
	Model func(settings.Key) string
}

// Report is one suite run.
type Report struct {
	Feature    string        `json:"feature"`
	Mode       string        `json:"mode"` // recorded or live
	Cases      int           `json:"cases"`
	Passed     int           `json:"passed"`
	Score      float64       `json:"score"`
	PassBar    float64       `json:"passBar"`
	OK         bool          `json:"ok"`
	CostPaise  int64         `json:"costPaise"`
	LatencyP95 time.Duration `json:"latencyP95"`
	Failures   []Failure     `json:"failures,omitempty"`
}

// Failure is a failed case.
type Failure struct {
	Case string `json:"case"`
	Note string `json:"note"`
}

var (
	mu     sync.Mutex
	suites = map[string]Suite{}
	reName = regexp.MustCompile(`^[a-z0-9_]{1,60}$`)
)

// Register adds a suite; call it from the feature package's init.
func Register(s Suite) {
	if !client.KnownFeature(s.Feature) || s.Run == nil || s.Fixtures == nil || s.PassBar <= 0 || s.PassBar > 1 {
		panic("eval: bad suite " + s.Feature)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := suites[s.Feature]; dup {
		panic("eval: suite registered twice: " + s.Feature)
	}
	suites[s.Feature] = s
}

// Get returns a registered suite.
func Get(feature string) (Suite, bool) {
	mu.Lock()
	defer mu.Unlock()
	s, ok := suites[feature]
	return s, ok
}

// Features lists the registered suites in name order.
func Features() []string {
	mu.Lock()
	defer mu.Unlock()
	out := make([]string, 0, len(suites))
	for f := range suites {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// LoadCases reads and checks a suite's fixtures. recorded requires every case to carry a reply.
func LoadCases(s Suite, recorded bool) ([]Case, error) {
	files, err := fs.Glob(s.Fixtures, "*.json")
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("eval: %s has no fixtures", s.Feature)
	}
	seen := map[string]bool{}
	out := make([]Case, 0, len(files))
	for _, f := range files {
		b, err := fs.ReadFile(s.Fixtures, f)
		if err != nil {
			return nil, err
		}
		var c Case
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("eval: %s/%s: %w", s.Feature, f, err)
		}
		if !reName.MatchString(c.Name) || seen[c.Name] {
			return nil, fmt.Errorf("eval: %s/%s: name %q missing, malformed or repeated", s.Feature, f, c.Name)
		}
		if recorded && len(c.Reply) == 0 {
			return nil, fmt.Errorf("eval: %s/%s: no recorded reply", s.Feature, f)
		}
		seen[c.Name] = true
		out = append(out, c)
	}
	return out, nil
}

// Run runs a suite through llm. Each call's Ref is set to the case name (the replay server
// finds the recorded reply by it; live, it tags the provider's metadata).
func Run(ctx context.Context, s Suite, cases []Case, env Env, mode string) (Report, error) {
	r := Report{Feature: s.Feature, Mode: mode, Cases: len(cases), PassBar: s.PassBar}
	var lat []time.Duration
	for _, c := range cases {
		tl := &tally{next: env.LLM, ref: c.Name}
		v, err := s.Run(ctx, Env{LLM: tl, Model: env.Model}, c)
		if err != nil {
			return r, fmt.Errorf("eval: %s/%s: %w", s.Feature, c.Name, err)
		}
		r.CostPaise += tl.cost
		lat = append(lat, tl.latency...)
		if v.Pass {
			r.Passed++
		} else {
			r.Failures = append(r.Failures, Failure{Case: c.Name, Note: v.Note})
		}
	}
	if r.Cases > 0 {
		r.Score = float64(r.Passed) / float64(r.Cases)
	}
	r.OK = r.Cases > 0 && r.Score >= s.PassBar
	r.LatencyP95 = p95(lat)
	return r, nil
}

// ErrBelowBar is a run under its suite's pass bar.
var ErrBelowBar = errors.New("eval: score below the pass bar")

type tally struct {
	next    client.LLM
	ref     string
	cost    int64
	latency []time.Duration
}

func (t *tally) Complete(ctx context.Context, req client.Request) (client.Response, error) {
	req.Ref = t.ref
	resp, err := t.next.Complete(ctx, req)
	t.cost += resp.CostPaise
	if resp.Latency > 0 {
		t.latency = append(t.latency, resp.Latency)
	}
	return resp, err
}

func p95(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := (len(s)*95+99)/100 - 1
	return s[max(i, 0)]
}

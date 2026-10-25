// Package prompts is the prompt registry (11 §1 rule 8, §5). A feature keeps its prompts as
// files in its own package (embed.FS) and registers them here; a draft records the version
// it was made with (ai_drafts.prompt_version), so an eval score and a confirm rate can be read
// per prompt version on dashboard 14.
//
// A prompt file is text/template source with a header:
//
//	version: 3
//	---
//	You read a shop owner's spoken stock update ... {{.Lang}} ...
//
// The version is the header number plus the first 8 hex digits of the body's SHA-256
// ("3-1a2b3c4d"), so an edit that forgets to bump the number still shows up as a new version.
// Rendering fails on a missing field (missingkey=error); user text is passed as data, never
// spliced into the template source.
package prompts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"
	"text/template"
)

// Prompt is one parsed prompt.
type Prompt struct {
	Name    string // "<feature>/<file name without .md>", e.g. voice_stock/system
	Version string
	tmpl    *template.Template
}

// Render executes the prompt with data.
func (p *Prompt) Render(data any) (string, error) {
	var b bytes.Buffer
	if err := p.tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("prompts: render %s: %w", p.Name, err)
	}
	return strings.TrimSpace(b.String()), nil
}

// ErrFormat is a prompt file without a valid header.
var ErrFormat = errors.New("prompts: bad prompt file")

var (
	reHeader  = regexp.MustCompile(`\Aversion:[ \t]*([1-9][0-9]{0,5})[ \t]*\r?\n---[ \t]*\r?\n`)
	reFeature = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
)

// Parse reads one prompt file's source.
func Parse(name string, src []byte) (*Prompt, error) {
	m := reHeader.FindSubmatchIndex(src)
	if m == nil {
		return nil, fmt.Errorf("%w: %s: the first lines must be `version: N` and `---`", ErrFormat, name)
	}
	body := src[m[1]:]
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("%w: %s: empty body", ErrFormat, name)
	}
	t, err := template.New(name).Option("missingkey=error").Parse(string(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrFormat, name, err)
	}
	sum := sha256.Sum256(body)
	return &Prompt{Name: name, Version: string(src[m[2]:m[3]]) + "-" + hex.EncodeToString(sum[:4]), tmpl: t}, nil
}

var (
	mu       sync.RWMutex
	registry = map[string]*Prompt{}
)

// MustRegister parses every *.md file at the root of fsys as feature's prompts. Call it from
// a package-level var or init in the feature package; a bad file panics at start-up (and in
// that package's tests), never at the first request.
func MustRegister(feature string, fsys fs.FS) {
	if !reFeature.MatchString(feature) {
		panic("prompts: bad feature name " + feature)
	}
	files, err := fs.Glob(fsys, "*.md")
	if err != nil || len(files) == 0 {
		panic(fmt.Sprintf("prompts: %s has no prompt files", feature))
	}
	mu.Lock()
	defer mu.Unlock()
	for _, f := range files {
		src, err := fs.ReadFile(fsys, f)
		if err != nil {
			panic(err)
		}
		name := feature + "/" + strings.TrimSuffix(f, ".md")
		p, err := Parse(name, src)
		if err != nil {
			panic(err)
		}
		if _, dup := registry[name]; dup {
			panic("prompts: registered twice: " + name)
		}
		registry[name] = p
	}
}

// Get returns a registered prompt ("<feature>/<name>").
func Get(name string) (*Prompt, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[name]
	return p, ok
}

// MustGet is Get for a name the code knows exists.
func MustGet(name string) *Prompt {
	p, ok := Get(name)
	if !ok {
		panic("prompts: not registered: " + name)
	}
	return p
}

// Version is the draft's prompt_version for a feature: the versions of all its prompts in name
// order, joined with "+" (one prompt is the common case).
func Version(feature string) string {
	mu.RLock()
	defer mu.RUnlock()
	var names []string
	for n := range registry {
		if strings.HasPrefix(n, feature+"/") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	vs := make([]string, len(names))
	for i, n := range names {
		vs[i] = registry[n].Version
	}
	return strings.Join(vs, "+")
}

// Package registry loads and validates the problem code registry: the files
// backend/api/problems/<domain>.yaml (02 §2.6, D-24). It has no dependencies
// on the rest of the backend, so the code generator and the runtime share it.
package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Entry is one registered problem code.
type Entry struct {
	Code   string `yaml:"code"`
	Status int    `yaml:"status"`
	I18n   string `yaml:"i18n"`
	Title  string `yaml:"title"`
	Domain string `yaml:"-"`
}

// File is one <domain>.yaml.
type File struct {
	Domain   string  `yaml:"domain"`
	Problems []Entry `yaml:"problems"`
}

// Registry is the merged set of codes.
type Registry struct {
	byCode map[string]Entry
}

var (
	reCode   = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	reDomain = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	reI18n   = regexp.MustCompile(`^problems\.[a-z][a-z0-9_]*$`)
)

// Load reads every *.yaml file at the root of fsys.
func Load(fsys fs.FS) (*Registry, error) {
	names, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, errors.New("problems: no <domain>.yaml files")
	}
	files := make([]File, 0, len(names))
	for _, n := range names {
		b, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, err
		}
		f, err := Parse(n, b)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return Merge(files...)
}

// Parse reads and checks one file. name is the file name (<domain>.yaml).
func Parse(name string, b []byte) (File, error) {
	var f File
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return f, fmt.Errorf("problems/%s: %w", name, err)
	}
	want := strings.TrimSuffix(path.Base(name), ".yaml")
	if f.Domain != want {
		return f, fmt.Errorf("problems/%s: domain %q must match the file name %q", name, f.Domain, want)
	}
	if !reDomain.MatchString(f.Domain) {
		return f, fmt.Errorf("problems/%s: bad domain %q", name, f.Domain)
	}
	if len(f.Problems) == 0 {
		return f, fmt.Errorf("problems/%s: no problems listed", name)
	}
	var errs []error
	for i := range f.Problems {
		e := &f.Problems[i]
		e.Domain = f.Domain
		switch {
		case !reCode.MatchString(e.Code):
			errs = append(errs, fmt.Errorf("problems/%s: code %q is not snake_case", name, e.Code))
		case e.Status < 400 || e.Status > 599 || http.StatusText(e.Status) == "":
			errs = append(errs, fmt.Errorf("problems/%s: %s: status %d is not an HTTP error status", name, e.Code, e.Status))
		case e.I18n != "problems."+e.Code || !reI18n.MatchString(e.I18n):
			errs = append(errs, fmt.Errorf("problems/%s: %s: i18n key must be %q", name, e.Code, "problems."+e.Code))
		case strings.TrimSpace(e.Title) == "":
			errs = append(errs, fmt.Errorf("problems/%s: %s: title is empty", name, e.Code))
		}
	}
	return f, errors.Join(errs...)
}

// Merge combines files; a code may be registered only once, and
// invalid_transition only in common (a refused state change always uses it).
func Merge(files ...File) (*Registry, error) {
	r := &Registry{byCode: map[string]Entry{}}
	var errs []error
	for _, f := range files {
		for _, e := range f.Problems {
			if prev, dup := r.byCode[e.Code]; dup {
				errs = append(errs, fmt.Errorf("problems: code %q is in both %s.yaml and %s.yaml", e.Code, prev.Domain, e.Domain))
				continue
			}
			r.byCode[e.Code] = e
		}
	}
	for _, must := range []string{"internal", "invalid_transition", "not_found", "validation_failed", "idempotency_mismatch"} {
		if e, ok := r.byCode[must]; !ok || e.Domain != "common" {
			errs = append(errs, fmt.Errorf("problems: common.yaml must register %q", must))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return r, nil
}

// Lookup returns the entry for code.
func (r *Registry) Lookup(code string) (Entry, bool) {
	e, ok := r.byCode[code]
	return e, ok
}

// Codes returns every code, sorted.
func (r *Registry) Codes() []string {
	out := make([]string, 0, len(r.byCode))
	for c := range r.byCode {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// GoName turns a code into an exported Go identifier: idempotency_mismatch → IdempotencyMismatch.
func GoName(code string) string {
	var b strings.Builder
	for _, part := range strings.Split(code, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

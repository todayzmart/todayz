package main

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/todayz/backend/internal/platform/httpx/openapi/schemacheck"
)

// httpMethods are the path-item keys that hold operations.
var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

var (
	reDomainFile = regexp.MustCompile(`^[a-z][a-z0-9-]*\.yaml$`)
	reSchemaName = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	reOperation  = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
)

// domainFile is one paths/<domain>.yaml or schemas/<domain>.yaml.
type domainFile struct {
	Domain string
	Path   string // on disk
	Body   map[string]any
}

// rawSpec is everything apigen root reads: the per-domain files, unparsed
// beyond their top level, so the lints can say which file owns what.
type rawSpec struct {
	Root    map[string]any
	Paths   []domainFile
	Schemas []domainFile
	// Operations by "METHOD path", for the lints.
	ops []rawOp
}

type rawOp struct {
	Domain, Method, Path, ID string
	Body                     map[string]any
}

// readRaw reads api/root.yaml and every paths/ and schemas/ file.
func readRaw(api string) (*rawSpec, error) {
	s := &rawSpec{}
	if err := readYAML(filepath.Join(api, "root.yaml"), &s.Root); err != nil {
		return nil, err
	}
	var err error
	if s.Paths, err = readDomainDir(filepath.Join(api, "paths")); err != nil {
		return nil, err
	}
	if s.Schemas, err = readDomainDir(filepath.Join(api, "schemas")); err != nil {
		return nil, err
	}
	return s, nil
}

func readYAML(path string, into any) error {
	b, err := os.ReadFile(path) // #nosec G304 -- the repo's own contract files
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, into); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func readDomainDir(dir string) ([]domainFile, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var out []domainFile
	for _, n := range names {
		base := filepath.Base(n)
		if !reDomainFile.MatchString(base) {
			return nil, fmt.Errorf("%s: a domain file is <domain>.yaml with a lowercase, hyphenated name", n)
		}
		f := domainFile{Domain: strings.TrimSuffix(base, ".yaml"), Path: n}
		if err := readYAML(n, &f.Body); err != nil {
			return nil, err
		}
		if f.Body == nil {
			f.Body = map[string]any{}
		}
		out = append(out, f)
	}
	return out, nil
}

// lintRaw runs the per-file lints that the bundle cannot express: an
// operation is tagged with its file's domain, no path or operationId is
// defined twice across files, no schema name is used by two files,
// schemas/common.yaml is the only file that may define a reason enum, a file
// references only what its domain owns, and no schema uses a keyword the
// validator does not enforce.
func lintRaw(s *rawSpec) error {
	var errs []error
	owner := map[string]string{} // "METHOD path" -> file
	pathOwner := map[string]string{}
	ids := map[string]string{}
	for _, f := range s.Paths {
		errs = append(errs, lintRefs(f, "paths")...)
		for p, item := range f.Body {
			if err := schemacheck.Check(p, item); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", f.Path, err))
			}
			if !strings.HasPrefix(p, "/") || strings.Contains(p, "?") || (len(p) > 1 && strings.HasSuffix(p, "/")) {
				errs = append(errs, fmt.Errorf("%s: path %q must start with / and have no trailing slash or query", f.Path, p))
				continue
			}
			if prev, dup := pathOwner[p]; dup {
				errs = append(errs, fmt.Errorf("%s: path %s is already defined in %s (D-10: a path lives in one domain file)", f.Path, p, prev))
				continue
			}
			pathOwner[p] = f.Path
			m, ok := item.(map[string]any)
			if !ok {
				errs = append(errs, fmt.Errorf("%s: path %s is not a path item", f.Path, p))
				continue
			}
			for _, method := range httpMethods {
				raw, ok := m[method]
				if !ok {
					continue
				}
				op, ok := raw.(map[string]any)
				if !ok {
					errs = append(errs, fmt.Errorf("%s: %s %s is not an operation", f.Path, strings.ToUpper(method), p))
					continue
				}
				key := strings.ToUpper(method) + " " + p
				if prev, dup := owner[key]; dup {
					errs = append(errs, fmt.Errorf("%s: %s is already defined in %s", f.Path, key, prev))
				}
				owner[key] = f.Path
				id, _ := op["operationId"].(string)
				switch {
				case id == "":
					errs = append(errs, fmt.Errorf("%s: %s has no operationId", f.Path, key))
				case !reOperation.MatchString(id):
					errs = append(errs, fmt.Errorf("%s: %s: operationId %q is not camelCase", f.Path, key, id))
				case ids[id] != "":
					errs = append(errs, fmt.Errorf("%s: %s: operationId %q is already used by %s", f.Path, key, id, ids[id]))
				default:
					ids[id] = key
				}
				if tags := stringList(op["tags"]); len(tags) != 1 || tags[0] != f.Domain {
					errs = append(errs, fmt.Errorf("%s: %s: tags must be [%s] (the file's domain), got %v", f.Path, key, f.Domain, tags))
				}
				s.ops = append(s.ops, rawOp{Domain: f.Domain, Method: strings.ToUpper(method), Path: p, ID: id, Body: op})
			}
		}
	}
	names := map[string]string{}
	for _, f := range s.Schemas {
		errs = append(errs, lintRefs(f, "schemas")...)
		for name := range f.Body {
			if !reSchemaName.MatchString(name) {
				errs = append(errs, fmt.Errorf("%s: schema name %q is not UpperCamelCase", f.Path, name))
				continue
			}
			if err := schemacheck.Check(name, f.Body[name]); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", f.Path, err))
			}
			if prev, dup := names[name]; dup {
				errs = append(errs, fmt.Errorf("%s: schema %s is already defined in %s (names are unique across the API)", f.Path, name, prev))
				continue
			}
			names[name] = f.Path
			if f.Domain != "common" && strings.HasSuffix(name, "Reason") {
				errs = append(errs, fmt.Errorf("%s: %s: reason lists live only in schemas/common.yaml (04 §1.5, D-60)", f.Path, name))
			}
		}
	}
	sort.Slice(s.ops, func(i, j int) bool {
		if s.ops[i].Path != s.ops[j].Path {
			return s.ops[i].Path < s.ops[j].Path
		}
		return s.ops[i].Method < s.ops[j].Method
	})
	return errors.Join(errs...)
}

// lintRefs (D-100, code review L1) keeps a domain inside its own files: a
// `$ref` in paths/<domain>.yaml or schemas/<domain>.yaml may point into the
// same file, schemas/<domain>.yaml, schemas/common.yaml or common/*.yaml,
// never into another domain's schemas. dir is the file's directory under api/.
func lintRefs(f domainFile, dir string) []error {
	var errs []error
	allowed := func(target string) bool {
		return target == "schemas/"+f.Domain+".yaml" || target == "schemas/common.yaml" ||
			(strings.HasPrefix(target, "common/") && !strings.Contains(strings.TrimPrefix(target, "common/"), "/"))
	}
	walkRefs(f.Body, func(ref string) {
		file, _, _ := strings.Cut(ref, "#")
		if file == "" {
			return // inside this file
		}
		target := path.Clean(path.Join(dir, file))
		if !allowed(target) {
			errs = append(errs, fmt.Errorf("%s: $ref %q reaches %s; a domain file references only its own schemas, schemas/common.yaml and common/ (D-100)", f.Path, ref, target))
		}
	})
	return errs
}

// walkRefs calls fn with every `$ref` string under v, skipping example data.
func walkRefs(v any, fn func(ref string)) {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(x) {
			if k == "example" || k == "examples" {
				continue
			}
			if k == "$ref" {
				if s, ok := x[k].(string); ok {
					fn(s)
				}
				continue
			}
			walkRefs(x[k], fn)
		}
	case []any:
		for _, e := range x {
			walkRefs(e, fn)
		}
	}
}

func stringList(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))
	for _, x := range l {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// jsonPointer escapes one reference token (RFC 6901).
func jsonPointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// buildRoot returns the root document: root.yaml's own keys, then paths and
// components that $ref every domain file (relative to out).
func buildRoot(s *rawSpec, api, out string) (*yaml.Node, error) {
	rel, err := filepath.Rel(filepath.Dir(out), api)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)
	ref := func(file, pointer string) map[string]any {
		return map[string]any{"$ref": rel + "/" + file + "#/" + pointer}
	}

	paths := map[string]any{}
	domains := map[string]bool{}
	for _, f := range s.Paths {
		for p := range f.Body {
			paths[p] = ref("paths/"+f.Domain+".yaml", jsonPointer(p))
			domains[f.Domain] = true
		}
	}
	schemas := map[string]any{}
	for _, f := range s.Schemas {
		for name := range f.Body {
			schemas[name] = ref("schemas/"+f.Domain+".yaml", name)
		}
	}
	components := map[string]any{}
	if c, ok := s.Root["components"].(map[string]any); ok {
		for k, v := range c {
			components[k] = v
		}
	}
	for _, kind := range []string{"parameters", "headers", "responses"} {
		var m map[string]any
		if err := readYAML(filepath.Join(api, "common", kind+".yaml"), &m); err != nil {
			return nil, err
		}
		entries := map[string]any{}
		for name := range m {
			entries[name] = ref("common/"+kind+".yaml", name)
		}
		components[kind] = entries
	}
	components["schemas"] = schemas

	// Tags: root.yaml's, plus one per domain that has operations.
	var tags []any
	seen := map[string]bool{}
	for _, t := range stringMapList(s.Root["tags"]) {
		if name, _ := t["name"].(string); name != "" {
			seen[name] = true
			tags = append(tags, t)
		}
	}
	for _, d := range sortedKeys(domains) {
		if !seen[d] {
			tags = append(tags, map[string]any{"name": d, "description": "The " + d + " domain (paths/" + d + ".yaml)."})
		}
	}

	doc := &yaml.Node{Kind: yaml.MappingNode}
	add := func(k string, v any) error {
		var n yaml.Node
		if err := n.Encode(v); err != nil {
			return err
		}
		doc.Content = append(doc.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &n)
		return nil
	}
	for _, k := range []string{"openapi", "info", "servers", "security"} {
		v, ok := s.Root[k]
		if !ok {
			return nil, fmt.Errorf("root.yaml: %q is required", k)
		}
		if err := add(k, v); err != nil {
			return nil, err
		}
	}
	for _, kv := range []struct {
		k string
		v any
	}{{"tags", tags}, {"paths", paths}, {"components", components}} {
		if err := add(kv.k, kv.v); err != nil {
			return nil, err
		}
	}
	for k, v := range s.Root {
		switch k {
		case "openapi", "info", "servers", "security", "tags", "paths", "components":
		default:
			if err := add(k, v); err != nil {
				return nil, err
			}
		}
	}
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{doc}}, nil
}

func stringMapList(v any) []map[string]any {
	var out []map[string]any
	for _, x := range stringAnyList(v) {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func stringAnyList(v any) []any { l, _ := v.([]any); return l }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// runRoot reads, lints and writes the root document.
func runRoot(api, out string) error {
	s, err := readRaw(api)
	if err != nil {
		return err
	}
	if err := lintRaw(s); err != nil {
		return err
	}
	doc, err := buildRoot(s, api, out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return err
	}
	f, err := os.Create(out) // #nosec G304 -- the -out flag of make gen
	if err != nil {
		return err
	}
	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if _, err := fmt.Fprintln(f, "# Generated by apigen root from backend/api; DO NOT EDIT. Redocly bundles it into api/openapi.yaml."); err != nil {
		return err
	}
	if err := enc.Encode(doc); err != nil {
		_ = f.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("apigen root: %d operations in %d path files -> %s\n", len(s.ops), len(s.Paths), out)
	return nil
}

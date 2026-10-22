// Command wflint is the TodayZ policy lint for the CI definitions (W0-06).
//
// It parses workflows, composite actions and .github/dependabot.yml as YAML
// (gopkg.in/yaml.v3), so flow style, quoting, escapes, anchors and comments
// cannot hide a violation, and it scans every script, make file and infra file
// for Terraform CLI calls that would touch a cloud account before Wave 5.
//
// Usage:
//
//	wflint [-root DIR] [-v]            discover and lint the repo (fails closed)
//	wflint [-root DIR] [-v] FILE...    lint the given files (selftest fixtures)
//
// Exit status: 0 clean, 1 findings, 2 usage error. Every finding is printed as
// "path:line: [rule] message", so callers can assert on the rule tag.
//
// Rules (tag: what fails):
//
//	trigger      an `on:` event other than pull_request, push, workflow_dispatch, schedule
//	anchor       a YAML anchor or alias anywhere
//	yaml         unparseable YAML, several documents, duplicate keys, explicit tags
//	structure    unknown top-level workflow keys, a job or step that is not a mapping
//	permissions  missing at workflow or job level, not a mapping, or any value other
//	             than read or none (write-all, read-all and write in any spelling)
//	secrets      `secrets` inside any ${{ }} (run blocks and comments included), in an
//	             `if:` condition, github.token, or a `secrets:` key (secrets: inherit)
//	pin          a remote action not pinned by a 40-hex SHA with a `# vX.Y.Z` comment,
//	             or a docker:// action, container or services image without @sha256:
//	uses         a ./ path outside .github/actions/, or an action owner that is not in
//	             allowed-owners.txt
//	checkout     actions/checkout without `persist-credentials: false`
//	shell        a workflow without `defaults: run: shell: bash`, or any shell that is
//	             not bash (steps get -eo pipefail)
//	timeout      a job without timeout-minutes (reusable-workflow call jobs must not
//	             set it; GitHub forbids it there)
//	runner       runs-on that is not a single GitHub-hosted ubuntu-* label
//	concurrency  no concurrency group, or a group without ${{ github.event_name }}
//	make         a make call in a run block whose targets are not all canonical
//	             (root Makefile CANONICAL), use ${{ }}, use variables, options other
//	             than -s -k -n -j and -C ., variable overrides, or MAKEFLAGS-style env
//	forbidden    a Terraform CLI subcommand other than fmt, validate, version,
//	             init -backend=false and providers lock, including one passed through
//	             a variable, a quoted word, an alias (tf=terraform) or a pinned image
//	dependabot   a missing ecosystem, a Go module not covered, an interval other than
//	             weekly, or a cooldown below 14 days (D-88)
//	discovery    a required folder or file missing, or no files found
//
// Out of reach (documented, not checked): commands built at run time beyond a plain
// alias (for example `m=ma; ${m}ke x`, `$(printf make) x`, or eval of computed text),
// and Terraform run from inside another program (Go, Node, a .tf local-exec).
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type finding struct {
	file string
	line int
	rule string
	msg  string
}

type linter struct {
	root      string
	verbose   bool
	canonical map[string]bool
	owners    map[string]bool
	idToken   map[string]bool
	goMods    []string // repo-relative dirs holding a go.mod (dependabot coverage)
	tf        *tfScanner
	findings  []finding
	seen      map[string]bool
}

func (l *linter) add(file string, line int, rule, format string, args ...any) {
	f := finding{file: file, line: line, rule: rule, msg: fmt.Sprintf(format, args...)}
	key := fmt.Sprintf("%s:%d:%s:%s", f.file, f.line, f.rule, f.msg)
	if l.seen[key] {
		return
	}
	l.seen[key] = true
	l.findings = append(l.findings, f)
}

func main() {
	root := flag.String("root", ".", "repo root")
	verbose := flag.Bool("v", false, "list every file linted")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: wflint [-root DIR] [-v] [FILE...]")
	}
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wflint:", err)
		os.Exit(2)
	}
	l := &linter{root: abs, verbose: *verbose, seen: map[string]bool{}, tf: newTFScanner()}
	if err := l.loadPolicy(); err != nil {
		fmt.Fprintln(os.Stderr, "wflint:", err)
		os.Exit(2)
	}

	var files []target
	if flag.NArg() > 0 {
		for _, a := range flag.Args() {
			files = append(files, l.classify(a))
		}
	} else {
		files = l.discover()
	}
	l.run(files)

	sort.SliceStable(l.findings, func(i, j int) bool {
		if l.findings[i].file != l.findings[j].file {
			return l.findings[i].file < l.findings[j].file
		}
		return l.findings[i].line < l.findings[j].line
	})
	for _, f := range l.findings {
		fmt.Printf("%s:%d: [%s] %s\n", f.file, f.line, f.rule, f.msg)
	}
	if len(l.findings) > 0 {
		fmt.Printf("wflint: %d finding(s)\n", len(l.findings))
		os.Exit(1)
	}
}

type kind int

const (
	kWorkflow kind = iota
	kAction
	kDependabot
	kText // scanned for Terraform CLI calls only
)

func (k kind) String() string {
	return [...]string{"workflow", "action", "dependabot", "text"}[k]
}

type target struct {
	abs, rel string
	kind     kind
}

func (l *linter) rel(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	r, err := filepath.Rel(l.root, abs)
	if err != nil || strings.HasPrefix(r, "..") {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

func (l *linter) classify(p string) target {
	abs, _ := filepath.Abs(p)
	base := filepath.Base(p)
	t := target{abs: abs, rel: l.rel(p), kind: kText}
	switch {
	case base == "action.yml" || base == "action.yaml":
		t.kind = kAction
	case base == "dependabot.yml" || base == "dependabot.yaml":
		t.kind = kDependabot
	case strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml"):
		t.kind = kWorkflow
	}
	return t
}

// Folders and files discovery requires; a missing one is a finding, not a skip.
var requiredDirs = []string{".github/workflows", ".github/actions", "scripts", "mk", "infra"}
var requiredFiles = []string{"Makefile", ".github/dependabot.yml"}

// Roots scanned for Terraform CLI calls.
var tfRoots = []string{".github", "scripts", "mk", "infra"}

func skipDir(rel, name string) bool {
	switch name {
	case "node_modules", ".terraform", "testdata", ".git", ".cache", ".tools":
		return true
	}
	switch rel {
	case "infra/local/data", "infra/local/caddy/data", "infra/local/pmtiles":
		return true
	}
	return false
}

// Prose and Go sources are not shell; everything else under the roots is scanned.
func skipTextFile(name string) bool {
	for _, s := range []string{".md", ".go", ".mod", ".sum", ".png", ".jpg", ".svg", ".pmtiles", ".pem", ".crt"} {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

func (l *linter) discover() []target {
	var out []target
	for _, d := range requiredDirs {
		if st, err := os.Stat(filepath.Join(l.root, d)); err != nil || !st.IsDir() {
			l.add(d, 0, "discovery", "required folder %s is missing", d)
		}
	}
	for _, f := range requiredFiles {
		if st, err := os.Stat(filepath.Join(l.root, f)); err != nil || st.IsDir() {
			l.add(f, 0, "discovery", "required file %s is missing", f)
		}
	}

	nWF := 0
	if ents, err := os.ReadDir(filepath.Join(l.root, ".github/workflows")); err == nil {
		for _, e := range ents {
			n := e.Name()
			if e.Type().IsRegular() && (strings.HasSuffix(n, ".yml") || strings.HasSuffix(n, ".yaml")) {
				out = append(out, target{abs: filepath.Join(l.root, ".github/workflows", n), rel: ".github/workflows/" + n, kind: kWorkflow})
				nWF++
			}
		}
	}
	if nWF == 0 {
		l.add(".github/workflows", 0, "discovery", "no workflow files found")
	}

	nAct := 0
	_ = filepath.WalkDir(filepath.Join(l.root, ".github/actions"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && (d.Name() == "action.yml" || d.Name() == "action.yaml") {
			out = append(out, target{abs: p, rel: l.rel(p), kind: kAction})
			nAct++
		}
		return nil
	})
	if nAct == 0 {
		l.add(".github/actions", 0, "discovery", "no composite actions found")
	}

	if _, err := os.Stat(filepath.Join(l.root, ".github/dependabot.yml")); err == nil {
		out = append(out, target{abs: filepath.Join(l.root, ".github/dependabot.yml"), rel: ".github/dependabot.yml", kind: kDependabot})
	}

	known := map[string]bool{}
	for _, t := range out {
		known[t.rel] = true
	}
	nText := 0
	add := func(p string) {
		r := l.rel(p)
		if known[r] {
			return
		}
		known[r] = true
		out = append(out, target{abs: p, rel: r, kind: kText})
		nText++
	}
	if _, err := os.Stat(filepath.Join(l.root, "Makefile")); err == nil {
		add(filepath.Join(l.root, "Makefile"))
	}
	for _, r := range tfRoots {
		_ = filepath.WalkDir(filepath.Join(l.root, r), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != filepath.Join(l.root, r) && skipDir(l.rel(p), d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type().IsRegular() && !skipTextFile(d.Name()) {
				add(p)
			}
			return nil
		})
	}
	if nText == 0 {
		l.add(".", 0, "discovery", "no scripts, make files or infra files found")
	}
	return out
}

func (l *linter) run(files []target) {
	counts := map[kind]int{}
	// Pass 1: Terraform aliases from every file (a variable set in versions.env is
	// used in terraform.sh).
	contents := map[string][]byte{}
	for _, t := range files {
		data, err := os.ReadFile(t.abs)
		if err != nil {
			l.add(t.rel, 0, "discovery", "cannot read: %v", err)
			continue
		}
		if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
			if t.kind != kText {
				l.add(t.rel, 0, "yaml", "binary file")
			}
			continue // binary: nothing to scan
		}
		contents[t.rel] = data
		l.tf.collect(t.rel, string(data))
	}
	l.tf.finish()

	for _, t := range files {
		data, ok := contents[t.rel]
		if !ok {
			continue
		}
		counts[t.kind]++
		if l.verbose {
			fmt.Printf("  %-10s %s\n", t.kind, t.rel)
		}
		switch t.kind {
		case kWorkflow:
			l.lintWorkflow(t.rel, data)
		case kAction:
			l.lintAction(t.rel, data)
		case kDependabot:
			l.lintDependabot(t.rel, data)
		}
		for _, f := range l.tf.check(t.rel, string(data)) {
			l.add(t.rel, f.line, "forbidden", "%s", f.msg)
		}
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	fmt.Printf("wflint: linted %d files (%d workflows, %d actions, %d dependabot, %d scanned for Terraform commands)\n",
		total, counts[kWorkflow], counts[kAction], counts[kDependabot], counts[kText])
	if total == 0 {
		l.add(".", 0, "discovery", "no files linted")
	}
}

var canonicalRe = regexp.MustCompile(`^CANONICAL\s*:?=\s*(.+)$`)

// loadPolicy reads the canonical make targets from the root Makefile and the
// allow-lists that sit next to this program.
func (l *linter) loadPolicy() error {
	mf, err := os.Open(filepath.Join(l.root, "Makefile"))
	if err != nil {
		return fmt.Errorf("root Makefile: %w", err)
	}
	defer mf.Close()
	l.canonical = map[string]bool{}
	sc := bufio.NewScanner(mf)
	for sc.Scan() {
		if m := canonicalRe.FindStringSubmatch(strings.TrimSpace(sc.Text())); m != nil {
			for _, t := range strings.Fields(m[1]) {
				l.canonical[t] = true
			}
		}
	}
	if len(l.canonical) == 0 {
		return fmt.Errorf("no CANONICAL := line in the root Makefile")
	}
	dir := filepath.Join(l.root, "scripts/ci/wflint")
	if l.owners, err = readList(filepath.Join(dir, "allowed-owners.txt")); err != nil {
		return err
	}
	if len(l.owners) == 0 {
		return fmt.Errorf("allowed-owners.txt is empty")
	}
	if l.idToken, err = readList(filepath.Join(dir, "id-token-allow.txt")); err != nil {
		return err
	}
	l.goMods = findGoMods(l.root)
	return nil
}

func readList(p string) (map[string]bool, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if line = strings.TrimSpace(line); line != "" {
			out[line] = true
		}
	}
	return out, nil
}

func findGoMods(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "testdata", ".git", ".cache", ".tools", ".claude", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			r, _ := filepath.Rel(root, filepath.Dir(p))
			out = append(out, "/"+strings.TrimPrefix(filepath.ToSlash(r), "."))
		}
		return nil
	})
	for i, m := range out {
		if m != "/" {
			out[i] = strings.TrimSuffix(m, "/")
		}
	}
	sort.Strings(out)
	return out
}

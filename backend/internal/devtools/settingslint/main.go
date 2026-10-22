// Command settingslint is SC-PL-06's static check: a configurable number
// (01 §17, platform_settings) must be read from settings, never hard-coded.
//
// It reads every numeric default from the platform_settings rows in the
// embedded goose migrations, keeps the distinctive ones (see distinctive), and
// flags any numeric literal with one of those values in non-test domain code:
// every package under internal/ except platform/, devtools/, testkit/ and
// seed/ (seed data are fixtures). Test files, testdata/ and generated files
// are skipped.
//
// A line that really means the number for another reason says so:
//
//	const weekdays = 14 //settings:ok fortnight in the IST calendar, not a rule
//
// The reason is required. `make check` runs it (check-backend):
//
//	go run ./internal/devtools/settingslint .
package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	dbschema "github.com/todayz/backend/db"
)

// Directive is the escape comment; it must be followed by a reason.
const Directive = "//settings:ok"

// common are numbers that turn up for reasons unrelated to any product rule
// (HTTP statuses, unit conversions, percentages, small counts). A default with
// one of these values is still read from settings by convention, but a bare
// literal is not evidence of a hard-coded rule, so the lint does not flag it.
// Everything from 0 to 12 is common too.
var common = map[int64]bool{
	15: true, 20: true, 24: true, 30: true, 45: true, 50: true, 60: true, 64: true,
	90: true, 100: true, 128: true, 200: true, 256: true, 500: true, 1000: true,
	1024: true, 3600: true,
}

// distinctive reports whether a default value is specific enough that a bare
// literal with that value is almost certainly the rule hard-coded.
func distinctive(v int64) bool { return v > 12 && !common[v] }

// Defaults maps each distinctive numeric default to the settings keys that
// have it. It reads every platform_settings row in the migrations (a later
// migration that changes a default adds its new value).
func Defaults(migrations fs.FS) (map[int64][]string, error) {
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	row := regexp.MustCompile(`^\s*\('([a-z0-9_]+)',\s*'([^']*)',\s*'([a-z]+)'`)
	out := map[int64][]string{}
	for _, f := range files {
		b, err := fs.ReadFile(migrations, f)
		if err != nil {
			return nil, err
		}
		if !bytes.Contains(b, []byte("platform_settings")) {
			continue
		}
		up := strings.SplitN(string(b), "-- +goose Down", 2)[0]
		for _, line := range strings.Split(up, "\n") {
			m := row.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			v, err := strconv.ParseInt(m[2], 10, 64)
			if err != nil || !distinctive(v) {
				continue // bool, text or a common number
			}
			if !contains(out[v], m[1]) {
				out[v] = append(out[v], m[1])
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no platform_settings defaults found in the migrations")
	}
	return out, nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Finding is one flagged literal.
type Finding struct {
	Pos     token.Position
	Message string
}

func (f Finding) String() string { return fmt.Sprintf("%s: %s", f.Pos, f.Message) }

// skipDirs are the internal/ trees the lint does not apply to.
var skipDirs = []string{"platform", "devtools", "testkit", "seed"}

// Lint walks root/internal and returns the findings, sorted by position, with
// file names relative to root.
func Lint(root string, defaults map[int64][]string) ([]Finding, error) {
	var out []Finding
	base := filepath.Join(root, "internal")
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error { // #nosec G703 -- walking the source tree is the job
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(base, path)
			first := strings.Split(filepath.ToSlash(rel), "/")[0]
			if d.Name() == "testdata" || (rel != "." && contains(skipDirs, first)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		found, err := lintFile(root, path, defaults)
		out = append(out, found...)
		return err
	})
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Pos, out[j].Pos
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Line < b.Line || (a.Line == b.Line && a.Column < b.Column)
	})
	return out, err
}

func lintFile(root, path string, defaults map[int64][]string) ([]Finding, error) {
	src, err := os.ReadFile(path) // #nosec G304 -- walking the source tree is the job
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	rel, _ := filepath.Rel(root, path)
	f, err := parser.ParseFile(fset, filepath.ToSlash(rel), src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if ast.IsGenerated(f) {
		return nil, nil
	}
	lines := splitLines(src)
	var out []Finding
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || (lit.Kind != token.INT && lit.Kind != token.FLOAT) {
			return true
		}
		v, ok := intValue(lit)
		if !ok {
			return true
		}
		keys, hit := defaults[v]
		if !hit {
			return true
		}
		pos := fset.Position(lit.Pos())
		line := ""
		if pos.Line-1 < len(lines) {
			line = lines[pos.Line-1]
		}
		if i := strings.Index(line, Directive); i >= 0 {
			if strings.TrimSpace(line[i+len(Directive):]) != "" {
				return true
			}
			out = append(out, Finding{pos, Directive + " needs a reason"})
			return true
		}
		out = append(out, Finding{pos, fmt.Sprintf(
			"literal %s equals the default of %s; read it from platform_settings (01 §17) or add %s <reason>",
			lit.Value, strings.Join(keys, ", "), Directive)})
		return true
	})
	return out, nil
}

func intValue(lit *ast.BasicLit) (int64, bool) {
	if lit.Kind == token.INT {
		v, err := strconv.ParseInt(lit.Value, 0, 64)
		return v, err == nil
	}
	fv, err := strconv.ParseFloat(strings.ReplaceAll(lit.Value, "_", ""), 64)
	if err != nil || fv != math.Trunc(fv) || math.Abs(fv) > math.MaxInt32 {
		return 0, false
	}
	return int64(fv), true
}

func splitLines(src []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

func run(root string, stdout io.Writer) (int, error) {
	defaults, err := Defaults(dbschema.Migrations)
	if err != nil {
		return 0, err
	}
	found, err := Lint(root, defaults)
	if err != nil {
		return 0, err
	}
	for _, f := range found {
		_, _ = fmt.Fprintln(stdout, f)
	}
	return len(found), nil
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	n, err := run(root, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "settingslint:", err)
		os.Exit(2)
	}
	if n > 0 {
		fmt.Fprintf(os.Stderr, "settingslint: %d hard-coded configurable number(s); see backend/CLAUDE.md (SC-PL-06)\n", n)
		os.Exit(1)
	}
}

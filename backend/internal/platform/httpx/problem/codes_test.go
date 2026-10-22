package problem

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const problemPkg = "github.com/todayz/backend/internal/platform/httpx/problem"

// TestNoUnregisteredCodes scans every non-test Go file in the backend for a
// problem built with a literal code (problem.New("x", …), Newf, Wrap, or
// problem.Error{Code: "x"}) and fails if that code is not registered in
// backend/api/problems/*.yaml. Codes taken from gen/problems constants are
// registered by construction; anything else is caught at run time by Write.
func TestNoUnregisteredCodes(t *testing.T) {
	root := moduleRoot(t)
	v := scanForUnregistered(t, root)
	require.Empty(t, v, "handlers use problem codes that are not in backend/api/problems/*.yaml")
}

// The scanner is proven on a seeded violation in testdata.
func TestScannerCatchesFixtureViolation(t *testing.T) {
	v := scanForUnregistered(t, filepath.Join("testdata", "violation"))
	require.Len(t, v, 3, "%v", v)
	joined := strings.Join(v, "\n")
	require.Contains(t, joined, `"no_such_code"`)
	require.Contains(t, joined, `"another_missing"`)
	require.Contains(t, joined, `"literal_in_struct"`)
	require.NotContains(t, joined, `"not_found"`, "a registered literal is fine")
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "go.mod not found")
		dir = parent
	}
}

func scanForUnregistered(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "gen" || name == "tools" || name == "testdata" || name == "node_modules" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		out = append(out, checkFile(fset, f)...)
		return nil
	})
	require.NoError(t, err)
	sort.Strings(out)
	return out
}

func checkFile(fset *token.FileSet, f *ast.File) []string {
	local := ""
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p != problemPkg {
			continue
		}
		local = "problem"
		if imp.Name != nil {
			local = imp.Name.Name
		}
	}
	inPkg := f.Name.Name == "problem"
	if local == "" && !inPkg {
		return nil
	}
	isProblem := func(e ast.Expr, names ...string) bool {
		var name string
		switch x := e.(type) {
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			if !ok || id.Name != local {
				return false
			}
			name = x.Sel.Name
		case *ast.Ident:
			if !inPkg {
				return false
			}
			name = x.Name
		default:
			return false
		}
		for _, n := range names {
			if n == name {
				return true
			}
		}
		return false
	}
	var out []string
	report := func(lit *ast.BasicLit) {
		if lit == nil || lit.Kind != token.STRING {
			return
		}
		code, err := strconv.Unquote(lit.Value)
		if err != nil || Registered(code) {
			return
		}
		out = append(out, fmt.Sprintf("%s: %q is not a registered problem code", fset.Position(lit.Pos()), code))
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if len(x.Args) > 0 && isProblem(x.Fun, "New", "Newf", "Wrap") {
				lit, _ := x.Args[0].(*ast.BasicLit)
				report(lit)
			}
		case *ast.CompositeLit:
			if isProblem(x.Type, "Error") {
				for _, el := range x.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Code" {
							lit, _ := kv.Value.(*ast.BasicLit)
							report(lit)
						}
					}
				}
			}
		}
		return true
	})
	return out
}

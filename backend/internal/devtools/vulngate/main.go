// Command vulngate reads `govulncheck -format json` on stdin and decides the
// check-backend verdict. Only vulnerabilities our code actually calls count
// (symbol-level findings, what govulncheck's text mode reports as "affected").
//
//   - A finding in a module from go.mod always fails: bump the module.
//   - A finding in the Go standard library is fixed by the machine's Go
//     toolchain, not by this repo. With -stdlib=fail it fails too; with
//     -stdlib=warn (the make default until every machine runs a patched Go)
//     it prints the Go version that fixes it and passes.
//
// Output names the OSV IDs, modules and fixed versions only.
//
//	govulncheck -db file://.cache/govulndb -format json ./... | go run ./internal/devtools/vulngate -stdlib=warn
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type frame struct {
	Module   string `json:"module"`
	Version  string `json:"version"`
	Package  string `json:"package"`
	Function string `json:"function"`
}

type message struct {
	Finding *struct {
		OSV          string  `json:"osv"`
		FixedVersion string  `json:"fixed_version"`
		Trace        []frame `json:"trace"`
	} `json:"finding"`
}

// Vuln is one called vulnerability.
type Vuln struct {
	OSV, Module, Version, Fixed string
}

// Stdlib reports whether the vulnerability is in the Go standard library.
func (v Vuln) Stdlib() bool { return v.Module == "stdlib" || v.Module == "toolchain" }

func (v Vuln) String() string {
	fixed := v.Fixed
	if fixed == "" {
		fixed = "no fix yet"
	}
	return fmt.Sprintf("%s  %s@%s  fixed in %s", v.OSV, v.Module, v.Version, fixed)
}

// Parse returns the called vulnerabilities, one per OSV ID and module, sorted.
func Parse(r io.Reader) ([]Vuln, error) {
	dec := json.NewDecoder(r)
	seen := map[string]Vuln{}
	for {
		var m message
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("vulngate: reading govulncheck json: %w", err)
		}
		f := m.Finding
		if f == nil || len(f.Trace) == 0 || f.Trace[0].Function == "" {
			continue // module- or package-level: imported, not called
		}
		v := Vuln{OSV: f.OSV, Module: f.Trace[0].Module, Version: f.Trace[0].Version, Fixed: f.FixedVersion}
		seen[v.OSV+" "+v.Module] = v
	}
	out := make([]Vuln, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Module != out[j].Module {
			return out[i].Module < out[j].Module
		}
		return out[i].OSV < out[j].OSV
	})
	return out, nil
}

// Verdict prints the report and returns whether the check fails.
func Verdict(vulns []Vuln, stdlibFails bool, w io.Writer) bool {
	var mods, std []Vuln
	maxGo := ""
	for _, v := range vulns {
		if v.Stdlib() {
			std = append(std, v)
			if goVersionLess(maxGo, v.Fixed) {
				maxGo = v.Fixed
			}
		} else {
			mods = append(mods, v)
		}
	}
	for _, v := range mods {
		_, _ = fmt.Fprintln(w, "vulnerable module:", v)
	}
	if len(std) > 0 {
		label := "warning"
		if stdlibFails {
			label = "error"
		}
		_, _ = fmt.Fprintf(w, "%s: %d standard library vulnerabilities in the local Go toolchain; upgrade Go to %s or later\n",
			label, len(std), strings.TrimPrefix(maxGo, "v"))
		for _, v := range std {
			_, _ = fmt.Fprintln(w, "  ", v)
		}
	}
	fail := len(mods) > 0 || (stdlibFails && len(std) > 0)
	if !fail {
		_, _ = fmt.Fprintf(w, "govulncheck: no called vulnerabilities in modules (%d in the standard library)\n", len(std))
	}
	return fail
}

// goVersionLess compares "v1.26.1"-style versions; "" is the smallest.
func goVersionLess(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func versionParts(s string) []int {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "go"), "v")
	if s == "" {
		return nil
	}
	var out []int
	for _, p := range strings.Split(s, ".") {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		out = append(out, n)
	}
	return out
}

func main() {
	stdlib := flag.String("stdlib", "warn", "what a standard library finding does: warn or fail")
	flag.Parse()
	if *stdlib != "warn" && *stdlib != "fail" {
		fmt.Fprintln(os.Stderr, "vulngate: -stdlib must be warn or fail")
		os.Exit(2)
	}
	vulns, err := Parse(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if Verdict(vulns, *stdlib == "fail", os.Stdout) {
		os.Exit(1)
	}
}

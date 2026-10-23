// Command apigen joins, lints and generates from the per-domain OpenAPI files
// in backend/api (W0-04; 04 §1, D-50, D-55). `make gen` (gen-contract) runs it:
//
//	apigen root    -api api -out gen/api/root.yaml
//	   lints the raw domain files (one domain per file, unique paths, operationIds
//	   and schema names) and writes the root document Redocly bundles
//	apigen check   -api api -bundle api/openapi.yaml [-expect-count 307]
//	   loads the bundle with kin-openapi, validates it with its examples and runs
//	   the TodayZ lints (SC-PL-01, SC-PL-02, 04 §1.2, §1.6, §3)
//	apigen codegen -api api -bundle api/openapi.yaml -out gen/api -tool oapi-codegen
//	   writes gen/api/spec (the embedded bundle) and one Go package per domain
//	   with operations: types, chi server and strict server from oapi-codegen
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "apigen: usage: apigen root|check|codegen [flags]")
		os.Exit(2)
	}
	cmd, args := flag.Arg(0), flag.Args()[1:]
	fs := flag.NewFlagSet("apigen "+cmd, flag.ExitOnError)
	api := fs.String("api", "api", "directory with root.yaml, common/, paths/, schemas/, problems/")
	var err error
	switch cmd {
	case "root":
		out := fs.String("out", "gen/api/root.yaml", "root document to write")
		_ = fs.Parse(args)
		err = runRoot(*api, *out)
	case "check":
		bundle := fs.String("bundle", "api/openapi.yaml", "the Redocly bundle")
		expect := fs.Int("expect-count", 0, "fail unless the bundle has this many method+path pairs under /v1 (0 = only print the counts)")
		_ = fs.Parse(args)
		err = runCheck(*api, *bundle, *expect, os.Stdout)
	case "codegen":
		bundle := fs.String("bundle", "api/openapi.yaml", "the Redocly bundle")
		out := fs.String("out", "gen/api", "output directory")
		tool := fs.String("tool", "oapi-codegen", "the oapi-codegen binary")
		_ = fs.Parse(args)
		err = runCodegen(*api, *bundle, *out, *tool, os.Stdout)
	default:
		err = fmt.Errorf("unknown command %q (root|check|codegen)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "apigen:", err)
		os.Exit(1)
	}
}

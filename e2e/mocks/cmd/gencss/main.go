// Command gencss regenerates internal/ui/assets/tz.css from the design system.
//
//	go run ./cmd/gencss                     # run in e2e/mocks
//	go run ./cmd/gencss -design DIR -out FILE
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"todayz.local/e2e/mocks/internal/ui/cssgen"
)

func main() {
	design := flag.String("design", filepath.Join("..", "..", "design", "source"), "design system directory (tokens.json, components/bundle.css)")
	out := flag.String("out", filepath.Join("internal", "ui", "assets", "tz.css"), "output file")
	flag.Parse()

	tokens, err := os.ReadFile(filepath.Join(*design, "tokens.json"))
	if err != nil {
		fail(err)
	}
	bundle, err := os.ReadFile(filepath.Join(*design, "components", "bundle.css"))
	if err != nil {
		fail(err)
	}
	css, err := cssgen.Generate(tokens, bundle)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(*out, css, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", *out, len(css))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gencss:", err)
	os.Exit(1)
}

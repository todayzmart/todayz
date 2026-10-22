package ui

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"todayz.local/e2e/mocks/internal/ui/cssgen"
)

// designDir is the design system copy at the repo root.
var designDir = filepath.Join("..", "..", "..", "..", "design", "source")

func readAsset(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(AssetsFS(), name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// tz.css must be exactly what the generator makes from the design system.
func TestTzCSSUpToDate(t *testing.T) {
	tokens, err := os.ReadFile(filepath.Join(designDir, "tokens.json"))
	if err != nil {
		t.Skip("design system not present (split repo); skipping the drift check")
	}
	bundle, err := os.ReadFile(filepath.Join(designDir, "components", "bundle.css"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := cssgen.Generate(tokens, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, []byte(readAsset(t, "tz.css"))) {
		t.Error("internal/ui/assets/tz.css is stale; run `go run ./cmd/gencss` in e2e/mocks")
	}
}

// The UI loads nothing from the network: no @import, no web fonts, no remote URLs.
func TestAssetsAreOffline(t *testing.T) {
	remote := regexp.MustCompile(`(?i)(@import|googleapis|gstatic|https?://)`)
	for _, name := range []string{"tz.css", "mocks.css", "app.js", "index.html"} {
		s := readAsset(t, name)
		if name == "index.html" {
			// The SVG namespace is an identifier, not a fetch.
			s = strings.ReplaceAll(s, `xmlns="http://www.w3.org/2000/svg"`, "")
		}
		if m := remote.FindString(s); m != "" {
			t.Errorf("%s references %q", name, m)
		}
	}
}

// Every var(--x) used is defined in tz.css, except the documented local ones.
func TestCSSVarsDefined(t *testing.T) {
	css := readAsset(t, "tz.css") + readAsset(t, "mocks.css")
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`(--[a-z0-9-]+)\s*:`).FindAllStringSubmatch(css, -1) {
		defined[m[1]] = true
	}
	local := map[string]bool{"--dot": true, "--t-bg": true, "--t-ink": true} // set per component in bundle.css
	for _, m := range regexp.MustCompile(`var\((--[a-z0-9-]+)`).FindAllStringSubmatch(css, -1) {
		if !defined[m[1]] && !local[m[1]] {
			t.Errorf("%s is used but not defined", m[1])
		}
	}
	for _, v := range []string{"--brand", "--wash", "--font-sans", "--font-mono", "--space-4", "--radius-lg", "--content-max"} {
		if !defined[v] {
			t.Errorf("token %s missing", v)
		}
	}
}

// Captured data is untrusted: the script never parses it as HTML.
func TestScriptUsesTextOnly(t *testing.T) {
	js := readAsset(t, "app.js")
	for _, bad := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval("} {
		if strings.Contains(js, bad) {
			t.Errorf("app.js uses %s", bad)
		}
	}
	html := readAsset(t, "index.html")
	if strings.Contains(html, " style=") || strings.Contains(html, "<style") || regexp.MustCompile(`<script>[^<]`).MatchString(html) {
		t.Error("index.html must have no inline style or script (CSP default-src 'self')")
	}
	for _, kind := range []string{"sms", "whatsapp", "push", "turnstile", "stt", "llm"} {
		if !strings.Contains(html, `data-kind="`+kind+`"`) || !strings.Contains(js, kind+": {") {
			t.Errorf("tab %s missing", kind)
		}
	}
	// The tab comes from location.hash: only own keys of KINDS count, so
	// "#constructor" or "#__proto__" falls back to sms.
	if !strings.Contains(js, "Object.hasOwn(KINDS, k)") || strings.Contains(js, "!KINDS[") {
		t.Error("app.js must check the tab with Object.hasOwn(KINDS, k)")
	}
}

func TestGenerateRejectsBadTokens(t *testing.T) {
	if _, err := cssgen.Generate([]byte(`{`), nil); err == nil {
		t.Error("bad JSON accepted")
	}
	if _, err := cssgen.Generate([]byte(`{"color":{"tokens":[]}}`), nil); err == nil {
		t.Error("no tokens accepted")
	}
	if _, err := cssgen.Generate([]byte(`{"color":{"tokens":[{"name":"x","value":{"dark":"#000"}}]}}`), nil); err == nil {
		t.Error("token without light value accepted")
	}
	out, err := cssgen.Generate([]byte(`{"color":{"tokens":[{"name":"bg","value":{"light":"#fff"}}]},"zIndex":{"tokens":[{"name":"z-nav","value":20}]}}`),
		[]byte("@import url(\"https://x\");\n.a{color:var(--bg)}"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "--bg: #fff;") || !strings.Contains(s, "--z-nav: 20;") || strings.Contains(s, "@import") || !strings.Contains(s, ".a{color:var(--bg)}") {
		t.Errorf("generated:\n%s", s)
	}
}

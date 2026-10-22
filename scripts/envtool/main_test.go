package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tempEnv(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".env.local")
	if content != "" {
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestGenerateFreshPrintsOnlyNames(t *testing.T) {
	p := tempEnv(t, "")
	var out bytes.Buffer
	if err := runGenerate([]string{"-env", p}, &out); err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	for _, n := range AppKeys {
		want.WriteString("generated: " + n + "\n")
	}
	if out.String() != want.String() {
		t.Fatalf("output:\n%s\nwant:\n%s", out.String(), want.String())
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", st.Mode().Perm())
	}
	ef, _ := readEnv(p)
	for _, n := range AppKeys {
		if ef.Values[n] == "" {
			t.Fatalf("%s not written", n)
		}
	}
	// The printed output must not contain any value in any form.
	outFile := filepath.Join(t.TempDir(), "out.txt")
	_ = os.WriteFile(outFile, out.Bytes(), 0o600)
	var scan bytes.Buffer
	if err := runLeakscan([]string{"-env", p, outFile}, &scan); err != nil {
		t.Fatalf("generate output leaks: %s", scan.String())
	}
}

func TestGenerateSecondRunIsSilent(t *testing.T) {
	p := tempEnv(t, "")
	var first, second bytes.Buffer
	if err := runGenerate([]string{"-env", p}, &first); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p)
	if err := runGenerate([]string{"-env", p}, &second); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p)
	if second.Len() != 0 {
		t.Fatalf("second run printed %q", second.String())
	}
	if !bytes.Equal(before, after) {
		t.Fatal("second run changed the file")
	}
}

func TestGenerateKeepsExistingAndFillsGaps(t *testing.T) {
	p := tempEnv(t, "APP_ENV=local\nPII_ENC_KEY='keep-me-0123456789'\nJWT_CURRENT_KID=kfixed\n") // gitleaks:allow (test sentinel)
	var out bytes.Buffer
	if err := runGenerate([]string{"-env", p}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "PII_ENC_KEY") || strings.Contains(out.String(), "JWT_CURRENT_KID") {
		t.Fatalf("regenerated an existing key: %s", out.String())
	}
	ef, _ := readEnv(p)
	if ef.Values["PII_ENC_KEY"] != "keep-me-0123456789" || ef.Values["APP_ENV"] != "local" {
		t.Fatal("existing values changed")
	}
	kid, err := firstKid(ef.Values["JWT_SIGNING_KEYS"])
	if err != nil || kid != "kfixed" {
		t.Fatalf("signing set kid %q, want the existing current kid", kid)
	}
}

func TestGenerateCurrentKidFromExistingSet(t *testing.T) {
	set, _ := newSigningKeys("kold")
	p := tempEnv(t, "JWT_SIGNING_KEYS='"+set+"'\n")
	if err := runGenerate([]string{"-env", p}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	ef, _ := readEnv(p)
	if ef.Values["JWT_CURRENT_KID"] != "kold" {
		t.Fatalf("current kid %q, want kold", ef.Values["JWT_CURRENT_KID"])
	}
}

func TestGenerateRejectsBrokenSet(t *testing.T) {
	p := tempEnv(t, "JWT_SIGNING_KEYS='not json'\n")
	err := runGenerate([]string{"-env", p}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("want error for a broken key set")
	}
	if strings.Contains(err.Error(), "not json") {
		t.Fatal("error echoes the value")
	}
}

func TestSigningKeysAreUsableEd25519(t *testing.T) {
	setJSON, err := newSigningKeys(newKid(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	var s jwkSet
	if err := json.Unmarshal([]byte(setJSON), &s); err != nil {
		t.Fatal(err)
	}
	k := s.Keys[0]
	if k.Kty != "OKP" || k.Crv != "Ed25519" || k.Alg != "EdDSA" || k.Kid == "" {
		t.Fatalf("bad jwk header fields: %+v", k.Kid)
	}
	seed, _ := base64.RawURLEncoding.DecodeString(k.D)
	pub, _ := base64.RawURLEncoding.DecodeString(k.X)
	priv := ed25519.NewKeyFromSeed(seed)
	sig := ed25519.Sign(priv, []byte("msg"))
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte("msg"), sig) {
		t.Fatal("x does not match d")
	}
}

func TestSymmetricKeysAre32RandomBytes(t *testing.T) {
	_, _, err := Generate(map[string]string{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		lines, _, _ := Generate(map[string]string{}, time.Now())
		for _, l := range lines {
			name, v, _ := strings.Cut(l, "=")
			v = unquote(v)
			if name == "JWT_SIGNING_KEYS" || name == "JWT_CURRENT_KID" {
				continue
			}
			b, err := base64.StdEncoding.DecodeString(v)
			if err != nil || len(b) != 32 {
				t.Fatalf("%s is not 32 base64 bytes", name)
			}
			if seen[v] {
				t.Fatalf("%s repeated a value", name)
			}
			seen[v] = true
		}
	}
}

func TestCheckPrintsNamesOnly(t *testing.T) {
	dir := t.TempDir()
	example := filepath.Join(dir, ".env.example")
	_ = os.WriteFile(example, []byte("# c\nAPP_ENV=\nSENTRY_DSN=\nPII_ENC_KEY=\n"), 0o600)
	p := tempEnv(t, "APP_ENV=local\nPII_ENC_KEY='secret-value-xyz'\n")
	t.Setenv("SENTRY_DSN", "")
	var out bytes.Buffer
	if err := runCheck([]string{"-env", p, "-example", example}, &out); err != nil {
		t.Fatal(err)
	}
	want := "APP_ENV set\nSENTRY_DSN missing\nPII_ENC_KEY set\n"
	if out.String() != want {
		t.Fatalf("got %q want %q", out.String(), want)
	}
	if strings.Contains(out.String(), "secret-value-xyz") || strings.Contains(out.String(), "local") {
		t.Fatal("check printed a value")
	}
}

func TestCheckFailsWhenAppKeyMissing(t *testing.T) {
	dir := t.TempDir()
	example := filepath.Join(dir, ".env.example")
	_ = os.WriteFile(example, []byte("PII_ENC_KEY=\n"), 0o600)
	t.Setenv("PII_ENC_KEY", "")
	var out bytes.Buffer
	if err := runCheck([]string{"-env", filepath.Join(dir, "none"), "-example", example}, &out); err == nil {
		t.Fatal("want error when an app key is missing")
	}
	if out.String() != "PII_ENC_KEY missing\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestCheckSeesProcessEnv(t *testing.T) {
	dir := t.TempDir()
	example := filepath.Join(dir, ".env.example")
	_ = os.WriteFile(example, []byte("ANTHROPIC_API_KEY=\n"), 0o600)
	t.Setenv("ANTHROPIC_API_KEY", "sentinel-123456789") // gitleaks:allow (test sentinel)
	var out bytes.Buffer
	_ = runCheck([]string{"-env", filepath.Join(dir, "none"), "-example", example}, &out)
	if out.String() != "ANTHROPIC_API_KEY set\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestLeakscanFindsEveryForm(t *testing.T) {
	p := tempEnv(t, "")
	if err := runGenerate([]string{"-env", p}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	ef, _ := readEnv(p)
	raw, _ := base64.StdEncoding.DecodeString(ef.Values["PII_HASH_KEY"])
	var s jwkSet
	_ = json.Unmarshal([]byte(ef.Values["JWT_SIGNING_KEYS"]), &s)
	seed, _ := base64.RawURLEncoding.DecodeString(s.Keys[0].D)
	cases := map[string]string{
		"raw":        "log line " + ef.Values["ORDER_CODE_ENC_KEY"] + " end",
		"hex":        "x=" + hex.EncodeToString(raw),
		"rawurl":     base64.RawURLEncoding.EncodeToString(raw),
		"jwk d":      "d=" + s.Keys[0].D,
		"jwk d hex":  hex.EncodeToString(seed),
		"whole json": ef.Values["JWT_SIGNING_KEYS"],
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "out.txt")
			_ = os.WriteFile(f, []byte(content), 0o600)
			var out bytes.Buffer
			if err := runLeakscan([]string{"-env", p, f}, &out); err == nil {
				t.Fatal("leak not found")
			}
			if !strings.HasPrefix(out.String(), "leak: ") {
				t.Fatalf("unexpected output %q", out.String())
			}
			// The report must not repeat the value it found.
			for _, n := range AppKeys {
				v := ef.Values[n]
				if len(v) >= 8 && strings.Contains(out.String(), v) {
					t.Fatal("leakscan printed a value")
				}
			}
		})
	}
}

func TestLeakscanCleanFile(t *testing.T) {
	p := tempEnv(t, "")
	_ = runGenerate([]string{"-env", p}, &bytes.Buffer{})
	f := filepath.Join(t.TempDir(), "out.txt")
	_ = os.WriteFile(f, []byte("generated: PII_ENC_KEY\nall good\n"), 0o600)
	var out bytes.Buffer
	if err := runLeakscan([]string{"-env", p, f}, &out); err != nil {
		t.Fatalf("false positive: %s", out.String())
	}
}

func TestReadEnvQuotesAndExport(t *testing.T) {
	p := tempEnv(t, "export A='x=y'\nB=\"q\"\nlower=1\n# C=2\nD=plain\n")
	ef, err := readEnv(p)
	if err != nil {
		t.Fatal(err)
	}
	if ef.Values["A"] != "x=y" || ef.Values["B"] != "q" || ef.Values["D"] != "plain" {
		t.Fatalf("parsed %v", ef.Order)
	}
	if _, ok := ef.Values["lower"]; ok {
		t.Fatal("lowercase names are not env vars here")
	}
	if _, ok := ef.Values["C"]; ok {
		t.Fatal("comment parsed")
	}
}

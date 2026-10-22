package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"todayz.local/e2e/mocks/internal/fixtures"
)

func embeddedFixtures(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(embedded, "fixtures")
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

// Every committed fixture loads and validates, in the embedded copy too.
func TestCommittedFixturesLoad(t *testing.T) {
	for name, fsys := range map[string]fs.FS{"embedded": embeddedFixtures(t), "dir": os.DirFS("fixtures")} {
		llm, err := fixtures.LoadLLM(fsys, fixtures.LLMOptions{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(llm.Errors) > 0 {
			t.Errorf("%s: broken LLM fixtures: %v", name, llm.Errors)
		}
		if len(llm.Fixtures) < 4 {
			t.Errorf("%s: %d LLM fixtures", name, len(llm.Fixtures))
		}
		stt, broken, err := fixtures.LoadSTT(fsys)
		if err != nil || len(broken) > 0 || len(stt) < 1 {
			t.Fatalf("%s: STT fixtures: %v %v (%d)", name, err, broken, len(stt))
		}
		for _, f := range []string{"sms/templates.json", "whatsapp/templates.json"} {
			if tpl, err := fixtures.LoadTemplates(fsys, f); err != nil || len(tpl) == 0 {
				t.Errorf("%s: %s: %v", name, f, err)
			}
		}
	}
}

// Each STT fixture's sha256 matches a committed audio file next to it.
func TestSTTFixtureHashesMatchAudio(t *testing.T) {
	stt, broken, err := fixtures.LoadSTT(os.DirFS("fixtures"))
	if err != nil || len(broken) > 0 {
		t.Fatal(err, broken)
	}
	audio, _ := filepath.Glob(filepath.Join("fixtures", "stt", "*.wav"))
	hashes := map[string]string{}
	for _, a := range audio {
		b, err := os.ReadFile(a)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		hashes[hex.EncodeToString(sum[:])] = a
	}
	for sum, fx := range stt {
		if _, ok := hashes[sum]; !ok {
			t.Errorf("%s: no audio file with sha256 %s", fx.Path, sum)
		}
	}
	fx := stt[findHash(t, hashes, "stock-update-hinglish.wav")]
	if fx.Transcript != "do kilo atta aur paanch Amul doodh bik gaye" || fx.LanguageCode != "hi-IN" {
		t.Errorf("sample = %+v", fx)
	}
}

func findHash(t *testing.T, hashes map[string]string, base string) string {
	for h, p := range hashes {
		if filepath.Base(p) == base {
			return h
		}
	}
	t.Fatalf("%s missing", base)
	return ""
}

// The binary never carries local recordings (or any "_" / "." path), so a
// recording can not change what a build serves.
func TestEmbeddedSkipsRecordings(t *testing.T) {
	err := fs.WalkDir(embedded, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if base := path.Base(p); p != "." && (strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".")) {
			t.Errorf("embedded: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(embedded, "fixtures/llm/_recorded"); err == nil {
		t.Error("fixtures/llm/_recorded is embedded")
	}
}

func TestFixturesSource(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	_, dir, err := fixturesFS(env(nil))
	if err != nil || dir != "" {
		t.Errorf("default should be embedded: %q %v", dir, err)
	}
	abs, _ := filepath.Abs("fixtures")
	_, dir, _ = fixturesFS(env(map[string]string{"MOCKS_FIXTURES_DIR": "fixtures"}))
	if dir != abs {
		t.Errorf("MOCKS_FIXTURES_DIR: %q", dir)
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	_, dir, _ = fixturesFS(env(map[string]string{"TODAYZ_ROOT": root}))
	if dir != filepath.Join(root, "e2e", "mocks", "fixtures") {
		t.Errorf("TODAYZ_ROOT: %q", dir)
	}
	_, dir, _ = fixturesFS(env(map[string]string{"TODAYZ_ROOT": t.TempDir()}))
	if dir != "" {
		t.Errorf("TODAYZ_ROOT without fixtures should fall back to embedded: %q", dir)
	}
	if _, _, err := fixturesFS(env(map[string]string{"MOCKS_FIXTURES_DIR": "/nonexistent-tz"})); err == nil {
		t.Error("bad MOCKS_FIXTURES_DIR accepted")
	}
}

// run serves on a free port, logs no key, and stops cleanly.
func TestRunServesAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var logs syncWriter
	ready := make(chan string, 1)
	done := make(chan error, 1)
	env := map[string]string{"MOCKS_ADDR": "127.0.0.1:0", "ANTHROPIC_API_KEY": "sk-ant-SENTINEL-run", "MOCK_LLM": "1"}
	go func() { done <- run(ctx, func(k string) string { return env[k] }, &logs, ready) }()

	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	var h map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&h)
	resp.Body.Close()
	if h["status"] != "ok" || h["llm_mode"] != "fixtures" || h["fixtures"] != "embedded" {
		t.Errorf("healthz = %v", h)
	}
	if fe, ok := h["fixture_errors"].([]any); !ok || len(fe) != 0 {
		t.Errorf("healthz fixture_errors = %v", h["fixture_errors"])
	}

	// Sensitive values through the real listener: none may reach the log.
	const (
		phone   = "919000000023"
		otp     = "736104"
		authkey = "authkey-SENTINEL-run-41"
		llmText = "llm-body-SENTINEL-run"
	)
	send := func(method, path, body string, hdr map[string]string) int {
		t.Helper()
		req, err := http.NewRequest(method, "http://"+addr+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := send("POST", "/msg91/api/v5/otp?template_id=tz_otp_login&mobile="+phone+"&otp="+otp, "", map[string]string{"authkey": authkey}); got != 200 {
		t.Errorf("send OTP = %d", got)
	}
	if got := send("GET", "/msg91/api/v5/otp/verify?mobile="+phone+"&otp="+otp, "", map[string]string{"authkey": authkey}); got != 200 {
		t.Errorf("verify OTP = %d", got)
	}
	llmBody := `{"model":"claude-sonnet-4-5","max_tokens":64,"messages":[{"role":"user","content":"` + llmText + `"}]}`
	send("POST", "/anthropic/v1/messages", llmBody, map[string]string{"x-api-key": "sk-ant-SENTINEL-run", "anthropic-version": "2023-06-01", "content-type": "application/json"})
	// A Host outside the allowlist is refused.
	req, _ := http.NewRequest("GET", "http://"+addr+"/healthz", nil)
	req.Host = "evil.example"
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("foreign Host: %v %v", resp, err)
	} else {
		resp.Body.Close()
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
	out := logs.String()
	for _, s := range []string{"SENTINEL", phone, otp, "9000000023", "evil.example"} {
		if strings.Contains(out, s) {
			t.Errorf("log output contains %q", s)
		}
	}
	// Every line, net/http's own included (ErrorLog), is a JSON object.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("log line is not JSON: %q", line)
		}
	}
	if !strings.Contains(out, `"route":"host-rejected"`) {
		t.Errorf("host rejection not logged: %s", out)
	}
	if !strings.Contains(out, "tz-mocks listening") || !strings.Contains(out, "tz-mocks stopped") {
		t.Errorf("logs = %s", out)
	}
}

type syncWriter struct {
	mu sync.Mutex
	b  strings.Builder
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

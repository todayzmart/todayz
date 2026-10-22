package server

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"todayz.local/e2e/mocks/internal/capture"
	"todayz.local/e2e/mocks/internal/fixtures"
)

type captureLLM = capture.LLM

var anthropicHeaders = map[string]string{"anthropic-version": "2023-06-01", "x-api-key": "test-placeholder"}

const msgPath = "/anthropic/v1/messages"

func voiceRequest(text string) map[string]any {
	return map[string]any{
		"model":      "claude-sonnet-4-5",
		"max_tokens": 1024,
		"system":     []map[string]any{{"type": "text", "text": "You turn a shopkeeper's spoken update into stock lines."}},
		"tools": []map[string]any{{
			"name":         "record_stock_lines",
			"description":  "Record stock lines",
			"input_schema": map[string]any{"type": "object"},
		}},
		"tool_choice": map[string]any{"type": "tool", "name": "record_stock_lines"},
		"metadata":    map[string]any{"user_id": "tz:voice_stock:store-0001"},
		"messages":    []map[string]any{{"role": "user", "content": text}},
	}
}

func TestAnthropicToolUseFixture(t *testing.T) {
	h := newHarness(t, nil)
	rr := h.postJSON(msgPath, voiceRequest("Transcript: do kilo atta aur paanch Amul doodh bik gaye"), anthropicHeaders)
	wantStatus(t, rr, 200)

	m := decode(t, rr)
	wantKeys(t, m, "id", "type", "role", "model", "content", "stop_reason", "stop_sequence", "usage")
	if !strings.HasPrefix(m["id"].(string), "msg_") || m["type"] != "message" || m["role"] != "assistant" ||
		m["model"] != "claude-sonnet-4-5" || m["stop_reason"] != "tool_use" || m["stop_sequence"] != nil {
		t.Errorf("message = %v", m)
	}
	wantKeys(t, obj(t, m["usage"]), "input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens", "service_tier")

	content := m["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content = %v", content)
	}
	block := obj(t, content[0])
	wantKeys(t, block, "type", "id", "name", "input")
	if block["type"] != "tool_use" || block["name"] != "record_stock_lines" || !strings.HasPrefix(block["id"].(string), "toolu_") {
		t.Errorf("block = %v", block)
	}
	lines := obj(t, block["input"])["lines"].([]any)
	if len(lines) != 2 || obj(t, lines[0])["unmatched_text"] != "atta" || obj(t, lines[1])["quantity"] != float64(5) {
		t.Errorf("lines = %v", lines)
	}

	for _, hdr := range []string{"request-id", "anthropic-ratelimit-requests-limit", "anthropic-ratelimit-requests-remaining",
		"anthropic-ratelimit-requests-reset", "anthropic-ratelimit-tokens-remaining", "anthropic-ratelimit-input-tokens-limit", "anthropic-ratelimit-output-tokens-reset"} {
		if rr.Header().Get(hdr) == "" {
			t.Errorf("missing header %s", hdr)
		}
	}
	if rr.Header().Get("X-TZ-Mock-Fixture") != "llm/voice_stock/stock-update-hinglish.json" {
		t.Errorf("fixture header = %q", rr.Header().Get("X-TZ-Mock-Fixture"))
	}
	rec := h.srv.Store().LLM.List(nil, 1)[0]
	if rec.Feature != "voice_stock" || rec.Mode != "fixture" || rec.Status != 200 || rec.StopReason != "tool_use" ||
		!strings.Contains(rec.Response, "record_stock_lines") || !strings.Contains(rec.Prompt, "do kilo atta") || len(rec.Tools) != 1 {
		t.Errorf("capture = %+v", rec)
	}
}

func TestAnthropicFixturePrecedence(t *testing.T) {
	h := newHarness(t, nil)
	// Same feature, text that the specific fixture does not match: generic fallback.
	rr := h.postJSON(msgPath, voiceRequest("teen packet Maggi bik gaye"), anthropicHeaders)
	wantStatus(t, rr, 200)
	if got := rr.Header().Get("X-TZ-Mock-Fixture"); got != "llm/voice_stock/fallback.json" {
		t.Errorf("fixture = %q", got)
	}
	// Matching text but the tool is missing: the tool rule fails, fallback again.
	req := voiceRequest("do kilo atta")
	delete(req, "tools")
	delete(req, "tool_choice")
	rr = h.postJSON(msgPath, req, anthropicHeaders)
	if got := rr.Header().Get("X-TZ-Mock-Fixture"); got != "llm/voice_stock/fallback.json" {
		t.Errorf("without tool, fixture = %q", got)
	}
	// Content as text blocks and tool_result still feeds the text matcher.
	req = voiceRequest("")
	req["messages"] = []map[string]any{
		{"role": "user", "content": []map[string]any{{"type": "text", "text": "Transcript follows."}}},
		{"role": "assistant", "content": []map[string]any{{"type": "tool_use", "id": "toolu_1", "name": "get_transcript", "input": map[string]any{}}}},
		{"role": "user", "content": []map[string]any{{"type": "tool_result", "tool_use_id": "toolu_1", "content": []map[string]any{{"type": "text", "text": "DO KILO ATTA aur doodh"}}}}},
	}
	rr = h.postJSON(msgPath, req, anthropicHeaders)
	if got := rr.Header().Get("X-TZ-Mock-Fixture"); got != "llm/voice_stock/stock-update-hinglish.json" {
		t.Errorf("tool_result text, fixture = %q", got)
	}
}

func TestAnthropicTextFixture(t *testing.T) {
	h := newHarness(t, nil)
	rr := h.postJSON(msgPath, map[string]any{
		"model": "claude-haiku-4-5", "max_tokens": 200,
		"metadata": map[string]any{"user_id": "tz:coach_card"},
		"messages": []map[string]any{{"role": "user", "content": "Write a coach card."}},
	}, anthropicHeaders)
	wantStatus(t, rr, 200)
	m := decode(t, rr)
	block := obj(t, m["content"].([]any)[0])
	wantKeys(t, block, "type", "text")
	if m["stop_reason"] != "end_turn" || !strings.HasPrefix(block["text"].(string), "Maggi and Amul milk") {
		t.Errorf("message = %v", m)
	}
}

func TestAnthropicErrors(t *testing.T) {
	h := newHarness(t, nil)
	base := func() map[string]any {
		return map[string]any{"model": "m", "max_tokens": 10, "metadata": map[string]any{"user_id": "tz:coach_card:1"},
			"messages": []map[string]any{{"role": "user", "content": "hi"}}}
	}
	type tc struct {
		name    string
		body    any
		headers map[string]string
		status  int
		typ     string
		msg     string
	}
	noFeature := base()
	delete(noFeature, "metadata")
	badTag := base()
	badTag["metadata"] = map[string]any{"user_id": "store-0001"}
	unknown := base()
	unknown["metadata"] = map[string]any{"user_id": "tz:shelf_photo:1"}
	noModel := base()
	delete(noModel, "model")
	noMax := base()
	delete(noMax, "max_tokens")
	noMsgs := base()
	noMsgs["messages"] = []any{}
	badRole := base()
	badRole["messages"] = []map[string]any{{"role": "system", "content": "x"}}
	overloaded := base()
	overloaded["metadata"] = map[string]any{"user_id": "tz:mock_overloaded:1"}

	cases := []tc{
		{"no version header", base(), map[string]string{"x-api-key": "k"}, 400, "invalid_request_error", "anthropic-version"},
		{"bad json", "{", anthropicHeaders, 400, "invalid_request_error", "Invalid JSON"},
		{"no model", noModel, anthropicHeaders, 400, "invalid_request_error", "model"},
		{"no max_tokens", noMax, anthropicHeaders, 400, "invalid_request_error", "max_tokens"},
		{"empty messages", noMsgs, anthropicHeaders, 400, "invalid_request_error", "messages"},
		{"bad role", badRole, anthropicHeaders, 400, "invalid_request_error", "role"},
		{"no feature tag", noFeature, anthropicHeaders, 400, "invalid_request_error", "tz:<feature>"},
		{"untagged user id", badTag, anthropicHeaders, 400, "invalid_request_error", "tz:<feature>"},
		{"unknown feature", unknown, anthropicHeaders, 400, "invalid_request_error", `feature "shelf_photo"`},
		{"error fixture", overloaded, anthropicHeaders, 529, "overloaded_error", "Overloaded"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := h.postJSON(msgPath, c.body, c.headers)
			wantStatus(t, rr, c.status)
			m := decode(t, rr)
			wantKeys(t, m, "type", "error", "request_id")
			e := obj(t, m["error"])
			wantKeys(t, e, "type", "message")
			if m["type"] != "error" || e["type"] != c.typ || !strings.Contains(e["message"].(string), c.msg) {
				t.Errorf("error = %v", m)
			}
			if rr.Header().Get("request-id") == "" {
				t.Error("request-id header missing")
			}
		})
	}
	misses := h.srv.Store().LLM.List(func(l captureLLM) bool { return l.Mode == "miss" }, 0)
	if len(misses) != 3 {
		t.Errorf("miss captures = %d", len(misses))
	}
}

func TestAnthropicStreaming(t *testing.T) {
	h := newHarness(t, nil)
	req := voiceRequest("do kilo atta aur paanch Amul doodh bik gaye")
	req["stream"] = true
	srv := httptest.NewServer(h.srv)
	defer srv.Close()
	resp := postTo(t, srv.URL+msgPath, req)
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	var events []string
	var partial strings.Builder
	var stop string
	sc := bufio.NewScanner(resp.Body)
	var event string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
			events = append(events, event)
		case strings.HasPrefix(line, "data: "):
			var d map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &d); err != nil {
				t.Fatalf("bad data line: %v", err)
			}
			if d["type"] != event {
				t.Errorf("data type %v under event %s", d["type"], event)
			}
			if event == "content_block_delta" {
				delta := obj(t, d["delta"])
				if delta["type"] == "input_json_delta" {
					partial.WriteString(delta["partial_json"].(string))
				}
			}
			if event == "message_delta" {
				stop, _ = obj(t, d["delta"])["stop_reason"].(string)
			}
		}
	}
	if events[0] != "message_start" || events[len(events)-1] != "message_stop" {
		t.Errorf("event order = %v", events)
	}
	for _, want := range []string{"ping", "content_block_start", "content_block_delta", "content_block_stop", "message_delta"} {
		if !contains(events, want) {
			t.Errorf("missing event %s in %v", want, events)
		}
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(partial.String()), &input); err != nil {
		t.Fatalf("reassembled tool input is not JSON: %v (%s)", err, partial.String())
	}
	if len(input["lines"].([]any)) != 2 || stop != "tool_use" {
		t.Errorf("input = %v, stop = %s", input, stop)
	}
	if rec := h.srv.Store().LLM.List(nil, 1)[0]; !rec.Stream {
		t.Error("stream flag not captured")
	}
}

func TestAnthropicCountTokens(t *testing.T) {
	h := newHarness(t, nil)
	rr := h.postJSON("/anthropic/v1/messages/count_tokens", voiceRequest("do kilo atta"), anthropicHeaders)
	wantStatus(t, rr, 200)
	m := decode(t, rr)
	wantKeys(t, m, "input_tokens")
	if m["input_tokens"].(float64) < 1 {
		t.Errorf("count = %v", m)
	}
	wantStatus(t, h.postJSON("/anthropic/v1/messages/count_tokens", `{"model":"m"}`, anthropicHeaders), 400)
	wantStatus(t, h.postJSON("/anthropic/v1/messages/count_tokens", voiceRequest("x"), nil), 400)
}

// --- MOCK_LLM gating and live/record mode ---

const sentinelKey = "sk-ant-SENTINEL-must-never-leave-0123456789"

// fakeUpstream stands in for api.anthropic.com. It counts calls and records
// the key it saw so the test can prove where the key went.
type fakeUpstream struct {
	srv     *httptest.Server
	calls   atomic.Int32
	lastKey atomic.Value
	status  atomic.Int32
}

func newFakeUpstream(t *testing.T, status int) *fakeUpstream {
	f := &fakeUpstream{}
	f.status.Store(int32(status))
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.lastKey.Store(r.Header.Get("x-api-key"))
		if r.URL.Path != "/v1/messages" || r.Header.Get("anthropic-version") == "" {
			http.Error(w, "bad upstream request", 400)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["stream"] == true {
			http.Error(w, "upstream must not be asked to stream", 400)
			return
		}
		w.Header().Set("request-id", "req_upstream_1")
		w.Header().Set("Content-Type", "application/json")
		if st := int(f.status.Load()); st != 200 {
			w.WriteHeader(st)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"},"request_id":"req_upstream_1"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"msg_live_1","type":"message","role":"assistant","model":"claude-sonnet-4-5",`+
			`"content":[{"type":"tool_use","id":"toolu_live_1","name":"record_stock_lines","input":{"lines":[{"action":"sale","unmatched_text":"chai patti","quantity":1,"unit":"pack","price":null}]}}],`+
			`"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":321,"output_tokens":45}}`)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigFromEnvGating(t *testing.T) {
	cases := []struct {
		env  map[string]string
		live bool
	}{
		{map[string]string{"ANTHROPIC_API_KEY": sentinelKey}, false},                      // MOCK_LLM unset
		{map[string]string{"ANTHROPIC_API_KEY": sentinelKey, "MOCK_LLM": "1"}, false},     // explicit mock
		{map[string]string{"ANTHROPIC_API_KEY": sentinelKey, "MOCK_LLM": "true"}, false},  // only "0" enables
		{map[string]string{"ANTHROPIC_API_KEY": sentinelKey, "MOCK_LLM": "false"}, false}, // only "0" enables
		{map[string]string{"MOCK_LLM": "0"}, false},                                       // no key
		{map[string]string{"MOCK_LLM": "0", "ANTHROPIC_API_KEY": sentinelKey}, true},
	}
	for _, c := range cases {
		cfg := ConfigFromEnv(envOf(c.env))
		if cfg.LLMLive != c.live {
			t.Errorf("env %v: live = %v, want %v", redactEnv(c.env), cfg.LLMLive, c.live)
		}
		if !c.live && cfg.AnthropicKey != "" {
			t.Errorf("env %v: the key must not be kept when not live", redactEnv(c.env))
		}
	}
}

func redactEnv(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if k == "ANTHROPIC_API_KEY" {
			v = "(set)"
		}
		out[k] = v
	}
	return out
}

// With a key in the environment but MOCK_LLM unset or "1", nothing is forwarded.
func TestMockLLMNeverForwardsWithoutOptIn(t *testing.T) {
	for _, env := range []map[string]string{
		{"ANTHROPIC_API_KEY": sentinelKey},
		{"ANTHROPIC_API_KEY": sentinelKey, "MOCK_LLM": "1"},
		{"MOCK_LLM": "0"},
	} {
		up := newFakeUpstream(t, 200)
		cfg := ConfigFromEnv(envOf(env))
		h := newHarness(t, func(c *Config) {
			c.LLMLive, c.AnthropicKey, c.AnthropicUpstream = cfg.LLMLive, cfg.AnthropicKey, up.srv.URL
			c.UpstreamClient = up.srv.Client()
			c.FixturesDir = t.TempDir()
		})
		rr := h.postJSON(msgPath, voiceRequest("do kilo atta"), anthropicHeaders)
		wantStatus(t, rr, 200)
		if rr.Header().Get("X-TZ-Mock-Fixture") == "" {
			t.Errorf("env %v: not answered from a fixture", redactEnv(env))
		}
		// An unmatched feature must be a 400, never a forward.
		rr = h.postJSON(msgPath, map[string]any{"model": "m", "max_tokens": 5, "metadata": map[string]any{"user_id": "tz:nothing_here"},
			"messages": []map[string]any{{"role": "user", "content": "x"}}}, anthropicHeaders)
		wantStatus(t, rr, 400)
		if n := up.calls.Load(); n != 0 {
			t.Errorf("env %v: upstream called %d times", redactEnv(env), n)
		}
		if strings.Contains(h.logs.String(), sentinelKey) {
			t.Error("key in logs")
		}
	}
}

func TestMockLLMLiveRecords(t *testing.T) {
	up := newFakeUpstream(t, 200)
	dir := t.TempDir()
	cfg := ConfigFromEnv(envOf(map[string]string{"MOCK_LLM": "0", "ANTHROPIC_API_KEY": sentinelKey}))
	h := newHarness(t, func(c *Config) {
		c.LLMLive, c.AnthropicKey, c.AnthropicUpstream = cfg.LLMLive, cfg.AnthropicKey, up.srv.URL+"/"
		c.UpstreamClient = up.srv.Client()
		c.Fixtures, c.FixturesDir = os.DirFS(dir), dir
	})
	req := voiceRequest("ek packet chai patti bik gaya")
	rr := h.postJSON(msgPath, req, map[string]string{"anthropic-version": "2023-06-01", "x-api-key": "client-side-key"})
	wantStatus(t, rr, 200)
	if up.calls.Load() != 1 {
		t.Fatalf("upstream calls = %d", up.calls.Load())
	}
	if up.lastKey.Load() != sentinelKey {
		t.Error("upstream must get the mock's own key, not the client's")
	}
	if m := decode(t, rr); m["id"] != "msg_live_1" {
		t.Errorf("live response not passed through: %v", m)
	}
	if rr.Header().Get("request-id") != "req_upstream_1" {
		t.Errorf("request-id = %q", rr.Header().Get("request-id"))
	}

	rel := rr.Header().Get("X-TZ-Mock-Recorded")
	if !strings.HasPrefix(rel, "llm/_recorded/voice_stock-") {
		t.Fatalf("recorded = %q", rel)
	}
	saved, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), sentinelKey) || strings.Contains(string(saved), "client-side-key") {
		t.Error("a key was written to the recording")
	}
	if strings.Contains(h.logs.String(), sentinelKey) || strings.Contains(h.logs.String(), "client-side-key") {
		t.Error("a key was logged")
	}
	capJSON, _ := json.Marshal(h.srv.Store().LLM.List(nil, 0))
	if strings.Contains(string(capJSON), sentinelKey) {
		t.Error("the key was captured")
	}

	// The recording is a valid fixture that matches the same request offline.
	set, err := fixtures.LoadLLM(os.DirFS(dir), fixtures.LLMOptions{IncludeRecorded: true})
	all := set.Fixtures
	if err != nil || len(set.Errors) > 0 || len(all) != 1 {
		t.Fatalf("recorded fixture does not load: %v %v (%d)", err, set.Errors, len(all))
	}
	if all[0].Match.Feature != "voice_stock" || all[0].Match.Tool != "record_stock_lines" || len(all[0].Match.Contains) != 1 {
		t.Errorf("recorded match = %+v", all[0].Match)
	}
	// Recordings are ignored by default ...
	off := newHarness(t, func(c *Config) { c.Fixtures = os.DirFS(dir) })
	wantStatus(t, off.postJSON(msgPath, req, anthropicHeaders), 400)
	// ... and replayed only when asked.
	off = newHarness(t, func(c *Config) { c.Fixtures, c.ReplayRecorded = os.DirFS(dir), true })
	rr = off.postJSON(msgPath, req, anthropicHeaders)
	wantStatus(t, rr, 200)
	if rr.Header().Get("X-TZ-Mock-Fixture") != rel {
		t.Errorf("replay fixture = %q, want %q", rr.Header().Get("X-TZ-Mock-Fixture"), rel)
	}

	// Streaming in live mode: upstream gets a non-streaming call, the client gets SSE.
	req["stream"] = true
	rr = h.postJSON(msgPath, req, anthropicHeaders)
	wantStatus(t, rr, 200)
	if !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(rr.Body.String(), "event: message_stop") {
		t.Errorf("live stream = %s", rr.Body.String())
	}
}

func TestMockLLMLivePassesErrorsAndSkipsRecordingWhenEmbedded(t *testing.T) {
	up := newFakeUpstream(t, 429)
	h := newHarness(t, func(c *Config) {
		c.LLMLive, c.AnthropicKey, c.AnthropicUpstream = true, sentinelKey, up.srv.URL
		c.UpstreamClient = up.srv.Client()
	})
	rr := h.postJSON(msgPath, voiceRequest("x"), anthropicHeaders)
	wantStatus(t, rr, 429)
	if e := obj(t, decode(t, rr)["error"]); e["type"] != "rate_limit_error" {
		t.Errorf("error = %v", e)
	}

	up.status.Store(200)
	rr = h.postJSON(msgPath, voiceRequest("x"), anthropicHeaders)
	wantStatus(t, rr, 200)
	if rr.Header().Get("X-TZ-Mock-Recorded") != "" {
		t.Error("embedded fixtures (no dir) must not record")
	}
	if rec := h.srv.Store().LLM.List(nil, 1)[0]; rec.Mode != "live" || rec.InputTokens != 321 {
		t.Errorf("capture = %+v", rec)
	}

	// Upstream down: a 502 in Anthropic's error shape.
	up.srv.Close()
	rr = h.postJSON(msgPath, voiceRequest("x"), anthropicHeaders)
	wantStatus(t, rr, 502)
	wantKeys(t, decode(t, rr), "type", "error", "request_id")
}

func postTo(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

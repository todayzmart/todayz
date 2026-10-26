package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// --- Host allowlist ---

func TestHostAllowlist(t *testing.T) {
	h := newHarness(t, nil)
	for _, host := range []string{
		"127.0.0.1", "127.0.0.1:4010", "localhost", "localhost:4999", "LOCALHOST:1",
		"[::1]", "[::1]:4010", "mocks.todayz.localhost", "mocks.todayz.localhost:443", "mocks.todayz.localhost.",
	} {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Host = host
		if rr := h.do(req); rr.Code != http.StatusOK {
			t.Errorf("Host %q: %d", host, rr.Code)
		}
	}
	for _, host := range []string{
		"evil.example", "evil.example:4010", "127.0.0.2", "0.0.0.0", "app.todayz.localhost",
		"todayz.localhost", "mocks.todayz.localhost.evil.example", "[::2]:4010", "",
	} {
		for _, path := range []string{"/healthz", "/_mock/sms", "/_mock/sms/latest-otp?to=919000000001"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = host
			rr := h.do(req)
			if rr.Code != http.StatusMisdirectedRequest {
				t.Errorf("Host %q %s: %d, want 421", host, path, rr.Code)
			}
			if strings.Contains(rr.Body.String(), "items") {
				t.Errorf("Host %q: data leaked: %s", host, rr.Body.String())
			}
		}
	}
	if strings.Contains(h.logs.String(), "evil.example") {
		t.Error("the rejected Host was logged")
	}
}

// --- Request bodies: 413 only when over the limit, 400 for other read errors ---

// bigReader yields n bytes of 'a' without holding them in memory.
type bigReader struct{ n int64 }

func (b *bigReader) Read(p []byte) (int, error) {
	if b.n <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > b.n {
		p = p[:b.n]
	}
	for i := range p {
		p[i] = 'a'
	}
	b.n -= int64(len(p))
	return len(p), nil
}

// brokenReader fails mid-body, like a client that drops the connection.
type brokenReader struct{ sent bool }

func (b *brokenReader) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, `{"a":"`), nil
	}
	return 0, errors.New("connection reset")
}

func TestBodyErrors(t *testing.T) {
	h := newHarness(t, nil)
	type tc struct {
		name, path, ct string
		hdr            map[string]string
		limit          int64
	}
	cases := []tc{
		{"llm", msgPath, "application/json", anthropicHeaders, llmBodyLimit},
		{"count_tokens", msgPath + "/count_tokens", "application/json", anthropicHeaders, llmBodyLimit},
		{"sms otp", "/msg91/api/v5/otp?mobile=919000000001", "application/json", msg91Auth_, smsBodyLimit},
		{"sms flow", "/msg91/api/v5/flow", "application/json", msg91Auth_, smsBodyLimit},
		{"wa bulk", "/whatsapp/api/v5/whatsapp/whatsapp-outbound-message/bulk/", "application/json", msg91Auth_, smsBodyLimit},
		{"wa session", "/whatsapp/api/v5/whatsapp/whatsapp-outbound-message/", "application/json", msg91Auth_, smsBodyLimit},
		{"gupshup", "/whatsapp/wa/api/v1/msg", "application/x-www-form-urlencoded", map[string]string{"apikey": "test-placeholder"}, smsBodyLimit},
		{"stt", "/sarvam/speech-to-text", "multipart/form-data; boundary=xyz", map[string]string{"api-subscription-key": "test-placeholder"}, sttBodyLimit},
		{"turnstile form", siteverify, "application/x-www-form-urlencoded", nil, 64 << 10},
		{"turnstile json", siteverify, "application/json", nil, 64 << 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			send := func(body io.Reader) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, c.path, body)
				req.Header.Set("Content-Type", c.ct)
				for k, v := range c.hdr {
					req.Header.Set(k, v)
				}
				return h.do(req)
			}
			// A prefix that parses, so the size limit (not the syntax) stops the read.
			prefix := `{"secret":"`
			if strings.HasPrefix(c.ct, "multipart/") {
				prefix = "--xyz\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.wav\"\r\n\r\n"
			} else if strings.HasPrefix(c.ct, "application/x-www") {
				prefix = "secret="
			}
			rr := send(io.MultiReader(strings.NewReader(prefix), &bigReader{n: c.limit + 1024}))
			if rr.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("over the limit: %d %s", rr.Code, rr.Body.String())
			}
			var m map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
				t.Errorf("413 body is not JSON: %s", rr.Body.String())
			}
			rr = send(&brokenReader{})
			want := http.StatusBadRequest
			if strings.HasPrefix(c.name, "turnstile") {
				want = http.StatusOK // Cloudflare answers bad input with 200 and "bad-request"
			}
			if rr.Code != want {
				t.Errorf("read error: %d, want %d: %s", rr.Code, want, rr.Body.String())
			}
		})
	}
	// Push keeps its plain-text errors (RFC 8030 servers do).
	req := pushReq("sub-1", []byte(strings.Repeat("a", maxPushPayload+1)), map[string]string{"TTL": "1", "Content-Encoding": "aes128gcm"})
	wantStatus(t, h.do(req), http.StatusRequestEntityTooLarge)
	req = pushReq("sub-1", nil, map[string]string{"TTL": "1", "Content-Encoding": "aes128gcm"})
	req.Body = io.NopCloser(&brokenReader{})
	wantStatus(t, h.do(req), http.StatusBadRequest)
}

// --- Config ---

func TestConfigIgnoresUpstreamOverride(t *testing.T) {
	cfg := ConfigFromEnv(envOf(map[string]string{"MOCK_LLM": "0", "ANTHROPIC_API_KEY": "x", "MOCK_LLM_UPSTREAM_URL": "http://127.0.0.1:1"}))
	if cfg.AnthropicUpstream != DefaultAnthropicUpstream || DefaultAnthropicUpstream != "https://api.anthropic.com" {
		t.Errorf("upstream = %q", cfg.AnthropicUpstream)
	}
	cfg = ConfigFromEnv(envOf(map[string]string{"MOCK_LLM_REPLAY_RECORDED": "1", "MOCK_TURNSTILE_HOSTNAME": "app.todayz.localhost"}))
	if !cfg.ReplayRecorded || cfg.TurnstileHostname != "app.todayz.localhost" {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg := ConfigFromEnv(envOf(nil)); cfg.ReplayRecorded {
		t.Error("replay must be off by default")
	}
}

// --- Turnstile hostname ---

func TestTurnstileHostname(t *testing.T) {
	for host, want := range map[string]string{"": "localhost", "app.todayz.localhost": "app.todayz.localhost"} {
		h := newHarness(t, func(c *Config) { c.TurnstileHostname = host })
		rr := h.postForm(siteverify, url.Values{"secret": {cfTestSecretPass}, "response": {"tok"}}, nil)
		wantStatus(t, rr, 200)
		if m := decode(t, rr); m["success"] != true || m["hostname"] != want {
			t.Errorf("hostname %q: %v", host, m)
		}
	}
}

// --- LLM streaming fidelity (fixtures from a MapFS) ---

type sse struct {
	event string
	data  map[string]any
}

func parseSSE(t *testing.T, body string) []sse {
	t.Helper()
	var out []sse
	for _, chunk := range strings.Split(strings.TrimSpace(body), "\n\n") {
		var ev sse
		for _, line := range strings.Split(chunk, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev.data); err != nil {
					t.Fatalf("bad data line %q", line)
				}
			}
		}
		out = append(out, ev)
	}
	return out
}

func featureRequest(feature, text string, stream bool) map[string]any {
	return map[string]any{
		"model": "claude-sonnet-4-5", "max_tokens": 256, "stream": stream,
		"metadata": map[string]any{"user_id": "tz:" + feature + ":t"},
		"messages": []map[string]any{{"role": "user", "content": text}},
	}
}

func mapFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for k, v := range files {
		m[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return m
}

func TestStreamThinkingPingAndError(t *testing.T) {
	fsys := mapFS(map[string]string{
		"llm/think/a.json": `{"match":{},"response":{"content":[` +
			`{"type":"thinking","thinking":"weigh the lines","signature":"sig-abc"},` +
			`{"type":"text","text":"done"}]}}`,
		"llm/flaky/a.json": `{"match":{},"stream_error_after":3,"response":{"content":[{"type":"text","text":"never finished"}]}}`,
	})
	h := newHarness(t, func(c *Config) { c.Fixtures = fsys })

	rr := h.postJSON(msgPath, featureRequest("think", "x", true), anthropicHeaders)
	wantStatus(t, rr, 200)
	ev := parseSSE(t, rr.Body.String())
	names := make([]string, len(ev))
	for i, e := range ev {
		names[i] = e.event
	}
	// message_start, then the first block's start, then ping.
	if len(ev) < 4 || names[0] != "message_start" || names[1] != "content_block_start" || names[2] != "ping" {
		t.Fatalf("order = %v", names)
	}
	if strings.Count(strings.Join(names, ","), "ping") != 1 {
		t.Errorf("one ping expected: %v", names)
	}
	start := obj(t, ev[1].data["content_block"])
	wantKeys(t, start, "type", "thinking", "signature")
	if start["type"] != "thinking" || start["signature"] != "" || start["thinking"] != "" {
		t.Errorf("thinking start = %v", start)
	}
	var thinking, sig string
	sigIdx, stopIdx := -1, -1
	for i, e := range ev {
		if e.event == "content_block_delta" && e.data["index"] == float64(0) {
			d := obj(t, e.data["delta"])
			switch d["type"] {
			case "thinking_delta":
				thinking += d["thinking"].(string)
			case "signature_delta":
				sig, sigIdx = d["signature"].(string), i
			}
		}
		if e.event == "content_block_stop" && e.data["index"] == float64(0) && stopIdx < 0 {
			stopIdx = i
		}
	}
	if thinking != "weigh the lines" || sig != "sig-abc" {
		t.Errorf("thinking %q signature %q", thinking, sig)
	}
	if sigIdx < 0 || sigIdx != stopIdx-1 {
		t.Errorf("signature_delta must be the last delta before the block stops: %v", names)
	}
	if names[len(names)-1] != "message_stop" {
		t.Errorf("last = %v", names)
	}

	// stream_error_after: three events, then an SSE error and no message_stop.
	rr = h.postJSON(msgPath, featureRequest("flaky", "x", true), anthropicHeaders)
	wantStatus(t, rr, 200)
	ev = parseSSE(t, rr.Body.String())
	if len(ev) != 4 || ev[3].event != "error" {
		t.Fatalf("flaky stream = %s", rr.Body.String())
	}
	if e := obj(t, ev[3].data["error"]); ev[3].data["type"] != "error" || e["type"] != "overloaded_error" {
		t.Errorf("error event = %v", ev[3].data)
	}
	if got := h.srv.Store().LLM.List(nil, 1)[0]; got.Error == "" {
		t.Error("capture should note the injected stream error")
	}
	// Without stream it is an ordinary reply.
	rr = h.postJSON(msgPath, featureRequest("flaky", "x", false), anthropicHeaders)
	wantStatus(t, rr, 200)
	if m := decode(t, rr); m["type"] != "message" {
		t.Errorf("non-stream = %v", m)
	}

	// An empty content list still pings right after message_start.
	empty := sseEvents(message{ID: "m", Model: "x", Usage: map[string]any{"output_tokens": 0}})
	if len(empty) < 2 || empty[0].name != "message_start" || empty[1].name != "ping" {
		t.Errorf("empty message events = %v", empty)
	}
}

// --- Fixture isolation ---

func TestBrokenFixtureIsolated(t *testing.T) {
	good := `{"match":{},"response":{"content":[{"type":"text","text":"ok"}]}}`
	fsys := mapFS(map[string]string{
		"llm/voice_stock/a.json":  good,
		"llm/coach_card/a.json":   good,
		"llm/coach_card/bad.json": `{"match":{},"response":`,
		"llm/stray/b.json":        `{"match":{"feature":"other_feature"},"response":{"content":[]}}`,
	})
	h := newHarness(t, func(c *Config) { c.Fixtures = fsys })

	// Another feature's broken file does not matter.
	wantStatus(t, h.postJSON(msgPath, featureRequest("voice_stock", "x", false), anthropicHeaders), 200)
	// The same feature's broken file does, even though a good one would match.
	rr := h.postJSON(msgPath, featureRequest("coach_card", "x", false), anthropicHeaders)
	wantStatus(t, rr, 500)
	if e := obj(t, decode(t, rr)["error"]); e["type"] != "api_error" || !strings.Contains(e["message"].(string), "llm/coach_card/bad.json") {
		t.Errorf("error = %v", e)
	}
	// A file is attributed to match.feature when it parses that far.
	wantStatus(t, h.postJSON(msgPath, featureRequest("other_feature", "x", false), anthropicHeaders), 500)
	wantStatus(t, h.postJSON(msgPath, featureRequest("stray", "x", false), anthropicHeaders), 400)

	rr = h.get("/healthz")
	wantStatus(t, rr, 200)
	var hz struct {
		Status        string   `json:"status"`
		FixtureErrors []string `json:"fixture_errors"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &hz); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(hz.FixtureErrors, "\n")
	if hz.Status != "ok" || !strings.Contains(joined, "llm/coach_card/bad.json") || !strings.Contains(joined, "llm/stray/b.json") {
		t.Errorf("healthz = %s", rr.Body.String())
	}
	if strings.Contains(joined, "voice_stock") {
		t.Errorf("good files listed: %s", joined)
	}

	// The committed tree has no broken files.
	h = newHarness(t, nil)
	if !strings.Contains(h.get("/healthz").Body.String(), `"fixture_errors":[]`) {
		t.Errorf("committed fixtures report errors: %s", h.get("/healthz").Body.String())
	}
}

// A recording never changes fixture-mode results unless replay is on.
func TestRecordingDoesNotOverrideCurated(t *testing.T) {
	fsys := mapFS(map[string]string{
		"llm/voice_stock/a.json": `{"match":{"contains":["chai"]},"response":{"content":[{"type":"text","text":"curated"}]}}`,
		"llm/_recorded/voice_stock-1.json": `{"match":{"feature":"voice_stock","contains":["chai"],"tool":"record_voice_stock_lines"},` +
			`"response":{"content":[{"type":"text","text":"recorded"}]}}`,
	})
	for replay, want := range map[bool]string{false: "llm/voice_stock/a.json", true: "llm/_recorded/voice_stock-1.json"} {
		h := newHarness(t, func(c *Config) { c.Fixtures, c.ReplayRecorded = fsys, replay })
		rr := h.postJSON(msgPath, voiceRequest("ek chai"), anthropicHeaders)
		wantStatus(t, rr, 200)
		if got := rr.Header().Get("X-TZ-Mock-Fixture"); got != want {
			t.Errorf("replay=%v: fixture %q, want %q", replay, got, want)
		}
	}
	// A broken recording is not even read when replay is off.
	fsys["llm/_recorded/voice_stock-2.json"] = &fstest.MapFile{Data: []byte("{")}
	h := newHarness(t, func(c *Config) { c.Fixtures = fsys })
	wantStatus(t, h.postJSON(msgPath, voiceRequest("ek chai"), anthropicHeaders), 200)
	if !strings.Contains(h.get("/healthz").Body.String(), `"fixture_errors":[]`) {
		t.Errorf("healthz reports a recording while replay is off: %s", h.get("/healthz").Body.String())
	}
}

// An upstream reply the fixture loader would refuse is passed through but not
// written, and the capture says why.
func TestInvalidRecordingNotWritten(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_live_2","type":"message","role":"assistant","model":"claude-sonnet-4-5",`+
			`"content":[{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"x"}}],`+
			`"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":3,"output_tokens":4}}`)
	}))
	t.Cleanup(up.Close)
	dir := t.TempDir()
	h := newHarness(t, func(c *Config) {
		c.LLMLive, c.AnthropicKey, c.AnthropicUpstream = true, sentinelKey, up.URL
		c.UpstreamClient = up.Client()
		c.Fixtures, c.FixturesDir = os.DirFS(dir), dir
	})
	rr := h.postJSON(msgPath, voiceRequest("x"), anthropicHeaders)
	wantStatus(t, rr, 200)
	if rr.Header().Get("X-TZ-Mock-Recorded") != "" {
		t.Error("an invalid recording was announced")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "llm", "_recorded", "*"))
	if len(files) != 0 {
		t.Errorf("files written: %v", files)
	}
	rec := h.srv.Store().LLM.List(nil, 1)[0]
	if !strings.Contains(rec.Error, "not recorded") || !strings.Contains(rec.Error, "server_tool_use") {
		t.Errorf("capture error = %q", rec.Error)
	}
}

// --- Strict phones on every provider route ---

func TestProviderRoutesNeedCountryCode(t *testing.T) {
	h := newHarness(t, nil)
	gup := map[string]string{"apikey": "test-placeholder"}
	bulk := func(from, to string) any {
		return map[string]any{"integrated_number": from, "content_type": "template", "payload": map[string]any{
			"type": "template", "template": map[string]any{"name": "tz_order_ready", "language": map[string]any{"code": "en"},
				"to_and_components": []map[string]any{{"to": []string{to}, "components": map[string]any{}}}}}}
	}
	session := func(from, to string) any {
		return map[string]any{"integrated_number": from, "recipient_number": to, "content_type": "text", "text": "hi"}
	}
	const wa = "/whatsapp/api/v5/whatsapp/whatsapp-outbound-message/"
	for _, c := range []struct {
		name string
		do   func(from, to string) *httptest.ResponseRecorder
		ok   int
	}{
		{"wa bulk", func(f, to string) *httptest.ResponseRecorder { return h.postJSON(wa+"bulk/", bulk(f, to), msg91Auth_) }, 200},
		{"wa session", func(f, to string) *httptest.ResponseRecorder { return h.postJSON(wa, session(f, to), msg91Auth_) }, 200},
		{"gupshup msg", func(f, to string) *httptest.ResponseRecorder {
			return h.postForm("/whatsapp/wa/api/v1/msg", url.Values{"source": {f}, "destination": {to}, "message": {"hi"}}, gup)
		}, 202},
		{"gupshup template", func(f, to string) *httptest.ResponseRecorder {
			return h.postForm("/whatsapp/wa/api/v1/template/msg", url.Values{"source": {f}, "destination": {to}, "template": {`{"id":"x","params":[]}`}}, gup)
		}, 202},
		{"sms flow", func(_, to string) *httptest.ResponseRecorder {
			return h.postJSON("/msg91/api/v5/flow", map[string]any{"template_id": "x", "recipients": []map[string]any{{"mobiles": to}}}, msg91Auth_)
		}, 200},
		{"sms otp", func(_, to string) *httptest.ResponseRecorder {
			return h.postJSON("/msg91/api/v5/otp?mobile="+to, "", msg91Auth_)
		}, 200},
		{"sms retry", func(_, to string) *httptest.ResponseRecorder {
			return h.postJSON("/msg91/api/v5/otp/retry?mobile="+to, "", msg91Auth_)
		}, 200},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := h.srv.Store().Counts()
			for _, to := range []string{"9000000031", "09000000031", "919000000", "91900000003100"} {
				if rr := c.do("919000000050", to); rr.Code != http.StatusBadRequest {
					t.Errorf("to %q: %d %s", to, rr.Code, rr.Body.String())
				}
			}
			if c.name != "sms flow" && !strings.HasPrefix(c.name, "sms") {
				if rr := c.do("9000000050", "919000000031"); rr.Code != http.StatusBadRequest {
					t.Errorf("sender without country code: %d", rr.Code)
				}
			}
			after := h.srv.Store().Counts()
			if before["sms"] != after["sms"] || before["whatsapp"] != after["whatsapp"] {
				t.Errorf("refused sends were captured: %v -> %v", before, after)
			}
			if rr := c.do("919000000050", "919000000031"); rr.Code != c.ok {
				t.Errorf("valid: %d %s", rr.Code, rr.Body.String())
			}
		})
	}
	// /_mock filters stay lenient: a bare 10-digit number finds the +91 entries.
	if items := decodeItems(t, h.get("/_mock/whatsapp?to=9000000031")); len(items) != 4 {
		t.Errorf("lenient filter found %d", len(items))
	}
	if items := decodeItems(t, h.get("/_mock/sms?to=09000000031")); len(items) != 3 {
		t.Errorf("lenient sms filter found %d", len(items))
	}
}

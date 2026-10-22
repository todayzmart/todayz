package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"todayz.local/e2e/mocks/internal/capture"
	"todayz.local/e2e/mocks/internal/fixtures"
)

// Messages API: https://docs.anthropic.com/en/api/messages
// Errors:       https://docs.anthropic.com/en/api/errors
// Streaming:    https://docs.anthropic.com/en/docs/build-with-claude/streaming

const llmBodyLimit = 32 << 20 // the real API's request size limit

// featureTagRe reads the feature from metadata.user_id = "tz:<feature>:...".
var featureTagRe = regexp.MustCompile(`^tz:([a-z0-9_]+)(?::|$)`)

type msgParam struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type msgRequest struct {
	Model     string          `json:"model"`
	MaxTokens *int            `json:"max_tokens"`
	Messages  []msgParam      `json:"messages"`
	System    json.RawMessage `json:"system"`
	Tools     []struct {
		Name string `json:"name"`
	} `json:"tools"`
	ToolChoice *struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"tool_choice"`
	Metadata *struct {
		UserID string `json:"user_id"`
	} `json:"metadata"`
	Stream bool `json:"stream"`
}

// message is the Messages API response object.
type message struct {
	ID           string           `json:"id"`
	Type         string           `json:"type"`
	Role         string           `json:"role"`
	Model        string           `json:"model"`
	Content      []map[string]any `json:"content"`
	StopReason   string           `json:"stop_reason"`
	StopSequence *string          `json:"stop_sequence"`
	Usage        map[string]any   `json:"usage"`
}

var anthropicErrorTypes = map[int]string{
	400: "invalid_request_error", 401: "authentication_error", 403: "permission_error",
	404: "not_found_error", 413: "request_too_large", 429: "rate_limit_error",
	500: "api_error", 502: "api_error", 529: "overloaded_error",
}

func anthropicError(w http.ResponseWriter, status int, typ, msg, reqID string) {
	if typ == "" {
		typ = anthropicErrorTypes[status]
		if typ == "" {
			typ = "api_error"
		}
	}
	w.Header().Set("request-id", reqID)
	if status == http.StatusTooManyRequests {
		w.Header().Set("retry-after", "1")
	}
	writeJSON(w, status, map[string]any{
		"type":       "error",
		"error":      map[string]string{"type": typ, "message": msg},
		"request_id": reqID,
	})
}

// setAnthropicHeaders sets the response headers SDKs read.
func (s *Server) setAnthropicHeaders(w http.ResponseWriter, reqID string) {
	h := w.Header()
	h.Set("request-id", reqID)
	h.Set("anthropic-organization-id", "00000000-0000-4000-8000-000000000000")
	reset := s.cfg.Now().UTC().Add(time.Minute).Format(time.RFC3339)
	for _, kind := range []string{"requests", "tokens", "input-tokens", "output-tokens"} {
		limit := "4000"
		if kind != "requests" {
			limit = "400000"
		}
		h.Set("anthropic-ratelimit-"+kind+"-limit", limit)
		h.Set("anthropic-ratelimit-"+kind+"-remaining", limit)
		h.Set("anthropic-ratelimit-"+kind+"-reset", reset)
	}
}

// textOf flattens a content value (string or block array) into text.
func textOf(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}
	}
	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Input   json.RawMessage `json:"input"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	var out []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			out = append(out, b.Text)
		case "tool_use":
			out = append(out, string(b.Input))
		case "tool_result":
			out = append(out, textOf(b.Content)...)
		}
	}
	return out
}

func (req *msgRequest) validate() string {
	switch {
	case req.Model == "":
		return "model: Field required"
	case req.MaxTokens == nil:
		return "max_tokens: Field required"
	case *req.MaxTokens < 1:
		return "max_tokens: Input should be greater than or equal to 1"
	case req.Messages == nil:
		return "messages: Field required"
	case len(req.Messages) == 0:
		return "messages: at least one message is required"
	}
	for i, m := range req.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			return fmt.Sprintf("messages.%d.role: Input should be 'user' or 'assistant'", i)
		}
		if len(m.Content) == 0 {
			return fmt.Sprintf("messages.%d.content: Field required", i)
		}
	}
	return ""
}

func (req *msgRequest) feature() string {
	if req.Metadata == nil {
		return ""
	}
	m := featureTagRe.FindStringSubmatch(req.Metadata.UserID)
	if m == nil {
		return ""
	}
	return m[1]
}

func (req *msgRequest) toolNames() []string {
	out := make([]string, 0, len(req.Tools))
	for _, t := range req.Tools {
		if t.Name != "" {
			out = append(out, t.Name)
		}
	}
	return out
}

func (req *msgRequest) allText() string {
	parts := textOf(req.System)
	for _, m := range req.Messages {
		parts = append(parts, textOf(m.Content)...)
	}
	return strings.Join(parts, "\n")
}

func (req *msgRequest) lastUserText() string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return strings.Join(textOf(req.Messages[i].Content), "\n")
		}
	}
	return ""
}

// summarize turns content blocks into one line for the capture UI.
func summarize(content []map[string]any) string {
	var parts []string
	for _, b := range content {
		switch b["type"] {
		case "text":
			t, _ := b["text"].(string)
			parts = append(parts, t)
		case "tool_use":
			in, _ := json.Marshal(b["input"])
			name, _ := b["name"].(string)
			parts = append(parts, name+" "+string(in))
		default:
			parts = append(parts, fmt.Sprintf("[%v]", b["type"]))
		}
	}
	return truncate(strings.Join(parts, "\n"), 2000)
}

func intOf(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	}
	return 0
}

// POST /v1/messages
func (s *Server) anthropicMessages(w http.ResponseWriter, r *http.Request) {
	reqID := "req_mock_" + randomHex(12)
	rec := capture.LLM{Mode: "invalid"}
	fail := func(status int, typ, msg string) {
		rec.Meta, rec.Status, rec.Error = s.store.Next(), status, msg
		s.store.LLM.Add(rec)
		anthropicError(w, status, typ, msg, reqID)
	}
	if r.Header.Get("anthropic-version") == "" {
		fail(http.StatusBadRequest, "", "anthropic-version: header is required")
		return
	}
	raw, status := readBody(w, r, llmBodyLimit)
	if status != 0 {
		fail(status, "", llmBodyErrText(status))
		return
	}
	var req msgRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		fail(http.StatusBadRequest, "", "Invalid JSON in request body: "+err.Error())
		return
	}
	rec.Feature, rec.Model, rec.Stream, rec.Tools = req.feature(), req.Model, req.Stream, req.toolNames()
	rec.Prompt = truncate(req.lastUserText(), 1000)
	if msg := req.validate(); msg != "" {
		fail(http.StatusBadRequest, "", msg)
		return
	}

	if s.cfg.LLMLive {
		s.forwardLLM(w, r, raw, &req, &rec, reqID)
		return
	}

	set, err := s.loadLLM()
	if err != nil {
		fail(http.StatusInternalServerError, "api_error", "tz-mock: LLM fixtures unreadable: "+err.Error())
		return
	}
	rec.Mode = "miss"
	if rec.Feature == "" {
		fail(http.StatusBadRequest, "", `tz-mock: no feature tag; set metadata.user_id to "tz:<feature>:<id>" so the mock can pick a fixture`)
		return
	}
	// A broken file for this feature could be the one that should answer, so
	// say so instead of guessing. Other features are not affected.
	if broken := set.BrokenFor(rec.Feature); len(broken) > 0 {
		fail(http.StatusInternalServerError, "api_error", "tz-mock: broken LLM fixture for feature "+rec.Feature+": "+joinErrors(broken))
		return
	}
	fx := fixtures.MatchLLM(set.Fixtures, fixtures.LLMRequest{Feature: rec.Feature, Text: req.allText(), Tools: rec.Tools})
	if fx == nil {
		if unknown := set.BrokenFor(""); len(unknown) > 0 {
			fail(http.StatusInternalServerError, "api_error", fmt.Sprintf(
				"tz-mock: no LLM fixture matches feature %q, and fixture files whose feature is unknown failed to load: %s",
				rec.Feature, joinErrors(unknown)))
			return
		}
		fail(http.StatusBadRequest, "", fmt.Sprintf(
			"tz-mock: no LLM fixture matches feature %q (tools %v); add one under e2e/mocks/fixtures/llm/%s/",
			rec.Feature, rec.Tools, rec.Feature))
		return
	}
	rec.Mode, rec.Fixture = "fixture", fx.Path
	w.Header().Set("X-TZ-Mock-Fixture", fx.Path)
	if fx.Error != nil {
		fail(fx.Error.Status, fx.Error.Type, fx.Error.Message)
		return
	}

	msg := s.buildMessage(fx.Response, &req, len(raw))
	rec.Meta, rec.Status, rec.StopReason = s.store.Next(), http.StatusOK, msg.StopReason
	rec.Response = summarize(msg.Content)
	rec.InputTokens, rec.OutputTokens = intOf(msg.Usage["input_tokens"]), intOf(msg.Usage["output_tokens"])
	if req.Stream && fx.StreamErrorAfter > 0 {
		rec.Error = fmt.Sprintf("stream error after %d events (fixture)", fx.StreamErrorAfter)
	}
	s.store.LLM.Add(rec)
	s.writeMessage(w, msg, req.Stream, reqID, fx.StreamErrorAfter)
}

func llmBodyErrText(status int) string {
	if status == http.StatusRequestEntityTooLarge {
		return "Request exceeds the maximum allowed number of bytes."
	}
	return "Unable to read the request body."
}

func joinErrors(errs []fixtures.FileError) string {
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}

func (s *Server) buildMessage(resp *fixtures.LLMResponse, req *msgRequest, reqBytes int) message {
	id := randomHex(12)
	content := make([]map[string]any, len(resp.Content))
	hasTool := false
	for i, b := range resp.Content {
		c := make(map[string]any, len(b)+1)
		for k, v := range b {
			c[k] = v
		}
		if c["type"] == "tool_use" {
			hasTool = true
			if _, ok := c["id"]; !ok {
				c["id"] = fmt.Sprintf("toolu_mock_%s_%d", id, i)
			}
		}
		content[i] = c
	}
	stop := resp.StopReason
	if stop == "" {
		stop = "end_turn"
		if hasTool {
			stop = "tool_use"
		}
	}
	out, _ := json.Marshal(content)
	usage := map[string]any{
		"input_tokens":                max(1, reqBytes/4),
		"output_tokens":               max(1, len(out)/4),
		"cache_creation_input_tokens": 0,
		"cache_read_input_tokens":     0,
		"service_tier":                "standard",
	}
	for k, v := range resp.Usage {
		usage[k] = v
	}
	return message{
		ID: "msg_mock_" + id, Type: "message", Role: "assistant", Model: req.Model,
		Content: content, StopReason: stop, StopSequence: resp.StopSequence, Usage: usage,
	}
}

func (s *Server) writeMessage(w http.ResponseWriter, msg message, stream bool, reqID string, errorAfter int) {
	s.setAnthropicHeaders(w, reqID)
	if !stream {
		writeJSON(w, http.StatusOK, msg)
		return
	}
	writeSSE(w, msg, errorAfter)
}

type sseEvent struct {
	name string
	data any
}

// writeSSE replays a complete message as the Messages streaming events. With
// errorAfter > 0 it sends that many events, then an overloaded_error "error"
// event, and ends the stream (a mid-stream failure, as the API can do).
func writeSSE(w http.ResponseWriter, msg message, errorAfter int) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	write := func(event string, data any) {
		b, _ := json.Marshal(data)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		_ = rc.Flush()
	}
	for i, e := range sseEvents(msg) {
		if errorAfter > 0 && i == errorAfter {
			write("error", map[string]any{"type": "error", "error": map[string]any{"type": "overloaded_error", "message": "Overloaded"}})
			return
		}
		write(e.name, e.data)
	}
}

// sseEvents lists a message's stream in the API's order: message_start, the
// first content_block_start, ping, deltas, content_block_stop, ...,
// message_delta, message_stop.
func sseEvents(msg message) []sseEvent {
	var ev []sseEvent
	add := func(name string, data any) { ev = append(ev, sseEvent{name, data}) }
	ping := func() { add("ping", map[string]any{"type": "ping"}) }

	startUsage := map[string]any{}
	for k, v := range msg.Usage {
		startUsage[k] = v
	}
	startUsage["output_tokens"] = 1
	add("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": msg.ID, "type": "message", "role": "assistant", "model": msg.Model,
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": startUsage,
	}})
	if len(msg.Content) == 0 {
		ping()
	}
	for i, b := range msg.Content {
		start := func(block map[string]any) {
			add("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": block})
			if i == 0 {
				ping()
			}
		}
		delta := func(d map[string]any) {
			add("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": d})
		}
		switch b["type"] {
		case "text":
			start(map[string]any{"type": "text", "text": ""})
			t, _ := b["text"].(string)
			for _, c := range chunks(t, 32) {
				delta(map[string]any{"type": "text_delta", "text": c})
			}
		case "tool_use":
			start(map[string]any{"type": "tool_use", "id": b["id"], "name": b["name"], "input": map[string]any{}})
			in, _ := json.Marshal(b["input"])
			for _, c := range chunks(string(in), 32) {
				delta(map[string]any{"type": "input_json_delta", "partial_json": c})
			}
		case "thinking":
			start(map[string]any{"type": "thinking", "thinking": "", "signature": ""})
			t, _ := b["thinking"].(string)
			for _, c := range chunks(t, 32) {
				delta(map[string]any{"type": "thinking_delta", "thinking": c})
			}
			if sig, _ := b["signature"].(string); sig != "" {
				delta(map[string]any{"type": "signature_delta", "signature": sig})
			}
		default:
			start(b)
		}
		add("content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
	}
	add("message_delta", map[string]any{"type": "message_delta",
		"delta": map[string]any{"stop_reason": msg.StopReason, "stop_sequence": msg.StopSequence},
		"usage": map[string]any{"output_tokens": msg.Usage["output_tokens"]}})
	add("message_stop", map[string]any{"type": "message_stop"})
	return ev
}

// chunks splits s into pieces of at most n bytes on rune boundaries.
func chunks(s string, n int) []string {
	var out []string
	for len(s) > n {
		cut := n
		for cut > 0 && !utf8Start(s[cut]) {
			cut--
		}
		if cut == 0 {
			cut = n
		}
		out = append(out, s[:cut])
		s = s[cut:]
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}

// forwardLLM is live/record mode: call the real API with the mock's own key,
// save a 200 response as a fixture under fixtures/llm/_recorded/, and return
// it to the client. The key is sent upstream only; it is never logged or stored.
func (s *Server) forwardLLM(w http.ResponseWriter, r *http.Request, raw []byte, req *msgRequest, rec *capture.LLM, reqID string) {
	rec.Mode = "live"
	body := raw
	if req.Stream {
		// Ask upstream for a complete message; the mock replays it as SSE.
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) == nil {
			m["stream"] = json.RawMessage("false")
			body, _ = json.Marshal(m)
		}
	}
	up, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.cfg.AnthropicUpstream+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		s.liveFail(w, rec, reqID, http.StatusBadGateway, "tz-mock: bad upstream URL")
		return
	}
	up.Header.Set("Content-Type", "application/json")
	up.Header.Set("x-api-key", s.cfg.AnthropicKey)
	up.Header.Set("anthropic-version", r.Header.Get("anthropic-version"))
	if b := r.Header.Get("anthropic-beta"); b != "" {
		up.Header.Set("anthropic-beta", b)
	}
	resp, err := s.cfg.UpstreamClient.Do(up)
	if err != nil {
		s.liveFail(w, rec, reqID, http.StatusBadGateway, "tz-mock: upstream request failed")
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, llmBodyLimit))
	if err != nil {
		s.liveFail(w, rec, reqID, http.StatusBadGateway, "tz-mock: upstream response unreadable")
		return
	}
	for k, vs := range resp.Header {
		lk := strings.ToLower(k)
		if lk == "request-id" || lk == "retry-after" || strings.HasPrefix(lk, "anthropic-ratelimit-") || lk == "anthropic-organization-id" {
			w.Header()[k] = vs
		}
	}
	upID := resp.Header.Get("request-id")
	if upID == "" {
		upID = reqID
	}
	if resp.StatusCode != http.StatusOK {
		rec.Meta, rec.Status, rec.Error = s.store.Next(), resp.StatusCode, truncate(string(respBody), 500)
		s.store.LLM.Add(*rec)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}
	var msg message
	if err := json.Unmarshal(respBody, &msg); err != nil {
		s.liveFail(w, rec, reqID, http.StatusBadGateway, "tz-mock: upstream response is not a message")
		return
	}
	switch path, err := s.saveRecording(req, &msg); {
	case err != nil:
		// The client still gets the live answer; only the recording is skipped.
		rec.Error = "not recorded: " + err.Error()
	case path != "":
		rec.Recorded = path
		w.Header().Set("X-TZ-Mock-Recorded", path)
	}
	rec.Meta, rec.Status, rec.StopReason = s.store.Next(), http.StatusOK, msg.StopReason
	rec.Response = summarize(msg.Content)
	rec.InputTokens, rec.OutputTokens = intOf(msg.Usage["input_tokens"]), intOf(msg.Usage["output_tokens"])
	s.store.LLM.Add(*rec)
	if req.Stream {
		s.writeMessage(w, msg, true, upID, 0)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody)
}

func (s *Server) liveFail(w http.ResponseWriter, rec *capture.LLM, reqID string, status int, msg string) {
	rec.Meta, rec.Status, rec.Error = s.store.Next(), status, msg
	s.store.LLM.Add(*rec)
	anthropicError(w, status, "api_error", msg, reqID)
}

// saveRecording writes msg as a fixture under llm/_recorded/. It returns
// ("", nil) when recording is off (embedded fixtures), the relative path on
// success, and an error when the recording is invalid (nothing is written:
// it would not load, for example a block type fixtures do not support) or
// the file cannot be written.
func (s *Server) saveRecording(req *msgRequest, msg *message) (string, error) {
	dir := s.fixturesRecordDir()
	if dir == "" {
		return "", nil
	}
	feature := req.feature()
	if feature == "" {
		feature = "untagged"
	}
	fx := fixtures.LLMFixture{
		Description: fmt.Sprintf("Recorded from the live Messages API at %s (model %s). Review before committing.",
			s.cfg.Now().UTC().Format(time.RFC3339), msg.Model),
		Match:    fixtures.LLMMatch{Feature: feature},
		Response: &fixtures.LLMResponse{Content: msg.Content, StopReason: msg.StopReason, StopSequence: msg.StopSequence, Usage: msg.Usage},
	}
	if snippet := strings.TrimSpace(truncate(req.lastUserText(), 120)); snippet != "" {
		fx.Match.Contains = []string{strings.TrimSuffix(snippet, "…")}
	}
	if req.ToolChoice != nil && req.ToolChoice.Type == "tool" && req.ToolChoice.Name != "" {
		fx.Match.Tool = req.ToolChoice.Name
	}
	b, err := json.MarshalIndent(fx, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode recording: %w", err)
	}
	name := fmt.Sprintf("%s-%s-%s.json", feature, s.cfg.Now().UTC().Format("20060102T150405Z"), randomHex(3))
	rel := "llm/_recorded/" + name
	// Parse it back exactly as the loader will, so a bad recording is refused
	// here instead of breaking the fixtures later.
	if _, err := fixtures.ParseLLM(rel, b); err != nil {
		return "", fmt.Errorf("invalid recording: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errors.New("cannot create the recordings folder")
	}
	tmp, err := os.CreateTemp(dir, ".rec-*")
	if err != nil {
		return "", errors.New("cannot create the recording file")
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", errors.New("cannot write the recording file")
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", errors.New("cannot write the recording file")
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp.Name())
		return "", errors.New("cannot move the recording into place")
	}
	return rel, nil
}

// POST /v1/messages/count_tokens — always answered locally (never forwarded),
// with a rough estimate of 1 token per 4 bytes of request.
func (s *Server) anthropicCountTokens(w http.ResponseWriter, r *http.Request) {
	reqID := "req_mock_" + randomHex(12)
	if r.Header.Get("anthropic-version") == "" {
		anthropicError(w, http.StatusBadRequest, "", "anthropic-version: header is required", reqID)
		return
	}
	raw, status := readBody(w, r, llmBodyLimit)
	if status != 0 {
		anthropicError(w, status, "", llmBodyErrText(status), reqID)
		return
	}
	var req msgRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		anthropicError(w, http.StatusBadRequest, "", "Invalid JSON in request body: "+err.Error(), reqID)
		return
	}
	if req.Model == "" {
		anthropicError(w, http.StatusBadRequest, "", "model: Field required", reqID)
		return
	}
	if len(req.Messages) == 0 {
		anthropicError(w, http.StatusBadRequest, "", "messages: Field required", reqID)
		return
	}
	s.setAnthropicHeaders(w, reqID)
	writeJSON(w, http.StatusOK, map[string]int{"input_tokens": max(1, len(raw)/4)})
}

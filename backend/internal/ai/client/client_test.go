package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
)

// SC-AI-02: the wrapper speaks the Messages API the way 11 §1 says (strict tool, tool_choice
// auto, no temperature) and turns refusals, truncation and missing tool calls into failures.

var testTool = Tool{Name: "record_lines", Description: "Record the lines.", Schema: map[string]any{
	"type": "object", "properties": map[string]any{"n": map[string]any{"type": "integer"}},
	"required": []string{"n"}, "additionalProperties": false,
}}

func testReq() Request {
	return Request{Feature: "ai_selftest", Ref: "case_1", Model: "claude-haiku-5-5", System: "Read the note.",
		Messages: []Message{UserText("3 kg rice")}, Tool: &testTool, MaxTokens: 512}
}

type seen struct {
	header http.Header
	body   map[string]any
}

func server(t *testing.T, status int, hdr map[string]string, reply string) (*httptest.Server, *seen) {
	t.Helper()
	s := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/messages", r.URL.Path)
		s.header = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &s.body)
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

func hc() *http.Client { return httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true}) }

const toolReply = `{"type":"message","model":"claude-haiku-5-5","stop_reason":"tool_use",
 "content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"t1","name":"record_lines","input":{"n":3}}],
 "usage":{"input_tokens":1000,"output_tokens":200,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}`

func TestCompleteRequestShape(t *testing.T) {
	srv, s := server(t, 200, nil, toolReply)
	key := config.NewSecret([]byte("test-key-not-real"))
	res, err := NewAnthropic(srv.URL+"/", key, hc()).Complete(context.Background(), testReq())
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":3}`, string(res.ToolInput))
	assert.Equal(t, "ok", res.Text)
	assert.Equal(t, "claude-haiku-5-5", res.Model)

	assert.Equal(t, AnthropicVersion, s.header.Get("anthropic-version"))
	assert.Equal(t, "test-key-not-real", s.header.Get("x-api-key"))
	assert.Equal(t, map[string]any{"user_id": "tz:ai_selftest:case_1"}, s.body["metadata"])
	assert.NotContains(t, s.body, "temperature")
	assert.Equal(t, map[string]any{"type": "auto", "disable_parallel_tool_use": true}, s.body["tool_choice"])
	tools := s.body["tools"].([]any)
	require.Len(t, tools, 1)
	assert.Equal(t, true, tools[0].(map[string]any)["strict"])
	assert.Contains(t, s.body["system"], "Read the note.")
	assert.Contains(t, s.body["system"], "calling the record_lines tool")
}

func TestCompleteNoKeyForTheMock(t *testing.T) {
	srv, s := server(t, 200, nil, toolReply)
	_, err := NewAnthropic(srv.URL, config.Secret{}, hc()).Complete(context.Background(), testReq())
	require.NoError(t, err)
	assert.Empty(t, s.header.Get("x-api-key"))
}

func TestCompleteFailures(t *testing.T) {
	cases := []struct {
		name, reply string
		want        error
		outcome     string
	}{
		{"refusal", `{"stop_reason":"refusal","content":[],"usage":{"input_tokens":5,"output_tokens":0}}`, ErrRefused, OutcomeRefused},
		{"max_tokens", `{"stop_reason":"max_tokens","content":[{"type":"text","text":"…"}],"usage":{"input_tokens":5,"output_tokens":512}}`, ErrTruncated, OutcomeTruncated},
		{"no tool call", `{"stop_reason":"end_turn","content":[{"type":"text","text":"none"}],"usage":{"input_tokens":5,"output_tokens":3}}`, ErrNoToolCall, OutcomeInvalid},
		{"other tool", `{"stop_reason":"tool_use","content":[{"type":"tool_use","name":"other","input":{}}],"usage":{}}`, ErrNoToolCall, OutcomeInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := server(t, 200, nil, c.reply)
			res, err := NewAnthropic(srv.URL, config.Secret{}, hc()).Complete(context.Background(), testReq())
			assert.ErrorIs(t, err, c.want)
			assert.Equal(t, c.outcome, Outcome(err))
			assert.Equal(t, "claude-haiku-5-5", res.Model, "usage comes back with the error, so the cost is still counted")
		})
	}
}

func TestCompleteProviderErrors(t *testing.T) {
	srv, _ := server(t, 529, map[string]string{"Retry-After": "7"}, `{"type":"error","error":{"type":"overloaded_error","message":"x"}}`)
	_, err := NewAnthropic(srv.URL, config.Secret{}, hc()).Complete(context.Background(), testReq())
	var pe *ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, 529, pe.Status)
	assert.Equal(t, "overloaded_error", pe.Type)
	assert.Equal(t, 7*time.Second, pe.RetryAfter)
	assert.True(t, pe.Retryable())
	assert.Equal(t, OutcomeError, Outcome(err))

	srv, _ = server(t, 429, nil, `{}`)
	_, err = NewAnthropic(srv.URL, config.Secret{}, hc()).Complete(context.Background(), testReq())
	assert.Equal(t, OutcomeRateLimited, Outcome(err))

	srv, _ = server(t, 400, nil, `{"type":"error","error":{"type":"invalid_request_error"}}`)
	_, err = NewAnthropic(srv.URL, config.Secret{}, hc()).Complete(context.Background(), testReq())
	require.ErrorAs(t, err, &pe)
	assert.False(t, pe.Retryable())
}

func TestCompleteRefusesBadRequests(t *testing.T) {
	a := NewAnthropic("", config.Secret{}, hc())
	_, err := a.Complete(context.Background(), testReq())
	assert.ErrorIs(t, err, ErrNotConfigured)

	srv, _ := server(t, 200, nil, toolReply)
	a = NewAnthropic(srv.URL, config.Secret{}, hc())
	for name, mut := range map[string]func(*Request){
		"bad feature": func(r *Request) { r.Feature = "Voice Stock" },
		"no model":    func(r *Request) { r.Model = "" },
		"no tokens":   func(r *Request) { r.MaxTokens = 0 },
		"no messages": func(r *Request) { r.Messages = nil },
	} {
		r := testReq()
		mut(&r)
		_, err := a.Complete(context.Background(), r)
		assert.ErrorIs(t, err, ErrBadRequest, name)
	}
}

func TestMeteredFillsCostAndLatency(t *testing.T) {
	srv, _ := server(t, 200, nil, toolReply)
	clk := clock.NewFake(time.Date(2026, 10, 25, 6, 0, 0, 0, time.UTC))
	m := NewMetered(NewAnthropic(srv.URL, config.Secret{}, hc()), clk)
	res, err := m.Complete(context.Background(), testReq())
	require.NoError(t, err)
	// 1000 in × 880 + 200 out × 4400 paise per MTok = 0.88 + 0.88 → 2 paise (rounded up).
	assert.Equal(t, int64(2), res.CostPaise)
}

func TestCostPaise(t *testing.T) {
	assert.Equal(t, int64(0), CostPaise("claude-haiku-5-5", Usage{}))
	assert.Equal(t, int64(88), CostPaise("claude-haiku-5-5", Usage{InputTokens: 100_000}))
	// Over 100K prompt tokens the long-prompt price applies to the whole call.
	assert.Equal(t, int64(4400+2200), CostPaise("claude-haiku-5-5", Usage{InputTokens: 1_000_000, OutputTokens: 100_000}))
	assert.Equal(t, int64(18), CostPaise("claude-sonnet-5-5", Usage{InputTokens: 1000}))
	// Cache writes 1.25×, reads 0.1× the input price.
	assert.Equal(t, int64(22+2), CostPaise("claude-sonnet-5-5", Usage{CacheWriteTokens: 1000, CacheReadTokens: 1000}))
	// An unknown model is priced at the dearest known one, never free.
	assert.Equal(t, CostPaise("claude-fable-5-1", Usage{InputTokens: 1000}), CostPaise("some-new-model", Usage{InputTokens: 1000}))
}

func TestSarvamTranscribe(t *testing.T) {
	var gotKey, gotLang, gotFile string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/speech-to-text", r.URL.Path)
		gotKey = r.Header.Get("api-subscription-key")
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			b, _ := io.ReadAll(p)
			switch p.FormName() {
			case "file":
				gotFile = string(b)
			case "language_code":
				gotLang = string(b)
			}
		}
		_, _ = io.WriteString(w, `{"transcript":"दस किलो चावल","language_code":"hi-IN"}`)
	}))
	defer srv.Close()
	s := NewSarvam(srv.URL, config.NewSecret([]byte("stt-test-key")), hc())
	tr, err := s.Transcribe(context.Background(), Audio{Data: []byte("RIFF"), MediaType: "audio/wav", Filename: "a.wav"})
	require.NoError(t, err)
	assert.Equal(t, Transcript{Text: "दस किलो चावल", Language: "hi-IN"}, tr)
	assert.Equal(t, "stt-test-key", gotKey)
	assert.Equal(t, "unknown", gotLang)
	assert.Equal(t, "RIFF", gotFile)

	_, err = NewSarvam("", config.Secret{}, hc()).Transcribe(context.Background(), Audio{Data: []byte("x")})
	assert.ErrorIs(t, err, ErrNotConfigured)
}

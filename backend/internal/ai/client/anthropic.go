package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/todayz/backend/internal/platform/config"
)

// AnthropicVersion is the Messages API version header. 2023-06-01 is the stable version the
// mock checks; a newer one would change which sampling fields are accepted, and this client
// sends none.
const AnthropicVersion = "2023-06-01"

// maxReplyBytes bounds a reply read into memory.
const maxReplyBytes = 4 << 20

var reFeature = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

// Anthropic calls the Messages API at {LLM_API_URL}/v1/messages over raw HTTP (the Go SDK's
// dependencies are not vendored, and raw HTTP keeps the one outbound client and the explicit
// base URL; W3-10 REPORT).
type Anthropic struct {
	url  string
	key  config.Secret
	http *http.Client
}

// NewAnthropic returns a client on baseURL (LLM_API_URL). key may be unset against the mock.
// hc must come from httpx.NewOutboundClient.
func NewAnthropic(baseURL string, key config.Secret, hc *http.Client) *Anthropic {
	return &Anthropic{url: strings.TrimRight(baseURL, "/"), key: key, http: hc}
}

type wireSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type wireBlock struct {
	Type   string          `json:"type"`
	Text   string          `json:"text,omitempty"`
	Source *wireSource     `json:"source,omitempty"`
	ID     string          `json:"id,omitempty"`
	Name   string          `json:"name,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
}

type wireMessage struct {
	Role    Role        `json:"role"`
	Content []wireBlock `json:"content"`
}

type wireTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
	Strict      bool           `json:"strict"`
}

type wireToolChoice struct {
	Type                   string `json:"type"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use,omitempty"`
}

type wireRequest struct {
	Model      string          `json:"model"`
	MaxTokens  int             `json:"max_tokens"`
	System     string          `json:"system,omitempty"`
	Messages   []wireMessage   `json:"messages"`
	Tools      []wireTool      `json:"tools,omitempty"`
	ToolChoice *wireToolChoice `json:"tool_choice,omitempty"`
	Metadata   struct {
		UserID string `json:"user_id"`
	} `json:"metadata"`
}

type wireResponse struct {
	Model      string      `json:"model"`
	Content    []wireBlock `json:"content"`
	StopReason string      `json:"stop_reason"`
	Usage      Usage       `json:"usage"`
}

type wireError struct {
	Error struct {
		Type string `json:"type"`
	} `json:"error"`
}

// toolLine is appended to the system prompt of a structured call: with tool_choice auto the
// prompt is what asks for the tool.
func toolLine(name string) string {
	return "Answer only by calling the " + name + " tool exactly once. Do not answer in text."
}

func buildRequest(req Request) (wireRequest, error) {
	if !reFeature.MatchString(req.Feature) || req.Model == "" || req.MaxTokens < 1 || len(req.Messages) == 0 {
		return wireRequest{}, ErrBadRequest
	}
	w := wireRequest{Model: req.Model, MaxTokens: req.MaxTokens, System: req.System}
	w.Metadata.UserID = "tz:" + req.Feature
	if req.Ref != "" {
		w.Metadata.UserID += ":" + req.Ref
	}
	for _, m := range req.Messages {
		wm := wireMessage{Role: m.Role}
		for _, b := range m.Content {
			switch {
			case b.Image != nil:
				wm.Content = append(wm.Content, wireBlock{Type: "image", Source: &wireSource{
					Type: "base64", MediaType: b.Image.MediaType, Data: base64.StdEncoding.EncodeToString(b.Image.Data),
				}})
			default:
				wm.Content = append(wm.Content, wireBlock{Type: "text", Text: b.Text})
			}
		}
		w.Messages = append(w.Messages, wm)
	}
	if t := req.Tool; t != nil {
		if t.Name == "" || t.Schema == nil {
			return wireRequest{}, ErrBadRequest
		}
		w.Tools = []wireTool{{Name: t.Name, Description: t.Description, InputSchema: t.Schema, Strict: true}}
		w.ToolChoice = &wireToolChoice{Type: "auto", DisableParallelToolUse: true}
		if w.System != "" {
			w.System += "\n\n"
		}
		w.System += toolLine(t.Name)
	}
	return w, nil
}

// Complete implements LLM.
func (a *Anthropic) Complete(ctx context.Context, req Request) (Response, error) {
	if a.url == "" {
		return Response{}, ErrNotConfigured
	}
	w, err := buildRequest(req)
	if err != nil {
		return Response{}, err
	}
	body, err := json.Marshal(w)
	if err != nil {
		return Response{}, fmt.Errorf("ai: encode request: %w", err)
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("ai: build request: %w", err)
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("anthropic-version", AnthropicVersion)
	if a.key.IsSet() {
		hr.Header.Set("x-api-key", a.key.Reveal())
	}
	res, err := a.http.Do(hr)
	if err != nil {
		return Response{}, fmt.Errorf("ai: anthropic call: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxReplyBytes))
	if err != nil {
		return Response{}, fmt.Errorf("ai: read reply: %w", err)
	}
	if res.StatusCode/100 != 2 {
		pe := &ProviderError{Provider: "anthropic", Status: res.StatusCode, RetryAfter: retryAfter(res.Header.Get("Retry-After"))}
		var we wireError
		if json.Unmarshal(raw, &we) == nil {
			pe.Type = we.Error.Type
		}
		return Response{}, pe
	}
	var wr wireResponse
	if err := json.Unmarshal(raw, &wr); err != nil {
		return Response{}, fmt.Errorf("ai: decode reply: %w", err)
	}
	return parseReply(req, wr)
}

func parseReply(req Request, wr wireResponse) (Response, error) {
	out := Response{Model: wr.Model, StopReason: wr.StopReason, Usage: wr.Usage}
	if out.Model == "" {
		out.Model = req.Model
	}
	var text []string
	for _, b := range wr.Content {
		switch b.Type {
		case "text":
			text = append(text, b.Text)
		case "tool_use":
			if req.Tool != nil && b.Name == req.Tool.Name && out.ToolInput == nil {
				out.ToolInput = b.Input
			}
		}
	}
	out.Text = strings.Join(text, "")
	switch wr.StopReason {
	case "refusal":
		return out, ErrRefused
	case "max_tokens":
		return out, ErrTruncated
	}
	if req.Tool != nil && len(out.ToolInput) == 0 {
		return out, ErrNoToolCall
	}
	return out, nil
}

// retryAfter reads a Retry-After of whole seconds (the providers send no dates).
func retryAfter(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

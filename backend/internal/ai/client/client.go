// Package client is the one way TodayZ code calls a language model or a speech-to-text
// provider (11 §1, §5; D-17, D-19). Feature packages (W3-04, W3-11…W3-13) depend on the LLM
// and STT interfaces, never on a provider.
//
// Rules this package holds:
//   - Provider base URLs come from LLM_API_URL and STT_API_URL, never an SDK default, and every
//     call goes through httpx.NewOutboundClient, so under TODAYZ_NO_EGRESS only the local mocks
//     answer (SC-PL-04).
//   - Structured output only (11 §1 rule 4): a request with a Tool gets tool_choice auto with
//     strict tool input and a system line naming the tool (forced tool_choice is refused by the
//     current models). A reply without that tool call, a refusal or a reply cut at max_tokens is
//     an error, never something to parse loosely.
//   - No retries here: a failed call costs a cap slot already, and the job or the person decides
//     whether to try again (FAIL-M.15: no retry storm).
//   - Metered wraps any LLM with the tz_ai_* metrics, a span and the call's cost in paise
//     (05 §14). Nothing here logs prompt text, replies, transcripts or keys.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Role is a message author.
type Role string

// Roles.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Image is an inline image (a bill or shelf photo).
type Image struct {
	MediaType string // image/jpeg, image/png, image/webp
	Data      []byte
}

// Block is one piece of a message: text or an image.
type Block struct {
	Text  string
	Image *Image
}

// Message is one turn.
type Message struct {
	Role    Role
	Content []Block
}

// UserText is a user message of one text block.
func UserText(s string) Message { return Message{Role: RoleUser, Content: []Block{{Text: s}}} }

// Tool is the single tool a structured call answers through. Schema is a JSON Schema object
// (build it with internal/ai/schema): every object closed and every property required, as
// strict tool input needs.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
}

// Request is one model call.
type Request struct {
	// Feature is the ai_drafts feature name (voice_stock, coach_card, …): the metric label and
	// the mock's fixture folder.
	Feature string
	// Ref is an opaque id for the provider's abuse tracking (a draft or store id). Never a
	// phone, name or anything a person typed.
	Ref string
	// Model comes from settings (ai_model_fast or ai_model_smart, 11 §1 rule 3).
	Model     string
	System    string
	Messages  []Message
	Tool      *Tool
	MaxTokens int
}

// Usage is the provider's token count for one call.
type Usage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CacheReadTokens  int `json:"cache_read_input_tokens"`
	CacheWriteTokens int `json:"cache_creation_input_tokens"`
}

// Response is one model reply. Metered fills CostPaise and Latency.
type Response struct {
	Model      string
	Text       string          // the text blocks joined
	ToolInput  json.RawMessage // the tool call's input; nil without a tool call
	StopReason string
	Usage      Usage
	CostPaise  int64
	Latency    time.Duration
}

// LLM is a language model.
type LLM interface {
	// Complete makes one call. On ErrRefused, ErrTruncated and ErrNoToolCall the Response is
	// still returned: the call was billed.
	Complete(ctx context.Context, req Request) (Response, error)
}

// Audio is one recording for speech to text.
type Audio struct {
	Data      []byte
	MediaType string // audio/webm, audio/ogg, audio/wav, audio/mpeg, audio/mp4
	Filename  string
	// Language is a BCP-47 code (hi-IN, kn-IN, …) or empty for the provider's detection.
	Language string
}

// Transcript is what the speech provider heard. It is shown to the person and never stored by
// the customer features (D-101).
type Transcript struct {
	Text     string
	Language string
}

// STT is a speech-to-text provider.
type STT interface {
	Transcribe(ctx context.Context, a Audio) (Transcript, error)
}

// Errors.
var (
	// ErrNotConfigured: no base URL (the feature is hidden, 11 §1 rule 2).
	ErrNotConfigured = errors.New("ai: provider not configured")
	// ErrRefused: the model declined (stop_reason refusal).
	ErrRefused = errors.New("ai: the model refused")
	// ErrTruncated: the reply hit max_tokens, so any tool input is incomplete.
	ErrTruncated = errors.New("ai: the reply was cut at max_tokens")
	// ErrNoToolCall: a structured call came back without its tool call.
	ErrNoToolCall = errors.New("ai: the reply has no tool call")
	// ErrBadRequest: the request is not one this client sends (a programming error).
	ErrBadRequest = errors.New("ai: bad request")
)

// ProviderError is a non-2xx answer. The provider's message is not kept: it can echo input.
type ProviderError struct {
	Provider   string
	Status     int
	Type       string // the provider's error type, e.g. overloaded_error
	RetryAfter time.Duration
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("ai: %s answered %d %s", e.Provider, e.Status, e.Type)
}

// Retryable reports whether a later attempt can succeed (rate limit, overload, server error).
func (e *ProviderError) Retryable() bool {
	return e.Status == 429 || e.Status >= 500
}

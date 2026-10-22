package capture

import (
	"sync/atomic"
	"time"
)

// Meta is set on every captured record.
type Meta struct {
	ID int64     `json:"id"`
	At time.Time `json:"at"`
}

// SMS is one message accepted by the MSG91 mock.
type SMS struct {
	Meta
	API        string            `json:"api"` // otp, otp_retry, flow
	To         string            `json:"to"`  // E.164, e.g. +919000000001
	Sender     string            `json:"sender,omitempty"`
	TemplateID string            `json:"template_id"`
	Variables  map[string]string `json:"variables"`
	Body       string            `json:"body"`
	OTP        string            `json:"otp"` // extracted code, "" when the message has none
	RequestID  string            `json:"request_id"`
}

// WhatsApp is one message accepted by the WhatsApp mock (MSG91 or Gupshup shape).
type WhatsApp struct {
	Meta
	Provider  string   `json:"provider"` // msg91, gupshup
	Kind      string   `json:"kind"`     // template, text
	To        string   `json:"to"`
	From      string   `json:"from,omitempty"`
	Template  string   `json:"template,omitempty"`
	Language  string   `json:"language,omitempty"`
	Variables []string `json:"variables,omitempty"`
	Body      string   `json:"body"`
	MessageID string   `json:"message_id"`
}

// Push is one Web Push delivery attempt.
type Push struct {
	Meta
	Subscription    string `json:"subscription"`
	Status          int    `json:"status"`
	TTL             string `json:"ttl"`
	Urgency         string `json:"urgency,omitempty"`
	Topic           string `json:"topic,omitempty"`
	ContentEncoding string `json:"content_encoding,omitempty"`
	PayloadBytes    int    `json:"payload_bytes"`
	VAPID           bool   `json:"vapid"` // an Authorization header was present (value never kept)
	Title           string `json:"title,omitempty"`
	Body            string `json:"body,omitempty"`
	Plaintext       string `json:"plaintext,omitempty"` // X-TZ-Mock-Plaintext, local/test only
}

// Turnstile is one siteverify call. The secret is never kept, only its kind.
type Turnstile struct {
	Meta
	Token          string   `json:"token"` // first characters only
	RemoteIP       string   `json:"remoteip,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
	SecretKind     string   `json:"secret_kind"` // missing, test-pass, test-fail, test-spent, set
	Success        bool     `json:"success"`
	ErrorCodes     []string `json:"error_codes"`
}

// STT is one speech-to-text call.
type STT struct {
	Meta
	Endpoint          string `json:"endpoint"` // speech-to-text, speech-to-text-translate
	Status            int    `json:"status"`
	SHA256            string `json:"sha256"`
	Bytes             int    `json:"bytes"`
	Filename          string `json:"filename,omitempty"`
	ContentType       string `json:"content_type,omitempty"`
	Model             string `json:"model,omitempty"`
	LanguageRequested string `json:"language_requested,omitempty"`
	LanguageCode      string `json:"language_code,omitempty"`
	Fixture           string `json:"fixture,omitempty"`
	Transcript        string `json:"transcript"`
	Error             string `json:"error,omitempty"`
}

// LLM is one Messages API call.
type LLM struct {
	Meta
	Feature      string   `json:"feature"`
	Model        string   `json:"model"`
	Mode         string   `json:"mode"` // fixture, live, miss, invalid
	Fixture      string   `json:"fixture,omitempty"`
	Recorded     string   `json:"recorded,omitempty"`
	Stream       bool     `json:"stream"`
	Status       int      `json:"status"`
	StopReason   string   `json:"stop_reason,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	Prompt       string   `json:"prompt"`   // last user text, truncated
	Response     string   `json:"response"` // text or tool input JSON, truncated
	InputTokens  int      `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	Error        string   `json:"error,omitempty"`
}

// Kinds lists every capture kind, in UI tab order.
var Kinds = []string{"sms", "whatsapp", "push", "turnstile", "stt", "llm"}

// Store holds one ring per kind.
type Store struct {
	seq atomic.Int64
	now func() time.Time

	SMS       *Ring[SMS]
	WhatsApp  *Ring[WhatsApp]
	Push      *Ring[Push]
	Turnstile *Ring[Turnstile]
	STT       *Ring[STT]
	LLM       *Ring[LLM]
}

// NewStore returns a store with capacity entries per kind. now may be nil.
func NewStore(capacity int, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{
		now:       now,
		SMS:       NewRing[SMS](capacity),
		WhatsApp:  NewRing[WhatsApp](capacity),
		Push:      NewRing[Push](capacity),
		Turnstile: NewRing[Turnstile](capacity),
		STT:       NewRing[STT](capacity),
		LLM:       NewRing[LLM](capacity),
	}
}

// Next returns a fresh Meta (increasing id, current time).
func (s *Store) Next() Meta {
	return Meta{ID: s.seq.Add(1), At: s.now().UTC()}
}

// Counts reports the number of entries per kind.
func (s *Store) Counts() map[string]int {
	return map[string]int{
		"sms":       s.SMS.Len(),
		"whatsapp":  s.WhatsApp.Len(),
		"push":      s.Push.Len(),
		"turnstile": s.Turnstile.Len(),
		"stt":       s.STT.Len(),
		"llm":       s.LLM.Len(),
	}
}

// Clear empties one kind. It reports false for an unknown kind.
func (s *Store) Clear(kind string) bool {
	switch kind {
	case "sms":
		s.SMS.Clear()
	case "whatsapp":
		s.WhatsApp.Clear()
	case "push":
		s.Push.Clear()
	case "turnstile":
		s.Turnstile.Clear()
	case "stt":
		s.STT.Clear()
	case "llm":
		s.LLM.Clear()
	default:
		return false
	}
	return true
}

// Reset empties every kind.
func (s *Store) Reset() {
	for _, k := range Kinds {
		s.Clear(k)
	}
}

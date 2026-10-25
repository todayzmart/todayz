package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/todayz/backend/internal/platform/config"
)

// Sarvam calls Sarvam speech to text at {STT_API_URL}/speech-to-text.
type Sarvam struct {
	url  string
	key  config.Secret
	http *http.Client
}

// NewSarvam returns a client on baseURL (STT_API_URL); hc must come from
// httpx.NewOutboundClient. key may be unset against the mock.
func NewSarvam(baseURL string, key config.Secret, hc *http.Client) *Sarvam {
	return &Sarvam{url: strings.TrimRight(baseURL, "/"), key: key, http: hc}
}

type sarvamReply struct {
	Transcript   string `json:"transcript"`
	LanguageCode string `json:"language_code"`
}

// Transcribe implements STT.
func (s *Sarvam) Transcribe(ctx context.Context, a Audio) (Transcript, error) {
	if s.url == "" {
		return Transcript{}, ErrNotConfigured
	}
	if len(a.Data) == 0 || a.MediaType == "" {
		return Transcript{}, ErrBadRequest
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	name := a.Filename
	if name == "" {
		name = "audio"
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, name))
	h.Set("Content-Type", a.MediaType)
	fw, err := mw.CreatePart(h)
	if err != nil {
		return Transcript{}, fmt.Errorf("ai: build audio part: %w", err)
	}
	if _, err := fw.Write(a.Data); err != nil {
		return Transcript{}, fmt.Errorf("ai: build audio part: %w", err)
	}
	lang := a.Language
	if lang == "" {
		lang = "unknown" // the provider detects the language
	}
	if err := mw.WriteField("language_code", lang); err != nil {
		return Transcript{}, fmt.Errorf("ai: build form: %w", err)
	}
	if err := mw.Close(); err != nil {
		return Transcript{}, fmt.Errorf("ai: build form: %w", err)
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url+"/speech-to-text", &buf)
	if err != nil {
		return Transcript{}, fmt.Errorf("ai: build request: %w", err)
	}
	hr.Header.Set("Content-Type", mw.FormDataContentType())
	if s.key.IsSet() {
		hr.Header.Set("api-subscription-key", s.key.Reveal())
	}
	res, err := s.http.Do(hr)
	if err != nil {
		return Transcript{}, fmt.Errorf("ai: sarvam call: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxReplyBytes))
	if err != nil {
		return Transcript{}, fmt.Errorf("ai: read reply: %w", err)
	}
	if res.StatusCode/100 != 2 {
		return Transcript{}, &ProviderError{Provider: "sarvam", Status: res.StatusCode, RetryAfter: retryAfter(res.Header.Get("Retry-After"))}
	}
	var r sarvamReply
	if err := json.Unmarshal(raw, &r); err != nil {
		return Transcript{}, fmt.Errorf("ai: decode reply: %w", err)
	}
	return Transcript{Text: r.Transcript, Language: r.LanguageCode}, nil
}

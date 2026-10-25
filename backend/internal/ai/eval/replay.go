package eval

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/todayz/backend/internal/ai/client"
)

// Replay is a loopback Messages API that answers each case with its recorded reply. It
// checks what the mock server checks (W0-07): the anthropic-version header and the
// tz:<feature>:<case> metadata.
type Replay struct {
	srv     *httptest.Server
	feature string
	replies map[string]json.RawMessage
}

// NewReplay starts a replay server for a suite's cases. Close it when done.
func NewReplay(feature string, cases []Case) *Replay {
	r := &Replay{feature: feature, replies: make(map[string]json.RawMessage, len(cases))}
	for _, c := range cases {
		r.replies[c.Name] = c.Reply
	}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	return r
}

// URL is the base URL to give client.NewAnthropic.
func (r *Replay) URL() string { return r.srv.URL }

// Close stops the server.
func (r *Replay) Close() { r.srv.Close() }

func (r *Replay) serve(w http.ResponseWriter, req *http.Request) {
	fail := func(status int, typ, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]string{"type": typ, "message": msg}})
	}
	if req.Method != http.MethodPost || req.URL.Path != "/v1/messages" {
		fail(http.StatusNotFound, "not_found_error", "replay serves POST /v1/messages only")
		return
	}
	if req.Header.Get("anthropic-version") != client.AnthropicVersion {
		fail(http.StatusBadRequest, "invalid_request_error", "anthropic-version header missing")
		return
	}
	var body struct {
		Metadata struct {
			UserID string `json:"user_id"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 8<<20)).Decode(&body); err != nil {
		fail(http.StatusBadRequest, "invalid_request_error", "body is not JSON")
		return
	}
	name, ok := strings.CutPrefix(body.Metadata.UserID, "tz:"+r.feature+":")
	reply, found := r.replies[name]
	if !ok || !found {
		fail(http.StatusBadRequest, "invalid_request_error", "no recorded reply for this case")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(reply)
}

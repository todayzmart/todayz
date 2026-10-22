package server

import (
	"encoding/json"
	"mime"
	"net/http"
	"time"

	"todayz.local/e2e/mocks/internal/capture"
)

// FailToken always fails verification (the mock's magic token).
const FailToken = "XXXX.DUMMY.TOKEN.FAIL"

// Cloudflare's published test secret keys
// (https://developers.cloudflare.com/turnstile/troubleshooting/testing/).
// These are public dummy values, not secrets.
const (
	cfTestSecretPass  = "1x0000000000000000000000000000000AA"
	cfTestSecretFail  = "2x0000000000000000000000000000000AA"
	cfTestSecretSpent = "3x0000000000000000000000000000000AA"
)

// POST /turnstile/v0/siteverify — form-encoded (or JSON) secret, response,
// remoteip, idempotency_key. Always HTTP 200; the result is in "success".
// Docs: https://developers.cloudflare.com/turnstile/get-started/server-side-validation/
func (s *Server) turnstile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var secret, token, remoteIP, idem string
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "application/json" {
		var b struct {
			Secret         string `json:"secret"`
			Response       string `json:"response"`
			RemoteIP       string `json:"remoteip"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			s.turnstileBadRequest(w, err)
			return
		}
		secret, token, remoteIP, idem = b.Secret, b.Response, b.RemoteIP, b.IdempotencyKey
	} else {
		if err := r.ParseForm(); err != nil {
			s.turnstileBadRequest(w, err)
			return
		}
		f := r.PostForm
		secret, token, remoteIP, idem = f.Get("secret"), f.Get("response"), f.Get("remoteip"), f.Get("idempotency_key")
	}

	rec := capture.Turnstile{Token: truncate(token, 24), RemoteIP: remoteIP, IdempotencyKey: idem}
	switch secret {
	case "":
		rec.SecretKind = "missing"
	case cfTestSecretPass:
		rec.SecretKind = "test-pass"
	case cfTestSecretFail:
		rec.SecretKind = "test-fail"
	case cfTestSecretSpent:
		rec.SecretKind = "test-spent"
	default:
		rec.SecretKind = "set" // the value is never kept
	}

	var codes []string
	switch {
	case secret == "":
		codes = []string{"missing-input-secret"}
	case token == "":
		codes = []string{"missing-input-response"}
	case secret == cfTestSecretFail, token == FailToken, len(token) > 2048:
		codes = []string{"invalid-input-response"}
	case secret == cfTestSecretSpent:
		codes = []string{"timeout-or-duplicate"}
	}
	s.turnstileReply(w, rec, codes)
}

// turnstileBadRequest answers a body that could not be read: 413 (in the
// siteverify error shape) when it was over the limit, otherwise Cloudflare's
// HTTP 200 with "bad-request".
func (s *Server) turnstileBadRequest(w http.ResponseWriter, err error) {
	if bodyErrStatus(err) != http.StatusRequestEntityTooLarge {
		s.turnstileReply(w, capture.Turnstile{SecretKind: "missing"}, []string{"bad-request"})
		return
	}
	codes := []string{"bad-request"}
	s.store.Turnstile.Add(capture.Turnstile{Meta: s.store.Next(), SecretKind: "missing", ErrorCodes: codes})
	writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
		"success": false, "error-codes": codes, "messages": []string{"request body too large"},
	})
}

func (s *Server) turnstileReply(w http.ResponseWriter, rec capture.Turnstile, codes []string) {
	rec.Meta = s.store.Next()
	rec.Success = len(codes) == 0
	if codes == nil {
		codes = []string{}
	}
	rec.ErrorCodes = codes
	s.store.Turnstile.Add(rec)
	if !rec.Success {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error-codes": codes, "messages": []string{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"challenge_ts": rec.At.Add(-time.Second).Format("2006-01-02T15:04:05.000Z"),
		"hostname":     s.cfg.TurnstileHostname,
		"error-codes":  codes,
		"action":       "",
		"cdata":        "",
		"metadata":     map[string]any{"interactive": false},
	})
}

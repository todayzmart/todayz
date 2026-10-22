package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"todayz.local/e2e/mocks/internal/capture"
)

// PlaintextHeader carries the notification JSON in local/test only, because
// the real payload is encrypted (RFC 8291) and the mock cannot read it.
const PlaintextHeader = "X-TZ-Mock-Plaintext"

// maxPushPayload is the RFC 8030/8291 limit push services enforce.
const maxPushPayload = 4096

var topicRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// POST /push/{subscriptionId...} — a push service endpoint (RFC 8030 §5).
//
// Required: TTL. Optional: Urgency (very-low|low|normal|high), Topic (<= 32
// base64url chars), Content-Encoding (aes128gcm or aesgcm, required with a
// body), Authorization (VAPID, RFC 8292; presence only is recorded).
// 201 Created with a Location header on success; 413 above 4096 bytes.
// A subscription id starting "gone-404-" returns 404, any other "gone-" 410.
func (s *Server) push(w http.ResponseWriter, r *http.Request) {
	sub := r.PathValue("subscriptionId")
	payload, status := readBody(w, r, maxPushPayload)
	if status != 0 {
		msg := "could not read body"
		if status == http.StatusRequestEntityTooLarge {
			msg = "payload too large"
		}
		http.Error(w, msg, status)
		return
	}
	rec := capture.Push{
		Meta:            s.store.Next(),
		Subscription:    sub,
		TTL:             r.Header.Get("TTL"),
		Urgency:         r.Header.Get("Urgency"),
		Topic:           r.Header.Get("Topic"),
		ContentEncoding: r.Header.Get("Content-Encoding"),
		PayloadBytes:    len(payload),
		VAPID:           r.Header.Get("Authorization") != "",
	}
	if pt := r.Header.Get(PlaintextHeader); pt != "" {
		rec.Plaintext, rec.Title, rec.Body = decodePlaintext(pt)
	}

	status, msg := http.StatusCreated, ""
	switch {
	case strings.HasPrefix(sub, "gone-404-"):
		status, msg = http.StatusNotFound, "subscription not found"
	case strings.HasPrefix(sub, "gone-"):
		status, msg = http.StatusGone, "push subscription has unsubscribed or expired"
	case rec.TTL == "":
		status, msg = http.StatusBadRequest, "missing TTL header"
	case !validTTL(rec.TTL):
		status, msg = http.StatusBadRequest, "invalid TTL header"
	case rec.Urgency != "" && !validUrgency(rec.Urgency):
		status, msg = http.StatusBadRequest, "invalid Urgency header"
	case rec.Topic != "" && !topicRe.MatchString(rec.Topic):
		status, msg = http.StatusBadRequest, "invalid Topic header"
	case len(payload) > 0 && rec.ContentEncoding != "aes128gcm" && rec.ContentEncoding != "aesgcm":
		status, msg = http.StatusBadRequest, "payload needs Content-Encoding aes128gcm"
	}
	rec.Status = status
	s.store.Push.Add(rec)

	if status != http.StatusCreated {
		http.Error(w, msg, status)
		return
	}
	w.Header().Set("Location", "/push/_messages/"+strconv.FormatInt(rec.ID, 10))
	w.Header().Set("TTL", rec.TTL)
	w.WriteHeader(http.StatusCreated)
}

func validTTL(v string) bool {
	n, err := strconv.ParseInt(v, 10, 64)
	return err == nil && n >= 0
}

func validUrgency(v string) bool {
	switch v {
	case "very-low", "low", "normal", "high":
		return true
	}
	return false
}

// decodePlaintext accepts the notification JSON as-is or base64-encoded (for
// clients that keep header values ASCII). It returns the raw text (truncated)
// and the title and body when present at the top level or under "notification".
func decodePlaintext(h string) (raw, title, body string) {
	raw = h
	var m map[string]any
	if err := json.Unmarshal([]byte(h), &m); err != nil {
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
			if b, err := enc.DecodeString(h); err == nil && json.Unmarshal(b, &m) == nil {
				raw = string(b)
				break
			}
		}
	}
	if m != nil {
		src := m
		if n, ok := m["notification"].(map[string]any); ok {
			src = n
		}
		title, _ = src["title"].(string)
		body, _ = src["body"].(string)
	}
	return truncate(raw, 4096), title, body
}

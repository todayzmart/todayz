package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"todayz.local/e2e/mocks/internal/capture"
)

// The /_mock JSON API is for tests, QA agents and the web UI. It is a local
// tool: it returns full phones and OTPs, so it never runs outside local/test.

const maxOTPWait = 10 * time.Second

func listLimit(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// items keeps "items" a JSON array even when empty.
func items[T any](v []T) map[string]any {
	if v == nil {
		v = []T{}
	}
	return map[string]any{"items": v}
}

// GET /_mock/{kind}?limit=  (sms: ?to=, whatsapp: ?to=, push: ?subscription=, llm: ?feature=)
// Newest first.
func (s *Server) apiList(w http.ResponseWriter, r *http.Request) {
	q, limit := r.URL.Query(), listLimit(r)
	switch r.PathValue("kind") {
	case "sms":
		to := NormalizePhone(q.Get("to"))
		writeJSON(w, http.StatusOK, items(s.store.SMS.List(func(m capture.SMS) bool { return to == "" || m.To == to }, limit)))
	case "whatsapp":
		to := NormalizePhone(q.Get("to"))
		writeJSON(w, http.StatusOK, items(s.store.WhatsApp.List(func(m capture.WhatsApp) bool { return to == "" || m.To == to }, limit)))
	case "push":
		sub := q.Get("subscription")
		writeJSON(w, http.StatusOK, items(s.store.Push.List(func(m capture.Push) bool { return sub == "" || m.Subscription == sub }, limit)))
	case "turnstile":
		writeJSON(w, http.StatusOK, items(s.store.Turnstile.List(nil, limit)))
	case "stt":
		writeJSON(w, http.StatusOK, items(s.store.STT.List(nil, limit)))
	case "llm":
		f := q.Get("feature")
		writeJSON(w, http.StatusOK, items(s.store.LLM.List(func(m capture.LLM) bool { return f == "" || m.Feature == f }, limit)))
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown kind; use one of " + strings.Join(capture.Kinds, ", ")})
	}
}

// GET /_mock/sms/latest-otp?to=&since=&wait=
//
// Returns {"otp","at"} for the newest SMS to that phone that carries an OTP,
// or 404. since (RFC 3339) ignores older messages; wait (seconds, max 10)
// polls until one arrives, for tests that race the send.
func (s *Server) apiLatestOTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	to := NormalizePhone(q.Get("to"))
	if to == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "to is required"})
		return
	}
	var since time.Time
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "since must be RFC 3339"})
			return
		}
		since = t
	}
	var wait time.Duration
	if v := q.Get("wait"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wait must be seconds"})
			return
		}
		wait = min(time.Duration(f*float64(time.Second)), maxOTPWait)
	}
	deadline := time.Now().Add(wait)
	for {
		if m, ok := s.latestOTP(to); ok && !m.At.Before(since) {
			writeJSON(w, http.StatusOK, map[string]any{"otp": m.OTP, "at": m.At})
			return
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "no OTP for that phone"})
}

// latestOTP returns the newest captured SMS with a code for that number.
func (s *Server) latestOTP(to string) (capture.SMS, bool) {
	got := s.store.SMS.List(func(m capture.SMS) bool { return m.To == to && m.OTP != "" }, 1)
	if len(got) == 0 {
		return capture.SMS{}, false
	}
	return got[0], true
}

// GET /_mock/summary — entry counts per kind (the UI's tab badges).
func (s *Server) apiSummary(w http.ResponseWriter, _ *http.Request) {
	mode := "fixtures"
	if s.cfg.LLMLive {
		mode = "live"
	}
	writeJSON(w, http.StatusOK, map[string]any{"counts": s.store.Counts(), "kinds": capture.Kinds, "llm_mode": mode})
}

// DELETE /_mock/reset — clears every kind.
func (s *Server) apiReset(w http.ResponseWriter, _ *http.Request) {
	s.store.Reset()
	s.otps.clear()
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /_mock/{kind} — clears one kind. Clearing sms also forgets the
// MSG91 OTP state (codes waiting to be verified).
func (s *Server) apiClear(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !s.store.Clear(kind) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown kind"})
		return
	}
	if kind == "sms" {
		s.otps.clear()
	}
	w.WriteHeader(http.StatusNoContent)
}

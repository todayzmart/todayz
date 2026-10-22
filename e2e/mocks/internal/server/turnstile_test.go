package server

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

const siteverify = "/turnstile/turnstile/v0/siteverify"

func TestTurnstileSuccessShape(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Now = func() time.Time { return fixedNow } })
	rr := h.postForm(siteverify, url.Values{
		"secret":          {"local-placeholder-secret"},
		"response":        {"0.AAAAAAAAAAAAAAAAAAAAAAAA.zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
		"remoteip":        {"127.0.0.1"},
		"idempotency_key": {"5c5d8f1e-0000-4000-8000-000000000001"},
	}, nil)
	wantStatus(t, rr, 200)
	m := decode(t, rr)
	wantKeys(t, m, "success", "challenge_ts", "hostname", "error-codes", "action", "cdata", "metadata")
	if m["success"] != true || m["hostname"] != "localhost" {
		t.Errorf("response = %v", m)
	}
	if codes := m["error-codes"].([]any); len(codes) != 0 {
		t.Errorf("error-codes = %v", codes)
	}
	if _, err := time.Parse(time.RFC3339, m["challenge_ts"].(string)); err != nil {
		t.Errorf("challenge_ts not ISO 8601: %v", m["challenge_ts"])
	}

	rec := h.srv.Store().Turnstile.List(nil, 1)[0]
	if !rec.Success || rec.SecretKind != "set" || rec.RemoteIP != "127.0.0.1" || rec.IdempotencyKey == "" {
		t.Errorf("capture = %+v", rec)
	}
	if len([]rune(rec.Token)) > 25 {
		t.Errorf("token must be truncated, got %q", rec.Token)
	}
	b, _ := json.Marshal(rec)
	if strings.Contains(string(b), "local-placeholder-secret") {
		t.Error("the secret must never be stored")
	}
}

func TestTurnstileFailures(t *testing.T) {
	h := newHarness(t, nil)
	cases := []struct {
		name   string
		form   url.Values
		code   string
		secret string
	}{
		{"magic fail token", url.Values{"secret": {"s"}, "response": {FailToken}}, "invalid-input-response", "set"},
		{"empty token", url.Values{"secret": {"s"}, "response": {""}}, "missing-input-response", "set"},
		{"no token field", url.Values{"secret": {"s"}}, "missing-input-response", "set"},
		{"missing secret", url.Values{"response": {"tok"}}, "missing-input-secret", "missing"},
		{"too long token", url.Values{"secret": {"s"}, "response": {strings.Repeat("a", 2049)}}, "invalid-input-response", "set"},
		{"cloudflare always-fail secret", url.Values{"secret": {cfTestSecretFail}, "response": {"tok"}}, "invalid-input-response", "test-fail"},
		{"cloudflare spent secret", url.Values{"secret": {cfTestSecretSpent}, "response": {"tok"}}, "timeout-or-duplicate", "test-spent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := h.postForm(siteverify, c.form, nil)
			wantStatus(t, rr, 200) // Cloudflare reports failures in the body
			m := decode(t, rr)
			wantKeys(t, m, "success", "error-codes", "messages")
			codes := m["error-codes"].([]any)
			if m["success"] != false || len(codes) != 1 || codes[0] != c.code {
				t.Errorf("response = %v, want code %s", m, c.code)
			}
			rec := h.srv.Store().Turnstile.List(nil, 1)[0]
			if rec.Success || rec.SecretKind != c.secret {
				t.Errorf("capture = %+v", rec)
			}
		})
	}
}

func TestTurnstileJSONAndPaths(t *testing.T) {
	h := newHarness(t, nil)
	for _, p := range []string{siteverify, "/turnstile/v0/siteverify", "/turnstile/siteverify"} {
		rr := h.postJSON(p, map[string]string{"secret": cfTestSecretPass, "response": "XXXX.DUMMY.TOKEN.XXXX"}, nil)
		wantStatus(t, rr, 200)
		if m := decode(t, rr); m["success"] != true {
			t.Errorf("%s: %v", p, m)
		}
	}
	if rec := h.srv.Store().Turnstile.List(nil, 1)[0]; rec.SecretKind != "test-pass" {
		t.Errorf("secret kind = %s", rec.SecretKind)
	}
	rr := h.postJSON(siteverify, `{`, nil)
	wantStatus(t, rr, 200)
	if m := decode(t, rr); m["success"] != false || m["error-codes"].([]any)[0] != "bad-request" {
		t.Errorf("bad json = %v", m)
	}
}

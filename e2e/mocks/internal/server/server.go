// Package server is the TodayZ mocks HTTP server: provider look-alikes for
// AWS End User Messaging SMS, MSG91 WhatsApp (and legacy MSG91 SMS), Gupshup WhatsApp, Web Push, Cloudflare Turnstile,
// Sarvam speech-to-text and the Anthropic Messages API, plus a capture UI and
// a JSON API for tests.
//
// Logging rule (D-15): request logs hold only method, route template, status
// and duration. Never a body, query string, header, phone, OTP or token.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"todayz.local/e2e/mocks/internal/capture"
	"todayz.local/e2e/mocks/internal/fixtures"
	"todayz.local/e2e/mocks/internal/ui"
)

// DefaultAnthropicUpstream is where live mode forwards Messages API calls. It
// is fixed: no environment variable changes it (tests set
// Config.AnthropicUpstream directly).
const DefaultAnthropicUpstream = "https://api.anthropic.com"

// DefaultTurnstileHostname is the "hostname" a passing siteverify reports.
const DefaultTurnstileHostname = "localhost"

// Config is everything the server needs. Build it with ConfigFromEnv.
type Config struct {
	// Fixtures is read on every request (fixtures/ layout: llm/, stt/, sms/, whatsapp/).
	Fixtures fs.FS
	// FixturesDir is the on-disk fixtures directory, or "" when Fixtures is the
	// embedded copy. Live LLM recordings are saved only when it is set.
	FixturesDir string
	// Capacity is the ring size per capture kind.
	Capacity int

	// LLMLive forwards Messages API calls upstream and records them. Only
	// ConfigFromEnv should set it: MOCK_LLM=0 and ANTHROPIC_API_KEY set.
	LLMLive           bool
	AnthropicKey      string // never logged or stored
	AnthropicUpstream string
	// UpstreamClient is used in live mode; nil means a client with a 120 s timeout.
	UpstreamClient *http.Client
	// ReplayRecorded also answers from fixtures/llm/_recorded/ (and any other
	// "_" folder). Off by default so recordings never change fixture results.
	ReplayRecorded bool

	// TurnstileHostname is the hostname a passing siteverify reports.
	TurnstileHostname string

	Logger *slog.Logger
	Now    func() time.Time
}

// ConfigFromEnv reads the mock's environment through getenv.
//
// LLM live/record mode is on only when MOCK_LLM is exactly "0" AND
// ANTHROPIC_API_KEY is non-empty. Unset or "1" never forwards, even with a key.
// The upstream is always DefaultAnthropicUpstream.
func ConfigFromEnv(getenv func(string) string) Config {
	key := getenv("ANTHROPIC_API_KEY")
	live := getenv("MOCK_LLM") == "0" && key != ""
	c := Config{
		Capacity:          500,
		LLMLive:           live,
		AnthropicUpstream: DefaultAnthropicUpstream,
		ReplayRecorded:    getenv("MOCK_LLM_REPLAY_RECORDED") == "1",
		TurnstileHostname: DefaultTurnstileHostname,
	}
	if live {
		c.AnthropicKey = key
	}
	if v := strings.TrimSpace(getenv("MOCK_TURNSTILE_HOSTNAME")); v != "" {
		c.TurnstileHostname = v
	}
	if n, err := strconv.Atoi(getenv("MOCKS_CAPACITY")); err == nil && n > 0 {
		c.Capacity = n
	}
	return c
}

// Server holds the capture store and config.
type Server struct {
	cfg   Config
	store *capture.Store
	otps  otpStore // MSG91 OTP state per mobile (verify is single-use)
	log   *slog.Logger
	mux   *http.ServeMux
}

// New builds the server.
func New(cfg Config) *Server {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 500
	}
	if cfg.AnthropicUpstream == "" {
		cfg.AnthropicUpstream = DefaultAnthropicUpstream
	}
	cfg.AnthropicUpstream = strings.TrimRight(cfg.AnthropicUpstream, "/")
	if cfg.TurnstileHostname == "" {
		cfg.TurnstileHostname = DefaultTurnstileHostname
	}
	if cfg.UpstreamClient == nil {
		cfg.UpstreamClient = &http.Client{Timeout: 120 * time.Second}
	}
	s := &Server{cfg: cfg, store: capture.NewStore(cfg.Capacity, cfg.Now), log: cfg.Logger, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Store exposes the capture store (tests).
func (s *Server) Store() *capture.Store { return s.store }

func (s *Server) routes() {
	m := s.mux

	// AWS End User Messaging SMS (D-212): SMS_API_URL=http://127.0.0.1:4010/sms-voice.
	// The SDK posts to the base path itself, without a trailing slash.
	m.HandleFunc("POST "+awsSMSRequestPath, s.awsSendTextMessage)
	m.HandleFunc("POST "+awsSMSRequestPath+"/{$}", s.awsSendTextMessage)

	// Legacy MSG91 SMS (base https://control.msg91.com -> /msg91), kept until the
	// lead removes it; the backend no longer calls it (D-212). Provider routes take
	// phones with the country code only (919000000001); /_mock filters are lenient.
	m.HandleFunc("POST /msg91/api/v5/otp", s.msg91SendOTP)
	m.HandleFunc("POST /msg91/api/v5/otp/retry", s.msg91RetryOTP)
	m.HandleFunc("GET /msg91/api/v5/otp/retry", s.msg91RetryOTP)
	m.HandleFunc("GET /msg91/api/v5/otp/verify", s.msg91VerifyOTP)
	m.HandleFunc("POST /msg91/api/v5/flow", s.msg91Flow)
	m.HandleFunc("POST /msg91/api/v5/flow/{$}", s.msg91Flow)

	// WhatsApp: MSG91 (base https://api.msg91.com) and Gupshup (base https://api.gupshup.io) -> /whatsapp.
	m.HandleFunc("POST /whatsapp/api/v5/whatsapp/whatsapp-outbound-message/bulk/{$}", s.waMSG91Bulk)
	m.HandleFunc("POST /whatsapp/api/v5/whatsapp/whatsapp-outbound-message/bulk", s.waMSG91Bulk)
	m.HandleFunc("POST /whatsapp/api/v5/whatsapp/whatsapp-outbound-message/{$}", s.waMSG91Session)
	m.HandleFunc("POST /whatsapp/api/v5/whatsapp/whatsapp-outbound-message", s.waMSG91Session)
	m.HandleFunc("POST /whatsapp/wa/api/v1/msg", s.waGupshupMsg)
	m.HandleFunc("POST /whatsapp/wa/api/v1/template/msg", s.waGupshupTemplate)

	// Web Push (the subscription endpoint becomes PUSH_API_URL/<id>).
	m.HandleFunc("POST /push/{subscriptionId...}", s.push)

	// Turnstile (base https://challenges.cloudflare.com -> /turnstile).
	m.HandleFunc("POST /turnstile/turnstile/v0/siteverify", s.turnstile)
	m.HandleFunc("POST /turnstile/v0/siteverify", s.turnstile)
	m.HandleFunc("POST /turnstile/siteverify", s.turnstile)

	// Sarvam (base https://api.sarvam.ai -> /sarvam).
	m.HandleFunc("POST /sarvam/speech-to-text", s.sarvamSTT)
	m.HandleFunc("POST /sarvam/speech-to-text-translate", s.sarvamSTT)

	// Anthropic (base https://api.anthropic.com -> /anthropic).
	m.HandleFunc("POST /anthropic/v1/messages", s.anthropicMessages)
	m.HandleFunc("POST /anthropic/v1/messages/count_tokens", s.anthropicCountTokens)

	// JSON API for tests and QA agents.
	m.HandleFunc("GET /healthz", s.healthz)
	m.HandleFunc("GET /_mock/summary", s.apiSummary)
	m.HandleFunc("GET /_mock/sms/latest-otp", s.apiLatestOTP)
	m.HandleFunc("GET /_mock/{kind}", s.apiList)
	m.HandleFunc("DELETE /_mock/reset", s.apiReset)
	m.HandleFunc("DELETE /_mock/{kind}", s.apiClear)

	// Web UI.
	m.Handle("GET /{$}", ui.Index())
	m.Handle("GET /assets/", ui.Assets())
}

// allowedHosts are the Host header names the mock answers, on any port. Caddy
// proxies https://mocks.todayz.localhost here and keeps the original Host.
// Anything else gets 421: a page on another origin that DNS-rebinds its name to
// 127.0.0.1 cannot read captured OTPs through /_mock.
var allowedHosts = map[string]bool{
	"127.0.0.1": true, "localhost": true, "::1": true, "mocks.todayz.localhost": true,
}

// hostAllowed reports whether a Host header ("name", "name:port", "[::1]:port")
// names this machine.
func hostAllowed(host string) bool {
	h := host
	if name, _, err := net.SplitHostPort(host); err == nil {
		h = name
	} else {
		h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	}
	h = strings.TrimSuffix(strings.ToLower(h), ".")
	return allowedHosts[h]
}

// ServeHTTP checks the Host header, logs every request (route template only)
// and recovers panics.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	if !hostAllowed(r.Host) {
		http.Error(rec, "tz-mock: host not allowed; use 127.0.0.1, localhost, [::1] or mocks.todayz.localhost", http.StatusMisdirectedRequest)
		s.log.Info("request", "method", r.Method, "route", "host-rejected", "status", rec.status,
			"duration_ms", float64(time.Since(start).Microseconds())/1000)
		return
	}
	defer func() {
		if p := recover(); p != nil {
			// The panic value may hold request data; never log it.
			if !rec.wrote {
				http.Error(rec, "internal error", http.StatusInternalServerError)
			}
			rec.status = http.StatusInternalServerError
			s.log.Error("panic recovered", "route", routeOf(r))
		}
		s.log.Info("request",
			"method", r.Method,
			"route", routeOf(r),
			"status", rec.status,
			"duration_ms", float64(time.Since(start).Microseconds())/1000,
		)
	}()
	s.mux.ServeHTTP(rec, r)
}

// routeOf returns the matched route template, e.g. "/push/{subscriptionId...}".
// It never returns the real path, which may hold a phone or token.
func routeOf(r *http.Request) string {
	p := r.Pattern
	if p == "" {
		return "unmatched"
	}
	if i := strings.IndexByte(p, ' '); i >= 0 {
		p = p[i+1:]
	}
	return p
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wrote = true
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach Flush on the real writer (SSE).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// healthz stays 200 when fixture files are broken (the mock still serves the
// rest); it lists them so a person can fix them.
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	mode := "fixtures"
	if s.cfg.LLMLive {
		mode = "live"
	}
	source := "embedded"
	if s.cfg.FixturesDir != "" {
		source = "dir"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "llm_mode": mode, "fixtures": source,
		"replay_recorded": s.cfg.ReplayRecorded,
		"fixture_errors":  s.fixtureErrors(),
	})
}

// fixtureErrors lists every fixture file that does not load, as
// "path: reason" (always a JSON array).
func (s *Server) fixtureErrors() []string {
	out := []string{}
	if set, err := s.loadLLM(); err != nil {
		out = append(out, "llm: "+err.Error())
	} else {
		for _, e := range set.Errors {
			out = append(out, e.Error())
		}
	}
	if _, bad, err := fixtures.LoadSTT(s.cfg.Fixtures); err != nil {
		out = append(out, "stt: "+err.Error())
	} else {
		for _, e := range bad {
			out = append(out, e.Error())
		}
	}
	for _, name := range []string{"sms/templates.json", dltTemplatesFile, "whatsapp/templates.json"} {
		if _, err := fixtures.LoadTemplates(s.cfg.Fixtures, name); err != nil {
			out = append(out, err.Error())
		}
	}
	return out
}

func (s *Server) loadLLM() (fixtures.LLMSet, error) {
	return fixtures.LoadLLM(s.cfg.Fixtures, fixtures.LLMOptions{IncludeRecorded: s.cfg.ReplayRecorded})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// readBody reads at most limit bytes. On failure it returns the status the
// caller should answer with in its provider's error shape: 413 when the body
// is larger than limit, 400 for any other read error.
func readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, int) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		return nil, bodyErrStatus(err)
	}
	return b, 0
}

// bodyErrStatus maps a body read or parse error to 413 (too large) or 400.
func bodyErrStatus(err error) int {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

// bodyErrText is the generic message for a readBody status.
func bodyErrText(status int) string {
	if status == http.StatusRequestEntityTooLarge {
		return "Request body too large"
	}
	return "Unable to read request body"
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut on a rune boundary.
	for n > 0 && !utf8Start(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// fixturesRecordDir is where live LLM calls are saved, or "".
func (s *Server) fixturesRecordDir() string {
	if s.cfg.FixturesDir == "" {
		return ""
	}
	return filepath.Join(s.cfg.FixturesDir, "llm", "_recorded")
}

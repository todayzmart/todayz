package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// testFixtures is the committed fixtures tree.
const testFixtures = "../../fixtures"

type harness struct {
	t    *testing.T
	srv  *Server
	logs *syncBuffer
}

func newHarness(t *testing.T, mutate func(*Config)) *harness {
	t.Helper()
	logs := &syncBuffer{}
	cfg := Config{
		Fixtures: os.DirFS(testFixtures),
		Capacity: 100,
		Logger:   slog.New(slog.NewJSONHandler(logs, nil)),
		// No live mode unless a test sets it; any accidental upstream call fails loudly.
		UpstreamClient: &http.Client{Transport: failTransport{t}},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return &harness{t: t, srv: New(cfg), logs: logs}
}

type failTransport struct{ t *testing.T }

func (f failTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("unexpected upstream request to %s", r.URL.Host)
	return nil, io.ErrUnexpectedEOF
}

func (h *harness) do(req *http.Request) *httptest.ResponseRecorder {
	h.t.Helper()
	rr := httptest.NewRecorder()
	if req.Host == "example.com" { // httptest.NewRequest's default; the mock allows local hosts only
		req.Host = "127.0.0.1"
	}
	h.srv.ServeHTTP(rr, req)
	return rr
}

func (h *harness) postJSON(path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	var raw []byte
	switch b := body.(type) {
	case string:
		raw = []byte(b)
	case []byte:
		raw = b
	default:
		var err error
		if raw, err = json.Marshal(b); err != nil {
			h.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return h.do(req)
}

func (h *harness) postForm(path string, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return h.do(req)
}

func (h *harness) get(path string) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.do(httptest.NewRequest(http.MethodGet, path, nil))
}

func multipartBody(t *testing.T, fields map[string]string, filename string, file []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := mw.WriteField(k, fields[k]); err != nil {
			t.Fatal(err)
		}
	}
	if file != nil {
		fw, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(file)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func decode(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rr.Body.String())
	}
	return m
}

func decodeItems(t *testing.T, rr *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var m struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("bad list: %v\n%s", err, rr.Body.String())
	}
	return m.Items
}

// wantKeys checks a JSON object has exactly these keys (shape fidelity).
func wantKeys(t *testing.T, m map[string]any, keys ...string) {
	t.Helper()
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	want := append([]string(nil), keys...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

func wantStatus(t *testing.T, rr *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rr.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, status, rr.Body.String())
	}
}

func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("not an object: %#v", v)
	}
	return m
}

var fixedNow = time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC)

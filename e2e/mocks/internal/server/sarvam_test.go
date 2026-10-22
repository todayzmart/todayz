package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func sampleWAV(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(testFixtures + "/stt/stock-update-hinglish.wav")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (h *harness) stt(path string, fields map[string]string, file []byte) *httptest.ResponseRecorder {
	h.t.Helper()
	body, ct := multipartBody(h.t, fields, "clip.wav", file)
	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("api-subscription-key", "test-placeholder")
	return h.do(req)
}

func TestSarvamSTT(t *testing.T) {
	h := newHarness(t, nil)
	rr := h.stt("/sarvam/speech-to-text", map[string]string{"model": "saarika:v2.5", "language_code": "unknown"}, sampleWAV(t))
	wantStatus(t, rr, 200)
	m := decode(t, rr)
	wantKeys(t, m, "request_id", "transcript", "timestamps", "diarized_transcript", "language_code")
	if m["transcript"] != "do kilo atta aur paanch Amul doodh bik gaye" || m["language_code"] != "hi-IN" || m["timestamps"] != nil {
		t.Errorf("response = %v", m)
	}
	rec := h.srv.Store().STT.List(nil, 1)[0]
	if rec.Fixture != "stt/stock-update-hinglish.json" || rec.Status != 200 || rec.Bytes != len(sampleWAV(t)) || rec.LanguageRequested != "unknown" {
		t.Errorf("capture = %+v", rec)
	}
}

func TestSarvamTimestampsAndTranslate(t *testing.T) {
	h := newHarness(t, nil)
	rr := h.stt("/sarvam/speech-to-text", map[string]string{"with_timestamps": "true"}, sampleWAV(t))
	wantStatus(t, rr, 200)
	ts := obj(t, decode(t, rr)["timestamps"])
	wantKeys(t, ts, "words", "start_time_seconds", "end_time_seconds")
	if n := len(ts["words"].([]any)); n != 9 {
		t.Errorf("words = %d", n)
	}

	rr = h.stt("/sarvam/speech-to-text-translate", nil, sampleWAV(t))
	wantStatus(t, rr, 200)
	m := decode(t, rr)
	wantKeys(t, m, "request_id", "transcript", "diarized_transcript", "language_code")
	if m["transcript"] != "two kilos of atta and five Amul milk sold" {
		t.Errorf("translate = %v", m["transcript"])
	}
	if rec := h.srv.Store().STT.List(nil, 1)[0]; rec.Model != "saaras:v2.5" || rec.Endpoint != "speech-to-text-translate" {
		t.Errorf("capture = %+v", rec)
	}
}

func TestSarvamErrors(t *testing.T) {
	h := newHarness(t, nil)
	unknown := []byte("RIFF-not-a-fixture")
	sum := sha256.Sum256(unknown)

	rr := h.stt("/sarvam/speech-to-text", nil, unknown)
	wantStatus(t, rr, 400)
	m := decode(t, rr)
	wantKeys(t, m, "error")
	e := obj(t, m["error"])
	wantKeys(t, e, "message", "code", "request_id")
	if e["code"] != "invalid_request_error" || !strings.Contains(e["message"].(string), hex.EncodeToString(sum[:])) {
		t.Errorf("unknown audio = %v", e)
	}
	if rec := h.srv.Store().STT.List(nil, 1)[0]; rec.Status != 400 || rec.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("miss capture = %+v", rec)
	}

	wantStatus(t, h.stt("/sarvam/speech-to-text", map[string]string{"model": "x"}, nil), 400)                      // no file
	wantStatus(t, h.stt("/sarvam/speech-to-text", nil, []byte{}), 400)                                             // empty file
	wantStatus(t, h.stt("/sarvam/speech-to-text", map[string]string{"language_code": "fr-FR"}, sampleWAV(t)), 400) // unsupported language
	wantStatus(t, h.postJSON("/sarvam/speech-to-text", `{"file":"x"}`, nil), 400)                                  // not multipart
}

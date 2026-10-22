package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"

	"todayz.local/e2e/mocks/internal/capture"
	"todayz.local/e2e/mocks/internal/fixtures"
)

const sttBodyLimit = 32 << 20

var sarvamLanguages = map[string]bool{
	"unknown": true, "hi-IN": true, "bn-IN": true, "kn-IN": true, "ml-IN": true, "mr-IN": true,
	"od-IN": true, "pa-IN": true, "ta-IN": true, "te-IN": true, "en-IN": true, "gu-IN": true,
}

func sarvamError(w http.ResponseWriter, status int, code, msg, reqID string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg, "code": code, "request_id": reqID}})
}

// POST /speech-to-text and /speech-to-text-translate — multipart/form-data
// with "file" (audio) and optional "model", "language_code", "with_timestamps",
// "with_diarization", "prompt". Header api-subscription-key is optional here
// (the mock counts as configured without a key in local/test, 11 §1).
//
// The transcript comes from fixtures/stt/*.json keyed by the sha256 of the
// audio bytes. Unknown audio returns 400 invalid_request_error naming the hash.
// Docs: https://docs.sarvam.ai/api-reference-docs/speech-to-text/transcribe
func (s *Server) sarvamSTT(w http.ResponseWriter, r *http.Request) {
	reqID := "mock_" + randomHex(12)
	endpoint := strings.TrimPrefix(r.URL.Path, "/sarvam/")
	rec := capture.STT{Endpoint: endpoint}
	fail := func(status int, code, msg string) {
		rec.Meta, rec.Status, rec.Error = s.store.Next(), status, msg
		s.store.STT.Add(rec)
		sarvamError(w, status, code, msg, reqID)
	}

	r.Body = http.MaxBytesReader(w, r.Body, sttBodyLimit)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(http.StatusRequestEntityTooLarge, "invalid_request_error", "file is too large")
			return
		}
		fail(http.StatusBadRequest, "invalid_request_error", "body must be multipart/form-data with a file field")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	rec.Model = r.FormValue("model")
	if rec.Model == "" {
		rec.Model = "saarika:v2.5"
		if endpoint == "speech-to-text-translate" {
			rec.Model = "saaras:v2.5"
		}
	}
	rec.LanguageRequested = r.FormValue("language_code")
	if rec.LanguageRequested != "" && !sarvamLanguages[rec.LanguageRequested] {
		fail(http.StatusBadRequest, "invalid_request_error", "language_code: unsupported value "+rec.LanguageRequested)
		return
	}

	f, hdr, err := r.FormFile("file")
	if err != nil {
		fail(http.StatusBadRequest, "invalid_request_error", "file: Field required")
		return
	}
	audio, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil || len(audio) == 0 {
		fail(http.StatusBadRequest, "invalid_request_error", "file: audio is empty")
		return
	}
	sum := sha256.Sum256(audio)
	rec.SHA256 = hex.EncodeToString(sum[:])
	rec.Bytes = len(audio)
	rec.Filename = hdr.Filename
	rec.ContentType = hdr.Header.Get("Content-Type")

	all, broken, err := fixtures.LoadSTT(s.cfg.Fixtures)
	if err != nil {
		fail(http.StatusInternalServerError, "internal_server_error", "tz-mock: "+err.Error())
		return
	}
	fx, ok := all[rec.SHA256]
	if !ok && len(broken) > 0 {
		// The fixture for this audio may be one of the files that failed to load.
		fail(http.StatusInternalServerError, "internal_server_error",
			"tz-mock: no STT fixture for audio sha256 "+rec.SHA256+", and some STT fixtures are broken: "+joinErrors(broken))
		return
	}
	if !ok {
		fail(http.StatusBadRequest, "invalid_request_error",
			"tz-mock: no STT fixture for audio sha256 "+rec.SHA256+"; add e2e/mocks/fixtures/stt/<name>.json with {sha256, language_code, transcript}")
		return
	}
	rec.Fixture = fx.Path
	rec.LanguageCode = fx.LanguageCode
	rec.Transcript = fx.Transcript
	if endpoint == "speech-to-text-translate" && fx.Translation != "" {
		rec.Transcript = fx.Translation
	}
	rec.Meta, rec.Status = s.store.Next(), http.StatusOK
	s.store.STT.Add(rec)

	resp := map[string]any{
		"request_id":          reqID,
		"transcript":          rec.Transcript,
		"language_code":       fx.LanguageCode,
		"diarized_transcript": nil,
	}
	if endpoint == "speech-to-text" {
		resp["timestamps"] = nil
		if r.FormValue("with_timestamps") == "true" {
			resp["timestamps"] = wordTimestamps(rec.Transcript)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// wordTimestamps spreads words evenly, 0.4 s each (deterministic).
func wordTimestamps(t string) map[string]any {
	words := strings.Fields(t)
	start := make([]float64, len(words))
	end := make([]float64, len(words))
	for i := range words {
		start[i] = float64(i) * 0.4
		end[i] = start[i] + 0.35
	}
	return map[string]any{"words": words, "start_time_seconds": start, "end_time_seconds": end}
}

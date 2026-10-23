package jobs

import (
	"unicode/utf8"

	"github.com/todayz/backend/internal/platform/telemetry"
)

// Size caps on what a run stores (job_runs CHECKs).
const (
	maxErrorBytes = 2048
	maxNoteRunes  = 500
)

// scrubError is the error text a run stores and logs (SC-JB-04): phones,
// emails, bearer tokens, JWTs, credentials and inline codes masked by the
// shared redactor, then cut to 2 kB on a UTF-8 boundary.
func scrubError(err error) string {
	if err == nil {
		return ""
	}
	return truncateBytes(telemetry.RedactString(err.Error()), maxErrorBytes)
}

// scrubNote is a handler's note, redacted and cut to 500 characters.
func scrubNote(s string) string {
	s = telemetry.RedactString(s)
	if utf8.RuneCountInString(s) <= maxNoteRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxNoteRunes-1]) + "…"
}

// truncateBytes cuts s to at most n bytes without splitting a rune; a cut
// string ends with "…" inside the limit.
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	const ell = "…" // 3 bytes
	cut := n - len(ell)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ell
}

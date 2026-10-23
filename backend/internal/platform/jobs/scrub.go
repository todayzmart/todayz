package jobs

import (
	"strings"
	"unicode/utf8"

	"github.com/todayz/backend/internal/platform/telemetry"
)

// Size caps on what a run stores (job_runs CHECKs).
const (
	maxErrorBytes = 2048
	maxNoteRunes  = 500
)

// cleanText makes s storable: invalid UTF-8 becomes U+FFFD and NUL bytes are
// dropped. Postgres refuses both in text (and "\u0000" in jsonb), and an
// OTLP export refuses invalid UTF-8, so a handler error carrying raw bytes
// would otherwise leave its run stuck in `running` (the finish write fails)
// until the watchdog calls it abandoned.
func cleanText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	return s
}

// scrubError is the error text a run stores and logs (SC-JB-04): made valid
// UTF-8 without NULs, then phones, emails, bearer tokens, JWTs, credentials
// and inline codes masked by the shared redactor, then cut to 2 kB on a
// UTF-8 boundary.
func scrubError(err error) string {
	if err == nil {
		return ""
	}
	return truncateBytes(telemetry.RedactString(cleanText(err.Error())), maxErrorBytes)
}

// scrubNote is a handler's note, cleaned, redacted and cut to 500 characters.
func scrubNote(s string) string {
	s = telemetry.RedactString(cleanText(s))
	if utf8.RuneCountInString(s) <= maxNoteRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxNoteRunes-1]) + "…"
}

// scrubSummary cleans a handler's summary keys the same way (the summary is
// stored as jsonb). Keys that become equal are summed.
func scrubSummary(m map[string]int64) map[string]int64 {
	if m == nil {
		return nil
	}
	out := make(map[string]int64, len(m))
	for k, v := range m {
		out[cleanText(k)] += v
	}
	return out
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

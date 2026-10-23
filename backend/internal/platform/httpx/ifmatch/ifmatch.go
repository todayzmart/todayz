// Package ifmatch reads the version out of an If-Match header (04 §1.7).
//
// The API sends a strong ETag, "v{version}". A proxy that compresses the
// response may hand the client a changed tag: Caddy appends the coding
// ("v3-gzip"), Cloudflare weakens it (W/"v3"). Clients send back what they
// got, so Parse accepts those forms and reads only the version (D-109).
package ifmatch

import (
	"strconv"
	"strings"
)

// Parse returns the version of "v{n}", W/"v{n}" or "v{n}-{coding}".
func Parse(s string) (int32, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "W/")
	if len(s) < 4 || !strings.HasPrefix(s, `"v`) || !strings.HasSuffix(s, `"`) {
		return 0, false
	}
	s = s[2 : len(s)-1]
	if i := strings.IndexByte(s, '-'); i >= 0 {
		if !isCoding(s[i+1:]) {
			return 0, false
		}
		s = s[:i]
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n < 0 || strings.HasPrefix(s, "+") {
		return 0, false
	}
	return int32(n), true
}

func isCoding(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

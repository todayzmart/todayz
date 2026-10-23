package search

import (
	"encoding/base64"
	"strconv"
	"strings"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/httpx/problem"
)

// Cursors are opaque to clients (04 §1): base64url of "<list>:<offset>".
// Every list here is computed in full and sliced (search ranks in memory, the
// store lists are bounded), so an offset is enough. A cursor of another list
// or one that does not decode is a 422 on query.cursor.

const (
	cursorSearch = "s"
	cursorStores = "l"
	cursorItems  = "i"
)

// maxOffset bounds an offset read from a cursor.
const maxOffset = 1_000_000

func encodeCursor(list string, offset int) *string {
	c := base64.RawURLEncoding.EncodeToString([]byte(list + ":" + strconv.Itoa(offset)))
	return &c
}

// decodeCursor returns the offset of c; a nil or empty c is the first page.
func decodeCursor(list string, c *string) (int, error) {
	if c == nil || *c == "" {
		return 0, nil
	}
	bad := problem.New(genproblems.ValidationFailed, "The cursor is not valid for this list.").
		WithField("query.cursor", "not a cursor from this list")
	raw, err := base64.RawURLEncoding.DecodeString(*c)
	if err != nil {
		return 0, bad
	}
	kind, num, ok := strings.Cut(string(raw), ":")
	if !ok || kind != list {
		return 0, bad
	}
	off, err := strconv.Atoi(num)
	if err != nil || off <= 0 || off > maxOffset || strconv.Itoa(off) != num {
		return 0, bad
	}
	return off, nil
}

// pageLimit is the request's limit, or def.
func pageLimit(l *int, def int) int {
	if l == nil || *l <= 0 {
		return def
	}
	return *l
}

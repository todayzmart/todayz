// Package ids makes ULIDs (03: ids are ULIDs stored as uuid, generated in Go).
//
// A ULID is time-ordered, so new rows land at the end of the primary-key index.
// The 128 bits map one-to-one onto a Postgres uuid; the API shows the 26-char
// Crockford form.
package ids

import (
	"crypto/rand"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oklog/ulid/v2"
)

// ID is a ULID.
type ID = ulid.ULID

// Zero is the empty ID.
var Zero ID

// ErrInvalid is returned for a string that is not a ULID.
var ErrInvalid = errors.New("ids: not a ULID")

// Generator makes monotonic ULIDs: ids made in the same millisecond still sort
// in creation order. It is safe for concurrent use.
type Generator struct {
	mu      sync.Mutex
	entropy io.Reader
}

// NewGenerator returns a generator that reads randomness from crypto/rand.
func NewGenerator() *Generator {
	return &Generator{entropy: ulid.Monotonic(rand.Reader, 0)}
}

// New returns a ULID for time t (callers pass clock.Now()).
func (g *Generator) New(t time.Time) ID {
	g.mu.Lock()
	defer g.mu.Unlock()
	return ulid.MustNew(ulid.Timestamp(t), g.entropy)
}

var std = NewGenerator()

// New returns a ULID for time t from the package generator.
func New(t time.Time) ID { return std.New(t) }

// Parse reads a 26-character ULID string (case-insensitive).
func Parse(s string) (ID, error) {
	id, err := ulid.ParseStrict(s)
	if err != nil {
		return Zero, ErrInvalid
	}
	return id, nil
}

// Valid reports whether s is a ULID string.
func Valid(s string) bool {
	_, err := Parse(s)
	return err == nil
}

// UUID converts an ID for a uuid column.
func UUID(id ID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }

// FromUUID converts a uuid column value back to an ID.
func FromUUID(u pgtype.UUID) (ID, bool) {
	if !u.Valid {
		return Zero, false
	}
	return ID(u.Bytes), true
}

// Time returns the timestamp embedded in id.
func Time(id ID) time.Time { return ulid.Time(id.Time()).UTC() }

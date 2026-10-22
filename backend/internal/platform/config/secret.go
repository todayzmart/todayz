package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

const redacted = "[redacted]"

// Secret holds a key or credential. It never prints, logs or marshals its
// value: fmt, slog and encoding/json all see "[redacted]". The bytes live in
// a closure, not a field, so reflection-based printers (spew, testify's
// failure diffs, %+v of a containing struct through reflection) see only a
// func pointer. Call Bytes or Reveal only where the value is used (a cipher,
// an HTTP header).
type Secret struct {
	get func() []byte
}

// NewSecret wraps a copy of b.
func NewSecret(b []byte) Secret {
	if len(b) == 0 {
		return Secret{}
	}
	v := append([]byte(nil), b...)
	return Secret{get: func() []byte { return v }}
}

// IsSet reports whether the secret has a value.
func (s Secret) IsSet() bool { return s.get != nil }

// Bytes returns a copy of the value.
func (s Secret) Bytes() []byte {
	if s.get == nil {
		return nil
	}
	return append([]byte(nil), s.get()...)
}

// Reveal returns the value as a string. Use it only to hand the value to the
// one place that needs it.
func (s Secret) Reveal() string { return string(s.Bytes()) }

// String implements fmt.Stringer.
func (s Secret) String() string { return redacted }

// GoString implements fmt.GoStringer (%#v).
func (s Secret) GoString() string { return redacted }

// Format makes every fmt verb print "[redacted]".
func (s Secret) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// LogValue implements slog.LogValuer.
func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON implements json.Marshaler.
func (s Secret) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }

// MarshalText implements encoding.TextMarshaler.
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

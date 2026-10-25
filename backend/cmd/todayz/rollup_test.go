package main

import (
	"context"
	"errors"
	"io"
	"testing"
)

// The flags are checked before anything boots: a bad call is a usage error
// and never touches the database.
func TestRollupUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"--to", "2026-10-20"},
		{"--from", "20-10-2026"},
		{"--from", "2026-10-20", "--to", "2026-10-19"},
		{"--from", "2025-01-01", "--to", "2026-10-20"},
		{"--from", "2026-10-20", "extra"},
	} {
		err := runRollup(context.Background(), args, func(string) string { return "" }, io.Discard)
		var u usageError
		if !errors.As(err, &u) {
			t.Errorf("%v: err = %v, want a usage error", args, err)
		}
	}
}

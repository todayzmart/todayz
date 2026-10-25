package admin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
)

func TestE164(t *testing.T) {
	for in, want := range map[string]string{
		"+919876543210":   "+919876543210",
		"919876543210":    "+919876543210",
		"9876543210":      "+919876543210",
		"+91 98765 43210": "+919876543210",
	} {
		got, ok := e164(in)
		require.True(t, ok, in)
		require.Equal(t, want, got)
	}
	for _, in := range []string{"", "12345", "+11234567890", "0123456789", "+9198765432101"} {
		_, ok := e164(in)
		require.False(t, ok, in)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 25, 9, 30, 0, 123000, time.UTC)
	id := ids.New(at)
	gotAt, gotID, err := decodeCursor(encodeCursor(at, id))
	require.NoError(t, err)
	require.True(t, at.Equal(gotAt))
	require.Equal(t, id, gotID)
	_, _, _, err = pageArgs(nil, new("not-a-cursor"))
	require.Error(t, err)
}

func TestCleanReason(t *testing.T) {
	_, err := cleanReason("   ")
	require.Error(t, err)
	r, err := cleanReason("  ok  ")
	require.NoError(t, err)
	require.Equal(t, "ok", r)
}

func TestEverySubjectTypeHasAViewOrIsRefused(t *testing.T) {
	// business has no view action yet (A-02): refused, never unchecked.
	_, ok := subjectView["business"]
	require.False(t, ok)
	require.Len(t, subjectView, 8)
}

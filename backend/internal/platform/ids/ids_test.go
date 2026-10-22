package ids

import (
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 10, 22, 4, 30, 0, 0, time.UTC)

func TestNewIsSortableAndCarriesTime(t *testing.T) {
	a := New(t0)
	b := New(t0.Add(time.Millisecond))
	require.Less(t, a.String(), b.String())
	require.Equal(t, t0, Time(a))
	require.Len(t, a.String(), 26)
}

func TestMonotonicWithinOneMillisecond(t *testing.T) {
	g := NewGenerator()
	var got []string
	for range 1000 {
		got = append(got, g.New(t0).String())
	}
	require.True(t, sort.StringsAreSorted(got), "ids made in the same millisecond keep creation order")
}

func TestConcurrentUse(t *testing.T) {
	g := NewGenerator()
	var mu sync.Mutex
	seen := map[ID]bool{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				id := g.New(t0)
				mu.Lock()
				require.False(t, seen[id])
				seen[id] = true
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	require.Len(t, seen, 1600)
}

func TestParse(t *testing.T) {
	id := New(t0)
	for _, s := range []string{id.String(), strings.ToLower(id.String())} {
		got, err := Parse(s)
		require.NoError(t, err)
		require.Equal(t, id, got)
	}
	for _, bad := range []string{"", "nope", id.String() + "0", "8ZZZZZZZZZZZZZZZZZZZZZZZZZ", "01J!!!!!!!!!!!!!!!!!!!!!!!"} {
		_, err := Parse(bad)
		require.ErrorIs(t, err, ErrInvalid, bad)
		require.False(t, Valid(bad))
	}
}

func TestUUIDRoundTrip(t *testing.T) {
	id := New(t0)
	u := UUID(id)
	require.True(t, u.Valid)
	back, ok := FromUUID(u)
	require.True(t, ok)
	require.Equal(t, id, back)

	_, ok = FromUUID(pgtype.UUID{})
	require.False(t, ok)
}

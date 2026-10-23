//go:build realtimeload

package realtime

// The 2,000-stream load test (W2-05). Not part of make check: run it on
// purpose, once per change to the hub or the stream handler, and record the
// numbers in the QA report:
//
//	go test -tags realtimeload -run TestLoad2000Streams -count=1 -v ./internal/platform/realtime/
//
// It needs Docker (testcontainers). Client and server share the process, so
// memory and goroutines are the whole test's; the per-stream figures are an
// upper bound for the server side.

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/testkit"
)

func TestLoad2000Streams(t *testing.T) {
	const (
		channels   = 1000
		perChannel = 2
		streams    = channels * perChannel
		publishers = 8
	)
	db := testkit.NewDB(t)
	h := startHub(t, db.Pool, Options{})
	srvStream := &Stream{Hub: h, DB: db.Pool, App: "customer"}
	chans := make([]Channel, channels)
	for i := range chans {
		chans[i] = UserChannel(newID())
	}
	// One server; the channel comes from the query string.
	srvStream.Channel = func(r *http.Request) (Channel, error) { return Channel(r.URL.Query().Get("c")), nil }
	srv := streamServer(t, srvStream, "", nil)

	var m0 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	g0 := runtime.NumGoroutine()

	conns := make([]*sseConn, streams)
	start := clock.Real{}.Now()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 100)
	for i := 0; i < streams; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			c := dial(t, srv.URL+"?c="+string(chans[i%channels]), "")
			c.ready(t)
			conns[i] = c
		}(i)
	}
	wg.Wait()
	connectTook := clock.Real{}.Now().Sub(start)
	require.Equal(t, streams, h.Len())

	var m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)
	g1 := runtime.NumGoroutine()

	// Publish one event per channel from several transactions at once; each
	// reaches the channel's two streams.
	var sent sync.Map // event id -> commit start
	var got atomic.Int64
	lat := make(chan time.Duration, streams)
	for _, c := range conns {
		go func(c *sseConn) {
			for f := range c.frames {
				if f.Event != string(OrderUpdated) {
					continue
				}
				now := clock.Real{}.Now()
				var ev Event
				if json.Unmarshal([]byte(f.Data), &ev) != nil {
					continue
				}
				if at, ok := sent.Load(ev.ID); ok {
					lat <- now.Sub(at.(time.Time))
					got.Add(1)
				}
			}
		}(c)
	}
	pubStart := clock.Real{}.Now()
	var pw sync.WaitGroup
	for p := 0; p < publishers; p++ {
		pw.Add(1)
		go func(p int) {
			defer pw.Done()
			ctx := context.Background()
			for i := p; i < channels; i += publishers {
				ev := Event{Type: OrderUpdated, ID: ids.New(clock.Real{}.Now()), Version: 1}
				tx, err := db.Pool.Begin(ctx)
				if err != nil {
					t.Error(err)
					return
				}
				if err := Publish(ctx, tx, chans[i], ev); err != nil {
					t.Error(err)
					return
				}
				sent.Store(ev.ID, clock.Real{}.Now())
				if err := tx.Commit(ctx); err != nil {
					t.Error(err)
					return
				}
			}
		}(p)
	}
	pw.Wait()
	publishTook := clock.Real{}.Now().Sub(pubStart)

	var ds []time.Duration
	deadline := time.After(60 * time.Second)
	for len(ds) < streams {
		select {
		case d := <-lat:
			ds = append(ds, d)
		case <-deadline:
			t.Fatalf("delivered %d of %d", len(ds), streams)
		}
	}
	deliverTook := clock.Real{}.Now().Sub(pubStart)
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	pct := func(p float64) time.Duration { return ds[int(float64(len(ds)-1)*p)] }
	require.Equal(t, streams, h.Len(), "no stream was dropped as a slow consumer")

	heap := int64(m1.HeapInuse) - int64(m0.HeapInuse)
	t.Logf("streams=%d channels=%d connect=%s", streams, channels, connectTook.Round(time.Millisecond))
	t.Logf("heap in use: %.1f MiB more with the streams open (%.1f KiB per stream, client and server)",
		float64(heap)/(1<<20), float64(heap)/1024/streams)
	t.Logf("goroutines: %d before, %d with the streams open (%.2f per stream, client and server)",
		g0, g1, float64(g1-g0)/streams)
	t.Logf("published %d events (%d publishers) in %s; all %d deliveries in %s",
		channels, publishers, publishTook.Round(time.Millisecond), len(ds), deliverTook.Round(time.Millisecond))
	t.Logf("publish->deliver: p50=%s p90=%s p99=%s max=%s",
		pct(0.50).Round(10*time.Microsecond), pct(0.90).Round(10*time.Microsecond),
		pct(0.99).Round(10*time.Microsecond), ds[len(ds)-1].Round(10*time.Microsecond))
}

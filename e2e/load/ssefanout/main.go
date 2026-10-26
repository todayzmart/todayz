// Command ssefanout is the W4-02 sse_fanout scenario (08 §6): 2,000 business and 3,000 customer
// Server-Sent Event streams held open while 20 events a second are published, measuring how
// long each event takes to reach every stream of its channel and how the API's memory moves.
// k6 cannot hold thousands of streaming responses, so this is a small stdlib-only Go program.
//
//	go run . -api http://127.0.0.1:8180 -mocks http://127.0.0.1:4010 -fixture ../out/fixture.json -out ../out
//
// Streams: 2 per load store (stores 1…1000, signed in as their pool-A owner) and 10 per customer
// for 300 customers. Events: order.updated on a random store channel (70%) or user channel (30%),
// published the way the API publishes (outbox row + pg_notify in one statement) through psql on
// DATABASE_URL, whose value this program never prints. The event id is a fresh ULID that marks the
// event; delivery latency is the time from writing the statement to reading the frame.
//
// Pass: delivery p95 < 2 s; memory stable (API RSS at the end within 10% of the RSS after the
// streams opened, and no stream lost for good). Prints counts, durations and sizes only.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type fixtureFile struct {
	Stores []struct {
		N   int    `json:"n"`
		ID  string `json:"id"`
		BID string `json:"bid"`
	} `json:"stores"`
}

type session struct {
	token  string
	userID string
}

type cfg struct {
	api, mocks, fixture, out string
	storeStreams, customers  int
	perCustomer              int
	rate                     float64
	duration, ramp           time.Duration
}

func main() {
	var c cfg
	flag.StringVar(&c.api, "api", "http://127.0.0.1:8180", "API base URL (loopback only)")
	flag.StringVar(&c.mocks, "mocks", "http://127.0.0.1:4010", "mocks base URL (loopback only)")
	flag.StringVar(&c.fixture, "fixture", "../out/fixture.json", "fixture from make load-data")
	flag.StringVar(&c.out, "out", "../out", "where sse_fanout-summary.json goes")
	flag.IntVar(&c.storeStreams, "store-streams", 2000, "business streams (2 per store)")
	flag.IntVar(&c.customers, "customers", 300, "customers")
	flag.IntVar(&c.perCustomer, "per-customer", 10, "streams per customer")
	flag.Float64Var(&c.rate, "rate", 20, "events published per second")
	flag.DurationVar(&c.duration, "duration", 8*time.Minute, "publishing time after the streams are open (tokens live 15 min)")
	flag.DurationVar(&c.ramp, "ramp", 60*time.Second, "time over which the streams open")
	flag.Parse()
	if err := run(c); err != nil {
		fmt.Fprintln(os.Stderr, "sse_fanout:", err)
		os.Exit(1)
	}
}

func loopback(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.User != nil || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("refusing base URL (not a plain loopback URL)")
	}
	h := u.Hostname()
	if h != "127.0.0.1" && h != "localhost" && h != "::1" && !strings.HasSuffix(h, ".todayz.localhost") {
		return fmt.Errorf("refusing non-local base URL")
	}
	return nil
}

// --- run ---------------------------------------------------------------------------------------

type stats struct {
	mu        sync.Mutex
	published map[string]pub // event id -> publish time and expected receivers
	lat       []float64      // ms, one per delivery
	got       map[string]int
}

type pub struct {
	at   time.Time
	want int
}

var (
	opened, failedOpen, reconnects, lost, frames atomic.Int64
	openNow                                      atomic.Int64
)

func run(c cfg) error {
	if err := loopback(c.api); err != nil {
		return err
	}
	if err := loopback(c.mocks); err != nil {
		return err
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set (make load-sse exports it from infra/local/local.env)")
	}
	if u, err := url.Parse(dbURL); err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || u.Port() != "5433" {
		return errors.New("refusing DATABASE_URL that is not the local stack (127.0.0.1:5433)")
	}
	raw, err := os.ReadFile(c.fixture)
	if err != nil {
		return fmt.Errorf("read fixture: %w", err)
	}
	var fx fixtureFile
	if err := json.Unmarshal(raw, &fx); err != nil {
		return fmt.Errorf("parse fixture: %w", err)
	}
	stores := len(fx.Stores)
	if stores == 0 {
		return errors.New("fixture has no stores")
	}

	tr := &http.Transport{
		MaxIdleConns: 200, MaxIdleConnsPerHost: 200, IdleConnTimeout: 30 * time.Second,
		DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	}
	hc := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	streamClient := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: -1, DisableKeepAlives: true,
		DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext}}

	// Sign-ins: the owners of the stores the business streams use, and the customers.
	t0 := time.Now()
	perStore := 2
	storeCount := c.storeStreams / perStore
	if storeCount > stores {
		storeCount = stores
	}
	owners := (storeCount + 24) / 25
	ownerSess := make([]session, owners+1)
	custSess := make([]session, c.customers)
	var signFail atomic.Int64
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := 1; i <= owners; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s, err := signIn(hc, c, "701"+fmt.Sprintf("%07d", i), "business")
			if err != nil {
				signFail.Add(1)
				fmt.Fprintf(os.Stderr, "owner %d: %v\n", i, err)
				return
			}
			ownerSess[i] = s
		}(i)
	}
	for i := 0; i < c.customers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s, err := signIn(hc, c, randomNational(), "customer")
			if err != nil {
				signFail.Add(1)
				fmt.Fprintf(os.Stderr, "customer %d: %v\n", i, err)
				return
			}
			custSess[i] = s
		}(i)
	}
	wg.Wait()
	if signFail.Load() > 0 {
		return fmt.Errorf("%d sign-ins failed", signFail.Load())
	}
	fmt.Printf("signed in %d owners and %d customers in %s\n", owners, c.customers, time.Since(t0).Round(time.Millisecond))

	// Streams.
	type target struct {
		path, token, channel string
	}
	var targets []target
	for n := 1; n <= storeCount; n++ {
		st := fx.Stores[n-1]
		for k := 0; k < perStore; k++ {
			targets = append(targets, target{
				path:    "/v1/biz/businesses/" + st.BID + "/stores/" + st.ID + "/stream",
				token:   ownerSess[(n-1)/25+1].token,
				channel: "store:" + st.ID,
			})
		}
	}
	for _, s := range custSess {
		for k := 0; k < c.perCustomer; k++ {
			targets = append(targets, target{path: "/v1/me/stream", token: s.token, channel: "user:" + s.userID})
		}
	}
	receivers := map[string]int{}
	for _, t := range targets {
		receivers[t.channel]++
	}
	channels := make([]string, 0, len(receivers))
	for ch := range receivers {
		channels = append(channels, ch)
	}
	sort.Strings(channels)
	var storeChans, userChans []string
	for _, ch := range channels {
		if strings.HasPrefix(ch, "store:") {
			storeChans = append(storeChans, ch)
		} else {
			userChans = append(userChans, ch)
		}
	}

	apiPID := listenerPID("8180")
	rss0 := rssKB(apiPID)
	st := &stats{published: map[string]pub{}, got: map[string]int{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{}, len(targets))
	gap := c.ramp / time.Duration(len(targets))
	t1 := time.Now()
	for i, t := range targets {
		go hold(ctx, streamClient, c.api+t.path, t.token, st, ready)
		if i%50 == 49 {
			time.Sleep(gap * 50)
		}
	}
	deadline := time.After(c.ramp + 60*time.Second)
	readyN := 0
wait:
	for readyN < len(targets) {
		select {
		case <-ready:
			readyN++
		case <-deadline:
			break wait
		}
	}
	fmt.Printf("streams ready %d of %d in %s (failed opens %d)\n", readyN, len(targets), time.Since(t1).Round(time.Millisecond), failedOpen.Load())
	if readyN < len(targets)*95/100 {
		return fmt.Errorf("only %d of %d streams opened; see the API log (ulimit -n, MaxStreams)", readyN, len(targets))
	}

	time.Sleep(5 * time.Second)
	rssOpen := rssKB(apiPID)
	fmt.Printf("api rss before the streams %d MB, with them open %d MB\n", rss0/1024, rssOpen/1024)

	// Publisher: psql reading statements on stdin, autocommit (each statement commits and notifies).
	cmd := exec.CommandContext(ctx, "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-o", "/dev/null", "--dbname", dbURL)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	var psqlErr bytes.Buffer
	cmd.Stderr = &psqlErr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start psql: %w", err)
	}
	pw := bufio.NewWriter(stdin)

	type sample struct {
		At    string `json:"at"`
		RSSMB int64  `json:"rssMB"`
		Open  int64  `json:"open"`
	}
	var samples []sample
	tick := time.NewTicker(time.Duration(float64(time.Second) / c.rate))
	defer tick.Stop()
	sampleT := time.NewTicker(15 * time.Second)
	defer sampleT.Stop()
	end := time.After(c.duration)
	published := 0
	t2 := time.Now()
pubLoop:
	for {
		select {
		case <-tick.C:
			ch := storeChans[mrand.IntN(len(storeChans))]
			if mrand.Float64() < 0.3 && len(userChans) > 0 {
				ch = userChans[mrand.IntN(len(userChans))]
			}
			id := ulid()
			ev := `{"type":"order.updated","id":"` + id + `","version":1}`
			st.mu.Lock()
			st.published[id] = pub{at: time.Now(), want: receivers[ch]}
			st.mu.Unlock()
			fmt.Fprintf(pw, "WITH ins AS (INSERT INTO outbox_events (channel, payload) VALUES ('%s', '%s'::jsonb) RETURNING seq) "+
				"SELECT pg_notify('tz_realtime', json_build_object('s', ins.seq, 'c', '%s'::text, 'e', '%s'::jsonb, "+
				"'x', pg_snapshot_xmin(pg_current_snapshot())::text, 't', (extract(epoch FROM clock_timestamp()) * 1000)::bigint)::text) FROM ins;\n",
				ch, ev, ch, ev)
			if err := pw.Flush(); err != nil {
				return fmt.Errorf("psql stdin: %w (stderr: %s)", err, firstLine(psqlErr.String()))
			}
			published++
		case <-sampleT.C:
			r := rssKB(apiPID)
			samples = append(samples, sample{At: time.Now().UTC().Format(time.RFC3339), RSSMB: r / 1024, Open: openNow.Load()})
			fmt.Printf("t+%s published %d, deliveries %d, open %d, reconnects %d, api rss %d MB\n",
				time.Since(t2).Round(time.Second), published, frames.Load(), openNow.Load(), reconnects.Load(), r/1024)
		case <-end:
			break pubLoop
		}
	}
	time.Sleep(5 * time.Second) // the last events land
	_ = stdin.Close()
	_ = cmd.Wait()
	rssEnd := rssKB(apiPID)
	cancel()

	st.mu.Lock()
	defer st.mu.Unlock()
	lat := append([]float64(nil), st.lat...)
	sort.Float64s(lat)
	want, missing := 0, 0
	for id, p := range st.published {
		want += p.want
		if g := st.got[id]; g < p.want {
			missing += p.want - g
		}
	}
	p := func(q float64) float64 {
		if len(lat) == 0 {
			return 0
		}
		return lat[int(q*float64(len(lat)-1))]
	}
	growth := 0.0
	if rssOpen > 0 {
		growth = float64(rssEnd-rssOpen) / float64(rssOpen)
	}
	passLat := len(lat) > 0 && p(0.95) < 2000
	passMem := growth < 0.10 && lost.Load() == 0
	res := map[string]any{
		"scenario": "sse_fanout", "at": time.Now().UTC().Format(time.RFC3339),
		"streams": len(targets), "storeStreams": storeCount * perStore, "customerStreams": c.customers * c.perCustomer,
		"ready": readyN, "failedOpens": failedOpen.Load(), "reconnects": reconnects.Load(), "lostStreams": lost.Load(),
		"published": published, "ratePerSec": c.rate, "durationSec": c.duration.Seconds(),
		"deliveriesExpected": want, "deliveries": len(lat), "deliveriesMissing": missing,
		"latencyMs": map[string]float64{"p50": p(0.50), "p95": p(0.95), "p99": p(0.99), "max": p(1)},
		"apiRssMB":  map[string]int64{"beforeStreams": rss0 / 1024, "afterOpen": rssOpen / 1024, "end": rssEnd / 1024},
		"rssGrowth": growth, "samples": samples,
		"pass": map[string]bool{"deliveryP95Under2s": passLat, "memoryStable": passMem},
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(filepath.Join(c.out, "sse_fanout-summary.json"), b, 0o644); err != nil {
		return err
	}
	verdict := func(ok bool) string {
		if ok {
			return "PASS"
		}
		return "FAIL"
	}
	fmt.Printf("\n== sse_fanout\nstreams %d (business %d, customer %d), ready %d, reconnects %d, lost %d\n",
		len(targets), storeCount*perStore, c.customers*c.perCustomer, readyN, reconnects.Load(), lost.Load())
	fmt.Printf("events %d at %.0f/s, deliveries %d of %d expected (missing %d)\n", published, c.rate, len(lat), want, missing)
	fmt.Printf("delivery ms p50=%.1f p95=%.1f p99=%.1f max=%.1f  %s (p95 < 2000)\n", p(0.5), p(0.95), p(0.99), p(1), verdict(passLat))
	fmt.Printf("api rss MB before %d, open %d, end %d (growth %.1f%%)  %s (growth < 10%%, no stream lost)\n",
		rss0/1024, rssOpen/1024, rssEnd/1024, growth*100, verdict(passMem))
	if !passLat || !passMem {
		return errors.New("FAIL")
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// hold keeps one stream open, reconnecting with Last-Event-ID as the API asks (`event:
// reconnect` or a dropped connection), and records each order.updated frame it reads.
func hold(ctx context.Context, hc *http.Client, u, token string, st *stats, ready chan<- struct{}) {
	lastID := ""
	first := true
	failures := 0
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "text/event-stream")
		if lastID != "" {
			req.Header.Set("Last-Event-ID", lastID)
		}
		resp, err := hc.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			if ctx.Err() != nil {
				return
			}
			failedOpen.Add(1)
			failures++
			if failures > 10 {
				lost.Add(1)
				return
			}
			time.Sleep(time.Duration(failures) * time.Second)
			continue
		}
		failures = 0
		if !first {
			reconnects.Add(1)
		}
		opened.Add(1)
		openNow.Add(1)
		lastID = read(resp.Body, st, lastID, func() {
			if first {
				first = false
				ready <- struct{}{}
			}
		})
		openNow.Add(-1)
		_ = resp.Body.Close()
		if ctx.Err() == nil {
			time.Sleep(time.Second) // retry: 1000 on reconnect
		}
	}
}

// read parses SSE frames until the stream ends; returns the last id seen.
func read(body io.Reader, st *stats, lastID string, onReady func()) string {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	var event, data string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if event == "ready" {
				onReady()
			}
			if event == "order.updated" && data != "" {
				now := time.Now()
				var ev struct {
					ID string `json:"id"`
				}
				if json.Unmarshal([]byte(data), &ev) == nil {
					st.mu.Lock()
					if p, ok := st.published[ev.ID]; ok {
						st.lat = append(st.lat, float64(now.Sub(p.at).Microseconds())/1000)
						st.got[ev.ID]++
					}
					st.mu.Unlock()
					frames.Add(1)
				}
			}
			if event == "reconnect" {
				return lastID
			}
			event, data = "", ""
		case strings.HasPrefix(line, "event: "):
			event = line[7:]
		case strings.HasPrefix(line, "data: "):
			data = line[6:]
		case strings.HasPrefix(line, "id: "):
			lastID = line[4:]
		}
	}
	return lastID
}

// --- sign-in -----------------------------------------------------------------------------------

func signIn(hc *http.Client, c cfg, national, app string) (session, error) {
	phone := "+91" + national
	ip := fakeIP()
	since := time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	var send struct {
		RequestID string `json:"requestId"`
	}
	for attempt := 0; ; attempt++ {
		st, ra, err := postJSON(hc, c.api+"/v1/auth/otp/send", ip, map[string]any{
			"phone": phone, "purpose": "login", "app": app, "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
		}, &send)
		if err != nil {
			return session{}, err
		}
		if st == http.StatusOK {
			break
		}
		if st == http.StatusTooManyRequests && attempt == 0 && ra > 0 && ra <= 35 {
			time.Sleep(time.Duration(ra+1) * time.Second)
			since = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
			continue
		}
		return session{}, fmt.Errorf("otp send answered %d", st)
	}
	q := url.Values{"to": {phone}, "since": {since}, "wait": {"8"}}
	resp, err := hc.Get(c.mocks + "/_mock/sms/latest-otp?" + q.Encode())
	if err != nil {
		return session{}, fmt.Errorf("mocks: %w", err)
	}
	var otp struct {
		OTP string `json:"otp"`
	}
	err = json.NewDecoder(resp.Body).Decode(&otp)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || err != nil {
		return session{}, fmt.Errorf("mocks answered %d", resp.StatusCode)
	}
	var v struct {
		AccessToken string `json:"accessToken"`
		UserID      string `json:"userId"`
	}
	st, _, err := postJSON(hc, c.api+"/v1/auth/otp/verify", ip, map[string]any{
		"requestId": send.RequestID, "code": otp.OTP, "app": app, "deviceId": ulid(),
	}, &v)
	if err != nil {
		return session{}, err
	}
	if st != http.StatusOK {
		return session{}, fmt.Errorf("otp verify answered %d", st)
	}
	return session{token: v.AccessToken, userID: v.UserID}, nil
}

func postJSON(hc *http.Client, u, ip string, body any, out any) (int, int, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, u, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("CF-Connecting-IP", ip)
	req.Header.Set("Idempotency-Key", ulid())
	resp, err := hc.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	ra, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
	if resp.StatusCode == http.StatusOK && out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return 0, 0, err
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode, ra, nil
}

// --- helpers -----------------------------------------------------------------------------------

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func ulid() string {
	var b [26]byte
	t := uint64(time.Now().UnixMilli())
	for i := 9; i >= 0; i-- {
		b[i] = crockford[t%32]
		t /= 32
	}
	for i := 10; i < 26; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(32))
		b[i] = crockford[n.Int64()]
	}
	return string(b[:])
}

func randomNational() string {
	s := "8"
	for i := 0; i < 9; i++ {
		s += strconv.Itoa(mrand.IntN(10))
	}
	return s
}

func fakeIP() string {
	return fmt.Sprintf("100.%d.%d.%d", 64+mrand.IntN(64), mrand.IntN(256), mrand.IntN(256))
}

// listenerPID is the pid listening on 127.0.0.1:<port> (the API), or 0.
func listenerPID(port string) int {
	out, err := exec.Command("lsof", "-nP", "-t", "-iTCP:"+port, "-sTCP:LISTEN").Output()
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]))
	return pid
}

// rssKB is the resident set of pid in KiB (ps), or 0.
func rssKB(pid int) int64 {
	if pid == 0 {
		return 0
	}
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	return v
}

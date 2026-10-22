package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// testHTTP has its own transport: the in-process api replaces
// http.DefaultTransport at start-up (egress guard), which would race with a
// test reading it.
var testHTTP = httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true})

func localEnv(db *testkit.DB) map[string]string {
	m := map[string]string{
		"APP_ENV":              "test",
		"DATABASE_URL":         db.URL,
		"DATABASE_MIGRATE_URL": db.OwnerURL,
	}
	for k, v := range appKeys() {
		m[k] = v
	}
	return m
}

// TestMigrateSeedAndServe: migrate up as todayz_owner, seed as todayz_app,
// status lists everything applied, then the api serves /readyz and the test
// clock, and the worker serves /readyz with River running, all on a fresh
// database. Logs never carry the database passwords.
func TestMigrateSeedAndServe(t *testing.T) {
	db := testkit.NewEmptyDB(t)
	e := localEnv(db)

	code, out, stderr := runCmd([]string{"migrate", "up"}, e)
	require.Equal(t, 0, code, stderr)
	require.Contains(t, out, `"migrated up"`)
	code, _, stderr = runCmd([]string{"seed", "dev"}, e)
	require.Equal(t, 0, code, stderr)
	code, out, stderr = runCmd([]string{"migrate", "status"}, e)
	require.Equal(t, 0, code, stderr)
	require.NotContains(t, out, "pending")
	require.Contains(t, out, "00003  applied  river")

	// The app role cannot migrate.
	bad := localEnv(db)
	bad["DATABASE_MIGRATE_URL"] = db.URL
	code, _, _ = runCmd([]string{"migrate", "down"}, bad)
	require.Equal(t, 1, code)

	apiAddr, workerAddr := freeAddr(t), freeAddr(t)
	e["API_ADDR"] = apiAddr
	e["WORKER_ADDR"] = workerAddr
	ctx, cancel := context.WithCancel(context.Background())
	var apiOut, workerOut syncBuffer
	apiDone := make(chan int, 1)
	workerDone := make(chan int, 1)
	go func() { apiDone <- run(ctx, []string{"api"}, env(e), &apiOut, &apiOut) }()
	go func() { workerDone <- run(ctx, []string{"worker"}, env(e), &workerOut, &workerOut) }()

	waitOK(t, "http://"+apiAddr+"/readyz")
	waitOK(t, "http://"+workerAddr+"/readyz")
	resp, err := testHTTP.Post("http://"+apiAddr+"/v1/test/clock", "application/json", strings.NewReader(`{"advance":"5m","wait":"0s"}`))
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)

	cancel()
	require.Equal(t, 0, <-apiDone, apiOut.String())
	require.Equal(t, 0, <-workerDone, workerOut.String())

	logs := apiOut.String() + workerOut.String()
	require.Contains(t, logs, `"test clock moved"`)
	require.Contains(t, logs, `"worker started"`)
	for _, u := range []string{db.URL, db.OwnerURL} {
		pw := u[strings.Index(u, ":")+3:]
		pw = pw[strings.Index(pw, ":")+1 : strings.Index(pw, "@")]
		require.NotContains(t, logs, pw, "a database password reached the logs")
	}
	for _, k := range []string{"PII_HASH_KEY", "TOTP_ENC_KEY", "JWT_SIGNING_KEYS"} {
		require.NotContains(t, logs, e[k], "%s reached the logs", k)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitOK(t *testing.T, url string) {
	t.Helper()
	require.Eventually(t, func() bool {
		resp, err := testHTTP.Get(url)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 100*time.Millisecond, url)
}

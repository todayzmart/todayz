// Command tz-mocks is the TodayZ local provider mocks server (W0-07).
//
// Environment:
//
//	MOCKS_ADDR             listen address (default 127.0.0.1:4010)
//	MOCKS_FIXTURES_DIR     fixtures directory; else $TODAYZ_ROOT/e2e/mocks/fixtures
//	                       when it exists; else the copy embedded at build time
//	MOCKS_CAPACITY         captured entries kept per kind (default 500)
//	MOCK_LLM               "0" with ANTHROPIC_API_KEY set turns on LLM live/record
//	                       mode (upstream https://api.anthropic.com); unset or
//	                       "1" never forwards
//	MOCK_LLM_REPLAY_RECORDED  "1" also serves recordings from fixtures/llm/_recorded
//	                       (off by default, so recordings never change results)
//	MOCK_TURNSTILE_HOSTNAME   hostname in a passing siteverify reply (default localhost)
//
// The embedded fixtures leave out files and folders starting with "_" or "."
// (go:embed without "all:"), so local recordings are never built in.
//
// See README.md for the emulated endpoints.
package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"todayz.local/e2e/mocks/internal/server"
)

//go:embed fixtures
var embedded embed.FS

const defaultAddr = "127.0.0.1:4010"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, "tz-mocks:", err)
		os.Exit(1)
	}
}

// fixturesFS picks the fixtures source: an explicit dir, the repo checkout, or
// the embedded copy. dir is "" for the embedded copy.
func fixturesFS(getenv func(string) string) (fsys fs.FS, dir string, err error) {
	if d := getenv("MOCKS_FIXTURES_DIR"); d != "" {
		st, err := os.Stat(d)
		if err != nil || !st.IsDir() {
			return nil, "", fmt.Errorf("MOCKS_FIXTURES_DIR is not a directory")
		}
		abs, _ := filepath.Abs(d)
		return os.DirFS(abs), abs, nil
	}
	if root := getenv("TODAYZ_ROOT"); root != "" {
		d := filepath.Join(root, "e2e", "mocks", "fixtures")
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return os.DirFS(d), d, nil
		}
	}
	sub, err := fs.Sub(embedded, "fixtures")
	return sub, "", err
}

// run serves until ctx ends. ready, when set, receives the bound address.
func run(ctx context.Context, getenv func(string) string, stdout io.Writer, ready chan<- string) error {
	log := slog.New(slog.NewJSONHandler(stdout, nil))
	cfg := server.ConfigFromEnv(getenv)
	fsys, dir, err := fixturesFS(getenv)
	if err != nil {
		return err
	}
	cfg.Fixtures, cfg.FixturesDir, cfg.Logger = fsys, dir, log

	addr := getenv("MOCKS_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           server.New(cfg),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// net/http's own messages (bad requests, panics) as JSON lines too.
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	mode, source := "fixtures", "embedded"
	if cfg.LLMLive {
		mode = "live"
	}
	if dir != "" {
		source = dir
	}
	// Never the key: only the mode and where fixtures come from.
	log.Info("tz-mocks listening", "addr", ln.Addr().String(), "llm_mode", mode, "fixtures", source)
	if ready != nil {
		ready <- ln.Addr().String()
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		return err
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("tz-mocks stopped")
	return nil
}

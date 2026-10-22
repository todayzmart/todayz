package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// sinkHandler refuses every proxied request and records the target host.
// Only host names are recorded, never paths, headers or bodies (they could hold keys).
type sinkHandler struct {
	mu  sync.Mutex
	log *os.File
}

func targetHost(r *http.Request) string {
	h := r.Host
	if r.Method != http.MethodConnect && r.URL != nil && r.URL.Host != "" {
		h = r.URL.Host
	}
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

func (s *sinkHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(targetHost(r))
	s.mu.Lock()
	fmt.Fprintln(s.log, host)
	_ = s.log.Sync()
	s.mu.Unlock()
	http.Error(w, "outbound traffic is blocked during make check / make e2e (SC-PL-04)", http.StatusForbidden)
}

func runEgressSink(args []string) error {
	fs := flag.NewFlagSet("egress-sink", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:0", "listen address")
	portFile := fs.String("port-file", "", "write the chosen port here once listening")
	logPath := fs.String("log", "", "append one blocked host per line")
	_ = fs.Parse(args)
	if *portFile == "" || *logPath == "" {
		return errors.New("egress-sink needs -port-file and -log")
	}
	lf, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s", *logPath)
	}
	defer lf.Close()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: &sinkHandler{log: lf}, ReadHeaderTimeout: 5 * time.Second}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(*portFile, []byte(fmt.Sprint(port)), 0o600); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() { <-ctx.Done(); _ = srv.Close() }()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

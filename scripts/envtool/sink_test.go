package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSinkRefusesAndLogsHostOnly(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "egress.log")
	lf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0o600)
	defer lf.Close()
	srv := httptest.NewServer(&sinkHandler{log: lf})
	defer srv.Close()
	proxy, _ := url.Parse(srv.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}

	resp, err := client.Get("http://api.example.com/v1/secret?token=abc123")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", resp.StatusCode)
	}
	if _, err := client.Get("https://secure.example.org/x"); err == nil {
		t.Fatal("CONNECT through the sink must fail")
	}
	b, _ := os.ReadFile(logPath)
	got := string(b)
	if got != "api.example.com\nsecure.example.org\n" {
		t.Fatalf("log %q", got)
	}
	if strings.Contains(got, "abc123") || strings.Contains(got, "/v1") {
		t.Fatal("sink logged more than the host")
	}
}

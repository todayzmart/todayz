// Package egress builds outbound HTTP clients that can be locked to loopback.
//
// When TODAYZ_NO_EGRESS=1 or APP_ENV=test (SC-PL-04, D-17) every outbound call
// must stay on this machine. The guard works at two levels:
//
//  1. Before DNS: a request whose host is not localhost, *.localhost or a
//     loopback literal is refused, so no lookup for example.com ever leaves.
//  2. After DNS, at dial time: the socket is refused unless the resolved IP is
//     loopback, so a local name that resolves outward is still caught.
//
// Proxies from the environment are ignored while blocked, so the request is
// refused here rather than handed to a proxy.
//
// httpx.NewOutboundClient is the public entry point; telemetry exporters and
// provider clients use it too.
package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrBlocked is returned for an outbound call to a non-loopback destination
// while egress is blocked.
var ErrBlocked = errors.New("egress: blocked: only loopback destinations are allowed (TODAYZ_NO_EGRESS or APP_ENV=test)")

// Options configure a client.
type Options struct {
	// Block refuses every non-loopback destination.
	Block bool
	// Timeout is the whole-request timeout (default 10 s).
	Timeout time.Duration
}

// NewClient returns an HTTP client honouring opts.
func NewClient(opts Options) *http.Client {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &http.Client{Timeout: timeout, Transport: NewTransport(opts.Block)}
}

// NewTransport returns a transport; with block set it only reaches loopback.
func NewTransport(block bool) http.RoundTripper {
	t := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	}
	if !block {
		return t
	}
	t.Proxy = nil
	t.DialContext = GuardedDialer().DialContext
	return &guard{next: t}
}

// GuardedDialer returns a dialer that refuses any non-loopback IP after DNS.
func GuardedDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			if strings.HasPrefix(network, "unix") {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("%w: %s", ErrBlocked, network)
			}
			if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
				return fmt.Errorf("%w: resolved address is not loopback", ErrBlocked)
			}
			return nil
		},
	}
}

type guard struct{ next http.RoundTripper }

func (g *guard) RoundTrip(r *http.Request) (*http.Response, error) {
	if !IsLocalHost(r.URL.Hostname()) {
		return nil, fmt.Errorf("%w: host %q", ErrBlocked, r.URL.Hostname())
	}
	return g.next.RoundTrip(r)
}

// IsLocalHost reports whether host is localhost, a *.localhost name or a loopback IP literal.
func IsLocalHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// InstallGuard replaces http.DefaultTransport with a blocked transport, as a
// safety net for libraries that use the default client. Call it once at start-up
// when egress is blocked. A transport that is already guarded is kept, so
// two boots in one process (the api and the worker in a test) write it once.
func InstallGuard() {
	installMu.Lock()
	defer installMu.Unlock()
	if _, ok := http.DefaultTransport.(*guard); !ok {
		http.DefaultTransport = NewTransport(true)
	}
}

var installMu sync.Mutex

// DialGuard is a DialContext that refuses non-loopback destinations; for
// clients that are not net/http (for example a raw TCP client).
func DialGuard(ctx context.Context, network, address string) (net.Conn, error) {
	return GuardedDialer().DialContext(ctx, network, address)
}

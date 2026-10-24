package egress

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBlockedClientRefusesNonLoopback(t *testing.T) {
	c := NewClient(Options{Block: true, Timeout: 2 * time.Second})
	for _, u := range []string{
		"https://example.com/",
		"http://example.com:80/x",
		"http://93.184.215.14/", // a public IP literal
		"http://10.0.0.1/",      // private is not loopback either
		"http://[2606:2800:220:1::]/",
		"http://localhost.example.com/",
	} {
		t.Run(u, func(t *testing.T) {
			resp, err := c.Get(u) //nolint:noctx // test
			if resp != nil {
				_ = resp.Body.Close()
			}
			require.ErrorIs(t, err, ErrBlocked)
		})
	}
}

func TestBlockedClientAllowsLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	c := NewClient(Options{Block: true, Timeout: 2 * time.Second})
	resp, err := c.Get(srv.URL) // http://127.0.0.1:port
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	require.Equal(t, "ok", string(b))
}

func TestBlockedClientIgnoresProxyEnv(t *testing.T) {
	// A proxy in the environment must not carry a blocked request out.
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	c := NewClient(Options{Block: true, Timeout: 2 * time.Second})
	resp, err := c.Get("https://example.com/") //nolint:noctx // test
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.ErrorIs(t, err, ErrBlocked)
}

func TestDialerChecksResolvedIP(t *testing.T) {
	// The dial-level check is what catches a local-looking name that resolves outward.
	_, err := DialGuard(context.Background(), "tcp", "93.184.215.14:80")
	require.ErrorIs(t, err, ErrBlocked)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	conn, err := DialGuard(context.Background(), "tcp", ln.Addr().String())
	require.NoError(t, err)
	_ = conn.Close()
}

func TestIsLocalHost(t *testing.T) {
	for h, want := range map[string]bool{
		"localhost": true, "LOCALHOST": true, "api.todayz.localhost": true, "api.todayz.localhost.": true,
		"127.0.0.1": true, "127.9.9.9": true, "::1": true, "[::1]": true,
		"example.com": false, "localhost.example.com": false, "0.0.0.0": false, "10.1.2.3": false, "": false,
	} {
		require.Equal(t, want, IsLocalHost(h), h)
	}
}

func TestUnblockedClientIsPlain(t *testing.T) {
	c := NewClient(Options{})
	_, isGuard := c.Transport.(*guard)
	require.False(t, isGuard)
	require.Equal(t, 10*time.Second, c.Timeout)
}

func TestInstallGuard(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	InstallGuard()
	resp, err := http.Get("https://example.com/") //nolint:noctx // test
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.ErrorIs(t, err, ErrBlocked)

	// A plain &http.Client{} uses http.DefaultTransport, so it is refused too
	// (literal non-loopback IPs: nothing resolves, nothing leaves the host).
	for _, u := range []string{"http://192.0.2.10/", "https://[2001:db8::1]/", "http://10.0.0.1:8080/x"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
		require.NoError(t, err)
		resp, err := (&http.Client{}).Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		require.ErrorIs(t, err, ErrBlocked, u)
	}
}

// NoProxy (the EC2 instance metadata client) never sends through HTTP(S)_PROXY:
// NO_PROXY rarely lists 169.254.169.254.
func TestNoProxyIgnoresProxyEnv(t *testing.T) {
	tr, ok := NewClient(Options{NoProxy: true}).Transport.(*http.Transport)
	require.True(t, ok)
	require.Nil(t, tr.Proxy)
	tr, ok = NewClient(Options{}).Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, tr.Proxy, "other clients keep the environment's proxy")
}

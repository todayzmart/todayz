package db

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPoolSize: MinMaxConns raises a small pool, a larger pool_max_conns in the
// URL wins over it, and MaxConns overrides both.
func TestPoolSize(t *testing.T) {
	const url = "postgres://u@127.0.0.1:1/x"
	for _, tc := range []struct {
		name string
		url  string
		opts Options
		want int32
	}{
		{"floor raises the default", url + "?pool_max_conns=4", Options{MinMaxConns: 16}, 16},
		{"larger url value wins", url + "?pool_max_conns=40", Options{MinMaxConns: 16}, 40},
		{"MaxConns overrides", url + "?pool_max_conns=40", Options{MinMaxConns: 16, MaxConns: 2}, 2},
		{"no floor keeps the url", url + "?pool_max_conns=3", Options{}, 3},
	} {
		cfg, err := poolConfig(tc.url, tc.opts)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.want, cfg.MaxConns, tc.name)
	}
	_, err := poolConfig("not a url ::", Options{})
	require.Error(t, err)
}

package support

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/config"
)

func TestPresigner(t *testing.T) {
	require.Nil(t, newPresigner(config.Storage{}), "no storage configured: uploads answer 503")
	require.Nil(t, newPresigner(config.Storage{Endpoint: "ftp://x", AccessKeyID: config.NewSecret([]byte("a")),
		SecretAccessKey: config.NewSecret([]byte("b")), BucketPrivate: "p"}))

	p := newPresigner(config.Storage{Endpoint: "http://127.0.0.1:9100", AccessKeyID: config.NewSecret([]byte("minio")),
		SecretAccessKey: config.NewSecret([]byte("minio-secret")), BucketPrivate: "todayz-private"})
	require.NotNil(t, p)
	now := time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC)

	put, err := url.Parse(p.putURL("support/admin/x", "image/png", 42, now))
	require.NoError(t, err)
	require.Equal(t, "http", put.Scheme)
	require.Equal(t, "/todayz-private/support/admin/x", put.Path)
	q := put.Query()
	require.Equal(t, "20261022T093000Z", q.Get("X-Amz-Date"))
	require.Equal(t, "900", q.Get("X-Amz-Expires"))
	require.Equal(t, "content-length;content-type;host", q.Get("X-Amz-SignedHeaders"))
	require.Len(t, q.Get("X-Amz-Signature"), 64)

	get, err := url.Parse(p.getURL("support/admin/x", now))
	require.NoError(t, err)
	require.Equal(t, "host", get.Query().Get("X-Amz-SignedHeaders"))
	require.NotEqual(t, q.Get("X-Amz-Signature"), get.Query().Get("X-Amz-Signature"))
	require.Equal(t, p.getURL("support/admin/x", now), p.getURL("support/admin/x", now), "deterministic for a fixed clock")
}

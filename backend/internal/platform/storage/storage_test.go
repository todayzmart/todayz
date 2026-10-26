package storage

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/storage/storagetest"
)

// The presigned GET example of the AWS SigV4 documentation ("Authenticating
// Requests: Using Query Parameters"), which uses UNSIGNED-PAYLOAD as we do.
func TestPresignAWSVector(t *testing.T) {
	u := presignURL(presignInput{
		Method: "GET", Scheme: "https", Host: "examplebucket.s3.amazonaws.com", Path: "/test.txt",
		AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Time: time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC), Expires: 86400 * time.Second,
	})
	require.True(t, strings.HasSuffix(u, "&X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"), u)
}

func TestNew(t *testing.T) {
	require.Nil(t, New(config.Storage{}, nil), "nothing configured")
	require.Nil(t, New(config.Storage{Endpoint: "ftp://x", AccessKeyID: config.NewSecret([]byte("a")),
		SecretAccessKey: config.NewSecret([]byte("b")), BucketPrivate: "p"}, nil))
	var none *Client
	require.False(t, none.HasImages())

	c := New(config.Storage{Endpoint: "http://127.0.0.1:9100", AccessKeyID: config.NewSecret([]byte("minio")),
		SecretAccessKey: config.NewSecret([]byte("minio-secret")), BucketPrivate: "todayz-private"}, nil)
	require.NotNil(t, c)
	require.False(t, c.HasImages())
	now := time.Date(2026, 10, 26, 9, 30, 0, 0, time.UTC)
	put, err := url.Parse(c.PutURL("product-images/p/i.jpg", "image/jpeg", 42, now))
	require.NoError(t, err)
	require.Equal(t, "/todayz-private/product-images/p/i.jpg", put.Path)
	require.Equal(t, "900", put.Query().Get("X-Amz-Expires"))
	require.Equal(t, "content-length;content-type;host", put.Query().Get("X-Amz-SignedHeaders"))
	get, err := url.Parse(c.GetURL("product-images/p/i.jpg", now))
	require.NoError(t, err)
	require.Equal(t, "host", get.Query().Get("X-Amz-SignedHeaders"))
}

func TestCalls(t *testing.T) {
	f := storagetest.New(t)
	c := New(f.Config(), f.Client())
	require.True(t, c.HasImages())
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 9, 30, 0, 0, time.UTC)

	_, err := c.Head(ctx, "a/b.jpg", now)
	require.ErrorIs(t, err, ErrNoObject)
	require.NoError(t, c.Put(ctx, "a/b.jpg", "image/jpeg", []byte("jpeg!"), now))
	o, err := c.Head(ctx, "a/b.jpg", now)
	require.NoError(t, err)
	require.Equal(t, Object{Size: 5, ContentType: "image/jpeg"}, o)

	require.ErrorIs(t, c.Publish(ctx, "a/b.jpg", "products/x.jpg", "image/jpeg", 4, now), ErrTooLarge)
	require.ErrorIs(t, c.Publish(ctx, "a/none.jpg", "products/x.jpg", "image/jpeg", 10, now), ErrNoObject)
	require.NoError(t, c.Publish(ctx, "a/b.jpg", "products/x.jpg", "image/jpeg", 10, now))
	body, ct, ok := f.Image("products/x.jpg")
	require.True(t, ok)
	require.Equal(t, "jpeg!", string(body))
	require.Equal(t, "image/jpeg", ct)

	require.NoError(t, c.RemoveImage(ctx, "products/x.jpg", now))
	_, _, ok = f.Image("products/x.jpg")
	require.False(t, ok)
	require.NoError(t, c.Remove(ctx, "a/b.jpg", now))
	require.NoError(t, c.Remove(ctx, "a/b.jpg", now), "already gone is fine")
	_, ok = f.Private("a/b.jpg")
	require.False(t, ok)
}

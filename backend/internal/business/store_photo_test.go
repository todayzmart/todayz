package business

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/testkit"
)

// photoBuckets is the private and images buckets: GETs from private, PUTs
// and DELETEs on images, failing the private keys in fail.
type photoBuckets struct {
	mu      sync.Mutex
	private map[string]string
	images  map[string]string // key → content type + ":" + body
	fail    map[string]bool
}

func (b *photoBuckets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/private/"):
		key := strings.TrimPrefix(r.URL.Path, "/private/")
		body, ok := b.private[key]
		if b.fail[key] {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, body)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/images/"):
		body, _ := io.ReadAll(r.Body)
		b.images[strings.TrimPrefix(r.URL.Path, "/images/")] = r.Header.Get("Content-Type") + ":" + string(body)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/images/"):
		delete(b.images, strings.TrimPrefix(r.URL.Path, "/images/"))
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

// D-246: the newest accepted storefront photo is copied to the images
// bucket and set on the store; a newer one replaces it (the old file goes),
// a later rejection clears it, and a failed copy is left for the next run.
func TestPublishStorePhotosDB(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	s, clk := leadService(t, tdb, nil)
	b := &photoBuckets{private: map[string]string{}, images: map[string]string{}, fail: map[string]bool{}}
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	s.presign = &presigner{scheme: "http", host: u.Host, bucket: "private", images: "images", accessKey: "k", secretKey: "s", client: srv.Client()}

	now := clk.Now()
	bid, sid, other := ids.New(now), ids.New(now), ids.New(now)
	_, err = tdb.OwnerPool.Exec(ctx, `INSERT INTO businesses (id, brand_name, status, referral_code) VALUES ($1, 'Photo Shop', 'draft', 'AAAAAP')`, ids.UUID(bid))
	require.NoError(t, err)
	for i, id := range []ids.ID{sid, other} {
		_, err = tdb.OwnerPool.Exec(ctx, `INSERT INTO stores (id, business_id, slug, short_code, name) VALUES ($1, $2, $3, $4, 'Photo Shop')`,
			ids.UUID(id), ids.UUID(bid), "photo-shop-"+string(rune('a'+i)), "PPPPP"+string(rune('A'+i)))
		require.NoError(t, err)
	}
	doc := func(store ids.ID, kind, status, contentType string, reviewed time.Time) (ids.ID, string) {
		id := ids.New(now)
		key := "businesses/" + bid.String() + "/documents/" + id.String()
		_, err := tdb.OwnerPool.Exec(ctx, `INSERT INTO business_documents (id, business_id, store_id, kind, object_key, content_type, size_bytes, status, reviewed_at)
		  VALUES ($1, $2, $3, $4, $5, $6, 4, $7, $8)`, ids.UUID(id), ids.UUID(bid), ids.UUID(store), kind, key, contentType, status, reviewed)
		require.NoError(t, err)
		b.private[key] = "img" + id.String()[22:]
		return id, key
	}
	photo := func(store ids.ID) (key *string) {
		require.NoError(t, tdb.OwnerPool.QueryRow(ctx, `SELECT photo_key FROM stores WHERE id = $1`, ids.UUID(store)).Scan(&key))
		return
	}

	first, _ := doc(sid, "storefront", "accepted", "image/jpeg", now.Add(-2*time.Hour))
	doc(sid, "storefront", "uploaded", "image/png", now) // not accepted yet
	doc(sid, "shop_photo", "accepted", "image/png", now) // not the storefront
	_, failKey := doc(other, "storefront", "accepted", "image/webp", now)
	b.fail[failKey] = true

	res, err := s.PublishStorePhotos(ctx)
	require.NoError(t, err)
	assert.Equal(t, PhotoResult{Published: 1, Failed: 1}, res)
	firstKey := "stores/" + sid.String() + "/" + first.String() + ".jpg"
	assert.Equal(t, &firstKey, photo(sid))
	assert.Equal(t, "image/jpeg:"+b.private["businesses/"+bid.String()+"/documents/"+first.String()], b.images[firstKey])
	assert.Nil(t, photo(other), "a failed copy leaves the store as it was")

	// Nothing changed: nothing to do but the store whose copy failed.
	b.fail[failKey] = false
	res, err = s.PublishStorePhotos(ctx)
	require.NoError(t, err)
	assert.Equal(t, PhotoResult{Published: 1}, res)
	res, err = s.PublishStorePhotos(ctx)
	require.NoError(t, err)
	assert.Equal(t, PhotoResult{}, res)

	// A newer accepted photo replaces it and the old file is deleted.
	second, _ := doc(sid, "storefront", "accepted", "image/png", now.Add(-time.Hour))
	res, err = s.PublishStorePhotos(ctx)
	require.NoError(t, err)
	assert.Equal(t, PhotoResult{Published: 1}, res)
	secondKey := "stores/" + sid.String() + "/" + second.String() + ".png"
	assert.Equal(t, &secondKey, photo(sid))
	assert.NotContains(t, b.images, firstKey)

	// Rejecting both later clears the photo.
	_, err = tdb.OwnerPool.Exec(ctx, `UPDATE business_documents SET status = 'rejected' WHERE store_id = $1 AND kind = 'storefront'`, ids.UUID(sid))
	require.NoError(t, err)
	res, err = s.PublishStorePhotos(ctx)
	require.NoError(t, err)
	assert.Equal(t, PhotoResult{Cleared: 1}, res)
	assert.Nil(t, photo(sid))
	assert.NotContains(t, b.images, secondKey)
}

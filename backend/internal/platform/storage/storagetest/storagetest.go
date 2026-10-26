// Package storagetest is an in-memory S3 for tests: two buckets on an
// httptest server that answer the presigned PUT, GET, HEAD and DELETE calls of
// internal/platform/storage (and of a browser's upload). It does not check
// signatures; the storage package's own tests do.
package storagetest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/todayz/backend/internal/platform/config"
)

// The bucket names Config returns.
const (
	PrivateBucket = "todayz-private"
	ImagesBucket  = "todayz-images"
)

type object struct {
	body        []byte
	contentType string
}

// Fake is the in-memory store.
type Fake struct {
	srv     *httptest.Server
	mu      sync.Mutex
	objects map[string]object // by "/bucket/key"
}

// New starts a fake closed when the test ends.
func New(t testing.TB) *Fake {
	t.Helper()
	f := &Fake{objects: map[string]object{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects[r.URL.Path]
	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.objects[r.URL.Path] = object{body: body, contentType: r.Header.Get("Content-Type")}
	case http.MethodHead, http.MethodGet:
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", o.contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(o.body)))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(o.body)
		}
	case http.MethodDelete:
		delete(f.objects, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// URL is the fake's endpoint.
func (f *Fake) URL() string { return f.srv.URL }

// Client is an HTTP client for the fake (loopback, so egress allows it).
func (f *Fake) Client() *http.Client { return f.srv.Client() }

// Config is a storage config pointing at the fake with both buckets.
func (f *Fake) Config() config.Storage {
	return config.Storage{
		Endpoint: f.srv.URL, AccessKeyID: config.NewSecret([]byte("test")),
		SecretAccessKey: config.NewSecret([]byte("test-secret")),
		BucketPrivate:   PrivateBucket, BucketImages: ImagesBucket,
	}
}

// PutPrivate stores an object in the private bucket, as a client upload would.
func (f *Fake) PutPrivate(key, contentType string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects["/"+PrivateBucket+"/"+key] = object{body: body, contentType: contentType}
}

// PutURL stores an object at a presigned URL's path, as the browser's PUT would.
func (f *Fake) PutURL(presigned, contentType string, body []byte) {
	path := presigned
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimPrefix(path, f.srv.URL)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[path] = object{body: body, contentType: contentType}
}

// Private returns a private object's bytes.
func (f *Fake) Private(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects["/"+PrivateBucket+"/"+key]
	return o.body, ok
}

// Image returns a public image's bytes and type.
func (f *Fake) Image(key string) ([]byte, string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects["/"+ImagesBucket+"/"+key]
	return o.body, o.contentType, ok
}

// Images lists the public images bucket's keys.
func (f *Fake) Images() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for k := range f.objects {
		if rest, ok := strings.CutPrefix(k, "/"+ImagesBucket+"/"); ok {
			keys = append(keys, rest)
		}
	}
	return keys
}

package support

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeS3 is the private bucket for the service tests: it keeps what was PUT
// (type and size, by path), answers HEAD and DELETE, and answers 500 to HEAD
// and DELETE for the paths in fail. It does not check signatures; presign_test does.
type fakeS3 struct {
	srv     *httptest.Server
	mu      sync.Mutex
	objects map[string]object
	fail    map[string]bool
	deletes int
}

func newFakeS3(t *testing.T) *fakeS3 {
	t.Helper()
	f := &fakeS3{objects: map[string]object{}, fail: map[string]bool{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		o, ok := f.objects[r.URL.Path]
		if f.fail[r.URL.Path] && (r.Method == http.MethodHead || r.Method == http.MethodDelete) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		switch r.Method {
		case http.MethodPut:
			f.objects[r.URL.Path] = object{size: r.ContentLength, contentType: r.Header.Get("Content-Type")}
		case http.MethodHead, http.MethodGet:
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", o.contentType)
			w.Header().Set("Content-Length", strconv.FormatInt(o.size, 10))
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			f.deletes++
			delete(f.objects, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func fakePath(t *testing.T, presigned string) string {
	t.Helper()
	u, err := url.Parse(presigned)
	require.NoError(t, err)
	return u.Path
}

// put stores an object at the presigned URL's path as if the client had
// uploaded it.
func (f *fakeS3) put(t *testing.T, presigned, contentType string, size int64) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[fakePath(t, presigned)] = object{size: size, contentType: contentType}
}

func (f *fakeS3) has(t *testing.T, presigned string) bool {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[fakePath(t, presigned)]
	return ok
}

// failing makes HEAD and DELETE of the presigned URL's path answer 500, or
// stop doing so.
func (f *fakeS3) failing(t *testing.T, presigned string, on bool) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[fakePath(t, presigned)] = on
}

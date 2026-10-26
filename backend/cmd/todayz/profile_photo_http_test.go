package main

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/platform/storage/storagetest"
	"github.com/todayz/backend/internal/platform/testclock"
)

// The profile photo through the router (D-253): register, PUT, confirm,
// remove. Customer app only (a business token is 403), another customer's
// upload is 404, the upload URL is no-store, and photoUrl reaches only the
// customer app's /v1/me. Bodies are checked against the contract.
func TestProfilePhotoHTTP(t *testing.T) {
	o := newOrderShop(t)
	fake := storagetest.New(t)
	cfg := *o.cfg
	cfg.Storage = fake.Config()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	q, err := queue.NewInsertClient(o.a.tdb.Pool, queue.Options{Log: log})
	require.NoError(t, err)
	h, err := buildRouter(deps{
		Config: &cfg, Log: log, Clock: o.a.clk,
		DB: db.New(o.a.tdb.Pool), Ready: &health.Readiness{}, Settings: o.settings, Queue: q,
		TestClock: testclock.New(clock.NewOffset(nil), nopStore{}, log),
	}, idempotency.NewMemoryStore(), contractValidator(t))
	require.NoError(t, err)
	o.a.h = h

	var img bytes.Buffer
	require.NoError(t, jpeg.Encode(&img, image.NewGray(image.Rect(0, 0, 32, 32)), nil))
	size := img.Len()
	reg := map[string]any{"contentType": "image/jpeg", "sizeBytes": size}

	r := o.a.call(o.cashier, http.MethodPost, "/v1/me/photo/uploads", reg, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a business token: status %d", r.code)
	r = o.a.call(o.cust, http.MethodPost, "/v1/me/photo/uploads", map[string]any{"contentType": "image/gif", "sizeBytes": 10}, nil)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "a GIF: status %d", r.code)
	r = o.a.call(o.cust, http.MethodPost, "/v1/me/photo/uploads", map[string]any{"contentType": "image/jpeg", "sizeBytes": 5<<20 + 1}, nil)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "over 5 MB: status %d", r.code)

	r = o.a.call(o.cust, http.MethodPost, "/v1/me/photo/uploads", reg, nil)
	require.Equal(t, http.StatusCreated, r.code, "status %d", r.code)
	require.Equal(t, "no-store", r.hdr.Get("Cache-Control"))
	id := r.body["id"].(string)
	target := r.body["upload"].(map[string]any)
	hdrs := target["headers"].(map[string]any)
	require.Equal(t, "image/jpeg", hdrs["Content-Type"])
	require.Equal(t, strconv.Itoa(size), hdrs["Content-Length"])
	fake.PutURL(target["url"].(string), "image/jpeg", img.Bytes())

	confirm := "/v1/me/photo/uploads/" + id + "/confirm"
	r = o.a.call(o.cust2, http.MethodPost, confirm, nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, "another customer's upload: status %d", r.code)
	r = o.a.call(o.cust, http.MethodPost, confirm, nil, nil)
	require.Equal(t, http.StatusOK, r.code, "status %d", r.code)
	require.NotEmpty(t, r.hdr.Get("ETag"))
	require.Contains(t, r.body["photoUrl"], "/"+storagetest.PrivateBucket+"/profile/")

	me := o.a.call(o.cust, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(t, http.StatusOK, me.code)
	require.Contains(t, me.body["photoUrl"], "/profile/")
	biz := o.a.call(o.cashier, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(t, http.StatusOK, biz.code)
	require.NotContains(t, biz.body, "photoUrl", "the business app never sees it")

	r = o.a.call(o.cashier, http.MethodDelete, "/v1/me/photo", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a business token: status %d", r.code)
	r = o.a.call(o.cust, http.MethodDelete, "/v1/me/photo", nil, nil)
	require.Equal(t, http.StatusOK, r.code, "status %d", r.code)
	require.Nil(t, r.body["photoUrl"])
	require.Empty(t, fake.Images(), "nothing is ever public")
}

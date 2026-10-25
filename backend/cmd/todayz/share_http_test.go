package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/testclock"
	"github.com/todayz/backend/internal/share"
	"github.com/todayz/backend/internal/testkit"
)

// W3-16: share-card reads through the real pipeline. The customer Pages
// Function reads a preview from Cloudflare, so it forwards the visitor's IP
// with SHARE_PREVIEW_KEY; the API believes that IP only with the key. The
// test requests come from loopback, so CF-Connecting-IP stands for the IP
// the API sees (a Cloudflare egress IP for the Function).

const (
	shareReads = 60 // share.readRule's burst
	cfEgress   = "198.51.100.1"
)

type shareAPI struct {
	*bizAPI
	key  string
	code string
	item string
}

// newShareAPI is the api with SHARE_PREVIEW_KEY set (unless withKey is
// false), a live store of a verified business with one item, and its card.
func newShareAPI(t *testing.T, withKey bool) *shareAPI {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	e := localEnv(tdb)
	key := base64.StdEncoding.EncodeToString([]byte("share-preview-key-for-tests-0001"))
	if withKey {
		e["SHARE_PREVIEW_KEY"] = key
	}
	cfg, err := config.Load(env(e), nil)
	require.NoError(t, err)
	require.Equal(t, withKey, cfg.Keys.SharePreviewKey.IsSet())
	clk := clock.NewFake(time.Date(2026, 10, 23, 6, 30, 0, 0, time.UTC)) // Friday 12:00 IST
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := deps{
		Config: cfg, Log: log, Clock: clk,
		DB: db.New(tdb.Pool), Ready: &health.Readiness{},
		Settings:  settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{Log: log}),
		TestClock: testclock.New(clock.NewOffset(nil), nopStore{}, log),
	}
	h, err := buildRouter(d, idempotency.NewMemoryStore(), contractValidator(t))
	require.NoError(t, err)
	a := &shareAPI{bizAPI: &bizAPI{t: t, h: h, clk: clk, tdb: tdb}, key: key}

	now := clk.Now()
	biz, store, product, item := ids.New(now), ids.New(now), ids.New(now), ids.New(now)
	a.exec(`INSERT INTO businesses (id, brand_name, referral_code, status, category_id)
	  VALUES ($1, 'Biz', 'SHRHTP', 'verified', '0190c47e-0000-7000-8000-000000000100')`, ids.UUID(biz))
	a.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, locality, city, location, hours, status, live_at,
	    accepts_pickup, accepts_delivery, delivery_fee_paise)
	  VALUES ($1, $2, 'share-shop', 'SHRSTA', 'Share Shop', 'Indiranagar', 'Bengaluru',
	    ST_SetSRID(ST_MakePoint(77.6412, 12.9719), 4326)::geography, '{"weekly":{},"holidays":[]}'::jsonb, 'live', $3,
	    true, false, 0)`, ids.UUID(store), ids.UUID(biz), now.Add(-24*time.Hour))
	a.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, barcodes, mrp_paise)
	  VALUES ($1, 'Toor dal', '0190c47e-0000-7000-8000-000000000101', 1, 'kg', '{}', 9900)`, ids.UUID(product))
	a.exec(`INSERT INTO store_items (id, store_id, product_id, price_paise, on_hand, is_listed, stock_updated_at)
	  VALUES ($1, $2, $3, 3850, 7, true, $4)`, ids.UUID(item), ids.UUID(store), ids.UUID(product), now.Add(-time.Hour))
	a.item = item.String()

	r := a.create("203.0.113.250")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	a.code = r.body["code"].(string)
	return a
}

func (a *shareAPI) create(ip string) resp {
	a.t.Helper()
	return a.call("", http.MethodPost, "/v1/public/share-cards", map[string]any{"storeItemId": a.item},
		map[string]string{"CF-Connecting-IP": ip})
}

// preview reads the card as the Function does: from upstream, with the
// forwarded IP and key when they are not empty.
func (a *shareAPI) preview(upstream, forwarded, key string) resp {
	a.t.Helper()
	h := map[string]string{"CF-Connecting-IP": upstream}
	if forwarded != "" {
		h[share.HeaderPreviewIP] = forwarded
	}
	if key != "" {
		h[share.HeaderPreviewKey] = key
	}
	return a.call("", http.MethodGet, "/v1/public/share-cards/"+a.code, nil, h)
}

func (a *shareAPI) opened() int {
	return a.count(`SELECT count(*) FROM events WHERE name = 'share_opened'`)
}

func requireLimited(t *testing.T, r resp) {
	t.Helper()
	require.Equal(t, http.StatusTooManyRequests, r.code, r.raw)
	require.Equal(t, "rate_limited", problemCode(r))
	n, err := strconv.Atoi(r.hdr.Get("Retry-After"))
	require.NoError(t, err, "Retry-After is whole seconds")
	require.GreaterOrEqual(t, n, 1)
}

// With the key, previews from one Cloudflare IP for different visitors are
// counted per visitor: none is throttled, each visitor's first read records
// share_opened, and one visitor past the limit gets 429.
func TestSharePreviewForwardedIP(t *testing.T) {
	a := newShareAPI(t, true)
	for i := range shareReads + 10 {
		r := a.preview(cfEgress, fmt.Sprintf("203.0.113.%d", i+1), a.key)
		require.Equal(t, http.StatusOK, r.code, "visitor %d: %s", i+1, r.raw)
	}
	require.Equal(t, shareReads+10, a.opened(), "one open per visitor, not per Cloudflare IP")

	// An IPv4-mapped IPv6 address is the same visitor as its IPv4 address.
	r := a.preview(cfEgress, "::ffff:203.0.113.1", a.key)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, shareReads+10, a.opened())

	// The Cloudflare IP's own bucket was never used.
	r = a.preview(cfEgress, "", "")
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// One visitor: 60 reads, then 429.
	for i := 1; i < shareReads; i++ {
		r := a.preview(cfEgress, "203.0.113.200", a.key)
		require.Equal(t, http.StatusOK, r.code, "read %d: %s", i, r.raw)
	}
	r = a.preview(cfEgress, "203.0.113.200", a.key)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	requireLimited(t, a.preview(cfEgress, "203.0.113.200", a.key))
	// Another visitor through the same Cloudflare IP is not affected.
	r = a.preview(cfEgress, "203.0.113.201", a.key)
	require.Equal(t, http.StatusOK, r.code, r.raw)
}

// A wrong or missing key, or a forwarded value that is not an IP, is ignored:
// the reads count against the IP the API sees, as without the Function.
func TestSharePreviewForwardedIPNeedsTheKey(t *testing.T) {
	a := newShareAPI(t, true)
	wrong := base64.StdEncoding.EncodeToString([]byte("share-preview-key-for-tests-0002"))
	sends := []struct{ forwarded, key string }{
		{"203.0.113.1", wrong},
		{"203.0.113.2", ""},
		{"203.0.113.3", a.key[:len(a.key)-1]},
		{"not-an-ip", a.key},
		{"", a.key},
	}
	for i := range shareReads {
		s := sends[i%len(sends)]
		r := a.preview(cfEgress, s.forwarded, s.key)
		require.Equal(t, http.StatusOK, r.code, "read %d: %s", i+1, r.raw)
	}
	require.Equal(t, 1, a.opened(), "all of them are the Cloudflare IP")
	requireLimited(t, a.preview(cfEgress, "203.0.113.4", wrong))
	requireLimited(t, a.preview(cfEgress, "203.0.113.5", ""))
	// The right key still works for a fresh visitor.
	r := a.preview(cfEgress, "203.0.113.6", a.key)
	require.Equal(t, http.StatusOK, r.code, r.raw)
}

// Without SHARE_PREVIEW_KEY configured, nothing is believed, whatever is sent.
func TestSharePreviewWithoutConfiguredKey(t *testing.T) {
	a := newShareAPI(t, false)
	for i := range shareReads {
		r := a.preview(cfEgress, fmt.Sprintf("203.0.113.%d", i+1), a.key)
		require.Equal(t, http.StatusOK, r.code, "read %d: %s", i+1, r.raw)
	}
	require.Equal(t, 1, a.opened())
	requireLimited(t, a.preview(cfEgress, "203.0.113.250", a.key))
}

// Creates take from the same read bucket: one IP past the limit gets 429 with
// Retry-After on POST too; the forwarded IP is not believed on POST.
func TestShareCreateLimitedHTTP(t *testing.T) {
	a := newShareAPI(t, true)
	for i := range shareReads {
		r := a.call("", http.MethodPost, "/v1/public/share-cards", map[string]any{"storeItemId": a.item}, map[string]string{
			"CF-Connecting-IP": "203.0.113.77", share.HeaderPreviewIP: fmt.Sprintf("198.51.100.%d", i+10), share.HeaderPreviewKey: a.key,
		})
		require.Equal(t, http.StatusOK, r.code, "create %d: %s", i+1, r.raw)
		require.Equal(t, a.code, r.body["code"])
	}
	requireLimited(t, a.create("203.0.113.77"))
	r := a.create("203.0.113.78")
	require.Equal(t, http.StatusOK, r.code, r.raw)
}

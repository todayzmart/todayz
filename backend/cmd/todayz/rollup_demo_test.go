package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The demo's flags and its local-only guard are checked before any call.
func TestRollupDemoRefusesNonLocal(t *testing.T) {
	local := func(k string) string {
		if k == "APP_ENV" {
			return "local"
		}
		return ""
	}
	for _, args := range [][]string{
		{"demo", "--api", "https://api.todayz.in"},
		{"demo", "--api", "http://10.0.0.5:8180"},
		{"demo", "--mocks", "http://mocks.example.com"},
		{"demo", "--api", "http://user@127.0.0.1:8180"},
		{"demo", "--api", "http://127.0.0.1:8180/v1"},
		{"demo", "--every", "10ms"},
		{"demo", "--cancel-pct", "101"},
		{"demo", "extra"},
	} {
		err := runRollup(context.Background(), args, local, io.Discard)
		var u usageError
		require.True(t, errors.As(err, &u), "%v: err = %v, want a usage error", args, err)
	}
	err := runRollup(context.Background(), []string{"demo"}, func(k string) string {
		if k == "APP_ENV" {
			return "prod"
		}
		return ""
	}, io.Discard)
	require.ErrorContains(t, err, "APP_ENV local or test")
}

// handlerTransport serves the demo's requests from the in-process router,
// as if from loopback.
type handlerTransport struct{ h http.Handler }

func (ht handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	ht.h.ServeHTTP(w, req)
	return w.Result(), nil
}

// TestRollupDemoTraffic runs demo ticks against the real router and
// pipeline on a fresh database (W2-02's order shop): every step answers 2xx,
// orders end collected or cancelled, searches, events and RUM land, a
// session renews after its access token expires, and neither the output
// nor the API's logs carry a pickup code or a phone.
func TestRollupDemoTraffic(t *testing.T) {
	o := newOrderShop(t)
	bid := strings.TrimPrefix(o.B, "/v1/biz/businesses/")
	cfg := demoConfig{
		API: "https://api.todayz.localhost", Mocks: "http://127.0.0.1:4010",
		CustomerPhone: custPhone, OwnerPhone: ownerPhone,
		BusinessID: bid, StoreID: o.sid, Items: []string{o.atta, o.butter, o.salt},
		Lat: 12.9719, Lng: 77.6412, Every: time.Second, CancelPct: 0,
	}
	require.NoError(t, cfg.check())
	var out strings.Builder
	otp := func(context.Context, string, time.Time) (string, error) { return testOTP, nil }
	d := newDemoRun(cfg, &http.Client{Transport: handlerTransport{o.a.h}}, o.a.clk, otp, &out)
	ctx := context.Background()

	o.a.clk.Advance(31 * time.Second) // past the OTP resend cooldown of the shop's own sign-ins
	var err error
	d.cust, err = d.signIn(ctx, cfg.CustomerPhone, "customer")
	require.NoError(t, err)
	d.owner, err = d.signIn(ctx, cfg.OwnerPhone, "business")
	require.NoError(t, err)
	require.NotNil(t, d.cust.cookie, "the customer keeps a refresh cookie")

	for range 3 {
		require.NoError(t, d.tick(ctx))
	}
	// Past the 15-minute access token: the next calls renew once.
	o.a.clk.Advance(16 * time.Minute)
	d.cfg.CancelPct = 100
	require.NoError(t, d.tick(ctx))

	for k := range d.counts {
		require.NotContains(t, k, ":", "a refused step: %s (all: %s)", k, d.summary())
	}
	require.Equal(t, 3, d.counts["collect"], d.summary())
	require.Equal(t, 1, d.counts["cancel"], d.summary())
	require.Equal(t, 4, d.counts["rum"], d.summary())
	require.Equal(t, 4, d.counts["events"], d.summary())
	require.GreaterOrEqual(t, d.counts["search"], 4, d.summary())

	var collected, cancelled, searches, events int
	require.NoError(t, o.a.tdb.OwnerPool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'collected'), count(*) FILTER (WHERE status = 'cancelled'),
		       (SELECT count(*) FROM search_log), (SELECT count(*) FROM events WHERE name = 'app_opened')
		FROM orders`).Scan(&collected, &cancelled, &searches, &events))
	require.Equal(t, 3, collected)
	require.Equal(t, 1, cancelled)
	require.Equal(t, d.counts["search"], searches)
	require.Equal(t, 4, events)

	_, _ = io.WriteString(&out, d.summary())
	for _, secret := range []string{custPhone, ownerPhone, testOTP, d.cust.token, d.owner.token} {
		require.NotContains(t, out.String(), secret, "the demo's output carries a secret")
	}
	require.NotContains(t, o.logs.String(), custPhone, "the API's logs carry the customer's phone")
}

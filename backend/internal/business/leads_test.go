package business

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	publicapi "github.com/todayz/backend/gen/api/public"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/testkit"
)

// fakeHuman is a Turnstile verifier with a fixed answer.
type fakeHuman struct {
	ok    bool
	err   error
	calls int
}

func (f *fakeHuman) Verify(context.Context, string, string) (bool, error) {
	f.calls++
	return f.ok, f.err
}

func leadService(t *testing.T, tdb *testkit.DB, h HumanVerifier) (*Service, *clock.Fake) {
	t.Helper()
	enc, hash := make([]byte, 32), make([]byte, 32)
	_, err := rand.Read(enc)
	require.NoError(t, err)
	_, err = rand.Read(hash)
	require.NoError(t, err)
	clk := clock.NewFake(time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC))
	cfg := &config.Config{Env: config.EnvTest, Keys: config.AppKeys{PIIEncKey: config.NewSecret(enc), PIIHashKey: config.NewSecret(hash)}}
	d := Deps{Config: cfg, Clock: clk, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Human: h}
	if tdb != nil {
		d.DB = db.New(tdb.Pool)
	}
	s, err := New(d)
	require.NoError(t, err)
	return s, clk
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var p *problem.Error
	require.True(t, errors.As(err, &p), "not a problem: %v", err)
	return p.Code
}

func lead() *publicapi.LeadCreate {
	return &publicapi.LeadCreate{
		Name: "Lakshmi Devi", Phone: "+919000000021", ShopName: "Lakshmi Traders",
		City: "Bengaluru", Source: "site:home:en", TurnstileToken: "token",
	}
}

// TestCreateLeadTurnstile: a failed check is 403 and stores nothing; a
// provider error is 503; no verifier at all is 503 (fail closed); a bad field
// behind a failed check is still 403 (the bot learns nothing about fields).
func TestCreateLeadTurnstile(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	leads := func() int {
		var n int
		require.NoError(t, tdb.OwnerPool.QueryRow(ctx, `SELECT count(*) FROM leads`).Scan(&n))
		return n
	}

	s, _ := leadService(t, tdb, &fakeHuman{ok: false})
	require.Equal(t, genproblems.Forbidden, codeOf(t, s.createLead(ctx, lead())))
	bad := lead()
	bad.Phone = "+911234567890"
	require.Equal(t, genproblems.Forbidden, codeOf(t, s.createLead(ctx, bad)))

	s, _ = leadService(t, tdb, &fakeHuman{err: errors.New("provider down")})
	require.Equal(t, genproblems.ServiceUnavailable, codeOf(t, s.createLead(ctx, lead())))

	s, _ = leadService(t, tdb, nil)
	require.Equal(t, genproblems.ServiceUnavailable, codeOf(t, s.createLead(ctx, lead())))
	require.Equal(t, 0, leads(), "nothing is stored without a passed check")

	h := &fakeHuman{ok: true}
	s, clk := leadService(t, tdb, h)
	require.NoError(t, s.createLead(ctx, lead()))
	again := lead()
	again.Name, again.ShopName, again.City = "LAKSHMI  devi", "lakshmi traders", "BENGALURU"
	require.NoError(t, s.createLead(ctx, again))
	require.Equal(t, 1, leads(), "the same details in another case are one lead")
	other := lead()
	other.Phone = "+919000000022"
	require.NoError(t, s.createLead(ctx, other))
	require.Equal(t, 2, leads(), "another phone is another lead")
	clk.Advance(leadDedupeWindow)
	require.NoError(t, s.createLead(ctx, lead()))
	require.Equal(t, 3, leads(), "after the window the same details are stored again")
	require.Equal(t, 4, h.calls, "every submission is checked")

	bad = lead()
	bad.Phone = "+911234567890"
	require.Equal(t, genproblems.ValidationFailed, codeOf(t, s.createLead(ctx, bad)))
}

// TestCreateLeadNotConfigured: without the database or keys the form answers
// 503 before it asks Turnstile.
func TestCreateLeadNotConfigured(t *testing.T) {
	h := &fakeHuman{ok: true}
	s, _ := leadService(t, nil, h)
	require.Equal(t, genproblems.ServiceUnavailable, codeOf(t, s.createLead(context.Background(), lead())))
	require.Zero(t, h.calls)
}

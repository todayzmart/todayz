package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/ids"
)

// D-239: a refresh whose response was lost. The client still holds the token
// the server rotated; its retry inside RotationGrace, while the successor is
// unused, gets the lost response again (same refresh token, same family, a
// new access token), writes nothing and audits nothing.
func TestRefreshLostResponseReplay(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in := f.signIn("+919876543210", authn.AppCustomer)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)
	fam := ids.UUID(p.SessionID)

	r1, err := refreshIn(f, authn.AppCustomer, in.RefreshToken) // the response is lost
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(r1.RefreshToken))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM sessions WHERE refresh_hash = $1 AND revoked_at IS NULL`, sum[:]),
		"the successor is stored as its SHA-256")
	require.Equal(t, 1, f.count(`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_reason = 'rotated'
		AND successor_id IS NOT NULL AND octet_length(successor_salt) = 32`, fam))

	f.clk.Advance(time.Second)
	rr, err := refreshIn(f, authn.AppCustomer, in.RefreshToken)
	require.NoError(t, err, "a retry at once recovers")
	require.Equal(t, r1.RefreshToken, rr.RefreshToken, "the same refresh token: the lost response")
	require.Equal(t, r1.RefreshExpiresAt.UTC(), rr.RefreshExpiresAt.UTC(), "the same expiry")
	rp, err := f.svc.tokens.Verify(rr.AccessToken)
	require.NoError(t, err)
	require.Equal(t, p.SessionID, rp.SessionID, "the same family")
	require.Equal(t, in.UserID, rp.UserID)

	f.clk.Advance(RotationGrace - time.Second)
	rr, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
	require.NoError(t, err, "a retry at the edge of the window recovers")
	require.Equal(t, r1.RefreshToken, rr.RefreshToken)
	require.Contains(t, f.out.String(), "lost refresh response replayed", "the log capture is live")

	require.Zero(t, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.refresh_reuse'`))
	require.Equal(t, 2, f.count(`SELECT count(*) FROM sessions WHERE family_id = $1`, fam), "a replay inserts nothing")
	require.Equal(t, 1, f.count(`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NULL`, fam))

	// The recovered token carries on.
	r2, err := refreshIn(f, authn.AppCustomer, rr.RefreshToken)
	require.NoError(t, err)

	// The successor is now used: the first token, still inside its window,
	// is a plain grace 401 that ends nothing (as before D-239).
	_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized)
	require.True(t, errors.As(err, new(graceRetry)), "the successor is in use")
	require.Contains(t, f.out.String(), "within the grace window")
	require.Equal(t, 1, f.count(`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NULL`, fam))
	require.Zero(t, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.refresh_reuse'`))

	// Just past the window, the first token is reuse: the family ends.
	f.clk.Advance(time.Microsecond)
	_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized)
	require.False(t, errors.As(err, new(graceRetry)))
	_, err = refreshIn(f, authn.AppCustomer, r2.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized, "the newest token died with the family")
	st, err := f.svc.CheckSession(context.Background(), in.UserID, p.SessionID, authn.AppCustomer)
	require.NoError(t, err)
	require.False(t, st.Active)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.refresh_reuse'`))
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NULL`, fam))
}

// A successor that has been used means the old token is real reuse once its
// window is over: the family is revoked and audited, as before D-239.
func TestRefreshSuccessorUsedThenOldToken(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in := f.signIn("+919876543210", authn.AppCustomer)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)
	fam := ids.UUID(p.SessionID)

	r1, err := refreshIn(f, authn.AppCustomer, in.RefreshToken)
	require.NoError(t, err)
	f.clk.Advance(time.Minute)
	r2, err := refreshIn(f, authn.AppCustomer, r1.RefreshToken) // the successor is used
	require.NoError(t, err)

	// r1 was rotated just now and its successor is unused: a replay of r1.
	rr, err := refreshIn(f, authn.AppCustomer, r1.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, r2.RefreshToken, rr.RefreshToken)

	// in was rotated a minute ago: reuse.
	_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized)
	require.False(t, errors.As(err, new(graceRetry)))
	_, err = refreshIn(f, authn.AppCustomer, r2.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized, "the whole family is revoked")
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NULL`, fam))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.refresh_reuse'`))
	require.Contains(t, f.out.String(), "refresh token reuse")
}

// Past the window the replay is gone even when the successor was never used:
// presenting the rotated token is reuse, as before D-239.
func TestRefreshReplayAfterGrace(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in := f.signIn("+919876543210", authn.AppCustomer)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)

	r1, err := refreshIn(f, authn.AppCustomer, in.RefreshToken)
	require.NoError(t, err)
	f.clk.Advance(RotationGrace + time.Microsecond)
	_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized)
	require.False(t, errors.As(err, new(graceRetry)))
	_, err = refreshIn(f, authn.AppCustomer, r1.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized, "the unused successor died with the family")
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NULL`, ids.UUID(p.SessionID)))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.refresh_reuse'`))
}

// No replay without a derivable successor: a row rotated before D-239 (no
// successor recorded), a salt that does not derive the successor's hash (a
// rotated PII_HASH_KEY), or a successor revoked by a sign-out. Each is the
// plain grace 401, as before.
func TestRefreshReplayNeedsLiveSuccessor(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	for i, spoil := range []string{
		`UPDATE sessions SET successor_id = NULL, successor_salt = NULL WHERE family_id = $1 AND revoked_reason = 'rotated'`,
		`UPDATE sessions SET successor_salt = decode(repeat('ab', 32), 'hex') WHERE family_id = $1 AND revoked_reason = 'rotated'`,
		`UPDATE sessions SET revoked_at = now(), revoked_reason = 'logout' WHERE family_id = $1 AND revoked_at IS NULL`,
	} {
		f.clk.Advance(OTPWindow)
		in := f.signIn(fmt.Sprintf("+9198765436%02d", i), authn.AppCustomer)
		p, err := f.svc.tokens.Verify(in.AccessToken)
		require.NoError(t, err)
		_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
		require.NoError(t, err)
		f.owner(spoil, ids.UUID(p.SessionID))
		_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
		requireCode(t, err, genproblems.Unauthorized, "case %d", i)
		require.True(t, errors.As(err, new(graceRetry)), "case %d", i)
	}
	require.Zero(t, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.refresh_reuse'`))
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE revoked_reason = 'reuse'`))
}

// A replay passes the same checks as a refresh (a blocked user is refused),
// and the admin app never replays.
func TestRefreshReplayChecks(t *testing.T) {
	t.Run("blocked", func(t *testing.T) {
		f := newFixture(t, config.EnvTest)
		in := f.signIn("+919876543210", authn.AppCustomer)
		_, err := refreshIn(f, authn.AppCustomer, in.RefreshToken)
		require.NoError(t, err)
		f.owner(`UPDATE users SET status = 'blocked' WHERE id = $1`, ids.UUID(in.UserID))
		_, err = refreshIn(f, authn.AppCustomer, in.RefreshToken)
		requireCode(t, err, genproblems.AccountBlocked)
	})
	t.Run("admin", func(t *testing.T) {
		// The admin app never replays: a retry inside the window is the
		// grace 401, nothing is revoked, and the successor keeps working.
		f := newFixture(t, config.EnvTest)
		e := enrolAdmin(f)
		r1, err := refreshIn(f, authn.AppAdmin, e.refresh)
		require.NoError(t, err)
		_, err = refreshIn(f, authn.AppAdmin, e.refresh)
		requireCode(t, err, genproblems.Unauthorized)
		require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE revoked_reason = 'reuse'`))
		r2, err := refreshIn(f, authn.AppAdmin, r1.RefreshToken)
		require.NoError(t, err)
		require.Equal(t, e.in.RefreshExpiresAt.UTC(), r2.RefreshExpiresAt.UTC(), "the admin sign-in stays absolute")
		rp, err := f.svc.tokens.Verify(r2.AccessToken)
		require.NoError(t, err)
		require.Equal(t, authn.AMROTPTOTP, rp.AMR)

		f.owner(`UPDATE admin_users SET disabled_at = now() WHERE user_id = $1`, ids.UUID(e.in.UserID))
		_, err = refreshIn(f, authn.AppAdmin, r2.RefreshToken)
		requireCode(t, err, genproblems.Forbidden)
		require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE app = 'admin' AND revoked_at IS NULL`))
	})
}

// Two tabs (or a retry and its own retry) present the lost token at once:
// the users row lock serialises them, both get the same successor, nothing is
// revoked or audited, and one live row remains.
func TestRefreshConcurrentReplays(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const retries = 4
	for i := range raceTrials {
		f.clk.Advance(OTPWindow)
		in := f.signIn(fmt.Sprintf("+9198765437%02d", i), authn.AppCustomer)
		p, err := f.svc.tokens.Verify(in.AccessToken)
		require.NoError(t, err)
		r1, err := refreshIn(f, authn.AppCustomer, in.RefreshToken) // lost
		require.NoError(t, err)

		var wg sync.WaitGroup
		start := make(chan struct{})
		outs := make([]SignIn, retries)
		errs := make([]error, retries)
		for j := range retries {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				outs[j], errs[j] = refreshIn(f, authn.AppCustomer, in.RefreshToken)
			}()
		}
		close(start)
		wg.Wait()
		for j := range retries {
			require.NoError(t, errs[j], "trial %d retry %d", i, j)
			require.Equal(t, r1.RefreshToken, outs[j].RefreshToken, "trial %d retry %d", i, j)
		}
		require.Equal(t, 1, f.count(`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NULL`, ids.UUID(p.SessionID)), "trial %d", i)
		require.Equal(t, 2, f.count(`SELECT count(*) FROM sessions WHERE family_id = $1`, ids.UUID(p.SessionID)), "trial %d", i)
	}
	require.Zero(t, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.refresh_reuse'`))
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE revoked_reason = 'reuse'`))
}

// A replay of the lost token racing a refresh of its successor (one tab got
// the response, another retries): whichever commits first, the replay either
// returns the successor or is a grace 401, the refresh succeeds or (if it
// presented the successor after the replay) succeeds too, and the family is
// never revoked.
func TestRefreshReplayRacesSuccessorRefresh(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	for i := range raceTrials {
		f.clk.Advance(OTPWindow)
		in := f.signIn(fmt.Sprintf("+9198765438%02d", i), authn.AppCustomer)
		p, err := f.svc.tokens.Verify(in.AccessToken)
		require.NoError(t, err)
		r1, err := refreshIn(f, authn.AppCustomer, in.RefreshToken)
		require.NoError(t, err)

		var (
			wg             sync.WaitGroup
			replay, next   SignIn
			replayE, nextE error
		)
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			replay, replayE = refreshIn(f, authn.AppCustomer, in.RefreshToken)
		}()
		go func() {
			defer wg.Done()
			<-start
			next, nextE = refreshIn(f, authn.AppCustomer, r1.RefreshToken)
		}()
		close(start)
		wg.Wait()

		require.NoError(t, nextE, "trial %d: the successor's holder carries on", i)
		require.NotEqual(t, r1.RefreshToken, next.RefreshToken, "trial %d", i)
		if replayE != nil {
			requireCode(t, replayE, genproblems.Unauthorized, "trial %d", i)
			require.True(t, errors.As(replayE, new(graceRetry)), "trial %d: %v", i, replayE)
		} else {
			require.Equal(t, r1.RefreshToken, replay.RefreshToken, "trial %d", i)
		}
		require.Equal(t, 1, f.count(`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NULL`, ids.UUID(p.SessionID)), "trial %d", i)
	}
	require.Zero(t, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.refresh_reuse'`))
}

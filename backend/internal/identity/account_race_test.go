package identity

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// raceTrials is how many times each revocation races a refresh loop. Without
// the users row lock (M1) a run of this size missed a rotated row in most
// trials; with it, none may.
const raceTrials = 12

// refreshLoop keeps rotating a sign-in from token on, each refresh with the
// newest token, until stop is called or a refresh is refused. started is
// closed after the first rotation, so the caller's revocation overlaps the
// loop. stop returns the error that ended the loop (nil when stop did).
type refreshLoop struct {
	started chan struct{}
	quit    chan struct{}
	done    chan error
}

func startRefreshLoop(f *fixture, app authn.App, token string) *refreshLoop {
	l := &refreshLoop{started: make(chan struct{}), quit: make(chan struct{}), done: make(chan error, 1)}
	go func() {
		first := true
		for {
			select {
			case <-l.quit:
				l.done <- nil
				return
			default:
			}
			out, err := f.svc.Refresh(f.ctx("10.0.0.2"), RefreshInput{App: app, Token: token, UserAgent: "loop"})
			f.remember(out.AccessToken, out.RefreshToken)
			if first {
				first = false
				close(l.started)
			}
			if err != nil {
				l.done <- err
				return
			}
			token = out.RefreshToken
		}
	}()
	return l
}

func (l *refreshLoop) stop() error {
	close(l.quit)
	return <-l.done
}

// refusedOrStopped is what may end a loop: the revocation's 401 (or the
// account's refusal once deleted), or stop. Anything else, a deadlock or a
// 500, fails the test.
func refusedOrStopped(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var p *problem.Error
	require.True(t, errors.As(err, &p), "the loop ended on %v", err)
	require.Contains(t, []string{genproblems.Unauthorized, genproblems.AccountBlocked}, p.Code, "the loop ended on %v", err)
}

// M1: logout-all racing a refresh loop on another device of the same user
// leaves no live sign-in of that app (the rotated row is not missed).
func TestLogoutAllRacesRefresh(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	for i := range raceTrials {
		f.clk.Advance(OTPWindow) // fresh send limits per trial
		phone := fmt.Sprintf("+9198765432%02d", i)
		a := f.signIn(phone, authn.AppCustomer)
		b := f.signIn(phone, authn.AppCustomer)
		biz := f.signIn(phone, authn.AppBusiness)
		uid := ids.UUID(a.UserID)

		l := startRefreshLoop(f, authn.AppCustomer, a.RefreshToken)
		<-l.started
		_, err := f.svc.LogoutAll(f.ctx("10.0.0.1"), RefreshInput{App: authn.AppCustomer, Token: b.RefreshToken})
		require.NoError(t, err, "trial %d", i)
		refusedOrStopped(t, l.stop())

		require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND app = 'customer' AND revoked_at IS NULL`, uid),
			"trial %d: a refresh slipped a live row past logout-all", i)
		_, err = refreshIn(f, authn.AppBusiness, biz.RefreshToken)
		require.NoError(t, err, "trial %d: the business app stays", i)
	}
}

// M1: the TOTP lockout racing a refresh loop on another admin sign-in of the
// same admin leaves no live admin session.
func TestTOTPLockoutRacesRefresh(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	a := enrolAdmin(f)
	for i := range raceTrials {
		f.clk.Advance(OTPWindow)
		_, p1 := adminSignIn(f)
		f.clk.Advance(TOTPPeriod)
		elevated := f.totp(p1, a.secret, TOTPPurposeLogin) // the sign-in the loop keeps
		_, p2 := adminSignIn(f)
		uid := ids.UUID(p2.UserID)

		l := startRefreshLoop(f, authn.AppAdmin, elevated.RefreshToken)
		<-l.started
		var err error
		for range TOTPMaxFailures {
			_, err = f.svc.VerifyTOTP(f.ctx("10.0.0.1"), p2, wrongCode(a.secret, f.clk.Now()), TOTPPurposeLogin)
		}
		requireCode(t, err, genproblems.OtpLocked, "trial %d", i)
		refusedOrStopped(t, l.stop())

		require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND app = 'admin' AND revoked_at IS NULL`, uid),
			"trial %d: a refresh slipped a live admin row past the lockout", i)
	}
}

// M1 + L2: deleting the account while another device refreshes neither
// deadlocks (users FOR NO KEY UPDATE against the refresh's FOR SHARE, never
// FOR UPDATE against the session insert's KEY SHARE) nor misses a row.
func TestDeleteAccountRacesRefresh(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	for i := range raceTrials {
		f.clk.Advance(OTPWindow)
		phone := fmt.Sprintf("+9198765433%02d", i)
		a := f.signIn(phone, authn.AppCustomer)
		b := f.signIn(phone, authn.AppCustomer)
		p, err := f.svc.tokens.Verify(b.AccessToken)
		require.NoError(t, err)
		uid := ids.UUID(a.UserID)
		reqID, code := stepUpCode(f, phone)

		l := startRefreshLoop(f, authn.AppCustomer, a.RefreshToken)
		<-l.started
		require.NoError(t, f.svc.DeleteAccount(f.ctx("10.0.0.1"), p, DeleteInput{RequestID: reqID, Code: code}), "trial %d", i)
		refusedOrStopped(t, l.stop())

		require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, uid),
			"trial %d: a refresh slipped a live row past the deletion", i)
		require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND (user_agent IS NOT NULL OR ip_hash IS NOT NULL)`, uid),
			"trial %d: a session kept its user agent or IP hash", i)
	}
}

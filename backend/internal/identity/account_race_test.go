package identity

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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

// Re-review finding 1: reuse detection racing a refresh loop on the same
// sign-in. The loop rotates the family while an old token (rotated past the
// grace window) is presented; the reuse revocation must end the whole family,
// including a row the loop inserts meanwhile.
func TestRefreshReuseRacesRefresh(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	for i := range raceTrials {
		f.clk.Advance(OTPWindow)
		phone := fmt.Sprintf("+9198765434%02d", i)
		in := f.signIn(phone, authn.AppCustomer)
		p, err := f.svc.tokens.Verify(in.AccessToken)
		require.NoError(t, err)
		r1, err := refreshIn(f, authn.AppCustomer, in.RefreshToken)
		require.NoError(t, err)
		f.clk.Advance(RotationGrace + time.Second) // in.RefreshToken is now reuse

		l := startRefreshLoop(f, authn.AppCustomer, r1.RefreshToken)
		<-l.started
		_, err = f.svc.Refresh(f.ctx("10.0.0.1"), RefreshInput{App: authn.AppCustomer, Token: in.RefreshToken, UserAgent: "thief"})
		requireCode(t, err, genproblems.Unauthorized, "trial %d", i)
		require.False(t, errors.As(err, new(graceRetry)), "trial %d", i)
		refusedOrStopped(t, l.stop())

		require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NULL`, ids.UUID(p.SessionID)),
			"trial %d: a refresh slipped a live row past reuse detection", i)
	}
}

// Re-review finding 1: a plain logout with an older cookie of the sign-in,
// racing a refresh loop on it, ends the whole family.
func TestLogoutRacesRefresh(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	for i := range raceTrials {
		f.clk.Advance(OTPWindow)
		phone := fmt.Sprintf("+9198765435%02d", i)
		in := f.signIn(phone, authn.AppCustomer)
		p, err := f.svc.tokens.Verify(in.AccessToken)
		require.NoError(t, err)
		r1, err := refreshIn(f, authn.AppCustomer, in.RefreshToken)
		require.NoError(t, err)

		l := startRefreshLoop(f, authn.AppCustomer, r1.RefreshToken)
		<-l.started
		require.NoError(t, f.svc.Logout(f.ctx("10.0.0.1"), RefreshInput{App: authn.AppCustomer, Token: in.RefreshToken}), "trial %d", i)
		refusedOrStopped(t, l.stop())

		require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NULL`, ids.UUID(p.SessionID)),
			"trial %d: a refresh slipped a live row past logout", i)
	}
}

// waitingLocks is how many lock requests of this test database's backends
// are waiting.
func waitingLocks(f *fixture) int {
	return f.count(`SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid
		WHERE NOT l.granted AND a.datname = current_database()`)
}

// waitFor polls until cond holds or done is closed, failing after 10 s of
// real time.
func waitFor(t *testing.T, done <-chan struct{}, cond func() bool, what string) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case <-done:
			return
		case <-timeout:
			require.FailNow(t, "timed out waiting for "+what)
		default:
		}
		if cond() {
			return
		}
		select {
		case <-done:
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// loginCode sends a customer login OTP to phone and returns its request id
// and code.
func loginCode(f *fixture, phone string) (ids.ID, string) {
	f.t.Helper()
	res, err := f.send(phone, authn.AppCustomer, "10.0.0.1")
	require.NoError(f.t, err)
	code := f.sms.code(phone)
	f.remember(code)
	return res.RequestID, code
}

type signInOutcome struct {
	out SignIn
	err error
}

// startSignIn verifies a login code in the background on a new device.
func startSignIn(f *fixture, reqID ids.ID, code string, device ids.ID) (<-chan signInOutcome, <-chan struct{}) {
	res, done := make(chan signInOutcome, 1), make(chan struct{})
	go func() {
		defer close(done)
		out, err := f.svc.VerifyOTP(f.ctx("10.0.0.3"), VerifyInput{
			App: authn.AppCustomer, RequestID: reqID.String(), Code: code, DeviceID: device.String(), UserAgent: "race",
		})
		f.remember(out.AccessToken, out.RefreshToken)
		res <- signInOutcome{out, err}
	}()
	return res, done
}

// Re-review finding 2: a sign-in that found the user before a deletion
// committed must not leave a session or device behind. The sign-in is held
// after its user lookup (a test transaction holds an uncommitted delete of
// the user's customer profile, which the sign-in's profile insert waits for)
// while the deletion runs. With the users row lock the deletion waits for the
// sign-in and then revokes and scrubs what it wrote; without it the deletion
// commits first and the sign-in leaves a live session for a deleted user.
func TestSignInRacesDeleteAccount(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const phone = "+919876543620"
	in := f.signIn(phone, authn.AppCustomer)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)
	uid := ids.UUID(in.UserID)
	delReq, delCode := stepUpCode(f, phone)
	loginReq, code := loginCode(f, phone)
	device := ids.New(f.clk.Now())

	ctx := context.Background()
	hold, err := f.db.OwnerPool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = hold.Rollback(ctx) })
	_, err = hold.Exec(ctx, `DELETE FROM customer_profiles WHERE user_id = $1`, uid)
	require.NoError(t, err)

	signedIn, signDone := startSignIn(f, loginReq, code, device)
	waitFor(t, signDone, func() bool { return waitingLocks(f) >= 1 }, "the sign-in to wait on the held profile")

	deleted, delDone := make(chan error, 1), make(chan struct{})
	go func() {
		defer close(delDone)
		deleted <- f.svc.DeleteAccount(f.ctx("10.0.0.1"), p, DeleteInput{RequestID: delReq, Code: delCode})
	}()
	// With the lock the deletion queues behind the sign-in (two waiters);
	// without it, it finishes.
	waitFor(t, delDone, func() bool { return waitingLocks(f) >= 2 }, "the deletion to finish or wait")
	require.NoError(t, hold.Rollback(ctx))

	s := <-signedIn
	require.NoError(t, s.err, "the sign-in committed before the deletion")
	require.NoError(t, <-deleted)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM users WHERE id = $1 AND status = 'deleted'`, uid))
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, uid),
		"a sign-in left a live session for a deleted user")
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND (user_agent IS NOT NULL OR ip_hash IS NOT NULL)`, uid))
	require.Zero(t, f.count(`SELECT count(*) FROM devices WHERE user_id = $1 AND device_key = $2`, uid, device.String()),
		"the sign-in's device key still links to the deleted user")
	_, err = refreshIn(f, authn.AppCustomer, s.out.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized)
}

// Re-review finding 2, the other order: a sign-in that looked the user up
// while a deletion held the users row waits for it, then reads the user as
// deleted and is refused (account_blocked) without writing a session or a
// device. The deletion is held in an OnAccountDelete hook, after its users
// row lock and before it deletes the phone identity.
func TestSignInDuringDeletionIsRefused(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const phone = "+919876543621"
	in := f.signIn(phone, authn.AppCustomer)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)
	uid := ids.UUID(in.UserID)
	delReq, delCode := stepUpCode(f, phone)
	loginReq, code := loginCode(f, phone)
	device := ids.New(f.clk.Now())

	inHook, release := make(chan struct{}), make(chan struct{})
	f.svc.AddDeletionHook(OnDeleteFunc(func(context.Context, ids.ID) error {
		close(inHook)
		<-release
		return nil
	}))
	deleted := make(chan error, 1)
	go func() {
		deleted <- f.svc.DeleteAccount(f.ctx("10.0.0.1"), p, DeleteInput{RequestID: delReq, Code: delCode})
	}()
	<-inHook

	signedIn, signDone := startSignIn(f, loginReq, code, device)
	waitFor(t, signDone, func() bool { return waitingLocks(f) >= 1 }, "the sign-in to wait for the deletion")
	close(release)
	require.NoError(t, <-deleted)

	s := <-signedIn
	requireCode(t, s.err, genproblems.AccountBlocked, "the user was deleted while the sign-in waited")
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, uid))
	require.Zero(t, f.count(`SELECT count(*) FROM devices WHERE device_key = $1`, device.String()))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM otp_requests WHERE id = $1 AND consumed_at IS NOT NULL`, ids.UUID(loginReq)),
		"the code is used, as for any refused sign-in")
}

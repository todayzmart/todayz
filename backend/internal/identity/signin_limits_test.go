package identity

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// The defaults of 49502_w4-95_admin_signin_limits.sql (01 §17).
const (
	defaultMaxFailures   = 10
	defaultFailureWindow = 24 * time.Hour
	defaultLockFor       = 24 * time.Hour
	defaultStepUpPerHour = 10
)

// adminCodes walks admin-app sign-in codes of phone: a fresh code whenever
// the last one has used its five tries.
type adminCodes struct {
	f     *fixture
	phone string
	req   ids.ID
	code  string
	used  int
}

func newAdminCodes(f *fixture, phone string) *adminCodes {
	return &adminCodes{f: f, phone: phone, used: OTPMaxAttempts}
}

// fresh sends a new admin sign-in code and waits out the resend gap.
func (c *adminCodes) fresh() {
	c.f.t.Helper()
	res, err := c.f.send(c.phone, authn.AppAdmin, "10.0.0.1")
	require.NoError(c.f.t, err)
	c.req, c.code, c.used = res.RequestID, c.f.sms.code(c.phone), 0
	c.f.remember(c.code)
	c.f.clk.Advance(OTPResendGap)
}

// wrong tries n wrong codes and returns the last answer.
func (c *adminCodes) wrong(n int) error {
	c.f.t.Helper()
	var err error
	for range n {
		if c.used == OTPMaxAttempts {
			c.fresh()
		}
		_, err = c.f.verify(authn.AppAdmin, c.req, otherCode(c.code))
		c.used++
	}
	return err
}

// requireLocked checks a signin_locked answer and its Retry-After.
func requireLocked(t *testing.T, err error, wait time.Duration, msg string) {
	t.Helper()
	requireCode(t, err, genproblems.SigninLocked, msg)
	require.Equal(t, strconv.Itoa(int(wait/time.Second)), problemOf(t, err).Header.Get("Retry-After"), msg)
}

func (f *fixture) failures(phone string) int {
	f.t.Helper()
	return f.count(`SELECT count(*) FROM admin_signin_failures WHERE phone_hash = $1`, f.svc.hash.Phone(phone))
}

func (f *fixture) locks(phone string) int {
	f.t.Helper()
	return f.count(`SELECT count(*) FROM admin_signin_locks WHERE phone_hash = $1`, f.svc.hash.Phone(phone))
}

// D-264 review (founder, 27 Oct: "Lock after 10 wrong"): wrong admin sign-in
// codes count per number across codes; the tenth within the window locks the
// number's admin sign-in for the lock time or until another super admin
// unlocks it; a good code clears the count.
func TestAdminSigninLock(t *testing.T) {
	const (
		strangerPhone = "+919876543212" // no account at all
		customerPhone = "+919876543213" // a customer, not an admin
	)
	type world struct {
		f     *fixture
		admin authn.Principal
	}
	setup := func(t *testing.T) world {
		f := newFixture(t, config.EnvTest)
		_, p := newAdmin(f, adminPhone)
		f.clk.Advance(OTPWindow) // a fresh hour of sends for the case
		return world{f: f, admin: p}
	}

	cases := []struct {
		name string
		run  func(t *testing.T, w world)
	}{
		{"the tenth wrong code locks", func(t *testing.T, w world) {
			f := w.f
			c := newAdminCodes(f, adminPhone)
			for i := 1; i < defaultMaxFailures; i++ {
				err := c.wrong(1)
				requireCode(t, err, genproblems.Unauthorized, i)
			}
			require.Equal(t, defaultMaxFailures-1, f.failures(adminPhone), "counted across codes")
			c.fresh() // a good code in hand before the lock
			goodReq, goodCode := c.req, c.code
			err := c.wrong(1)
			requireLocked(t, err, defaultLockFor, "the tenth wrong code answers the lock")
			require.Equal(t, 1, f.locks(adminPhone))
			require.Zero(t, f.failures(adminPhone), "the count starts again after the lock")

			_, err = f.verify(authn.AppAdmin, goodReq, goodCode)
			requireLocked(t, err, defaultLockFor, "a good code is refused while locked")
			require.Equal(t, 1, f.count(`SELECT attempts FROM otp_requests WHERE id = $1`, ids.UUID(goodReq)), "and no try is spent")
			_, err = f.send(adminPhone, authn.AppAdmin, "10.0.0.2")
			requireLocked(t, err, defaultLockFor, "no admin sign-in code is sent while locked")

			f.clk.Advance(time.Hour)
			_, err = f.send(adminPhone, authn.AppAdmin, "10.0.0.2")
			requireLocked(t, err, defaultLockFor-time.Hour, "Retry-After counts down")
			f.signIn(adminPhone, authn.AppCustomer) // other apps of the number are not locked
			st, err := f.svc.CheckSession(context.Background(), w.admin.UserID, w.admin.SessionID, authn.AppAdmin)
			require.NoError(t, err)
			require.True(t, st.Active, "a session already open keeps working")

			f.clk.Advance(defaultLockFor - time.Hour)
			adminSignIn(f, adminPhone) // the lock has run out
		}},
		{"only wrong codes within the window count", func(t *testing.T, w world) {
			f := w.f
			c := newAdminCodes(f, adminPhone)
			requireCode(t, c.wrong(defaultMaxFailures-1), genproblems.Unauthorized)
			f.clk.Advance(defaultFailureWindow)
			c.used = OTPMaxAttempts // the old code has expired
			requireCode(t, c.wrong(1), genproblems.Unauthorized, "the nine old ones have left the window")
			requireCode(t, c.wrong(defaultMaxFailures-2), genproblems.Unauthorized, "nine within the window")
			requireLocked(t, c.wrong(1), defaultLockFor, "the tenth within the window")
		}},
		{"a good sign-in resets the count", func(t *testing.T, w world) {
			f := w.f
			c := newAdminCodes(f, adminPhone)
			requireCode(t, c.wrong(defaultMaxFailures-1), genproblems.Unauthorized)
			c.fresh()
			_, err := f.verify(authn.AppAdmin, c.req, c.code)
			require.NoError(t, err)
			require.Zero(t, f.failures(adminPhone))
			f.clk.Advance(OTPWindow) // sends
			c.used = OTPMaxAttempts
			requireCode(t, c.wrong(defaultMaxFailures-1), genproblems.Unauthorized, "nine again is not ten")
			requireLocked(t, c.wrong(1), defaultLockFor, "the count started from zero")
		}},
		{"another super admin unlocks", func(t *testing.T, w world) {
			f := w.f
			acc := &Accounts{s: f.svc}
			unlock := func(user ids.ID) *time.Time {
				var until *time.Time
				require.NoError(t, f.svc.db.InTx(context.Background(), func(ctx context.Context) error {
					var err error
					until, err = acc.UnlockAdminSignIn(ctx, user, f.clk.Now().UTC())
					return err
				}))
				return until
			}
			require.Nil(t, unlock(w.admin.UserID), "nothing to unlock")
			c := newAdminCodes(f, adminPhone)
			requireLocked(t, c.wrong(defaultMaxFailures), defaultLockFor, "locked")
			lockedAt := f.clk.Now().UTC() // the tenth wrong code, after the last send's gap
			until := unlock(w.admin.UserID)
			require.NotNil(t, until)
			require.Equal(t, lockedAt.Add(defaultLockFor), until.UTC())
			require.Zero(t, f.locks(adminPhone))
			require.Zero(t, f.failures(adminPhone))
			adminSignIn(f, adminPhone) // straight away
			require.Nil(t, unlock(w.admin.UserID), "a second unlock finds no lock")

			f.clk.Advance(OTPWindow)
			c.used = OTPMaxAttempts
			requireLocked(t, c.wrong(defaultMaxFailures), defaultLockFor, "locked again")
			f.clk.Advance(defaultLockFor)
			require.Nil(t, unlock(w.admin.UserID), "a lock that ran out is not unlocked")
		}},
		{"a number that is not an admin locks alike", func(t *testing.T, w world) {
			f := w.f
			f.signIn(customerPhone, authn.AppCustomer)
			for _, phone := range []string{strangerPhone, customerPhone} {
				c := newAdminCodes(f, phone)
				requireCode(t, c.wrong(defaultMaxFailures-1), genproblems.Unauthorized, phone)
				requireLocked(t, c.wrong(1), defaultLockFor, phone)
				_, err := f.send(phone, authn.AppAdmin, "10.0.0.3")
				requireLocked(t, err, defaultLockFor, phone)
			}
			require.Equal(t, 1, f.locks(strangerPhone))
			require.Zero(t, f.count(`SELECT count(*) FROM users u JOIN phone_identities pi ON pi.user_id = u.id WHERE pi.phone_hash = $1`,
				f.svc.hash.Phone(strangerPhone)), "the lock makes no account")
		}},
		{"a good code of a number that is not an admin clears the count", func(t *testing.T, w world) {
			f := w.f
			c := newAdminCodes(f, strangerPhone)
			requireCode(t, c.wrong(3), genproblems.Unauthorized)
			c.fresh()
			_, err := f.verify(authn.AppAdmin, c.req, c.code)
			requireCode(t, err, genproblems.Forbidden, "sign-in still refuses a stranger")
			require.Zero(t, f.failures(strangerPhone), "as it does for an admin")
		}},
		{"customer app codes do not count", func(t *testing.T, w world) {
			f := w.f
			res, err := f.send(adminPhone, authn.AppCustomer, "10.0.0.1")
			require.NoError(t, err)
			code := f.sms.code(adminPhone)
			f.remember(code)
			for range OTPMaxAttempts {
				_, err = f.verify(authn.AppCustomer, res.RequestID, otherCode(code))
				requireCode(t, err, genproblems.Unauthorized)
			}
			require.Zero(t, f.failures(adminPhone))
		}},
		{"the numbers are settings", func(t *testing.T, w world) {
			f := w.f
			f.owner(`UPDATE platform_settings SET value = '3' WHERE key = 'admin_signin_max_failures'`)
			f.owner(`UPDATE platform_settings SET value = '2' WHERE key = 'admin_signin_lock_hours'`)
			f.svc.settings.Invalidate()
			c := newAdminCodes(f, adminPhone)
			requireCode(t, c.wrong(2), genproblems.Unauthorized)
			requireLocked(t, c.wrong(1), 2*time.Hour, "three wrong, two hours")
		}},
		{"the purge", func(t *testing.T, w world) {
			f := w.f
			requireCode(t, newAdminCodes(f, strangerPhone).wrong(1), genproblems.Unauthorized)
			requireLocked(t, newAdminCodes(f, adminPhone).wrong(defaultMaxFailures), defaultLockFor, "locked")
			res, err := Purge(context.Background(), f.svc.db, f.clk.Now())
			require.NoError(t, err)
			require.Zero(t, res.AdminSigninFailures+res.AdminSigninLocks, "nothing is old yet")
			f.clk.Advance(AdminSigninFailureRetention)
			res, err = Purge(context.Background(), f.svc.db, f.clk.Now())
			require.NoError(t, err)
			require.EqualValues(t, 1, res.AdminSigninFailures)
			require.EqualValues(t, 1, res.AdminSigninLocks)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, setup(t)) })
	}
}

// D-264 review (founder, 27 Oct: "Own quota of 10/hour"): admin step-up sends
// have their own bucket per number, apart from the sign-in limits, with the
// resend gap among step-up codes only.
func TestAdminStepUpQuota(t *testing.T) {
	setup := func(t *testing.T) (*fixture, authn.Principal) {
		f := newFixture(t, config.EnvTest)
		_, p := newAdmin(f, adminPhone)
		return f, p
	}
	stepUpSend := func(f *fixture, p authn.Principal) error {
		_, err := f.svc.SendStepUp(f.ctx("10.0.0.1"), p)
		f.remember(f.sms.code(adminPhone))
		return err
	}

	cases := []struct {
		name string
		run  func(t *testing.T, f *fixture, p authn.Principal)
	}{
		{"a step-up straight after sign-in", func(t *testing.T, f *fixture, _ authn.Principal) {
			res, err := f.send(adminPhone, authn.AppAdmin, "10.0.0.1")
			require.NoError(t, err)
			in, err := f.verify(authn.AppAdmin, res.RequestID, f.sms.code(adminPhone))
			require.NoError(t, err)
			p, err := f.svc.tokens.Verify(in.AccessToken)
			require.NoError(t, err)
			require.NoError(t, stepUpSend(f, p), "the sign-in's resend gap does not hold up a step-up")
		}},
		{"the resend gap among step-up codes", func(t *testing.T, f *fixture, p authn.Principal) {
			require.NoError(t, stepUpSend(f, p))
			err := stepUpSend(f, p)
			require.Equal(t, int(OTPResendGap/time.Second), retryAfter(t, err))
			f.clk.Advance(OTPResendGap)
			require.NoError(t, stepUpSend(f, p))
		}},
		{"its own ten an hour", func(t *testing.T, f *fixture, p authn.Principal) {
			first := f.clk.Now()
			for i := range defaultStepUpPerHour {
				require.NoError(t, stepUpSend(f, p), i)
				f.clk.Advance(OTPResendGap)
			}
			err := stepUpSend(f, p)
			require.Equal(t, int(first.Add(OTPWindow).Sub(f.clk.Now())/time.Second), retryAfter(t, err), "the eleventh waits for the oldest")
			// Two sign-in sends were spent by newAdmin; the step-ups took none.
			for i := 2; i < OTPPerPhone; i++ {
				_, err := f.send(adminPhone, authn.AppAdmin, "10.0.0.1")
				require.NoError(t, err, "sign-in send %d", i+1)
				f.clk.Advance(OTPResendGap)
			}
			_, err = f.send(adminPhone, authn.AppAdmin, "10.0.0.1")
			require.Positive(t, retryAfter(t, err), "the sign-in limit is its own")
			f.clk.Advance(OTPWindow)
			require.NoError(t, stepUpSend(f, p), "a new hour")
		}},
		{"sign-in sends do not use up step-ups", func(t *testing.T, f *fixture, p authn.Principal) {
			for i := 2; i < OTPPerPhone; i++ {
				_, err := f.send(adminPhone, authn.AppAdmin, "10.0.0.1")
				require.NoError(t, err)
				f.clk.Advance(OTPResendGap)
			}
			_, err := f.send(adminPhone, authn.AppAdmin, "10.0.0.1")
			require.Positive(t, retryAfter(t, err), "sign-in sends spent")
			require.NoError(t, stepUpSend(f, p), "a step-up still goes")
		}},
		{"the quota is a setting", func(t *testing.T, f *fixture, p authn.Principal) {
			f.owner(`UPDATE platform_settings SET value = '2' WHERE key = 'admin_step_up_sends_per_hour'`)
			f.svc.settings.Invalidate()
			require.NoError(t, stepUpSend(f, p))
			f.clk.Advance(OTPResendGap)
			require.NoError(t, stepUpSend(f, p))
			f.clk.Advance(OTPResendGap)
			require.Positive(t, retryAfter(t, stepUpSend(f, p)))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, p := setup(t)
			tc.run(t, f, p)
		})
	}
}

// VerifyStepUp takes only an admin-app step_up request: the admin's own
// sign-in request, with its right code, reads as unknown and is untouched.
func TestVerifyStepUpRefusesLoginRequest(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	_, p := newAdmin(f, adminPhone)
	res, err := f.send(adminPhone, authn.AppAdmin, "10.0.0.1")
	require.NoError(t, err)
	code := f.sms.code(adminPhone)
	f.remember(code)
	_, err = f.svc.VerifyStepUp(f.ctx("10.0.0.1"), p, res.RequestID.String(), code)
	requireCode(t, err, genproblems.OtpExpired)
	require.True(t, f.stepUpAt(p).IsZero())
	require.Equal(t, 1, f.count(`SELECT count(*) FROM otp_requests WHERE id = $1 AND attempts = 0 AND consumed_at IS NULL`, ids.UUID(res.RequestID)))
	_, err = f.verify(authn.AppAdmin, res.RequestID, code)
	require.NoError(t, err, "it still signs in")
}

// Two verifies of the same good step-up code at once: exactly one stamps the
// sign-in; the other finds the code used.
func TestConcurrentStepUpVerify(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	_, p := newAdmin(f, adminPhone)
	reqID, code := f.sendStepUp(p, adminPhone)

	const n = 2
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = f.svc.VerifyStepUp(f.ctx("10.0.0.1"), p, reqID, code)
		}()
	}
	close(start)
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
			continue
		}
		requireCode(t, err, genproblems.OtpExpired)
	}
	require.Equal(t, 1, ok, "exactly one verify succeeds")
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.step_up_verified'`))
}

// D-264 second review: when the lock settings cannot be read, a wrong admin
// code answers 503, but the transaction commits: the spent try and the
// counted failure stand, and the next readable failure trips the lock.
func TestAdminSigninFailedSettingsDown(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(f *fixture)
	}{
		{"the read fails", func(f *fixture) {
			f.svc.settings = settings.NewCache(func(context.Context) (map[settings.Key]settings.Value, error) {
				return nil, errors.New("settings are down")
			}, settings.Options{Log: f.log})
		}},
		{"no settings cache", func(f *fixture) { f.svc.settings = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, config.EnvTest)
			newAdmin(f, adminPhone)
			f.clk.Advance(OTPWindow)
			good := f.svc.settings
			c := newAdminCodes(f, adminPhone)
			requireCode(t, c.wrong(defaultMaxFailures-2), genproblems.Unauthorized)
			c.fresh()
			tc.break_(f)
			_, err := f.verify(authn.AppAdmin, c.req, otherCode(c.code))
			c.used++
			requireCode(t, err, genproblems.ServiceUnavailable)
			require.Equal(t, 1, f.count(`SELECT attempts FROM otp_requests WHERE id = $1`, ids.UUID(c.req)), "the try is spent")
			require.Equal(t, defaultMaxFailures-1, f.failures(adminPhone), "and the failure counted")
			require.Zero(t, f.locks(adminPhone))

			f.svc.settings = good
			requireLocked(t, c.wrong(1), defaultLockFor, "the tenth, once the settings read again")
		})
	}
}

// D-264 second review: two wrong codes at once on two requests of the same
// number. The per-number lock serialises them, so the lock trips exactly
// once. At eight, both are counted (nine, then ten) and both tries spent. At
// nine, the first trips the lock and the second then finds the number locked:
// it answers signin_locked and, like any verify while locked, spends no try
// and counts nothing (the count started again with the lock).
func TestAdminSigninParallelWrongCodes(t *testing.T) {
	cases := []struct {
		name    string
		before  int
		wantErr []string // the two answers, in any order
		tries   int      // tries spent across the two requests
	}{
		{"at eight both count", defaultMaxFailures - 2, []string{genproblems.Unauthorized, genproblems.SigninLocked}, 2},
		{"at nine the lock trips once", defaultMaxFailures - 1, []string{genproblems.SigninLocked, genproblems.SigninLocked}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, config.EnvTest)
			newAdmin(f, adminPhone)
			f.clk.Advance(OTPWindow)
			c := newAdminCodes(f, adminPhone)
			requireCode(t, c.wrong(tc.before), genproblems.Unauthorized)
			require.Equal(t, tc.before, f.failures(adminPhone))
			c.fresh()
			reqA, codeA := c.req, c.code
			c.fresh()
			reqB, codeB := c.req, c.code

			errs := make([]error, 2)
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i, r := range []struct {
				req  ids.ID
				code string
			}{{reqA, codeA}, {reqB, codeB}} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					_, errs[i] = f.verify(authn.AppAdmin, r.req, otherCode(r.code))
				}()
			}
			close(start)
			wg.Wait()
			got := make([]string, 0, 2)
			for _, err := range errs {
				require.Error(t, err)
				got = append(got, problemOf(t, err).Code)
			}
			require.ElementsMatch(t, tc.wantErr, got)
			require.Equal(t, 1, f.locks(adminPhone), "the lock trips exactly once")
			require.Zero(t, f.failures(adminPhone), "the count started again with the lock")
			require.Equal(t, tc.tries, f.count(`SELECT sum(attempts)::int FROM otp_requests WHERE id = ANY($1)`,
				[]any{ids.UUID(reqA), ids.UUID(reqB)}))
		})
	}
}

// D-264 second review: the operator's break-glass unlock (`todayz admin
// unlock-sign-in`) for when every super admin is locked out.
func TestBreakGlassUnlockAdminSignIn(t *testing.T) {
	const strangerPhone = "+919876543214"
	f := newFixture(t, config.EnvTest)
	_, p := newAdmin(f, adminPhone)
	f.clk.Advance(OTPWindow)
	unlock := func(phone, reason string) (BreakGlassResult, error) {
		f.remember(phone)
		return f.svc.BreakGlassUnlockAdminSignIn(context.Background(), phone, reason)
	}

	for _, tc := range []struct {
		name, phone, reason string
		want                error
	}{
		{"not E.164", "9876543210", "locked out", ErrBreakGlassPhone},
		{"empty", "", "locked out", ErrBreakGlassPhone},
		{"no reason", adminPhone, "  ", ErrBreakGlassReason},
		{"a number in the reason", adminPhone, "call 98765 43210", ErrBreakGlassReason},
		{"not an admin", strangerPhone, "locked out", ErrBreakGlassNotAdmin},
	} {
		_, err := unlock(tc.phone, tc.reason)
		require.ErrorIs(t, err, tc.want, tc.name)
		require.NotContains(t, err.Error(), "987654", tc.name)
	}
	require.Zero(t, f.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.unlock_sign_in'`), "a refusal changes nothing")

	// No lock: the failure count is cleared and the call audited all the same.
	requireCode(t, newAdminCodes(f, adminPhone).wrong(3), genproblems.Unauthorized)
	res, err := unlock(" "+adminPhone+"\n", "stale count")
	require.NoError(t, err)
	require.Equal(t, p.UserID, res.UserID)
	require.Nil(t, res.LockedUntil)
	require.Zero(t, f.failures(adminPhone))

	// Locked: lifted, and the admin signs in at once.
	f.clk.Advance(OTPWindow)
	requireLocked(t, newAdminCodes(f, adminPhone).wrong(defaultMaxFailures), defaultLockFor, "locked")
	lockedAt := f.clk.Now().UTC()
	res, err = unlock(adminPhone, "every super admin locked out")
	require.NoError(t, err)
	require.NotNil(t, res.LockedUntil)
	require.Equal(t, lockedAt.Add(defaultLockFor), res.LockedUntil.UTC())
	require.Zero(t, f.locks(adminPhone))
	adminSignIn(f, adminPhone)

	require.Equal(t, 2, f.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.unlock_sign_in' AND actor_kind = 'system'
		AND actor_id IS NULL AND subject_type = 'admin_user' AND subject_id = $1 AND after ->> 'via' = 'break_glass'`, p.UserID.String()))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.unlock_sign_in'
		AND after ->> 'reason' = 'every super admin locked out' AND before ->> 'signInLockedUntil' IS NOT NULL`))
	require.Zero(t, f.count(`SELECT count(*) FROM audit_log WHERE before::text LIKE '%9876543%' OR after::text LIKE '%9876543%'`))

	// A disabled admin's lock is lifted too; sign-in still refuses them.
	f.owner(`UPDATE admin_users SET disabled_at = now() WHERE user_id = $1`, ids.UUID(p.UserID))
	f.clk.Advance(OTPWindow)
	requireLocked(t, newAdminCodes(f, adminPhone).wrong(defaultMaxFailures), defaultLockFor, "a disabled admin's number locks")
	res, err = unlock(adminPhone, "clean up before re-enabling")
	require.NoError(t, err)
	require.NotNil(t, res.LockedUntil)
	require.Zero(t, f.locks(adminPhone))
}

package identity

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/ids"
)

// adminPhone is the admin of these tests; it is a customer first, since only
// an existing user becomes an admin (admin_users).
const adminPhone = "+919876543210"

// newAdmin makes phone a support+finance admin and signs them in to the
// admin app. Sends: one customer sign-in and one admin sign-in.
func newAdmin(f *fixture, phone string) (SignIn, authn.Principal) {
	f.t.Helper()
	cust := f.signIn(phone, authn.AppCustomer)
	f.owner(`INSERT INTO admin_users (user_id, roles) VALUES ($1, ARRAY['support','finance'])`, ids.UUID(cust.UserID))
	return adminSignIn(f, phone)
}

// adminSignIn signs phone in to the admin app with the SMS OTP alone
// (D-264): the token is amr otp and carries no step-up.
func adminSignIn(f *fixture, phone string) (SignIn, authn.Principal) {
	f.t.Helper()
	in := f.signIn(phone, authn.AppAdmin)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(f.t, err)
	require.Equal(f.t, authn.AMROTP, p.AMR)
	return in, p
}

// stepUpAt is what the authenticator puts in the principal: the sign-in's
// last SMS step-up (CheckSession), zero when there is none.
func (f *fixture) stepUpAt(p authn.Principal) time.Time {
	f.t.Helper()
	st, err := f.svc.CheckSession(context.Background(), p.UserID, p.SessionID, authn.AppAdmin)
	require.NoError(f.t, err)
	require.True(f.t, st.Active)
	if st.StepUpAt.IsZero() {
		return time.Time{}
	}
	return st.StepUpAt.UTC()
}

// sendStepUp sends p a step-up code and returns the request and the code the
// SMS carried. It waits out the resend gap afterwards.
func (f *fixture) sendStepUp(p authn.Principal, phone string) (string, string) {
	f.t.Helper()
	res, err := f.svc.SendStepUp(f.ctx("10.0.0.1"), p)
	require.NoError(f.t, err)
	require.Equal(f.t, OTPResendGap, res.ResendAfter)
	code := f.sms.code(phone)
	require.NotEmpty(f.t, code)
	f.remember(code)
	f.clk.Advance(OTPResendGap)
	return res.RequestID.String(), code
}

// stepUp passes the SMS step-up on p and returns p as the authenticator
// would then see it.
func (f *fixture) stepUp(p authn.Principal, phone string) authn.Principal {
	f.t.Helper()
	reqID, code := f.sendStepUp(p, phone)
	until, err := f.svc.VerifyStepUp(f.ctx("10.0.0.1"), p, reqID, code)
	require.NoError(f.t, err)
	require.Equal(f.t, f.clk.Now().UTC().Add(authn.StepUpWindow), until.UTC())
	p.StepUpAt = f.stepUpAt(p)
	return p
}

// otherCode is a well-formed code that is not code.
func otherCode(code string) string {
	if code == "111111" {
		return "222222"
	}
	return "111111"
}

// D-264: the SMS OTP alone signs an admin in. The sign-in is a full admin
// session for reads, with no step-up, so a † action asks for one; the app
// itself never sends an admin step_up code through the sign-in send.
func TestAdminSignInWithoutTOTP(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in, p := newAdmin(f, adminPhone)
	require.NotEmpty(t, in.AccessToken)
	require.NotEmpty(t, in.RefreshToken)
	require.Equal(t, []string{"support", "finance"}, p.Roles)
	require.True(t, f.stepUpAt(p).IsZero(), "a sign-in is not a step-up")

	gate := authz.NewAdminGate(f.clk)
	ctx := authn.WithPrincipal(context.Background(), p)
	_, err := gate.Require(ctx, []authz.Role{authz.Support}, false)
	require.NoError(t, err, "a read needs the sign-in only")
	_, err = gate.Require(ctx, []authz.Role{authz.Finance}, true)
	requireCode(t, err, genproblems.StepUpRequired, "a † action needs the SMS step-up")

	_, err = f.svc.SendOTP(f.ctx("10.0.0.1"), authn.AppAdmin, adminPhone, PurposeStepUp, "tok")
	requireCode(t, err, genproblems.ValidationFailed, "admin step-up codes come from /v1/auth/step-up/send only")
}

// D-264, AU-12: the SMS step-up. Send goes to the admin's own number; verify
// checks the code like a sign-in code (5 minutes, 5 tries, once) and bound
// to this admin's admin-app step_up request.
func TestAdminSMSStepUp(t *testing.T) {
	const otherPhone = "+919876543211"
	type world struct {
		f     *fixture
		p     authn.Principal // the admin under test, signed in
		other authn.Principal // a second admin, signed in
	}
	setup := func(t *testing.T) world {
		f := newFixture(t, config.EnvTest)
		_, p := newAdmin(f, adminPhone)
		_, o := newAdmin(f, otherPhone)
		return world{f: f, p: p, other: o}
	}

	cases := []struct {
		name string
		run  func(t *testing.T, w world)
	}{
		{"good code", func(t *testing.T, w world) {
			f := w.f
			reqID, code := f.sendStepUp(w.p, adminPhone)
			until, err := f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.p, reqID, code)
			require.NoError(t, err)
			require.Equal(t, f.clk.Now().UTC().Add(authn.StepUpWindow), until.UTC())
			require.Equal(t, f.clk.Now().UTC(), f.stepUpAt(w.p))
			require.True(t, f.stepUpAt(w.other).IsZero(), "only the caller's sign-in steps up")
			require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.step_up_verified'`))
			require.Equal(t, 1, f.count(`SELECT count(*) FROM otp_requests WHERE id = $1 AND consumed_at IS NOT NULL`, ids.UUID(mustParse(t, reqID))))

			_, err = f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.p, reqID, code)
			requireCode(t, err, genproblems.OtpExpired, "a code works once")
		}},
		{"wrong code", func(t *testing.T, w world) {
			f := w.f
			reqID, code := f.sendStepUp(w.p, adminPhone)
			_, err := f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.p, reqID, otherCode(code))
			requireCode(t, err, genproblems.CodeInvalid)
			require.Equal(t, OTPMaxAttempts-1, problemOf(t, err).Extensions["attemptsLeft"])
			require.True(t, f.stepUpAt(w.p).IsZero())
			_, err = f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.p, reqID, code)
			require.NoError(t, err, "the right code still works after a miss")
		}},
		{"malformed request", func(t *testing.T, w world) {
			_, err := w.f.svc.VerifyStepUp(w.f.ctx("10.0.0.1"), w.p, "not-a-uuid", "123456")
			requireCode(t, err, genproblems.OtpExpired)
			_, err = w.f.svc.VerifyStepUp(w.f.ctx("10.0.0.1"), w.p, ids.New(t0).String(), "123456")
			requireCode(t, err, genproblems.OtpExpired, "an unknown request")
		}},
		{"expired", func(t *testing.T, w world) {
			f := w.f
			reqID, code := f.sendStepUp(w.p, adminPhone)
			f.clk.Advance(OTPTTL - OTPResendGap)
			_, err := f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.p, reqID, code)
			requireCode(t, err, genproblems.OtpExpired)
			require.True(t, f.stepUpAt(w.p).IsZero())
		}},
		{"attempts exhausted", func(t *testing.T, w world) {
			f := w.f
			cust := f.signIn(adminPhone, authn.AppCustomer) // another app of the same user
			reqID, code := f.sendStepUp(w.p, adminPhone)
			var err error
			for i := 1; i < OTPMaxAttempts; i++ {
				_, err = f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.p, reqID, otherCode(code))
				requireCode(t, err, genproblems.CodeInvalid, i)
				require.Equal(t, OTPMaxAttempts-i, problemOf(t, err).Extensions["attemptsLeft"], i)
			}
			_, err = f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.p, reqID, otherCode(code))
			requireCode(t, err, genproblems.OtpLocked, "the fifth miss locks")
			require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND app = 'admin' AND revoked_at IS NULL`, ids.UUID(w.p.UserID)),
				"every admin session of the admin ends")
			require.Equal(t, 1, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND app = 'admin' AND revoked_at IS NULL`, ids.UUID(w.other.UserID)),
				"another admin keeps theirs")
			require.NotZero(t, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND app = 'customer' AND revoked_at IS NULL`, ids.UUID(cust.UserID)),
				"other apps stay")
			require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.step_up_locked'`))

			_, err = f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.p, reqID, code)
			requireCode(t, err, genproblems.OtpLocked, "the right code no longer counts")
			st, err := f.svc.CheckSession(context.Background(), w.p.UserID, w.p.SessionID, authn.AppAdmin)
			require.NoError(t, err)
			require.False(t, st.Active)
			reqID, code = f.sendStepUp(w.p, adminPhone)
			_, err = f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.p, reqID, code)
			requireCode(t, err, genproblems.Unauthorized, "the ended sign-in cannot step up")
		}},
		{"rate limited", func(t *testing.T, w world) {
			f := w.f
			// D-264: step-up sends have their own bucket (TestAdminStepUpQuota
			// has the full table): the resend gap holds among step-up codes.
			res, err := f.svc.SendStepUp(f.ctx("10.0.0.1"), w.p)
			require.NoError(t, err)
			f.remember(f.sms.code(adminPhone))
			_, err = f.svc.SendStepUp(f.ctx("10.0.0.1"), w.p)
			require.Positive(t, retryAfter(t, err), "the resend gap")
			_, err = f.send(adminPhone, authn.AppAdmin, "10.0.0.2")
			require.NoError(t, err, "a sign-in send is not held up by a step-up")
			f.remember(f.sms.code(adminPhone))
			require.NotEqual(t, ids.Zero, res.RequestID)
		}},
		{"another admin's code", func(t *testing.T, w world) {
			f := w.f
			reqID, code := f.sendStepUp(w.other, otherPhone)
			_, err := f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.p, reqID, code)
			requireCode(t, err, genproblems.OtpExpired, "a code is bound to its admin's number")
			require.True(t, f.stepUpAt(w.p).IsZero())
			require.True(t, f.stepUpAt(w.other).IsZero())
			require.Equal(t, 1, f.count(`SELECT count(*) FROM otp_requests WHERE id = $1 AND consumed_at IS NULL AND attempts = 0`, ids.UUID(mustParse(t, reqID))),
				"the other admin's code is untouched")
			_, err = f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.other, reqID, code)
			require.NoError(t, err, "and still works for its owner")
		}},
		{"a customer step_up code", func(t *testing.T, w world) {
			f := w.f
			reqID, code := stepUpCode(f, adminPhone) // the account-deletion step-up of the same number
			_, err := f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.p, reqID, code)
			requireCode(t, err, genproblems.OtpExpired, "only an admin-app step_up request counts")
		}},
		{"not an admin sign-in", func(t *testing.T, w world) {
			f := w.f
			cust := f.signIn(adminPhone, authn.AppCustomer)
			cp, err := f.svc.tokens.Verify(cust.AccessToken)
			require.NoError(t, err)
			_, err = f.svc.SendStepUp(f.ctx("10.0.0.1"), cp)
			requireCode(t, err, genproblems.Forbidden, "a customer sign-in")
			_, err = f.svc.VerifyStepUp(f.ctx("10.0.0.1"), cp, ids.New(t0).String(), "123456")
			requireCode(t, err, genproblems.Forbidden)
			_, err = f.svc.SendStepUp(f.ctx("10.0.0.1"), authn.Principal{})
			requireCode(t, err, genproblems.Unauthorized, "anonymous")
			_, err = f.svc.VerifyStepUp(f.ctx("10.0.0.1"), authn.Principal{}, ids.New(t0).String(), "123456")
			requireCode(t, err, genproblems.Unauthorized)
		}},
		{"disabled admin", func(t *testing.T, w world) {
			f := w.f
			reqID, code := f.sendStepUp(w.p, adminPhone)
			f.owner(`UPDATE admin_users SET disabled_at = now() WHERE user_id = $1`, ids.UUID(w.p.UserID))
			_, err := f.svc.SendStepUp(f.ctx("10.0.0.1"), w.p)
			requireCode(t, err, genproblems.Forbidden)
			_, err = f.svc.VerifyStepUp(f.ctx("10.0.0.1"), w.p, reqID, code)
			requireCode(t, err, genproblems.Forbidden)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, setup(t)) })
	}
}

// AU-12, D-264: a † action needs an SMS step-up younger than the window;
// past it the gate answers step_up_required until the admin steps up again.
// A refresh keeps the step-up; a new sign-in does not.
func TestAdminStepUpWindow(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in, p := newAdmin(f, adminPhone)
	gate := authz.NewAdminGate(f.clk)
	require.Equal(t, 10*time.Minute, authn.StepUpWindow)
	money := func(p authn.Principal) error {
		_, err := gate.Require(authn.WithPrincipal(context.Background(), p), []authz.Role{authz.Finance}, true)
		return err
	}
	read := func(p authn.Principal) error {
		_, err := gate.Require(authn.WithPrincipal(context.Background(), p), []authz.Role{authz.Support}, false)
		return err
	}
	requireCode(t, money(p), genproblems.StepUpRequired)

	p = f.stepUp(p, adminPhone)
	require.NoError(t, money(p))

	// A refresh copies the step-up to the next row.
	r, err := refreshIn(f, authn.AppAdmin, in.RefreshToken)
	require.NoError(t, err)
	rp, err := f.svc.tokens.Verify(r.AccessToken)
	require.NoError(t, err)
	rp.StepUpAt = f.stepUpAt(rp)
	require.Equal(t, p.StepUpAt, rp.StepUpAt)
	require.NoError(t, money(rp))

	f.clk.Advance(authn.StepUpWindow - time.Second)
	require.NoError(t, money(rp), "inside the window")
	f.clk.Advance(2 * time.Second)
	requireCode(t, money(rp), genproblems.StepUpRequired, "stale after the window")
	require.NoError(t, read(rp), "a plain read needs no step-up")

	rp = f.stepUp(rp, adminPhone)
	require.NoError(t, money(rp))

	// A second sign-in of the same admin starts without one.
	f.clk.Advance(OTPWindow)
	_, p2 := adminSignIn(f, adminPhone)
	p2.StepUpAt = f.stepUpAt(p2)
	requireCode(t, money(p2), genproblems.StepUpRequired)
}

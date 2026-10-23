package identity

import (
	"bytes"
	"context"
	"fmt"
	"strings"
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

// newAdmin makes adminPhone a support+finance admin and signs them in to the
// admin app with OTP only.
func newAdmin(f *fixture) (SignIn, authn.Principal) {
	f.t.Helper()
	cust := f.signIn(adminPhone, authn.AppCustomer)
	f.owner(`INSERT INTO admin_users (user_id, roles) VALUES ($1, ARRAY['support','finance'])`, ids.UUID(cust.UserID))
	return adminSignIn(f)
}

// adminSignIn signs adminPhone in to the admin app (OTP only).
func adminSignIn(f *fixture) (SignIn, authn.Principal) {
	f.t.Helper()
	in := f.signIn(adminPhone, authn.AppAdmin)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(f.t, err)
	require.Equal(f.t, authn.AMROTP, p.AMR)
	return in, p
}

// enrolled is an admin who finished TOTP enrolment.
type enrolled struct {
	in       SignIn
	otp      authn.Principal // the OTP-only principal of the sign-in
	totp     authn.Principal // the otp+totp principal after verify
	secret   []byte
	recovery []string
}

func enrolAdmin(f *fixture) enrolled {
	f.t.Helper()
	in, p := newAdmin(f)
	require.Equal(f.t, NextStepTOTPEnrol, in.NextStep)
	ctx := f.ctx("10.0.0.1")
	enr, err := f.svc.EnrolTOTP(ctx, p)
	require.NoError(f.t, err)
	f.remember(enr.Secret)
	f.remember(enr.RecoveryCodes...)
	secret, err := DecodeTOTPSecret(enr.Secret)
	require.NoError(f.t, err)
	res := f.totp(p, secret, TOTPPurposeLogin)
	tp, err := f.svc.tokens.Verify(res.AccessToken)
	require.NoError(f.t, err)
	tp.StepUpAt = f.stepUpAt(tp)
	return enrolled{in: in, otp: p, totp: tp, secret: secret, recovery: enr.RecoveryCodes}
}

// totp verifies the current code for p and returns the result.
func (f *fixture) totp(p authn.Principal, secret []byte, purpose string) TOTPResult {
	f.t.Helper()
	code := TOTPCode(secret, f.clk.Now())
	f.remember(code)
	res, err := f.svc.VerifyTOTP(f.ctx("10.0.0.1"), p, code, purpose)
	require.NoError(f.t, err)
	f.remember(res.AccessToken)
	return res
}

// stepUpAt is what the authenticator puts in the principal: the sign-in's
// last TOTP verification (CheckSession).
func (f *fixture) stepUpAt(p authn.Principal) time.Time {
	f.t.Helper()
	st, err := f.svc.CheckSession(context.Background(), p.UserID, p.SessionID, authn.AppAdmin)
	require.NoError(f.t, err)
	require.True(f.t, st.Active)
	return st.StepUpAt.UTC()
}

// wrongCode is a well-formed code that matches no step around now.
func wrongCode(secret []byte, now time.Time) string {
	valid := map[string]bool{}
	for d := -TOTPSkew - 1; d <= TOTPSkew+1; d++ {
		valid[TOTPCode(secret, now.Add(time.Duration(d)*TOTPPeriod))] = true
	}
	for i := 0; ; i++ {
		c := fmt.Sprintf("%06d", i)
		if !valid[c] {
			return c
		}
	}
}

// AU-11: an admin needs OTP and TOTP. The OTP-only token says what to do
// next; enrolment shows the secret and 10 recovery codes once, stores both
// sealed or hashed, and the first code turns the sign-in into otp+totp.
func TestAdminTOTPEnrolAndVerify(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in, p := newAdmin(f)
	require.Equal(t, NextStepTOTPEnrol, in.NextStep)
	ctx := f.ctx("10.0.0.1")

	_, err := f.svc.VerifyTOTP(ctx, p, "123456", TOTPPurposeLogin)
	requireCode(t, err, genproblems.Conflict, "verify before enrol")

	enr, err := f.svc.EnrolTOTP(ctx, p)
	require.NoError(t, err)
	f.remember(enr.Secret)
	f.remember(enr.RecoveryCodes...)
	require.Len(t, enr.RecoveryCodes, 10)
	require.True(t, strings.HasPrefix(enr.URI, "otpauth://totp/"))
	require.NotContains(t, enr.URI, adminPhone)
	secret, err := DecodeTOTPSecret(enr.Secret)
	require.NoError(t, err)

	// Stored sealed and hashed, never in the clear.
	var sealed []byte
	require.NoError(t, f.db.OwnerPool.QueryRow(context.Background(),
		`SELECT totp_secret_enc FROM admin_users WHERE user_id = $1`, ids.UUID(p.UserID)).Scan(&sealed))
	require.False(t, bytes.Contains(sealed, secret))
	require.False(t, bytes.Contains(sealed, []byte(enr.Secret)))
	rows, err := f.db.OwnerPool.Query(context.Background(), `SELECT code_hash FROM admin_recovery_codes WHERE admin_id = $1`, ids.UUID(p.UserID))
	require.NoError(t, err)
	var hashes [][]byte
	for rows.Next() {
		var h []byte
		require.NoError(t, rows.Scan(&h))
		hashes = append(hashes, h)
	}
	require.NoError(t, rows.Err())
	require.Len(t, hashes, 10)
	for _, h := range hashes {
		for _, c := range enr.RecoveryCodes {
			require.False(t, bytes.Contains(h, []byte(c)))
			require.False(t, bytes.Contains(h, []byte(strings.ReplaceAll(c, "-", ""))))
		}
	}

	// Pending: recovery needs a confirmed authenticator; a restart replaces
	// the secret and the codes.
	_, err = f.svc.RecoverTOTP(ctx, p, enr.RecoveryCodes[0])
	requireCode(t, err, genproblems.Conflict)
	again, err := f.svc.EnrolTOTP(ctx, p)
	require.NoError(t, err)
	f.remember(again.Secret)
	f.remember(again.RecoveryCodes...)
	require.NotEqual(t, enr.Secret, again.Secret)
	require.Equal(t, 10, f.count(`SELECT count(*) FROM admin_recovery_codes WHERE admin_id = $1`, ids.UUID(p.UserID)))
	_, err = f.svc.VerifyTOTP(ctx, p, TOTPCode(secret, f.clk.Now()), TOTPPurposeLogin)
	requireCode(t, err, genproblems.CodeInvalid, "the replaced secret no longer works")
	secret, err = DecodeTOTPSecret(again.Secret)
	require.NoError(t, err)

	// Step-up never finishes a sign-in.
	_, err = f.svc.VerifyTOTP(ctx, p, TOTPCode(secret, f.clk.Now()), TOTPPurposeStepUp)
	requireCode(t, err, genproblems.Forbidden)
	_, err = f.svc.VerifyTOTP(ctx, p, TOTPCode(secret, f.clk.Now()), "other")
	requireCode(t, err, genproblems.ValidationFailed)

	res := f.totp(p, secret, TOTPPurposeLogin)
	tp, err := f.svc.tokens.Verify(res.AccessToken)
	require.NoError(t, err)
	require.Equal(t, authn.AMROTPTOTP, tp.AMR)
	require.Equal(t, p.SessionID, tp.SessionID, "the same sign-in, elevated")
	require.Equal(t, []string{"support", "finance"}, tp.Roles)
	require.Equal(t, f.clk.Now().UTC(), f.stepUpAt(tp))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.totp_enrolled'`))
	require.Equal(t, 2, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.totp_enrol_started'`))

	// The same code twice is a replay.
	code := TOTPCode(secret, f.clk.Now())
	_, err = f.svc.VerifyTOTP(ctx, p, code, TOTPPurposeLogin)
	requireCode(t, err, genproblems.CodeInvalid, "replay")

	// Enrolled: no self-service re-enrolment.
	_, err = f.svc.EnrolTOTP(ctx, tp)
	requireCode(t, err, genproblems.Conflict)

	// A refresh keeps otp+totp; the next sign-in starts at totp_verify.
	r, err := refreshIn(f, authn.AppAdmin, in.RefreshToken)
	require.NoError(t, err)
	rp, err := f.svc.tokens.Verify(r.AccessToken)
	require.NoError(t, err)
	require.Equal(t, authn.AMROTPTOTP, rp.AMR)
	next, np := adminSignIn(f)
	require.Equal(t, NextStepTOTPVerify, next.NextStep)
	require.True(t, f.stepUpAt(np).IsZero(), "a new sign-in starts without TOTP")

	// Only an admin-app session reaches the TOTP flows.
	cust := f.signIn(adminPhone, authn.AppCustomer)
	cp, err := f.svc.tokens.Verify(cust.AccessToken)
	require.NoError(t, err)
	_, err = f.svc.EnrolTOTP(ctx, cp)
	requireCode(t, err, genproblems.Forbidden)
	_, err = f.svc.VerifyTOTP(ctx, authn.Principal{}, "123456", TOTPPurposeLogin)
	requireCode(t, err, genproblems.Unauthorized)
}

// AU-12: a † action needs a TOTP verification younger than the step-up
// window; past it the gate answers step_up_required until the admin verifies
// again with purpose step_up.
func TestAdminStepUp(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	a := enrolAdmin(f)
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
	require.NoError(t, money(a.totp))

	f.clk.Advance(authn.StepUpWindow + time.Second)
	a.totp.StepUpAt = f.stepUpAt(a.totp)
	requireCode(t, money(a.totp), genproblems.StepUpRequired, "stale after the window")
	require.NoError(t, read(a.totp), "a plain read needs no step-up")

	res := f.totp(a.totp, a.secret, TOTPPurposeStepUp)
	sp, err := f.svc.tokens.Verify(res.AccessToken)
	require.NoError(t, err)
	require.Equal(t, authn.AMROTPTOTP, sp.AMR)
	sp.StepUpAt = f.stepUpAt(sp)
	require.Equal(t, f.clk.Now().UTC(), sp.StepUpAt)
	require.NoError(t, money(sp))

	// An OTP-only token of the same admin never passes the gate, and cannot
	// step up.
	requireCode(t, read(a.otp), genproblems.Forbidden)
	f.clk.Advance(TOTPPeriod)
	_, err = f.svc.VerifyTOTP(f.ctx("10.0.0.1"), a.otp, TOTPCode(a.secret, f.clk.Now()), TOTPPurposeStepUp)
	requireCode(t, err, genproblems.Forbidden)
}

// D-60: each recovery code works once, in any case and with or without its
// dash, and stands in for the TOTP code of a sign-in.
func TestAdminRecoveryCodes(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	a := enrolAdmin(f)
	_, p := adminSignIn(f)
	ctx := f.ctx("10.0.0.1")

	typed := strings.ToLower(strings.ReplaceAll(a.recovery[0], "-", ""))
	res, err := f.svc.RecoverTOTP(ctx, p, typed)
	require.NoError(t, err)
	f.remember(res.AccessToken)
	require.Equal(t, 9, res.RecoveryCodesLeft)
	rp, err := f.svc.tokens.Verify(res.AccessToken)
	require.NoError(t, err)
	require.Equal(t, authn.AMROTPTOTP, rp.AMR)
	require.Equal(t, f.clk.Now().UTC(), f.stepUpAt(rp))

	_, p2 := adminSignIn(f)
	_, err = f.svc.RecoverTOTP(ctx, p2, a.recovery[0])
	requireCode(t, err, genproblems.CodeInvalid, "used once")
	_, err = f.svc.RecoverTOTP(ctx, p2, "not-a-code")
	requireCode(t, err, genproblems.CodeInvalid)
	res, err = f.svc.RecoverTOTP(ctx, p2, a.recovery[1])
	require.NoError(t, err)
	f.remember(res.AccessToken)
	require.Equal(t, 8, res.RecoveryCodesLeft)
	require.Equal(t, 2, f.count(`SELECT count(*) FROM admin_recovery_codes WHERE used_at IS NOT NULL`))
	require.Equal(t, 2, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.totp_recovered'`))
}

// Wrong TOTP or recovery codes count across sign-ins; a success resets the
// count, and at the limit every admin session of the admin ends.
func TestAdminTOTPLockout(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	a := enrolAdmin(f)
	_, p := adminSignIn(f)
	ctx := f.ctx("10.0.0.1")
	bad := func(p authn.Principal) error {
		_, err := f.svc.VerifyTOTP(ctx, p, wrongCode(a.secret, f.clk.Now()), TOTPPurposeLogin)
		return err
	}
	attemptsLeft := func(err error) any {
		t.Helper()
		requireCode(t, err, genproblems.CodeInvalid)
		return problemOf(t, err).Extensions["attemptsLeft"]
	}

	require.Equal(t, TOTPMaxFailures-1, attemptsLeft(bad(p)))
	f.clk.Advance(TOTPPeriod)
	f.totp(p, a.secret, TOTPPurposeLogin)
	require.Equal(t, TOTPMaxFailures-1, attemptsLeft(bad(p)), "a success resets the count")

	for i := 2; i < TOTPMaxFailures; i++ {
		require.Equal(t, TOTPMaxFailures-i, attemptsLeft(bad(a.otp)), i)
	}
	_, err := f.svc.RecoverTOTP(ctx, p, "AAAAA-AAAAA")
	requireCode(t, err, genproblems.OtpLocked, "recovery codes count too")
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE app = 'admin' AND revoked_at IS NULL`))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM sessions WHERE app = 'customer' AND revoked_at IS NULL`), "other apps stay")
	require.Zero(t, f.count(`SELECT totp_failures FROM admin_users`))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.totp_locked'`))

	f.clk.Advance(TOTPPeriod)
	_, err = f.svc.VerifyTOTP(ctx, p, TOTPCode(a.secret, f.clk.Now()), TOTPPurposeLogin)
	requireCode(t, err, genproblems.Unauthorized, "the ended sign-in cannot be elevated")
	_, np := adminSignIn(f)
	f.totp(np, a.secret, TOTPPurposeLogin)
}

// The TOTP flows of one admin run one at a time (row lock): concurrent
// verifies of the same code succeed at most once.
func TestAdminTOTPConcurrentReplay(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	a := enrolAdmin(f)
	_, p := adminSignIn(f)
	f.clk.Advance(TOTPPeriod)
	code := TOTPCode(a.secret, f.clk.Now())
	f.remember(code)
	const n = 5
	errs := make(chan error, n)
	for range n {
		go func() {
			res, err := f.svc.VerifyTOTP(f.ctx("10.0.0.1"), p, code, TOTPPurposeLogin)
			f.remember(res.AccessToken)
			errs <- err
		}()
	}
	ok := 0
	for range n {
		err := <-errs
		if err == nil {
			ok++
			continue
		}
		requireCode(t, err, genproblems.CodeInvalid)
	}
	require.Equal(t, 1, ok)
}

// A TOTP route without the key configured is 503, not a crash.
func TestTOTPUnconfigured(t *testing.T) {
	f := newFixtureWith(t, config.EnvTest, func(c *config.Config, _ *Deps) { c.Keys.TOTPEncKey = config.Secret{} })
	ctx := f.ctx("10.0.0.1")
	_, err := f.svc.EnrolTOTP(ctx, authn.Principal{})
	requireCode(t, err, genproblems.ServiceUnavailable)
	_, err = f.svc.VerifyTOTP(ctx, authn.Principal{}, "123456", TOTPPurposeLogin)
	requireCode(t, err, genproblems.ServiceUnavailable)
	_, err = f.svc.RecoverTOTP(ctx, authn.Principal{}, "AAAAA-AAAAA")
	requireCode(t, err, genproblems.ServiceUnavailable)
}

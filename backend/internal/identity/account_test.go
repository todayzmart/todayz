package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// AU-13: logout-all ends every sign-in of the user in the token's app, and
// only there; it needs a live token.
func TestLogoutAll(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const phone = "+919876543210"
	a := f.signIn(phone, authn.AppCustomer)
	b := f.signIn(phone, authn.AppCustomer) // a second device
	biz := f.signIn(phone, authn.AppBusiness)
	other := f.signIn("+919876543211", authn.AppCustomer) // another user
	ctx := f.ctx("10.0.0.1")

	_, err := f.svc.LogoutAll(ctx, RefreshInput{App: authn.AppCustomer})
	requireCode(t, err, genproblems.Unauthorized, "no cookie")
	_, err = f.svc.LogoutAll(ctx, RefreshInput{App: authn.AppCustomer, Token: "junk"})
	requireCode(t, err, genproblems.Unauthorized)
	_, err = f.svc.LogoutAll(ctx, RefreshInput{App: authn.AppCustomer, Token: biz.RefreshToken})
	requireCode(t, err, genproblems.Unauthorized, "another app's token")

	// A token rotated a moment ago (another tab) is a grace retry: refused,
	// nothing ended.
	r, err := refreshIn(f, authn.AppCustomer, a.RefreshToken)
	require.NoError(t, err)
	f.remember(r.AccessToken, r.RefreshToken)
	_, err = f.svc.LogoutAll(ctx, RefreshInput{App: authn.AppCustomer, Token: a.RefreshToken})
	requireCode(t, err, genproblems.Unauthorized)
	require.True(t, errors.As(err, new(graceRetry)), "within the grace window")
	f.clk.Advance(RotationGrace + 1)
	_, err = f.svc.LogoutAll(ctx, RefreshInput{App: authn.AppCustomer, Token: a.RefreshToken})
	requireCode(t, err, genproblems.Unauthorized, "an old token cannot sign everyone out")
	require.False(t, errors.As(err, new(graceRetry)))
	require.Equal(t, 4, f.count(`SELECT count(*) FROM sessions WHERE revoked_at IS NULL`))

	n, err := f.svc.LogoutAll(ctx, RefreshInput{App: authn.AppCustomer, Token: r.RefreshToken})
	require.NoError(t, err)
	require.EqualValues(t, 2, n)
	_, err = refreshIn(f, authn.AppCustomer, b.RefreshToken)
	requireCode(t, err, genproblems.Unauthorized, "the other device is out")
	_, err = refreshIn(f, authn.AppBusiness, biz.RefreshToken)
	require.NoError(t, err, "the business app stays signed in")
	_, err = refreshIn(f, authn.AppCustomer, other.RefreshToken)
	require.NoError(t, err, "another user is untouched")
	require.Equal(t, 2, f.count(`SELECT count(*) FROM sessions WHERE revoked_reason = 'logout_all'`))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.sign_out_all'`))

	_, err = f.svc.LogoutAll(ctx, RefreshInput{App: authn.AppCustomer, Token: r.RefreshToken})
	requireCode(t, err, genproblems.Unauthorized, "twice")
}

// stepUpCode asks for a step_up OTP to phone and returns its request id and
// code.
func stepUpCode(f *fixture, phone string) (string, string) {
	f.t.Helper()
	res, err := f.svc.SendOTP(f.ctx("10.0.0.1"), authn.AppCustomer, phone, PurposeStepUp, "tok")
	require.NoError(f.t, err)
	code := f.sms.code(phone)
	f.remember(code)
	f.clk.Advance(OTPResendGap)
	return res.RequestID.String(), code
}

// AU-18: DELETE /v1/me with a fresh step_up OTP. The checks may refuse
// (nothing changes, the code stays usable); the hooks run in the deletion's
// transaction; identity anonymises the user and ends every sign-in.
func TestDeleteAccount(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const phone = "+919876543210"
	in := f.signIn(phone, authn.AppCustomer)
	f.signIn(phone, authn.AppBusiness)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)
	uid := ids.UUID(in.UserID)
	f.owner(`UPDATE users SET name = 'Asha' WHERE id = $1`, uid)
	f.owner(`INSERT INTO addresses (id, user_id, label, line1, pincode, location)
		VALUES ($1, $2, 'Home', '1 MG Road', '560001', 'SRID=4326;POINT(77.6 12.97)')`, ids.UUID(ids.New(t0)), uid)
	f.owner(`UPDATE devices SET push_endpoint = 'https://push.example/1', push_keys = '\x01' WHERE user_id = $1`, uid)
	ctx := f.ctx("10.0.0.1")
	deviceKeys := f.strings(`SELECT device_key FROM devices WHERE user_id = $1`, uid)
	require.NotEmpty(t, deviceKeys)
	require.Equal(t, 2, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND user_agent IS NOT NULL AND ip_hash IS NOT NULL`, uid))
	hookDB := db.New(f.db.Pool)

	var (
		refuse   error
		checked  []ids.ID
		ran      []ids.ID
		hookFail error
	)
	f.svc.AddDeletionCheck(CanDeleteFunc(func(_ context.Context, id ids.ID) error {
		checked = append(checked, id)
		return refuse
	}))
	f.svc.AddDeletionHook(OnDeleteFunc(func(ctx context.Context, id ids.ID) error {
		ran = append(ran, id)
		// Inside the deletion's transaction: the user row identity locked is
		// ours to lock again, and a write here commits or rolls back with it.
		var status string
		if err := hookDB.Q(ctx).QueryRow(ctx, `SELECT status FROM users WHERE id = $1 FOR UPDATE NOWAIT`, ids.UUID(id)).Scan(&status); err != nil {
			return err
		}
		if _, err := hookDB.Q(ctx).Exec(ctx, `UPDATE users SET preferred_lang = 'hi' WHERE id = $1`, ids.UUID(id)); err != nil {
			return err
		}
		return hookFail
	}))
	untouched := func(msg string) {
		t.Helper()
		require.Equal(t, 1, f.count(`SELECT count(*) FROM users WHERE id = $1 AND status = 'active' AND name = 'Asha' AND preferred_lang = 'en'`, uid), msg)
		require.Equal(t, 1, f.count(`SELECT count(*) FROM phone_identities WHERE user_id = $1`, uid), msg)
		require.Equal(t, 1, f.count(`SELECT count(*) FROM addresses WHERE user_id = $1`, uid), msg)
		require.Equal(t, 2, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, uid), msg)
	}

	// Who may ask.
	requireCode(t, f.svc.DeleteAccount(ctx, authn.Principal{}, DeleteInput{}), genproblems.Unauthorized)
	bp := p
	bp.App = authn.AppBusiness
	requireCode(t, f.svc.DeleteAccount(ctx, bp, DeleteInput{}), genproblems.Forbidden)
	requireCode(t, f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: "x", Code: "000000"}), genproblems.OtpExpired)

	// A sign-in code is not a deletion code.
	login, err := f.send(phone, authn.AppCustomer, "10.0.0.1")
	require.NoError(t, err)
	f.remember(f.sms.code(phone))
	f.clk.Advance(OTPResendGap)
	requireCode(t, f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: login.RequestID.String(), Code: f.sms.code(phone)}), genproblems.OtpExpired)

	reqID, code := stepUpCode(f, phone)
	err = f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: reqID, Code: "999999"})
	if code == "999999" {
		t.Skip("the random code happened to be the wrong one")
	}
	requireCode(t, err, genproblems.CodeInvalid)
	require.Equal(t, OTPMaxAttempts-1, problemOf(t, err).Extensions["attemptsLeft"])
	untouched("a wrong code")
	require.Empty(t, checked, "the checks run after the code")

	// A refusing check: answered as is, nothing changes, the code stays.
	refuse = problem.New(genproblems.Conflict, "an open order")
	requireCode(t, f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: reqID, Code: code}), genproblems.Conflict)
	untouched("a refused deletion")
	require.Equal(t, []ids.ID{in.UserID}, checked)
	require.Empty(t, ran, "no hook runs after a refusal")
	require.Equal(t, 1, f.count(`SELECT count(*) FROM otp_requests WHERE id = $1 AND consumed_at IS NULL`, ids.UUID(mustParse(t, reqID))))
	// L10a: the right code refused by a check is not a try; only the wrong
	// one above counted. Five refusals in a row still leave the code usable.
	for range OTPMaxAttempts {
		requireCode(t, f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: reqID, Code: code}), genproblems.Conflict)
	}
	require.Equal(t, 1, f.count(`SELECT attempts FROM otp_requests WHERE id = $1`, ids.UUID(mustParse(t, reqID))), "a refusal does not use a try")

	// A check that fails (not a refusal) is a 500.
	refuse = errors.New("orders unreachable")
	requireCode(t, f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: reqID, Code: code}), genproblems.Internal)
	untouched("a failed check")

	// A failing hook rolls everything back, its own write included.
	refuse = nil
	hookFail = problem.New(genproblems.Conflict, "wallet busy")
	requireCode(t, f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: reqID, Code: code}), genproblems.Conflict)
	untouched("a failed hook")
	require.Equal(t, 1, f.count(`SELECT count(*) FROM otp_requests WHERE id = $1 AND consumed_at IS NULL`, ids.UUID(mustParse(t, reqID))), "the code is not spent by a rolled-back deletion")
	hookFail = errors.New("boom")
	requireCode(t, f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: reqID, Code: code}), genproblems.Internal)
	untouched("a broken hook")

	hookFail = nil
	require.NoError(t, f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: reqID, Code: code}))
	require.Len(t, ran, 3)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM users WHERE id = $1 AND status = 'deleted' AND deleted_at IS NOT NULL AND name IS NULL AND preferred_lang = 'hi'`, uid), "anonymised, with the hook's write")
	require.Zero(t, f.count(`SELECT count(*) FROM phone_identities WHERE user_id = $1`, uid))
	require.Zero(t, f.count(`SELECT count(*) FROM addresses WHERE user_id = $1`, uid))
	require.Zero(t, f.count(`SELECT count(*) FROM devices WHERE user_id = $1 AND (push_endpoint IS NOT NULL OR push_keys IS NOT NULL OR counter_secret_enc IS NOT NULL)`, uid))
	// L1: the browser's device id no longer links to the deleted user, and no
	// session keeps a user agent or IP hash.
	require.Equal(t, len(deviceKeys), f.count(`SELECT count(*) FROM devices WHERE user_id = $1`, uid), "the rows stay")
	require.Zero(t, f.count(`SELECT count(*) FROM devices WHERE device_key = ANY($1)`, deviceKeys), "every device_key replaced")
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND (user_agent IS NOT NULL OR ip_hash IS NOT NULL)`, uid))
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, uid), "every app")
	require.Equal(t, 2, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_reason = 'deleted'`, uid))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM otp_requests WHERE id = $1 AND consumed_at IS NOT NULL`, ids.UUID(mustParse(t, reqID))))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.account_deleted'`))

	// The deleted principal can do nothing more; the number is free again.
	require.Error(t, f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: reqID, Code: code}))
	again := f.signIn(phone, authn.AppCustomer)
	require.True(t, again.IsNewUser)
	require.NotEqual(t, in.UserID, again.UserID)
}

// D-106 (M3): while no-show strikes limit the account, it cannot be deleted
// (that would shed the limit): 409 conflict naming the date, the code stays
// unused and no try is spent. After the date it can.
func TestDeleteAccountWhileLimited(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const phone = "+919876543210"
	in := f.signIn(phone, authn.AppCustomer)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)
	uid := ids.UUID(in.UserID)
	var other []ids.ID
	f.svc.AddDeletionCheck(CanDeleteFunc(func(_ context.Context, id ids.ID) error {
		other = append(other, id)
		return nil
	}))
	reqID, code := stepUpCode(f, phone)
	ctx := f.ctx("10.0.0.1")
	// Limited for two more minutes, inside the code's five.
	until := f.clk.Now().Add(2 * time.Minute).UTC().Truncate(time.Second)
	require.Equal(t, 1, f.count(`WITH u AS (UPDATE customer_profiles SET limited_until = $2 WHERE user_id = $1 RETURNING 1) SELECT count(*) FROM u`, uid, until))

	err = f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: reqID, Code: code})
	requireCode(t, err, genproblems.Conflict)
	pe := problemOf(t, err)
	require.Equal(t, until.Format(time.RFC3339), pe.Extensions["limitedUntil"])
	require.Contains(t, pe.Detail, until.Format(time.RFC3339))
	require.Empty(t, other, "identity's own check runs first and stops the rest")
	require.Equal(t, 1, f.count(`SELECT count(*) FROM users WHERE id = $1 AND status = 'active'`, uid))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM otp_requests WHERE id = $1 AND consumed_at IS NULL AND attempts = 0`, ids.UUID(mustParse(t, reqID))))

	// A second before the limit's end it still holds; from limited_until on
	// (the rule is limited_until > now) the account can go.
	f.clk.Set(until.Add(-time.Second))
	requireCode(t, f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: reqID, Code: code}), genproblems.Conflict)
	f.clk.Advance(time.Second)
	require.NoError(t, f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: reqID, Code: code}))
	require.Equal(t, []ids.ID{in.UserID}, other)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM users WHERE id = $1 AND status = 'deleted'`, uid))
}

// An active admin keeps their account: a super_admin disables them first.
func TestDeleteAccountRefusesAnAdmin(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	const phone = "+919876543210"
	in := f.signIn(phone, authn.AppCustomer)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)
	f.owner(`INSERT INTO admin_users (user_id, roles) VALUES ($1, ARRAY['support'])`, ids.UUID(in.UserID))
	reqID, code := stepUpCode(f, phone)
	requireCode(t, f.svc.DeleteAccount(f.ctx("10.0.0.1"), p, DeleteInput{RequestID: reqID, Code: code}), genproblems.Conflict)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM users WHERE status = 'active'`))

	f.owner(`UPDATE admin_users SET disabled_at = now() WHERE user_id = $1`, ids.UUID(in.UserID))
	require.NoError(t, f.svc.DeleteAccount(f.ctx("10.0.0.1"), p, DeleteInput{RequestID: reqID, Code: code}))
}

// L1: deleting a disabled admin ends their admin sign-ins with the account;
// the admin_users row stays (disabled) for the audit trail.
func TestDeleteAccountDisabledAdmin(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	ad, _ := newAdmin(f, adminPhone)
	uid := ids.UUID(ad.UserID)
	f.owner(`UPDATE admin_users SET disabled_at = now() WHERE user_id = $1`, uid)

	cust := f.signIn(adminPhone, authn.AppCustomer)
	p, err := f.svc.tokens.Verify(cust.AccessToken)
	require.NoError(t, err)
	reqID, code := stepUpCode(f, adminPhone)
	require.NoError(t, f.svc.DeleteAccount(f.ctx("10.0.0.1"), p, DeleteInput{RequestID: reqID, Code: code}))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM admin_users WHERE user_id = $1 AND disabled_at IS NOT NULL`, uid))
	require.Zero(t, f.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, uid))
}

// A step_up code is bound to its number: another user's code never deletes.
func TestDeleteAccountOtherUsersCode(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	in := f.signIn("+919876543210", authn.AppCustomer)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(t, err)
	f.signIn("+919876543211", authn.AppCustomer)
	reqID, code := stepUpCode(f, "+919876543211")
	requireCode(t, f.svc.DeleteAccount(f.ctx("10.0.0.1"), p, DeleteInput{RequestID: reqID, Code: code}), genproblems.OtpExpired)
	require.Equal(t, 2, f.count(`SELECT count(*) FROM users WHERE status = 'active'`))
}

func mustParse(t *testing.T, s string) ids.ID {
	t.Helper()
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return id
}

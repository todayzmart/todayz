package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/identitydb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// SendResult is what a send answers.
type SendResult struct {
	RequestID   ids.ID
	ResendAfter time.Duration
}

// SendOTP checks Turnstile and the limits, stores a hashed code and sends it
// by SMS (01 §2). It never looks the number up (AU-06): a known and an
// unknown number get the same answer.
func (s *Service) SendOTP(ctx context.Context, app authn.App, phone, purpose, turnstileToken string) (SendResult, error) {
	if err := s.ready(); err != nil {
		return SendResult{}, unavailable(ctx, err)
	}
	e164, ok := pii.NormalizePhone(phone)
	if !ok {
		return SendResult{}, problem.New(genproblems.ValidationFailed, "").WithField("body.phone", "must be an Indian mobile number in E.164")
	}
	if purpose != PurposeLogin {
		return SendResult{}, problem.New(genproblems.ValidationFailed, "").WithField("body.purpose", "must be login")
	}
	if !app.Valid() {
		return SendResult{}, problem.New(genproblems.ValidationFailed, "").WithField("body.app", "unknown app")
	}

	clientIP := reqctx.ClientIP(ctx)
	human, err := s.human.Verify(ctx, turnstileToken, clientIP)
	if err != nil {
		return SendResult{}, problem.Wrap(genproblems.ServiceUnavailable, err)
	}
	if !human {
		// TODO(lead): 04 names no code for a failed Turnstile check; 403
		// forbidden (AU-20), and nothing is stored or sent.
		return SendResult{}, problem.New(genproblems.Forbidden, "")
	}

	code, err := randomDigits(OTPDigits)
	if err != nil {
		return SendResult{}, problem.Wrap(genproblems.Internal, err)
	}
	phoneHash := s.hash.Phone(e164)
	ipHash := s.hash.IP(clientIP)
	phoneEnc, err := s.seal.Seal("phone", []byte(e164))
	if err != nil {
		return SendResult{}, problem.Wrap(genproblems.Internal, err)
	}

	var id ids.ID
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		// Phone before IP, always: two sends never wait on each other in
		// opposite orders.
		if err := q.LockKey(ctx, "otp:phone:"+hex.EncodeToString(phoneHash)); err != nil {
			return err
		}
		if ipHash != nil {
			if err := q.LockKey(ctx, "otp:ip:"+hex.EncodeToString(ipHash)); err != nil {
				return err
			}
		}
		now := s.clock.Now().UTC()
		since := now.Add(-OTPWindow)
		byPhone, err := q.OtpSendsByPhoneSince(ctx, identitydb.OtpSendsByPhoneSinceParams{Since: since, PhoneHash: phoneHash})
		if err != nil {
			return err
		}
		if byPhone.Sends > 0 && now.Sub(byPhone.Latest) < OTPResendGap {
			return limited(byPhone.Latest.Add(OTPResendGap).Sub(now), OTPResendGap)
		}
		if byPhone.Sends >= OTPPerPhone {
			return limited(byPhone.Oldest.Add(OTPWindow).Sub(now), OTPWindow)
		}
		if ipHash != nil {
			byIP, err := q.OtpSendsByIPSince(ctx, identitydb.OtpSendsByIPSinceParams{Since: since, IpHash: ipHash})
			if err != nil {
				return err
			}
			if byIP.Sends >= OTPPerIP {
				return limited(byIP.Oldest.Add(OTPWindow).Sub(now), OTPWindow)
			}
		}
		id = ids.New(now)
		return q.InsertOtpRequest(ctx, identitydb.InsertOtpRequestParams{
			ID: ids.UUID(id), PhoneHash: phoneHash, PhoneEnc: phoneEnc,
			Purpose: purpose, App: string(app), CodeHash: s.codeHash(id, code),
			ExpiresAt: now.Add(OTPTTL), IpHash: ipHash, CreatedAt: now,
		})
	})
	if err != nil {
		return SendResult{}, asProblem(err)
	}

	// After commit: the provider call never holds the limit locks. A failed
	// send still counts toward the limits (the row is there), so a provider
	// outage cannot be used to bypass them.
	if err := s.sms.SendOTP(ctx, e164, code); err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "identity: OTP SMS failed", "error", err, "app", string(app))
		return SendResult{}, problem.Wrap(genproblems.ServiceUnavailable, err)
	}
	return SendResult{RequestID: id, ResendAfter: OTPResendGap}, nil
}

// VerifyInput is one verify call.
type VerifyInput struct {
	App       authn.App
	RequestID string
	Code      string
	DeviceID  string
	UserAgent string
}

// SignIn is a successful verify or refresh.
type SignIn struct {
	UserID       ids.ID
	IsNewUser    bool
	AccessToken  string
	ExpiresAt    time.Time
	RefreshToken string
	// RefreshExpiresAt is when the refresh cookie expires.
	RefreshExpiresAt time.Time
}

// VerifyOTP checks a code and signs in: five tries per code, then locked
// (AU-02); expired, used and unknown codes are otp_expired. The attempt count
// commits even when the code is wrong.
func (s *Service) VerifyOTP(ctx context.Context, in VerifyInput) (SignIn, error) {
	if err := s.readyTokens(); err != nil {
		return SignIn{}, unavailable(ctx, err)
	}
	if !in.App.Valid() {
		return SignIn{}, problem.New(genproblems.ValidationFailed, "").WithField("body.app", "unknown app")
	}
	reqID, err := ids.Parse(in.RequestID)
	if err != nil {
		return SignIn{}, problem.New(genproblems.OtpExpired, "")
	}
	device, err := ids.Parse(in.DeviceID)
	if err != nil {
		return SignIn{}, problem.New(genproblems.ValidationFailed, "").WithField("body.deviceId", "must be a ULID")
	}

	var (
		out     SignIn
		outcome error // a domain refusal that must not roll back the attempt count
		family  ids.ID
		roles   []string
	)
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		now := s.clock.Now().UTC()
		row, err := q.GetOtpRequestForUpdate(ctx, ids.UUID(reqID))
		switch {
		case db.IsNoRows(err):
			outcome = problem.New(genproblems.OtpExpired, "")
			return nil
		case err != nil:
			return err
		}
		if row.ConsumedAt != nil || row.App != string(in.App) || row.Purpose != PurposeLogin {
			outcome = problem.New(genproblems.OtpExpired, "")
			return nil
		}
		if row.Attempts >= OTPMaxAttempts {
			outcome = problem.New(genproblems.OtpLocked, "")
			return nil
		}
		if !now.Before(row.ExpiresAt) {
			outcome = problem.New(genproblems.OtpExpired, "")
			return nil
		}
		attempts, err := q.RecordOtpAttempt(ctx, row.ID)
		if err != nil {
			return err
		}
		plain, err := s.seal.Open("phone", row.PhoneEnc)
		if err != nil {
			return fmt.Errorf("identity: open otp phone: %w", err)
		}
		e164 := string(plain)
		match := hmac.Equal(s.codeHash(reqID, in.Code), row.CodeHash)
		if !match && s.allowTestCode && pii.IsReservedTestPhone(e164) {
			match = hmac.Equal([]byte(in.Code), []byte(testCode))
		}
		if !match {
			left := max(OTPMaxAttempts-int(attempts), 0)
			outcome = problem.New(genproblems.Unauthorized, "").WithExtension("attemptsLeft", left)
			return nil
		}
		if err := q.ConsumeOtpRequest(ctx, identitydb.ConsumeOtpRequestParams{Now: now, ID: row.ID}); err != nil {
			return err
		}
		res, refusal, err := s.signIn(ctx, q, now, in, row.PhoneHash, plain, device)
		if err != nil {
			return err
		}
		if refusal != nil {
			// The code is used either way; the refusal answers after commit.
			outcome = refusal
			return nil
		}
		out, family, roles = res.SignIn, res.family, res.roles
		return nil
	})
	if err != nil {
		return SignIn{}, asProblem(err)
	}
	if outcome != nil {
		return SignIn{}, outcome
	}
	tok, exp, err := s.tokens.Issue(authn.Principal{
		UserID: out.UserID, App: in.App, SessionID: family, Roles: roles, AMR: authn.AMROTP,
	})
	if err != nil {
		return SignIn{}, problem.Wrap(genproblems.Internal, err)
	}
	out.AccessToken, out.ExpiresAt = tok, exp
	return out, nil
}

type signInResult struct {
	SignIn
	family ids.ID
	roles  []string
}

// signIn finds or creates the user of a verified phone and opens a session.
// A refusal (blocked, deleted, not an admin) is returned separately from a
// database error so the caller can commit the used code.
func (s *Service) signIn(ctx context.Context, q *identitydb.Queries, now time.Time, in VerifyInput, phoneHash, phonePlain []byte, device ids.ID) (res signInResult, refusal, err error) {
	// One sign-in per number at a time: the find-or-create never races into
	// two users.
	if err := q.LockKey(ctx, "identity:phone:"+hex.EncodeToString(phoneHash)); err != nil {
		return signInResult{}, nil, err
	}
	var (
		userID ids.ID
		isNew  bool
	)
	u, err := q.GetUserByPhoneHash(ctx, phoneHash)
	switch {
	case db.IsNoRows(err):
		if in.App == authn.AppAdmin {
			// Admins are created by the admin seed or another admin (phase B), never by signing in.
			return signInResult{}, problem.New(genproblems.Forbidden, ""), nil
		}
		userID, isNew = ids.New(now), true
		if err := q.InsertUser(ctx, identitydb.InsertUserParams{ID: ids.UUID(userID), Now: now}); err != nil {
			return signInResult{}, nil, err
		}
		enc, err := s.seal.Seal("phone", phonePlain)
		if err != nil {
			return signInResult{}, nil, err
		}
		if err := q.InsertPhoneIdentity(ctx, identitydb.InsertPhoneIdentityParams{
			ID: ids.UUID(ids.New(now)), UserID: ids.UUID(userID), PhoneEnc: enc, PhoneHash: phoneHash, Now: now,
		}); err != nil {
			return signInResult{}, nil, err
		}
	case err != nil:
		return signInResult{}, nil, err
	default:
		id, ok := ids.FromUUID(u.ID)
		if !ok {
			return signInResult{}, nil, errors.New("identity: user without id")
		}
		userID = id
		if refusal := statusRefusal(u.Status); refusal != nil {
			return signInResult{}, refusal, nil
		}
	}

	var roles []string
	switch in.App {
	case authn.AppAdmin:
		a, err := q.GetAdminUser(ctx, ids.UUID(userID))
		if db.IsNoRows(err) || (err == nil && a.DisabledAt != nil) {
			return signInResult{}, problem.New(genproblems.Forbidden, ""), nil
		}
		if err != nil {
			return signInResult{}, nil, err
		}
		roles = a.Roles
	case authn.AppCustomer:
		if err := s.ensureCustomerProfile(ctx, q, userID, now); err != nil {
			return signInResult{}, nil, err
		}
	}

	deviceRow, err := q.UpsertDevice(ctx, identitydb.UpsertDeviceParams{
		ID: ids.UUID(ids.New(now)), UserID: ids.UUID(userID), DeviceKey: device.String(), Now: now,
	})
	if err != nil {
		return signInResult{}, nil, err
	}
	family := ids.New(now)
	refresh, refreshHash, err := newRefreshToken()
	if err != nil {
		return signInResult{}, nil, err
	}
	refreshExp := now.Add(in.App.RefreshTTL())
	if err := q.InsertSession(ctx, identitydb.InsertSessionParams{
		ID: ids.UUID(family), UserID: ids.UUID(userID), App: string(in.App), FamilyID: ids.UUID(family),
		RefreshHash: refreshHash, ExpiresAt: refreshExp, DeviceID: deviceRow,
		UserAgent: cleanUserAgent(in.UserAgent), IpHash: s.hash.IP(reqctx.ClientIP(ctx)), Now: now,
	}); err != nil {
		return signInResult{}, nil, err
	}
	if _, err := s.audit.Write(ctx, auditEntry(in.App, userID, "identity.sign_in", map[string]any{
		"app": string(in.App), "newUser": isNew, "sessionId": family.String(),
	})); err != nil {
		return signInResult{}, nil, err
	}
	return signInResult{
		SignIn: SignIn{UserID: userID, IsNewUser: isNew, RefreshToken: refresh, RefreshExpiresAt: refreshExp},
		family: family, roles: roles,
	}, nil, nil
}

// statusRefusal is the answer for a user who may not sign in.
func statusRefusal(status string) error {
	switch status {
	case "active":
		return nil
	case "blocked":
		return problem.New(genproblems.AccountBlocked, "")
	default:
		// TODO(lead): a deleted account signing in again. The phone identity
		// is released on deletion in phase B; until then 403 forbidden.
		return problem.New(genproblems.Forbidden, "")
	}
}

// referralAlphabet is the referral code alphabet (no 0, O, 1, I, L).
const referralAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// ensureCustomerProfile creates the customer's profile with a fresh referral
// code, retrying a code that is already taken.
func (s *Service) ensureCustomerProfile(ctx context.Context, q *identitydb.Queries, userID ids.ID, now time.Time) error {
	for range 8 {
		code, err := randomString(referralAlphabet, 6)
		if err != nil {
			return err
		}
		n, err := q.InsertCustomerProfile(ctx, identitydb.InsertCustomerProfileParams{UserID: ids.UUID(userID), ReferralCode: code, Now: now})
		if err != nil {
			return err
		}
		if n == 1 {
			return nil
		}
		if _, err := q.GetCustomerProfile(ctx, ids.UUID(userID)); err == nil {
			return nil // it already existed
		} else if !db.IsNoRows(err) {
			return err
		}
	}
	return errors.New("identity: no free referral code after 8 tries")
}

// codeHash binds a code to its request: HMAC(PII_HASH_KEY, "otp:<id>:<code>").
func (s *Service) codeHash(requestID ids.ID, code string) []byte {
	return s.hash.Sum("otp:" + requestID.String() + ":" + code)
}

// limited is 429 rate_limited with Retry-After in whole seconds, clamped to
// [1 s, ceiling] (the local test clock may move backwards, D-80).
func limited(wait, ceiling time.Duration) error {
	wait = min(max(wait, time.Second), ceiling)
	secs := int((wait + time.Second - 1) / time.Second)
	return problem.New(genproblems.RateLimited, "").WithHeader("Retry-After", strconv.Itoa(secs))
}

// randomDigits returns n uniformly random decimal digits (crypto/rand).
func randomDigits(n int) (string, error) { return randomString("0123456789", n) }

func randomString(alphabet string, n int) (string, error) {
	b := make([]byte, n)
	limit := big.NewInt(int64(len(alphabet)))
	for i := range b {
		v, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("identity: random: %w", err)
		}
		b[i] = alphabet[v.Int64()]
	}
	return string(b), nil
}

// asProblem passes a problem through and wraps anything else as 500.
func asProblem(err error) error {
	var p *problem.Error
	if errors.As(err, &p) {
		return p
	}
	return problem.Wrap(genproblems.Internal, err)
}

// unavailable logs what is missing (names only) and answers 503.
func unavailable(ctx context.Context, err error) error {
	telemetry.Logger(ctx).ErrorContext(ctx, "identity: sign-in is not configured (needs the database, PII_ENC_KEY, PII_HASH_KEY and JWT_SIGNING_KEYS)")
	return problem.Wrap(genproblems.ServiceUnavailable, err)
}

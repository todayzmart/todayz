package identity

import (
	"context"
	"errors"
	"strconv"
	"time"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/identitydb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/firebaseauth"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// D-275: Firebase Auth may send and check the sign-in code instead of AWS
// SMS (OTP_PROVIDER=firebase). The page signs in to Firebase by phone, then
// swaps the Firebase ID token for a TodayZ session here. Firebase owns
// nothing else: users, sessions, roles, refresh cookies and audit are the
// same as for otp/verify (signIn).
const (
	// FirebaseMaxAuthAge is how old the phone sign-in (auth_time) may be when
	// it is swapped (founder, D-275).
	FirebaseMaxAuthAge = 5 * time.Minute
	// firebaseReplayTTL keeps a used verification for as long as a token of
	// it could still pass the age check, clock skew included.
	firebaseReplayTTL = FirebaseMaxAuthAge + 2*firebaseauth.ClockSkew
)

// FirebaseVerifier checks a Firebase ID token (firebaseauth.Verifier; a fake
// in tests).
type FirebaseVerifier interface {
	Verify(ctx context.Context, raw string) (firebaseauth.Claims, error)
}

// FirebaseInput is one swap of a Firebase ID token.
type FirebaseInput struct {
	App       authn.App
	IDToken   string
	DeviceID  string
	UserAgent string
}

// OTPProvider is who sends the sign-in code: config.OTPProviderSMS or
// config.OTPProviderFirebase.
func (s *Service) OTPProvider() string {
	if s.cfg.Providers.OTPProvider == config.OTPProviderFirebase {
		return config.OTPProviderFirebase
	}
	return config.OTPProviderSMS
}

// FirebaseWebConfig is the public Firebase config the apps get from GET
// /v1/auth/config.
func (s *Service) FirebaseWebConfig() config.Firebase { return s.cfg.Providers.Firebase }

// VerifyFirebase checks a Firebase phone sign-in and signs in. A bad token is
// 401 unauthorized; a sign-in older than FirebaseMaxAuthAge, or one already
// swapped, is 401 otp_expired; while the provider is sms it is 409
// sign_in_method_changed. An admin-app sign-in respects the D-264 lock and
// clears the wrong-code count, as a good SMS code does. Neither the token nor
// the number is logged.
func (s *Service) VerifyFirebase(ctx context.Context, in FirebaseInput) (SignIn, error) {
	if s.OTPProvider() != config.OTPProviderFirebase {
		return SignIn{}, problem.New(genproblems.SignInMethodChanged, "")
	}
	if err := s.readyTokens(); err != nil {
		return SignIn{}, unavailable(ctx, err)
	}
	if s.firebase == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "identity: OTP_PROVIDER is firebase but FIREBASE_PROJECT_ID is unset")
		return SignIn{}, problem.New(genproblems.ServiceUnavailable, "")
	}
	if !in.App.Valid() {
		return SignIn{}, problem.New(genproblems.ValidationFailed, "").WithField("body.app", "unknown app")
	}
	device, err := ids.Parse(in.DeviceID)
	if err != nil {
		return SignIn{}, problem.New(genproblems.ValidationFailed, "").WithField("body.deviceId", "must be a ULID")
	}
	claims, err := s.firebase.Verify(ctx, in.IDToken)
	switch {
	case errors.Is(err, firebaseauth.ErrStale):
		return SignIn{}, problem.New(genproblems.OtpExpired, "")
	case errors.Is(err, firebaseauth.ErrKeys):
		telemetry.Logger(ctx).ErrorContext(ctx, "identity: Firebase public keys unavailable")
		return SignIn{}, problem.Wrap(genproblems.ServiceUnavailable, err)
	case err != nil:
		return SignIn{}, problem.New(genproblems.Unauthorized, "")
	}
	// TodayZ signs in Indian mobiles only (01 §2); the Firebase project's SMS
	// region policy should already refuse any other.
	e164, ok := pii.NormalizePhone(claims.Phone)
	if !ok {
		return SignIn{}, problem.New(genproblems.Forbidden, "")
	}
	phoneHash := s.hash.Phone(e164)
	replayKey := s.hash.Sum("firebase:" + claims.UID + ":" + strconv.FormatInt(claims.AuthTime.Unix(), 10))

	var (
		out     SignIn
		outcome error // a refusal that still commits (the verification is used)
		family  ids.ID
		roles   []string
	)
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		now := s.clock.Now().UTC()
		adminLogin := in.App == authn.AppAdmin
		if adminLogin {
			if err := q.LockKey(ctx, adminSigninKey(phoneHash)); err != nil {
				return err
			}
			until, locked, err := adminSigninLock(ctx, q, phoneHash, now)
			if err != nil {
				return err
			}
			if locked {
				outcome = signinLocked(until, now)
				return nil
			}
		}
		n, err := q.ClaimFirebaseSignin(ctx, identitydb.ClaimFirebaseSigninParams{
			KeyHash: replayKey, UsedAt: now, ExpiresAt: now.Add(firebaseReplayTTL),
		})
		if err != nil {
			return err
		}
		if n == 0 {
			outcome = problem.New(genproblems.OtpExpired, "")
			return nil
		}
		if adminLogin {
			if err := q.ClearAdminSigninFailures(ctx, phoneHash); err != nil {
				return err
			}
		}
		res, refusal, err := s.signIn(ctx, q, now, VerifyInput{
			App: in.App, DeviceID: in.DeviceID, UserAgent: in.UserAgent, method: config.OTPProviderFirebase,
		}, phoneHash, []byte(e164), device)
		if err != nil {
			return err
		}
		if refusal != nil {
			outcome = refusal
			return nil //nolint:nilerr // commit (the verification is used), then answer the refusal
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

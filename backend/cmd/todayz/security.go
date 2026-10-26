package main

import (
	"fmt"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/httpx/mw"
	"github.com/todayz/backend/internal/platform/ratelimit"
	"github.com/todayz/backend/internal/platform/storage"
)

// buildSecurity builds the sign-in pieces the pipeline needs before any route
// mounts (W1-01): the token service, the identity service (its session check
// backs the authenticator, slot 11), the permission engine (slot 15) and the
// limiter (slot 12). It fills d.Identity and d.Authz for the domain mounts.
//
// Without JWT keys (a route-only test) nothing can verify a token, so the
// authenticator is a nil interface and any Authorization header is 401;
// sign-in then answers 503. Staging and prod refuse to start instead
// (identity.New).
func buildSecurity(d *deps) (mw.Authenticator, mw.Limiter, error) {
	var tokens *authn.Tokens
	if len(d.Config.Keys.JWTSigningKeys) > 0 {
		ks, err := authn.NewKeySet(d.Config.Keys.JWTSigningKeys, d.Config.Keys.JWTCurrentKID)
		if err != nil {
			return nil, nil, fmt.Errorf("security: %w", err)
		}
		tokens = authn.NewTokens(ks, d.Clock)
	}
	svc, err := identity.New(identity.Deps{
		Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log, Tokens: tokens, Settings: d.Settings,
		// Profile photos (D-259); nil without storage config, and the photo routes answer 503.
		Storage:    storage.New(d.Config.Storage, httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: d.Config.NoEgress})),
		TestRoutes: d.TestClock != nil, // the /v1/test/clock gate (D-80)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("security: %w", err)
	}
	// Business membership comes from the staff table (W1-04). Without a
	// database the resolver stays a nil interface and the engine refuses
	// every business action.
	var staff authz.StaffResolver
	if d.DB != nil {
		staff = business.NewStaffResolver(d.DB)
	}
	engine, err := authz.New(policy.All(), authz.Options{Clock: d.Clock, Staff: staff})
	if err != nil {
		return nil, nil, fmt.Errorf("security: %w", err)
	}
	d.Identity, d.Authz = svc, engine

	var a mw.Authenticator
	switch {
	case tokens == nil:
	case d.DB == nil:
		// No database (route-only tests): tokens are trusted until they expire.
		a = authn.NewAuthenticator(tokens, nil)
	default:
		a = authn.NewAuthenticator(tokens, svc)
	}

	// The generic limiter guards staging and prod. Locally and in tests it is
	// off so the contract fuzzing and e2e journeys are not throttled; the OTP
	// limits (Postgres, internal/identity) apply everywhere.
	var l mw.Limiter
	if d.Config.Env.IsDeployed() {
		l = ratelimit.New(d.Clock, ratelimit.DefaultRules)
	}
	return a, l, nil
}

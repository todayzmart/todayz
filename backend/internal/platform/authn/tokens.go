package authn

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/ids"
)

// Issuer is the iss claim of every access token.
const Issuer = "todayz"

// MaxTokenLen bounds a bearer token before any parsing (a real one is ~400 bytes).
const MaxTokenLen = 4096

// ErrInvalidToken is every reason a token is refused: bad shape, bad
// signature, unknown kid, wrong alg, expired, wrong issuer or audience. The
// reason is never shown to the caller.
var ErrInvalidToken = errors.New("authn: invalid token")

// KeySet holds the Ed25519 signing keys by kid (JWT_SIGNING_KEYS) and the
// current signer (JWT_CURRENT_KID). A retired kid stays in the set until every
// token it signed has expired (AU-19); a kid not in the set is refused.
type KeySet struct {
	current string
	priv    ed25519.PrivateKey
	pub     map[string]ed25519.PublicKey
}

// NewKeySet builds the set. currentKID must name one of keys.
func NewKeySet(keys []config.SigningKey, currentKID string) (*KeySet, error) {
	ks := &KeySet{current: currentKID, pub: map[string]ed25519.PublicKey{}}
	for _, k := range keys {
		if k.KID == "" || len(k.Public) != ed25519.PublicKeySize {
			return nil, errors.New("authn: a signing key without a kid or with a bad public key")
		}
		if _, dup := ks.pub[k.KID]; dup {
			return nil, fmt.Errorf("authn: kid %q appears twice", k.KID)
		}
		ks.pub[k.KID] = k.Public
		if k.KID == currentKID {
			ks.priv = k.Private()
		}
	}
	if ks.priv == nil {
		return nil, errors.New("authn: JWT_CURRENT_KID does not name a key in JWT_SIGNING_KEYS")
	}
	return ks, nil
}

// CurrentKID is the kid new tokens are signed with.
func (k *KeySet) CurrentKID() string { return k.current }

// claims is the access token body. Field names are the registered JWT ones
// plus sid, roles and amr.
type claims struct {
	jwt.RegisteredClaims
	SessionID string   `json:"sid"`
	Roles     []string `json:"roles,omitempty"`
	AMR       AMR      `json:"amr"`
}

// Tokens issues and verifies access tokens.
type Tokens struct {
	keys  *KeySet
	clock clock.Clock
}

// NewTokens returns a token service on keys and the injected clock.
func NewTokens(keys *KeySet, c clock.Clock) *Tokens { return &Tokens{keys: keys, clock: c} }

// Issue signs an access token for p (UserID, App, SessionID, Roles, AMR are
// read; the times are set here) and returns it with its expiry.
func (t *Tokens) Issue(p Principal) (string, time.Time, error) {
	if p.IsAnonymous() || !p.App.Valid() || p.SessionID == ids.Zero || (p.AMR != AMROTP && p.AMR != AMROTPTOTP) {
		return "", time.Time{}, errors.New("authn: issue: incomplete principal")
	}
	now := t.clock.Now().UTC().Truncate(time.Second)
	exp := now.Add(AccessTTL)
	c := claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   p.UserID.String(),
			Audience:  jwt.ClaimStrings{string(p.App)},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		SessionID: p.SessionID.String(),
		AMR:       p.AMR,
	}
	if p.App == AppAdmin {
		c.Roles = slices.Clone(p.Roles)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, c)
	tok.Header["kid"] = t.keys.current
	s, err := tok.SignedString(t.keys.priv)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("authn: sign: %w", err)
	}
	return s, exp, nil
}

// Verify checks raw and returns its principal. Every failure is
// ErrInvalidToken. Only EdDSA is accepted; the kid must be in the set; exp is
// required; iss, aud and sub must be well formed. iat is not checked against
// the clock: the local test clock may move back (D-80), and exp bounds the
// token anyway.
func (t *Tokens) Verify(raw string) (Principal, error) {
	if raw == "" || len(raw) > MaxTokenLen {
		return Principal{}, ErrInvalidToken
	}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer(Issuer),
		jwt.WithTimeFunc(t.clock.Now),
		jwt.WithStrictDecoding(),
	)
	var c claims
	_, err := parser.ParseWithClaims(raw, &c, func(tok *jwt.Token) (any, error) {
		kid, _ := tok.Header["kid"].(string)
		pub, ok := t.keys.pub[kid]
		if !ok {
			return nil, ErrInvalidToken
		}
		return pub, nil
	})
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	if len(c.Audience) != 1 {
		return Principal{}, ErrInvalidToken
	}
	app, ok := ParseApp(c.Audience[0])
	if !ok {
		return Principal{}, ErrInvalidToken
	}
	uid, err := ids.Parse(c.Subject)
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	sid, err := ids.Parse(c.SessionID)
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	if c.AMR != AMROTP && c.AMR != AMROTPTOTP {
		return Principal{}, ErrInvalidToken
	}
	if c.ExpiresAt == nil || c.IssuedAt == nil {
		return Principal{}, ErrInvalidToken
	}
	p := Principal{
		UserID:    uid,
		App:       app,
		SessionID: sid,
		AMR:       c.AMR,
		IssuedAt:  c.IssuedAt.UTC(),
		ExpiresAt: c.ExpiresAt.UTC(),
	}
	if app == AppAdmin {
		p.Roles = c.Roles
	}
	return p, nil
}

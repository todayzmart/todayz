// Package firebaseauth checks a Firebase Auth ID token from a phone sign-in
// (D-275). Firebase only sends and checks the SMS code; TodayZ signs the
// person in once this package has checked the token. It needs no secret:
// the token is RS256-signed by Google, and the public keys come from
// Google's JWK URL (in local and test, the mocks server's). Nothing here
// logs a token, a phone number or a uid.
package firebaseauth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GoogleJWKSURL is where Google publishes the keys that sign Firebase ID
// tokens (firebase.google.com/docs/auth/admin/verify-id-tokens).
const GoogleJWKSURL = "https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com"

const (
	// MaxTokenLen bounds what is parsed; a Firebase ID token is about 1 KB.
	MaxTokenLen = 8 << 10
	// ClockSkew is how far iat and auth_time may sit in our future.
	ClockSkew = time.Minute
	// minKeyRefetch stops an unknown kid from turning into a fetch per
	// request.
	minKeyRefetch = time.Minute
	defaultKeyTTL = time.Hour
	maxKeyTTL     = 24 * time.Hour
)

var (
	// ErrInvalidToken is every token that is not a good Firebase phone
	// sign-in for this project: bad signature, wrong project, expired,
	// no phone number, another sign-in method.
	ErrInvalidToken = errors.New("firebaseauth: invalid token")
	// ErrStale is a good token whose sign-in (auth_time) is older than the
	// allowed age.
	ErrStale = errors.New("firebaseauth: sign-in too old")
	// ErrKeys means the public keys could not be read, so nothing could be
	// checked.
	ErrKeys = errors.New("firebaseauth: public keys unavailable")
)

// Claims is what a checked token says. Phone is E.164 as Firebase gives it.
type Claims struct {
	UID       string
	Phone     string
	AuthTime  time.Time
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// Verifier checks ID tokens for one Firebase project.
type Verifier struct {
	projectID string
	maxAge    time.Duration
	now       func() time.Time
	keys      *keySet
}

// New makes a Verifier for projectID. jwksURL is GoogleJWKSURL when empty.
// client should be httpx.NewOutboundClient (the egress guard). maxAge is how
// old the sign-in may be.
func New(projectID, jwksURL string, client *http.Client, now func() time.Time, maxAge time.Duration) *Verifier {
	if jwksURL == "" {
		jwksURL = GoogleJWKSURL
	}
	return &Verifier{
		projectID: projectID,
		maxAge:    maxAge,
		now:       now,
		keys:      &keySet{url: jwksURL, client: client, now: now},
	}
}

type claims struct {
	jwt.RegisteredClaims
	AuthTime int64  `json:"auth_time"`
	Phone    string `json:"phone_number"`
	Firebase struct {
		SignInProvider string `json:"sign_in_provider"`
	} `json:"firebase"`
}

// Verify checks raw: RS256 with a key Google publishes; aud the project;
// iss https://securetoken.google.com/<project>; exp in the future; iat and
// auth_time not in the future; a uid; sign-in by phone with a phone
// number; and auth_time no older than maxAge (else ErrStale).
func (v *Verifier) Verify(ctx context.Context, raw string) (Claims, error) {
	if v.projectID == "" || raw == "" || len(raw) > MaxTokenLen {
		return Claims{}, ErrInvalidToken
	}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithIssuer("https://securetoken.google.com/"+v.projectID),
		jwt.WithAudience(v.projectID),
		jwt.WithTimeFunc(v.now),
		jwt.WithLeeway(ClockSkew),
	)
	var keyErr error
	var c claims
	_, err := parser.ParseWithClaims(raw, &c, func(tok *jwt.Token) (any, error) {
		kid, _ := tok.Header["kid"].(string)
		if kid == "" {
			return nil, ErrInvalidToken
		}
		k, err := v.keys.get(ctx, kid)
		if err != nil {
			keyErr = err
			return nil, err
		}
		return k, nil
	})
	if keyErr != nil && errors.Is(keyErr, ErrKeys) {
		return Claims{}, keyErr
	}
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	// WithLeeway also loosens exp; a token past its exp is refused here
	// without it.
	now := v.now()
	if c.ExpiresAt == nil || !now.Before(c.ExpiresAt.Time) || c.IssuedAt == nil {
		return Claims{}, ErrInvalidToken
	}
	if c.Subject == "" || len(c.Subject) > 128 || len(c.Audience) != 1 {
		return Claims{}, ErrInvalidToken
	}
	if c.Firebase.SignInProvider != "phone" || c.Phone == "" || c.AuthTime <= 0 {
		return Claims{}, ErrInvalidToken
	}
	auth := time.Unix(c.AuthTime, 0).UTC()
	if auth.After(now.Add(ClockSkew)) {
		return Claims{}, ErrInvalidToken
	}
	if now.Sub(auth) > v.maxAge {
		return Claims{}, ErrStale
	}
	return Claims{
		UID:       c.Subject,
		Phone:     c.Phone,
		AuthTime:  auth,
		IssuedAt:  c.IssuedAt.UTC(),
		ExpiresAt: c.ExpiresAt.UTC(),
	}, nil
}

// keySet caches Google's public keys for as long as their Cache-Control
// says (1 h when it says nothing, 24 h at most). An unknown kid refetches,
// at most once a minute.
type keySet struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	expires time.Time
	fetched time.Time
}

func (s *keySet) get(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if k, ok := s.keys[kid]; ok && now.Before(s.expires) {
		return k, nil
	}
	stale := !now.Before(s.expires)
	if stale || now.Sub(s.fetched) >= minKeyRefetch {
		if err := s.fetch(ctx, now); err != nil {
			if k, ok := s.keys[kid]; ok && !stale {
				return k, nil
			}
			return nil, err
		}
	}
	k, ok := s.keys[kid]
	if !ok {
		return nil, ErrInvalidToken
	}
	return k, nil
}

type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (s *keySet) fetch(ctx context.Context, now time.Time) error {
	s.fetched = now
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return fmt.Errorf("%w: request", ErrKeys)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrKeys, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrKeys, res.StatusCode)
	}
	var body struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(&body); err != nil {
		return fmt.Errorf("%w: decode", ErrKeys)
	}
	keys := make(map[string]*rsa.PublicKey, len(body.Keys))
	for _, k := range body.Keys {
		if k.Kid == "" || k.Kty != "RSA" || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		pub, err := rsaKey(k)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return fmt.Errorf("%w: no RSA keys", ErrKeys)
	}
	s.keys = keys
	s.expires = now.Add(maxAge(res.Header.Get("Cache-Control")))
	return nil
}

func rsaKey(k jwk) (*rsa.PublicKey, error) {
	n, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	e, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}
	ev := new(big.Int).SetBytes(e)
	if len(n) < 256 || !ev.IsInt64() || ev.Int64() < 3 || ev.Int64() > 1<<31 {
		return nil, errors.New("firebaseauth: weak or malformed key")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(ev.Int64())}, nil
}

// maxAge reads max-age from a Cache-Control header, within [1 min, 24 h].
func maxAge(cc string) time.Duration {
	for _, part := range strings.Split(cc, ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || !strings.EqualFold(name, "max-age") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			break
		}
		return min(max(time.Duration(n)*time.Second, minKeyRefetch), maxKeyTTL)
	}
	return defaultKeyTTL
}

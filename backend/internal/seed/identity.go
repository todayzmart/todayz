package seed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// identityUser is one seeded account. Every seeded phone is in the reserved
// test range (+91 90000 000xx, 01 §2), so locally and in test it signs in with
// 000000; staging and prod never run the seed.
type identityUser struct {
	Key      string // stable handle other domains' seeds use (identityUserID)
	ID       string // fixed ULID
	Phone    string
	Name     string // empty: no name yet (a new user)
	Lang     string
	Referral string // customers only: the referral code
}

// identityUsers are the same in every profile: three customers and the three
// staff of the seeded business (W1-04's seed links them as owner, manager and
// cashier through identityUserID). Admins come from SEED_ADMINS_FILE
// (seedAdmins below), never from here.
var identityUsers = []identityUser{
	{Key: "customer.asha", ID: "01KE0S1W6W8ZV7T1C3A6M9N001", Phone: "+919000000001", Name: "Asha", Lang: "en", Referral: "ASHA22"},
	{Key: "customer.ravi", ID: "01KE0S1W6W8ZV7T1C3A6M9N002", Phone: "+919000000002", Name: "Ravi", Lang: "kn", Referral: "RVKM23"},
	{Key: "customer.new", ID: "01KE0S1W6W8ZV7T1C3A6M9N003", Phone: "+919000000003", Lang: "hi", Referral: "NMQP34"},
	{Key: "staff.owner", ID: "01KE0S1W6W8ZV7T1C3A6M9N011", Phone: "+919000000011", Name: "Lakshmi", Lang: "en"},
	{Key: "staff.manager", ID: "01KE0S1W6W8ZV7T1C3A6M9N012", Phone: "+919000000012", Name: "Suresh", Lang: "ta"},
	{Key: "staff.cashier", ID: "01KE0S1W6W8ZV7T1C3A6M9N013", Phone: "+919000000013", Name: "Meena", Lang: "te"},
}

// identityUserID returns the fixed id of a seeded user by its Key. It panics
// on an unknown key: that is a bug in a seed, found by the seed test.
func identityUserID(key string) ids.ID {
	for _, u := range identityUsers {
		if u.Key == key {
			return mustID(u.ID)
		}
	}
	panic("seed: no identity user " + key)
}

// phoneIdentityID derives the phone identity's fixed id from the user's: the
// same ULID with its 23rd character changed from N to P, so it never collides
// with a user id.
func phoneIdentityID(userID string) ids.ID {
	return mustID(userID[:len(userID)-4] + "P" + userID[len(userID)-3:])
}

// mustID parses a fixed ULID written in this file; a typo panics, and the
// seed test finds it.
func mustID(s string) ids.ID {
	id, err := ids.Parse(s)
	if err != nil {
		panic("seed: bad fixed id " + s)
	}
	return id
}

// seedidentity writes the seeded users with their sealed phones (the same
// sealing and keyed hash the services use, so sign-in finds them). It is
// deterministic in everything but the AES-GCM nonce, and running it again on
// the same database changes nothing (ON CONFLICT DO NOTHING).
//
// Without PII_ENC_KEY and PII_HASH_KEY it seeds no users and warns, naming
// the variables (make setup writes them).
func seedidentity(ctx context.Context, d Deps, _ Profile) error {
	if !d.Keys.PIIEncKey.IsSet() || !d.Keys.PIIHashKey.IsSet() {
		d.Log.WarnContext(ctx, "seed identity: PII_ENC_KEY or PII_HASH_KEY unset; no users seeded (run make setup)")
		return nil
	}
	seal, err := pii.NewSealer(d.Keys.PIIEncKey.Bytes())
	if err != nil {
		return fmt.Errorf("PII_ENC_KEY: %w", err)
	}
	hash, err := pii.NewHasher(d.Keys.PIIHashKey.Bytes())
	if err != nil {
		return fmt.Errorf("PII_HASH_KEY: %w", err)
	}
	now := d.Clock.Now().UTC()
	for _, u := range identityUsers {
		if err := seedUser(ctx, d, seal, hash, u, now); err != nil {
			return err
		}
	}
	d.Log.DebugContext(ctx, "seed identity", slog.Int("users", len(identityUsers)))
	return seedAdmins(ctx, d, seal, hash, now)
}

// seedUser writes one user, its sealed phone and (customers) its profile.
func seedUser(ctx context.Context, d Deps, seal *pii.Sealer, hash *pii.Hasher, u identityUser, now time.Time) error {
	q := d.DB.Q(ctx)
	id := mustID(u.ID)
	e164, ok := pii.NormalizePhone(u.Phone)
	if !ok || !pii.IsReservedTestPhone(e164) {
		return fmt.Errorf("user %s: the phone must be a reserved test number", u.Key)
	}
	enc, err := seal.Seal("phone", []byte(e164))
	if err != nil {
		return fmt.Errorf("user %s: %w", u.Key, err)
	}
	var name *string
	if u.Name != "" {
		name = &u.Name
	}
	if _, err := q.Exec(ctx, `INSERT INTO users (id, name, preferred_lang, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4) ON CONFLICT DO NOTHING`, ids.UUID(id), name, u.Lang, now); err != nil {
		return fmt.Errorf("user %s: %w", u.Key, err)
	}
	if _, err := q.Exec(ctx, `INSERT INTO phone_identities (id, user_id, phone_enc, phone_hash, verified_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $5) ON CONFLICT DO NOTHING`,
		ids.UUID(phoneIdentityID(u.ID)), ids.UUID(id), enc, hash.Phone(e164), now); err != nil {
		return fmt.Errorf("user %s: phone: %w", u.Key, err)
	}
	if u.Referral != "" {
		if _, err := q.Exec(ctx, `INSERT INTO customer_profiles (user_id, referral_code, created_at, updated_at)
			VALUES ($1, $2, $3, $3) ON CONFLICT DO NOTHING`, ids.UUID(id), u.Referral, now); err != nil {
			return fmt.Errorf("user %s: profile: %w", u.Key, err)
		}
	}
	return nil
}

// --- Seed admins (08 §2 Fixtures, D-64, Q-20) ---------------------------------
//
// The admins live in a YAML file whose path is SEED_ADMINS_FILE; the backend
// never looks under e2e/ by itself (infra/local/local.env points the variable
// at e2e/fixtures/admins.yaml, relative to the repo root where make runs).
// Each admin is a user with a reserved test phone, an admin_users row with
// its roles and, unless totp_secret is empty, a TOTP secret sealed under
// TOTP_ENC_KEY with totp_enabled_at set (pre-enrolled). An admin with an
// empty totp_secret is unenrolled (A-01 first-login enrolment). No recovery
// codes are seeded: enrolment through the API makes them.
//
// With SEED_ADMINS_FILE unset no admins are seeded and the variable's name is
// logged. With it set, the seed refuses unless APP_ENV is local or test (the
// secrets are deterministic test values). Nothing here logs a phone or a
// secret, and an error names the admin by its key only.

// seedAdminsFileEnv is the variable holding the fixture path.
const seedAdminsFileEnv = "SEED_ADMINS_FILE"

// maxSeedAdminsFile caps the read; the fixture is about a kilobyte.
const maxSeedAdminsFile = 64 << 10

// minSeedTOTPSecret is the shortest seeded secret (RFC 4226 §4: at least 128 bits).
const minSeedTOTPSecret = 16

// getenv reads the environment; tests replace it.
var getenv = os.Getenv

type seedAdminFile struct {
	Admins []seedAdmin `yaml:"admins"`
}

type seedAdmin struct {
	Key        string   `yaml:"key"`
	ID         string   `yaml:"id"` // fixed ULID ending in N0xx (phoneIdentityID)
	Phone      string   `yaml:"phone"`
	Name       string   `yaml:"name"`
	Lang       string   `yaml:"lang"`
	Roles      []string `yaml:"roles"`
	TOTPSecret string   `yaml:"totp_secret"` // base32; empty: not enrolled
}

// seedAdmins seeds the admins of SEED_ADMINS_FILE (see above).
func seedAdmins(ctx context.Context, d Deps, seal *pii.Sealer, hash *pii.Hasher, now time.Time) error {
	path := getenv(seedAdminsFileEnv)
	if path == "" {
		d.Log.InfoContext(ctx, "seed identity: "+seedAdminsFileEnv+" unset; no admins seeded")
		return nil
	}
	if env := config.Env(getenv("APP_ENV")); env != config.EnvLocal && env != config.EnvTest {
		return fmt.Errorf("admins: refused: APP_ENV is %q; %s is seeded only in local and test", env, seedAdminsFileEnv)
	}
	if !d.Keys.TOTPEncKey.IsSet() {
		d.Log.WarnContext(ctx, "seed identity: TOTP_ENC_KEY unset; no admins seeded (run make setup)")
		return nil
	}
	totpSeal, err := pii.NewSealer(d.Keys.TOTPEncKey.Bytes())
	if err != nil {
		return fmt.Errorf("TOTP_ENC_KEY: %w", err)
	}
	admins, err := loadSeedAdmins(path)
	if err != nil {
		return err
	}
	q := d.DB.Q(ctx)
	enrolled := 0
	for _, a := range admins {
		u := identityUser{Key: a.Key, ID: a.ID, Phone: a.Phone, Name: a.Name, Lang: a.Lang}
		if err := seedUser(ctx, d, seal, hash, u, now); err != nil {
			return err
		}
		var secretEnc []byte
		var enabledAt *time.Time
		if a.TOTPSecret != "" {
			secret, err := identity.DecodeTOTPSecret(a.TOTPSecret)
			if err != nil {
				return fmt.Errorf("admin %s: totp_secret is not valid base32", a.Key)
			}
			if secretEnc, err = totpSeal.Seal(identity.TOTPSealField, secret); err != nil {
				return fmt.Errorf("admin %s: %w", a.Key, err)
			}
			enabledAt = &now
			enrolled++
		}
		if _, err := q.Exec(ctx, `INSERT INTO admin_users (user_id, roles, totp_secret_enc, totp_enabled_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $5) ON CONFLICT DO NOTHING`,
			ids.UUID(mustID(a.ID)), a.Roles, secretEnc, enabledAt, now); err != nil {
			return fmt.Errorf("admin %s: %w", a.Key, err)
		}
	}
	d.Log.InfoContext(ctx, "seed identity: admins seeded", slog.Int("admins", len(admins)), slog.Int("enrolled", enrolled))
	return nil
}

// loadSeedAdmins reads and checks the fixture. Errors never quote a phone or
// a secret from it.
func loadSeedAdmins(path string) ([]seedAdmin, error) {
	f, err := os.Open(path) // #nosec G304 -- the path is the operator's SEED_ADMINS_FILE, local/test only
	if err != nil {
		return nil, fmt.Errorf("admins: %s: %w", seedAdminsFileEnv, err)
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxSeedAdminsFile+1))
	if err != nil {
		return nil, fmt.Errorf("admins: %s: %w", seedAdminsFileEnv, err)
	}
	if len(raw) > maxSeedAdminsFile {
		return nil, fmt.Errorf("admins: %s is larger than %d bytes", seedAdminsFileEnv, maxSeedAdminsFile)
	}
	var file seedAdminFile
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("admins: %s is not a valid admins file (%s)", seedAdminsFileEnv, yamlErrorLine(err))
	}
	if len(file.Admins) == 0 {
		return nil, fmt.Errorf("admins: %s lists no admins", seedAdminsFileEnv)
	}
	if err := checkSeedAdmins(file.Admins); err != nil {
		return nil, err
	}
	return file.Admins, nil
}

// yamlErrorLine keeps a decode error's position and drops its text, which can
// quote a value from the file.
func yamlErrorLine(err error) string {
	var te *yaml.TypeError
	if errors.As(err, &te) {
		return fmt.Sprintf("%d field errors", len(te.Errors))
	}
	return "syntax error"
}

// checkSeedAdmins validates every admin and that no id or phone collides with
// another admin or a seeded user.
func checkSeedAdmins(admins []seedAdmin) error {
	usedIDs := map[ids.ID]string{}
	usedPhones := map[string]string{}
	usedKeys := map[string]bool{}
	for _, u := range identityUsers {
		usedIDs[mustID(u.ID)], usedIDs[phoneIdentityID(u.ID)] = u.Key, u.Key
		usedPhones[u.Phone] = u.Key
		usedKeys[u.Key] = true
	}
	for i := range admins {
		a := &admins[i]
		if a.Key == "" {
			return fmt.Errorf("admins: entry %d has no key", i+1)
		}
		if usedKeys[a.Key] {
			return fmt.Errorf("admin %s: the key is used twice", a.Key)
		}
		usedKeys[a.Key] = true
		id, err := ids.Parse(a.ID)
		if err != nil || len(a.ID) != 26 || a.ID[len(a.ID)-4] != 'N' {
			return fmt.Errorf("admin %s: id must be a ULID whose 23rd character is N", a.Key)
		}
		for _, x := range []ids.ID{id, phoneIdentityID(a.ID)} {
			if prev, dup := usedIDs[x]; dup {
				return fmt.Errorf("admin %s: id collides with %s", a.Key, prev)
			}
			usedIDs[x] = a.Key
		}
		e164, ok := pii.NormalizePhone(a.Phone)
		if !ok || !pii.IsReservedTestPhone(e164) {
			return fmt.Errorf("admin %s: the phone must be a reserved test number", a.Key)
		}
		if prev, dup := usedPhones[e164]; dup {
			return fmt.Errorf("admin %s: the phone is also %s's", a.Key, prev)
		}
		usedPhones[e164] = a.Key
		a.Phone = e164
		if a.Lang == "" {
			a.Lang = "en"
		}
		if len(a.Roles) == 0 {
			return fmt.Errorf("admin %s: no roles", a.Key)
		}
		for j, r := range a.Roles {
			if !authz.IsAdminRole(r) || slices.Contains(a.Roles[:j], r) {
				return fmt.Errorf("admin %s: role %q is not a D-64 admin role or is repeated", a.Key, r)
			}
		}
		if a.TOTPSecret != "" {
			secret, err := identity.DecodeTOTPSecret(a.TOTPSecret)
			if err != nil || len(secret) < minSeedTOTPSecret {
				return fmt.Errorf("admin %s: totp_secret must be base32 of at least %d bytes", a.Key, minSeedTOTPSecret)
			}
		}
	}
	return nil
}

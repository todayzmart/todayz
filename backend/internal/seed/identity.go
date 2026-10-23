package seed

import (
	"context"
	"fmt"
	"log/slog"

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
// cashier through identityUserID). Admins come from SEED_ADMINS_FILE in
// W1-01 phase B, never from here.
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
	q := d.DB.Q(ctx)
	for _, u := range identityUsers {
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
	}
	d.Log.DebugContext(ctx, "seed identity", slog.Int("users", len(identityUsers)))
	return nil
}

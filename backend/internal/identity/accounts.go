package identity

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/todayz/backend/gen/sqlc/identitydb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// Accounts finds or makes the account of a phone number for another domain:
// A-18 inviting an admin by number (W3-05). The account it makes is the one a
// first OTP sign-in makes, through the same queries, hash and seal.
type Accounts struct{ s *Service }

// NewAccounts returns Accounts on d. Both keys are required.
func NewAccounts(d *db.DB, seal *pii.Sealer, hash *pii.Hasher) (*Accounts, error) {
	if d == nil || seal == nil || hash == nil {
		return nil, errors.New("identity: accounts need the database, PII_ENC_KEY and PII_HASH_KEY")
	}
	return &Accounts{s: &Service{db: d, seal: seal, hash: hash}}, nil
}

// Account is a phone number's user.
type Account struct {
	UserID  ids.ID
	Status  string // users.status; "active" for an account made now
	Created bool
}

// FindOrCreateByPhone returns the user of an E.164 phone, making the user and
// its phone identity when the number is new. It runs in the caller's
// transaction and takes the per-number lock sign-in takes, so the two never
// make two users for one number.
func (a *Accounts) FindOrCreateByPhone(ctx context.Context, phone string, now time.Time) (Account, error) {
	e164, ok := pii.NormalizePhone(phone)
	if !ok {
		return Account{}, errors.New("identity: not an Indian mobile number")
	}
	q := identitydb.New(a.s.db.Q(ctx))
	hash := a.s.hash.Phone(e164)
	if err := q.LockKey(ctx, "identity:phone:"+hex.EncodeToString(hash)); err != nil {
		return Account{}, err
	}
	u, err := q.GetUserByPhoneHash(ctx, hash)
	switch {
	case db.IsNoRows(err):
		id := ids.New(now)
		if err := q.InsertUser(ctx, identitydb.InsertUserParams{ID: ids.UUID(id), Now: now}); err != nil {
			return Account{}, err
		}
		enc, err := a.s.seal.Seal("phone", []byte(e164))
		if err != nil {
			return Account{}, err
		}
		if err := q.InsertPhoneIdentity(ctx, identitydb.InsertPhoneIdentityParams{
			ID: ids.UUID(ids.New(now)), UserID: ids.UUID(id), PhoneEnc: enc, PhoneHash: hash, Now: now,
		}); err != nil {
			return Account{}, err
		}
		return Account{UserID: id, Status: "active", Created: true}, nil
	case err != nil:
		return Account{}, err
	}
	id, ok := ids.FromUUID(u.ID)
	if !ok {
		return Account{}, errors.New("identity: user without id")
	}
	// Lock the users row as sign-in does, and read the status under it.
	status, err := q.LockUserShared(ctx, u.ID)
	if err != nil {
		return Account{}, err
	}
	return Account{UserID: id, Status: status}, nil
}

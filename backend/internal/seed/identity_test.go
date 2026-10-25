package seed

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/testkit"
)

func testKeys(t *testing.T) config.AppKeys {
	t.Helper()
	enc, hash := make([]byte, 32), make([]byte, 32)
	_, err := rand.Read(enc)
	require.NoError(t, err)
	_, err = rand.Read(hash)
	require.NoError(t, err)
	return config.AppKeys{PIIEncKey: config.NewSecret(enc), PIIHashKey: config.NewSecret(hash)}
}

func TestIdentityFixedIDs(t *testing.T) {
	seen := map[ids.ID]string{}
	for _, u := range identityUsers {
		for _, id := range []ids.ID{mustID(u.ID), phoneIdentityID(u.ID)} {
			prev, dup := seen[id]
			require.False(t, dup, "%s reuses the id of %s", u.Key, prev)
			seen[id] = u.Key
		}
		require.Equal(t, mustID(u.ID), identityUserID(u.Key))
		e164, ok := pii.NormalizePhone(u.Phone)
		require.True(t, ok, u.Key)
		require.True(t, pii.IsReservedTestPhone(e164), "%s must use a reserved test number", u.Key)
	}
	require.Panics(t, func() { identityUserID("nobody") })
}

func TestIdentitySeedsSealedPhones(t *testing.T) {
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	keys := testKeys(t)
	d := Deps{DB: db.New(tdb.Pool), Keys: keys}
	require.NoError(t, Run(ctx, config.EnvTest, Dev, d))
	require.NoError(t, Run(ctx, config.EnvTest, Demo, d), "a second run is a no-op")

	seal, err := pii.NewSealer(keys.PIIEncKey.Bytes())
	require.NoError(t, err)
	hash, err := pii.NewHasher(keys.PIIHashKey.Bytes())
	require.NoError(t, err)

	var users, profiles int
	// Accounts with a phone; the asks seed adds bare shopper users (W3-16).
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT count(*) FROM users u
	  WHERE EXISTS (SELECT 1 FROM phone_identities p WHERE p.user_id = u.id)`).Scan(&users))
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT count(*) FROM customer_profiles`).Scan(&profiles))
	require.Equal(t, len(identityUsers), users)
	require.Equal(t, 3, profiles)

	for _, u := range identityUsers {
		var enc, h []byte
		require.NoError(t, tdb.Pool.QueryRow(ctx,
			`SELECT phone_enc, phone_hash FROM phone_identities WHERE user_id = $1`,
			ids.UUID(identityUserID(u.Key))).Scan(&enc, &h), u.Key)
		require.True(t, bytes.Equal(hash.Phone(u.Phone), h), "%s: sign-in finds the phone by its keyed hash", u.Key)
		plain, err := seal.Open("phone", enc)
		require.NoError(t, err, u.Key)
		require.Equal(t, u.Phone, string(plain))
		require.False(t, bytes.Contains(enc, []byte(u.Phone)), "%s: the phone is stored sealed", u.Key)
	}
}

func TestIdentitySkipsWithoutKeys(t *testing.T) {
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	require.NoError(t, Run(ctx, config.EnvTest, Test, Deps{DB: db.New(tdb.Pool)}))
	var users int
	// Accounts with a phone; the asks seed adds bare shopper users (W3-16).
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT count(*) FROM users u
	  WHERE EXISTS (SELECT 1 FROM phone_identities p WHERE p.user_id = u.id)`).Scan(&users))
	require.Zero(t, users)
}

package business

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/testkit"
)

// Synthetic numbers only (the reserved test range).
const (
	testStorePhone = "+919999900001"
	testOwnerPhone = "+919999900002"
)

func TestContacts(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	now := time.Date(2026, 1, 5, 4, 30, 0, 0, time.UTC)

	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	cfg := &config.Config{Env: config.EnvTest, Keys: config.AppKeys{PIIEncKey: config.NewSecret(key)}}
	sealer, err := pii.NewSealer(key)
	require.NoError(t, err)
	c, err := NewContacts(db.New(tdb.Pool), cfg)
	require.NoError(t, err)

	n := 0
	// fixture makes a business with an active owner (login phone, language)
	// and one store; storePhone "" leaves the store without a phone.
	fixture := func(storePhone, lang string) (store, biz, owner ids.ID) {
		owner, biz, store = ids.New(now), ids.New(now), ids.New(now)
		_, err := tdb.Pool.Exec(ctx, `INSERT INTO users (id, preferred_lang) VALUES ($1, $2)`, ids.UUID(owner), lang)
		require.NoError(t, err)
		enc, err := sealer.Seal(identityPhoneField, []byte(testOwnerPhone))
		require.NoError(t, err)
		_, err = tdb.Pool.Exec(ctx, `INSERT INTO phone_identities (id, user_id, phone_enc, phone_hash, verified_at) VALUES ($1, $2, $3, $4, $5)`,
			ids.UUID(ids.New(now)), ids.UUID(owner), enc, append(make([]byte, 31), byte(n)), now)
		require.NoError(t, err)
		_, err = tdb.Pool.Exec(ctx, `INSERT INTO businesses (id, brand_name, referral_code) VALUES ($1, 'Test Shop', $2)`,
			ids.UUID(biz), "CCCCC"+string(codeAlphabet[n]))
		require.NoError(t, err)
		_, err = tdb.Pool.Exec(ctx, `INSERT INTO staff (id, business_id, user_id, role, status) VALUES ($1, $2, $3, 'owner', 'active')`,
			ids.UUID(ids.New(now)), ids.UUID(biz), ids.UUID(owner))
		require.NoError(t, err)
		var phoneEnc []byte
		if storePhone != "" {
			phoneEnc, err = sealer.Seal(SealField(FieldPhone, store), []byte(storePhone))
			require.NoError(t, err)
		}
		_, err = tdb.Pool.Exec(ctx, `INSERT INTO stores (id, business_id, slug, short_code, name, phone_enc) VALUES ($1, $2, $3, $4, 'Lakshmi Traders', $5)`,
			ids.UUID(store), ids.UUID(biz), "shop-"+strings.ToLower(string(codeAlphabet[n])), "DDDDD"+string(codeAlphabet[n]), phoneEnc)
		require.NoError(t, err)
		n++
		return store, biz, owner
	}

	t.Run("the store's own phone wins", func(t *testing.T) {
		store, biz, owner := fixture(testStorePhone, "kn")
		got, ok, err := c.Contact(ctx, store)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, testStorePhone, got.Phone)
		require.Equal(t, "kn", got.Lang)
		require.Equal(t, owner, got.OwnerUserID)
		require.Equal(t, biz, got.BusinessID)
		require.Equal(t, "Lakshmi Traders", got.StoreName)
	})

	t.Run("falls back to the owner's phone; English is empty", func(t *testing.T) {
		store, _, owner := fixture("", "en")
		got, ok, err := c.Contact(ctx, store)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, testOwnerPhone, got.Phone)
		require.Equal(t, "", got.Lang)
		require.Equal(t, owner, got.OwnerUserID)
	})

	t.Run("no phone at all", func(t *testing.T) {
		store, biz, _ := fixture("", "hi")
		_, err := tdb.Pool.Exec(ctx, `DELETE FROM phone_identities`)
		require.NoError(t, err)
		got, ok, err := c.Contact(ctx, store)
		require.NoError(t, err)
		require.True(t, ok)
		require.Empty(t, got.Phone)
		require.Equal(t, biz, got.BusinessID)
		require.Equal(t, "hi", got.Lang)
	})

	t.Run("a phone sealed for another store does not open", func(t *testing.T) {
		store, _, _ := fixture(testStorePhone, "en")
		other, err := sealer.Seal(SealField(FieldPhone, ids.New(now)), []byte(testStorePhone))
		require.NoError(t, err)
		_, err = tdb.Pool.Exec(ctx, `UPDATE stores SET phone_enc = $2 WHERE id = $1`, ids.UUID(store), other)
		require.NoError(t, err)
		got, ok, err := c.Contact(ctx, store)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, testOwnerPhone, got.Phone, "falls back instead of failing")
	})

	t.Run("unknown store", func(t *testing.T) {
		_, ok, err := c.Contact(ctx, ids.New(now))
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("needs the key", func(t *testing.T) {
		_, err := NewContacts(db.New(tdb.Pool), &config.Config{})
		require.Error(t, err)
	})
}

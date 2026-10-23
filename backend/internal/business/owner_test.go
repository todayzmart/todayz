package business

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// TestOwnsActiveBusiness: the account-deletion check of W1-01 phase B. An
// active owner of a business in any state but rejected owns an active
// business; a manager, a removed owner and the owner of a rejected business
// do not.
func TestOwnsActiveBusiness(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	d := db.New(tdb.Pool)
	now := time.Date(2026, 1, 5, 4, 30, 0, 0, time.UTC)

	user := func() ids.ID {
		id := ids.New(now)
		_, err := tdb.Pool.Exec(ctx, `INSERT INTO users (id) VALUES ($1)`, ids.UUID(id))
		require.NoError(t, err)
		return id
	}
	n := 0
	business := func(status string) ids.ID {
		id := ids.New(now)
		code := "AAAAA" + string(codeAlphabet[n]) // unique referral codes
		n++
		_, err := tdb.Pool.Exec(ctx, `INSERT INTO businesses (id, brand_name, status, referral_code) VALUES ($1, 'Test Shop', $2, $3)`,
			ids.UUID(id), status, code)
		require.NoError(t, err)
		return id
	}
	staff := func(bid, uid ids.ID, role, status string) {
		var removed *time.Time
		if status == "removed" {
			removed = &now
		}
		_, err := tdb.Pool.Exec(ctx, `INSERT INTO staff (id, business_id, user_id, role, status, removed_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			ids.UUID(ids.New(now)), ids.UUID(bid), ids.UUID(uid), role, status, removed)
		require.NoError(t, err)
	}
	owns := func(uid ids.ID) bool {
		ok, err := OwnsActiveBusiness(ctx, d, uid)
		require.NoError(t, err)
		return ok
	}

	for _, status := range []string{"draft", "submitted", "needs_info", "verified", "suspended"} {
		u := user()
		staff(business(status), u, "owner", "active")
		require.True(t, owns(u), status)
	}

	rejected := user()
	staff(business("rejected"), rejected, "owner", "active")
	require.False(t, owns(rejected), "rejected business")

	removed := user()
	staff(business("verified"), removed, "owner", "removed")
	require.False(t, owns(removed), "removed owner")

	manager := user()
	staff(business("verified"), manager, "manager", "active")
	require.False(t, owns(manager), "manager")

	nobody := user()
	require.False(t, owns(nobody), "no staff row")

	_, err := OwnsActiveBusiness(ctx, nil, nobody)
	require.Error(t, err, "no database")
}

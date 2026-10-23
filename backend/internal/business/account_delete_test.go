package business

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/testkit"
)

// TestOnAccountDelete: deleting an account withdraws the invites bound to it
// and those waiting for its phone, clears their phone, leaves everything else
// alone, and runs twice without harm (review M2).
func TestOnAccountDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	d := db.New(tdb.Pool)
	now := time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now)

	hashOf := func(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }
	user := func(phoneHash []byte) ids.ID {
		id := ids.New(now)
		_, err := tdb.Pool.Exec(ctx, `INSERT INTO users (id) VALUES ($1)`, ids.UUID(id))
		require.NoError(t, err)
		if phoneHash != nil {
			_, err = tdb.Pool.Exec(ctx, `INSERT INTO phone_identities (id, user_id, phone_enc, phone_hash, verified_at) VALUES ($1, $2, '\x01', $3, $4)`,
				ids.UUID(ids.New(now)), ids.UUID(id), phoneHash, now)
			require.NoError(t, err)
		}
		return id
	}
	n := 0
	business := func() ids.ID {
		id := ids.New(now)
		code := "BBBBB" + string(codeAlphabet[n])
		n++
		_, err := tdb.Pool.Exec(ctx, `INSERT INTO businesses (id, brand_name, status, referral_code) VALUES ($1, 'Test Shop', 'verified', $2)`,
			ids.UUID(id), code)
		require.NoError(t, err)
		return id
	}
	// staff inserts a row; uid and hash may each be absent.
	staff := func(bid ids.ID, uid *ids.ID, hash []byte, role, status string) ids.ID {
		id := ids.New(now)
		var user any
		if uid != nil {
			user = ids.UUID(*uid)
		}
		var enc []byte
		if hash != nil {
			enc = []byte{0x02}
		}
		var invitedAt *time.Time
		if status == "invited" {
			invitedAt = &now
		}
		_, err := tdb.Pool.Exec(ctx, `INSERT INTO staff (id, business_id, user_id, role, status, invite_phone_hash, invite_phone_enc, invited_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			ids.UUID(id), ids.UUID(bid), user, role, status, hash, enc, invitedAt)
		require.NoError(t, err)
		return id
	}
	type state struct {
		status  string
		hasHash bool
		removed bool
	}
	stateOf := func(id ids.ID) state {
		var s state
		require.NoError(t, tdb.Pool.QueryRow(ctx,
			`SELECT status, invite_phone_hash IS NOT NULL AND invite_phone_enc IS NOT NULL, removed_at IS NOT NULL FROM staff WHERE id = $1`,
			ids.UUID(id)).Scan(&s.status, &s.hasHash, &s.removed))
		return s
	}

	mine := hashOf(1)
	gone := user(mine)
	other := user(hashOf(2))
	b1, b2, b3, b4 := business(), business(), business(), business()

	boundToUser := staff(b1, &gone, hashOf(9), "cashier", "invited") // bound, typed phone since changed
	waitingForPhone := staff(b2, nil, mine, "manager", "invited")
	activeMember := staff(b3, &gone, nil, "cashier", "active")
	otherInvite := staff(b3, nil, hashOf(3), "cashier", "invited")
	otherUsersInvite := staff(b4, &other, hashOf(2), "cashier", "invited")
	ownerRow := staff(b4, &gone, nil, "owner", "active")

	for range 2 {
		require.NoError(t, OnAccountDelete(ctx, d, clk, gone))
		require.Equal(t, state{"removed", false, true}, stateOf(boundToUser), "an invite bound to the account")
		require.Equal(t, state{"removed", false, true}, stateOf(waitingForPhone), "an invite waiting for its phone")
		require.Equal(t, state{"active", false, false}, stateOf(activeMember), "an active membership is not an invite")
		require.Equal(t, state{"active", false, false}, stateOf(ownerRow))
		require.Equal(t, state{"invited", true, false}, stateOf(otherInvite), "another phone's invite")
		require.Equal(t, state{"invited", true, false}, stateOf(otherUsersInvite), "another account's invite")
	}

	// An account without a phone identity: only the bound invites go.
	nophone := user(nil)
	bound := staff(b2, &nophone, nil, "cashier", "invited")
	require.NoError(t, OnAccountDelete(ctx, d, clk, nophone))
	require.Equal(t, state{"removed", false, true}, stateOf(bound))
	require.Equal(t, state{"invited", true, false}, stateOf(otherInvite))

	// Through the service, as identity's hook calls it.
	svc := &Service{db: d, clock: clk}
	require.NoError(t, svc.OnAccountDelete(ctx, gone))
	require.Error(t, OnAccountDelete(ctx, nil, clk, gone), "no database")
}

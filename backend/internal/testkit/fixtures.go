package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/todayz/backend/internal/platform/ids"
)

// Parent-row fixtures. The wave 1 and 2 constraint migrations (D-211) make
// the plain uuid columns real foreign keys, so a test that writes a child row
// (a hold, a redemption, a referral, a job run) first needs the parent. These
// helpers insert the smallest valid parent with raw SQL as todayz_owner, so
// testkit stays free of domain packages. Each takes the id to use (ids.Zero
// makes a fresh one), creates nothing that is not named, is idempotent for an
// id that already exists (ON CONFLICT DO NOTHING), and returns the id. The
// stores, businesses and users a parent points at are the caller's to create
// (InsertUser makes a bare user).

func (d *DB) fixtureExec(t testing.TB, sql string, args ...any) {
	t.Helper()
	if _, err := d.OwnerPool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("testkit: fixture: %v", err)
	}
}

func fixtureID(id ids.ID) ids.ID {
	if id == ids.Zero {
		return ids.New(time.Now())
	}
	return id
}

// InsertUser inserts a bare user.
func (d *DB) InsertUser(t testing.TB, id ids.ID) ids.ID {
	t.Helper()
	id = fixtureID(id)
	d.fixtureExec(t, `INSERT INTO users (id) VALUES ($1) ON CONFLICT DO NOTHING`, ids.UUID(id))
	return id
}

// InsertOrder inserts a placed pickup order of one rupee for customer at
// store (both must exist).
func (d *DB) InsertOrder(t testing.TB, id, customer, store ids.ID) ids.ID {
	t.Helper()
	id = fixtureID(id)
	d.fixtureExec(t, `INSERT INTO orders (id, customer_id, store_id, fulfilment, subtotal_paise,
		  items_after_offers_paise, item_subtotal_paise, total_paise, code_enc, code_hmac, placed_at)
		VALUES ($1, $2, $3, 'pickup', 100, 100, 100, 100, '\x00', sha256(($1::uuid)::text::bytea), now())
		ON CONFLICT DO NOTHING`, ids.UUID(id), ids.UUID(customer), ids.UUID(store))
	return id
}

// InsertPosSale inserts a completed cash sale of one rupee at store rung up
// by cashier (both must exist), with no linked customer.
func (d *DB) InsertPosSale(t testing.TB, id, store, cashier ids.ID) ids.ID {
	t.Helper()
	id = fixtureID(id)
	d.fixtureExec(t, `INSERT INTO pos_sales (id, store_id, client_id, device_id, cashier_user_id,
		  item_subtotal_paise, subtotal_paise, total_paise, payment_method, sold_at, received_at)
		VALUES ($1, $2, gen_random_uuid(), gen_random_uuid(), $3, 100, 100, 100, 'cash', now(), now())
		ON CONFLICT DO NOTHING`, ids.UUID(id), ids.UUID(store), ids.UUID(cashier))
	return id
}

// InsertBillClaim inserts an open bill QR claim of one rupee at store created
// by createdBy (both must exist).
func (d *DB) InsertBillClaim(t testing.TB, id, store, createdBy ids.ID) ids.ID {
	t.Helper()
	id = fixtureID(id)
	d.fixtureExec(t, `INSERT INTO bill_claims (id, store_id, sale_client_id, amount_paise, token_nonce,
		  expires_at, created_by)
		VALUES ($1, $2, gen_random_uuid(), 100, substring(sha256(($1::uuid)::text::bytea) from 1 for 16), now() + interval '1 hour', $3)
		ON CONFLICT DO NOTHING`, ids.UUID(id), ids.UUID(store), ids.UUID(createdBy))
	return id
}

// InsertLedgerTx inserts a ledger transaction of the given kind (one of the
// ck_ledger_transactions_kind values) with no entries, under a unique
// idempotency key.
func (d *DB) InsertLedgerTx(t testing.TB, id ids.ID, kind string) ids.ID {
	t.Helper()
	id = fixtureID(id)
	d.fixtureExec(t, `INSERT INTO ledger_transactions (id, kind, idempotency_key)
		VALUES ($1, $2, 'testkit:' || ($1::uuid)::text) ON CONFLICT DO NOTHING`, ids.UUID(id), kind)
	return id
}

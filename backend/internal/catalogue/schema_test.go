package catalogue

import (
	"context"
	"strings"
	"testing"

	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// TestSchemaTriggers checks the ledger triggers directly in SQL, as todayz_app:
// a movement moves on_hand and the freshness moment, an allocation row moves
// its batch, the ledgers refuse UPDATE and DELETE, a movement for another
// store's item and an allocation that overdraws a batch are refused, and a
// catalogue seq does not touch stores.updated_at.
func TestSchemaTriggers(t *testing.T) {
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	exec := func(sql string) error { _, err := tdb.Pool.Exec(ctx, sql); return err }
	must := func(sql string) {
		t.Helper()
		if err := exec(sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	mustFail := func(sql, want string) {
		t.Helper()
		err := exec(sql)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: want error containing %q, got %v", sql, want, err)
		}
	}
	const (
		biz   = "'01900000-0000-7000-8000-0000000000b1'"
		st    = "'01900000-0000-7000-8000-0000000000c1'"
		st2   = "'01900000-0000-7000-8000-0000000000c2'"
		prod  = "'01900000-0000-7000-8000-0000000000d1'"
		item  = "'01900000-0000-7000-8000-0000000000e1'"
		batch = "'01900000-0000-7000-8000-0000000000f1'"
		mv1   = "'01900000-0000-7000-8000-000000000101'"
		mv2   = "'01900000-0000-7000-8000-000000000102'"
		mv3   = "'01900000-0000-7000-8000-000000000103'"
	)
	must(`INSERT INTO businesses (id, brand_name, referral_code) VALUES (` + biz + `, 'B', 'ABCDEF')`)
	must(`INSERT INTO stores (id, business_id, slug, short_code, name, updated_at) VALUES
	  (` + st + `, ` + biz + `, 's1', 'ABCDE2', 'S1', '2026-01-01'),
	  (` + st2 + `, ` + biz + `, 's2', 'ABCDE3', 'S2', '2026-01-01')`)
	must(`INSERT INTO products (id, name, category_id, pack_size, unit) VALUES
	  (` + prod + `, 'Atta', '0190c47e-0000-7000-8000-000000000101', 5, 'kg')`)
	must(`INSERT INTO store_items (id, store_id, product_id, price_paise) VALUES (` + item + `, ` + st + `, ` + prod + `, 25000)`)

	must(`INSERT INTO stock_movements (id, store_id, store_item_id, delta, source, created_at)
	  VALUES (` + mv1 + `, ` + st + `, ` + item + `, 10, 'restock', '2026-10-20T10:00:00Z')`)
	must(`INSERT INTO stock_batches (id, store_item_id, received_quantity, expires_on, received_at)
	  VALUES (` + batch + `, ` + item + `, 10, '2026-11-01', '2026-10-20T10:00:00Z')`)
	must(`INSERT INTO stock_movement_batches (movement_id, batch_id, delta) VALUES (` + mv1 + `, ` + batch + `, 10)`)

	var onHand, qty int
	if err := tdb.Pool.QueryRow(ctx, `SELECT si.on_hand, b.quantity FROM store_items si, stock_batches b
	  WHERE si.id = `+item+` AND b.id = `+batch).Scan(&onHand, &qty); err != nil {
		t.Fatal(err)
	}
	if onHand != 10 || qty != 10 {
		t.Fatalf("on_hand %d batch %d, want 10 10", onHand, qty)
	}

	mustFail(`INSERT INTO stock_movements (id, store_id, store_item_id, delta, source, created_at)
	  VALUES (`+mv2+`, `+st2+`, `+item+`, -1, 'damage', '2026-10-20T11:00:00Z')`, "is not in store")
	must(`INSERT INTO stock_movements (id, store_id, store_item_id, delta, source, created_at)
	  VALUES (` + mv2 + `, ` + st + `, ` + item + `, -3, 'damage', '2026-10-20T11:00:00Z')`)
	mustFail(`INSERT INTO stock_movement_batches (movement_id, batch_id, delta) VALUES (`+mv2+`, `+batch+`, 3)`, "sign")
	mustFail(`INSERT INTO stock_movement_batches (movement_id, batch_id, delta) VALUES (`+mv2+`, `+batch+`, -4)`, "exceeds")
	must(`INSERT INTO stock_movement_batches (movement_id, batch_id, delta) VALUES (` + mv2 + `, ` + batch + `, -3)`)
	must(`INSERT INTO stock_movements (id, store_id, store_item_id, delta, source, created_at)
	  VALUES (` + mv3 + `, ` + st + `, ` + item + `, -20, 'sale_pos', '2026-10-20T12:00:00Z')`)
	mustFail(`INSERT INTO stock_movement_batches (movement_id, batch_id, delta) VALUES (`+mv3+`, `+batch+`, -8)`, "ck_stock_batches_quantity")

	mustFail(`UPDATE stock_movements SET note = 'x'`, "permission denied")
	mustFail(`DELETE FROM stock_movement_batches`, "permission denied")
	if _, err := tdb.OwnerPool.Exec(ctx, `DELETE FROM stock_movements`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("owner delete: %v", err)
	}

	must(`INSERT INTO catalogue_changes (store_id, seq, entity, entity_id, op, created_at)
	  SELECT id, catalogue_seq + 1, 'store_item', ` + item + `, 'upsert', '2026-10-20' FROM stores WHERE id = ` + st)
	must(`UPDATE stores SET catalogue_seq = catalogue_seq + 1 WHERE id = ` + st)
	var touched bool
	if err := tdb.Pool.QueryRow(ctx, `SELECT updated_at > '2026-01-02' FROM stores WHERE id = `+st).Scan(&touched); err != nil {
		t.Fatal(err)
	}
	if touched {
		t.Fatal("a catalogue seq moved stores.updated_at")
	}
	must(`UPDATE stores SET name = 'S1b' WHERE id = ` + st)
	if err := tdb.Pool.QueryRow(ctx, `SELECT updated_at > '2026-01-02' FROM stores WHERE id = `+st).Scan(&touched); err != nil {
		t.Fatal(err)
	}
	if !touched {
		t.Fatal("a store edit did not move stores.updated_at")
	}
}

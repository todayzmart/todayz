package migrate_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	dbschema "github.com/todayz/backend/db"
	"github.com/todayz/backend/internal/platform/migrate"
)

// TestMigrationBlockOrder (D-90, D-100 L5): no migration in db/migrations
// touches a table that a higher block, or a later file of its own block,
// creates. Blocks merge in any order, so such a file could run before the
// table exists.
func TestMigrationBlockOrder(t *testing.T) {
	entries, err := fs.ReadDir(dbschema.Migrations, "migrations")
	require.NoError(t, err)
	files := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := fs.ReadFile(dbschema.Migrations, "migrations/"+e.Name())
		require.NoError(t, err)
		files[e.Name()] = string(b)
	}
	require.GreaterOrEqual(t, len(files), 2)
	require.NoError(t, migrate.CheckBlockOrder(files))
}

const (
	createOrders = "-- +goose Up\nCREATE TABLE orders (id int);\n-- +goose Down\nDROP TABLE orders;\n"
	createUsers  = "-- +goose Up\nCREATE TABLE IF NOT EXISTS public.\"users\" (id int);\nCREATE INDEX ix_users ON users (id);\n-- +goose Down\nDROP TABLE users;\n"
)

// TestCheckBlockOrderFires is the fixture: every way a file can reach into a
// block it may not rely on, and the shapes that are allowed.
func TestCheckBlockOrderFires(t *testing.T) {
	good := map[string]string{
		"00001_w0-03_baseline.sql":  createUsers,
		"00002_w0-03_seed.sql":      "-- +goose Up\nINSERT INTO users (id) VALUES (1);\nUPDATE users SET id = 2 WHERE id = 1;\n-- +goose Down\nDELETE FROM users;\n",
		"10101_w1-01_profiles.sql":  "-- +goose Up\nALTER TABLE users ADD COLUMN name text;\nCREATE TABLE profiles (id int);\nCREATE TRIGGER trg BEFORE UPDATE ON profiles FOR EACH ROW EXECUTE FUNCTION f();\n-- +goose Down\nDROP TABLE profiles;\nALTER TABLE users DROP COLUMN name;\n",
		"10201_w1-02_orders.sql":    createOrders,
		"10202_w1-02_orders_fk.sql": "-- +goose Up\nALTER TABLE ONLY orders ADD COLUMN user_id int REFERENCES users (id);\nREVOKE UPDATE, DELETE ON orders, users FROM todayz_app;\nCOMMENT ON TABLE orders IS 'x';\n-- +goose Down\nALTER TABLE orders DROP COLUMN user_id;\n",
		"10301_w1-03_events.sql":    "-- +goose Up\nCREATE TABLE events (id int, at timestamptz) PARTITION BY RANGE (at);\nCREATE TABLE events_default PARTITION OF events DEFAULT;\nINSERT INTO orders (id) VALUES (1);\n-- +goose Down\nDROP TABLE events;\n",
		"20101_w2-01_cleanup.sql":   "-- +goose Up\nDROP TABLE IF EXISTS profiles, events;\n-- A comment mentioning ALTER TABLE nothing;\nSELECT 'ALTER TABLE nothing';\n-- +goose StatementBegin\nDO $$ BEGIN EXECUTE format('ALTER TABLE %I ADD COLUMN x int', 'nothing'); END $$;\n-- +goose StatementEnd\nREVOKE EXECUTE ON FUNCTION f() FROM PUBLIC;\n-- +goose Down\n",
		// D-100 round 2, Low 14: references, policies and views over lower blocks and views.
		"10203_w1-02_lines.sql": "-- +goose Up\nCREATE TABLE order_lines (id int, order_id int REFERENCES orders (id), user_id int REFERENCES public.\"users\" (id));\nALTER TABLE order_lines ADD CONSTRAINT fk FOREIGN KEY (order_id) REFERENCES orders (id);\nCREATE POLICY own ON order_lines FOR SELECT USING (true);\nCREATE VIEW order_totals AS SELECT o.id, count(*) FROM orders o JOIN order_lines l ON l.order_id = o.id LEFT JOIN users u ON u.id = o.id, generate_series(1, 2) g GROUP BY o.id;\nCREATE MATERIALIZED VIEW order_totals_m AS SELECT * FROM order_totals;\nCREATE OR REPLACE VIEW v2 AS WITH c AS (SELECT 1) SELECT * FROM c;\n-- +goose Down\n",
	}
	require.NoError(t, migrate.CheckBlockOrder(good))

	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"higher block": {
			files: map[string]string{
				"10101_w1-01_users.sql":  "-- +goose Up\nALTER TABLE orders ADD COLUMN x int;\n-- +goose Down\n",
				"10201_w1-02_orders.sql": createOrders,
			},
			want: "10101_w1-01_users.sql touches table orders, created by 10201_w1-02_orders.sql in a higher block",
		},
		"later file of the same block": {
			files: map[string]string{
				"10201_w1-02_fk.sql":     "-- +goose Up\nCREATE INDEX ix ON orders (id);\n-- +goose Down\n",
				"10202_w1-02_orders.sql": createOrders,
			},
			want: "10201_w1-02_fk.sql touches table orders, created later in the same block by 10202_w1-02_orders.sql",
		},
		"never created": {
			files: map[string]string{
				"10201_w1-02_seed.sql": "-- +goose Up\nINSERT INTO orders (id) VALUES (1);\n-- +goose Down\n",
			},
			want: "10201_w1-02_seed.sql touches table orders, which no migration creates",
		},
		"partition of a higher block": {
			files: map[string]string{
				"10101_w1-01_part.sql":   "-- +goose Up\nCREATE TABLE events_2026 PARTITION OF events FOR VALUES FROM ('a') TO ('b');\n-- +goose Down\n",
				"10301_w1-03_events.sql": "-- +goose Up\nCREATE TABLE events (id int);\n-- +goose Down\n",
			},
			want: "10101_w1-01_part.sql touches table events, created by 10301_w1-03_events.sql in a higher block",
		},
		"second name of a list": {
			files: map[string]string{
				"10101_w1-01_users.sql":  "-- +goose Up\nCREATE TABLE users (id int);\nREVOKE UPDATE ON users, orders FROM todayz_app;\n-- +goose Down\n",
				"10201_w1-02_orders.sql": createOrders,
			},
			want: "10101_w1-01_users.sql touches table orders, created by 10201_w1-02_orders.sql in a higher block",
		},
		"a trigger on a higher block": {
			files: map[string]string{
				"10101_w1-01_trg.sql":    "-- +goose Up\nCREATE TRIGGER trg AFTER INSERT OR UPDATE ON public.orders FOR EACH ROW EXECUTE FUNCTION f();\n-- +goose Down\n",
				"10201_w1-02_orders.sql": createOrders,
			},
			want: "10101_w1-01_trg.sql touches table orders, created by 10201_w1-02_orders.sql in a higher block",
		},
		"wave 0 reaching into wave 1": {
			files: map[string]string{
				"00011_w0-03_seed.sql":   "-- +goose Up\nINSERT INTO orders (id) VALUES (1);\n-- +goose Down\n",
				"10201_w1-02_orders.sql": createOrders,
			},
			want: "00011_w0-03_seed.sql touches table orders, created by 10201_w1-02_orders.sql in a higher block",
		},
		"bad file name": {
			files: map[string]string{"orders.sql": createOrders},
			want:  "orders.sql: not a migration file name",
		},
		// D-100 round 2, Low 14.
		"REFERENCES in CREATE TABLE to a higher block": {
			files: map[string]string{
				"10101_w1-01_lines.sql":  "-- +goose Up\nCREATE TABLE lines (id int, order_id int NOT NULL REFERENCES orders (id) ON DELETE CASCADE);\n-- +goose Down\n",
				"10201_w1-02_orders.sql": createOrders,
			},
			want: "10101_w1-01_lines.sql touches table orders, created by 10201_w1-02_orders.sql in a higher block",
		},
		"REFERENCES in ALTER TABLE to a later file": {
			files: map[string]string{
				"10201_w1-02_fk.sql":     "-- +goose Up\nCREATE TABLE lines (id int, order_id int);\nALTER TABLE lines ADD CONSTRAINT fk FOREIGN KEY (order_id) REFERENCES orders (id);\n-- +goose Down\n",
				"10202_w1-02_orders.sql": createOrders,
			},
			want: "10201_w1-02_fk.sql touches table orders, created later in the same block by 10202_w1-02_orders.sql",
		},
		"CREATE POLICY on a higher block": {
			files: map[string]string{
				"10101_w1-01_pol.sql":    "-- +goose Up\nCREATE POLICY own ON public.orders FOR ALL USING (true);\n-- +goose Down\n",
				"10201_w1-02_orders.sql": createOrders,
			},
			want: "10101_w1-01_pol.sql touches table orders, created by 10201_w1-02_orders.sql in a higher block",
		},
		"CREATE VIEW from a higher block": {
			files: map[string]string{
				"10101_w1-01_view.sql":   "-- +goose Up\nCREATE VIEW recent AS SELECT id FROM orders WHERE id > 1;\n-- +goose Down\n",
				"10201_w1-02_orders.sql": createOrders,
			},
			want: "10101_w1-01_view.sql touches table orders, created by 10201_w1-02_orders.sql in a higher block",
		},
		"CREATE MATERIALIZED VIEW joining a higher block": {
			files: map[string]string{
				"10101_w1-01_users.sql":  "-- +goose Up\nCREATE TABLE users (id int);\nCREATE MATERIALIZED VIEW totals AS SELECT u.id FROM users u INNER JOIN orders o ON o.id = u.id;\n-- +goose Down\n",
				"10201_w1-02_orders.sql": createOrders,
			},
			want: "10101_w1-01_users.sql touches table orders, created by 10201_w1-02_orders.sql in a higher block",
		},
		"a view over a view nobody creates": {
			files: map[string]string{
				"10201_w1-02_view.sql": "-- +goose Up\nCREATE VIEW v2 AS SELECT * FROM v1;\n-- +goose Down\n",
			},
			want: "10201_w1-02_view.sql touches table v1, which no migration creates",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := migrate.CheckBlockOrder(c.files)
			require.Error(t, err)
			require.Contains(t, err.Error(), c.want)
		})
	}

	// Down sections are not read: dropping a lower block's table in Down is the
	// normal shape, and a Down that mentions a higher block's table is not a
	// dependency at Up time.
	downOnly := map[string]string{
		"10101_w1-01_users.sql":  "-- +goose Up\nCREATE TABLE users (id int);\n-- +goose Down\nDROP TABLE users;\nALTER TABLE orders DROP COLUMN x;\n",
		"10201_w1-02_orders.sql": createOrders,
	}
	require.NoError(t, migrate.CheckBlockOrder(downOnly))
}

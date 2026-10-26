-- W4-02 load data: 1,000 live stores (load-0001 … load-1000) with about 250 stocked items each,
-- 50,000 customers, and two weeks of history at the launch target (08 §6: 5,000 orders a day,
-- 50,000 POS sales a day; 14 days of collected orders, 7 days of POS sales).
--
-- Run by `make load-data` (mk/70-load.mk) after `make reset`, as the owner role (the app role
-- may not create the temp tables this needs, nor ANALYZE):
--   psql "$DATABASE_MIGRATE_URL" -v ON_ERROR_STOP=1 -v history=1 -f generate.sql
-- One transaction (BEGIN … COMMIT below): the workers never see a half-made store, and nothing
-- is left on failure.
--
-- Refuses unless platform_environment says local or test, so it never runs in prod. A second
-- run is a no-op (load-0001 exists). The shop people (pools A, B, C) are signed in through the
-- API afterwards (scenarios/bootstrap.js) and linked by data/link_staff.sql, because a phone
-- identity needs the API's keyed hash and sealed phone.
--
-- Generated customers have no phone identity for the same reason (a random sealed phone could
-- not be opened by a shop screen); the customers the scenarios sign in are real API sign-ins.
-- History orders and sales carry no cashback and no credits, so the credits ledger stays empty
-- and the reconcile in checks/order_burst.sql sees only what the run itself did.

\set ON_ERROR_STOP 1
\if :{?history}
\else
  \set history 1
\endif

DO $$
BEGIN
  IF (SELECT env FROM platform_environment) NOT IN ('local', 'test') THEN
    RAISE EXCEPTION 'generate.sql: platform_environment is not local or test; refusing';
  END IF;
END
$$;

SELECT EXISTS (SELECT 1 FROM stores WHERE slug = 'load-0001') AS load_done \gset
\if :load_done
  \echo 'generate.sql: load data already present (load-0001); nothing to do'
  \quit
\endif

\timing on
BEGIN;
SELECT setseed(0.4202);

-- A ULID (as uuid) at a given time: 48 bits of milliseconds, 80 random bits.
CREATE FUNCTION pg_temp.ulid_at(ts timestamptz) RETURNS uuid LANGUAGE sql VOLATILE AS $$
  SELECT (lpad(to_hex((extract(epoch FROM ts) * 1000)::bigint), 12, '0')
          || right(replace(gen_random_uuid()::text, '-', ''), 20))::uuid
$$;

-- n in base 31 over the referral/short-code alphabet, left-padded to `len` with its first letter.
CREATE FUNCTION pg_temp.code31(n bigint, len int) RETURNS text LANGUAGE sql IMMUTABLE AS $$
  SELECT lpad(string_agg(substr('ABCDEFGHJKMNPQRSTUVWXYZ23456789', ((n / (31 ^ k)::bigint) % 31)::int + 1, 1), '' ORDER BY k DESC), len, 'A')
  FROM generate_series(0, len - 1) AS k
$$;

------------------------------------------------------------------------------
-- Businesses and stores
------------------------------------------------------------------------------

CREATE TEMP TABLE load_city (name text, lat float8, lng float8, pincode text, first int, last int) ON COMMIT DROP;
INSERT INTO load_city VALUES
  ('Bengaluru', 12.9716, 77.5946, '560001', 1, 400),
  ('Chennai', 13.0827, 80.2707, '600001', 401, 750),
  ('Hyderabad', 17.3850, 78.4867, '500001', 751, 1000);

CREATE TEMP TABLE load_store ON COMMIT DROP AS
WITH n AS (
  SELECT g AS n, random() AS rc, random() AS rr, random() AS ra, random() AS rn, random() AS rb
  FROM generate_series(1, 1000) AS g
)
SELECT n.n,
       pg_temp.ulid_at(now() - interval '120 days' + n.n * interval '1 minute') AS business_id,
       pg_temp.ulid_at(now() - interval '120 days' + n.n * interval '1 minute' + interval '1 second') AS store_id,
       c.name AS city, c.pincode,
       -- within about 8 km of the city centre
       c.lat + (8.0 * sqrt(n.rr) * cos(2 * pi() * n.ra)) / 111.0 AS lat,
       c.lng + (8.0 * sqrt(n.rr) * sin(2 * pi() * n.ra)) / (111.0 * cos(radians(c.lat))) AS lng,
       CASE WHEN n.rc < 0.55 THEN 'grocery' WHEN n.rc < 0.70 THEN 'pharmacy' WHEN n.rc < 0.78 THEN 'beauty'
            WHEN n.rc < 0.85 THEN 'electronics' WHEN n.rc < 0.92 THEN 'stationery' ELSE 'hardware' END AS category,
       (ARRAY['Sri Lakshmi', 'Ganesh', 'Balaji', 'Annapurna', 'New India', 'Om Sai', 'Krishna', 'Venkateswara',
              'Royal', 'City', 'Murugan', 'Mahalakshmi', 'Sai Ram', 'Durga', 'Green Leaf', 'Daily Fresh'])[1 + floor(n.rn * 16)::int] AS first_word,
       (100 + 50 * floor(n.rb * 5))::int AS cashback_bp
FROM n JOIN load_city c ON n.n BETWEEN c.first AND c.last;

INSERT INTO businesses (id, brand_name, category_id, status, referral_code, submitted_at, verified_at, created_at, updated_at)
SELECT s.business_id,
       s.first_word || ' ' || CASE s.category WHEN 'grocery' THEN 'Kirana' WHEN 'pharmacy' THEN 'Medicals'
         WHEN 'beauty' THEN 'Beauty' WHEN 'electronics' THEN 'Electronics' WHEN 'stationery' THEN 'Stationers'
         ELSE 'Hardware' END || ' ' || lpad(s.n::text, 4, '0'),
       c.id, 'verified', 'Y' || pg_temp.code31(s.n, 5),
       now() - interval '120 days', now() - interval '119 days', now() - interval '120 days', now() - interval '119 days'
FROM load_store s JOIN categories c ON c.slug = s.category AND c.parent_id IS NULL;

INSERT INTO stores (id, business_id, slug, short_code, name, address_line1, locality, city, pincode, location, hours,
                    status, live_at, accepts_pickup, cashback_bp, created_at, updated_at)
SELECT s.store_id, s.business_id, 'load-' || lpad(s.n::text, 4, '0'), 'X' || pg_temp.code31(s.n, 5),
       b.brand_name, 'Shop ' || s.n || ', Main Road', 'Ward ' || (1 + s.n % 40), s.city, s.pincode,
       ST_SetSRID(ST_MakePoint(s.lng, s.lat), 4326)::geography,
       '{"weekly":{"mon":[{"open":"00:00","close":"00:00"}],"tue":[{"open":"00:00","close":"00:00"}],"wed":[{"open":"00:00","close":"00:00"}],"thu":[{"open":"00:00","close":"00:00"}],"fri":[{"open":"00:00","close":"00:00"}],"sat":[{"open":"00:00","close":"00:00"}],"sun":[{"open":"00:00","close":"00:00"}]},"holidays":[]}'::jsonb,
       'live', now() - interval '118 days', true, s.cashback_bp, now() - interval '120 days', now() - interval '118 days'
FROM load_store s JOIN businesses b ON b.id = s.business_id;

------------------------------------------------------------------------------
-- Catalogue and stock: about 250 items a store, mostly from its own category
------------------------------------------------------------------------------

CREATE TEMP TABLE load_product ON COMMIT DROP AS
SELECT p.id, p.mrp_paise, top.slug AS category,
       count(*) OVER (PARTITION BY top.slug) AS in_category
FROM products p
JOIN categories c ON c.id = p.category_id
JOIN categories top ON top.id = coalesce(c.parent_id, c.id)
WHERE p.status = 'active' AND p.merged_into IS NULL AND p.mrp_paise IS NOT NULL;

CREATE TEMP TABLE load_item ON COMMIT DROP AS
SELECT pg_temp.ulid_at(now() - interval '117 days') AS id, s.store_id, s.n, p.id AS product_id, p.mrp_paise,
       -- most items at MRP, some a little under (never above: price <= mrp)
       greatest(1, CASE WHEN random() < 0.7 THEN p.mrp_paise ELSE round(p.mrp_paise * (0.90 + random() * 0.10)) END)::bigint AS price_paise,
       (20 + floor(random() * 180))::int AS qty -- 20 to 199 units, so a 20-minute run never empties a basket item
FROM load_store s
JOIN load_product p ON random() < CASE WHEN p.category = s.category THEN least(1.0, 220.0 / p.in_category) ELSE 30.0 / 2993 END;

INSERT INTO store_items (id, store_id, product_id, price_paise, mrp_paise, low_stock_at, search, created_at, updated_at)
SELECT i.id, i.store_id, i.product_id, i.price_paise, i.mrp_paise, 3,
       p.search || setweight(to_tsvector('simple'::regconfig, coalesce(b.name, '')), 'B'),
       now() - interval '117 days', now() - interval '117 days'
FROM load_item i
JOIN products p ON p.id = i.product_id
LEFT JOIN brands b ON b.id = p.brand_id;

-- Opening stock, the way the app records it: a batch, an 'opening' movement and its allocation.
-- The triggers move store_items.on_hand and the batch quantity.
CREATE TEMP TABLE load_stock ON COMMIT DROP AS
SELECT i.id AS item_id, i.store_id, i.qty,
       pg_temp.ulid_at(now() - interval '117 days') AS batch_id,
       pg_temp.ulid_at(now() - interval '117 days') AS movement_id
FROM load_item i;

INSERT INTO stock_batches (id, store_item_id, received_quantity, quantity, received_at, created_at, updated_at)
SELECT batch_id, item_id, qty, 0, now() - interval '117 days', now() - interval '117 days', now() - interval '117 days'
FROM load_stock;

INSERT INTO stock_movements (id, store_id, store_item_id, delta, source, created_at)
SELECT movement_id, store_id, item_id, qty, 'opening', now() - interval '117 days'
FROM load_stock;

INSERT INTO stock_movement_batches (movement_id, batch_id, delta)
SELECT movement_id, batch_id, qty FROM load_stock;

-- One catalogue change per item (what a till's first sync downloads), and the store's seq.
INSERT INTO catalogue_changes (store_id, seq, entity, entity_id, op, created_at)
SELECT store_id, row_number() OVER (PARTITION BY store_id ORDER BY id), 'store_item', id, 'upsert', now() - interval '1 hour'
FROM load_item;

UPDATE stores s SET catalogue_seq = c.n
FROM (SELECT store_id, count(*) AS n FROM load_item GROUP BY store_id) c
WHERE s.id = c.store_id;

------------------------------------------------------------------------------
-- Customers (no phone identity; see the header)
------------------------------------------------------------------------------

CREATE TEMP TABLE load_customer ON COMMIT DROP AS
SELECT g AS n, pg_temp.ulid_at(now() - interval '90 days' + g * interval '100 seconds') AS id
FROM generate_series(1, 50000) AS g;

INSERT INTO users (id, name, preferred_lang, created_at, updated_at)
SELECT id, 'Load customer ' || n, (ARRAY['en', 'en', 'en', 'hi', 'kn', 'ta', 'te'])[1 + n % 7],
       now() - interval '90 days' + n * interval '100 seconds', now() - interval '90 days' + n * interval '100 seconds'
FROM load_customer;

INSERT INTO customer_profiles (user_id, referral_code, created_at, updated_at)
SELECT id, 'Z' || pg_temp.code31(n, 5), now() - interval '90 days', now() - interval '90 days'
FROM load_customer;

\if :history
------------------------------------------------------------------------------
-- History: 14 days of collected pickup orders (5,000 a day) and 7 days of POS sales
-- (50,000 a day), so the shop lists, history and reports read realistic tables.
------------------------------------------------------------------------------

-- One history cashier per business: a plain user, never signed in, not a staff member.
CREATE TEMP TABLE load_cashier ON COMMIT DROP AS
SELECT s.store_id, pg_temp.ulid_at(now() - interval '119 days') AS user_id FROM load_store s;
INSERT INTO users (id, name, created_at, updated_at)
SELECT user_id, 'Load history cashier', now() - interval '119 days', now() - interval '119 days' FROM load_cashier;

-- Items a basket draws from: each store's items numbered 1..k.
CREATE TEMP TABLE load_pick ON COMMIT DROP AS
SELECT i.id, i.store_id, i.price_paise, p.name,
       row_number() OVER (PARTITION BY i.store_id ORDER BY i.id) AS k,
       count(*) OVER (PARTITION BY i.store_id) AS kmax
FROM load_item i JOIN products p ON p.id = i.product_id;
CREATE INDEX ON load_pick (store_id, k);
ANALYZE load_pick;

CREATE TEMP TABLE load_store_idx ON COMMIT DROP AS
SELECT s.n, s.store_id, k.kmax FROM load_store s JOIN (SELECT store_id, count(*)::int AS kmax FROM load_item GROUP BY store_id) k USING (store_id);
CREATE INDEX ON load_store_idx (n);

-- Orders
CREATE TEMP TABLE load_order ON COMMIT DROP AS
SELECT g, ts, pg_temp.ulid_at(ts) AS id, st.store_id, st.kmax, c.id AS customer_id, (1 + floor(random() * 3))::int AS lines
FROM (SELECT g, now() - interval '14 days' + (g * interval '14 days') / 70000 AS ts,
             1 + floor(random() * 1000)::int AS sn, 1 + floor(random() * 50000)::int AS cn
      FROM generate_series(1, 70000) AS g) o
JOIN load_store_idx st ON st.n = o.sn
JOIN load_customer c ON c.n = o.cn;

CREATE TEMP TABLE load_order_line ON COMMIT DROP AS
SELECT o.id AS order_id, o.ts, p.id AS store_item_id, p.price_paise, p.name, (1 + floor(random() * 2))::int AS qty
FROM load_order o
CROSS JOIN LATERAL (SELECT DISTINCT 1 + floor(random() * o.kmax)::int AS k FROM generate_series(1, o.lines)) r
JOIN load_pick p ON p.store_id = o.store_id AND p.k = r.k;

INSERT INTO orders (id, customer_id, store_id, fulfilment, status, subtotal_paise, items_after_offers_paise, item_subtotal_paise,
                    total_paise, code_enc, code_hmac, placed_at, seen_at, created_at, updated_at, cashback_bp)
SELECT o.id, o.customer_id, o.store_id, 'pickup', 'placed', t.total, t.total, t.total, t.total,
       '\x00'::bytea, decode(md5(o.id::text) || md5(o.id::text || 'h'), 'hex'), o.ts, o.ts + interval '1 minute', o.ts, o.ts, 0
FROM load_order o
JOIN (SELECT order_id, sum(price_paise * qty) AS total FROM load_order_line GROUP BY order_id) t ON t.order_id = o.id;

INSERT INTO order_items (id, order_id, store_item_id, product_snapshot, unit_price_paise, quantity_requested, quantity_accepted,
                         line_total_paise, created_at)
SELECT pg_temp.ulid_at(l.ts), l.order_id, l.store_item_id, jsonb_build_object('name', l.name), l.price_paise, l.qty, l.qty,
       l.price_paise * l.qty, l.ts
FROM load_order_line l;

UPDATE orders SET status = 'accepted', accepted_at = placed_at + interval '3 minutes'
WHERE id IN (SELECT id FROM load_order);
UPDATE orders SET status = 'collected', collected_at = placed_at + interval '40 minutes',
                  payment_status = 'paid', payment_method = 'cash'
WHERE id IN (SELECT id FROM load_order);

-- POS sales (no customer link, so no cashback and no credits)
CREATE TEMP TABLE load_sale ON COMMIT DROP AS
SELECT g, ts, pg_temp.ulid_at(ts) AS id, pg_temp.ulid_at(ts) AS client_id, st.store_id, st.kmax, (1 + floor(random() * 4))::int AS lines,
       (ARRAY['cash', 'cash', 'upi', 'upi', 'card'])[1 + floor(random() * 5)::int] AS payment_method
FROM (SELECT g, now() - interval '7 days' + (g * interval '7 days') / 350000 AS ts, 1 + floor(random() * 1000)::int AS sn
      FROM generate_series(1, 350000) AS g) s
JOIN load_store_idx st ON st.n = s.sn;

CREATE TEMP TABLE load_sale_line ON COMMIT DROP AS
SELECT s.id AS sale_id, s.ts, p.id AS store_item_id, p.price_paise, p.name, (1 + floor(random() * 3))::int AS qty
FROM load_sale s
CROSS JOIN LATERAL (SELECT DISTINCT 1 + floor(random() * s.kmax)::int AS k FROM generate_series(1, s.lines)) r
JOIN load_pick p ON p.store_id = s.store_id AND p.k = r.k;

INSERT INTO pos_sales (id, store_id, client_id, device_id, cashier_user_id, item_subtotal_paise, subtotal_paise, total_paise,
                       payment_method, sold_at, received_at, created_at, updated_at)
SELECT s.id, s.store_id, s.client_id, s.store_id, c.user_id, t.total, t.total, t.total, s.payment_method, s.ts, s.ts, s.ts, s.ts
FROM load_sale s
JOIN load_cashier c ON c.store_id = s.store_id
JOIN (SELECT sale_id, sum(price_paise * qty) AS total FROM load_sale_line GROUP BY sale_id) t ON t.sale_id = s.id;

INSERT INTO pos_sale_lines (id, sale_id, client_id, store_item_id, name_snapshot, unit_price_paise, quantity, line_total_paise, created_at)
SELECT pg_temp.ulid_at(l.ts), l.sale_id, pg_temp.ulid_at(l.ts), l.store_item_id, left(l.name, 120), l.price_paise, l.qty,
       l.price_paise * l.qty, l.ts
FROM load_sale_line l;
\endif

COMMIT;

ANALYZE businesses;
ANALYZE stores;
ANALYZE store_items;
ANALYZE stock_batches;
ANALYZE stock_movements;
ANALYZE stock_movement_batches;
ANALYZE catalogue_changes;
ANALYZE users;
ANALYZE customer_profiles;
ANALYZE orders;
ANALYZE order_items;
ANALYZE pos_sales;
ANALYZE pos_sale_lines;

SELECT (SELECT count(*) FROM stores WHERE slug LIKE 'load-%') AS stores,
       (SELECT count(*) FROM store_items si JOIN stores s ON s.id = si.store_id WHERE s.slug LIKE 'load-%') AS items,
       (SELECT count(*) FROM users WHERE name LIKE 'Load customer %') AS customers,
       (SELECT count(*) FROM orders o JOIN stores s ON s.id = o.store_id WHERE s.slug LIKE 'load-%') AS orders,
       (SELECT count(*) FROM pos_sales p JOIN stores s ON s.id = p.store_id WHERE s.slug LIKE 'load-%') AS pos_sales;

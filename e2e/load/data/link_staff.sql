-- W4-02: makes the load shop people (signed in and named by scenarios/bootstrap.js) staff of the
-- generated stores. Pool A "Load owner NN" owns the 25 businesses of stores 25(NN-1)+1 … 25·NN,
-- pool B "Load manager NN" manages the same 25 stores, and pool C "Load soak NN" manages stores
-- 4(NN-1)+1 … 4·NN (stores 1–100, used by the soak while order_burst uses owners 1–4).
-- Idempotent; refuses outside local and test, like generate.sql.

\set ON_ERROR_STOP 1

DO $$
BEGIN
  IF (SELECT env FROM platform_environment) NOT IN ('local', 'test') THEN
    RAISE EXCEPTION 'link_staff.sql: platform_environment is not local or test; refusing';
  END IF;
END
$$;

BEGIN;

CREATE TEMP TABLE load_person ON COMMIT DROP AS
SELECT u.id AS user_id,
       CASE WHEN u.name LIKE 'Load owner %' THEN 'owner' WHEN u.name LIKE 'Load manager %' THEN 'manager' ELSE 'soak' END AS pool,
       substring(u.name FROM '(\d+)$')::int AS n
FROM users u
JOIN phone_identities pi ON pi.user_id = u.id
WHERE u.name ~ '^Load (owner|manager|soak) \d{2}$';

-- Every person must be there exactly once: bootstrap signs in 40 + 40 + 25.
DO $$
DECLARE
  got int;
  dup int;
BEGIN
  SELECT count(*), count(*) - count(DISTINCT (pool, n)) INTO got, dup FROM load_person;
  IF got <> 105 OR dup <> 0 THEN
    RAISE EXCEPTION 'link_staff.sql: expected 105 distinct load shop people, found % (% duplicate names); run make load-data again', got, dup;
  END IF;
END
$$;

CREATE TEMP TABLE load_store_n ON COMMIT DROP AS
SELECT substring(slug FROM 6)::int AS n, id AS store_id, business_id FROM stores WHERE slug ~ '^load-\d{4}$';

CREATE TEMP TABLE load_staff ON COMMIT DROP AS
SELECT p.user_id, s.business_id, 'owner'::text AS role, NULL::uuid[] AS store_ids
FROM load_person p JOIN load_store_n s ON p.pool = 'owner' AND (s.n - 1) / 25 + 1 = p.n
UNION ALL
SELECT p.user_id, s.business_id, 'manager', ARRAY[s.store_id]
FROM load_person p JOIN load_store_n s ON p.pool = 'manager' AND (s.n - 1) / 25 + 1 = p.n
UNION ALL
SELECT p.user_id, s.business_id, 'manager', ARRAY[s.store_id]
FROM load_person p JOIN load_store_n s ON p.pool = 'soak' AND (s.n - 1) / 4 + 1 = p.n;

-- Each generated business has one store, so a manager row names that one store.
INSERT INTO staff (id, business_id, user_id, role, store_ids, status, created_at, updated_at)
SELECT (lpad(to_hex((extract(epoch FROM now()) * 1000)::bigint), 12, '0') || right(replace(gen_random_uuid()::text, '-', ''), 20))::uuid,
       l.business_id, l.user_id, l.role, l.store_ids, 'active', now(), now()
FROM load_staff l
ON CONFLICT (business_id, user_id) DO NOTHING;

COMMIT;

SELECT role, count(*) FROM staff st JOIN users u ON u.id = st.user_id
WHERE u.name ~ '^Load (owner|manager|soak) \d{2}$' AND st.status = 'active' GROUP BY role ORDER BY role;

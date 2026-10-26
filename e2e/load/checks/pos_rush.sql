-- W4-02 pos_rush checks (08 §6: no duplicate sales).
--   psql "$DATABASE_MIGRATE_URL" -v ON_ERROR_STOP=1 -v since=<run start, ISO 8601> -v sent=<n_sales_unique_sent> -f checks/pos_rush.sql
-- A duplicate is two sales with one (store, clientId), which the unique index forbids, or more
-- sales in the window than distinct clientIds sent (a retry recorded as a new sale).

\set ON_ERROR_STOP 1
\pset footer off

CREATE TEMP VIEW w402_sales AS
SELECT p.* FROM pos_sales p JOIN stores s ON s.id = p.store_id
WHERE s.slug LIKE 'load-%' AND p.received_at >= :'since'::timestamptz;

SELECT count(*) AS sales_recorded,
       count(*) FILTER (WHERE device_seq IS NOT NULL) AS via_sync,
       count(DISTINCT (store_id, client_id)) AS distinct_client_ids,
       :sent::bigint AS distinct_sent
FROM w402_sales;

SELECT count(*) AS duplicate_client_ids FROM (
  SELECT store_id, client_id FROM w402_sales GROUP BY 1, 2 HAVING count(*) > 1) d;

-- Stock: every POS line with an item moved stock exactly once.
SELECT count(*) FILTER (WHERE l.movement_id IS NULL AND l.store_item_id IS NOT NULL) AS lines_without_movement,
       count(*) AS lines
FROM pos_sale_lines l JOIN w402_sales s ON s.id = l.sale_id;

SELECT set_config('w402.sent', :'sent', false) \gset w402_

DO $$
DECLARE
  recorded bigint;
  dup bigint;
BEGIN
  SELECT count(*) INTO recorded FROM w402_sales;
  SELECT count(*) INTO dup FROM (SELECT 1 FROM w402_sales GROUP BY store_id, client_id HAVING count(*) > 1) d;
  IF dup > 0 THEN
    RAISE EXCEPTION 'pos_rush: % (store, clientId) pairs have more than one sale (FAIL)', dup;
  END IF;
  IF recorded > current_setting('w402.sent')::bigint THEN
    RAISE EXCEPTION 'pos_rush: % sales recorded for % distinct sales sent (FAIL: a retry became a sale)', recorded, current_setting('w402.sent');
  END IF;
  RAISE NOTICE 'pos_rush: PASS (% sales, no duplicates)', recorded;
END
$$;

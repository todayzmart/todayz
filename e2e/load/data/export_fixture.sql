-- W4-02: writes the k6 fixture (out/fixture.json) from the generated stores, so the scenarios
-- never need the database. Ids are the API's ULID text. Holds no phone, code or token.
--   psql -At -v ON_ERROR_STOP=1 -f export_fixture.sql -o out/fixture.json
--
-- cities      [{name, lat, lng}]                     city centres the browse VUs stand near
-- stores      [{n, id, bid, slug, lat, lng, items}]  load-0001 … load-1000 in order (index n-1)
--   items     [{id, p, n}]                           20 well-stocked listed items: id, price, name
-- byCity      {city: [store index]}                  0-based indexes into stores
-- categories  [slug]                                 top-level category slugs
-- terms       [word]                                 the commonest words in product names

\set ON_ERROR_STOP 1
\set QUIET 1
\pset footer off

CREATE FUNCTION pg_temp.ulid_text(u uuid) RETURNS text LANGUAGE sql IMMUTABLE AS $$
  WITH b AS (SELECT (B'00' || ('x' || replace(u::text, '-', ''))::bit(128))::bit(130) AS bits)
  SELECT string_agg(substr('0123456789ABCDEFGHJKMNPQRSTVWXYZ', substring(b.bits FROM i * 5 + 1 FOR 5)::bit(5)::int + 1, 1), '' ORDER BY i)
  FROM b, generate_series(0, 25) AS i
$$;

WITH s AS (
  SELECT substring(st.slug FROM 6)::int AS n, st.id, st.business_id, st.slug, st.city,
         ST_Y(st.location::geometry) AS lat, ST_X(st.location::geometry) AS lng
  FROM stores st
  WHERE st.slug ~ '^load-\d{4}$' AND st.status = 'live'
),
items AS (
  SELECT x.store_id, jsonb_agg(jsonb_build_object('id', pg_temp.ulid_text(x.id), 'p', x.price_paise, 'n', left(x.name, 120)) ORDER BY x.id) AS items
  FROM (
    SELECT si.store_id, si.id, si.price_paise, coalesce(si.local_name, p.name) AS name,
           row_number() OVER (PARTITION BY si.store_id ORDER BY si.on_hand - si.reserved DESC, si.id) AS r
    FROM store_items si
    JOIN s ON s.id = si.store_id
    JOIN products p ON p.id = si.product_id
    WHERE si.is_listed AND si.on_hand - si.reserved > 0 AND p.status = 'active'
  ) x
  WHERE x.r <= 20
  GROUP BY x.store_id
),
terms AS (
  SELECT jsonb_agg(w ORDER BY c DESC, w) AS terms FROM (
    SELECT w, count(*) AS c
    FROM (SELECT lower(regexp_split_to_table(name, '[^A-Za-z]+')) AS w FROM products WHERE status = 'active') t
    WHERE length(w) >= 4
    GROUP BY w ORDER BY count(*) DESC, w LIMIT 60
  ) top
)
SELECT jsonb_build_object(
  'cities', jsonb_build_array(
    jsonb_build_object('name', 'Bengaluru', 'lat', 12.9716, 'lng', 77.5946),
    jsonb_build_object('name', 'Chennai', 'lat', 13.0827, 'lng', 80.2707),
    jsonb_build_object('name', 'Hyderabad', 'lat', 17.3850, 'lng', 78.4867)),
  'stores', (SELECT jsonb_agg(jsonb_build_object('n', s.n, 'id', pg_temp.ulid_text(s.id), 'bid', pg_temp.ulid_text(s.business_id),
                                                 'slug', s.slug, 'lat', round(s.lat::numeric, 6), 'lng', round(s.lng::numeric, 6),
                                                 'items', coalesce(i.items, '[]'::jsonb)) ORDER BY s.n)
             FROM s LEFT JOIN items i ON i.store_id = s.id),
  'byCity', (SELECT jsonb_object_agg(city, idx) FROM (SELECT city, jsonb_agg(n - 1 ORDER BY n) AS idx FROM s GROUP BY city) c),
  'categories', (SELECT jsonb_agg(slug ORDER BY sort, slug) FROM categories WHERE parent_id IS NULL),
  'terms', (SELECT terms FROM terms)
)::text;

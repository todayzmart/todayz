-- W4-02 (load): the per-store store_items index also carries is_listed, so the
-- discover summary's in-stock count (CountListingsInRadius: is_listed AND
-- on_hand - reserved > 0 over every item of every shop in the radius) is an
-- index-only scan instead of one heap page per item. Measured on 1,000 shops
-- with about 210 items each, 5 km around central Bengaluru: 92 ms to 6 ms.
--
-- The index replaces ix_store_items_store_id (store_id) INCLUDE (price_paise,
-- on_hand, reserved) from 10501, which every store_id lookup still uses: same
-- key, one more included column. on_hand and reserved were already included,
-- so stock updates were already not HOT; the only write that changes is a
-- listing toggle alone (is_listed flipped, stock untouched), which is now not
-- HOT either: rare, owner-driven, one row. A plain CREATE INDEX (not CONCURRENTLY, goose runs each file in a
-- transaction) locks store_items writes for the build, seconds at today's size.

-- +goose Up
CREATE INDEX ix_store_items_store_listing ON store_items (store_id)
  INCLUDE (price_paise, on_hand, reserved, is_listed);
DROP INDEX ix_store_items_store_id;

-- +goose Down
CREATE INDEX ix_store_items_store_id ON store_items (store_id) INCLUDE (price_paise, on_hand, reserved);
DROP INDEX ix_store_items_store_listing;

-- A shop may sell an item at any price, above its MRP too (D-245, founder
-- 26 Oct). The catalogue no longer refuses it, so the check goes.

-- +goose Up
ALTER TABLE store_items DROP CONSTRAINT ck_store_items_price_mrp;

-- +goose Down
ALTER TABLE store_items
  ADD CONSTRAINT ck_store_items_price_mrp CHECK (mrp_paise IS NULL OR price_paise <= mrp_paise);

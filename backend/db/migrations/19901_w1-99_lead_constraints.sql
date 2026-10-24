-- Wave 1 constraints migration (D-79, 03 §13; lead block w1-99, D-211).
--
-- The same-wave columns W1-04, W1-05 and W1-11 created as plain uuids get
-- their FKs here. Every column already has its index except job_runs.triggered_by
-- and job_controls.updated_by, which get one. product_review_queue.decided_by
-- waits for its table (deferred in the W1-05 weekend slice).

-- +goose Up

-- users (W1-01)
CREATE INDEX ix_job_runs_triggered_by ON job_runs (triggered_by) WHERE triggered_by IS NOT NULL;
ALTER TABLE job_runs ADD CONSTRAINT fk_job_runs_triggered_by FOREIGN KEY (triggered_by) REFERENCES users (id);
CREATE INDEX ix_job_controls_updated_by ON job_controls (updated_by) WHERE updated_by IS NOT NULL;
ALTER TABLE job_controls ADD CONSTRAINT fk_job_controls_updated_by FOREIGN KEY (updated_by) REFERENCES users (id);
ALTER TABLE businesses ADD CONSTRAINT fk_businesses_onboarded_by FOREIGN KEY (onboarded_by) REFERENCES users (id);
ALTER TABLE businesses ADD CONSTRAINT fk_businesses_verified_by FOREIGN KEY (verified_by) REFERENCES users (id);
ALTER TABLE business_kyc ADD CONSTRAINT fk_business_kyc_verified_by FOREIGN KEY (verified_by) REFERENCES users (id);
ALTER TABLE business_documents ADD CONSTRAINT fk_business_documents_reviewed_by FOREIGN KEY (reviewed_by) REFERENCES users (id);
ALTER TABLE staff ADD CONSTRAINT fk_staff_user_id FOREIGN KEY (user_id) REFERENCES users (id);
ALTER TABLE staff ADD CONSTRAINT fk_staff_invited_by FOREIGN KEY (invited_by) REFERENCES users (id);
ALTER TABLE stores ADD CONSTRAINT fk_stores_reviewed_by FOREIGN KEY (reviewed_by) REFERENCES users (id);
ALTER TABLE stock_movements ADD CONSTRAINT fk_stock_movements_actor_user_id FOREIGN KEY (actor_user_id) REFERENCES users (id);

-- devices (W1-01)
ALTER TABLE stock_movements ADD CONSTRAINT fk_stock_movements_device_id FOREIGN KEY (device_id) REFERENCES devices (id);

-- categories (W1-05)
ALTER TABLE businesses ADD CONSTRAINT fk_businesses_category_id FOREIGN KEY (category_id) REFERENCES categories (id);

-- stores (W1-04)
ALTER TABLE products ADD CONSTRAINT fk_products_created_by_store_id FOREIGN KEY (created_by_store_id) REFERENCES stores (id);
ALTER TABLE store_items ADD CONSTRAINT fk_store_items_store_id FOREIGN KEY (store_id) REFERENCES stores (id);
ALTER TABLE stock_movements ADD CONSTRAINT fk_stock_movements_store_id FOREIGN KEY (store_id) REFERENCES stores (id);
ALTER TABLE catalogue_changes ADD CONSTRAINT fk_catalogue_changes_store_id FOREIGN KEY (store_id) REFERENCES stores (id);

-- +goose Down

ALTER TABLE catalogue_changes DROP CONSTRAINT fk_catalogue_changes_store_id;
ALTER TABLE stock_movements DROP CONSTRAINT fk_stock_movements_store_id;
ALTER TABLE store_items DROP CONSTRAINT fk_store_items_store_id;
ALTER TABLE products DROP CONSTRAINT fk_products_created_by_store_id;
ALTER TABLE businesses DROP CONSTRAINT fk_businesses_category_id;
ALTER TABLE stock_movements DROP CONSTRAINT fk_stock_movements_device_id;
ALTER TABLE stock_movements DROP CONSTRAINT fk_stock_movements_actor_user_id;
ALTER TABLE stores DROP CONSTRAINT fk_stores_reviewed_by;
ALTER TABLE staff DROP CONSTRAINT fk_staff_invited_by;
ALTER TABLE staff DROP CONSTRAINT fk_staff_user_id;
ALTER TABLE business_documents DROP CONSTRAINT fk_business_documents_reviewed_by;
ALTER TABLE business_kyc DROP CONSTRAINT fk_business_kyc_verified_by;
ALTER TABLE businesses DROP CONSTRAINT fk_businesses_verified_by;
ALTER TABLE businesses DROP CONSTRAINT fk_businesses_onboarded_by;
ALTER TABLE job_controls DROP CONSTRAINT fk_job_controls_updated_by;
DROP INDEX ix_job_controls_updated_by;
ALTER TABLE job_runs DROP CONSTRAINT fk_job_runs_triggered_by;
DROP INDEX ix_job_runs_triggered_by;

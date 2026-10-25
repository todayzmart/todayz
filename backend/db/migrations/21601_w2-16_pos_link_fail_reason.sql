-- W2-16: why an offline counter-code link failed at sync (D-12, B-05 sales
-- history "Customer link failed"). Set only on a failed link; rows that
-- failed before this migration keep NULL (the shop sees the failure without a
-- reason).
--   code_used   the code's (device, time step) was used before
--   code_wrong  the code did not match the customer's devices at that step
--   clock_off   the device clock was more than 2 min off at its last check,
--               or the typed time is in the future
--   too_late    synced after pos_offline_link_max_hours
--   locked      too many wrong codes for this phone or cashier
--   unverified  the sale carried no code or clock offset to check

-- +goose Up
ALTER TABLE pos_sales ADD COLUMN link_fail_reason text;
ALTER TABLE pos_sales ADD CONSTRAINT ck_pos_sales_link_fail_reason CHECK (link_fail_reason IS NULL
  OR (link_status = 'failed'
    AND link_fail_reason IN ('code_used','code_wrong','clock_off','too_late','locked','unverified')));

-- +goose Down
ALTER TABLE pos_sales DROP CONSTRAINT ck_pos_sales_link_fail_reason;
ALTER TABLE pos_sales DROP COLUMN link_fail_reason;

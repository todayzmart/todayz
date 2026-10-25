-- W3-02 risk and fraud (D-78; 01 §6, §8, §17; D-225). The rules live in
-- internal/risk; risk_flags itself is W0-03's.
--
-- * order_code_lockouts: one row each time an order's code locks (5 wrong
--   tries, 01 §6). "New code" clears orders.code_locked_at, so the lockout
--   count per store and IST day (risk_code_lockouts_store_per_day) needs a
--   record of its own. A trigger on orders writes it in the same
--   transaction as the lock, so orders needs no risk call.
-- * risk_flags.review_note: the reason a person gave when clearing or
--   confirming a flag (A-13). The audit row keeps it too.
-- * The settings for the rules that 01 had no number for (D-225).
-- * Indexes the risk_scan job (every 5 minutes) reads through.

-- +goose Up
CREATE TABLE order_code_lockouts (
  order_id  uuid NOT NULL,
  store_id  uuid NOT NULL,
  locked_at timestamptz NOT NULL,
  PRIMARY KEY (order_id, locked_at),
  CONSTRAINT fk_order_code_lockouts_order_id FOREIGN KEY (order_id) REFERENCES orders (id),
  CONSTRAINT fk_order_code_lockouts_store_id FOREIGN KEY (store_id) REFERENCES stores (id)
);
CREATE INDEX ix_order_code_lockouts_store ON order_code_lockouts (store_id, locked_at);
CREATE TRIGGER trg_order_code_lockouts_append_only BEFORE UPDATE OR DELETE ON order_code_lockouts
  FOR EACH ROW EXECUTE FUNCTION append_only();
CREATE TRIGGER trg_order_code_lockouts_no_truncate BEFORE TRUNCATE ON order_code_lockouts
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();
REVOKE UPDATE, DELETE, TRUNCATE ON order_code_lockouts FROM todayz_app;

-- +goose StatementBegin
CREATE FUNCTION order_code_lockouts_record() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO order_code_lockouts (order_id, store_id, locked_at)
  VALUES (NEW.id, NEW.store_id, NEW.code_locked_at)
  ON CONFLICT DO NOTHING;
  RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_orders_code_lockout AFTER UPDATE OF code_locked_at ON orders
  FOR EACH ROW WHEN (NEW.code_locked_at IS NOT NULL AND NEW.code_locked_at IS DISTINCT FROM OLD.code_locked_at)
  EXECUTE FUNCTION order_code_lockouts_record();

ALTER TABLE risk_flags ADD COLUMN review_note text;
ALTER TABLE risk_flags ADD CONSTRAINT ck_risk_flags_review_note CHECK (review_note IS NULL
  OR (status <> 'open' AND char_length(review_note) BETWEEN 1 AND 500));
CREATE INDEX ix_risk_flags_status_rule ON risk_flags (status, rule, created_at DESC, id DESC);

CREATE INDEX ix_referrals_qualified_at ON referrals (qualified_at) WHERE qualified_at IS NOT NULL;
CREATE INDEX ix_bill_claims_claimed_at ON bill_claims (claimed_at) WHERE claimed_at IS NOT NULL;
CREATE INDEX ix_pos_sales_store_received ON pos_sales (store_id, received_at)
  WHERE link_status = 'linked' AND link_method IN ('bill_qr','counter_code');

-- 01 §17 Risk (D-225). TestSettingsMatchProductRules reads these inserts (D-95).
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('risk_bill_claim_stores', '3', 'count', 2, 20, 'Risk. 01 §6: different stores one customer claims bills at within risk_bill_claim_window_minutes that raise a risk flag'),
  ('risk_bill_claim_window_minutes', '10', 'minutes', 1, 240, 'Risk. 01 §6: the window for risk_bill_claim_stores'),
  ('risk_reversal_ratio_percent', '20', 'count', 1, 100, 'Risk. 01 §7: share of a store''s linked POS sales reversed in risk_reversal_window_days that raises a risk flag, in percent'),
  ('risk_reversal_min_sales', '10', 'count', 1, 1000, 'Risk. 01 §7: linked POS sales a store needs in the window before the reversal ratio counts'),
  ('risk_reversal_window_days', '7', 'days', 1, 30, 'Risk. 01 §7: the window for risk_reversal_ratio_percent'),
  ('risk_otp_phones_per_ip_hour', '10', 'count', 2, 100, 'Risk. 01 §2: different phone numbers one IP asks OTPs for in an hour that raise a risk flag'),
  ('risk_otp_failed_per_phone_day', '3', 'count', 1, 50, 'Risk. 01 §2: OTPs for one phone that used every attempt without a right code in 24 h that raise a risk flag');

-- +goose Down
DELETE FROM platform_settings WHERE key IN (
  'risk_bill_claim_stores',
  'risk_bill_claim_window_minutes',
  'risk_reversal_ratio_percent',
  'risk_reversal_min_sales',
  'risk_reversal_window_days',
  'risk_otp_phones_per_ip_hour',
  'risk_otp_failed_per_phone_day'
);
DROP INDEX ix_pos_sales_store_received;
DROP INDEX ix_bill_claims_claimed_at;
DROP INDEX ix_referrals_qualified_at;
DROP INDEX ix_risk_flags_status_rule;
ALTER TABLE risk_flags DROP CONSTRAINT ck_risk_flags_review_note;
ALTER TABLE risk_flags DROP COLUMN review_note;
DROP TRIGGER trg_orders_code_lockout ON orders;
DROP FUNCTION order_code_lockouts_record();
DROP TABLE order_code_lockouts;

-- W4-93 seller auto-onboarding (D-274, founder 27 Oct): a business and its
-- complete stores go live as soon as the owner (or the field agent, with the
-- owner's code) submits. No admin approves the business, a store, a shop
-- photo or GST. A field agent, admin or super_admin later checks the shop:
-- businesses.check_status is unchecked until then, checked after, or
-- rejected, which hides the shop and cancels its open online orders.
--
-- Payouts are held while a business is not checked (hold_reason
-- shop_unchecked); orders cancelled by a reject carry the system reason
-- shop_rejected.
--
-- Existing rows stay valid: a business an admin already verified (verified or
-- suspended now) counts as checked by that admin; a rejected one stays
-- rejected. Under the new rule, a submitted or needs_info business goes live
-- now with its stores that were waiting for review, and every uploaded
-- document is accepted. Down drops the check columns and restores the
-- constraints; it does not take the promoted businesses and stores back to
-- review (nothing records which ones moved).

-- +goose Up
ALTER TABLE businesses
  ADD COLUMN check_status text NOT NULL DEFAULT 'unchecked',
  ADD COLUMN checked_at   timestamptz,
  ADD COLUMN checked_by   uuid,
  ADD COLUMN check_note   text,
  ADD CONSTRAINT ck_businesses_check_status CHECK (check_status IN ('unchecked','checked','rejected')),
  ADD CONSTRAINT ck_businesses_checked CHECK ((check_status = 'unchecked') = (checked_at IS NULL)),
  ADD CONSTRAINT ck_businesses_check_note CHECK (check_note IS NULL OR char_length(check_note) BETWEEN 1 AND 500),
  ADD CONSTRAINT fk_businesses_checked_by FOREIGN KEY (checked_by) REFERENCES users (id);
CREATE INDEX ix_businesses_checked_by ON businesses (checked_by) WHERE checked_by IS NOT NULL;
-- The daily check queue (A-03 Checks): live unchecked businesses, newest first.
CREATE INDEX ix_businesses_check_queue ON businesses (verified_at DESC, id DESC)
  WHERE check_status = 'unchecked' AND status = 'verified';

UPDATE businesses SET check_status = 'checked', checked_at = coalesce(verified_at, updated_at), checked_by = verified_by
WHERE status IN ('verified','suspended');
UPDATE businesses SET check_status = 'rejected', checked_at = updated_at
WHERE status = 'rejected';

UPDATE businesses SET status = 'verified', verified_at = now(), needs_info_fields = NULL, needs_info_note = NULL,
  submitted_at = coalesce(submitted_at, now()), version = version + 1
WHERE status IN ('submitted','needs_info');
UPDATE stores SET status = 'live', live_at = coalesce(live_at, now()), version = version + 1
WHERE status = 'pending_review'
  AND business_id IN (SELECT id FROM businesses WHERE status = 'verified');

UPDATE business_documents SET status = 'accepted', updated_at = now(), version = version + 1
WHERE status = 'uploaded';

ALTER TABLE orders DROP CONSTRAINT ck_orders_status_reason;
ALTER TABLE orders ADD CONSTRAINT ck_orders_status_reason CHECK (
  (status IN ('placed','awaiting_customer','accepted','ready','out_for_delivery','collected','delivered')
    AND status_reason IS NULL)
  OR (status = 'rejected' AND status_reason IN ('out_of_stock','closing_soon','too_busy',
    'cannot_deliver_address','price_changed','other'))
  OR (status = 'expired' AND status_reason IN ('not_accepted','no_show','dispatch_timeout'))
  OR (status = 'cancelled' AND status_reason IN ('changed_mind','declined_partial','too_slow','other',
    'out_of_stock','customer_unreachable','closing_soon','partial_timeout',
    'customer_request','store_request','fraud_suspected','technical_issue','shop_rejected'))
  OR (status = 'delivery_failed' AND status_reason IN ('customer_unreachable','address_not_found',
    'customer_refused','other')));

ALTER TABLE payouts DROP CONSTRAINT ck_payouts_hold_reason;
ALTER TABLE payouts ADD CONSTRAINT ck_payouts_hold_reason CHECK (hold_reason IS NULL OR hold_reason IN
  ('kyc_incomplete','payout_details_missing','risk_flag','business_suspended','manual','shop_unchecked'));

-- +goose Down
UPDATE payouts SET hold_reason = 'kyc_incomplete' WHERE hold_reason = 'shop_unchecked';
ALTER TABLE payouts DROP CONSTRAINT ck_payouts_hold_reason;
ALTER TABLE payouts ADD CONSTRAINT ck_payouts_hold_reason CHECK (hold_reason IS NULL OR hold_reason IN
  ('kyc_incomplete','payout_details_missing','risk_flag','business_suspended','manual'));

UPDATE orders SET status_reason = 'technical_issue' WHERE status = 'cancelled' AND status_reason = 'shop_rejected';
ALTER TABLE orders DROP CONSTRAINT ck_orders_status_reason;
ALTER TABLE orders ADD CONSTRAINT ck_orders_status_reason CHECK (
  (status IN ('placed','awaiting_customer','accepted','ready','out_for_delivery','collected','delivered')
    AND status_reason IS NULL)
  OR (status = 'rejected' AND status_reason IN ('out_of_stock','closing_soon','too_busy',
    'cannot_deliver_address','price_changed','other'))
  OR (status = 'expired' AND status_reason IN ('not_accepted','no_show','dispatch_timeout'))
  OR (status = 'cancelled' AND status_reason IN ('changed_mind','declined_partial','too_slow','other',
    'out_of_stock','customer_unreachable','closing_soon','partial_timeout',
    'customer_request','store_request','fraud_suspected','technical_issue'))
  OR (status = 'delivery_failed' AND status_reason IN ('customer_unreachable','address_not_found',
    'customer_refused','other')));

DROP INDEX ix_businesses_check_queue;
DROP INDEX ix_businesses_checked_by;
ALTER TABLE businesses
  DROP CONSTRAINT fk_businesses_checked_by,
  DROP CONSTRAINT ck_businesses_check_note,
  DROP CONSTRAINT ck_businesses_checked,
  DROP CONSTRAINT ck_businesses_check_status,
  DROP COLUMN check_note,
  DROP COLUMN checked_by,
  DROP COLUMN checked_at,
  DROP COLUMN check_status;

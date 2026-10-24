-- Wave 2 constraints migration (D-79, 03 §13; lead block w2-99, D-211).
--
-- The same-wave columns created as plain uuids get their FKs here. Every
-- column already has its index except the two referral reward columns, which
-- get one. Not here, because the target table does not exist yet:
-- referrals.standee_id and referral_clicks.standee_id (qr_standees) and
-- support_tickets.order_id (support_tickets, W2-09). notify_order_escalations
-- .order_id was missing from 03 §13 and is added here.

-- +goose Up

-- orders (W2-02)
ALTER TABLE credit_holds ADD CONSTRAINT fk_credit_holds_order_id FOREIGN KEY (order_id) REFERENCES orders (id);
ALTER TABLE reviews ADD CONSTRAINT fk_reviews_order_id FOREIGN KEY (order_id) REFERENCES orders (id);
ALTER TABLE offer_redemptions ADD CONSTRAINT fk_offer_redemptions_order_id FOREIGN KEY (order_id) REFERENCES orders (id);
ALTER TABLE notify_order_escalations ADD CONSTRAINT fk_notify_order_escalations_order_id FOREIGN KEY (order_id) REFERENCES orders (id);

-- pos_sales and bill_claims (W2-04)
ALTER TABLE reviews ADD CONSTRAINT fk_reviews_pos_sale_id FOREIGN KEY (pos_sale_id) REFERENCES pos_sales (id);
ALTER TABLE offer_redemptions ADD CONSTRAINT fk_offer_redemptions_pos_sale_id FOREIGN KEY (pos_sale_id) REFERENCES pos_sales (id);
ALTER TABLE credit_holds ADD CONSTRAINT fk_credit_holds_bill_claim_id FOREIGN KEY (bill_claim_id) REFERENCES bill_claims (id);

-- ledger_transactions (W2-01)
CREATE INDEX ix_referrals_referrer_transaction_id ON referrals (referrer_transaction_id) WHERE referrer_transaction_id IS NOT NULL;
ALTER TABLE referrals ADD CONSTRAINT fk_referrals_referrer_transaction_id FOREIGN KEY (referrer_transaction_id) REFERENCES ledger_transactions (id);
CREATE INDEX ix_referrals_referee_transaction_id ON referrals (referee_transaction_id) WHERE referee_transaction_id IS NOT NULL;
ALTER TABLE referrals ADD CONSTRAINT fk_referrals_referee_transaction_id FOREIGN KEY (referee_transaction_id) REFERENCES ledger_transactions (id);

-- +goose Down

ALTER TABLE referrals DROP CONSTRAINT fk_referrals_referee_transaction_id;
DROP INDEX ix_referrals_referee_transaction_id;
ALTER TABLE referrals DROP CONSTRAINT fk_referrals_referrer_transaction_id;
DROP INDEX ix_referrals_referrer_transaction_id;
ALTER TABLE credit_holds DROP CONSTRAINT fk_credit_holds_bill_claim_id;
ALTER TABLE offer_redemptions DROP CONSTRAINT fk_offer_redemptions_pos_sale_id;
ALTER TABLE reviews DROP CONSTRAINT fk_reviews_pos_sale_id;
ALTER TABLE notify_order_escalations DROP CONSTRAINT fk_notify_order_escalations_order_id;
ALTER TABLE offer_redemptions DROP CONSTRAINT fk_offer_redemptions_order_id;
ALTER TABLE reviews DROP CONSTRAINT fk_reviews_order_id;
ALTER TABLE credit_holds DROP CONSTRAINT fk_credit_holds_order_id;

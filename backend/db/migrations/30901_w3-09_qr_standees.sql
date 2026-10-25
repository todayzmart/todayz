-- W3-09 QR standee campaigns (01 §8; D-26, D-162, D-211, D-223; 03 qr_standees).
--
-- qr_standees: one row per printed standee of a store. A standee has no code
-- of its own (D-26): its QR is the store's short-code link with `s=<id>`, and
-- `s` only says which standee was scanned. campaign is the shop's own label
-- ("Diwali counter", "Gate"). A scan is the referral_clicks row whose
-- standee_id points here, so the counts are read from referral_clicks and
-- referrals; there is no scans_count to keep in step (D-223).
-- reorder_requested_at/_by mark a request for a new printed standee; the
-- admin fulfilment that clears it is later work (TODO(lead), D-223).
-- printed_at is for W2-10's PDF route; nothing writes it yet. updated_at is
-- set from the injected clock by the queries (no touch trigger: it reads the
-- wall clock, not the test clock).
--
-- The two deferred FKs of D-211 land here: referrals.standee_id and
-- referral_clicks.standee_id now point at qr_standees.

-- +goose Up

CREATE TABLE qr_standees (
  id                   uuid PRIMARY KEY,
  store_id             uuid NOT NULL REFERENCES stores (id),
  campaign             text NOT NULL,
  created_by           uuid NOT NULL REFERENCES users (id),
  printed_at           timestamptz,
  reorder_requested_at timestamptz,
  reorder_requested_by uuid REFERENCES users (id),
  created_at           timestamptz NOT NULL,
  updated_at           timestamptz NOT NULL,
  version              int NOT NULL DEFAULT 1,
  CONSTRAINT ck_qr_standees_campaign CHECK (char_length(campaign) BETWEEN 1 AND 40 AND campaign = btrim(campaign)),
  CONSTRAINT ck_qr_standees_reorder CHECK ((reorder_requested_at IS NULL) = (reorder_requested_by IS NULL))
);
CREATE INDEX ix_qr_standees_store ON qr_standees (store_id, created_at);
CREATE INDEX ix_qr_standees_created_by ON qr_standees (created_by);
CREATE INDEX ix_qr_standees_reorder_by ON qr_standees (reorder_requested_by) WHERE reorder_requested_by IS NOT NULL;
CREATE INDEX ix_qr_standees_reorder ON qr_standees (reorder_requested_at) WHERE reorder_requested_at IS NOT NULL;

ALTER TABLE referral_clicks ADD CONSTRAINT fk_referral_clicks_standee
  FOREIGN KEY (standee_id) REFERENCES qr_standees (id);
ALTER TABLE referrals ADD CONSTRAINT fk_referrals_standee
  FOREIGN KEY (standee_id) REFERENCES qr_standees (id);

-- +goose Down
ALTER TABLE referrals DROP CONSTRAINT IF EXISTS fk_referrals_standee;
ALTER TABLE referral_clicks DROP CONSTRAINT IF EXISTS fk_referral_clicks_standee;
DROP TABLE IF EXISTS qr_standees;

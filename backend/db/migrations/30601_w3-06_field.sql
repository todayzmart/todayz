-- W3-06 field agents (A-04, A-15; 01 §14, D-39). businesses.onboarded_by (W1-04)
-- already records the agent; these two tables hold the owner's confirmation code
-- and which agent added which store item.
--
-- field_owner_codes: one row per code sent to a business owner's phone while an
-- agent onboards the shop. Only a keyed hash of the code is kept (never the
-- code); phone_hash ties the code to the number it went to, so a changed owner
-- number cannot be confirmed with an older code. A row is spent once
-- (consumed_at) or after the attempts run out. Rows are kept for the audit
-- trail; the send limits count them per business.
--
-- field_item_adds: the store items a field agent added (A-15 "products added").
-- One row per item, written in the same transaction as the item; an item the
-- shop adds itself has no row.

-- +goose Up

CREATE TABLE field_owner_codes (
  id          uuid PRIMARY KEY,
  business_id uuid NOT NULL REFERENCES businesses (id),
  phone_hash  bytea NOT NULL,
  code_hash   bytea NOT NULL,
  attempts    int NOT NULL DEFAULT 0,
  expires_at  timestamptz NOT NULL,
  consumed_at timestamptz,
  sent_by     uuid NOT NULL REFERENCES users (id),
  created_at  timestamptz NOT NULL,
  CONSTRAINT ck_field_owner_codes_hashes CHECK (octet_length(phone_hash) = 32 AND octet_length(code_hash) = 32),
  CONSTRAINT ck_field_owner_codes_attempts CHECK (attempts >= 0)
);
CREATE INDEX ix_field_owner_codes_business ON field_owner_codes (business_id, created_at DESC);
CREATE INDEX ix_field_owner_codes_sent_by ON field_owner_codes (sent_by);

CREATE TABLE field_item_adds (
  store_item_id uuid PRIMARY KEY REFERENCES store_items (id),
  business_id   uuid NOT NULL REFERENCES businesses (id),
  store_id      uuid NOT NULL REFERENCES stores (id),
  agent_id      uuid NOT NULL REFERENCES users (id),
  created_at    timestamptz NOT NULL
);
CREATE INDEX ix_field_item_adds_agent ON field_item_adds (agent_id, created_at);
CREATE INDEX ix_field_item_adds_business ON field_item_adds (business_id);
CREATE INDEX ix_field_item_adds_store ON field_item_adds (store_id);

-- +goose Down
DROP TABLE IF EXISTS field_item_adds;
DROP TABLE IF EXISTS field_owner_codes;

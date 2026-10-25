-- W3-16 Ask shops nearby, share cards and the site search strip (01 §11.1–§11.3,
-- 03 "Asks, share cards and the site search strip", D-101).
--
-- asks: the exact search point is never stored. It picks the recipients and
-- each recipient's distance band and a distance rounded to 500 m, then only
-- geo_cell (H3 res 8) and the city stay.
-- No name or phone is copied; the shop sees the item, the quantity and a
-- distance band.
--
-- ask_recipients: one row per store the ask reached. Its id is the reply id
-- (orders.ask_reply_id). One reply per row (answer goes null → yes|no once,
-- enforced in the reply UPDATE's WHERE). The MRP check is in Go in the reply
-- transaction (422 price_above_mrp).
--
-- version on asks bumps on every change the customer sees (a reply, a close),
-- so the ask.updated SSE event carries it.
--
-- Retention (asks_retention) deletes asks older than ask_retention_days;
-- recipients and blocks go by cascade, orders.ask_reply_id is set null.

-- +goose Up

CREATE TABLE asks (
  id               uuid PRIMARY KEY,
  customer_id      uuid NOT NULL REFERENCES users (id),
  product_id       uuid REFERENCES products (id),
  query_norm       text NOT NULL,
  quantity         int NOT NULL DEFAULT 1,
  need_by          text,
  geo_cell         bigint,
  city_place_id    uuid REFERENCES places (id),
  radius_m         int NOT NULL,
  target_store_id  uuid REFERENCES stores (id),
  recipients_count int NOT NULL DEFAULT 0,
  status           text NOT NULL DEFAULT 'open',
  close_reason     text,
  low_yield        boolean NOT NULL DEFAULT false,
  version          int NOT NULL DEFAULT 1,
  created_at       timestamptz NOT NULL DEFAULT now(),
  closed_at        timestamptz,
  -- set once, by the reply that sends the ask's one first-Yes push
  first_yes_notified_at timestamptz,
  CONSTRAINT ck_asks_query_norm CHECK (char_length(query_norm) BETWEEN 1 AND 200),
  CONSTRAINT ck_asks_quantity CHECK (quantity BETWEEN 1 AND 99),
  CONSTRAINT ck_asks_need_by CHECK (need_by IS NULL OR need_by IN ('now','today','tomorrow')),
  CONSTRAINT ck_asks_radius CHECK (radius_m > 0),
  CONSTRAINT ck_asks_recipients CHECK (recipients_count >= 0),
  CONSTRAINT ck_asks_status CHECK (status IN ('open','closed')),
  CONSTRAINT ck_asks_close_reason CHECK (close_reason IS NULL OR close_reason IN ('all_replied','window_ended','customer_closed')),
  CONSTRAINT ck_asks_closed CHECK ((status = 'closed') = (closed_at IS NOT NULL AND close_reason IS NOT NULL))
);
CREATE UNIQUE INDEX ux_asks_open ON asks (customer_id, coalesce(product_id::text, 'q:' || query_norm))
  WHERE status = 'open';
CREATE INDEX ix_asks_customer ON asks (customer_id, created_at DESC);
CREATE INDEX ix_asks_city_closed ON asks (city_place_id, product_id, created_at) WHERE status = 'closed';
CREATE INDEX ix_asks_created ON asks (created_at);

CREATE TABLE ask_recipients (
  id                uuid PRIMARY KEY,
  ask_id            uuid NOT NULL REFERENCES asks (id) ON DELETE CASCADE,
  store_id          uuid NOT NULL REFERENCES stores (id),
  sent_at           timestamptz NOT NULL,
  -- rounded to the nearest 500 m (never below 500): the customer's card shows
  -- it, and exact distances to three shops would place the customer
  distance_m        int NOT NULL,
  distance_band     text NOT NULL,
  reply_by          timestamptz NOT NULL,
  answer            text,
  price_paise       bigint,
  store_item_id     uuid REFERENCES store_items (id),
  ready_by          timestamptz,
  replied_at        timestamptz,
  replied_by        uuid REFERENCES users (id),
  price_valid_until timestamptz,
  reported_at       timestamptz,
  reported_by       uuid REFERENCES users (id),
  CONSTRAINT uq_ask_recipients_store UNIQUE (ask_id, store_id),
  CONSTRAINT ck_ask_recipients_distance CHECK (distance_m >= 500 AND distance_m % 500 = 0),
  CONSTRAINT ck_ask_recipients_band CHECK (distance_band IN ('lt_1km','1_3km','3_5km','gt_5km')),
  CONSTRAINT ck_ask_recipients_answer CHECK (answer IS NULL OR answer IN ('yes','no')),
  CONSTRAINT ck_ask_recipients_price CHECK ((answer = 'yes' OR price_paise IS NULL) AND (price_paise IS NULL OR price_paise > 0)),
  CONSTRAINT ck_ask_recipients_replied CHECK ((answer IS NULL) = (replied_at IS NULL)),
  CONSTRAINT ck_ask_recipients_ready CHECK (ready_by IS NULL OR answer = 'yes')
);
CREATE INDEX ix_ask_recipients_open ON ask_recipients (store_id, reply_by) WHERE answer IS NULL;
CREATE INDEX ix_ask_recipients_store_sent ON ask_recipients (store_id, sent_at);
CREATE INDEX ix_ask_recipients_ask ON ask_recipients (ask_id);

CREATE TABLE ask_store_blocks (
  store_id    uuid NOT NULL REFERENCES stores (id),
  customer_id uuid NOT NULL REFERENCES users (id),
  ask_id      uuid NOT NULL REFERENCES asks (id) ON DELETE CASCADE,
  created_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (store_id, customer_id)
);
CREATE INDEX ix_ask_store_blocks_ask ON ask_store_blocks (ask_id);
CREATE INDEX ix_ask_store_blocks_customer ON ask_store_blocks (customer_id);

-- W2-02 left orders.ask_reply_id a plain uuid until this table existed (03 §13).
ALTER TABLE orders ADD CONSTRAINT fk_orders_ask_reply_id
  FOREIGN KEY (ask_reply_id) REFERENCES ask_recipients (id) ON DELETE SET NULL;

-- orders_guard (W2-02) refuses any change to orders.ask_reply_id; the FK's
-- ON DELETE SET NULL (retention, account deletion) is an UPDATE to null, so
-- the guard now lets it go from a reply id to null and nothing else. The rest
-- of the function is W2-02's, unchanged.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION orders_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  ok boolean;
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'placed' THEN
      RAISE EXCEPTION 'orders: a new order is placed, not %', NEW.status
        USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id <> OLD.id OR NEW.number <> OLD.number OR NEW.customer_id <> OLD.customer_id
     OR NEW.store_id <> OLD.store_id OR NEW.fulfilment <> OLD.fulfilment
     OR NEW.placed_at <> OLD.placed_at
     OR (NEW.ask_reply_id IS DISTINCT FROM OLD.ask_reply_id AND NEW.ask_reply_id IS NOT NULL) THEN
    RAISE EXCEPTION 'orders: the identity of an order never changes'
      USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.status = OLD.status THEN
    RETURN NEW;
  END IF;
  ok := CASE OLD.status
    WHEN 'placed' THEN NEW.status IN ('accepted','awaiting_customer','rejected','expired','cancelled')
    WHEN 'awaiting_customer' THEN NEW.status IN ('accepted','cancelled')
    WHEN 'accepted' THEN NEW.status IN ('ready','collected','expired','cancelled','out_for_delivery')
    WHEN 'ready' THEN NEW.status IN ('collected','expired','cancelled')
    WHEN 'out_for_delivery' THEN NEW.status IN ('delivered','delivery_failed','cancelled')
    ELSE false
  END;
  IF ok AND NEW.status IN ('ready','collected') AND NEW.fulfilment <> 'pickup' THEN
    ok := false;
  END IF;
  IF ok AND NEW.status IN ('out_for_delivery','delivered') AND NEW.fulfilment <> 'delivery' THEN
    ok := false;
  END IF;
  -- Expiry from ready is a pickup no-show only (D-21).
  IF ok AND OLD.status = 'ready' AND NEW.status = 'expired' AND NEW.fulfilment <> 'pickup' THEN
    ok := false;
  END IF;
  IF NOT ok THEN
    RAISE EXCEPTION 'orders: invalid transition % -> %', OLD.status, NEW.status
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TABLE share_cards (
  id                   uuid PRIMARY KEY,
  store_item_id        uuid NOT NULL REFERENCES store_items (id) ON DELETE CASCADE,
  code                 text NOT NULL,
  image_key            text,
  price_paise_rendered bigint,
  rendered_at          timestamptz,
  created_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_share_cards_code CHECK (code ~ '^[0-9A-Za-z]{10}$'),
  CONSTRAINT ck_share_cards_image_key CHECK (image_key IS NULL OR image_key = 'share/' || code || '.png')
);
CREATE UNIQUE INDEX ux_share_cards_code ON share_cards (code);
CREATE UNIQUE INDEX ux_share_cards_store_item ON share_cards (store_item_id);

CREATE TABLE site_demand_marquee (
  id             uuid PRIMARY KEY,
  city_place_id  uuid NOT NULL REFERENCES places (id),
  product_id     uuid NOT NULL REFERENCES products (id),
  searchers      int NOT NULL,
  searches_total int NOT NULL,
  rank           int NOT NULL,
  built_on       date NOT NULL,
  status         text NOT NULL DEFAULT 'proposed',
  decided_by     uuid REFERENCES users (id),
  decided_at     timestamptz,
  CONSTRAINT uq_site_demand_marquee UNIQUE (city_place_id, built_on, product_id),
  CONSTRAINT ck_site_demand_marquee_counts CHECK (searchers > 0 AND searches_total >= searchers AND rank >= 1),
  CONSTRAINT ck_site_demand_marquee_status CHECK (status IN ('proposed','approved','rejected')),
  CONSTRAINT ck_site_demand_marquee_decided CHECK ((status = 'proposed') = (decided_at IS NULL))
);
CREATE INDEX ix_site_demand_marquee_city ON site_demand_marquee (city_place_id, built_on DESC, status);

-- Ask demand per day, cell, store and item (rollup_ask_demand, 01:30 IST):
-- asks counts the asks the store received, asks_without_yes those the ask
-- closed with no Yes from anyone. Read by insights (W3-03) through AskDemand.
CREATE TABLE rollup_daily_ask_demand (
  date             date NOT NULL,
  geo_cell         bigint,
  store_id         uuid NOT NULL REFERENCES stores (id),
  product_id       uuid,
  query_norm       text NOT NULL,
  asks             int NOT NULL,
  asks_without_yes int NOT NULL,
  CONSTRAINT ux_rollup_daily_ask_demand UNIQUE NULLS NOT DISTINCT (date, geo_cell, store_id, product_id, query_norm),
  CONSTRAINT ck_rollup_daily_ask_demand_counts CHECK (asks > 0 AND asks_without_yes BETWEEN 0 AND asks)
);
CREATE INDEX ix_rollup_daily_ask_demand_store ON rollup_daily_ask_demand (store_id, date);

-- New share cards per IP or anonymous device (share_card_create_per_hour,
-- 01 §11.1). key_hash is pii.Hasher.IPLimit of the IP or an HMAC of
-- "share:anon:<id>", never the value;
-- rows older than two hours are deleted as new ones arrive.
CREATE TABLE share_card_creates (
  key_hash   bytea NOT NULL,
  created_at timestamptz NOT NULL
);
CREATE INDEX ix_share_card_creates_key ON share_card_creates (key_hash, created_at);

-- +goose Down

DROP TABLE share_card_creates;
DROP TABLE rollup_daily_ask_demand;
DROP TABLE site_demand_marquee;
DROP TABLE share_cards;
ALTER TABLE orders DROP CONSTRAINT fk_orders_ask_reply_id;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION orders_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  ok boolean;
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'placed' THEN
      RAISE EXCEPTION 'orders: a new order is placed, not %', NEW.status
        USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id <> OLD.id OR NEW.number <> OLD.number OR NEW.customer_id <> OLD.customer_id
     OR NEW.store_id <> OLD.store_id OR NEW.fulfilment <> OLD.fulfilment
     OR NEW.placed_at <> OLD.placed_at OR NEW.ask_reply_id IS DISTINCT FROM OLD.ask_reply_id THEN
    RAISE EXCEPTION 'orders: the identity of an order never changes'
      USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.status = OLD.status THEN
    RETURN NEW;
  END IF;
  ok := CASE OLD.status
    WHEN 'placed' THEN NEW.status IN ('accepted','awaiting_customer','rejected','expired','cancelled')
    WHEN 'awaiting_customer' THEN NEW.status IN ('accepted','cancelled')
    WHEN 'accepted' THEN NEW.status IN ('ready','collected','expired','cancelled','out_for_delivery')
    WHEN 'ready' THEN NEW.status IN ('collected','expired','cancelled')
    WHEN 'out_for_delivery' THEN NEW.status IN ('delivered','delivery_failed','cancelled')
    ELSE false
  END;
  IF ok AND NEW.status IN ('ready','collected') AND NEW.fulfilment <> 'pickup' THEN
    ok := false;
  END IF;
  IF ok AND NEW.status IN ('out_for_delivery','delivered') AND NEW.fulfilment <> 'delivery' THEN
    ok := false;
  END IF;
  -- Expiry from ready is a pickup no-show only (D-21).
  IF ok AND OLD.status = 'ready' AND NEW.status = 'expired' AND NEW.fulfilment <> 'pickup' THEN
    ok := false;
  END IF;
  IF NOT ok THEN
    RAISE EXCEPTION 'orders: invalid transition % -> %', OLD.status, NEW.status
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
DROP TABLE ask_store_blocks;
DROP TABLE ask_recipients;
DROP TABLE asks;

-- W2-02 orders and booking (03 §4, §11; 01 §5; 02 §3.3; D-01, D-04, D-20,
-- D-21, D-22, D-23, D-28, D-60, D-65, D-68, D-69, D-92, D-101).
-- orders, order_items, order_events (the append-only timeline),
-- stock_reservations (03 §3: the only place reservations live, D-01) and
-- strikes, with order_number_seq.
--
-- Foreign keys go to tables of lower Wave 1 blocks (users 10101, stores
-- 10401, store_items and stock_movements 10501), which exist before this file
-- (D-35: only later targets wait for a constraints migration).
-- orders.ask_reply_id stays a plain uuid until W3-16 creates ask_recipients
-- (03 §13); orders.return_confirmed_by and strikes.reset_by are users ids
-- written only by later packages, so they are plain uuids with an index too.
--
-- Not built in this block (W2-02 report, Deferred): delivery, partial accept,
-- the return settlement and auto-accept. Their columns exist (03 §4) so the
-- schema does not change when they land; nothing writes them yet.
--
-- Two triggers hold the invariants the Go state machine also checks:
--   * trg_orders_guard refuses any status pair that is not in 03 §4.1 (and
--     the fulfilment it needs), a new order that is not `placed`, and a change
--     to the columns that never change after placing.
--   * trg_stock_reservations_apply keeps store_items.reserved = Σ quantity of
--     the item's held reservations (03 stock rule 2): an insert adds, a move
--     out of `held` subtracts, and nothing else may change a reservation.

-- +goose Up

------------------------------------------------------------------------------
-- order_number_seq (D-23): one global sequence, never reset. Gaps from
-- rolled-back transactions are acceptable.
------------------------------------------------------------------------------
CREATE SEQUENCE order_number_seq AS bigint START WITH 1 MINVALUE 1 NO CYCLE;

------------------------------------------------------------------------------
-- orders. Status changes only through internal/orders (and trg_orders_guard).
-- code_enc is the 4-digit pickup code sealed with ORDER_CODE_ENC_KEY, bound to
-- the order id; code_hmac is HMAC-SHA256 under ORDER_CODE_HMAC_KEY over the
-- store id and the code, unique among a store's open orders (D-04).
-- seen_at is when a staff member first listed or opened the order (B-04 and
-- the W2-05 "new order not seen" escalation).
------------------------------------------------------------------------------
CREATE TABLE orders (
  id                       uuid PRIMARY KEY,
  number                   bigint NOT NULL DEFAULT nextval('order_number_seq'),
  display_number           text GENERATED ALWAYS AS
                             ('TZ-' || lpad(number::text, greatest(6, length(number::text)), '0')) STORED,
  customer_id              uuid NOT NULL,
  store_id                 uuid NOT NULL,
  ask_reply_id             uuid,
  fulfilment               text NOT NULL,
  status                   text NOT NULL DEFAULT 'placed',
  status_reason            text,
  status_note              text,
  subtotal_paise           bigint NOT NULL,
  discount_paise           bigint NOT NULL DEFAULT 0,
  items_after_offers_paise bigint NOT NULL,
  item_subtotal_paise      bigint NOT NULL,
  credits_used             int NOT NULL DEFAULT 0,
  delivery_fee_paise       bigint NOT NULL DEFAULT 0,
  total_paise              bigint NOT NULL,
  cashback_credits         int NOT NULL DEFAULT 0,
  payment_status           text NOT NULL DEFAULT 'unpaid',
  payment_method           text,
  address_snapshot         jsonb,
  code_enc                 bytea NOT NULL,
  code_hmac                bytea NOT NULL,
  code_attempts            smallint NOT NULL DEFAULT 0,
  code_locked_at           timestamptz,
  accept_by                timestamptz,
  partial_confirm_by       timestamptz,
  hold_until               timestamptz,
  extended_at              timestamptz,
  dispatch_by              timestamptz,
  placed_at                timestamptz NOT NULL,
  accepted_at              timestamptz,
  collected_at             timestamptz,
  delivery_failed_at       timestamptz,
  return_confirmed_at      timestamptz,
  return_confirmed_by      uuid,
  return_outcome           text,
  return_reminders_sent    smallint NOT NULL DEFAULT 0,
  seen_at                  timestamptz,
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now(),
  version                  int NOT NULL DEFAULT 1,
  CONSTRAINT fk_orders_customer_id FOREIGN KEY (customer_id) REFERENCES users (id),
  CONSTRAINT fk_orders_store_id FOREIGN KEY (store_id) REFERENCES stores (id),
  CONSTRAINT ck_orders_fulfilment CHECK (fulfilment IN ('pickup','delivery')),
  CONSTRAINT ck_orders_status CHECK (status IN ('placed','awaiting_customer','accepted','ready',
    'out_for_delivery','collected','delivered','rejected','expired','cancelled','delivery_failed')),
  -- 03 §4.2: a reason per terminal status (cancelled takes the union of the
  -- customer, shop, system and admin lists); no reason on the other states.
  CONSTRAINT ck_orders_status_reason CHECK (
    (status IN ('placed','awaiting_customer','accepted','ready','out_for_delivery','collected','delivered')
      AND status_reason IS NULL)
    OR (status = 'rejected' AND status_reason IN ('out_of_stock','closing_soon','too_busy',
      'cannot_deliver_address','price_changed','other'))
    OR (status = 'expired' AND status_reason IN ('not_accepted','no_show','dispatch_timeout'))
    OR (status = 'cancelled' AND status_reason IN ('changed_mind','declined_partial','too_slow','other',
      'out_of_stock','customer_unreachable','closing_soon','partial_timeout',
      'customer_request','store_request','fraud_suspected','technical_issue'))
    OR (status = 'delivery_failed' AND status_reason IN ('customer_unreachable','address_not_found',
      'customer_refused','other'))),
  CONSTRAINT ck_orders_status_note CHECK (
    (status_note IS NULL OR char_length(status_note) BETWEEN 1 AND 200)
    AND (status_reason IS DISTINCT FROM 'other' OR status_note IS NOT NULL)),
  -- Money (03 order money columns; D-02, D-29).
  CONSTRAINT ck_orders_money CHECK (subtotal_paise >= 0 AND discount_paise >= 0 AND item_subtotal_paise >= 0
    AND credits_used >= 0 AND delivery_fee_paise >= 0 AND cashback_credits >= 0
    AND items_after_offers_paise = subtotal_paise - discount_paise
    AND items_after_offers_paise >= 0
    AND total_paise = items_after_offers_paise - credits_used::bigint * 100 + delivery_fee_paise
    AND total_paise >= 0),
  CONSTRAINT ck_orders_payment_status CHECK (payment_status IN ('unpaid','paid')),
  CONSTRAINT ck_orders_payment_method CHECK (payment_method IS NULL OR payment_method IN ('cash','upi','card','other')),
  CONSTRAINT ck_orders_paid CHECK ((payment_status = 'paid') = (payment_method IS NOT NULL)),
  CONSTRAINT ck_orders_address_snapshot CHECK (address_snapshot IS NULL OR jsonb_typeof(address_snapshot) = 'object'),
  CONSTRAINT ck_orders_code_hmac CHECK (octet_length(code_hmac) = 32),
  CONSTRAINT ck_orders_code_attempts CHECK (code_attempts >= 0),
  CONSTRAINT ck_orders_return_outcome CHECK (return_outcome IS NULL OR return_outcome IN ('returned','damaged','unconfirmed')),
  CONSTRAINT ck_orders_return_reminders CHECK (return_reminders_sent BETWEEN 0 AND 2),
  CONSTRAINT ck_orders_number CHECK (number > 0)
);
CREATE UNIQUE INDEX ux_orders_number ON orders (number);
CREATE INDEX ix_orders_store_status_placed ON orders (store_id, status, placed_at DESC);
-- The store's inbox (ListStoreOrders): a tab is several statuses, newest
-- first with id as the tie-break and the keyset cursor, so the scan walks the
-- store's orders in that order and stops at the page size.
CREATE INDEX ix_orders_store_placed ON orders (store_id, placed_at DESC, id DESC);
CREATE INDEX ix_orders_customer_placed ON orders (customer_id, placed_at DESC);
-- Timer stragglers after a restart (03 §11).
CREATE INDEX ix_orders_accept_by ON orders (accept_by) WHERE status = 'placed';
CREATE INDEX ix_orders_partial_confirm_by ON orders (partial_confirm_by) WHERE status = 'awaiting_customer';
CREATE INDEX ix_orders_hold_until ON orders (hold_until)
  WHERE status IN ('accepted','ready') AND fulfilment = 'pickup';
CREATE INDEX ix_orders_dispatch_by ON orders (dispatch_by)
  WHERE status = 'accepted' AND fulfilment = 'delivery';
CREATE INDEX ix_orders_delivery_failed_at ON orders (delivery_failed_at)
  WHERE status = 'delivery_failed' AND return_confirmed_at IS NULL;
-- D-04: a code is unique among a store's open orders (generation retries).
CREATE UNIQUE INDEX ux_orders_store_code_active ON orders (store_id, code_hmac)
  WHERE status IN ('placed','awaiting_customer','accepted','ready','out_for_delivery');
-- D-101: one live order per ask reply.
CREATE UNIQUE INDEX ux_orders_ask_reply_active ON orders (ask_reply_id)
  WHERE ask_reply_id IS NOT NULL AND status NOT IN ('cancelled','expired','rejected');
-- notice.UnseenOrders (W2-05 escalation): placed and not seen, oldest first.
CREATE INDEX ix_orders_unseen ON orders (placed_at, id) WHERE status = 'placed' AND seen_at IS NULL;
CREATE INDEX ix_orders_return_confirmed_by ON orders (return_confirmed_by) WHERE return_confirmed_by IS NOT NULL;
CREATE TRIGGER trg_orders_touch BEFORE UPDATE ON orders
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- orders_guard: 03 §4.1 in the database. Who may make a move (customer,
-- staff role, system, admin) and its time guards are checked in Go; this
-- refuses any (from, to) pair the table does not list, the fulfilment-only
-- moves on the wrong fulfilment, a new order that is not placed, and a change
-- to the identity of an order.
-- +goose StatementBegin
CREATE FUNCTION orders_guard() RETURNS trigger
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
CREATE TRIGGER trg_orders_guard BEFORE INSERT OR UPDATE ON orders
  FOR EACH ROW EXECUTE FUNCTION orders_guard();

------------------------------------------------------------------------------
-- order_items: the lines as placed. product_snapshot holds what the customer
-- saw (name, variant, pack size, unit, image) so a later catalogue change
-- does not rewrite history. price_source 'ask_reply' only on the line of the
-- order's ask reply (D-101).
------------------------------------------------------------------------------
CREATE TABLE order_items (
  id                 uuid PRIMARY KEY,
  order_id           uuid NOT NULL,
  store_item_id      uuid NOT NULL,
  product_snapshot   jsonb NOT NULL,
  unit_price_paise   bigint NOT NULL,
  quantity_requested int NOT NULL,
  quantity_accepted  int,
  line_total_paise   bigint NOT NULL,
  price_source       text NOT NULL DEFAULT 'catalogue',
  created_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_order_items_order_id FOREIGN KEY (order_id) REFERENCES orders (id),
  CONSTRAINT fk_order_items_store_item_id FOREIGN KEY (store_item_id) REFERENCES store_items (id),
  CONSTRAINT ck_order_items_snapshot CHECK (jsonb_typeof(product_snapshot) = 'object'),
  CONSTRAINT ck_order_items_unit_price CHECK (unit_price_paise > 0),
  CONSTRAINT ck_order_items_quantity CHECK (quantity_requested > 0
    AND (quantity_accepted IS NULL OR quantity_accepted BETWEEN 0 AND quantity_requested)),
  CONSTRAINT ck_order_items_line_total CHECK (line_total_paise >= 0),
  CONSTRAINT ck_order_items_price_source CHECK (price_source IN ('catalogue','ask_reply'))
);
CREATE UNIQUE INDEX ux_order_items_order_item ON order_items (order_id, store_item_id);
CREATE INDEX ix_order_items_store_item_id ON order_items (store_item_id);

------------------------------------------------------------------------------
-- order_events: the append-only timeline. from_status is null on the
-- creation row. meta never carries the pickup code.
------------------------------------------------------------------------------
CREATE TABLE order_events (
  id          uuid PRIMARY KEY,
  order_id    uuid NOT NULL,
  from_status text,
  to_status   text NOT NULL,
  actor_kind  text NOT NULL,
  actor_id    uuid,
  reason      text,
  meta        jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at  timestamptz NOT NULL,
  CONSTRAINT fk_order_events_order_id FOREIGN KEY (order_id) REFERENCES orders (id),
  CONSTRAINT ck_order_events_from CHECK (from_status IS NULL OR from_status IN ('placed','awaiting_customer',
    'accepted','ready','out_for_delivery','collected','delivered','rejected','expired','cancelled','delivery_failed')),
  CONSTRAINT ck_order_events_to CHECK (to_status IN ('placed','awaiting_customer','accepted','ready',
    'out_for_delivery','collected','delivered','rejected','expired','cancelled','delivery_failed')),
  CONSTRAINT ck_order_events_actor CHECK (actor_kind IN ('customer','staff','system','admin')),
  CONSTRAINT ck_order_events_actor_id CHECK (actor_kind = 'system' OR actor_id IS NOT NULL),
  CONSTRAINT ck_order_events_reason CHECK (reason IS NULL OR reason ~ '^[a-z][a-z_]*$'),
  CONSTRAINT ck_order_events_meta CHECK (jsonb_typeof(meta) = 'object')
);
CREATE INDEX ix_order_events_order_created ON order_events (order_id, created_at, id);
CREATE INDEX ix_order_events_actor_id ON order_events (actor_id) WHERE actor_id IS NOT NULL;
CREATE TRIGGER trg_order_events_append_only BEFORE UPDATE OR DELETE ON order_events
  FOR EACH ROW EXECUTE FUNCTION append_only();
CREATE TRIGGER trg_order_events_no_truncate BEFORE TRUNCATE ON order_events
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();
REVOKE UPDATE, DELETE, TRUNCATE ON order_events FROM todayz_app;

------------------------------------------------------------------------------
-- stock_reservations (03 §3, D-01): one per accepted line. held on accept;
-- consumed by the sale_online movement at collect; released on every other
-- end. Rows are never deleted.
------------------------------------------------------------------------------
CREATE TABLE stock_reservations (
  id                   uuid PRIMARY KEY,
  order_id             uuid NOT NULL,
  order_item_id        uuid NOT NULL,
  store_item_id        uuid NOT NULL,
  quantity             int NOT NULL,
  status               text NOT NULL DEFAULT 'held',
  consumed_movement_id uuid,
  released_reason      text,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_stock_reservations_order_id FOREIGN KEY (order_id) REFERENCES orders (id),
  CONSTRAINT fk_stock_reservations_order_item_id FOREIGN KEY (order_item_id) REFERENCES order_items (id),
  CONSTRAINT fk_stock_reservations_store_item_id FOREIGN KEY (store_item_id) REFERENCES store_items (id),
  CONSTRAINT fk_stock_reservations_movement FOREIGN KEY (consumed_movement_id) REFERENCES stock_movements (id),
  CONSTRAINT ck_stock_reservations_quantity CHECK (quantity > 0),
  CONSTRAINT ck_stock_reservations_status CHECK (status IN ('held','consumed','released')),
  CONSTRAINT ck_stock_reservations_consumed CHECK ((status = 'consumed') = (consumed_movement_id IS NOT NULL)),
  CONSTRAINT ck_stock_reservations_released CHECK ((status = 'released') = (released_reason IS NOT NULL)),
  CONSTRAINT ck_stock_reservations_released_reason CHECK (released_reason IS NULL OR released_reason ~ '^[a-z][a-z_]*$')
);
CREATE UNIQUE INDEX ux_stock_reservations_order_item ON stock_reservations (order_item_id);
CREATE INDEX ix_stock_reservations_held ON stock_reservations (store_item_id) WHERE status = 'held';
CREATE INDEX ix_stock_reservations_order_id ON stock_reservations (order_id);
CREATE INDEX ix_stock_reservations_movement ON stock_reservations (consumed_movement_id)
  WHERE consumed_movement_id IS NOT NULL;
CREATE TRIGGER trg_stock_reservations_touch BEFORE UPDATE ON stock_reservations
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
REVOKE DELETE, TRUNCATE ON stock_reservations FROM todayz_app;

-- stock_reservations_apply: store_items.reserved = Σ held quantity (03 stock
-- rule 2). A reservation is inserted held; it then moves once, to consumed or
-- released; its order, line, item and quantity never change; it is never
-- deleted.
-- +goose StatementBegin
CREATE FUNCTION stock_reservations_apply() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'held' THEN
      RAISE EXCEPTION 'stock_reservations: a reservation is inserted held'
        USING ERRCODE = 'check_violation';
    END IF;
    UPDATE public.store_items SET reserved = reserved + NEW.quantity WHERE id = NEW.store_item_id;
    RETURN NULL;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'stock_reservations: rows are never deleted'
      USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.order_id <> OLD.order_id OR NEW.order_item_id <> OLD.order_item_id
     OR NEW.store_item_id <> OLD.store_item_id OR NEW.quantity <> OLD.quantity THEN
    RAISE EXCEPTION 'stock_reservations: only the status of a reservation changes'
      USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.status = OLD.status THEN
    IF NEW.consumed_movement_id IS DISTINCT FROM OLD.consumed_movement_id
       OR NEW.released_reason IS DISTINCT FROM OLD.released_reason THEN
      RAISE EXCEPTION 'stock_reservations: a settled reservation never changes'
        USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
  END IF;
  IF OLD.status <> 'held' THEN
    RAISE EXCEPTION 'stock_reservations: % is final', OLD.status
      USING ERRCODE = 'check_violation';
  END IF;
  UPDATE public.store_items SET reserved = reserved - OLD.quantity WHERE id = OLD.store_item_id;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_stock_reservations_apply AFTER INSERT OR UPDATE OR DELETE ON stock_reservations
  FOR EACH ROW EXECUTE FUNCTION stock_reservations_apply();

------------------------------------------------------------------------------
-- strikes (01 §5.4, D-67, D-68): customer no-shows here; store strikes
-- (dispatch_timeout) and admin_cancel come with delivery and the admin
-- console. One strike per order and side. Reset by support (later), audited.
------------------------------------------------------------------------------
CREATE TABLE strikes (
  id           uuid PRIMARY KEY,
  subject_type text NOT NULL,
  subject_id   uuid NOT NULL,
  order_id     uuid,
  reason       text NOT NULL,
  created_at   timestamptz NOT NULL,
  reset_at     timestamptz,
  reset_by     uuid,
  reset_reason text,
  CONSTRAINT fk_strikes_order_id FOREIGN KEY (order_id) REFERENCES orders (id),
  CONSTRAINT ck_strikes_subject_type CHECK (subject_type IN ('customer','store')),
  CONSTRAINT ck_strikes_reason CHECK (reason IN ('no_show','dispatch_timeout','admin_cancel')),
  CONSTRAINT ck_strikes_reset CHECK ((reset_at IS NULL) = (reset_reason IS NULL)
    AND (reset_by IS NULL OR reset_at IS NOT NULL)),
  CONSTRAINT ck_strikes_reset_reason CHECK (reset_reason IS NULL OR char_length(reset_reason) BETWEEN 1 AND 500)
);
CREATE UNIQUE INDEX ux_strikes_order_subject ON strikes (order_id, subject_type) WHERE order_id IS NOT NULL;
CREATE INDEX ix_strikes_subject_active ON strikes (subject_type, subject_id, created_at DESC) WHERE reset_at IS NULL;
CREATE INDEX ix_strikes_reset_by ON strikes (reset_by) WHERE reset_by IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS strikes;
DROP TRIGGER IF EXISTS trg_stock_reservations_apply ON stock_reservations;
DROP FUNCTION IF EXISTS stock_reservations_apply();
DROP TABLE IF EXISTS stock_reservations;
DROP TABLE IF EXISTS order_events;
DROP TABLE IF EXISTS order_items;
DROP TRIGGER IF EXISTS trg_orders_guard ON orders;
DROP FUNCTION IF EXISTS orders_guard();
DROP INDEX IF EXISTS ix_orders_store_placed;
DROP TABLE IF EXISTS orders;
DROP SEQUENCE IF EXISTS order_number_seq;

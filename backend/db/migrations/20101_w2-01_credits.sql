-- W2-01 credits ledger (03 §6, §11; 01 §7, §9; D-02, D-08, D-27, D-28,
-- D-29, D-30, D-31, D-35, D-62, D-76).
-- ledger_accounts, ledger_transactions, ledger_entries (double entry),
-- account_balances (the cache), credit_holds, credit_lots and
-- credit_lot_consumptions. admin_adjustment_requests is W3-01's.
--
-- Foreign keys go to users (10101) and stores (10401), lower blocks.
-- credit_holds.order_id (orders, 20201) and bill_claim_id (W2-04) are plain
-- uuids with an index (D-35): both tables are in higher blocks.
--
-- What the database holds by itself, so a bug in Go cannot break it:
--   * trg_ledger_entries_balanced (deferred): Σ amount of a transaction = 0
--     at commit (invariant 1).
--   * trg_ledger_entries_apply: every entry adds its amount to the account's
--     account_balances row in the same statement (invariant 7). The posting
--     code locks those rows FOR UPDATE first and checks the balances there.
--   * ck_account_balances_not_negative: customer_wallet and customer_held
--     never go below 0 (invariant 2; the second wall behind the Go check).
--   * trg_credit_lot_consumptions_apply: a lot's remaining and held change
--     only by inserting a consumption row (invariant 8 holds by construction;
--     the nightly reconcile still checks it).
--   * ledger_transactions, ledger_entries and credit_lot_consumptions are
--     append-only (trigger + REVOKE, CR-19).

-- +goose Up

------------------------------------------------------------------------------
-- ledger_accounts: one per (kind, owner). Customer and store accounts are
-- created on first use; the four platform accounts are created here.
------------------------------------------------------------------------------
CREATE TABLE ledger_accounts (
  id         uuid PRIMARY KEY,
  kind       text NOT NULL,
  owner_type text NOT NULL,
  owner_id   uuid,
  currency   text NOT NULL DEFAULT 'credit',
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ux_ledger_accounts_kind_owner UNIQUE NULLS NOT DISTINCT (kind, owner_id),
  CONSTRAINT ck_ledger_accounts_kind CHECK (kind IN ('customer_wallet','customer_held','store_credit',
    'platform_marketing','platform_expired','platform_adjustments','platform_payouts_cash')),
  CONSTRAINT ck_ledger_accounts_owner CHECK (
    (kind IN ('customer_wallet','customer_held') AND owner_type = 'customer' AND owner_id IS NOT NULL) OR
    (kind = 'store_credit' AND owner_type = 'store' AND owner_id IS NOT NULL) OR
    (kind LIKE 'platform\_%' AND owner_type = 'platform' AND owner_id IS NULL)),
  CONSTRAINT ck_ledger_accounts_currency CHECK (currency = 'credit')
);
CREATE INDEX ix_ledger_accounts_owner ON ledger_accounts (owner_id) WHERE owner_id IS NOT NULL;
REVOKE UPDATE, DELETE, TRUNCATE ON ledger_accounts FROM todayz_app;

------------------------------------------------------------------------------
-- ledger_transactions: one per posting. idempotency_key makes a repeat of the
-- same posting return the first (invariant 6).
------------------------------------------------------------------------------
CREATE TABLE ledger_transactions (
  id              uuid PRIMARY KEY,
  kind            text NOT NULL,
  ref_type        text,
  ref_id          uuid,
  idempotency_key text NOT NULL,
  created_by      uuid,
  memo            text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ux_ledger_transactions_idempotency_key UNIQUE (idempotency_key),
  CONSTRAINT fk_ledger_transactions_created_by FOREIGN KEY (created_by) REFERENCES users (id),
  CONSTRAINT ck_ledger_transactions_kind CHECK (kind IN ('spend_hold','spend_capture','spend_release',
    'cashback','referral_reward','expiry','admin_adjustment','payout','reversal','forfeit_deletion')),
  CONSTRAINT ck_ledger_transactions_ref CHECK ((ref_type IS NULL) = (ref_id IS NULL)),
  CONSTRAINT ck_ledger_transactions_ref_type CHECK (ref_type IS NULL OR ref_type ~ '^[a-z][a-z_]{0,30}$'),
  CONSTRAINT ck_ledger_transactions_key CHECK (length(idempotency_key) BETWEEN 1 AND 200),
  CONSTRAINT ck_ledger_transactions_memo CHECK (memo IS NULL OR length(memo) <= 500)
);
CREATE INDEX ix_ledger_transactions_ref ON ledger_transactions (ref_type, ref_id) WHERE ref_id IS NOT NULL;
CREATE INDEX ix_ledger_transactions_created_by ON ledger_transactions (created_by) WHERE created_by IS NOT NULL;
CREATE TRIGGER trg_ledger_transactions_append_only BEFORE UPDATE OR DELETE ON ledger_transactions
  FOR EACH ROW EXECUTE FUNCTION append_only();
CREATE TRIGGER trg_ledger_transactions_no_truncate BEFORE TRUNCATE ON ledger_transactions
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();
REVOKE UPDATE, DELETE, TRUNCATE ON ledger_transactions FROM todayz_app;

------------------------------------------------------------------------------
-- account_balances: the cache of Σ entries per account (invariant 7),
-- written only by trg_ledger_entries_apply. kind is copied from the account
-- so the non-negative rule can be a CHECK.
------------------------------------------------------------------------------
CREATE TABLE account_balances (
  account_id uuid PRIMARY KEY,
  kind       text NOT NULL,
  balance    bigint NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_account_balances_account_id FOREIGN KEY (account_id) REFERENCES ledger_accounts (id),
  CONSTRAINT ck_account_balances_not_negative CHECK (
    kind NOT IN ('customer_wallet','customer_held') OR balance >= 0)
);
REVOKE DELETE, TRUNCATE ON account_balances FROM todayz_app;

------------------------------------------------------------------------------
-- ledger_entries: signed amounts in credits; + increases the account.
------------------------------------------------------------------------------
CREATE TABLE ledger_entries (
  id             uuid PRIMARY KEY,
  transaction_id uuid NOT NULL,
  account_id     uuid NOT NULL,
  amount         bigint NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_ledger_entries_transaction_id FOREIGN KEY (transaction_id) REFERENCES ledger_transactions (id),
  CONSTRAINT fk_ledger_entries_account_id FOREIGN KEY (account_id) REFERENCES ledger_accounts (id),
  CONSTRAINT ck_ledger_entries_amount CHECK (amount <> 0)
);
CREATE INDEX ix_ledger_entries_account_created ON ledger_entries (account_id, created_at, id);
CREATE INDEX ix_ledger_entries_transaction_id ON ledger_entries (transaction_id);
CREATE TRIGGER trg_ledger_entries_append_only BEFORE UPDATE OR DELETE ON ledger_entries
  FOR EACH ROW EXECUTE FUNCTION append_only();
CREATE TRIGGER trg_ledger_entries_no_truncate BEFORE TRUNCATE ON ledger_entries
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();
REVOKE UPDATE, DELETE, TRUNCATE ON ledger_entries FROM todayz_app;

-- ledger_entries_balanced: at commit, the entry's transaction sums to 0.
-- +goose StatementBegin
CREATE FUNCTION ledger_entries_balanced() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  total bigint;
BEGIN
  SELECT coalesce(sum(e.amount), 0) INTO total
  FROM public.ledger_entries e WHERE e.transaction_id = NEW.transaction_id;
  IF total <> 0 THEN
    RAISE EXCEPTION 'ledger transaction % does not balance (sum %)', NEW.transaction_id, total
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER trg_ledger_entries_balanced AFTER INSERT ON ledger_entries
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION ledger_entries_balanced();

-- ledger_entries_apply: the entry moves its account's cached balance.
-- +goose StatementBegin
CREATE FUNCTION ledger_entries_apply() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  UPDATE public.account_balances SET balance = balance + NEW.amount, updated_at = NEW.created_at
  WHERE account_id = NEW.account_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'ledger account % has no balance row', NEW.account_id
      USING ERRCODE = 'foreign_key_violation';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_ledger_entries_apply AFTER INSERT ON ledger_entries
  FOR EACH ROW EXECUTE FUNCTION ledger_entries_apply();

------------------------------------------------------------------------------
-- credit_holds: credits held for one order or one bill claim (D-35).
-- amount is what was held; released and captured grow from 0. A re-cap after
-- a partial accept (D-28) releases part and stays held. Caps (D-30) count
-- the holds that are held or captured, by created_at.
------------------------------------------------------------------------------
CREATE TABLE credit_holds (
  id            uuid PRIMARY KEY,
  customer_id   uuid NOT NULL,
  store_id      uuid NOT NULL,
  order_id      uuid,
  bill_claim_id uuid,
  amount        int NOT NULL,
  captured      int NOT NULL DEFAULT 0,
  released      int NOT NULL DEFAULT 0,
  status        text NOT NULL DEFAULT 'held',
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  settled_at    timestamptz,
  CONSTRAINT fk_credit_holds_customer_id FOREIGN KEY (customer_id) REFERENCES users (id),
  CONSTRAINT fk_credit_holds_store_id FOREIGN KEY (store_id) REFERENCES stores (id),
  CONSTRAINT ck_credit_holds_ref CHECK ((order_id IS NULL) <> (bill_claim_id IS NULL)),
  CONSTRAINT ck_credit_holds_amount CHECK (amount > 0 AND captured >= 0 AND released >= 0
    AND captured + released <= amount),
  CONSTRAINT ck_credit_holds_status CHECK (
    (status = 'held' AND captured = 0 AND released < amount AND settled_at IS NULL) OR
    (status = 'captured' AND captured > 0 AND captured + released = amount AND settled_at IS NOT NULL) OR
    (status = 'released' AND captured = 0 AND released = amount AND settled_at IS NOT NULL))
);
CREATE UNIQUE INDEX ux_credit_holds_order_id ON credit_holds (order_id) WHERE order_id IS NOT NULL;
CREATE UNIQUE INDEX ux_credit_holds_bill_claim_id ON credit_holds (bill_claim_id) WHERE bill_claim_id IS NOT NULL;
CREATE INDEX ix_credit_holds_customer_created ON credit_holds (customer_id, created_at)
  WHERE status IN ('held','captured');
CREATE INDEX ix_credit_holds_store_created ON credit_holds (store_id, created_at)
  WHERE status IN ('held','captured');
CREATE TRIGGER trg_credit_holds_touch BEFORE UPDATE ON credit_holds
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
REVOKE DELETE, TRUNCATE ON credit_holds FROM todayz_app;

------------------------------------------------------------------------------
-- credit_lots (D-27): every credit into a customer_wallet from cashback, a
-- referral reward or a positive admin adjustment. expires_at is 00:00 IST
-- on the day after the last usable day (01 §7). expired_at is when the
-- expiry job (or a forfeiture) closed the lot; reminded_at when the
-- customer was told it is expiring.
------------------------------------------------------------------------------
CREATE TABLE credit_lots (
  id                    uuid PRIMARY KEY,
  customer_id           uuid NOT NULL,
  customer_account_id   uuid NOT NULL,
  source_transaction_id uuid NOT NULL,
  source_kind           text NOT NULL,
  original              int NOT NULL,
  remaining             int NOT NULL,
  held                  int NOT NULL DEFAULT 0,
  earned_at             timestamptz NOT NULL,
  expires_at            timestamptz NOT NULL,
  expired_at            timestamptz,
  reminded_at           timestamptz,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_credit_lots_customer_id FOREIGN KEY (customer_id) REFERENCES users (id),
  CONSTRAINT fk_credit_lots_customer_account_id FOREIGN KEY (customer_account_id) REFERENCES ledger_accounts (id),
  CONSTRAINT fk_credit_lots_source_transaction_id FOREIGN KEY (source_transaction_id) REFERENCES ledger_transactions (id),
  CONSTRAINT ck_credit_lots_source_kind CHECK (source_kind IN ('cashback','referral_reward','admin_adjustment')),
  CONSTRAINT ck_credit_lots_amounts CHECK (original > 0 AND remaining >= 0 AND held >= 0 AND remaining + held <= original),
  CONSTRAINT ck_credit_lots_expiry CHECK (expires_at > earned_at)
);
CREATE INDEX ix_credit_lots_customer_expires ON credit_lots (customer_id, expires_at, id)
  WHERE remaining > 0 OR held > 0;
CREATE INDEX ix_credit_lots_due ON credit_lots (expires_at) WHERE expired_at IS NULL;
CREATE INDEX ix_credit_lots_customer_account_id ON credit_lots (customer_account_id);
CREATE INDEX ix_credit_lots_source_transaction_id ON credit_lots (source_transaction_id);
CREATE TRIGGER trg_credit_lots_touch BEFORE UPDATE ON credit_lots
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
REVOKE DELETE, TRUNCATE ON credit_lots FROM todayz_app;

------------------------------------------------------------------------------
-- credit_lot_consumptions (D-08, D-27): the append-only record of every
-- change to a lot. Inserting one applies its deltas to the lot.
------------------------------------------------------------------------------
CREATE TABLE credit_lot_consumptions (
  id              uuid PRIMARY KEY,
  lot_id          uuid NOT NULL,
  transaction_id  uuid NOT NULL,
  hold_id         uuid,
  kind            text NOT NULL,
  remaining_delta int NOT NULL,
  held_delta      int NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_credit_lot_consumptions_lot_id FOREIGN KEY (lot_id) REFERENCES credit_lots (id),
  CONSTRAINT fk_credit_lot_consumptions_transaction_id FOREIGN KEY (transaction_id) REFERENCES ledger_transactions (id),
  CONSTRAINT fk_credit_lot_consumptions_hold_id FOREIGN KEY (hold_id) REFERENCES credit_holds (id),
  CONSTRAINT ck_credit_lot_consumptions_kind CHECK (kind IN ('hold','capture','release','expiry',
    'clawback','adjustment','forfeit','restore')),
  CONSTRAINT ck_credit_lot_consumptions_delta CHECK (remaining_delta <> 0 OR held_delta <> 0),
  CONSTRAINT ck_credit_lot_consumptions_hold CHECK (kind NOT IN ('hold','capture','release') OR hold_id IS NOT NULL)
);
CREATE INDEX ix_credit_lot_consumptions_lot_id ON credit_lot_consumptions (lot_id);
CREATE INDEX ix_credit_lot_consumptions_transaction_id ON credit_lot_consumptions (transaction_id);
CREATE INDEX ix_credit_lot_consumptions_hold_id ON credit_lot_consumptions (hold_id) WHERE hold_id IS NOT NULL;
CREATE TRIGGER trg_credit_lot_consumptions_append_only BEFORE UPDATE OR DELETE ON credit_lot_consumptions
  FOR EACH ROW EXECUTE FUNCTION append_only();
CREATE TRIGGER trg_credit_lot_consumptions_no_truncate BEFORE TRUNCATE ON credit_lot_consumptions
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();
REVOKE UPDATE, DELETE, TRUNCATE ON credit_lot_consumptions FROM todayz_app;

-- credit_lot_consumptions_apply: the row's deltas move its lot. The lot's
-- CHECK refuses a lot driven below 0 or above its original.
-- +goose StatementBegin
CREATE FUNCTION credit_lot_consumptions_apply() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  UPDATE public.credit_lots
  SET remaining = remaining + NEW.remaining_delta, held = held + NEW.held_delta
  WHERE id = NEW.lot_id;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_credit_lot_consumptions_apply AFTER INSERT ON credit_lot_consumptions
  FOR EACH ROW EXECUTE FUNCTION credit_lot_consumptions_apply();

------------------------------------------------------------------------------
-- The four platform accounts (03 §6), with fixed ids.
------------------------------------------------------------------------------
INSERT INTO ledger_accounts (id, kind, owner_type) VALUES
  ('01930000-0000-7000-8000-000000000001', 'platform_marketing', 'platform'),
  ('01930000-0000-7000-8000-000000000002', 'platform_expired', 'platform'),
  ('01930000-0000-7000-8000-000000000003', 'platform_adjustments', 'platform'),
  ('01930000-0000-7000-8000-000000000004', 'platform_payouts_cash', 'platform');
INSERT INTO account_balances (account_id, kind) VALUES
  ('01930000-0000-7000-8000-000000000001', 'platform_marketing'),
  ('01930000-0000-7000-8000-000000000002', 'platform_expired'),
  ('01930000-0000-7000-8000-000000000003', 'platform_adjustments'),
  ('01930000-0000-7000-8000-000000000004', 'platform_payouts_cash');

-- +goose Down
DROP TRIGGER IF EXISTS trg_credit_lot_consumptions_apply ON credit_lot_consumptions;
DROP FUNCTION IF EXISTS credit_lot_consumptions_apply();
DROP TABLE IF EXISTS credit_lot_consumptions;
DROP TABLE IF EXISTS credit_lots;
DROP TABLE IF EXISTS credit_holds;
DROP TRIGGER IF EXISTS trg_ledger_entries_apply ON ledger_entries;
DROP FUNCTION IF EXISTS ledger_entries_apply();
DROP TRIGGER IF EXISTS trg_ledger_entries_balanced ON ledger_entries;
DROP FUNCTION IF EXISTS ledger_entries_balanced();
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS account_balances;
DROP TABLE IF EXISTS ledger_transactions;
DROP TABLE IF EXISTS ledger_accounts;

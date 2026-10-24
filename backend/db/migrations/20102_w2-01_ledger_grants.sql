-- W2-01 ledger grants (CR-19, D-123). In 20101 todayz_app could still
-- UPDATE account_balances and credit_lots directly, so a bug or an injected
-- statement could move a cached balance or a lot without a ledger entry or a
-- consumption row. Now:
--   * the two apply triggers run as the owner (SECURITY DEFINER, search_path
--     already fixed), so they still move the rows when the app inserts an
--     entry or a consumption;
--   * todayz_app keeps UPDATE only on the columns it writes itself:
--     account_balances.updated_at (none; it is there so SELECT … FOR UPDATE,
--     which needs UPDATE on one column, keeps working) and
--     credit_lots.expired_at and reminded_at (the expiry and reminder jobs);
--   * todayz_app inserts a balance row only with account_id, kind and
--     updated_at, so a new row always starts at 0.
--   * a balance row's kind must be its account's kind (a composite foreign
--     key on ledger_accounts (id, kind)), so a row cannot claim another
--     kind's rules (the non-negative check covers customer kinds only).
-- A direct UPDATE of a balance, a lot's remaining or held, or an INSERT with
-- a balance is refused with 42501, and a balance row of another kind with
-- 23503 (TestAppendOnlyDB). CR-19 means no direct UPDATE of balances or lots;
-- a forged INSERT of a lot needs arbitrary SQL and is caught by the nightly
-- reconcile (D-123).

-- +goose Up
ALTER FUNCTION ledger_entries_apply() SECURITY DEFINER;
ALTER FUNCTION credit_lot_consumptions_apply() SECURITY DEFINER;

REVOKE INSERT, UPDATE ON account_balances FROM todayz_app;
GRANT INSERT (account_id, kind, updated_at) ON account_balances TO todayz_app;
GRANT UPDATE (updated_at) ON account_balances TO todayz_app;

REVOKE UPDATE ON credit_lots FROM todayz_app;
GRANT UPDATE (expired_at, reminded_at) ON credit_lots TO todayz_app;

ALTER TABLE ledger_accounts ADD CONSTRAINT ux_ledger_accounts_id_kind UNIQUE (id, kind);
ALTER TABLE account_balances ADD CONSTRAINT fk_account_balances_account_kind
  FOREIGN KEY (account_id, kind) REFERENCES ledger_accounts (id, kind);

-- +goose Down
ALTER TABLE account_balances DROP CONSTRAINT fk_account_balances_account_kind;
ALTER TABLE ledger_accounts DROP CONSTRAINT ux_ledger_accounts_id_kind;

REVOKE UPDATE (expired_at, reminded_at) ON credit_lots FROM todayz_app;
GRANT UPDATE ON credit_lots TO todayz_app;

REVOKE UPDATE (updated_at) ON account_balances FROM todayz_app;
REVOKE INSERT (account_id, kind, updated_at) ON account_balances FROM todayz_app;
GRANT INSERT, UPDATE ON account_balances TO todayz_app;

ALTER FUNCTION credit_lot_consumptions_apply() SECURITY INVOKER;
ALTER FUNCTION ledger_entries_apply() SECURITY INVOKER;

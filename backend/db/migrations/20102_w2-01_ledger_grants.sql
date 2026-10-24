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
-- A direct UPDATE of a balance, a lot's remaining or held, or an INSERT with
-- a balance is refused with 42501 (TestAppendOnlyDB).

-- +goose Up
ALTER FUNCTION ledger_entries_apply() SECURITY DEFINER;
ALTER FUNCTION credit_lot_consumptions_apply() SECURITY DEFINER;

REVOKE INSERT, UPDATE ON account_balances FROM todayz_app;
GRANT INSERT (account_id, kind, updated_at) ON account_balances TO todayz_app;
GRANT UPDATE (updated_at) ON account_balances TO todayz_app;

REVOKE UPDATE ON credit_lots FROM todayz_app;
GRANT UPDATE (expired_at, reminded_at) ON credit_lots TO todayz_app;

-- +goose Down
REVOKE UPDATE (expired_at, reminded_at) ON credit_lots FROM todayz_app;
GRANT UPDATE ON credit_lots TO todayz_app;

REVOKE UPDATE (updated_at) ON account_balances FROM todayz_app;
REVOKE INSERT (account_id, kind, updated_at) ON account_balances FROM todayz_app;
GRANT INSERT, UPDATE ON account_balances TO todayz_app;

ALTER FUNCTION credit_lot_consumptions_apply() SECURITY INVOKER;
ALTER FUNCTION ledger_entries_apply() SECURITY INVOKER;

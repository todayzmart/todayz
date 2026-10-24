-- credits queries (W2-01), generated into package creditsdb (gen/sqlc/creditsdb).
-- Only the work package that builds internal/credits edits this folder (D-50).

------------------------------------------------------------------------------
-- Accounts and balances
------------------------------------------------------------------------------

-- name: AccountID :one
SELECT id FROM ledger_accounts WHERE kind = @kind AND owner_id = @owner_id;

-- InsertAccount creates a customer or store account and its balance row; a
-- concurrent first use of the same account inserts nothing.
-- name: InsertAccount :exec
WITH ins AS (
  INSERT INTO ledger_accounts (id, kind, owner_type, owner_id, created_at)
  VALUES (@id, @kind, @owner_type, @owner_id, @now)
  ON CONFLICT ON CONSTRAINT ux_ledger_accounts_kind_owner DO NOTHING
  RETURNING id, kind)
INSERT INTO account_balances (account_id, kind, updated_at) SELECT ins.id, ins.kind, @now FROM ins;

-- LockBalances locks the posting's accounts in id order (the one lock order).
-- name: LockBalances :many
SELECT account_id, kind, balance FROM account_balances
WHERE account_id = ANY(@ids::uuid[]) ORDER BY account_id FOR UPDATE;

-- name: CustomerBalances :one
SELECT
  coalesce(sum(b.balance) FILTER (WHERE a.kind = 'customer_wallet'), 0)::bigint AS wallet,
  coalesce(sum(b.balance) FILTER (WHERE a.kind = 'customer_held'), 0)::bigint AS held
FROM ledger_accounts a JOIN account_balances b ON b.account_id = a.id
WHERE a.owner_id = @customer_id AND a.kind IN ('customer_wallet', 'customer_held');

------------------------------------------------------------------------------
-- Transactions and entries
------------------------------------------------------------------------------

-- InsertTransaction returns no row when the key has posted before.
-- name: InsertTransaction :one
INSERT INTO ledger_transactions (id, kind, ref_type, ref_id, idempotency_key, created_by, memo, created_at)
VALUES (@id, @kind, sqlc.narg(ref_type), @ref_id, @idempotency_key, @created_by, sqlc.narg(memo), @now)
ON CONFLICT ON CONSTRAINT ux_ledger_transactions_idempotency_key DO NOTHING
RETURNING id;

-- name: TransactionByKey :one
SELECT id FROM ledger_transactions WHERE idempotency_key = @idempotency_key;

-- KeyAmount is what accounts of a kind got from the posting with this key.
-- PostedLegs are the kind, ref and entries of the posting with this key.
-- name: PostedLegs :many
SELECT t.kind, t.ref_type, t.ref_id, e.account_id, e.amount FROM ledger_transactions t
JOIN ledger_entries e ON e.transaction_id = t.id
WHERE t.idempotency_key = @idempotency_key;

-- name: KeyAmount :one
SELECT coalesce(sum(e.amount), 0)::bigint
FROM ledger_transactions t
JOIN ledger_entries e ON e.transaction_id = t.id
JOIN ledger_accounts a ON a.id = e.account_id
WHERE t.idempotency_key = @idempotency_key AND a.kind = @kind;

-- name: InsertEntry :exec
INSERT INTO ledger_entries (id, transaction_id, account_id, amount, created_at)
VALUES (@id, @transaction_id, @account_id, @amount, @now);

------------------------------------------------------------------------------
-- Lots
------------------------------------------------------------------------------

-- name: InsertLot :exec
INSERT INTO credit_lots (id, customer_id, customer_account_id, source_transaction_id, source_kind,
  original, remaining, earned_at, expires_at, created_at, updated_at)
VALUES (@id, @customer_id, @customer_account_id, @source_transaction_id, @source_kind,
  @original, @original, @now, @expires_at, @now, @now);

-- SpendableLots are the customer's lots with credits to spend or holds on
-- them, oldest expiry first. The caller holds the customer's balance rows.
-- name: SpendableLots :many
SELECT id, remaining, held, expires_at FROM credit_lots
WHERE customer_id = @customer_id AND (remaining > 0 OR held > 0)
ORDER BY expires_at, id FOR UPDATE;

-- name: SpendableCredits :one
SELECT coalesce(sum(remaining), 0)::bigint FROM credit_lots
WHERE customer_id = @customer_id AND remaining > 0 AND expires_at > @now;

-- DueLots are the customer's lots past expiry that the expiry has not closed.
-- name: DueLots :many
SELECT id, remaining, expires_at FROM credit_lots
WHERE customer_id = @customer_id AND expired_at IS NULL AND expires_at <= @now
ORDER BY expires_at, id FOR UPDATE;

-- name: OpenLotIDs :many
SELECT id FROM credit_lots WHERE customer_id = @customer_id AND expired_at IS NULL
ORDER BY expires_at, id FOR UPDATE;

-- name: MarkLotsExpired :exec
UPDATE credit_lots SET expired_at = @now WHERE id = ANY(@ids::uuid[]) AND expired_at IS NULL;

-- name: DueCustomers :many
SELECT DISTINCT customer_id FROM credit_lots
WHERE expired_at IS NULL AND expires_at <= @now
ORDER BY customer_id LIMIT @lim;

-- name: InsertConsumption :exec
INSERT INTO credit_lot_consumptions (id, lot_id, transaction_id, hold_id, kind, remaining_delta, held_delta, created_at)
VALUES (@id, @lot_id, @transaction_id, @hold_id, @kind, @remaining_delta, @held_delta, @now);

-- name: RemindCustomers :many
SELECT DISTINCT customer_id FROM credit_lots
WHERE expired_at IS NULL AND reminded_at IS NULL AND remaining > 0 AND expires_at > @now AND expires_at <= @until
ORDER BY customer_id LIMIT @lim;

-- name: RemindLots :many
SELECT id, remaining, expires_at FROM credit_lots
WHERE customer_id = @customer_id AND expired_at IS NULL AND reminded_at IS NULL AND remaining > 0
  AND expires_at > @now AND expires_at <= @until
ORDER BY expires_at, id FOR UPDATE SKIP LOCKED;

-- name: MarkReminded :exec
UPDATE credit_lots SET reminded_at = @now WHERE id = ANY(@ids::uuid[]);

-- name: ExpiringLots :many
SELECT remaining, expires_at FROM credit_lots
WHERE customer_id = @customer_id AND remaining > 0 AND expires_at > @now AND expires_at <= @until
ORDER BY expires_at, id;

------------------------------------------------------------------------------
-- Holds
------------------------------------------------------------------------------

-- HoldByOrder and HoldByBillClaim do not lock: the caller locks the
-- customer's balance rows first, then the hold with LockHold (one lock order).
-- name: HoldByOrder :one
SELECT * FROM credit_holds WHERE order_id = @order_id;

-- name: HoldByBillClaim :one
SELECT * FROM credit_holds WHERE bill_claim_id = @bill_claim_id;

-- name: LockHold :one
SELECT * FROM credit_holds WHERE id = @id FOR UPDATE;

-- name: InsertHold :exec
INSERT INTO credit_holds (id, customer_id, store_id, order_id, bill_claim_id, amount, created_at, updated_at)
VALUES (@id, @customer_id, @store_id, @order_id, @bill_claim_id, @amount, @now, @now);

-- name: AddHoldReleased :exec
UPDATE credit_holds SET released = released + @n,
  status = CASE WHEN @final::bool THEN 'released' ELSE status END,
  settled_at = CASE WHEN @final::bool THEN @now::timestamptz ELSE settled_at END
WHERE id = @id;

-- name: CaptureHold :exec
UPDATE credit_holds SET captured = @captured, status = 'captured', settled_at = @now WHERE id = @id;

-- HoldPortions is what the hold still holds on each lot.
-- name: HoldPortions :many
SELECT c.lot_id, l.expires_at, sum(c.held_delta)::int AS n
FROM credit_lot_consumptions c JOIN credit_lots l ON l.id = c.lot_id
WHERE c.hold_id = @hold_id
GROUP BY c.lot_id, l.expires_at
HAVING sum(c.held_delta) > 0;

-- CapUsage counts the holds that count against the caps (D-30): held or
-- captured, taken since the start of the IST month (customer) or day (store).
-- name: CapUsage :one
SELECT
  (SELECT count(*) FROM credit_holds h
    WHERE h.customer_id = @customer_id AND h.status IN ('held', 'captured')
      AND h.created_at >= @month_start AND h.created_at <= @now)::int AS customer_month,
  (SELECT count(*) FROM credit_holds h
    WHERE h.store_id = @store_id AND h.status IN ('held', 'captured')
      AND h.created_at >= @day_start AND h.created_at <= @now)::int AS store_day;

-- name: StoreCashbackBP :one
SELECT cashback_bp FROM stores WHERE id = @id;

-- InsertRiskFlag raises one flag per subject and rule per IST day (D-126):
-- a second refusal the same day adds nothing.
-- name: InsertRiskFlag :exec
INSERT INTO risk_flags (id, subject_type, subject_id, rule, score, details, created_at, updated_at)
SELECT @id, @subject_type, @subject_id, @rule, @score, @details, @now, @now
WHERE NOT EXISTS (
  SELECT 1 FROM risk_flags f
  WHERE f.subject_type = @subject_type AND f.subject_id = @subject_id AND f.rule = @rule
    AND f.created_at >= @day_start);

------------------------------------------------------------------------------
-- Reads
------------------------------------------------------------------------------

-- WalletLines are the postings that moved the customer's wallet or held
-- balance, newest first, with what the line text needs.
-- name: WalletLines :many
WITH moved AS (
  SELECT e.transaction_id,
    coalesce(sum(e.amount) FILTER (WHERE a.kind = 'customer_wallet'), 0)::bigint AS wallet_delta,
    coalesce(sum(e.amount) FILTER (WHERE a.kind = 'customer_held'), 0)::bigint AS held_delta
  FROM ledger_entries e JOIN ledger_accounts a ON a.id = e.account_id
  WHERE a.owner_id = @customer_id AND a.kind IN ('customer_wallet', 'customer_held')
  GROUP BY e.transaction_id
)
SELECT t.id, t.kind, t.ref_type, t.ref_id, t.memo, t.created_at, m.wallet_delta, m.held_delta,
  o.display_number AS order_number, o.status AS order_status,
  coalesce(so.name, sh.name, '') AS shop_name
FROM moved m
JOIN ledger_transactions t ON t.id = m.transaction_id
LEFT JOIN orders o ON t.ref_type = 'order' AND o.id = t.ref_id
LEFT JOIN stores so ON so.id = o.store_id
LEFT JOIN credit_holds h ON (t.ref_type = 'bill_claim' AND h.bill_claim_id = t.ref_id)
  OR (t.ref_type = 'order' AND h.order_id = t.ref_id)
LEFT JOIN stores sh ON sh.id = h.store_id
WHERE (sqlc.narg(cursor_at)::timestamptz IS NULL
  OR (t.created_at, t.id) < (sqlc.narg(cursor_at)::timestamptz, @cursor_id::uuid))
ORDER BY t.created_at DESC, t.id DESC
LIMIT @lim;

-- name: CustomerMonthHolds :one
SELECT count(*)::int FROM credit_holds
WHERE customer_id = @customer_id AND status IN ('held', 'captured')
  AND created_at >= @month_start AND created_at <= @now;

-- name: BusinessStoreBalances :many
SELECT s.id, s.name, coalesce(b.balance, 0)::bigint AS balance
FROM stores s
LEFT JOIN ledger_accounts a ON a.kind = 'store_credit' AND a.owner_id = s.id
LEFT JOIN account_balances b ON b.account_id = a.id
WHERE s.business_id = @business_id
  AND (sqlc.narg(store_ids)::uuid[] IS NULL OR s.id = ANY(sqlc.narg(store_ids)::uuid[]))
ORDER BY s.created_at, s.id;

-- StoreLines are the postings on the business's store accounts, newest first.
-- store_ids, when set, limits both reads to a member's stores (D-125).
-- name: StoreLines :many
SELECT e.id, e.amount, a.owner_id AS store_id, t.kind, t.ref_type, t.ref_id, t.memo, e.created_at,
  o.display_number AS order_number
FROM ledger_entries e
JOIN ledger_accounts a ON a.id = e.account_id AND a.kind = 'store_credit'
JOIN stores s ON s.id = a.owner_id
JOIN ledger_transactions t ON t.id = e.transaction_id
LEFT JOIN orders o ON t.ref_type = 'order' AND o.id = t.ref_id
WHERE s.business_id = @business_id
  AND (sqlc.narg(store_ids)::uuid[] IS NULL OR s.id = ANY(sqlc.narg(store_ids)::uuid[]))
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
    OR (e.created_at, e.id) < (sqlc.narg(cursor_at)::timestamptz, @cursor_id::uuid))
ORDER BY e.created_at DESC, e.id DESC
LIMIT @lim;

------------------------------------------------------------------------------
-- Reconcile (03 §6 invariants; counts only, nothing is fixed)
------------------------------------------------------------------------------

-- name: Reconcile :one
SELECT
  -- 1: every transaction sums to 0.
  (SELECT count(*) FROM (SELECT transaction_id FROM ledger_entries
     GROUP BY transaction_id HAVING sum(amount) <> 0) x)::bigint AS unbalanced,
  -- 2: no negative wallet or held.
  (SELECT count(*) FROM account_balances
     WHERE kind IN ('customer_wallet', 'customer_held') AND balance < 0)::bigint AS negative,
  -- 3: Σ lots (remaining + held) = wallet + held, per customer.
  (SELECT count(*) FROM (
     SELECT a.owner_id, sum(b.balance) AS bal FROM ledger_accounts a
     JOIN account_balances b ON b.account_id = a.id
     WHERE a.kind IN ('customer_wallet', 'customer_held') GROUP BY a.owner_id) w
   FULL JOIN (SELECT customer_id, sum(remaining + held) AS n FROM credit_lots GROUP BY customer_id) l
     ON l.customer_id = w.owner_id
   WHERE coalesce(w.bal, 0) <> coalesce(l.n, 0))::bigint AS lots_vs_balances,
  -- 4: an open hold holds exactly amount − released on its lots, a settled
  -- one nothing; the held holds add up to customer_held.
  (SELECT count(*) FROM credit_holds h
   LEFT JOIN (SELECT hold_id, sum(held_delta) AS n FROM credit_lot_consumptions
     WHERE hold_id IS NOT NULL GROUP BY hold_id) c ON c.hold_id = h.id
   WHERE coalesce(c.n, 0) <> CASE WHEN h.status = 'held' THEN h.amount - h.released ELSE 0 END)::bigint AS holds,
  (SELECT count(*) FROM (
     SELECT a.owner_id, b.balance FROM ledger_accounts a JOIN account_balances b ON b.account_id = a.id
     WHERE a.kind = 'customer_held') w
   FULL JOIN (SELECT customer_id, sum(amount - released) AS n FROM credit_holds
     WHERE status = 'held' GROUP BY customer_id) h ON h.customer_id = w.owner_id
   WHERE coalesce(w.balance, 0) <> coalesce(h.n, 0))::bigint AS held_vs_holds,
  -- 7: the cache equals Σ entries.
  (SELECT count(*) FROM account_balances b
   LEFT JOIN (SELECT account_id, sum(amount) AS s FROM ledger_entries GROUP BY account_id) e
     ON e.account_id = b.account_id
   WHERE b.balance <> coalesce(e.s, 0))::bigint AS cache_drift,
  -- 8: a lot equals its original plus Σ consumptions.
  (SELECT count(*) FROM credit_lots l
   LEFT JOIN (SELECT lot_id, sum(remaining_delta) AS r, sum(held_delta) AS h
     FROM credit_lot_consumptions GROUP BY lot_id) c ON c.lot_id = l.id
   WHERE l.remaining <> l.original + coalesce(c.r, 0) OR l.held <> coalesce(c.h, 0))::bigint AS lot_drift;

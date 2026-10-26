-- Admin credits queries (W3-01, A-09; 01 §7; D-05, D-73): the ledger browser,
-- the expiry preview and the two-person adjustment requests. Generated into
-- creditsdb with W2-01's credits.sql.

------------------------------------------------------------------------------
-- Ledger browser
------------------------------------------------------------------------------

-- AccountsByOwner finds the accounts owned by a customer (user id) or a store
-- (store id), or the account with that id.
-- name: AccountsByOwner :many
SELECT a.id, a.kind, a.owner_type, a.owner_id, b.balance,
  coalesce(u.name, s.name, '')::text AS owner_name
FROM ledger_accounts a
JOIN account_balances b ON b.account_id = a.id
LEFT JOIN users u ON a.owner_type = 'customer' AND u.id = a.owner_id
LEFT JOIN stores s ON a.owner_type = 'store' AND s.id = a.owner_id
WHERE a.owner_id = @id OR a.id = @id
ORDER BY a.kind, a.id;

-- name: AccountsByStoreCode :many
SELECT a.id, a.kind, a.owner_type, a.owner_id, b.balance, s.name::text AS owner_name
FROM stores s
JOIN ledger_accounts a ON a.kind = 'store_credit' AND a.owner_id = s.id
JOIN account_balances b ON b.account_id = a.id
WHERE upper(s.short_code) = upper(@code::text)
ORDER BY a.id;

-- name: AccountsByPhoneHash :many
SELECT a.id, a.kind, a.owner_type, a.owner_id, b.balance, coalesce(u.name, '')::text AS owner_name
FROM phone_identities p
JOIN users u ON u.id = p.user_id
JOIN ledger_accounts a ON a.owner_id = u.id AND a.kind IN ('customer_wallet', 'customer_held')
JOIN account_balances b ON b.account_id = a.id
WHERE p.phone_hash = @phone_hash
ORDER BY a.kind, a.id;

-- CustomerWithoutWalletByID and CustomerWithoutWalletByPhoneHash find a
-- customer (a user not deleted with a customer profile, that is one who has
-- signed in to the customer app) who has no wallet account yet: accounts open
-- at the first posting, so the A-09 search lists such a customer with no
-- account id, and an adjustment opens the wallet (D-240).
-- name: CustomerWithoutWalletByID :many
SELECT u.id, coalesce(u.name, '')::text AS owner_name
FROM users u
JOIN customer_profiles cp ON cp.user_id = u.id
WHERE u.id = @id AND u.status <> 'deleted'
  AND NOT EXISTS (SELECT 1 FROM ledger_accounts a WHERE a.kind = 'customer_wallet' AND a.owner_id = u.id);

-- name: CustomerWithoutWalletByPhoneHash :many
SELECT u.id, coalesce(u.name, '')::text AS owner_name
FROM phone_identities p
JOIN users u ON u.id = p.user_id
JOIN customer_profiles cp ON cp.user_id = u.id
WHERE p.phone_hash = @phone_hash AND u.status <> 'deleted'
  AND NOT EXISTS (SELECT 1 FROM ledger_accounts a WHERE a.kind = 'customer_wallet' AND a.owner_id = u.id)
ORDER BY u.id;

-- name: PlatformAccounts :many
SELECT a.id, a.kind, a.owner_type, a.owner_id, b.balance, ''::text AS owner_name
FROM ledger_accounts a
JOIN account_balances b ON b.account_id = a.id
WHERE a.owner_type = 'platform'
ORDER BY a.kind;

-- name: AccountByID :one
SELECT a.id, a.kind, a.owner_type, a.owner_id, b.balance,
  coalesce(u.name, s.name, '')::text AS owner_name
FROM ledger_accounts a
JOIN account_balances b ON b.account_id = a.id
LEFT JOIN users u ON a.owner_type = 'customer' AND u.id = a.owner_id
LEFT JOIN stores s ON a.owner_type = 'store' AND s.id = a.owner_id
WHERE a.id = @id;

-- AccountEntries lists one account's entries, newest first.
-- name: AccountEntries :many
SELECT e.id, e.amount, e.created_at, t.id AS transaction_id, t.kind, t.ref_type, t.ref_id, t.memo, t.created_by
FROM ledger_entries e
JOIN ledger_transactions t ON t.id = e.transaction_id
WHERE e.account_id = @account_id
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
    OR (e.created_at, e.id) < (sqlc.narg(cursor_at)::timestamptz, @cursor_id::uuid))
ORDER BY e.created_at DESC, e.id DESC
LIMIT @lim;

-- ExpiryPreview sums the credits that will expire in [from_at, to_at), per
-- IST day: what is left on each lot (held portions do not expire, D-27).
-- name: ExpiryPreview :many
SELECT (l.expires_at AT TIME ZONE 'Asia/Kolkata')::date AS day,
  coalesce(sum(l.remaining), 0)::bigint AS credits,
  count(*)::int AS lots,
  count(DISTINCT l.customer_id)::int AS customers
FROM credit_lots l
WHERE l.expired_at IS NULL AND l.remaining > 0
  AND l.expires_at >= @from_at AND l.expires_at < @to_at
GROUP BY day
ORDER BY day;

------------------------------------------------------------------------------
-- Adjustment requests (D-05)
------------------------------------------------------------------------------

-- InsertAdjustmentRequest writes nothing (0 rows) when the requester's
-- Idempotency-Key already made a request; AdjustmentRequestByKey reads it.
-- name: InsertAdjustmentRequest :execrows
INSERT INTO admin_adjustment_requests (id, customer_id, amount, reason, link, requested_by, requested_at, status, idempotency_key)
VALUES (@id, @customer_id, @amount, @reason, sqlc.narg(link), @requested_by, @now, 'pending', sqlc.narg(idempotency_key))
ON CONFLICT (requested_by, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING;

-- name: AdjustmentRequestByKey :one
SELECT * FROM admin_adjustment_requests WHERE requested_by = @requested_by AND idempotency_key = @idempotency_key;

-- AdjustmentRefByKey is the request id (ref) of the at-once adjustment this
-- key posted, so an identical replay posts with the same ref and gets the
-- first transaction back.
-- name: AdjustmentRefByKey :one
SELECT ref_id FROM ledger_transactions WHERE idempotency_key = @idempotency_key AND kind = 'admin_adjustment';

-- name: LockAdjustmentRequest :one
SELECT * FROM admin_adjustment_requests WHERE id = @id FOR UPDATE;

-- AdjustmentRequest is one request with the customer's, requester's and
-- approver's names.
-- name: AdjustmentRequest :one
SELECT r.*, coalesce(u.name, '')::text AS customer_name,
  coalesce(rq.name, '')::text AS requested_by_name, ap.name AS approved_by_name
FROM admin_adjustment_requests r
LEFT JOIN users u ON u.id = r.customer_id
LEFT JOIN users rq ON rq.id = r.requested_by
LEFT JOIN users ap ON ap.id = r.approved_by
WHERE r.id = @id;

-- name: ApproveAdjustmentRequest :exec
UPDATE admin_adjustment_requests SET status = 'approved', approved_by = @approved_by, decided_at = @now,
  decision_note = sqlc.narg(decision_note), transaction_id = @transaction_id, version = version + 1
WHERE id = @id AND status = 'pending';

-- DecideAdjustmentRequest closes a request without posting: rejected by a
-- second person, cancelled by its requester.
-- name: DecideAdjustmentRequest :exec
UPDATE admin_adjustment_requests SET status = @status, approved_by = sqlc.narg(approved_by), decided_at = @now,
  decision_note = @decision_note, version = version + 1
WHERE id = @id AND status = 'pending';

-- name: AdjustmentRequests :many
SELECT r.*, coalesce(u.name, '')::text AS customer_name,
  coalesce(rq.name, '')::text AS requested_by_name, ap.name AS approved_by_name
FROM admin_adjustment_requests r
LEFT JOIN users u ON u.id = r.customer_id
LEFT JOIN users rq ON rq.id = r.requested_by
LEFT JOIN users ap ON ap.id = r.approved_by
WHERE (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status)::text)
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
    OR (r.requested_at, r.id) < (sqlc.narg(cursor_at)::timestamptz, @cursor_id::uuid))
ORDER BY r.requested_at DESC, r.id DESC
LIMIT @lim;

-- name: CustomerExists :one
SELECT EXISTS (SELECT 1 FROM users WHERE id = @id AND status <> 'deleted')::boolean;

-- IsCustomer is CustomerExists for a user with no wallet yet (D-240): only a
-- user with a customer profile may have one opened by an adjustment, so an
-- admin or a shop owner who never used the customer app gets none.
-- name: IsCustomer :one
SELECT EXISTS (
  SELECT 1 FROM users u JOIN customer_profiles cp ON cp.user_id = u.id
  WHERE u.id = @id AND u.status <> 'deleted'
)::boolean;

------------------------------------------------------------------------------
-- The day's sum for the two-person rule (D-237)
------------------------------------------------------------------------------

-- LockAdminCustomerAdjust serialises one admin's adjustments to one customer
-- (pair_key: admin id, a colon, customer id) for the rest of the transaction, so two concurrent requests cannot both
-- read the day's sum before either is written.
-- name: LockAdminCustomerAdjust :exec
SELECT pg_advisory_xact_lock(hashtextextended('credits.adjust:' || sqlc.arg(pair_key)::text, 0));

-- AdminAdjustedSince is the credits (absolute values) one admin has adjusted
-- on one customer's wallet since a moment: their at-once postings plus their
-- requests still pending or approved. A request's own posting (made by its
-- approver) is counted once, as the request; rejected and cancelled requests
-- moved nothing and do not count.
-- name: AdminAdjustedSince :one
SELECT (
  coalesce((SELECT sum(abs(e.amount)) FROM ledger_entries e
    JOIN ledger_transactions t ON t.id = e.transaction_id
    WHERE e.account_id = @wallet_id AND e.created_at >= @since
      AND t.kind = 'admin_adjustment' AND t.created_by = @admin_id
      AND NOT EXISTS (SELECT 1 FROM admin_adjustment_requests r WHERE r.id = t.ref_id)), 0)
  + coalesce((SELECT sum(abs(r.amount)) FROM admin_adjustment_requests r
    WHERE r.requested_by = @admin_id AND r.customer_id = @customer_id AND r.requested_at >= @since
      AND r.status IN ('pending', 'approved')), 0)
)::bigint AS credits;

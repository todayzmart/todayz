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
  coalesce(u.name, s.name)::text AS owner_name
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

-- name: InsertAdjustmentRequest :exec
INSERT INTO admin_adjustment_requests (id, customer_id, amount, reason, link, requested_by, requested_at, status)
VALUES (@id, @customer_id, @amount, @reason, sqlc.narg(link), @requested_by, @now, 'pending');

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

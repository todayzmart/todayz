-- payouts queries (W3-01), generated into package payoutsdb (gen/sqlc/payoutsdb).
-- Only the work package that builds internal/payouts edits this folder (D-50).
--
-- The statement and payout amounts are read from the store_credit ledger
-- (W2-01's tables); nothing here writes the ledger. A payout's posting goes
-- through the credits Poster.

------------------------------------------------------------------------------
-- Statement close (01 §9)
------------------------------------------------------------------------------

-- BusinessesToClose lists the businesses with a store credit account and no
-- statement for the period yet, so a rerun of the close skips the ones done.
-- name: BusinessesToClose :many
SELECT DISTINCT s.business_id
FROM stores s
JOIN ledger_accounts a ON a.kind = 'store_credit' AND a.owner_id = s.id
WHERE NOT EXISTS (
  SELECT 1 FROM payout_statements ps WHERE ps.business_id = s.business_id AND ps.period = @period)
ORDER BY s.business_id;

-- StoreLedger sums a business's store credit entries per store and
-- transaction kind: before is everything before from_at (the opening), amount
-- and n the entries in [from_at, to_at).
-- name: StoreLedger :many
SELECT s.id AS store_id, t.kind,
  coalesce(sum(e.amount) FILTER (WHERE e.created_at < @from_at::timestamptz), 0)::bigint AS before,
  coalesce(sum(e.amount) FILTER (WHERE e.created_at >= @from_at::timestamptz), 0)::bigint AS amount,
  (count(*) FILTER (WHERE e.created_at >= @from_at::timestamptz))::int AS n
FROM stores s
JOIN ledger_accounts a ON a.kind = 'store_credit' AND a.owner_id = s.id
JOIN ledger_entries e ON e.account_id = a.id
JOIN ledger_transactions t ON t.id = e.transaction_id
WHERE s.business_id = @business_id AND e.created_at < @to_at::timestamptz
GROUP BY s.id, t.kind
ORDER BY s.id, t.kind;

-- StoreAccounts is every store credit account of the business with its
-- balance now, in store id order.
-- name: StoreAccounts :many
SELECT s.id AS store_id, b.balance
FROM stores s
JOIN ledger_accounts a ON a.kind = 'store_credit' AND a.owner_id = s.id
JOIN account_balances b ON b.account_id = a.id
WHERE s.business_id = @business_id
ORDER BY s.id;

-- HoldCauses reads the automatic hold causes (01 §9): a suspended
-- business, KYC not verified, no payout destination (neither a UPI id nor a
-- bank account), an open risk flag on the business or one of its stores. TODO(lead): every open flag holds, not only fraud rules (W3-02 owns
-- the rule list).
-- name: HoldCauses :one
SELECT
  (b.status = 'suspended')::boolean AS suspended,
  coalesce((SELECT k.status = 'verified' FROM business_kyc k WHERE k.business_id = b.id), false)::boolean AS kyc_verified,
  (NOT EXISTS (
    SELECT 1 FROM business_kyc k
    WHERE k.business_id = b.id AND (k.upi_id_enc IS NOT NULL OR k.bank_account_enc IS NOT NULL)
  ))::boolean AS details_missing,
  EXISTS (
    SELECT 1 FROM risk_flags f
    WHERE f.status = 'open' AND (
      (f.subject_type = 'business' AND f.subject_id = b.id) OR
      (f.subject_type = 'store' AND f.subject_id IN (SELECT st.id FROM stores st WHERE st.business_id = b.id)))
  )::boolean AS risk_flag
FROM businesses b
WHERE b.id = @business_id;

-- HasLaterStatement: a business closed for a later period keeps its payout
-- as that close computed it; a backfilled older close only writes its report.
-- name: HasLaterStatement :one
SELECT EXISTS (
  SELECT 1 FROM payout_statements WHERE business_id = @business_id AND period > @period)::boolean;

-- name: InsertStatement :exec
INSERT INTO payout_statements (id, business_id, period, opening, credits_in, cashback_out, referral_in,
  adjustments, reversals, payouts_out, closing, status, closed_at, payout_id, created_at)
VALUES (@id, @business_id, @period, @opening, @credits_in, @cashback_out, @referral_in,
  @adjustments, @reversals, @payouts_out, @closing, 'closed', @now, NULL, @now);

-- name: InsertStatementLine :exec
INSERT INTO payout_statement_lines (statement_id, store_id, kind, amount, count)
VALUES (@statement_id, @store_id, @kind, @amount, @count);

-- name: SetStatementPayout :exec
UPDATE payout_statements SET payout_id = @payout_id, computed_payout = @amount WHERE id = @id;

------------------------------------------------------------------------------
-- Payout rows
------------------------------------------------------------------------------

-- LockOpenPayout locks the business's open (pending or held) payout.
-- name: LockOpenPayout :one
SELECT * FROM payouts
WHERE business_id = @business_id AND status IN ('pending', 'held')
FOR UPDATE;

-- name: LockPayout :one
SELECT * FROM payouts WHERE id = @id FOR UPDATE;

-- name: InsertPayout :exec
INSERT INTO payouts (id, business_id, amount, status, hold_reason, computed_at, last_statement_id, created_at, updated_at)
VALUES (@id, @business_id, @amount, @status, sqlc.narg(hold_reason), @now, @last_statement_id, @now, @now);

-- RecomputePayout sets an open payout's new amount at a close. A changed
-- amount clears exported_at: a file exported before no longer matches.
-- name: RecomputePayout :exec
UPDATE payouts SET
  amount = @amount,
  status = @status,
  hold_reason = sqlc.narg(hold_reason),
  hold_note = sqlc.narg(hold_note),
  exported_at = CASE WHEN amount = @amount THEN exported_at ELSE NULL END,
  computed_at = @now,
  last_statement_id = @last_statement_id,
  updated_at = @now,
  version = version + 1
WHERE id = @id;

-- name: CancelPayout :exec
UPDATE payouts SET status = 'cancelled', hold_reason = NULL, hold_note = NULL,
  computed_at = @now, last_statement_id = @last_statement_id, updated_at = @now, version = version + 1
WHERE id = @id;

-- name: DeletePayoutLines :exec
DELETE FROM payout_lines WHERE payout_id = @payout_id;

-- name: InsertPayoutLine :exec
INSERT INTO payout_lines (payout_id, store_id, amount) VALUES (@payout_id, @store_id, @amount);

-- name: SetPayoutHold :exec
UPDATE payouts SET status = 'held', hold_reason = @hold_reason, hold_note = sqlc.narg(hold_note),
  updated_at = @now, version = version + 1
WHERE id = @id;

-- name: ClearPayoutHold :exec
UPDATE payouts SET status = 'pending', hold_reason = NULL, hold_note = NULL,
  updated_at = @now, version = version + 1
WHERE id = @id;

-- name: MarkPayoutPaid :exec
UPDATE payouts SET status = 'paid', utr = @utr, paid_at = @now, paid_by = @paid_by,
  transaction_id = @transaction_id, updated_at = @now, version = version + 1
WHERE id = @id;

-- name: PayoutByUTR :one
SELECT id FROM payouts WHERE utr = @utr;

------------------------------------------------------------------------------
-- Reads
------------------------------------------------------------------------------

-- name: PayoutLines :many
SELECT pl.store_id, pl.amount, s.name AS store_name
FROM payout_lines pl JOIN stores s ON s.id = pl.store_id
WHERE pl.payout_id = @payout_id
ORDER BY pl.store_id;

-- LockPayoutBalances locks the balances a payout's posting touches (its
-- stores' store_credit and platform_payouts_cash) in account id order, the
-- credits Poster's one lock order, and reads them. Mark paid checks each
-- store still holds its line: a store's balance can fall after the close
-- (cashback), and a payout never takes a store below zero.
-- name: LockPayoutBalances :many
SELECT a.id AS account_id, a.kind, a.owner_id AS store_id, b.balance
FROM ledger_accounts a JOIN account_balances b ON b.account_id = a.id
WHERE (a.kind = 'store_credit' AND a.owner_id = ANY(@store_ids::uuid[])) OR a.kind = 'platform_payouts_cash'
ORDER BY a.id
FOR UPDATE OF b;

-- PayoutRows lists payouts newest first, with their business and statement
-- period; period and status filter when given.
-- name: PayoutRows :many
SELECT p.*, b.brand_name, ps.period AS statement_period
FROM payouts p
JOIN businesses b ON b.id = p.business_id
LEFT JOIN payout_statements ps ON ps.id = p.last_statement_id
WHERE (sqlc.narg(period)::date IS NULL OR ps.period = sqlc.narg(period)::date)
  AND (sqlc.narg(status)::text IS NULL OR p.status = sqlc.narg(status)::text)
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
    OR (p.created_at, p.id) < (sqlc.narg(cursor_at)::timestamptz, @cursor_id::uuid))
ORDER BY p.created_at DESC, p.id DESC
LIMIT @lim;

-- PayoutDetail is one payout with its business and the payee's masked
-- details (last 4 only, D-44, D-85).
-- name: PayoutDetail :one
SELECT p.*, b.brand_name, b.status AS business_status, ps.period AS statement_period,
  k.status AS kyc_status, k.upi_id_last4, k.bank_account_last4, k.bank_ifsc
FROM payouts p
JOIN businesses b ON b.id = p.business_id
LEFT JOIN payout_statements ps ON ps.id = p.last_statement_id
LEFT JOIN business_kyc k ON k.business_id = p.business_id
WHERE p.id = @id;

-- ExportRows are the pending payouts of a statement period with the sealed
-- payee details, in business order (D-85, D-93). Only pending rows.
-- name: ExportRows :many
SELECT p.id, p.business_id, p.amount, ps.period AS statement_period,
  coalesce(b.legal_name, b.brand_name)::text AS legal_name,
  k.legal_name AS beneficiary_name, k.upi_id_enc, k.bank_account_enc, k.bank_ifsc
FROM payouts p
JOIN businesses b ON b.id = p.business_id
JOIN payout_statements ps ON ps.id = p.last_statement_id
LEFT JOIN business_kyc k ON k.business_id = p.business_id
WHERE p.status = 'pending' AND ps.period = @period
ORDER BY p.business_id
FOR UPDATE OF p;

-- name: StampExported :exec
UPDATE payouts SET exported_at = @now, updated_at = @now WHERE id = ANY(@ids::uuid[]);

-- AdminStatements lists one period's statements with the business, its KYC
-- and risk state now and its payout, in business name order.
-- name: AdminStatements :many
SELECT ps.*, b.brand_name, b.status AS business_status,
  coalesce(k.status, 'missing')::text AS kyc_status,
  EXISTS (
    SELECT 1 FROM risk_flags f
    WHERE f.status = 'open' AND (
      (f.subject_type = 'business' AND f.subject_id = b.id) OR
      (f.subject_type = 'store' AND f.subject_id IN (SELECT st.id FROM stores st WHERE st.business_id = b.id)))
  )::boolean AS open_risk_flag,
  p.status AS payout_status, p.amount AS payout_amount, p.utr AS payout_utr, p.hold_reason AS payout_hold_reason,
  (p.last_statement_id IS NOT DISTINCT FROM ps.id)::boolean AS payout_current
FROM payout_statements ps
JOIN businesses b ON b.id = ps.business_id
LEFT JOIN business_kyc k ON k.business_id = ps.business_id
LEFT JOIN payouts p ON p.id = ps.payout_id
WHERE ps.period = @period
  AND (sqlc.narg(cursor_name)::text IS NULL
    OR (b.brand_name, ps.id) > (sqlc.narg(cursor_name)::text, @cursor_id::uuid))
ORDER BY b.brand_name, ps.id
LIMIT @lim;

-- AdminStatement is one statement, as AdminStatements.
-- name: AdminStatement :one
SELECT ps.*, b.brand_name, b.status AS business_status,
  coalesce(k.status, 'missing')::text AS kyc_status,
  EXISTS (
    SELECT 1 FROM risk_flags f
    WHERE f.status = 'open' AND (
      (f.subject_type = 'business' AND f.subject_id = b.id) OR
      (f.subject_type = 'store' AND f.subject_id IN (SELECT st.id FROM stores st WHERE st.business_id = b.id)))
  )::boolean AS open_risk_flag,
  p.status AS payout_status, p.amount AS payout_amount, p.utr AS payout_utr, p.hold_reason AS payout_hold_reason,
  (p.last_statement_id IS NOT DISTINCT FROM ps.id)::boolean AS payout_current
FROM payout_statements ps
JOIN businesses b ON b.id = ps.business_id
LEFT JOIN business_kyc k ON k.business_id = ps.business_id
LEFT JOIN payouts p ON p.id = ps.payout_id
WHERE ps.id = @id;

-- BusinessStatements lists a business's statements, newest period first,
-- with the payout each one computed. payout_current is false once a later
-- close recomputed (or cancelled) that payout: the amount was carried on.
-- name: BusinessStatements :many
SELECT ps.*, p.status AS payout_status, p.amount AS payout_amount, p.utr AS payout_utr, p.paid_at AS payout_paid_at,
  p.hold_reason AS payout_hold_reason,
  (p.last_statement_id IS NOT DISTINCT FROM ps.id)::boolean AS payout_current
FROM payout_statements ps
LEFT JOIN payouts p ON p.id = ps.payout_id
WHERE ps.business_id = @business_id
  AND (sqlc.narg(before)::date IS NULL OR ps.period < sqlc.narg(before)::date)
ORDER BY ps.period DESC
LIMIT @lim;

-- name: BusinessStatement :one
SELECT ps.*, p.status AS payout_status, p.amount AS payout_amount, p.utr AS payout_utr, p.paid_at AS payout_paid_at,
  p.hold_reason AS payout_hold_reason,
  (p.last_statement_id IS NOT DISTINCT FROM ps.id)::boolean AS payout_current
FROM payout_statements ps
LEFT JOIN payouts p ON p.id = ps.payout_id
WHERE ps.business_id = @business_id AND ps.period = @period;

-- name: StatementLines :many
SELECT l.store_id, s.name AS store_name, l.kind, l.amount, l.count
FROM payout_statement_lines l JOIN stores s ON s.id = l.store_id
WHERE l.statement_id = @statement_id
ORDER BY s.name, l.store_id, l.kind;

-- name: BusinessName :one
SELECT brand_name FROM businesses WHERE id = @id;

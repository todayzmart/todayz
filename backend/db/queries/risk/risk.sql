-- risk queries, generated into package riskdb (gen/sqlc/riskdb). W3-02:
-- the rules the risk_scan job runs, the A-13 flags queue and the A-10
-- referrals list. Only W3-02 edits this folder (D-50).

-- --- scan: facts the rules read ---------------------------------------------------

-- name: LockoutCounts :many
-- Code lockouts per store in [@since, @until) (01 §6; the trigger on orders
-- records each lock in order_code_lockouts).
SELECT store_id, count(*)::int AS n
FROM order_code_lockouts
WHERE locked_at >= @since AND locked_at < @until
GROUP BY store_id
ORDER BY store_id;

-- name: BillClaimsSince :many
-- Claimed bills since @since, by customer then time (pos.bill_claim_spread).
SELECT id, claimed_by::uuid AS claimed_by, store_id, claimed_at::timestamptz AS claimed_at
FROM bill_claims
WHERE claimed_at >= @since AND claimed_by IS NOT NULL
ORDER BY claimed_by, claimed_at, id;

-- name: ReversalCounts :many
-- Linked POS sales (bill QR or counter code) per store received since
-- @since, and how many of them were reversed (01 §7).
SELECT store_id, count(*)::int AS sales, (count(*) FILTER (WHERE status = 'reversed'))::int AS reversed
FROM pos_sales
WHERE received_at >= @since AND link_status = 'linked' AND link_method IN ('bill_qr', 'counter_code')
GROUP BY store_id
ORDER BY store_id;

-- name: SelfLikeCandidates :many
-- Referrals that qualified since @since and were never flagged
-- referral.self_like, with the self-like signals (D-225):
--   * own_shop: the qualifying order or sale was at a store of a business on
--     the referrer's side (the referring store's business; for a customer
--     referrer, a business they work at);
--   * referee_staff: the referee works at that business;
--   * shared_device: the referee signed in on a device a person on the
--     referrer's side (the referrer, or that business's staff) also used;
--   * staff_phone: the referee's phone was invited as staff there.
-- For any_shop the referee is the new business: referee_staff means the
-- referrer (or a person of the referring business) works at it.
WITH c AS (
  SELECT r.id, r.kind, r.referrer_type, r.referrer_id, r.referee_type, r.referee_id, r.referee_phone_hash,
    r.qualifying_ref_type, r.qualifying_ref_id,
    coalesce(o.store_id, ps.store_id) AS qual_store_id
  FROM referrals r
  LEFT JOIN orders o ON r.qualifying_ref_type = 'order' AND o.id = r.qualifying_ref_id
  LEFT JOIN pos_sales ps ON r.qualifying_ref_type = 'pos_sale' AND ps.id = r.qualifying_ref_id
  WHERE r.qualified_at >= @since AND r.status IN ('qualified', 'rewarded')
    AND NOT EXISTS (SELECT 1 FROM risk_flags f
      WHERE f.subject_type = 'referral' AND f.subject_id = r.id AND f.rule = 'referral.self_like')
), side AS (
  -- The businesses on the referrer's side.
  SELECT c.id AS referral_id, st.business_id
  FROM c JOIN stores st ON c.referrer_type = 'store' AND st.id = c.referrer_id
  UNION
  SELECT c.id, s.business_id
  FROM c JOIN staff s ON c.referrer_type = 'customer' AND s.user_id = c.referrer_id AND s.status <> 'removed'
  UNION
  SELECT c.id, c.referrer_id
  FROM c WHERE c.referrer_type = 'business'
), own AS (
  -- The referrer-side business the qualifying purchase was at (customer referees).
  SELECT DISTINCT ON (c.id) c.id AS referral_id, side.business_id
  FROM c
  JOIN stores qs ON qs.id = c.qual_store_id
  JOIN side ON side.referral_id = c.id AND side.business_id = qs.business_id
  WHERE c.referee_type = 'customer'
  ORDER BY c.id, side.business_id
), people AS (
  -- The people on the referrer's side: the referrer and the side's staff.
  SELECT c.id AS referral_id, c.referrer_id AS user_id FROM c WHERE c.referrer_type = 'customer'
  UNION
  SELECT side.referral_id, s.user_id
  FROM side JOIN staff s ON s.business_id = side.business_id AND s.user_id IS NOT NULL AND s.status <> 'removed'
)
SELECT c.id, c.kind, c.qualifying_ref_type, c.qualifying_ref_id,
  own.business_id AS own_business_id,
  (own.business_id IS NOT NULL)::boolean AS own_shop,
  (CASE
    WHEN c.referee_type = 'customer' THEN own.business_id IS NOT NULL AND EXISTS (
      SELECT 1 FROM staff s WHERE s.business_id = own.business_id AND s.user_id = c.referee_id AND s.status <> 'removed')
    ELSE EXISTS (
      SELECT 1 FROM people p JOIN staff s ON s.user_id = p.user_id AND s.status <> 'removed'
      WHERE p.referral_id = c.id AND s.business_id = c.referee_id)
  END)::boolean AS referee_staff,
  (c.referee_type = 'customer' AND EXISTS (
    SELECT 1 FROM devices d1
    JOIN devices d2 ON d2.device_key = d1.device_key AND d2.user_id <> d1.user_id
    JOIN people p ON p.referral_id = c.id AND p.user_id = d2.user_id
    WHERE d1.user_id = c.referee_id))::boolean AS shared_device,
  (own.business_id IS NOT NULL AND c.referee_phone_hash IS NOT NULL AND EXISTS (
    SELECT 1 FROM staff s WHERE s.business_id = own.business_id AND s.invite_phone_hash = c.referee_phone_hash))::boolean AS staff_phone
FROM c
LEFT JOIN own ON own.referral_id = c.id
ORDER BY c.id;

-- name: OtpIPFanout :many
-- IPs that asked OTPs for @lim or more different phones since @since (01 §2).
SELECT ip_hash::bytea AS ip_hash, count(DISTINCT phone_hash)::int AS phones, count(*)::int AS requests
FROM otp_requests
WHERE created_at >= @since AND ip_hash IS NOT NULL
GROUP BY ip_hash
HAVING count(DISTINCT phone_hash) >= @lim::int
ORDER BY ip_hash;

-- name: OtpFailedPhones :many
-- Phones with @lim or more OTPs since @since that used every attempt and were
-- never verified (01 §2), with the account the phone belongs to, if any.
SELECT o.phone_hash, count(*)::int AS failed,
  (SELECT pi.user_id FROM phone_identities pi WHERE pi.phone_hash = o.phone_hash ORDER BY pi.created_at LIMIT 1)::uuid AS user_id
FROM otp_requests o
WHERE o.created_at >= @since AND o.consumed_at IS NULL AND o.attempts >= @max_attempts::int
GROUP BY o.phone_hash
HAVING count(*) >= @lim::int
ORDER BY o.phone_hash;

-- name: InsertFlagOnce :execrows
-- Raises a flag unless the subject already has one for the rule created at or
-- after @since (and, with @day, recorded for that IST day), whatever its
-- status: a cleared flag is not raised again from the same facts.
INSERT INTO risk_flags (id, subject_type, subject_id, rule, score, details, created_at, updated_at)
SELECT @id, @subject_type::text, @subject_id, @rule::text, @score, @details, @now, @now
WHERE NOT EXISTS (
  SELECT 1 FROM risk_flags f
  WHERE f.subject_type = @subject_type::text AND f.subject_id = @subject_id AND f.rule = @rule::text
    AND f.created_at >= @since
    AND (sqlc.narg(day)::text IS NULL OR f.details->>'day' = sqlc.narg(day)::text)
);

-- --- A-13: the flags queue --------------------------------------------------------

-- name: ListFlags :many
SELECT f.id, f.rule, f.subject_type, f.subject_id, f.score, f.status, f.created_at, f.reviewed_at, f.review_note,
  rv.name AS reviewer_name
FROM risk_flags f
LEFT JOIN users rv ON rv.id = f.reviewed_by
WHERE (sqlc.narg(status)::text IS NULL OR f.status = sqlc.narg(status)::text)
  AND (sqlc.narg(rule)::text IS NULL OR f.rule = sqlc.narg(rule)::text)
  AND (sqlc.narg(subject_type)::text IS NULL OR f.subject_type = sqlc.narg(subject_type)::text)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
    OR (f.created_at, f.id) < (sqlc.narg(before_at)::timestamptz, @before_id::uuid))
ORDER BY f.created_at DESC, f.id DESC
LIMIT @lim;

-- name: OpenByRule :many
SELECT rule, count(*)::int AS open
FROM risk_flags
WHERE status = 'open'
GROUP BY rule
ORDER BY count(*) DESC, rule
LIMIT 100;

-- name: GetFlag :one
SELECT f.id, f.rule, f.subject_type, f.subject_id, f.score, f.status, f.details, f.created_at, f.reviewed_at, f.review_note,
  rv.name AS reviewer_name
FROM risk_flags f
LEFT JOIN users rv ON rv.id = f.reviewed_by
WHERE f.id = @id;

-- name: SubjectFlags :many
-- The subject's other flags, newest first.
SELECT f.id, f.rule, f.subject_type, f.subject_id, f.score, f.status, f.created_at, f.reviewed_at, f.review_note,
  rv.name AS reviewer_name
FROM risk_flags f
LEFT JOIN users rv ON rv.id = f.reviewed_by
WHERE f.subject_type = @subject_type AND f.subject_id = @subject_id AND f.id <> @id
ORDER BY f.created_at DESC, f.id DESC
LIMIT 20;

-- name: LockFlag :one
SELECT id, status FROM risk_flags WHERE id = @id FOR UPDATE;

-- name: ResolveFlag :exec
-- Clear or confirm an open flag (the caller holds its row lock).
UPDATE risk_flags
SET status = @status, reviewed_by = @reviewed_by, reviewed_at = @now, review_note = @note,
  updated_at = @now, version = version + 1
WHERE id = @id AND status = 'open';

-- --- A-13: subject labels ------------------------------------------------------------

-- name: StoreLabels :many
SELECT st.id, st.name, st.status, st.business_id FROM stores st WHERE st.id = ANY(@ids::uuid[]);

-- name: BusinessLabels :many
SELECT b.id, b.brand_name, b.status FROM businesses b WHERE b.id = ANY(@ids::uuid[]);

-- name: UserLabels :many
-- A person's name and status, and whether they have a customer profile (the
-- A-06 block applies to customers).
SELECT u.id, u.name, u.status,
  EXISTS (SELECT 1 FROM customer_profiles cp WHERE cp.user_id = u.id)::boolean AS is_customer
FROM users u WHERE u.id = ANY(@ids::uuid[]);

-- name: ReferralLabels :many
-- A referral's two sides, for the "referrer → new person" label.
SELECT r.id, r.status, r.kind, r.referrer_type, r.referrer_id, r.referee_type, r.referee_id,
  coalesce(CASE r.referrer_type
    WHEN 'store' THEN (SELECT st.name FROM stores st WHERE st.id = r.referrer_id)
    WHEN 'business' THEN (SELECT b.brand_name FROM businesses b WHERE b.id = r.referrer_id)
    ELSE (SELECT u.name FROM users u WHERE u.id = r.referrer_id) END, '')::text AS referrer_label,
  coalesce(CASE r.referee_type
    WHEN 'business' THEN (SELECT b.brand_name FROM businesses b WHERE b.id = r.referee_id)
    ELSE (SELECT u.name FROM users u WHERE u.id = r.referee_id) END, '')::text AS referee_label
FROM referrals r WHERE r.id = ANY(@ids::uuid[]);

-- --- A-13: evidence -----------------------------------------------------------------

-- name: LockoutEvidence :many
SELECT l.order_id, l.locked_at, o.display_number, o.status, o.total_paise, st.name AS store_name
FROM order_code_lockouts l
JOIN orders o ON o.id = l.order_id
JOIN stores st ON st.id = l.store_id
WHERE l.store_id = @store_id AND l.locked_at >= @since AND l.locked_at < @until
ORDER BY l.locked_at DESC, l.order_id
LIMIT 50;

-- name: BillClaimEvidence :many
SELECT bc.id, bc.claimed_at::timestamptz AS claimed_at, bc.amount_paise, bc.status, st.name AS store_name
FROM bill_claims bc
JOIN stores st ON st.id = bc.store_id
WHERE bc.id = ANY(@ids::uuid[]) AND bc.claimed_at IS NOT NULL
ORDER BY bc.claimed_at DESC, bc.id
LIMIT 50;

-- name: ReversedSaleEvidence :many
SELECT ps.id, ps.reversed_at::timestamptz AS reversed_at, ps.total_paise, ps.link_method::text AS link_method, st.name AS store_name
FROM pos_sales ps
JOIN stores st ON st.id = ps.store_id
WHERE ps.store_id = @store_id AND ps.received_at >= @since AND ps.status = 'reversed'
  AND ps.link_status = 'linked' AND ps.link_method IN ('bill_qr', 'counter_code')
ORDER BY ps.reversed_at DESC, ps.id
LIMIT 50;

-- name: OfflineLinkEvidence :many
SELECT ps.id, ps.received_at, ps.total_paise, coalesce(ps.link_status, 'none')::text AS link_status, st.name AS store_name
FROM pos_sales ps
JOIN stores st ON st.id = ps.store_id
WHERE ps.store_id = @store_id AND ps.received_at >= @since AND ps.received_at < @until
  AND ((ps.offline_link_at IS NOT NULL AND ps.link_status = 'linked') OR ps.offline_offer_unmatched)
ORDER BY ps.received_at DESC, ps.id
LIMIT 50;

-- name: StrikeEvidence :many
SELECT k.id, k.created_at, k.reason, k.reset_at, o.display_number, st.name AS store_name
FROM strikes k
LEFT JOIN orders o ON o.id = k.order_id
JOIN stores st ON st.id = k.subject_id
WHERE k.subject_type = 'store' AND k.subject_id = @store_id AND k.created_at >= @since
ORDER BY k.created_at DESC, k.id
LIMIT 50;

-- name: OrderEvidence :many
SELECT o.id, o.placed_at, o.display_number, o.status, o.total_paise, st.name AS store_name
FROM orders o JOIN stores st ON st.id = o.store_id
WHERE o.id = ANY(@ids::uuid[])
ORDER BY o.placed_at DESC, o.id
LIMIT 50;

-- name: SaleEvidence :many
SELECT ps.id, ps.sold_at, ps.status, ps.total_paise, st.name AS store_name
FROM pos_sales ps JOIN stores st ON st.id = ps.store_id
WHERE ps.id = ANY(@ids::uuid[])
ORDER BY ps.sold_at DESC, ps.id
LIMIT 50;

-- name: DeviceReferralEvidence :many
-- The referrals made on the same device as @id (referral.device_accounts).
SELECT r.id, r.created_at, r.status, r.referrer_credits + r.referee_credits AS credits
FROM referrals r
WHERE r.device_hash = (SELECT x.device_hash FROM referrals x WHERE x.id = @id) AND r.device_hash IS NOT NULL
ORDER BY r.created_at DESC, r.id
LIMIT 50;

-- name: ReferralEvidence :one
SELECT r.id, r.created_at, r.status, r.reject_reason, r.qualifying_ref_type, r.qualifying_ref_id
FROM referrals r WHERE r.id = @id;

-- name: OtpEvidence :many
-- The OTP requests behind an ip or phone flag, found by the first 16 bytes of
-- the keyed hash (the flag's subject id). Kept 24 h only.
SELECT o.id, o.created_at, o.attempts, (o.consumed_at IS NOT NULL)::boolean AS used
FROM otp_requests o
WHERE o.created_at >= @since
  AND ((@by_ip::boolean AND o.ip_hash IS NOT NULL AND substring(o.ip_hash FROM 1 FOR 16) = @prefix::bytea)
    OR (NOT @by_ip::boolean AND substring(o.phone_hash FROM 1 FOR 16) = @prefix::bytea))
ORDER BY o.created_at DESC, o.id
LIMIT 50;

-- name: UserPhonePrefixes :many
-- The 16-byte hash prefixes of a user's verified phones (an otp_failed_phone
-- flag on a user lists its OTP requests through them).
SELECT substring(pi.phone_hash FROM 1 FOR 16)::bytea AS prefix FROM phone_identities pi WHERE pi.user_id = @user_id;

-- --- A-10: referrals -----------------------------------------------------------------

-- name: ListReferrals :many
-- A-10, newest first; @only_id reads one referral (after a reject).
SELECT r.id, r.kind, r.status, r.source, r.code, r.referrer_type, r.referrer_id, r.referee_type, r.referee_id,
  r.referrer_credits, r.referee_credits, r.reject_reason, r.qualified_at, r.created_at,
  r.referrer_rewarded_at, r.referee_rewarded_at,
  coalesce(CASE r.referrer_type
    WHEN 'store' THEN (SELECT st.name FROM stores st WHERE st.id = r.referrer_id)
    WHEN 'business' THEN (SELECT b.brand_name FROM businesses b WHERE b.id = r.referrer_id)
    ELSE (SELECT u.name FROM users u WHERE u.id = r.referrer_id) END, '')::text AS referrer_label,
  coalesce(CASE r.referee_type
    WHEN 'business' THEN (SELECT b.brand_name FROM businesses b WHERE b.id = r.referee_id)
    ELSE (SELECT u.name FROM users u WHERE u.id = r.referee_id) END, '')::text AS referee_label,
  coalesce((SELECT array_agg(DISTINCT f.rule ORDER BY f.rule) FROM risk_flags f
    WHERE f.subject_type = 'referral' AND f.subject_id = r.id AND f.status = 'open'), '{}')::text[] AS flag_rules,
  (SELECT count(*) FROM risk_flags f
    WHERE f.subject_type = 'referral' AND f.subject_id = r.id AND f.status = 'open')::int AS open_flags
FROM referrals r
WHERE (sqlc.narg(only_id)::uuid IS NULL OR r.id = sqlc.narg(only_id)::uuid)
  AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status)::text)
  AND (sqlc.narg(kind)::text IS NULL OR r.kind = sqlc.narg(kind)::text)
  AND (NOT @flagged::boolean OR EXISTS (SELECT 1 FROM risk_flags f
    WHERE f.subject_type = 'referral' AND f.subject_id = r.id AND f.status = 'open'))
  AND (sqlc.narg(before_at)::timestamptz IS NULL
    OR (r.created_at, r.id) < (sqlc.narg(before_at)::timestamptz, @before_id::uuid))
ORDER BY r.created_at DESC, r.id DESC
LIMIT @lim;

-- name: LockReferral :one
SELECT id, status, referrer_transaction_id, referee_transaction_id, referrer_rewarded_at, referee_rewarded_at
FROM referrals WHERE id = @id FOR UPDATE;

-- name: RejectReferral :exec
-- An admin reject (A-10): from pending, or from qualified with no reward
-- posted (the caller checked under the row lock). The reward sweep reads
-- only qualified rows, so nothing posts after this.
UPDATE referrals
SET status = 'rejected', reject_reason = 'admin_rejected', qualified_at = NULL,
  qualifying_ref_type = NULL, qualifying_ref_id = NULL, next_attempt_at = NULL,
  updated_at = @now, version = version + 1
WHERE id = @id;

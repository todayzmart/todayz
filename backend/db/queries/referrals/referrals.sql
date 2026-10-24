-- referrals queries, generated into package referralsdb (gen/sqlc/referralsdb).
-- Only the work package that builds internal/referrals edits this folder (D-50).

-- --- codes -----------------------------------------------------------------------

-- name: CustomerByCode :one
SELECT cp.user_id
FROM customer_profiles cp
JOIN users u ON u.id = cp.user_id
WHERE cp.referral_code = @code AND u.status = 'active';

-- name: BusinessByCode :one
SELECT id FROM businesses
WHERE referral_code = @code AND status NOT IN ('rejected', 'suspended');

-- name: StoreByShortCode :one
SELECT id, business_id, slug FROM stores
WHERE short_code = @code AND status IN ('live', 'paused');

-- name: CustomerCode :one
SELECT referral_code FROM customer_profiles WHERE user_id = @user_id;

-- name: BusinessCode :one
SELECT referral_code FROM businesses WHERE id = @id;

-- --- clicks ----------------------------------------------------------------------

-- name: InsertClick :exec
INSERT INTO referral_clicks (id, code, referrer_type, referrer_id, standee_id, landing, ip_hash, ua_hash, created_at)
VALUES (@id, @code, @referrer_type, @referrer_id, sqlc.narg('standee_id'), @landing, sqlc.narg('ip_hash'), sqlc.narg('ua_hash'), @created_at);

-- name: GetClick :one
SELECT id, code, referrer_type, referrer_id, standee_id, created_at
FROM referral_clicks WHERE id = @id;

-- --- the referee -----------------------------------------------------------------

-- name: CustomerForClaim :one
-- The new customer, locked so two claims of one customer serialise.
SELECT u.created_at, u.status, cp.referred_by_referral_id
FROM users u
JOIN customer_profiles cp ON cp.user_id = u.id
WHERE u.id = @user_id
FOR UPDATE OF cp;

-- name: BusinessForClaim :one
SELECT created_at, status, referred_by_referral_id
FROM businesses WHERE id = @id
FOR UPDATE;

-- name: BusinessOwner :one
SELECT user_id FROM staff
WHERE business_id = @business_id AND role = 'owner' AND status = 'active'
ORDER BY created_at LIMIT 1;

-- name: CustomerHasCompletedOrder :one
SELECT EXISTS (
  SELECT 1 FROM orders WHERE customer_id = @customer_id AND status IN ('collected', 'delivered')
) AS done;

-- name: UserPhoneHash :one
SELECT phone_hash FROM phone_identities WHERE user_id = @user_id
ORDER BY created_at DESC LIMIT 1;

-- name: PhoneAlreadyReferred :one
-- A referral of this referee type already claimed by this phone (a deleted
-- account signing up again keeps its phone hash on the row). For a business
-- the phone is its owner's.
SELECT EXISTS (
  SELECT 1 FROM referrals WHERE referee_phone_hash = @phone_hash AND referee_type = @referee_type
) AS taken;

-- name: IsStaffOf :one
-- user is a member (invited or active) of the business now.
SELECT EXISTS (
  SELECT 1 FROM staff WHERE business_id = @business_id AND user_id = @user_id AND status <> 'removed'
) AS member;

-- name: SharesBusinessStaff :one
-- Some active member of one business is a member of the other.
SELECT EXISTS (
  SELECT 1 FROM staff a JOIN staff b ON a.user_id = b.user_id
  WHERE a.business_id = @a AND b.business_id = @b AND a.status <> 'removed' AND b.status <> 'removed'
) AS shared;

-- name: StaffUserIDs :many
-- The members (invited or active) of a business, for the same-device rule.
SELECT user_id FROM staff WHERE business_id = @business_id AND status <> 'removed' AND user_id IS NOT NULL;

-- name: SessionDeviceKey :one
-- The device of the caller's session family (the latest session of it).
SELECT d.device_key
FROM sessions s JOIN devices d ON d.id = s.device_id
WHERE s.family_id = @family_id AND s.user_id = @user_id
ORDER BY s.created_at DESC LIMIT 1;

-- --- the same-device rule (D-13) -------------------------------------------------

-- name: DeviceTiedToUsers :one
-- The device is (or was) one of these users' devices, at any time.
SELECT EXISTS (
  SELECT 1 FROM devices WHERE device_key = @device_key AND user_id = ANY(@user_ids::uuid[])
) AS tied;

-- name: DeviceOtherAccounts :one
-- Other accounts seen on the device since @since: devices rows, and earlier
-- referrals claimed from it (which outlive an account's deletion).
SELECT (
  EXISTS (SELECT 1 FROM devices d WHERE d.device_key = @device_key AND d.user_id <> @user_id AND d.last_seen_at >= @since)
  OR EXISTS (SELECT 1 FROM referrals r WHERE r.device_hash = @device_hash AND r.referee_id <> @referee_id AND r.created_at >= @since)
)::bool AS seen;

-- name: DeviceAccountCount :one
-- How many accounts the device has carried: its devices rows, plus the
-- referees of referrals claimed from it.
SELECT count(*)::int AS n FROM (
  SELECT d.user_id AS id FROM devices d WHERE d.device_key = @device_key
  UNION
  SELECT r.referee_id FROM referrals r WHERE r.device_hash = @device_hash
) a;

-- name: InsertRiskFlag :exec
INSERT INTO risk_flags (id, subject_type, subject_id, rule, score, details, created_at, updated_at)
VALUES (@id, @subject_type, @subject_id, @rule, @score, @details, @now, @now);

-- --- referrals -------------------------------------------------------------------

-- name: InsertReferral :one
INSERT INTO referrals (
  id, kind, referrer_type, referrer_id, referee_type, referee_id, code, source, click_id, standee_id,
  status, reject_reason, device_hash, referee_phone_hash, referrer_credits, referee_credits, created_at, updated_at)
VALUES (
  @id, @kind, @referrer_type, @referrer_id, @referee_type, @referee_id, @code, @source, sqlc.narg('click_id'),
  sqlc.narg('standee_id'), @status, sqlc.narg('reject_reason'), sqlc.narg('device_hash'), sqlc.narg('referee_phone_hash'),
  @referrer_credits, @referee_credits, @now, @now)
ON CONFLICT (referee_type, referee_id) DO NOTHING
RETURNING *;

-- name: SetCustomerReferredBy :exec
UPDATE customer_profiles SET referred_by_referral_id = @referral_id, updated_at = @now, version = version + 1
WHERE user_id = @user_id AND referred_by_referral_id IS NULL;

-- name: SetBusinessReferredBy :exec
UPDATE businesses SET referred_by_referral_id = @referral_id, updated_at = @now, version = version + 1
WHERE id = @id AND referred_by_referral_id IS NULL;

-- name: RefereeReferral :one
SELECT * FROM referrals WHERE referee_type = @referee_type AND referee_id = @referee_id;

-- name: LockRefereeReferral :one
SELECT * FROM referrals WHERE referee_type = @referee_type AND referee_id = @referee_id FOR UPDATE;

-- name: LockReferral :one
SELECT * FROM referrals WHERE id = @id FOR UPDATE;

-- name: LockQualifyingReferrals :many
-- The referrals a purchase qualified, for a reversal (D-08): one order can
-- qualify its buyer's referral and complete its shop's count.
SELECT * FROM referrals WHERE qualifying_ref_type = @ref_type::text AND qualifying_ref_id = @ref_id
ORDER BY id FOR UPDATE;

-- name: SetQualified :one
UPDATE referrals SET status = 'qualified', qualifying_ref_type = @ref_type::text, qualifying_ref_id = @ref_id,
  qualified_at = @now::timestamptz, updated_at = @now::timestamptz, version = version + 1
WHERE id = @id AND status = 'pending'
RETURNING *;

-- name: SetSideRewarded :one
-- Marks one side posted; the row becomes rewarded when both are.
UPDATE referrals SET
  referrer_transaction_id = CASE WHEN @side::text = 'referrer' THEN sqlc.narg('transaction_id') ELSE referrer_transaction_id END,
  referrer_rewarded_at    = CASE WHEN @side::text = 'referrer' THEN @now ELSE referrer_rewarded_at END,
  referee_transaction_id  = CASE WHEN @side::text = 'referee' THEN sqlc.narg('transaction_id') ELSE referee_transaction_id END,
  referee_rewarded_at     = CASE WHEN @side::text = 'referee' THEN @now ELSE referee_rewarded_at END,
  status = CASE WHEN (CASE WHEN @side::text = 'referrer' THEN true ELSE referrer_rewarded_at IS NOT NULL END)
                 AND (CASE WHEN @side::text = 'referee' THEN true ELSE referee_rewarded_at IS NOT NULL END)
            THEN 'rewarded' ELSE status END,
  updated_at = @now, version = version + 1
WHERE id = @id AND status = 'qualified'
RETURNING *;

-- name: RevertQualified :exec
-- D-08: a reversed sale revokes a qualification whose reward has not posted.
UPDATE referrals SET status = 'pending', qualifying_ref_type = NULL, qualifying_ref_id = NULL, qualified_at = NULL,
  updated_at = @now, version = version + 1
WHERE id = @id AND status = 'qualified' AND referrer_rewarded_at IS NULL AND referee_rewarded_at IS NULL;

-- name: RejectReferral :exec
UPDATE referrals SET status = 'rejected', reject_reason = @reason, updated_at = @now, version = version + 1
WHERE id = @id AND status = 'pending';

-- name: PendingRefereeReferralOf :many
-- Pending referrals whose referee is this user (for account deletion).
SELECT id FROM referrals WHERE referee_type = 'customer' AND referee_id = @user_id AND status = 'pending';

-- name: QualifiedReferrals :many
-- Rewards still to post (the sweep), oldest first. A referral waiting only on
-- a blocked customer referrer is skipped (D-168), so a pile of them cannot
-- fill every batch; it comes back once the referrer is unblocked.
SELECT r.id FROM referrals r
WHERE r.status = 'qualified'
  AND NOT (r.referee_rewarded_at IS NOT NULL AND r.referrer_type = 'customer'
    AND EXISTS (SELECT 1 FROM users u WHERE u.id = r.referrer_id AND u.status = 'blocked'))
ORDER BY r.qualified_at, r.id LIMIT @lim;

-- name: PendingShopReferrals :many
-- New shops still counting toward qualification (the sweep).
SELECT id FROM referrals WHERE status = 'pending' AND kind = 'any_shop' ORDER BY created_at, id LIMIT @lim;

-- --- shop qualification ----------------------------------------------------------

-- name: StoreBusiness :one
SELECT business_id FROM stores WHERE id = @id;

-- name: BusinessVerified :one
SELECT (status = 'verified')::bool AS verified FROM businesses WHERE id = @id;

-- name: CountShopSale :exec
INSERT INTO referral_shop_sales (referral_id, ref_type, ref_id, store_id, customer_id, created_at)
VALUES (@referral_id, @ref_type, @ref_id, @store_id, @customer_id, @now)
ON CONFLICT DO NOTHING;

-- name: UncountShopSale :execrows
DELETE FROM referral_shop_sales WHERE ref_type = @ref_type AND ref_id = @ref_id
  AND referral_id IN (SELECT id FROM referrals WHERE status = 'pending');

-- name: ShopSaleCount :one
-- D-168: distinct customers, not purchases.
SELECT count(DISTINCT customer_id)::int AS n FROM referral_shop_sales WHERE referral_id = @referral_id;

-- name: LastShopSale :one
SELECT ref_type, ref_id FROM referral_shop_sales WHERE referral_id = @referral_id
ORDER BY created_at DESC, ref_id DESC LIMIT 1;

-- name: PrimaryStore :one
-- D-72: the business's oldest live store.
SELECT id FROM stores WHERE business_id = @business_id AND status = 'live'
ORDER BY live_at, id LIMIT 1;

-- name: UserStatus :one
SELECT status FROM users WHERE id = @id;

-- --- stats -----------------------------------------------------------------------

-- name: ReferrerStats :one
SELECT
  count(*) FILTER (WHERE status IN ('pending', 'qualified'))::int AS pending,
  count(*) FILTER (WHERE status = 'rewarded')::int AS rewarded,
  coalesce(sum(referrer_credits) FILTER (WHERE referrer_rewarded_at IS NOT NULL), 0)::bigint AS credits_earned
FROM referrals
WHERE referrer_type = @referrer_type AND referrer_id = ANY(@referrer_ids::uuid[]);

-- name: BusinessStoreIDs :many
SELECT id FROM stores WHERE business_id = @business_id ORDER BY id;

-- name: ClickCount :one
SELECT count(*)::int AS n FROM referral_clicks
WHERE referrer_type = @referrer_type AND referrer_id = ANY(@referrer_ids::uuid[]);

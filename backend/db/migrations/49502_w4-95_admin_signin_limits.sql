-- D-264 review (founder, 27 Oct): two limits on the admin's SMS codes.
--
-- 1. "Lock after 10 wrong". Wrong admin sign-in codes are counted per phone
--    number across OTP requests (admin_signin_failures). When
--    admin_signin_max_failures of them fall inside
--    admin_signin_failure_window_hours, the number's admin sign-in is locked
--    for admin_signin_lock_hours (admin_signin_locks): sends and verifies of
--    an admin-app sign-in code are refused with 429 signin_locked until the
--    lock runs out or another super_admin unlocks it (A-18). A good sign-in
--    code clears the count. The count is kept by phone hash for every
--    admin-app sign-in, whether or not the number belongs to an admin, so the
--    lock itself never tells a caller which numbers are admins.
-- 2. "Own quota of 10/hour". Admin step-up sends get their own bucket,
--    admin_step_up_sends_per_hour per number, apart from the sign-in limits
--    of 01 §2 (identity's queries leave admin step-up rows out of those).
--
-- Times are written from the application clock, never a now() default.
-- identity_purge deletes failures older than the longest window and locks
-- that have run out.

-- +goose Up
CREATE TABLE admin_signin_failures (
  id         uuid PRIMARY KEY,
  phone_hash bytea NOT NULL,
  failed_at  timestamptz NOT NULL
);
CREATE INDEX ix_admin_signin_failures_phone ON admin_signin_failures (phone_hash, failed_at);
CREATE INDEX ix_admin_signin_failures_failed ON admin_signin_failures (failed_at);

CREATE TABLE admin_signin_locks (
  phone_hash   bytea PRIMARY KEY,
  locked_at    timestamptz NOT NULL,
  locked_until timestamptz NOT NULL,
  CONSTRAINT ck_admin_signin_locks_until CHECK (locked_until > locked_at)
);

-- 01 §17 (D-264). TestSettingsMatchProductRules reads these inserts (D-95).
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('admin_signin_max_failures', '10', 'count', 3, 50, 'Admin sign-in. 01 §2: wrong admin sign-in codes for one number within admin_signin_failure_window_hours that lock its admin sign-in'),
  ('admin_signin_failure_window_hours', '24', 'hours', 1, 168, 'Admin sign-in. 01 §2: the window in which wrong admin sign-in codes are counted'),
  ('admin_signin_lock_hours', '24', 'hours', 1, 168, 'Admin sign-in. 01 §2: how long a locked admin sign-in stays locked unless another super admin unlocks it'),
  ('admin_step_up_sends_per_hour', '10', 'count', 1, 60, 'Admin sign-in. 01 §2: step-up codes one admin may ask for in a rolling hour, apart from the sign-in send limits');

-- +goose Down
DELETE FROM platform_settings WHERE key IN ('admin_signin_max_failures', 'admin_signin_failure_window_hours',
  'admin_signin_lock_hours', 'admin_step_up_sends_per_hour');
DROP TABLE admin_signin_locks;
DROP TABLE admin_signin_failures;

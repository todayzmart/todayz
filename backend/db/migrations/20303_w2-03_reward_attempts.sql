-- W2-03: reward posting retries back off (D-169).
--
-- A qualified referral's rewards post after the purchase commits (the
-- referral_reward job, the sweep as backstop). attempts counts the passes
-- that left a side unposted (a ledger error, a refusal, a beneficiary not
-- payable yet); next_attempt_at is when the sweep may try again. The sweep
-- reads only due rows, oldest due first, so stuck referrals cannot starve
-- fresh ones. Rows qualified before this migration are due at once.

-- +goose Up

ALTER TABLE referrals
  ADD COLUMN attempts int NOT NULL DEFAULT 0,
  ADD COLUMN next_attempt_at timestamptz,
  ADD CONSTRAINT ck_referrals_attempts CHECK (attempts >= 0);

UPDATE referrals SET next_attempt_at = qualified_at WHERE status = 'qualified';

CREATE INDEX ix_referrals_reward_due ON referrals (next_attempt_at, id) WHERE status = 'qualified';

-- +goose Down

DROP INDEX ix_referrals_reward_due;
ALTER TABLE referrals
  DROP CONSTRAINT ck_referrals_attempts,
  DROP COLUMN next_attempt_at,
  DROP COLUMN attempts;

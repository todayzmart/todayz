-- W3-06 field agents: audit an expired owner code once (the lead's review of
-- 9152967).
--
-- field_owner_codes.expired_audited_at is when the first verify on a code
-- that had lapsed, or that went to an earlier owner number, wrote its
-- business.owner_code_fail row. Later tries on that code are counted in the
-- metric only, so retries cannot fill the audit log (as a locked code is
-- audited once).

-- +goose Up

ALTER TABLE field_owner_codes ADD COLUMN expired_audited_at timestamptz;

-- +goose Down
ALTER TABLE field_owner_codes DROP COLUMN IF EXISTS expired_audited_at;

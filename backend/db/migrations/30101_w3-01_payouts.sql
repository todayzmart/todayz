-- W3-01 payouts and admin money (01 §7, §9; 03 admin_adjustment_requests,
-- payout_statements, payout_statement_lines, payouts, payout_lines; D-05,
-- D-09, D-73, D-85, D-93).
--
-- Amounts are credits (1 credit = ₹1 = 100 paise), as in the ledger; the API
-- shows them as *Paise.
--
-- admin_adjustment_requests: an adjustment whose |amount| is above
-- admin_adjust_dual_approval_above_paise waits here for a second finance or
-- super_admin. At or under it, the adjustment posts at once and no row is
-- written. The ledger account is the customer's wallet (03); link is the
-- optional ticket or order reference the A-09 form takes, which 03 does not
-- list (W3-01 report, Questions).
--
-- payout_statements and payout_statement_lines: the monthly report, closed on
-- the 1st at 00:30 IST. A statement never moves money; it is read from the
-- store_credit ledger entries of the business's stores in the month (IST).
--
-- payouts and payout_lines: at most one open (pending or held) payout per
-- business (D-09, ux_payouts_open_business); the next close recomputes it.
-- hold_reason adds `manual` (a finance hold from A-11, its text in
-- hold_note) to 03's three automatic causes (W3-01 report, Questions).

-- +goose Up

CREATE TABLE admin_adjustment_requests (
  id             uuid PRIMARY KEY,
  customer_id    uuid NOT NULL REFERENCES users (id),
  amount         int NOT NULL,
  reason         text NOT NULL,
  link           text,
  requested_by   uuid NOT NULL REFERENCES users (id),
  requested_at   timestamptz NOT NULL,
  status         text NOT NULL DEFAULT 'pending',
  approved_by    uuid REFERENCES users (id),
  decided_at     timestamptz,
  decision_note  text,
  transaction_id uuid REFERENCES ledger_transactions (id),
  version        int NOT NULL DEFAULT 1,
  -- The request's Idempotency-Key: a replay past the HTTP layer returns the
  -- first request instead of making a second one two approvers could post.
  idempotency_key text,
  CONSTRAINT ck_admin_adjustment_requests_idempotency_key CHECK (idempotency_key IS NULL OR char_length(idempotency_key) BETWEEN 1 AND 255),
  CONSTRAINT ck_admin_adjustment_requests_status CHECK (status IN ('pending','approved','rejected','cancelled')),
  CONSTRAINT ck_admin_adjustment_requests_amount CHECK (amount <> 0),
  CONSTRAINT ck_admin_adjustment_requests_reason CHECK (char_length(reason) BETWEEN 1 AND 500),
  CONSTRAINT ck_admin_adjustment_requests_link CHECK (link IS NULL OR char_length(link) BETWEEN 1 AND 200),
  CONSTRAINT ck_admin_adjustment_requests_note CHECK (decision_note IS NULL OR char_length(decision_note) BETWEEN 1 AND 500),
  -- D-05: the approver is never the requester.
  CONSTRAINT ck_admin_adjustment_requests_two_people CHECK (approved_by IS NULL OR approved_by <> requested_by),
  CONSTRAINT ck_admin_adjustment_requests_decided CHECK (
    (status = 'pending' AND decided_at IS NULL AND approved_by IS NULL AND transaction_id IS NULL) OR
    (status = 'approved' AND decided_at IS NOT NULL AND approved_by IS NOT NULL AND transaction_id IS NOT NULL) OR
    (status IN ('rejected','cancelled') AND decided_at IS NOT NULL AND transaction_id IS NULL))
);
CREATE INDEX ix_admin_adjustment_requests_pending ON admin_adjustment_requests (requested_at, id) WHERE status = 'pending';
CREATE INDEX ix_admin_adjustment_requests_requested ON admin_adjustment_requests (requested_at DESC, id DESC);
CREATE INDEX ix_admin_adjustment_requests_customer ON admin_adjustment_requests (customer_id);
CREATE INDEX ix_admin_adjustment_requests_requested_by ON admin_adjustment_requests (requested_by);
CREATE INDEX ix_admin_adjustment_requests_approved_by ON admin_adjustment_requests (approved_by) WHERE approved_by IS NOT NULL;
CREATE UNIQUE INDEX ux_admin_adjustment_requests_idempotency ON admin_adjustment_requests (requested_by, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE UNIQUE INDEX ux_admin_adjustment_requests_transaction ON admin_adjustment_requests (transaction_id) WHERE transaction_id IS NOT NULL;

CREATE TABLE payouts (
  id                uuid PRIMARY KEY,
  business_id       uuid NOT NULL REFERENCES businesses (id),
  amount            bigint NOT NULL,
  status            text NOT NULL,
  hold_reason       text,
  hold_note         text,
  exported_at       timestamptz,
  computed_at       timestamptz NOT NULL,
  last_statement_id uuid,
  utr               text,
  paid_at           timestamptz,
  paid_by           uuid REFERENCES users (id),
  transaction_id    uuid REFERENCES ledger_transactions (id),
  created_at        timestamptz NOT NULL,
  updated_at        timestamptz NOT NULL,
  version           int NOT NULL DEFAULT 1,
  CONSTRAINT ck_payouts_status CHECK (status IN ('pending','held','paid','cancelled')),
  CONSTRAINT ck_payouts_amount CHECK (amount > 0 OR status = 'cancelled'),
  CONSTRAINT ck_payouts_hold_reason CHECK (hold_reason IS NULL OR hold_reason IN ('kyc_incomplete','payout_details_missing','risk_flag','business_suspended','manual')),
  CONSTRAINT ck_payouts_held CHECK ((status = 'held') = (hold_reason IS NOT NULL)),
  CONSTRAINT ck_payouts_hold_note CHECK (hold_note IS NULL OR (hold_reason = 'manual' AND char_length(hold_note) BETWEEN 1 AND 500)),
  CONSTRAINT ck_payouts_utr CHECK (utr IS NULL OR utr ~ '^[A-Z0-9]{12,22}$'),
  CONSTRAINT ck_payouts_paid CHECK (
    (status = 'paid') = (utr IS NOT NULL) AND
    (status = 'paid') = (paid_at IS NOT NULL) AND
    (status = 'paid') = (paid_by IS NOT NULL) AND
    (status = 'paid') = (transaction_id IS NOT NULL))
);
-- D-09: at most one open payout per business.
CREATE UNIQUE INDEX ux_payouts_open_business ON payouts (business_id) WHERE status IN ('pending','held');
CREATE INDEX ix_payouts_business ON payouts (business_id, created_at DESC);
CREATE INDEX ix_payouts_status ON payouts (status, created_at DESC, id DESC);
CREATE INDEX ix_payouts_paid_by ON payouts (paid_by) WHERE paid_by IS NOT NULL;
CREATE UNIQUE INDEX ux_payouts_transaction ON payouts (transaction_id) WHERE transaction_id IS NOT NULL;
-- A bank reference pays one payout only.
CREATE UNIQUE INDEX ux_payouts_utr ON payouts (utr) WHERE utr IS NOT NULL;

CREATE TABLE payout_lines (
  payout_id uuid NOT NULL REFERENCES payouts (id),
  store_id  uuid NOT NULL REFERENCES stores (id),
  amount    bigint NOT NULL,
  PRIMARY KEY (payout_id, store_id),
  CONSTRAINT ck_payout_lines_amount CHECK (amount > 0)
);
CREATE INDEX ix_payout_lines_store ON payout_lines (store_id);

CREATE TABLE payout_statements (
  id           uuid PRIMARY KEY,
  business_id  uuid NOT NULL REFERENCES businesses (id),
  period       date NOT NULL,
  opening      bigint NOT NULL,
  credits_in   bigint NOT NULL,
  cashback_out bigint NOT NULL,
  referral_in  bigint NOT NULL,
  adjustments  bigint NOT NULL,
  reversals    bigint NOT NULL,
  payouts_out  bigint NOT NULL,
  closing      bigint NOT NULL,
  status       text NOT NULL,
  closed_at    timestamptz,
  payout_id    uuid REFERENCES payouts (id),
  -- What this close computed for payout_id. A later close recomputes the same
  -- open payout (D-09), so the payout row holds only the latest amount.
  computed_payout bigint,
  created_at   timestamptz NOT NULL,
  CONSTRAINT ck_payout_statements_status CHECK (status IN ('open','closed')),
  CONSTRAINT ck_payout_statements_computed CHECK ((payout_id IS NULL) = (computed_payout IS NULL) AND (computed_payout IS NULL OR computed_payout > 0)),
  CONSTRAINT ck_payout_statements_closed CHECK ((status = 'closed') = (closed_at IS NOT NULL)),
  CONSTRAINT ck_payout_statements_period CHECK (extract(day FROM period) = 1),
  -- 01 §9: opening + credits in − cashback ± reversals and adjustments − payouts paid = closing.
  CONSTRAINT ck_payout_statements_ties CHECK (
    opening + credits_in + referral_in - cashback_out + reversals + adjustments - payouts_out = closing)
);
CREATE UNIQUE INDEX ux_payout_statements_business_period ON payout_statements (business_id, period);
CREATE INDEX ix_payout_statements_period ON payout_statements (period, business_id);
CREATE INDEX ix_payout_statements_payout ON payout_statements (payout_id) WHERE payout_id IS NOT NULL;

ALTER TABLE payouts ADD CONSTRAINT fk_payouts_last_statement
  FOREIGN KEY (last_statement_id) REFERENCES payout_statements (id);
CREATE INDEX ix_payouts_last_statement ON payouts (last_statement_id) WHERE last_statement_id IS NOT NULL;

CREATE TABLE payout_statement_lines (
  statement_id uuid NOT NULL REFERENCES payout_statements (id),
  store_id     uuid NOT NULL REFERENCES stores (id),
  kind         text NOT NULL,
  amount       bigint NOT NULL,
  count        int NOT NULL,
  PRIMARY KEY (statement_id, store_id, kind),
  CONSTRAINT ck_payout_statement_lines_kind CHECK (kind IN ('opening','credits_in','referral_in','cashback_out','reversals','adjustments','payouts_out','closing')),
  CONSTRAINT ck_payout_statement_lines_count CHECK (count >= 0)
);
CREATE INDEX ix_payout_statement_lines_store ON payout_statement_lines (store_id);

-- +goose Down
DROP TABLE IF EXISTS payout_statement_lines;
ALTER TABLE payouts DROP CONSTRAINT IF EXISTS fk_payouts_last_statement;
DROP TABLE IF EXISTS payout_statements;
DROP TABLE IF EXISTS payout_lines;
DROP TABLE IF EXISTS payouts;
DROP TABLE IF EXISTS admin_adjustment_requests;

-- D-281: the shop's own store sheet (D-278, printed in the browser) leaves no
-- qr_standees row, so the setup step "Print the QR standee" never ticked for a
-- shop that only printed it. standee_printed_at is when an owner or manager
-- first pressed Print on the store sheet; the setup step counts it as done,
-- as it counts any campaign standee.

-- +goose Up
ALTER TABLE stores ADD COLUMN standee_printed_at timestamptz;

-- +goose Down
ALTER TABLE stores DROP COLUMN standee_printed_at;

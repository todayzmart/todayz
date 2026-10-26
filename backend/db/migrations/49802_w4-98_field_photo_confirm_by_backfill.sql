-- Backend risk fixes: confirm_by for the field photos left pending without
-- one. 30603 added business_documents.confirm_by but set it only on new
-- rows, so a field photo created before it and never confirmed has none: it
-- holds a place in the 5-per-kind cap for ever and the sweep never takes it.
--
-- A field photo is one the field routes created: its document.create audit
-- row has actor_kind 'admin' (the owner routes write 'staff', and their rows
-- keep confirm_by null on purpose). audit_log.subject_id is the API's id
-- form, the ULID text (Crockford base32 of the 128 bits, two zero bits
-- first), so the uuid is encoded the same way here.
--
-- confirm_by is created_at plus the code's pending life (pendingLife in
-- internal/business/agent.go: uploadTTL, 15 min, plus 5 min). It is a code
-- constant, not a platform setting, so the 20 minutes are written here; a
-- change to pendingLife does not reach rows this file already set. Every
-- such row is long past it, so the next business_field_photos_sweep rejects
-- them and deletes their objects.
--
-- The audit_log read sits in a subselect (out of TestMigrationBlockOrder's
-- sight, README); audit_log is a lower block's table (00001).

-- +goose Up
UPDATE business_documents d
   SET confirm_by = d.created_at + interval '20 minutes'
 WHERE d.status = 'pending' AND d.confirm_by IS NULL
   AND EXISTS (
     SELECT 1 FROM audit_log a
      WHERE a.action = 'document.create' AND a.subject_type = 'business_document' AND a.actor_kind = 'admin'
        AND a.subject_id = (
          SELECT string_agg(substr('0123456789ABCDEFGHJKMNPQRSTVWXYZ', substring(b.bits FROM i * 5 + 1 FOR 5)::int + 1, 1), '' ORDER BY i)
            FROM (SELECT B'00' || ('x' || replace(d.id::text, '-', ''))::bit(128) AS bits) b, generate_series(0, 25) i));

-- +goose Down
-- Nothing to undo: a backfilled row cannot be told from one the field routes
-- created with its confirm_by, and Down of 30603 drops the column.

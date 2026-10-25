-- support queries, generated into package supportdb (gen/sqlc/supportdb).
-- Only W3-08 (internal/support) edits this folder (D-50). A note (kind = 'note') leaves
-- only through the admin reads; every business and customer read filters it out.
-- sla_started_at and sla_due_at are set exactly while a ticket is open (ck_support_tickets_sla).

-- name: InsertTicket :one
INSERT INTO support_tickets (id, opener_type, opener_user_id, business_id, store_id, order_id, category, subject,
                             status, priority, sla_started_at, sla_due_at, last_message_at, created_at, updated_at)
VALUES (@id, @opener_type, @opener_user_id, sqlc.narg('business_id'), sqlc.narg('store_id'), sqlc.narg('order_id'),
        @category, @subject, 'open', @priority, @now::timestamptz, @sla_due_at, @now::timestamptz, @now::timestamptz, @now::timestamptz)
RETURNING id;

-- name: InsertMessage :exec
INSERT INTO support_messages (id, ticket_id, author_type, author_user_id, kind, body, created_at)
VALUES (@id, @ticket_id, @author_type, @author_user_id, @kind, @body, @now);

-- name: ClaimAttachments :many
-- Links the caller's own unclaimed uploads to a message and returns them, so the caller
-- can check the count against the ids it sent (fewer means one was not theirs, was
-- already used or is past support_upload_ttl_hours) and then the objects in the bucket.
-- The business must match exactly: a business's upload never goes on a customer's or an
-- agent's message, nor theirs on a business's. An agent's upload records the ticket it
-- was asked for and goes on a message of that ticket only; a customer's or business's
-- has none until now (upload_ticket is NULL for them).
UPDATE support_attachments
SET ticket_id = @ticket_id, message_id = @message_id
WHERE id = ANY(@ids::uuid[]) AND uploaded_by = @uploaded_by AND message_id IS NULL
  AND business_id IS NOT DISTINCT FROM sqlc.narg('business_id')::uuid
  AND ticket_id IS NOT DISTINCT FROM sqlc.narg('upload_ticket')::uuid
  AND created_at > @not_before::timestamptz
RETURNING id, object_key, content_type, size_bytes;

-- name: InsertAttachment :exec
INSERT INTO support_attachments (id, uploaded_by, business_id, ticket_id, object_key, content_type, size_bytes, file_name, created_at)
VALUES (@id, @uploaded_by, sqlc.narg('business_id'), sqlc.narg('ticket_id'), @object_key, @content_type, @size_bytes, @file_name, @now);

-- name: LockSupportUser :exec
-- Serialises one person's ticket openings and uploads (the limits below read, then
-- write) for the rest of the transaction. lock_key is 'support:' and the user id.
SELECT pg_advisory_xact_lock(hashtextextended(@lock_key::text, 0));

-- name: OpenerLoad :one
-- The person's tickets open or waiting now, and those they opened since @since.
SELECT (SELECT count(*) FROM support_tickets a WHERE a.opener_user_id = @user_id AND a.status IN ('open','waiting'))::int AS active,
       (SELECT count(*) FROM support_tickets r WHERE r.opener_user_id = @user_id AND r.created_at > @since::timestamptz)::int AS recent;

-- name: CountRecentUploads :one
SELECT count(*)::int FROM support_attachments WHERE uploaded_by = @uploaded_by AND created_at > @since::timestamptz;

-- name: StaleUploads :many
-- Uploads no message claimed before @before, oldest first, locked for the sweep. A row
-- a message is claiming right now is skipped (that claim refuses it anyway).
SELECT id, object_key FROM support_attachments
WHERE message_id IS NULL AND created_at < @before::timestamptz
ORDER BY created_at, id
LIMIT @lim
FOR UPDATE SKIP LOCKED;

-- name: DeleteAttachments :execrows
DELETE FROM support_attachments WHERE id = ANY(@ids::uuid[]);

-- name: CustomerTicketsToClose :many
-- Account deletion: the customer's own tickets not closed yet, locked.
SELECT id, status FROM support_tickets
WHERE opener_type = 'customer' AND opener_user_id = @user_id AND status <> 'closed'
ORDER BY id
FOR UPDATE;

-- name: ScrubCustomerTickets :exec
-- Account deletion: the subjects of the customer's own tickets become @placeholder.
UPDATE support_tickets SET subject = @placeholder, version = version + 1
WHERE opener_type = 'customer' AND opener_user_id = @user_id AND subject <> @placeholder;

-- name: ScrubCustomerMessages :exec
-- Account deletion: the customer's own messages on their tickets become @placeholder.
-- Support's replies and notes stay.
UPDATE support_messages m SET body = @placeholder
FROM support_tickets t
WHERE m.ticket_id = t.id AND t.opener_type = 'customer' AND t.opener_user_id = @user_id
  AND m.author_type = 'customer' AND m.author_user_id = @user_id AND m.body <> @placeholder;

-- name: CustomerAttachments :many
-- Account deletion: the customer's own uploads (business uploads stay with the business),
-- claimed on their tickets or not claimed yet, locked.
SELECT a.id, a.object_key FROM support_attachments a
WHERE a.uploaded_by = @user_id AND a.business_id IS NULL
  AND (a.ticket_id IS NULL OR a.ticket_id IN (
        SELECT t.id FROM support_tickets t WHERE t.opener_type = 'customer' AND t.opener_user_id = @user_id))
ORDER BY a.id
FOR UPDATE OF a;

-- name: LockTicket :one
SELECT id, opener_type, opener_user_id, business_id, status, priority, assignee_id, sla_started_at, sla_due_at, first_response_at
FROM support_tickets
WHERE id = @id
FOR UPDATE;

-- name: TicketAfterAgentReply :exec
-- Support answered: the opener's turn, the SLA stops.
UPDATE support_tickets
SET status = 'waiting', sla_started_at = NULL, sla_due_at = NULL, resolved_at = NULL, close_reason = NULL,
    first_response_at = COALESCE(first_response_at, @now),
    assignee_id = COALESCE(assignee_id, sqlc.narg('assignee_id')::uuid),
    last_message_at = @now, version = version + 1
WHERE id = @id;

-- name: TicketAfterOpenerReply :exec
-- The opener wrote: support's turn again. The caller passes the timer: kept when the
-- ticket was already open, otherwise started now with the priority's SLA (01 §13).
UPDATE support_tickets
SET status = 'open', sla_started_at = @sla_started_at, sla_due_at = @sla_due_at, resolved_at = NULL, close_reason = NULL,
    last_message_at = @now, version = version + 1
WHERE id = @id;

-- name: TouchTicket :exec
UPDATE support_tickets SET version = version + 1 WHERE id = @id;

-- name: SetTicketPriority :exec
-- The caller passes the new due time on an open ticket (its timer's start plus the new
-- priority's SLA) and NULL otherwise.
UPDATE support_tickets SET priority = @priority, sla_due_at = sqlc.narg('sla_due_at'), version = version + 1 WHERE id = @id;

-- name: SetTicketAssignee :exec
UPDATE support_tickets SET assignee_id = sqlc.narg('assignee_id'), version = version + 1 WHERE id = @id;

-- name: ResolveTicket :exec
UPDATE support_tickets
SET status = 'resolved', sla_started_at = NULL, sla_due_at = NULL, resolved_at = @now, close_reason = @reason, version = version + 1
WHERE id = @id;

-- name: CloseTicket :exec
UPDATE support_tickets
SET status = 'closed', sla_started_at = NULL, sla_due_at = NULL, resolved_at = NULL, closed_at = @now, close_reason = @reason, version = version + 1
WHERE id = @id;

-- name: GetAgent :one
-- An assignee: an active admin user who works the inbox.
SELECT au.user_id
FROM admin_users au JOIN users u ON u.id = au.user_id
WHERE au.user_id = @user_id AND au.disabled_at IS NULL AND u.status = 'active'
  AND au.roles && ARRAY['support','admin','super_admin']::text[];

-- name: ListAgents :many
SELECT au.user_id, u.name
FROM admin_users au JOIN users u ON u.id = au.user_id
WHERE au.disabled_at IS NULL AND u.status = 'active'
  AND au.roles && ARRAY['support','admin','super_admin']::text[]
ORDER BY u.name NULLS LAST, au.user_id;

-- name: StoreOfBusiness :one
SELECT id FROM stores WHERE id = @id AND business_id = @business_id;

-- name: OrderOfBusiness :one
-- The one read of the orders table: the order belongs to a store of this business.
SELECT o.id, o.store_id
FROM orders o JOIN stores s ON s.id = o.store_id
WHERE o.id = @id AND s.business_id = @business_id;

-- name: OrderOfCustomer :one
-- The customer's own order and its store.
SELECT o.id, o.store_id FROM orders o WHERE o.id = @id AND o.customer_id = @customer_id;

-- name: ListOpenTickets :many
-- The open tab: soonest due (overdue first), then id.
SELECT t.id, t.display_number, t.subject, t.category, t.status, t.priority, t.opener_type, t.opener_user_id,
       ou.name AS opener_name, t.business_id, b.brand_name AS business_name, t.store_id, s.name AS store_name,
       t.order_id, o.display_number AS order_number, t.assignee_id, au.name AS assignee_name,
       t.sla_due_at, t.last_message_at, t.created_at
FROM support_tickets t
JOIN users ou ON ou.id = t.opener_user_id
LEFT JOIN businesses b ON b.id = t.business_id
LEFT JOIN stores s ON s.id = t.store_id
LEFT JOIN orders o ON o.id = t.order_id
LEFT JOIN users au ON au.id = t.assignee_id
WHERE t.status = 'open'
  AND (@assignee_mode::text = ''
       OR (@assignee_mode::text = 'me' AND t.assignee_id = sqlc.narg('me')::uuid)
       OR (@assignee_mode::text = 'unassigned' AND t.assignee_id IS NULL))
  AND (@opener_type::text = '' OR t.opener_type = @opener_type::text)
  AND (sqlc.narg('after_at')::timestamptz IS NULL OR (t.sla_due_at, t.id) > (sqlc.narg('after_at')::timestamptz, sqlc.narg('after_id')::uuid))
ORDER BY t.sla_due_at, t.id
LIMIT @lim;

-- name: ListTicketsByActivity :many
-- The waiting, resolved and closed tabs: latest message first.
SELECT t.id, t.display_number, t.subject, t.category, t.status, t.priority, t.opener_type, t.opener_user_id,
       ou.name AS opener_name, t.business_id, b.brand_name AS business_name, t.store_id, s.name AS store_name,
       t.order_id, o.display_number AS order_number, t.assignee_id, au.name AS assignee_name,
       t.sla_due_at, t.last_message_at, t.created_at
FROM support_tickets t
JOIN users ou ON ou.id = t.opener_user_id
LEFT JOIN businesses b ON b.id = t.business_id
LEFT JOIN stores s ON s.id = t.store_id
LEFT JOIN orders o ON o.id = t.order_id
LEFT JOIN users au ON au.id = t.assignee_id
WHERE t.status = @status::text
  AND (@assignee_mode::text = ''
       OR (@assignee_mode::text = 'me' AND t.assignee_id = sqlc.narg('me')::uuid)
       OR (@assignee_mode::text = 'unassigned' AND t.assignee_id IS NULL))
  AND (@opener_type::text = '' OR t.opener_type = @opener_type::text)
  AND (sqlc.narg('before_at')::timestamptz IS NULL OR (t.last_message_at, t.id) < (sqlc.narg('before_at')::timestamptz, sqlc.narg('before_id')::uuid))
ORDER BY t.last_message_at DESC, t.id DESC
LIMIT @lim;

-- name: CountTickets :one
-- One count per status, each inside its own range of ix_support_tickets_counts (status,
-- opener_type, assignee_id INCLUDE sla_due_at), so no count reads another status's rows
-- or the table.
SELECT (SELECT count(*) FROM support_tickets o
        WHERE o.status = 'open'
          AND (@assignee_mode::text = '' OR (@assignee_mode::text = 'me' AND o.assignee_id = sqlc.narg('me')::uuid)
               OR (@assignee_mode::text = 'unassigned' AND o.assignee_id IS NULL))
          AND (@opener_type::text = '' OR o.opener_type = @opener_type::text))::int AS open,
       (SELECT count(*) FROM support_tickets w
        WHERE w.status = 'waiting'
          AND (@assignee_mode::text = '' OR (@assignee_mode::text = 'me' AND w.assignee_id = sqlc.narg('me')::uuid)
               OR (@assignee_mode::text = 'unassigned' AND w.assignee_id IS NULL))
          AND (@opener_type::text = '' OR w.opener_type = @opener_type::text))::int AS waiting,
       (SELECT count(*) FROM support_tickets r
        WHERE r.status = 'resolved'
          AND (@assignee_mode::text = '' OR (@assignee_mode::text = 'me' AND r.assignee_id = sqlc.narg('me')::uuid)
               OR (@assignee_mode::text = 'unassigned' AND r.assignee_id IS NULL))
          AND (@opener_type::text = '' OR r.opener_type = @opener_type::text))::int AS resolved,
       (SELECT count(*) FROM support_tickets c
        WHERE c.status = 'closed'
          AND (@assignee_mode::text = '' OR (@assignee_mode::text = 'me' AND c.assignee_id = sqlc.narg('me')::uuid)
               OR (@assignee_mode::text = 'unassigned' AND c.assignee_id IS NULL))
          AND (@opener_type::text = '' OR c.opener_type = @opener_type::text))::int AS closed,
       (SELECT count(*) FROM support_tickets d
        WHERE d.status = 'open' AND d.sla_due_at < @now::timestamptz
          AND (@assignee_mode::text = '' OR (@assignee_mode::text = 'me' AND d.assignee_id = sqlc.narg('me')::uuid)
               OR (@assignee_mode::text = 'unassigned' AND d.assignee_id IS NULL))
          AND (@opener_type::text = '' OR d.opener_type = @opener_type::text))::int AS overdue;

-- name: GetTicket :one
SELECT t.id, t.display_number, t.subject, t.category, t.status, t.priority, t.opener_type, t.opener_user_id,
       ou.name AS opener_name, t.business_id, b.brand_name AS business_name, t.store_id, s.name AS store_name,
       t.order_id, o.display_number AS order_number, o.status AS order_status, t.assignee_id, au.name AS assignee_name,
       t.sla_due_at, t.first_response_at, t.last_message_at, t.resolved_at, t.closed_at, t.close_reason, t.created_at
FROM support_tickets t
JOIN users ou ON ou.id = t.opener_user_id
LEFT JOIN businesses b ON b.id = t.business_id
LEFT JOIN stores s ON s.id = t.store_id
LEFT JOIN orders o ON o.id = t.order_id
LEFT JOIN users au ON au.id = t.assignee_id
WHERE t.id = @id;

-- name: ListMessages :many
-- Oldest first. include_notes is true only for the admin reads.
SELECT m.id, m.kind, m.author_type, m.author_user_id, u.name AS author_name, m.body, m.created_at
FROM support_messages m
JOIN users u ON u.id = m.author_user_id
WHERE m.ticket_id = @ticket_id AND (@include_notes::boolean OR m.kind = 'reply')
ORDER BY m.created_at, m.id;

-- name: ListTicketAttachments :many
SELECT a.id, a.message_id, a.object_key, a.content_type, a.size_bytes, a.file_name
FROM support_attachments a
WHERE a.ticket_id = @ticket_id AND a.message_id IS NOT NULL
ORDER BY a.created_at, a.id;

-- name: ListBusinessTickets :many
SELECT t.id, t.display_number, t.subject, t.category, t.status, t.store_id, s.name AS store_name,
       t.order_id, o.display_number AS order_number, t.last_message_at, t.created_at
FROM support_tickets t
LEFT JOIN stores s ON s.id = t.store_id
LEFT JOIN orders o ON o.id = t.order_id
WHERE t.business_id = @business_id
  AND (sqlc.narg('before_at')::timestamptz IS NULL OR (t.last_message_at, t.id) < (sqlc.narg('before_at')::timestamptz, sqlc.narg('before_id')::uuid))
ORDER BY t.last_message_at DESC, t.id DESC
LIMIT @lim;

-- name: GetBusinessTicket :one
SELECT t.id, t.display_number, t.subject, t.category, t.status, t.store_id, s.name AS store_name,
       t.order_id, o.display_number AS order_number, t.last_message_at, t.created_at
FROM support_tickets t
LEFT JOIN stores s ON s.id = t.store_id
LEFT JOIN orders o ON o.id = t.order_id
WHERE t.id = @id AND t.business_id = @business_id;

-- name: ListCustomerTickets :many
-- The customer's own tickets; a business ticket never appears here.
SELECT t.id, t.display_number, t.subject, t.category, t.status, s.name AS store_name,
       t.order_id, o.display_number AS order_number, t.last_message_at, t.created_at
FROM support_tickets t
LEFT JOIN stores s ON s.id = t.store_id
LEFT JOIN orders o ON o.id = t.order_id
WHERE t.opener_type = 'customer' AND t.opener_user_id = @user_id
  AND (sqlc.narg('before_at')::timestamptz IS NULL OR (t.last_message_at, t.id) < (sqlc.narg('before_at')::timestamptz, sqlc.narg('before_id')::uuid))
ORDER BY t.last_message_at DESC, t.id DESC
LIMIT @lim;

-- name: GetCustomerTicket :one
SELECT t.id, t.display_number, t.subject, t.category, t.status, s.name AS store_name,
       t.order_id, o.display_number AS order_number, t.last_message_at, t.created_at
FROM support_tickets t
LEFT JOIN stores s ON s.id = t.store_id
LEFT JOIN orders o ON o.id = t.order_id
WHERE t.id = @id AND t.opener_type = 'customer' AND t.opener_user_id = @user_id;

-- name: SetTicketTriage :exec
-- The A-14 triage assistant's confirmed category and linked order (W3-13): the
-- caller checked the order is the opener's and passes its store.
UPDATE support_tickets
SET category = @category, order_id = COALESCE(sqlc.narg('order_id')::uuid, order_id),
    store_id = COALESCE(sqlc.narg('store_id')::uuid, store_id), version = version + 1
WHERE id = @id;

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

-- name: ClaimAttachments :execrows
-- Links the caller's own unclaimed uploads to a message. The caller checks the row count
-- against the ids it sent; fewer means one was not theirs or was already used. The
-- business must match exactly: a business's upload never goes on a customer's or an
-- agent's message, nor theirs on a business's.
UPDATE support_attachments
SET ticket_id = @ticket_id, message_id = @message_id
WHERE id = ANY(@ids::uuid[]) AND uploaded_by = @uploaded_by AND message_id IS NULL
  AND business_id IS NOT DISTINCT FROM sqlc.narg('business_id')::uuid;

-- name: InsertAttachment :exec
INSERT INTO support_attachments (id, uploaded_by, business_id, object_key, content_type, size_bytes, file_name, created_at)
VALUES (@id, @uploaded_by, sqlc.narg('business_id'), @object_key, @content_type, @size_bytes, @file_name, @now);

-- name: LockTicket :one
SELECT id, opener_type, opener_user_id, business_id, status, priority, assignee_id, sla_started_at, sla_due_at, first_response_at
FROM support_tickets
WHERE id = @id
FOR UPDATE;

-- name: TicketAfterAgentReply :exec
-- Support answered: the opener's turn, the SLA stops.
UPDATE support_tickets
SET status = 'waiting', sla_started_at = NULL, sla_due_at = NULL, resolved_at = NULL,
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
SELECT count(*) FILTER (WHERE t.status = 'open')::int AS open,
       count(*) FILTER (WHERE t.status = 'waiting')::int AS waiting,
       count(*) FILTER (WHERE t.status = 'resolved')::int AS resolved,
       count(*) FILTER (WHERE t.status = 'closed')::int AS closed,
       count(*) FILTER (WHERE t.status = 'open' AND t.sla_due_at < @now::timestamptz)::int AS overdue
FROM support_tickets t
WHERE (@assignee_mode::text = ''
       OR (@assignee_mode::text = 'me' AND t.assignee_id = sqlc.narg('me')::uuid)
       OR (@assignee_mode::text = 'unassigned' AND t.assignee_id IS NULL))
  AND (@opener_type::text = '' OR t.opener_type = @opener_type::text);

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
WHERE a.ticket_id = @ticket_id
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

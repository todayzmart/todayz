-- aiadmin queries (W3-13 admin AI: catalogue clean-up, ticket triage, risk
-- summary), generated into package aiadmindb (gen/sqlc/aiadmindb). Only the
-- work package that builds internal/ai/{catalogue,triage,riskexplain} edits
-- this folder (D-50). Reads only: every write goes through the owning
-- domain's own writer. No query reads a phone, an address or a pickup code.

-- --- AI-A1 catalogue clean-up ---------------------------------------------------------

-- name: PendingForAssist :one
-- A product waiting in the A-08 review queue.
SELECT p.id, p.name, b.name AS brand_name, p.category_id, (c.names ->> 'en')::text AS category_name,
  c.names AS category_names, p.variant, p.pack_size::float8 AS pack_size, p.unit
FROM products p JOIN categories c ON c.id = p.category_id LEFT JOIN brands b ON b.id = p.brand_id
WHERE p.id = @id AND p.status = 'pending_review';

-- name: DuplicateCandidates :many
-- The queue's own look-alikes (A-08's suggestions): active products whose
-- names are close (pg_trgm), closest first. The model picks among these only.
SELECT p.id, p.name, b.name AS brand_name, p.variant, p.pack_size::float8 AS pack_size, p.unit,
  similarity(p.name, @name::text)::float8 AS similarity
FROM products p LEFT JOIN brands b ON b.id = p.brand_id
WHERE p.status = 'active' AND p.id <> @id AND p.name % @name::text
ORDER BY similarity(p.name, @name::text) DESC, p.id
LIMIT 5;

-- name: AssistCategories :many
-- The categories a product may move to, by their English name.
SELECT c.id, (c.names ->> 'en')::text AS name
FROM categories c
ORDER BY (c.parent_id IS NOT NULL), c.sort, c.slug;

-- --- AI-A2 ticket triage --------------------------------------------------------------

-- name: TicketForTriage :one
-- A ticket and its linked order; never the opener's name or phone.
SELECT t.id, t.display_number, t.subject, t.category, t.status, t.priority, t.opener_type, t.opener_user_id,
  t.business_id, t.order_id, o.display_number AS order_number
FROM support_tickets t LEFT JOIN orders o ON o.id = t.order_id
WHERE t.id = @id;

-- name: TicketReplies :many
-- The thread's replies, oldest first; internal notes are never read here.
SELECT m.author_type, m.body, m.created_at
FROM support_messages m
WHERE m.ticket_id = @ticket_id AND m.kind = 'reply'
ORDER BY m.created_at, m.id
LIMIT 40;

-- name: OpenerOrders :many
-- The orders a ticket may be about: a customer's own recent orders, or a
-- business's stores' recent orders. Never the address or the pickup code.
SELECT o.id, o.display_number, o.status, o.fulfilment, o.total_paise, o.placed_at, s.name AS store_name
FROM orders o JOIN stores s ON s.id = o.store_id
WHERE (sqlc.narg(customer_id)::uuid IS NOT NULL AND o.customer_id = sqlc.narg(customer_id)::uuid)
   OR (sqlc.narg(business_id)::uuid IS NOT NULL AND s.business_id = sqlc.narg(business_id)::uuid)
ORDER BY o.placed_at DESC, o.id DESC
LIMIT 10;

-- name: TriageOrderEvents :many
-- One order's timeline, oldest first.
SELECT e.from_status, e.to_status, e.actor_kind, e.reason, e.created_at
FROM order_events e
WHERE e.order_id = @order_id
ORDER BY e.created_at, e.id
LIMIT 30;

-- --- AI-A3 risk summary ---------------------------------------------------------------

-- name: FlagForSummary :one
SELECT f.id, f.rule, f.subject_type, f.subject_id, f.score, f.status, f.details, f.created_at
FROM risk_flags f
WHERE f.id = @id;

-- name: SubjectFlagCounts :one
-- The subject's other flags by status (A-13's history).
SELECT count(*) FILTER (WHERE f.status = 'open')::int AS open,
  count(*) FILTER (WHERE f.status = 'confirmed')::int AS confirmed,
  count(*) FILTER (WHERE f.status = 'cleared')::int AS cleared
FROM risk_flags f
WHERE f.subject_type = @subject_type AND f.subject_id = @subject_id AND f.id <> @id;

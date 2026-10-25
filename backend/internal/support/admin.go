package support

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	adminapi "github.com/todayz/backend/gen/api/admin-support"
	"github.com/todayz/backend/gen/sqlc/supportdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// The admin side (A-14, 01 §13, D-226): the inbox, the ticket, assign, reply,
// notes, priority, resolve or close, and uploads. Assign, priority, resolve
// and close write an audit_log row in the same transaction.

// summaryRow is one inbox row; ListTicketsByActivityRow has the same fields.
type summaryRow = supportdb.ListOpenTicketsRow

func (s *Service) listAdmin(ctx context.Context, me ids.ID, p adminapi.ListAdminTicketsParams) (adminapi.TicketList, error) {
	if err := s.ready(ctx); err != nil {
		return adminapi.TicketList{}, err
	}
	status := string(adminapi.TicketStatusOpen)
	if p.Status != nil {
		status = string(*p.Status)
	}
	var mode, opener string
	if p.Assignee != nil {
		mode = string(*p.Assignee)
	}
	if p.Opener != nil {
		opener = string(*p.Opener)
	}
	lim, at, after, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return adminapi.TicketList{}, err
	}
	q := s.q(ctx)
	var rows []summaryRow
	if status == string(adminapi.TicketStatusOpen) {
		rows, err = q.ListOpenTickets(ctx, supportdb.ListOpenTicketsParams{
			AssigneeMode: mode, Me: ids.UUID(me), OpenerType: opener, AfterAt: at, AfterID: after, Lim: lim + 1,
		})
	} else {
		var act []supportdb.ListTicketsByActivityRow
		act, err = q.ListTicketsByActivity(ctx, supportdb.ListTicketsByActivityParams{
			Status: status, AssigneeMode: mode, Me: ids.UUID(me), OpenerType: opener, BeforeAt: at, BeforeID: after, Lim: lim + 1,
		})
		for _, r := range act {
			rows = append(rows, summaryRow(r))
		}
	}
	if err != nil {
		return adminapi.TicketList{}, err
	}
	now := s.now()
	counts, err := q.CountTickets(ctx, supportdb.CountTicketsParams{Now: now, AssigneeMode: mode, Me: ids.UUID(me), OpenerType: opener})
	if err != nil {
		return adminapi.TicketList{}, err
	}
	out := adminapi.TicketList{
		Items: make([]adminapi.TicketSummary, 0, len(rows)),
		Counts: adminapi.TicketCounts{
			Open: int(counts.Open), Waiting: int(counts.Waiting), Resolved: int(counts.Resolved),
			Closed: int(counts.Closed), Overdue: int(counts.Overdue),
		},
		ServerNow: now,
	}
	if len(rows) > int(lim) {
		rows = rows[:lim]
		last := rows[len(rows)-1]
		key := last.LastMessageAt
		if status == string(adminapi.TicketStatusOpen) && last.SlaDueAt != nil {
			key = *last.SlaDueAt
		}
		out.NextCursor = ptr(encodeCursor(key, last.ID))
	}
	for _, r := range rows {
		out.Items = append(out.Items, toSummary(r, now))
	}
	return out, nil
}

func toSummary(r summaryRow, now time.Time) adminapi.TicketSummary {
	t := adminapi.TicketSummary{
		Id: idString(r.ID), Number: deref(r.DisplayNumber), Subject: r.Subject,
		Category: adminapi.TicketCategory(r.Category), Status: adminapi.TicketStatus(r.Status),
		Priority: adminapi.TicketPriority(r.Priority), OpenerType: adminapi.TicketOpenerType(r.OpenerType),
		Opener:     adminapi.TicketPerson{Id: idString(r.OpenerUserID), Name: r.OpenerName},
		BusinessId: optID(r.BusinessID), BusinessName: r.BusinessName,
		StoreId: optID(r.StoreID), StoreName: r.StoreName,
		OrderId: optID(r.OrderID), OrderNumber: r.OrderNumber,
		SlaDueAt: utcPtr(r.SlaDueAt), LastMessageAt: r.LastMessageAt.UTC(), CreatedAt: r.CreatedAt.UTC(),
	}
	if r.AssigneeID.Valid {
		t.Assignee = &adminapi.TicketPerson{Id: idString(r.AssigneeID), Name: r.AssigneeName}
	}
	t.Overdue = r.Status == "open" && r.SlaDueAt != nil && r.SlaDueAt.Before(now)
	return t
}

// detail reads the whole ticket as the console shows it, notes included.
func (s *Service) detail(ctx context.Context, ticket pgtype.UUID) (adminapi.TicketDetail, error) {
	q := s.q(ctx)
	t, err := q.GetTicket(ctx, ticket)
	if db.IsNoRows(err) {
		return adminapi.TicketDetail{}, notFound()
	}
	if err != nil {
		return adminapi.TicketDetail{}, err
	}
	msgs, err := q.ListMessages(ctx, supportdb.ListMessagesParams{TicketID: ticket, IncludeNotes: true})
	if err != nil {
		return adminapi.TicketDetail{}, err
	}
	files, err := s.filesByMessage(ctx, ticket)
	if err != nil {
		return adminapi.TicketDetail{}, err
	}
	agents, err := q.ListAgents(ctx)
	if err != nil {
		return adminapi.TicketDetail{}, err
	}
	assist, err := s.settings.Bool(ctx, settings.AiTriageEnabled)
	if err != nil {
		return adminapi.TicketDetail{}, err
	}
	now := s.now()
	out := adminapi.TicketDetail{
		Ticket: toSummary(summaryRow{
			ID: t.ID, DisplayNumber: t.DisplayNumber, Subject: t.Subject, Category: t.Category, Status: t.Status,
			Priority: t.Priority, OpenerType: t.OpenerType, OpenerUserID: t.OpenerUserID, OpenerName: t.OpenerName,
			BusinessID: t.BusinessID, BusinessName: t.BusinessName, StoreID: t.StoreID, StoreName: t.StoreName,
			OrderID: t.OrderID, OrderNumber: t.OrderNumber, AssigneeID: t.AssigneeID, AssigneeName: t.AssigneeName,
			SlaDueAt: t.SlaDueAt, LastMessageAt: t.LastMessageAt, CreatedAt: t.CreatedAt,
		}, now),
		FirstResponseAt: utcPtr(t.FirstResponseAt), ResolvedAt: utcPtr(t.ResolvedAt), ClosedAt: utcPtr(t.ClosedAt),
		CloseReason:   t.CloseReason,
		Messages:      make([]adminapi.TicketMessage, 0, len(msgs)),
		Agents:        make([]adminapi.TicketPerson, 0, len(agents)),
		AssistEnabled: assist,
		ServerNow:     now,
	}
	if t.OrderID.Valid {
		out.Order = &adminapi.TicketOrderLink{Id: idString(t.OrderID), Number: deref(t.OrderNumber), Status: deref(t.OrderStatus)}
	}
	for _, m := range msgs {
		atts := make([]adminapi.TicketAttachment, 0, len(files[m.ID]))
		for _, f := range files[m.ID] {
			atts = append(atts, adminapi.TicketAttachment{
				Id: f.id.String(), FileName: f.fileName, ContentType: adminapi.TicketAttachmentContentType(f.contentType),
				SizeBytes: f.size, Url: f.url,
			})
		}
		out.Messages = append(out.Messages, adminapi.TicketMessage{
			Id: idString(m.ID), Kind: adminapi.TicketMessageKind(m.Kind), AuthorType: adminapi.TicketMessageAuthorType(m.AuthorType),
			Author: adminapi.TicketPerson{Id: idString(m.AuthorUserID), Name: m.AuthorName},
			Body:   m.Body, CreatedAt: m.CreatedAt.UTC(), Attachments: atts,
		})
	}
	for _, a := range agents {
		out.Agents = append(out.Agents, adminapi.TicketPerson{Id: idString(a.UserID), Name: a.Name})
	}
	return out, nil
}

func (s *Service) getAdmin(ctx context.Context, ticket ids.ID) (adminapi.TicketDetail, error) {
	if err := s.ready(ctx); err != nil {
		return adminapi.TicketDetail{}, err
	}
	return s.detail(ctx, ids.UUID(ticket))
}

// write locks the ticket, runs fn and returns the ticket as it now is, all in
// one transaction.
func (s *Service) write(ctx context.Context, ticket ids.ID, fn func(ctx context.Context, t supportdb.LockTicketRow, now time.Time) error) (adminapi.TicketDetail, error) {
	var out adminapi.TicketDetail
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		t, err := s.q(ctx).LockTicket(ctx, ids.UUID(ticket))
		if db.IsNoRows(err) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if err := fn(ctx, t, s.now()); err != nil {
			return err
		}
		out, err = s.detail(ctx, t.ID)
		return err
	})
	return out, err
}

func (s *Service) assign(ctx context.Context, actor, ticket ids.ID, assignee *string) (adminapi.TicketDetail, error) {
	if err := s.auditReady(ctx); err != nil {
		return adminapi.TicketDetail{}, err
	}
	var to pgtype.UUID
	if assignee != nil {
		id, err := ids.Parse(*assignee)
		if err != nil {
			return adminapi.TicketDetail{}, fieldErr("body.assigneeId", "invalid")
		}
		to = ids.UUID(id)
	}
	return s.write(ctx, ticket, func(ctx context.Context, t supportdb.LockTicketRow, _ time.Time) error {
		if t.Status == "closed" {
			return invalidTransition()
		}
		q := s.q(ctx)
		if to.Valid {
			if _, err := q.GetAgent(ctx, to); db.IsNoRows(err) {
				return fieldErr("body.assigneeId", "must be an active support, admin or super_admin user")
			} else if err != nil {
				return err
			}
		}
		if to == t.AssigneeID {
			return nil // nothing changes, nothing to audit
		}
		if err := q.SetTicketAssignee(ctx, supportdb.SetTicketAssigneeParams{AssigneeID: to, ID: t.ID}); err != nil {
			return err
		}
		_, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "support.ticket_assign",
			SubjectType: "support_ticket", SubjectID: ticket.String(),
			Before: map[string]any{"assigneeId": optID(t.AssigneeID)}, After: map[string]any{"assigneeId": optID(to)},
		})
		return err
	})
}

// setPriority changes the ticket's priority (D-226). On an open ticket the due
// time becomes the timer's start plus the new priority's minutes, read from
// the setting now; a ticket that is not open has no timer and keeps none.
func (s *Service) setPriority(ctx context.Context, actor, ticket ids.ID, priority adminapi.TicketPriority) (adminapi.TicketDetail, error) {
	if err := s.auditReady(ctx); err != nil {
		return adminapi.TicketDetail{}, err
	}
	if _, ok := slaKeys[string(priority)]; !ok {
		return adminapi.TicketDetail{}, fieldErr("body.priority", "normal, high or urgent")
	}
	return s.write(ctx, ticket, func(ctx context.Context, t supportdb.LockTicketRow, _ time.Time) error {
		if t.Status == "closed" {
			return invalidTransition()
		}
		if t.Priority == string(priority) {
			return nil // nothing changes, nothing to audit
		}
		var due *time.Time
		if t.Status == "open" && t.SlaStartedAt != nil {
			d, err := s.slaDue(ctx, string(priority), *t.SlaStartedAt)
			if err != nil {
				return err
			}
			due = &d
		}
		if err := s.q(ctx).SetTicketPriority(ctx, supportdb.SetTicketPriorityParams{
			Priority: string(priority), SlaDueAt: due, ID: t.ID,
		}); err != nil {
			return err
		}
		_, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "support.ticket_priority",
			SubjectType: "support_ticket", SubjectID: ticket.String(),
			Before: map[string]any{"priority": t.Priority, "slaDueAt": utcPtr(t.SlaDueAt)},
			After:  map[string]any{"priority": string(priority), "slaDueAt": utcPtr(due)},
		})
		return err
	})
}

// adminMessage adds a reply or a note by an agent. A reply moves the ticket to
// waiting (the SLA stops) and assigns an unassigned ticket to the agent; a
// note changes nothing else and is the one thing a closed ticket still takes.
func (s *Service) adminMessage(ctx context.Context, actor, ticket ids.ID, kind string, body adminapi.TicketMessageRequest) (adminapi.TicketDetail, error) {
	ready := s.ready
	if kind == "reply" {
		ready = s.auditReady // a reply on a resolved ticket reopens it, audited
	}
	if err := ready(ctx); err != nil {
		return adminapi.TicketDetail{}, err
	}
	text, err := cleanBody(body.Body)
	if err != nil {
		return adminapi.TicketDetail{}, err
	}
	uploads, err := attachmentIDs(body.AttachmentIds)
	if err != nil {
		return adminapi.TicketDetail{}, err
	}
	return s.write(ctx, ticket, func(ctx context.Context, t supportdb.LockTicketRow, now time.Time) error {
		if kind == "reply" && t.Status == "closed" {
			return invalidTransition()
		}
		q := s.q(ctx)
		msg := ids.UUID(s.ids.New(now))
		if err := q.InsertMessage(ctx, supportdb.InsertMessageParams{
			ID: msg, TicketID: t.ID, AuthorType: "admin", AuthorUserID: ids.UUID(actor), Kind: kind, Body: text, Now: now,
		}); err != nil {
			return err
		}
		if err := s.claim(ctx, t.ID, msg, uploads, actor, pgtype.UUID{}, t.ID); err != nil {
			return err
		}
		if kind == "note" {
			return q.TouchTicket(ctx, t.ID)
		}
		if err := q.TicketAfterAgentReply(ctx, supportdb.TicketAfterAgentReplyParams{Now: now, AssigneeID: ids.UUID(actor), ID: t.ID}); err != nil {
			return err
		}
		if t.Status != "resolved" {
			return nil
		}
		// A reply on a resolved ticket reopens it for the opener: waiting, the
		// resolve reason cleared.
		_, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "support.ticket_reopen",
			SubjectType: "support_ticket", SubjectID: ticket.String(),
			Before: map[string]any{"status": "resolved"}, After: map[string]any{"status": "waiting"},
		})
		return err
	})
}

func (s *Service) closeTicket(ctx context.Context, actor, ticket ids.ID, outcome adminapi.TicketCloseRequestOutcome, reason string) (adminapi.TicketDetail, error) {
	if err := s.auditReady(ctx); err != nil {
		return adminapi.TicketDetail{}, err
	}
	reason, err := cleanReason(reason)
	if err != nil {
		return adminapi.TicketDetail{}, err
	}
	return s.write(ctx, ticket, func(ctx context.Context, t supportdb.LockTicketRow, now time.Time) error {
		q := s.q(ctx)
		var action string
		var err error
		switch outcome {
		case adminapi.TicketCloseRequestOutcomeResolved:
			if t.Status == "resolved" || t.Status == "closed" {
				return invalidTransition()
			}
			action = "support.ticket_resolve"
			err = q.ResolveTicket(ctx, supportdb.ResolveTicketParams{Now: &now, Reason: &reason, ID: t.ID})
		case adminapi.TicketCloseRequestOutcomeClosed:
			if t.Status == "closed" {
				return invalidTransition()
			}
			action = "support.ticket_close"
			err = q.CloseTicket(ctx, supportdb.CloseTicketParams{Now: &now, Reason: &reason, ID: t.ID})
		default:
			return fieldErr("body.outcome", "resolved or closed")
		}
		if err != nil {
			return err
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: action,
			SubjectType: "support_ticket", SubjectID: ticket.String(),
			Before: map[string]any{"status": t.Status}, After: map[string]any{"status": string(outcome), "reason": reason},
		})
		return err
	})
}

func (s *Service) adminUpload(ctx context.Context, actor, ticket ids.ID, body adminapi.TicketAttachmentCreate) (adminapi.TicketAttachmentUpload, error) {
	if err := s.ready(ctx); err != nil {
		return adminapi.TicketAttachmentUpload{}, err
	}
	if err := s.storageReady(); err != nil {
		return adminapi.TicketAttachmentUpload{}, err
	}
	var u upload
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		t, err := s.q(ctx).LockTicket(ctx, ids.UUID(ticket))
		if db.IsNoRows(err) {
			return notFound()
		} else if err != nil {
			return err
		}
		u, err = s.newUpload(ctx, actor, pgtype.UUID{}, t.ID, prefixAdmin, body.FileName, string(body.ContentType), body.SizeBytes)
		return err
	})
	if err != nil {
		return adminapi.TicketAttachmentUpload{}, err
	}
	ct := adminapi.TicketAttachmentContentType(u.contentType)
	return adminapi.TicketAttachmentUpload{
		Attachment: adminapi.TicketAttachment{Id: u.id.String(), FileName: u.fileName, ContentType: ct, SizeBytes: int(u.size)},
		Upload: adminapi.TicketUploadTarget{
			Url: u.url, Method: adminapi.PUT, ExpiresAt: u.expiresAt,
			Headers: adminapi.TicketUploadHeaders{ContentType: ct, ContentLength: itoa(u.size)},
		},
	}, nil
}

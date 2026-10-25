package support

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	meapi "github.com/todayz/backend/gen/api/support"
	"github.com/todayz/backend/gen/sqlc/supportdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
)

// The customer side (C-19, 01 §13, D-226): the customer's own tickets,
// opening one (optionally on one of their orders), the thread without notes,
// replying, and uploads. Every read is scoped to tickets the caller opened as
// a customer, so anyone else's ticket, and every business ticket, is 404.

// meRow is one ticket as the customer sees it; GetCustomerTicketRow has the same fields.
type meRow = supportdb.ListCustomerTicketsRow

func toMyTicket(r meRow) meapi.MyTicket {
	return meapi.MyTicket{
		Id: idString(r.ID), Number: deref(r.DisplayNumber), Subject: r.Subject,
		Category: meapi.MyTicketCategory(r.Category), Status: meapi.MyTicketStatus(r.Status),
		StoreName: r.StoreName, OrderId: optID(r.OrderID), OrderNumber: r.OrderNumber,
		LastMessageAt: r.LastMessageAt.UTC(), CreatedAt: r.CreatedAt.UTC(),
	}
}

func (s *Service) listMe(ctx context.Context, user ids.ID, limit *int, cursor *string) (meapi.MyTicketList, error) {
	if err := s.ready(ctx); err != nil {
		return meapi.MyTicketList{}, err
	}
	lim, at, before, err := pageArgs(limit, cursor)
	if err != nil {
		return meapi.MyTicketList{}, err
	}
	rows, err := s.q(ctx).ListCustomerTickets(ctx, supportdb.ListCustomerTicketsParams{
		UserID: ids.UUID(user), BeforeAt: at, BeforeID: before, Lim: lim + 1,
	})
	if err != nil {
		return meapi.MyTicketList{}, err
	}
	out := meapi.MyTicketList{Items: make([]meapi.MyTicket, 0, len(rows))}
	if len(rows) > int(lim) {
		rows = rows[:lim]
		last := rows[len(rows)-1]
		out.NextCursor = ptr(encodeCursor(last.LastMessageAt, last.ID))
	}
	for _, r := range rows {
		out.Items = append(out.Items, toMyTicket(r))
	}
	return out, nil
}

// meThread reads the customer's ticket and its replies; notes never leave
// this package through here.
func (s *Service) meThread(ctx context.Context, user, ticket ids.ID) (meapi.MyTicketThread, error) {
	q := s.q(ctx)
	t, err := q.GetCustomerTicket(ctx, supportdb.GetCustomerTicketParams{ID: ids.UUID(ticket), UserID: ids.UUID(user)})
	if db.IsNoRows(err) {
		return meapi.MyTicketThread{}, notFound()
	}
	if err != nil {
		return meapi.MyTicketThread{}, err
	}
	msgs, err := q.ListMessages(ctx, supportdb.ListMessagesParams{TicketID: t.ID, IncludeNotes: false})
	if err != nil {
		return meapi.MyTicketThread{}, err
	}
	files, err := s.filesByMessage(ctx, t.ID)
	if err != nil {
		return meapi.MyTicketThread{}, err
	}
	out := meapi.MyTicketThread{
		Ticket: toMyTicket(meRow(t)), Messages: make([]meapi.MyTicketMessage, 0, len(msgs)), ServerNow: s.now(),
	}
	for _, m := range msgs {
		if m.Kind != "reply" {
			continue // the query already leaves notes out; this is the second lock
		}
		atts := make([]meapi.MyTicketAttachment, 0, len(files[m.ID]))
		for _, f := range files[m.ID] {
			atts = append(atts, meapi.MyTicketAttachment{
				Id: f.id.String(), FileName: f.fileName, ContentType: meapi.MyTicketAttachmentContentType(f.contentType),
				SizeBytes: f.size, Url: f.url,
			})
		}
		from := meapi.Customer
		if m.AuthorType == "admin" {
			from = meapi.Support // the agent's name stays inside TodayZ
		}
		out.Messages = append(out.Messages, meapi.MyTicketMessage{
			Id: idString(m.ID), From: from, Body: m.Body, CreatedAt: m.CreatedAt.UTC(), Attachments: atts,
		})
	}
	return out, nil
}

func (s *Service) getMeThread(ctx context.Context, user, ticket ids.ID) (meapi.MyTicketThread, error) {
	if err := s.ready(ctx); err != nil {
		return meapi.MyTicketThread{}, err
	}
	return s.meThread(ctx, user, ticket)
}

// openMe opens a customer ticket at normal priority with its first message.
// An order on it must be the caller's own; its store goes on the ticket.
func (s *Service) openMe(ctx context.Context, user ids.ID, body meapi.MyTicketCreate) (meapi.MyTicketThread, error) {
	if err := s.ready(ctx); err != nil {
		return meapi.MyTicketThread{}, err
	}
	subject, err := cleanSubject(body.Subject)
	if err != nil {
		return meapi.MyTicketThread{}, err
	}
	text, err := cleanBody(body.Body)
	if err != nil {
		return meapi.MyTicketThread{}, err
	}
	uploads, err := attachmentIDs(body.AttachmentIds)
	if err != nil {
		return meapi.MyTicketThread{}, err
	}
	var order pgtype.UUID
	if body.OrderId != nil {
		id, err := ids.Parse(*body.OrderId)
		if err != nil {
			return meapi.MyTicketThread{}, fieldErr("body.orderId", "invalid")
		}
		order = ids.UUID(id)
	}
	var out meapi.MyTicketThread
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		var store pgtype.UUID
		if order.Valid {
			o, err := q.OrderOfCustomer(ctx, supportdb.OrderOfCustomerParams{ID: order, CustomerID: ids.UUID(user)})
			if db.IsNoRows(err) {
				return fieldErr("body.orderId", "must be one of your orders")
			}
			if err != nil {
				return err
			}
			store = o.StoreID
		}
		now := s.now()
		due, err := s.slaDue(ctx, "normal", now)
		if err != nil {
			return err
		}
		ticket := s.ids.New(now)
		if _, err := q.InsertTicket(ctx, supportdb.InsertTicketParams{
			ID: ids.UUID(ticket), OpenerType: "customer", OpenerUserID: ids.UUID(user),
			StoreID: store, OrderID: order, Category: string(body.Category), Subject: subject,
			Priority: "normal", SlaDueAt: &due, Now: now,
		}); err != nil {
			return err
		}
		msg := ids.UUID(s.ids.New(now))
		if err := q.InsertMessage(ctx, supportdb.InsertMessageParams{
			ID: msg, TicketID: ids.UUID(ticket), AuthorType: "customer", AuthorUserID: ids.UUID(user), Kind: "reply", Body: text, Now: now,
		}); err != nil {
			return err
		}
		if err := s.claim(ctx, ids.UUID(ticket), msg, uploads, user, pgtype.UUID{}); err != nil {
			return err
		}
		out, err = s.meThread(ctx, user, ticket)
		return err
	})
	return out, err
}

// replyMe adds the customer's reply. It reopens a waiting or resolved ticket
// with a new timer; on a ticket that is already open the timer stays.
func (s *Service) replyMe(ctx context.Context, user, ticket ids.ID, body meapi.MyTicketMessageCreate) (meapi.MyTicketThread, error) {
	if err := s.ready(ctx); err != nil {
		return meapi.MyTicketThread{}, err
	}
	text, err := cleanBody(body.Body)
	if err != nil {
		return meapi.MyTicketThread{}, err
	}
	uploads, err := attachmentIDs(body.AttachmentIds)
	if err != nil {
		return meapi.MyTicketThread{}, err
	}
	var out meapi.MyTicketThread
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		t, err := q.LockTicket(ctx, ids.UUID(ticket))
		if db.IsNoRows(err) || (err == nil && (t.OpenerType != "customer" || t.OpenerUserID != ids.UUID(user))) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if t.Status == "closed" {
			return invalidTransition()
		}
		now := s.now()
		msg := ids.UUID(s.ids.New(now))
		if err := q.InsertMessage(ctx, supportdb.InsertMessageParams{
			ID: msg, TicketID: t.ID, AuthorType: "customer", AuthorUserID: ids.UUID(user), Kind: "reply", Body: text, Now: now,
		}); err != nil {
			return err
		}
		if err := s.claim(ctx, t.ID, msg, uploads, user, pgtype.UUID{}); err != nil {
			return err
		}
		start, due, err := s.openerTimer(ctx, t, now)
		if err != nil {
			return err
		}
		if err := q.TicketAfterOpenerReply(ctx, supportdb.TicketAfterOpenerReplyParams{
			SlaStartedAt: start, SlaDueAt: due, Now: now, ID: t.ID,
		}); err != nil {
			return err
		}
		out, err = s.meThread(ctx, user, ticket)
		return err
	})
	return out, err
}

func (s *Service) meUpload(ctx context.Context, user ids.ID, body meapi.MyTicketAttachmentCreate) (meapi.MyTicketAttachmentUpload, error) {
	if err := s.ready(ctx); err != nil {
		return meapi.MyTicketAttachmentUpload{}, err
	}
	if err := s.storageReady(); err != nil {
		return meapi.MyTicketAttachmentUpload{}, err
	}
	u, err := s.newUpload(ctx, user, pgtype.UUID{}, prefixCustomers, body.FileName, string(body.ContentType), body.SizeBytes)
	if err != nil {
		return meapi.MyTicketAttachmentUpload{}, err
	}
	ct := meapi.MyTicketAttachmentContentType(u.contentType)
	return meapi.MyTicketAttachmentUpload{
		Attachment: meapi.MyTicketAttachment{Id: u.id.String(), FileName: u.fileName, ContentType: ct, SizeBytes: int(u.size)},
		Upload: meapi.MyTicketUploadTarget{
			Url: u.url, Method: meapi.PUT, ExpiresAt: u.expiresAt,
			Headers: meapi.MyTicketUploadHeaders{ContentType: ct, ContentLength: itoa(u.size)},
		},
	}, nil
}

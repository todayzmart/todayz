package support

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	bizapi "github.com/todayz/backend/gen/api/biz-support"
	"github.com/todayz/backend/gen/sqlc/supportdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
)

// The business side (B-15, 01 §13): the business's tickets, opening one, the
// thread without notes, replying, and uploads. authz has already checked that
// the caller is an owner or manager of {bid}; every read here is also scoped
// to it, so another business's ticket is 404.

// bizRow is one ticket as the business sees it; GetBusinessTicketRow has the same fields.
type bizRow = supportdb.ListBusinessTicketsRow

func toBizTicket(r bizRow) bizapi.BizTicket {
	return bizapi.BizTicket{
		Id: idString(r.ID), Number: deref(r.DisplayNumber), Subject: r.Subject,
		Category: bizapi.BizTicketCategory(r.Category), Status: bizapi.BizTicketStatus(r.Status),
		StoreId: optID(r.StoreID), StoreName: r.StoreName, OrderId: optID(r.OrderID), OrderNumber: r.OrderNumber,
		LastMessageAt: r.LastMessageAt.UTC(), CreatedAt: r.CreatedAt.UTC(),
	}
}

func (s *Service) listBiz(ctx context.Context, bid ids.ID, limit *int, cursor *string) (bizapi.BizTicketList, error) {
	if err := s.ready(ctx); err != nil {
		return bizapi.BizTicketList{}, err
	}
	lim, at, before, err := pageArgs(limit, cursor)
	if err != nil {
		return bizapi.BizTicketList{}, err
	}
	rows, err := s.q(ctx).ListBusinessTickets(ctx, supportdb.ListBusinessTicketsParams{
		BusinessID: ids.UUID(bid), BeforeAt: at, BeforeID: before, Lim: lim + 1,
	})
	if err != nil {
		return bizapi.BizTicketList{}, err
	}
	out := bizapi.BizTicketList{Items: make([]bizapi.BizTicket, 0, len(rows))}
	if len(rows) > int(lim) {
		rows = rows[:lim]
		last := rows[len(rows)-1]
		out.NextCursor = ptr(encodeCursor(last.LastMessageAt, last.ID))
	}
	for _, r := range rows {
		out.Items = append(out.Items, toBizTicket(r))
	}
	return out, nil
}

// thread reads the ticket and its replies; notes never leave this package
// through here.
func (s *Service) thread(ctx context.Context, bid, ticket ids.ID) (bizapi.BizTicketThread, error) {
	q := s.q(ctx)
	t, err := q.GetBusinessTicket(ctx, supportdb.GetBusinessTicketParams{ID: ids.UUID(ticket), BusinessID: ids.UUID(bid)})
	if db.IsNoRows(err) {
		return bizapi.BizTicketThread{}, notFound()
	}
	if err != nil {
		return bizapi.BizTicketThread{}, err
	}
	msgs, err := q.ListMessages(ctx, supportdb.ListMessagesParams{TicketID: t.ID, IncludeNotes: false})
	if err != nil {
		return bizapi.BizTicketThread{}, err
	}
	files, err := s.filesByMessage(ctx, t.ID)
	if err != nil {
		return bizapi.BizTicketThread{}, err
	}
	out := bizapi.BizTicketThread{
		Ticket: toBizTicket(bizRow(t)), Messages: make([]bizapi.BizTicketMessage, 0, len(msgs)), ServerNow: s.now(),
	}
	for _, m := range msgs {
		if m.Kind != "reply" {
			continue // the query already leaves notes out; this is the second lock
		}
		atts := make([]bizapi.BizTicketAttachment, 0, len(files[m.ID]))
		for _, f := range files[m.ID] {
			atts = append(atts, bizapi.BizTicketAttachment{
				Id: f.id.String(), FileName: f.fileName, ContentType: bizapi.BizAttachmentContentType(f.contentType),
				SizeBytes: f.size, Url: f.url,
			})
		}
		msg := bizapi.BizTicketMessage{Id: idString(m.ID), Body: m.Body, CreatedAt: m.CreatedAt.UTC(), Attachments: atts}
		if m.AuthorType == "admin" {
			msg.From = bizapi.Support // the agent's name stays inside TodayZ
		} else {
			msg.From, msg.AuthorName = bizapi.Business, m.AuthorName
		}
		out.Messages = append(out.Messages, msg)
	}
	return out, nil
}

func (s *Service) getThread(ctx context.Context, bid, ticket ids.ID) (bizapi.BizTicketThread, error) {
	if err := s.ready(ctx); err != nil {
		return bizapi.BizTicketThread{}, err
	}
	return s.thread(ctx, bid, ticket)
}

// openBiz opens a business ticket at normal priority with its first message.
func (s *Service) openBiz(ctx context.Context, user, bid ids.ID, body bizapi.BizTicketCreate) (bizapi.BizTicketThread, error) {
	if err := s.ready(ctx); err != nil {
		return bizapi.BizTicketThread{}, err
	}
	subject, err := cleanSubject(body.Subject)
	if err != nil {
		return bizapi.BizTicketThread{}, err
	}
	text, err := cleanBody(body.Body)
	if err != nil {
		return bizapi.BizTicketThread{}, err
	}
	uploads, err := attachmentIDs(body.AttachmentIds)
	if err != nil {
		return bizapi.BizTicketThread{}, err
	}
	var store, order pgtype.UUID
	if body.StoreId != nil {
		id, err := ids.Parse(*body.StoreId)
		if err != nil {
			return bizapi.BizTicketThread{}, fieldErr("body.storeId", "invalid")
		}
		store = ids.UUID(id)
	}
	if body.OrderId != nil {
		id, err := ids.Parse(*body.OrderId)
		if err != nil {
			return bizapi.BizTicketThread{}, fieldErr("body.orderId", "invalid")
		}
		order = ids.UUID(id)
	}
	business := ids.UUID(bid)
	var ticket ids.ID
	var out bizapi.BizTicketThread
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if store.Valid {
			if _, err := q.StoreOfBusiness(ctx, supportdb.StoreOfBusinessParams{ID: store, BusinessID: business}); db.IsNoRows(err) {
				return fieldErr("body.storeId", "must be a store of this business")
			} else if err != nil {
				return err
			}
		}
		if order.Valid {
			o, err := q.OrderOfBusiness(ctx, supportdb.OrderOfBusinessParams{ID: order, BusinessID: business})
			if db.IsNoRows(err) {
				return fieldErr("body.orderId", "must be an order of this business")
			}
			if err != nil {
				return err
			}
			if store.Valid && store != o.StoreID {
				return fieldErr("body.orderId", "must be an order of this store")
			}
			store = o.StoreID
		}
		now := s.now()
		if err := s.openLimit(ctx, user, now); err != nil {
			return err
		}
		due, err := s.slaDue(ctx, "normal", now)
		if err != nil {
			return err
		}
		ticket = s.ids.New(now)
		if _, err := q.InsertTicket(ctx, supportdb.InsertTicketParams{
			ID: ids.UUID(ticket), OpenerType: "business", OpenerUserID: ids.UUID(user), BusinessID: business,
			StoreID: store, OrderID: order, Category: string(body.Category), Subject: subject,
			Priority: "normal", SlaDueAt: &due, Now: now,
		}); err != nil {
			return err
		}
		msg := ids.UUID(s.ids.New(now))
		if err := q.InsertMessage(ctx, supportdb.InsertMessageParams{
			ID: msg, TicketID: ids.UUID(ticket), AuthorType: "business", AuthorUserID: ids.UUID(user), Kind: "reply", Body: text, Now: now,
		}); err != nil {
			return err
		}
		if err := s.claim(ctx, ids.UUID(ticket), msg, uploads, user, business, pgtype.UUID{}); err != nil {
			return err
		}
		out, err = s.thread(ctx, bid, ticket)
		return err
	})
	return out, err
}

// replyBiz adds the business's reply. It reopens a waiting or resolved ticket
// with a new timer; on a ticket that is already open the timer stays.
func (s *Service) replyBiz(ctx context.Context, user, bid, ticket ids.ID, body bizapi.BizTicketMessageCreate) (bizapi.BizTicketThread, error) {
	if err := s.ready(ctx); err != nil {
		return bizapi.BizTicketThread{}, err
	}
	text, err := cleanBody(body.Body)
	if err != nil {
		return bizapi.BizTicketThread{}, err
	}
	uploads, err := attachmentIDs(body.AttachmentIds)
	if err != nil {
		return bizapi.BizTicketThread{}, err
	}
	business := ids.UUID(bid)
	var out bizapi.BizTicketThread
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		t, err := q.LockTicket(ctx, ids.UUID(ticket))
		if db.IsNoRows(err) || (err == nil && t.BusinessID != business) {
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
			ID: msg, TicketID: t.ID, AuthorType: "business", AuthorUserID: ids.UUID(user), Kind: "reply", Body: text, Now: now,
		}); err != nil {
			return err
		}
		if err := s.claim(ctx, t.ID, msg, uploads, user, business, pgtype.UUID{}); err != nil {
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
		out, err = s.thread(ctx, bid, ticket)
		return err
	})
	return out, err
}

func (s *Service) bizUpload(ctx context.Context, user, bid ids.ID, body bizapi.BizAttachmentCreate) (bizapi.BizAttachmentUpload, error) {
	if err := s.ready(ctx); err != nil {
		return bizapi.BizAttachmentUpload{}, err
	}
	if err := s.storageReady(); err != nil {
		return bizapi.BizAttachmentUpload{}, err
	}
	var u upload
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		var err error
		u, err = s.newUpload(ctx, user, ids.UUID(bid), pgtype.UUID{}, "", body.FileName, string(body.ContentType), body.SizeBytes)
		return err
	})
	if err != nil {
		return bizapi.BizAttachmentUpload{}, err
	}
	ct := bizapi.BizAttachmentContentType(u.contentType)
	return bizapi.BizAttachmentUpload{
		Attachment: bizapi.BizTicketAttachment{Id: u.id.String(), FileName: u.fileName, ContentType: ct, SizeBytes: int(u.size)},
		Upload: bizapi.BizUploadTarget{
			Url: u.url, Method: bizapi.PUT, ExpiresAt: u.expiresAt,
			Headers: bizapi.BizUploadHeaders{ContentType: ct, ContentLength: itoa(u.size)},
		},
	}, nil
}

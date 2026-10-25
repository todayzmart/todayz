package support

import (
	"context"

	"github.com/go-chi/chi/v5"

	adminapi "github.com/todayz/backend/gen/api/admin-support"
	bizapi "github.com/todayz/backend/gen/api/biz-support"
	meapi "github.com/todayz/backend/gen/api/support"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/ids"
)

// noStore keeps a presigned URL out of every cache.
const noStore = "no-store"

// Ops maps the biz-support operations (B-15) to their authz actions, keyed by
// the generated strict server's method names.
var Ops = map[string]string{
	"ListBizTickets":            policy.SupportTickets,
	"CreateBizTicket":           policy.SupportTickets,
	"GetBizTicketThread":        policy.SupportTickets,
	"PostBizTicketMessage":      policy.SupportTickets,
	"CreateBizTicketAttachment": policy.SupportTickets,
}

// MeOps maps the customer's support operations (C-19) to their authz actions.
var MeOps = map[string]string{
	"ListMyTickets":            policy.MeSupportTickets,
	"CreateMyTicket":           policy.MeSupportTickets,
	"GetMyTicketThread":        policy.MeSupportTickets,
	"PostMyTicketMessage":      policy.MeSupportTickets,
	"CreateMyTicketAttachment": policy.MeSupportTickets,
}

// AdminOps maps the admin-support operations (A-14) to their authz actions.
var AdminOps = map[string]string{
	"ListAdminTickets":            policy.AdminSupportTickets,
	"GetAdminTicket":              policy.AdminSupportTickets,
	"AssignAdminTicket":           policy.AdminSupportTickets,
	"SetAdminTicketPriority":      policy.AdminSupportTickets,
	"ReplyAdminTicket":            policy.AdminSupportTickets,
	"AddAdminTicketNote":          policy.AdminSupportTickets,
	"CloseAdminTicket":            policy.AdminSupportTickets,
	"CreateAdminTicketAttachment": policy.AdminSupportTickets,
}

// Register mounts the customer, business and admin support routes behind slot 15.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	meapi.Register(r, meHandlers{s},
		meapi.StrictMiddlewareFunc(authz.Strict[meapi.StrictHandlerFunc](az, MeOps)))
	bizapi.Register(r, bizHandlers{s},
		bizapi.StrictMiddlewareFunc(authz.Strict[bizapi.StrictHandlerFunc](az, Ops)))
	adminapi.Register(r, adminHandlers{s},
		adminapi.StrictMiddlewareFunc(authz.Strict[adminapi.StrictHandlerFunc](az, AdminOps)))
}

type meHandlers struct{ s *Service }

var _ meapi.StrictServerInterface = meHandlers{}

// ListMyTickets implements GET /v1/me/support/tickets.
func (h meHandlers) ListMyTickets(ctx context.Context, req meapi.ListMyTicketsRequestObject) (meapi.ListMyTicketsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	l, err := h.s.listMe(ctx, p.UserID, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return meapi.ListMyTickets200JSONResponse(l), nil
}

// CreateMyTicket implements POST /v1/me/support/tickets.
func (h meHandlers) CreateMyTicket(ctx context.Context, req meapi.CreateMyTicketRequestObject) (meapi.CreateMyTicketResponseObject, error) {
	user, err := meCall(ctx, req.Body != nil)
	if err != nil {
		return nil, err
	}
	t, err := h.s.openMe(ctx, user, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return meapi.CreateMyTicket201JSONResponse(t), nil
}

// GetMyTicketThread implements GET /v1/me/support/tickets/{ticketId}/messages.
func (h meHandlers) GetMyTicketThread(ctx context.Context, req meapi.GetMyTicketThreadRequestObject) (meapi.GetMyTicketThreadResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	tid, err := pathID(req.TicketId)
	if err != nil {
		return nil, err
	}
	t, err := h.s.getMeThread(ctx, p.UserID, tid)
	if err != nil {
		return nil, asProblem(err)
	}
	return meapi.GetMyTicketThread200JSONResponse(t), nil
}

// PostMyTicketMessage implements POST /v1/me/support/tickets/{ticketId}/messages.
func (h meHandlers) PostMyTicketMessage(ctx context.Context, req meapi.PostMyTicketMessageRequestObject) (meapi.PostMyTicketMessageResponseObject, error) {
	user, err := meCall(ctx, req.Body != nil)
	if err != nil {
		return nil, err
	}
	tid, err := pathID(req.TicketId)
	if err != nil {
		return nil, err
	}
	t, err := h.s.replyMe(ctx, user, tid, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return meapi.PostMyTicketMessage201JSONResponse(t), nil
}

// CreateMyTicketAttachment implements POST /v1/me/support/attachments.
func (h meHandlers) CreateMyTicketAttachment(ctx context.Context, req meapi.CreateMyTicketAttachmentRequestObject) (meapi.CreateMyTicketAttachmentResponseObject, error) {
	user, err := meCall(ctx, req.Body != nil)
	if err != nil {
		return nil, err
	}
	u, err := h.s.meUpload(ctx, user, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return meapi.CreateMyTicketAttachment201JSONResponse{
		Body: u, Headers: meapi.CreateMyTicketAttachment201ResponseHeaders{CacheControl: ptr(noStore)},
	}, nil
}

// meCall reads the signed-in customer and checks that a body came.
func meCall(ctx context.Context, hasBody bool) (ids.ID, error) {
	p, err := principal(ctx)
	if err != nil {
		return ids.Zero, err
	}
	if !hasBody {
		return ids.Zero, fieldErr("body", "required")
	}
	return p.UserID, nil
}

type bizHandlers struct{ s *Service }

var _ bizapi.StrictServerInterface = bizHandlers{}

// ListBizTickets implements GET /v1/biz/businesses/{bid}/support/tickets.
func (h bizHandlers) ListBizTickets(ctx context.Context, req bizapi.ListBizTicketsRequestObject) (bizapi.ListBizTicketsResponseObject, error) {
	bid, err := pathID(req.Bid)
	if err != nil {
		return nil, err
	}
	l, err := h.s.listBiz(ctx, bid, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.ListBizTickets200JSONResponse(l), nil
}

// CreateBizTicket implements POST /v1/biz/businesses/{bid}/support/tickets.
func (h bizHandlers) CreateBizTicket(ctx context.Context, req bizapi.CreateBizTicketRequestObject) (bizapi.CreateBizTicketResponseObject, error) {
	user, bid, err := bizCall(ctx, req.Bid, req.Body != nil)
	if err != nil {
		return nil, err
	}
	t, err := h.s.openBiz(ctx, user, bid, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.CreateBizTicket201JSONResponse(t), nil
}

// GetBizTicketThread implements GET .../support/tickets/{ticketId}/messages.
func (h bizHandlers) GetBizTicketThread(ctx context.Context, req bizapi.GetBizTicketThreadRequestObject) (bizapi.GetBizTicketThreadResponseObject, error) {
	bid, err := pathID(req.Bid)
	if err != nil {
		return nil, err
	}
	tid, err := pathID(req.TicketId)
	if err != nil {
		return nil, err
	}
	t, err := h.s.getThread(ctx, bid, tid)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.GetBizTicketThread200JSONResponse(t), nil
}

// PostBizTicketMessage implements POST .../support/tickets/{ticketId}/messages.
func (h bizHandlers) PostBizTicketMessage(ctx context.Context, req bizapi.PostBizTicketMessageRequestObject) (bizapi.PostBizTicketMessageResponseObject, error) {
	user, bid, err := bizCall(ctx, req.Bid, req.Body != nil)
	if err != nil {
		return nil, err
	}
	tid, err := pathID(req.TicketId)
	if err != nil {
		return nil, err
	}
	t, err := h.s.replyBiz(ctx, user, bid, tid, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.PostBizTicketMessage201JSONResponse(t), nil
}

// CreateBizTicketAttachment implements POST .../support/attachments.
func (h bizHandlers) CreateBizTicketAttachment(ctx context.Context, req bizapi.CreateBizTicketAttachmentRequestObject) (bizapi.CreateBizTicketAttachmentResponseObject, error) {
	user, bid, err := bizCall(ctx, req.Bid, req.Body != nil)
	if err != nil {
		return nil, err
	}
	u, err := h.s.bizUpload(ctx, user, bid, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.CreateBizTicketAttachment201JSONResponse{
		Body: u, Headers: bizapi.CreateBizTicketAttachment201ResponseHeaders{CacheControl: ptr(noStore)},
	}, nil
}

// bizCall reads the caller, the business in the path, and checks that a body came.
func bizCall(ctx context.Context, bidParam string, hasBody bool) (user, bid ids.ID, err error) {
	p, err := principal(ctx)
	if err != nil {
		return ids.Zero, ids.Zero, err
	}
	if bid, err = pathID(bidParam); err != nil {
		return ids.Zero, ids.Zero, err
	}
	if !hasBody {
		return ids.Zero, ids.Zero, fieldErr("body", "required")
	}
	return p.UserID, bid, nil
}

type adminHandlers struct{ s *Service }

var _ adminapi.StrictServerInterface = adminHandlers{}

// ListAdminTickets implements GET /v1/admin/tickets.
func (h adminHandlers) ListAdminTickets(ctx context.Context, req adminapi.ListAdminTicketsRequestObject) (adminapi.ListAdminTicketsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	l, err := h.s.listAdmin(ctx, p.UserID, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.ListAdminTickets200JSONResponse(l), nil
}

// GetAdminTicket implements GET /v1/admin/tickets/{ticketId}.
func (h adminHandlers) GetAdminTicket(ctx context.Context, req adminapi.GetAdminTicketRequestObject) (adminapi.GetAdminTicketResponseObject, error) {
	tid, err := pathID(req.TicketId)
	if err != nil {
		return nil, err
	}
	d, err := h.s.getAdmin(ctx, tid)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.GetAdminTicket200JSONResponse(d), nil
}

// AssignAdminTicket implements POST /v1/admin/tickets/{ticketId}/assign.
func (h adminHandlers) AssignAdminTicket(ctx context.Context, req adminapi.AssignAdminTicketRequestObject) (adminapi.AssignAdminTicketResponseObject, error) {
	actor, tid, err := adminCall(ctx, req.TicketId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	d, err := h.s.assign(ctx, actor, tid, req.Body.AssigneeId)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.AssignAdminTicket200JSONResponse(d), nil
}

// SetAdminTicketPriority implements POST /v1/admin/tickets/{ticketId}/priority.
func (h adminHandlers) SetAdminTicketPriority(ctx context.Context, req adminapi.SetAdminTicketPriorityRequestObject) (adminapi.SetAdminTicketPriorityResponseObject, error) {
	actor, tid, err := adminCall(ctx, req.TicketId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	d, err := h.s.setPriority(ctx, actor, tid, req.Body.Priority)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.SetAdminTicketPriority200JSONResponse(d), nil
}

// ReplyAdminTicket implements POST /v1/admin/tickets/{ticketId}/reply.
func (h adminHandlers) ReplyAdminTicket(ctx context.Context, req adminapi.ReplyAdminTicketRequestObject) (adminapi.ReplyAdminTicketResponseObject, error) {
	actor, tid, err := adminCall(ctx, req.TicketId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	d, err := h.s.adminMessage(ctx, actor, tid, "reply", *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.ReplyAdminTicket200JSONResponse(d), nil
}

// AddAdminTicketNote implements POST /v1/admin/tickets/{ticketId}/notes.
func (h adminHandlers) AddAdminTicketNote(ctx context.Context, req adminapi.AddAdminTicketNoteRequestObject) (adminapi.AddAdminTicketNoteResponseObject, error) {
	actor, tid, err := adminCall(ctx, req.TicketId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	d, err := h.s.adminMessage(ctx, actor, tid, "note", *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.AddAdminTicketNote200JSONResponse(d), nil
}

// CloseAdminTicket implements POST /v1/admin/tickets/{ticketId}/close.
func (h adminHandlers) CloseAdminTicket(ctx context.Context, req adminapi.CloseAdminTicketRequestObject) (adminapi.CloseAdminTicketResponseObject, error) {
	actor, tid, err := adminCall(ctx, req.TicketId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	d, err := h.s.closeTicket(ctx, actor, tid, req.Body.Outcome, req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.CloseAdminTicket200JSONResponse(d), nil
}

// CreateAdminTicketAttachment implements POST /v1/admin/tickets/{ticketId}/attachments.
func (h adminHandlers) CreateAdminTicketAttachment(ctx context.Context, req adminapi.CreateAdminTicketAttachmentRequestObject) (adminapi.CreateAdminTicketAttachmentResponseObject, error) {
	actor, tid, err := adminCall(ctx, req.TicketId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	u, err := h.s.adminUpload(ctx, actor, tid, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.CreateAdminTicketAttachment201JSONResponse{
		Body: u, Headers: adminapi.CreateAdminTicketAttachment201ResponseHeaders{CacheControl: ptr(noStore)},
	}, nil
}

// adminCall reads the admin principal and the ticket in the path, and checks that a body came.
func adminCall(ctx context.Context, ticket string, hasBody bool) (actor, tid ids.ID, err error) {
	p, err := principal(ctx)
	if err != nil {
		return ids.Zero, ids.Zero, err
	}
	if tid, err = pathID(ticket); err != nil {
		return ids.Zero, ids.Zero, err
	}
	if !hasBody {
		return ids.Zero, ids.Zero, fieldErr("body", "required")
	}
	return p.UserID, tid, nil
}

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
)

// The writes the A-14 ticket assistant (W3-13, AI-A2, D-233) applies once an
// agent confirms its draft. They run inside the draft's confirm transaction
// (InTx joins it as a savepoint) and keep the A-14 rules: a closed ticket is
// final, an order must belong to the opener, a priority change moves the SLA.

// AssistTriage sets a ticket's category, priority and, when orderID is set,
// its order (and the order's store), in one audited change.
func (s *Service) AssistTriage(ctx context.Context, actor, ticket ids.ID, category, priority string, orderID *ids.ID) error {
	if err := s.auditReady(ctx); err != nil {
		return err
	}
	if _, ok := slaKeys[priority]; !ok {
		return fieldErr("body.lines", "priority: normal, high or urgent")
	}
	_, err := s.write(ctx, ticket, func(ctx context.Context, t supportdb.LockTicketRow, _ time.Time) error {
		if t.Status == "closed" {
			return invalidTransition()
		}
		q := s.q(ctx)
		cur, err := q.GetTicket(ctx, t.ID)
		if err != nil {
			return err
		}
		var order, store pgtype.UUID
		if orderID != nil {
			order = ids.UUID(*orderID)
			if t.OpenerType == "business" {
				o, err := q.OrderOfBusiness(ctx, supportdb.OrderOfBusinessParams{ID: order, BusinessID: t.BusinessID})
				if db.IsNoRows(err) {
					return fieldErr("body.lines", "orderId: must be an order of the opener")
				}
				if err != nil {
					return err
				}
				store = o.StoreID
			} else {
				o, err := q.OrderOfCustomer(ctx, supportdb.OrderOfCustomerParams{ID: order, CustomerID: t.OpenerUserID})
				if db.IsNoRows(err) {
					return fieldErr("body.lines", "orderId: must be an order of the opener")
				}
				if err != nil {
					return err
				}
				store = o.StoreID
			}
		}
		due := t.SlaDueAt
		if t.Priority != priority && t.Status == "open" && t.SlaStartedAt != nil {
			d, err := s.slaDue(ctx, priority, *t.SlaStartedAt)
			if err != nil {
				return err
			}
			due = &d
		}
		if err := q.SetTicketTriage(ctx, supportdb.SetTicketTriageParams{
			Category: category, OrderID: order, StoreID: store, ID: t.ID,
		}); err != nil {
			return err
		}
		if t.Priority != priority {
			if err := q.SetTicketPriority(ctx, supportdb.SetTicketPriorityParams{
				Priority: priority, SlaDueAt: due, ID: t.ID,
			}); err != nil {
				return err
			}
		}
		after := map[string]any{"category": category, "priority": priority, "slaDueAt": utcPtr(due), "source": "ai_assist"}
		if orderID != nil {
			after["orderId"] = orderID.String()
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "support.ticket_triage",
			SubjectType: "support_ticket", SubjectID: ticket.String(),
			Before: map[string]any{
				"category": cur.Category, "priority": t.Priority, "slaDueAt": utcPtr(t.SlaDueAt), "orderId": optID(cur.OrderID),
			},
			After: after,
		})
		return err
	})
	return err
}

// AssistReply sends the agent's confirmed reply, exactly as an A-14 reply.
func (s *Service) AssistReply(ctx context.Context, actor, ticket ids.ID, text string) error {
	_, err := s.adminMessage(ctx, actor, ticket, "reply", adminapi.TicketMessageRequest{Body: text})
	return err
}

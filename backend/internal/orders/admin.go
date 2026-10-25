package orders

import (
	"context"
	"errors"

	adminordersapi "github.com/todayz/backend/gen/api/admin-orders"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// The admin side, pulled forward from W3-05 for the Wave 2 gate (D-83):
// support cancels an order with a reason and names the side at fault. Only
// that side gets an admin_cancel strike (01 §5.2); the cancel, the strike
// and the audit row commit together.

// AdminOps maps the admin-orders operations to their authz actions.
var AdminOps = map[string]string{
	"CancelAdminOrder": policy.AdminOrderCancel,
	// A-07 (W3-05)
	"ListAdminOrders": policy.AdminOrderView,
	"GetAdminOrder":   policy.AdminOrderView,
}

// The side at fault for an admin cancel.
const (
	faultNone     = "none"
	faultCustomer = "customer"
	faultStore    = "store"
)

const reasonAdminCancel = "admin_cancel"

func admin(id ids.ID) actor { return actor{kind: actorAdmin, id: id} }

// anyOrder: support sees every order.
func anyOrder(ordersdb.Order) bool { return true }

func (s *Service) auditReady(ctx context.Context) error {
	if err := s.readyToWrite(ctx); err != nil {
		return err
	}
	if s.audit == nil {
		return problem.Wrap(genproblems.ServiceUnavailable, errors.New("orders: the audit writer is not configured"))
	}
	return nil
}

// adminCancel cancels an order from any non-terminal state (03 §4.1),
// releasing what end releases, strikes the side at fault and audits it.
func (s *Service) adminCancel(ctx context.Context, by, orderID ids.ID, b *adminordersapi.AdminCancelOrderRequest) (adminordersapi.AdminCancelledOrder, error) {
	var out adminordersapi.AdminCancelledOrder
	if err := s.auditReady(ctx); err != nil {
		return out, err
	}
	if b == nil {
		return out, fieldErr("body", "required")
	}
	reason, fault := string(b.Reason), string(b.Strike)
	if !b.Reason.Valid() {
		return out, fieldErr("body.reason", "not an admin cancel reason")
	}
	if !b.Strike.Valid() {
		return out, fieldErr("body.strike", "none, customer or store")
	}
	note, err := noteFor(reason, b.Note)
	if err != nil {
		return out, err
	}
	var rules noShow
	if fault == faultCustomer {
		if rules, err = s.noShowRules(ctx); err != nil {
			return out, err
		}
	}
	var threshold int
	if fault == faultStore {
		if threshold, err = s.count(ctx, settings.StoreStrikeRiskThreshold); err != nil {
			return out, err
		}
	}
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		q := s.q(ctx)
		o, err := s.lockOrder(ctx, orderID, anyOrder)
		if err != nil {
			return err
		}
		from := o.Status
		if fault == faultCustomer {
			if err := lockStrikeCustomer(ctx, q, o.CustomerID); err != nil {
				return err
			}
		}
		if err := s.end(ctx, o, StatusCancelled, reason, note, admin(by), generic, now); err != nil {
			return err
		}
		switch fault {
		case faultCustomer:
			if err := s.strikeCustomer(ctx, q, o, reasonAdminCancel, now, rules); err != nil {
				return err
			}
		case faultStore:
			if err := s.strikeStore(ctx, q, o, reasonAdminCancel, now, threshold); err != nil {
				return err
			}
		}
		if _, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: by, Action: "order.admin_cancel",
			SubjectType: "order", SubjectID: orderID.String(),
			Before: map[string]any{"status": from},
			After:  map[string]any{"status": StatusCancelled, "reason": reason, "strike": fault},
		}); err != nil {
			return err
		}
		cur, err := q.GetOrder(ctx, o.ID)
		if err != nil {
			return err
		}
		out = adminordersapi.AdminCancelledOrder{
			Id: orderID.String(), Number: displayNumber(cur), StoreId: mustID(cur.StoreID).String(), CustomerId: mustID(cur.CustomerID).String(),
			Status: adminordersapi.AdminCancelledOrderStatus(cur.Status), StatusReason: adminordersapi.AdminCancelReason(reason),
			StatusNote: cur.StatusNote, Strike: b.Strike, Version: int(cur.Version), ServerNow: now,
		}
		return nil
	})
	return out, asProblemOrNil(err)
}

type adminHandlers struct{ s *Service }

var _ adminordersapi.StrictServerInterface = adminHandlers{}

// CancelAdminOrder implements POST /v1/admin/orders/{orderId}/cancel.
func (h adminHandlers) CancelAdminOrder(ctx context.Context, req adminordersapi.CancelAdminOrderRequestObject) (adminordersapi.CancelAdminOrderResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := pathID(req.OrderId)
	if err != nil {
		return nil, err
	}
	o, err := h.s.adminCancel(ctx, p.UserID, id, req.Body)
	if err != nil {
		return nil, err
	}
	return adminordersapi.CancelAdminOrder200JSONResponse(o), nil
}

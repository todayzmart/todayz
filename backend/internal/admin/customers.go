package admin

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	api "github.com/todayz/backend/gen/api/admin-customers"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// A-06 Customers (01 §13, §5.4, D-41). Search is by exact phone only (the
// keyed hash), so there is no customer list to scroll. Block is global: the
// account's status refuses sign-in in every app and every live session is
// revoked in the same transaction. Only a customer account (one with a
// customer profile) is blocked here, never an admin: A-18 disables an admin,
// and an admin's sessions are neither listed nor revoked here.

func (s *Service) search(ctx context.Context, phone string) (api.AdminCustomerSearch, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminCustomerSearch{}, err
	}
	if s.hash == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "admin: phone search needs PII_HASH_KEY")
		return api.AdminCustomerSearch{}, problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	p, ok := e164(phone)
	if !ok {
		return api.AdminCustomerSearch{}, fieldErr("body.phone", "an Indian mobile number")
	}
	out := api.AdminCustomerSearch{Items: []api.AdminCustomerSummary{}, ServerNow: s.now()}
	r, err := s.q(ctx).CustomerByPhoneHash(ctx, s.hash.Phone(p))
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return api.AdminCustomerSearch{}, err
	}
	out.Items = append(out.Items, api.AdminCustomerSummary{
		Id: idString(r.ID), Name: r.Name, PhoneMasked: s.maskedPhone(ctx, r.PhoneEnc),
		Status: api.AdminCustomerStatus(r.Status), ActiveStrikes: int(r.ActiveStrikes), CreatedAt: r.CreatedAt.UTC(),
	})
	return out, nil
}

func (s *Service) customer(ctx context.Context, id ids.ID) (api.AdminCustomer, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminCustomer{}, err
	}
	q := s.q(ctx)
	u := ids.UUID(id)
	r, err := q.GetCustomer(ctx, u)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminCustomer{}, notFound()
	}
	if err != nil {
		return api.AdminCustomer{}, err
	}
	now := s.now()
	out := api.AdminCustomer{
		Id: idString(r.ID), Name: r.Name, PhoneMasked: s.maskedPhone(ctx, r.PhoneEnc),
		Status: api.AdminCustomerStatus(r.Status), PreferredLang: api.AdminCustomerPreferredLang(r.PreferredLang),
		IsAdmin: r.IsAdmin, IsCustomer: r.IsCustomer,
		CreatedAt: r.CreatedAt.UTC(), ActiveStrikes: int(r.ActiveStrikes), ServerNow: now,
		Strikes: []api.AdminStrike{}, Sessions: []api.AdminCustomerSession{}, RecentOrders: []api.AdminCustomerOrder{},
	}
	strikes, err := q.CustomerStrikes(ctx, u)
	if err != nil {
		return api.AdminCustomer{}, err
	}
	for _, k := range strikes {
		out.Strikes = append(out.Strikes, api.AdminStrike{
			Id: idString(k.ID), Reason: api.AdminStrikeReason(k.Reason), OrderId: optID(k.OrderID),
			CreatedAt: k.CreatedAt.UTC(), ResetAt: utcPtr(k.ResetAt), ResetReason: k.ResetReason,
		})
	}
	var sessions []admindb.LiveSessionsRow
	if !r.IsAdmin {
		if sessions, err = q.LiveSessions(ctx, admindb.LiveSessionsParams{UserID: u, Now: now}); err != nil {
			return api.AdminCustomer{}, err
		}
	}
	for _, x := range sessions {
		out.Sessions = append(out.Sessions, api.AdminCustomerSession{
			Id: idString(x.ID), App: api.AdminCustomerSessionApp(x.App), UserAgent: x.UserAgent,
			CreatedAt: x.CreatedAt.UTC(), LastUsedAt: x.LastUsedAt.UTC(), ExpiresAt: x.ExpiresAt.UTC(),
		})
	}
	orders, err := q.CustomerRecentOrders(ctx, u)
	if err != nil {
		return api.AdminCustomer{}, err
	}
	for _, o := range orders {
		n := ""
		if o.DisplayNumber != nil {
			n = *o.DisplayNumber
		}
		out.RecentOrders = append(out.RecentOrders, api.AdminCustomerOrder{
			Id: idString(o.ID), Number: n, StoreId: idString(o.StoreID), StoreName: o.StoreName,
			Status: o.Status, TotalPaise: int(o.TotalPaise), PlacedAt: o.PlacedAt.UTC(),
		})
	}
	return out, nil
}

// setStatus blocks (to = "blocked") or unblocks (to = "active") a customer.
func (s *Service) setStatus(ctx context.Context, actor, id ids.ID, to, reason string) (api.AdminCustomer, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminCustomer{}, err
	}
	reason, err := cleanReason(reason)
	if err != nil {
		return api.AdminCustomer{}, err
	}
	from, action := "active", "customer.block"
	if to == "active" {
		from, action = "blocked", "customer.unblock"
	}
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		u := ids.UUID(id)
		// Lock the user before its sessions, as identity's refresh does.
		status, err := lockCustomer(ctx, q, u)
		if err != nil {
			return err
		}
		if status != from {
			return invalidTransition("")
		}
		now := s.now()
		if err := q.SetCustomerStatus(ctx, admindb.SetCustomerStatusParams{Status: to, Now: now, ID: u}); err != nil {
			return err
		}
		after := map[string]any{"status": to, "reason": reason}
		if to == "blocked" {
			n, err := q.RevokeUserSessions(ctx, admindb.RevokeUserSessionsParams{Now: now, Reason: "blocked", UserID: u})
			if err != nil {
				return err
			}
			after["sessionsRevoked"] = n
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: action,
			SubjectType: "user", SubjectID: id.String(),
			Before: map[string]any{"status": from}, After: after,
		})
		return err
	})
	if err != nil {
		return api.AdminCustomer{}, err
	}
	return s.customer(ctx, id)
}

// resetStrikes clears every active strike of a customer with a reason (D-41)
// and recomputes the booking limit in the same transaction.
func (s *Service) resetStrikes(ctx context.Context, actor, id ids.ID, reason string) (api.AdminCustomer, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminCustomer{}, err
	}
	if s.strikes == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "admin: the strike recount is not configured")
		return api.AdminCustomer{}, problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	reason, err := cleanReason(reason)
	if err != nil {
		return api.AdminCustomer{}, err
	}
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		u := ids.UUID(id)
		if err := lockNonAdmin(ctx, q, u); err != nil {
			return err
		}
		now := s.now()
		reset, err := q.ResetStrikes(ctx, admindb.ResetStrikesParams{
			Now: &now, By: ids.UUID(actor), Reason: &reason, SubjectType: "customer", SubjectID: u,
		})
		if err != nil {
			return err
		}
		if len(reset) == 0 {
			return invalidTransition("This customer has no active strikes.")
		}
		if err := s.strikes.RecountCustomerStrikes(ctx, id); err != nil {
			return err
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "customer.strikes_reset",
			SubjectType: "user", SubjectID: id.String(),
			Before: map[string]any{"activeStrikes": len(reset)},
			After:  map[string]any{"activeStrikes": 0, "reason": reason, "strikeIds": uuidStrings(reset)},
		})
		return err
	})
	if err != nil {
		return api.AdminCustomer{}, err
	}
	return s.customer(ctx, id)
}

// revokeSession signs a customer out of one device.
func (s *Service) revokeSession(ctx context.Context, actor, id, session ids.ID) error {
	if err := s.writeReady(ctx); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		u := ids.UUID(id)
		if err := lockNonAdmin(ctx, q, u); err != nil {
			return err
		}
		n, err := q.RevokeUserSession(ctx, admindb.RevokeUserSessionParams{Now: s.now(), ID: ids.UUID(session), UserID: u})
		if err != nil {
			return err
		}
		if n == 0 {
			return notFound()
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "customer.session_revoke",
			SubjectType: "user", SubjectID: id.String(),
			After: map[string]any{"sessionId": session.String()},
		})
		return err
	})
}

// adminAccount is why the customer console refuses an admin account: A-18
// disables an admin instead.
const adminAccount = "An admin account is disabled from A-18, not changed here."

// lockNonAdmin locks the user row and refuses an admin account under that lock.
func lockNonAdmin(ctx context.Context, q *admindb.Queries, u pgtype.UUID) error {
	_, roles, err := lockUser(ctx, q, u)
	if err != nil {
		return err
	}
	if roles.IsAdmin {
		return invalidTransition(adminAccount)
	}
	return nil
}

// lockCustomer is lockNonAdmin that also refuses an account with no customer
// profile, for block and unblock; it returns the status read under the lock.
func lockCustomer(ctx context.Context, q *admindb.Queries, u pgtype.UUID) (string, error) {
	status, roles, err := lockUser(ctx, q, u)
	if err != nil {
		return "", err
	}
	if roles.IsAdmin {
		return "", invalidTransition(adminAccount)
	}
	if !roles.IsCustomer {
		return "", invalidTransition("Only a customer account can be blocked or unblocked here.")
	}
	return status, nil
}

// lockUser locks the users row, then reads the roles in a later statement so a
// change that committed during the wait counts.
func lockUser(ctx context.Context, q *admindb.Queries, u pgtype.UUID) (string, admindb.CustomerRolesRow, error) {
	status, err := q.LockCustomer(ctx, u)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", admindb.CustomerRolesRow{}, notFound()
	}
	if err != nil {
		return "", admindb.CustomerRolesRow{}, err
	}
	roles, err := q.CustomerRoles(ctx, u)
	return status, roles, err
}

func uuidStrings(us []pgtype.UUID) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = idString(u)
	}
	return out
}

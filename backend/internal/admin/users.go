package admin

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	api "github.com/todayz/backend/gen/api/admin-users"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// A-18 Admin users (D-64). super_admin only; every change needs a fresh
// step-up (authz) and writes an audit row with its reason. An invite gives a
// phone number admin access: a number with an account keeps it, one that has
// never signed in gets a users and phone_identities row here (identity never
// makes an admin at sign-in). Disabling and the authenticator reset end every
// admin session of the person; identity re-reads admin_users on every refresh
// and session check, so a role change holds from the next request.
//
// Guards: nobody changes or resets themselves, and no change leaves the
// platform without an active super_admin. Every change takes one advisory
// lock first, so two super_admins acting on each other at once are ordered
// and the count stays true; under it the actor is read again (still an
// active, enabled super_admin, or 403). A change that revokes sessions then
// locks the target's users row before its admin_users row, the order every
// revoke path in identity takes (users, admin_users, sessions).

const (
	superAdmin  = string(authz.SuperAdmin)
	maxUserName = 80 //settings:ok contract bound (users.name ≤ 80)
)

func (s *Service) adminUserOut(ctx context.Context, r admindb.GetAdminUserRow) api.AdminUser {
	roles := make([]api.AdminRole, 0, len(r.Roles))
	for _, x := range r.Roles {
		roles = append(roles, api.AdminRole(x))
	}
	out := api.AdminUser{
		Id: idString(r.UserID), Name: r.Name, PhoneMasked: s.maskedPhone(ctx, r.PhoneEnc), Roles: roles,
		Status: api.Active, TotpEnrolled: r.TotpEnabledAt != nil, CreatedBy: optID(r.CreatedBy),
		CreatedByName: r.CreatedByName, CreatedAt: r.CreatedAt.UTC(), DisabledAt: utcPtr(r.DisabledAt),
		Version: int(r.Version),
	}
	if r.DisabledAt != nil {
		out.Status = api.Disabled
	}
	if t, ok := r.LastActiveAt.(time.Time); ok {
		u := t.UTC()
		out.LastActiveAt = &u
	}
	return out
}

func (s *Service) listAdminUsers(ctx context.Context, p api.ListAdminUsersParams) (api.AdminUserList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminUserList{}, err
	}
	lim, at, after, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return api.AdminUserList{}, err
	}
	rows, err := s.q(ctx).ListAdminUsers(ctx, admindb.ListAdminUsersParams{
		Disabled: p.Status != nil && *p.Status == api.Disabled, AfterAt: at, AfterID: after,
		Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	})
	if err != nil {
		return api.AdminUserList{}, err
	}
	out := api.AdminUserList{Items: []api.AdminUser{}, ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			last := rows[lim-1]
			lid, _ := ids.FromUUID(last.UserID)
			c := encodeCursor(last.CreatedAt, lid)
			out.NextCursor = &c
			break
		}
		out.Items = append(out.Items, s.adminUserOut(ctx, admindb.GetAdminUserRow(r)))
	}
	return out, nil
}

func (s *Service) adminUser(ctx context.Context, id ids.ID) (api.AdminUser, error) {
	r, err := s.q(ctx).GetAdminUser(ctx, ids.UUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminUser{}, notFound()
	}
	if err != nil {
		return api.AdminUser{}, err
	}
	return s.adminUserOut(ctx, r), nil
}

// cleanRoles checks the roles and returns them in one fixed order, so the
// stored list and the audit row never differ only by order.
func cleanRoles(path string, in []api.AdminRole) ([]string, error) {
	if len(in) == 0 {
		return nil, fieldErr(path, "at least one role")
	}
	out := make([]string, 0, len(in))
	for _, r := range in {
		if !r.Valid() {
			return nil, fieldErr(path, "one of the 7 admin roles")
		}
		if !slices.Contains(out, string(r)) {
			out = append(out, string(r))
		}
	}
	slices.Sort(out)
	return out, nil
}

// inviteAdminUser gives a phone number admin access.
func (s *Service) inviteAdminUser(ctx context.Context, actor ids.ID, in api.AdminUserInvite) (api.AdminUser, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminUser{}, err
	}
	if s.accounts == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "admin: an invite needs the identity accounts (PII_ENC_KEY and PII_HASH_KEY)")
		return api.AdminUser{}, unavailable()
	}
	phone, ok := e164(in.Phone)
	if !ok {
		return api.AdminUser{}, fieldErr("body.phone", "an Indian mobile number")
	}
	name, err := cleanName("body.name", in.Name, maxUserName)
	if err != nil {
		return api.AdminUser{}, err
	}
	if err := noPhone("body.name", name); err != nil {
		return api.AdminUser{}, err
	}
	roles, err := cleanRoles("body.roles", in.Roles)
	if err != nil {
		return api.AdminUser{}, err
	}
	reason, err := cleanReason(in.Reason)
	if err != nil {
		return api.AdminUser{}, err
	}
	var id ids.ID
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := q.LockAdminUsers(ctx); err != nil {
			return err
		}
		if err := actorStillSuper(ctx, q, actor); err != nil {
			return err
		}
		now := s.now()
		acct, err := s.accounts.FindOrCreateByPhone(ctx, phone, now)
		if err != nil {
			return err
		}
		id = acct.UserID
		if !acct.Created {
			exists, err := q.AdminUserExists(ctx, ids.UUID(id))
			if err != nil {
				return err
			}
			if exists {
				return problem.New(genproblems.Conflict, "This number already has admin access.")
			}
			if acct.Status != "active" {
				return invalidTransition("This account is blocked, so it cannot be given admin access.")
			}
		}
		if err := q.NameUserIfUnnamed(ctx, admindb.NameUserIfUnnamedParams{Name: &name, ID: ids.UUID(id)}); err != nil {
			return err
		}
		if err := q.InsertAdminUser(ctx, admindb.InsertAdminUserParams{
			UserID: ids.UUID(id), Roles: roles, CreatedBy: ids.UUID(actor), Now: now,
		}); err != nil {
			return err
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "admin_user.invite",
			SubjectType: "admin_user", SubjectID: id.String(),
			After: map[string]any{"phone": pii.MaskPhone(phone), "roles": roles, "newAccount": acct.Created, "reason": reason},
		})
		return err
	})
	if err != nil {
		return api.AdminUser{}, err
	}
	return s.adminUser(ctx, id)
}

// updateAdminUser changes roles, or disables or re-enables an admin.
func (s *Service) updateAdminUser(ctx context.Context, actor, id ids.ID, in api.AdminUserChange) (api.AdminUser, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminUser{}, err
	}
	if in.Roles == nil && in.Status == nil {
		return api.AdminUser{}, fieldErr("body", "roles or status")
	}
	var roles []string
	if in.Roles != nil {
		r, err := cleanRoles("body.roles", *in.Roles)
		if err != nil {
			return api.AdminUser{}, err
		}
		roles = r
	}
	if in.Status != nil && !in.Status.Valid() {
		return api.AdminUser{}, fieldErr("body.status", "active or disabled")
	}
	reason, err := cleanReason(in.Reason)
	if err != nil {
		return api.AdminUser{}, err
	}
	if actor == id {
		return api.AdminUser{}, invalidTransition("You cannot change your own access. Ask another super admin.")
	}
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := q.LockAdminUsers(ctx); err != nil {
			return err
		}
		if err := actorStillSuper(ctx, q, actor); err != nil {
			return err
		}
		status, err := lockTargetUser(ctx, q, id)
		if err != nil {
			return err
		}
		cur, err := q.LockAdminUser(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if int(cur.Version) != in.Version {
			return problem.New(genproblems.Conflict, "Someone changed this admin since you opened it.")
		}
		now := s.now()
		next := admindb.UpdateAdminUserParams{Roles: cur.Roles, DisabledAt: cur.DisabledAt, Now: now, ID: ids.UUID(id)}
		if roles != nil {
			next.Roles = roles
		}
		wasDisabled := cur.DisabledAt != nil
		disable := in.Status != nil && *in.Status == api.Disabled
		enable := in.Status != nil && *in.Status == api.Active
		switch {
		case disable && !wasDisabled:
			next.DisabledAt = &now
		case enable && wasDisabled:
			next.DisabledAt = nil
		}
		sameRoles := roles == nil || slices.Equal(sortedCopy(cur.Roles), roles)
		sameStatus := (next.DisabledAt != nil) == wasDisabled
		if sameRoles && sameStatus {
			return invalidTransition("Nothing to change.")
		}
		before := map[string]any{"roles": cur.Roles, "disabled": wasDisabled}
		after := map[string]any{"roles": next.Roles, "disabled": next.DisabledAt != nil, "reason": reason}
		if err := s.keepSuperAdmin(ctx, q, status == userActive, cur.Roles, wasDisabled, next.Roles, next.DisabledAt != nil); err != nil {
			return err
		}
		if err := q.UpdateAdminUser(ctx, next); err != nil {
			return err
		}
		if next.DisabledAt != nil && !wasDisabled {
			n, err := q.RevokeAdminSessions(ctx, admindb.RevokeAdminSessionsParams{Now: now, UserID: ids.UUID(id)})
			if err != nil {
				return err
			}
			after["sessionsRevoked"] = n
		}
		action := "admin_user.update"
		switch {
		case next.DisabledAt != nil && !wasDisabled:
			action = "admin_user.disable"
		case next.DisabledAt == nil && wasDisabled:
			action = "admin_user.enable"
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: action,
			SubjectType: "admin_user", SubjectID: id.String(), Before: before, After: after,
		})
		return err
	})
	if err != nil {
		return api.AdminUser{}, err
	}
	return s.adminUser(ctx, id)
}

// keepSuperAdmin refuses a change that takes the last active super_admin
// away. A target whose account is not active is not counted as one (nor is it
// by CountActiveSuperAdmins), so taking the role from a blocked account is
// never refused.
func (s *Service) keepSuperAdmin(ctx context.Context, q *admindb.Queries, active bool, roles []string, disabled bool, nextRoles []string, nextDisabled bool) error {
	wasSuper := active && !disabled && slices.Contains(roles, superAdmin)
	staysSuper := !nextDisabled && slices.Contains(nextRoles, superAdmin)
	if !wasSuper || staysSuper {
		return nil
	}
	n, err := q.CountActiveSuperAdmins(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return invalidTransition("This is the last active super admin. Give the role to someone else first.")
	}
	return nil
}

// resetAdminTOTP clears an admin's authenticator and recovery codes and ends
// their admin sessions; the next sign-in enrols again.
func (s *Service) resetAdminTOTP(ctx context.Context, actor, id ids.ID, in api.AdminUserTOTPReset) (api.AdminUser, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminUser{}, err
	}
	reason, err := cleanReason(in.Reason)
	if err != nil {
		return api.AdminUser{}, err
	}
	if actor == id {
		return api.AdminUser{}, invalidTransition("You cannot reset your own authenticator. Ask another super admin.")
	}
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := q.LockAdminUsers(ctx); err != nil {
			return err
		}
		if err := actorStillSuper(ctx, q, actor); err != nil {
			return err
		}
		if _, err := lockTargetUser(ctx, q, id); err != nil {
			return err
		}
		if _, err := q.LockAdminUser(ctx, ids.UUID(id)); errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		} else if err != nil {
			return err
		}
		now := s.now()
		if err := q.ResetAdminTOTP(ctx, admindb.ResetAdminTOTPParams{Now: now, ID: ids.UUID(id)}); err != nil {
			return err
		}
		codes, err := q.DeleteAdminRecoveryCodes(ctx, ids.UUID(id))
		if err != nil {
			return err
		}
		n, err := q.RevokeAdminSessions(ctx, admindb.RevokeAdminSessionsParams{Now: now, UserID: ids.UUID(id)})
		if err != nil {
			return err
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "admin_user.totp_reset",
			SubjectType: "admin_user", SubjectID: id.String(),
			After: map[string]any{"recoveryCodesCleared": codes, "sessionsRevoked": n, "reason": reason},
		})
		return err
	})
	if err != nil {
		return api.AdminUser{}, err
	}
	return s.adminUser(ctx, id)
}

const userActive = "active"

// actorStillSuper re-reads the actor under the advisory lock. authz checked
// them when the request came in; a super_admin disabled, demoted or blocked by
// a change that committed since then makes no further change.
func actorStillSuper(ctx context.Context, q *admindb.Queries, actor ids.ID) error {
	r, err := q.AdminActor(ctx, ids.UUID(actor))
	if errors.Is(err, pgx.ErrNoRows) {
		return problem.New(genproblems.Forbidden, "")
	}
	if err != nil {
		return err
	}
	if r.DisabledAt != nil || !slices.Contains(r.Roles, superAdmin) || r.Status != userActive {
		return problem.New(genproblems.Forbidden, "")
	}
	return nil
}

// lockTargetUser locks the target's users row (see LockAdminTargetUser) and
// returns its status.
func lockTargetUser(ctx context.Context, q *admindb.Queries, id ids.ID) (string, error) {
	st, err := q.LockAdminTargetUser(ctx, ids.UUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", notFound()
	}
	return st, err
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

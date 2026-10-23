package business

// Staff (01 §3, D-66): an owner invites managers and cashiers, a manager only
// cashiers; the owner's row never changes here. An invite is by phone: it is
// bound to the account with that phone if there is one, else it waits for the
// phone to sign in. The invite phone is stored sealed and hashed, and cleared
// once the invite is accepted or removed.
//
// TODO(W1-01b): account deletion must refuse the owner of a live business
// until it is handed over (OwnsActiveBusiness), and must clear
// invite_phone_enc and invite_phone_hash on the staff rows still invited to
// the deleted account's phone.

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"

	businessapi "github.com/todayz/backend/gen/api/business"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

func (s *Service) toMember(r businessdb.GetStaffViewRow) (businessapi.StaffMember, error) {
	out := businessapi.StaffMember{
		CanDeliver: r.CanDeliver, CreatedAt: r.CreatedAt.UTC(), Id: idString(r.ID), Name: r.UserName,
		Role: businessapi.StaffRole(r.Role), Status: businessapi.StaffStatus(r.Status),
		StoreIds: idStrings(r.StoreIds), UserId: optIDString(r.UserID),
	}
	sealed := r.UserPhoneEnc
	if sealed == nil {
		sealed = r.InvitePhoneEnc
	}
	if sealed != nil {
		plain, err := s.seal.Open(FieldPhone, sealed)
		if err != nil {
			return businessapi.StaffMember{}, fmt.Errorf("business: open staff phone: %w", err)
		}
		masked := pii.MaskPhone(string(plain))
		out.PhoneMasked = &masked
	}
	return out, nil
}

func (s *Service) memberView(ctx context.Context, bid ids.ID, id pgtype.UUID) (businessapi.StaffMember, error) {
	row, err := s.q(ctx).GetStaffView(ctx, businessdb.GetStaffViewParams{ID: id, BusinessID: ids.UUID(bid)})
	if err != nil {
		return businessapi.StaffMember{}, fmt.Errorf("business: staff view: %w", err)
	}
	return s.toMember(row)
}

// staffAudit is what the audit log keeps of a staff row: never the phone.
func staffAudit(r businessdb.Staff) map[string]any {
	return map[string]any{"role": r.Role, "status": r.Status, "storeIds": idStrings(r.StoreIds), "canDeliver": r.CanDeliver}
}

func (s *Service) listStaff(ctx context.Context, bidStr string) (businessapi.StaffList, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.StaffList{}, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.StaffList{}, err
	}
	rows, err := s.q(ctx).ListStaff(ctx, ids.UUID(bid))
	if err != nil {
		return businessapi.StaffList{}, fmt.Errorf("business: staff: %w", err)
	}
	out := businessapi.StaffList{Items: make([]businessapi.StaffMember, 0, len(rows))}
	for _, r := range rows {
		m, err := s.toMember(businessdb.GetStaffViewRow(r))
		if err != nil {
			return businessapi.StaffList{}, err
		}
		out.Items = append(out.Items, m)
	}
	return out, nil
}

// requestStores reads an optional store list: absent or null is every store
// (nil), an empty list is refused.
func (s *Service) requestStores(ctx context.Context, v *invalid, path string, bid ids.ID, in *[]string) []pgtype.UUID {
	if in == nil {
		return nil
	}
	if len(*in) == 0 {
		v.add(path, "must name at least one store, or be null for every store")
		return nil
	}
	return s.storeIDs(ctx, v, path, bid, *in)
}

// withinScope reports whether stores (nil: every store) sit inside a
// membership's scope (nil: every store).
func withinScope(m authz.Membership, stores []pgtype.UUID) bool {
	if m.StoreIDs == nil {
		return true
	}
	if stores == nil {
		return false
	}
	for _, u := range stores {
		id, ok := ids.FromUUID(u)
		if !ok || !slices.Contains(m.StoreIDs, id) {
			return false
		}
	}
	return true
}

func (s *Service) inviteStaff(ctx context.Context, bidStr string, body *businessapi.StaffInvite) (businessapi.StaffMember, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.StaffMember{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.StaffMember{}, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.StaffMember{}, err
	}
	if body == nil {
		return businessapi.StaffMember{}, problem.New(genproblems.BadRequest, "")
	}
	role := string(body.Role)
	if role != roleManager && role != roleCashier {
		return businessapi.StaffMember{}, problem.New(genproblems.ValidationFailed, "").WithField("body.role", "must be manager or cashier")
	}
	if role == roleManager {
		// A manager may invite cashiers only (D-66).
		if err := s.also(ctx, policy.StaffManage, authz.Scope{BusinessID: bid}); err != nil {
			return businessapi.StaffMember{}, err
		}
	}
	var v invalid
	e164, ok := pii.NormalizePhone(body.Phone)
	if !ok {
		v.add("body.phone", "must be an Indian mobile number")
	}
	stores := s.requestStores(ctx, &v, "body.storeIds", bid, body.StoreIds)
	if err := v.err(); err != nil {
		return businessapi.StaffMember{}, err
	}
	m, err := s.membership(ctx, p.UserID, bid)
	if err != nil {
		return businessapi.StaffMember{}, err
	}
	if !withinScope(m, stores) {
		// A manager limited to some stores may invite only into those.
		return businessapi.StaffMember{}, problem.New(genproblems.Forbidden, "")
	}
	canDeliver := body.CanDeliver != nil && *body.CanDeliver
	hash := s.hash.Phone(e164)
	enc, err := s.seal.Seal(FieldPhone, []byte(e164))
	if err != nil {
		return businessapi.StaffMember{}, fmt.Errorf("business: seal invite phone: %w", err)
	}

	var row businessdb.Staff
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		uid, err := q.UserIDByPhoneHash(ctx, hash)
		if err != nil && !db.IsNoRows(err) {
			return err
		}
		if uid.Valid {
			cur, err := q.GetStaffByUser(ctx, businessdb.GetStaffByUserParams{BusinessID: ids.UUID(bid), UserID: uid})
			switch {
			case err == nil && cur.Status != staffRemoved:
				return problem.New(genproblems.Conflict, "this phone is already on the staff")
			case err == nil:
				row, err = q.ReinviteStaff(ctx, businessdb.ReinviteStaffParams{
					UserID: uid, Role: role, StoreIds: stores, CanDeliver: canDeliver, InvitedBy: ids.UUID(p.UserID),
					InvitePhoneHash: hash, InvitePhoneEnc: enc, ID: cur.ID,
				})
				if err != nil {
					return err
				}
				return s.writeAudit(ctx, p.UserID, "staff.invite", "staff", idString(row.ID), staffAudit(cur), staffAudit(row))
			case !db.IsNoRows(err):
				return err
			}
		}
		if _, err := q.GetInviteByPhone(ctx, businessdb.GetInviteByPhoneParams{BusinessID: ids.UUID(bid), PhoneHash: hash}); err == nil {
			return problem.New(genproblems.Conflict, "this phone is already invited")
		} else if !db.IsNoRows(err) {
			return err
		}
		now := s.clock.Now()
		row, err = q.InsertStaff(ctx, businessdb.InsertStaffParams{
			ID: ids.UUID(ids.New(now)), BusinessID: ids.UUID(bid), UserID: uid, Role: role, StoreIds: stores,
			CanDeliver: canDeliver, InvitedBy: ids.UUID(p.UserID), Status: staffInvited,
			InvitePhoneHash: hash, InvitePhoneEnc: enc, Now: now,
		})
		if err != nil {
			return err
		}
		return s.writeAudit(ctx, p.UserID, "staff.invite", "staff", idString(row.ID), nil, staffAudit(row))
	})
	if db.IsUniqueViolation(err, "ux_staff_business_user") || db.IsUniqueViolation(err, "ux_staff_business_invite_phone") {
		return businessapi.StaffMember{}, problem.New(genproblems.Conflict, "this phone is already on the staff")
	}
	if err != nil {
		return businessapi.StaffMember{}, err
	}
	return s.memberView(ctx, bid, row.ID)
}

// staffTarget locks a staff row of the business for a change: unknown or
// removed is 404, the owner's row is 403.
func (s *Service) staffTarget(ctx context.Context, bid ids.ID, staffIDStr string) (businessdb.Staff, error) {
	id, err := pathID(staffIDStr)
	if err != nil {
		return businessdb.Staff{}, err
	}
	row, err := s.q(ctx).GetStaffForUpdate(ctx, businessdb.GetStaffForUpdateParams{ID: ids.UUID(id), BusinessID: ids.UUID(bid)})
	if db.IsNoRows(err) || (err == nil && row.Status == staffRemoved) {
		return businessdb.Staff{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return businessdb.Staff{}, fmt.Errorf("business: staff: %w", err)
	}
	if row.Role == roleOwner {
		// TODO(lead): ownership transfer is not in 04; the owner's row is
		// refused with 403 rather than 409.
		return businessdb.Staff{}, problem.New(genproblems.Forbidden, "the owner cannot be changed here")
	}
	return row, nil
}

func (s *Service) removeStaff(ctx context.Context, bidStr, staffIDStr string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return err
	}
	return s.db.InTx(ctx, func(ctx context.Context) error {
		row, err := s.staffTarget(ctx, bid, staffIDStr)
		if err != nil {
			return err
		}
		if row.UserID == ids.UUID(p.UserID) {
			// TODO(lead): leaving a business is not in 04; nobody removes
			// themselves here.
			return problem.New(genproblems.Forbidden, "you cannot remove yourself")
		}
		if row.Role == roleManager {
			if err := s.also(ctx, policy.StaffManage, authz.Scope{BusinessID: bid}); err != nil {
				return err
			}
		}
		m, err := s.membership(ctx, p.UserID, bid)
		if err != nil {
			return err
		}
		if !withinScope(m, row.StoreIds) {
			return problem.New(genproblems.Forbidden, "")
		}
		if err := s.q(ctx).RemoveStaff(ctx, businessdb.RemoveStaffParams{Now: s.clock.Now(), ID: row.ID}); err != nil {
			return err
		}
		after := row
		after.Status = staffRemoved
		return s.writeAudit(ctx, p.UserID, "staff.remove", "staff", idString(row.ID), staffAudit(row), staffAudit(after))
	})
}

// updateStaff changes a member's role, stores or delivery flag (owner only,
// D-66). Fields left out keep their value; storeIds null is every store.
func (s *Service) updateStaff(ctx context.Context, bidStr, staffIDStr string, body *businessapi.StaffUpdate) (businessapi.StaffMember, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.StaffMember{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.StaffMember{}, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.StaffMember{}, err
	}
	if body == nil {
		return businessapi.StaffMember{}, problem.New(genproblems.BadRequest, "")
	}
	params := businessdb.UpdateStaffParams{}
	var v invalid
	if sent(ctx, "role") && body.Role != nil {
		r := string(*body.Role)
		if r != roleManager && r != roleCashier {
			v.add("body.role", "must be manager or cashier")
		}
		params.SetRole, params.Role = true, r
	}
	if sent(ctx, "storeIds") {
		params.SetStoreIds, params.StoreIds = true, s.requestStores(ctx, &v, "body.storeIds", bid, body.StoreIds)
	}
	if sent(ctx, "canDeliver") && body.CanDeliver != nil {
		params.SetCanDeliver, params.CanDeliver = true, *body.CanDeliver
	}
	if err := v.err(); err != nil {
		return businessapi.StaffMember{}, err
	}
	var row businessdb.Staff
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.staffTarget(ctx, bid, staffIDStr)
		if err != nil {
			return err
		}
		params.ID = cur.ID
		row, err = s.q(ctx).UpdateStaff(ctx, params)
		if err != nil {
			return err
		}
		return s.writeAudit(ctx, p.UserID, "staff.update", "staff", idString(row.ID), staffAudit(cur), staffAudit(row))
	})
	if err != nil {
		return businessapi.StaffMember{}, err
	}
	return s.memberView(ctx, bid, row.ID)
}

// acceptInvite turns the caller's pending invite into an active membership.
// The invite must name the caller's account, or (made before the account
// existed) the phone the caller signed in with; anything else is 404, so
// invites are not discoverable by id.
func (s *Service) acceptInvite(ctx context.Context, bidStr, staffIDStr string) (businessapi.StaffMember, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.StaffMember{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.StaffMember{}, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.StaffMember{}, err
	}
	id, err := pathID(staffIDStr)
	if err != nil {
		return businessapi.StaffMember{}, err
	}
	uid := ids.UUID(p.UserID)
	var row businessdb.Staff
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		cur, err := q.GetStaffForUpdate(ctx, businessdb.GetStaffForUpdateParams{ID: ids.UUID(id), BusinessID: ids.UUID(bid)})
		if db.IsNoRows(err) || (err == nil && cur.Status != staffInvited) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return err
		}
		if cur.UserID.Valid {
			if cur.UserID != uid {
				return problem.New(genproblems.NotFound, "")
			}
		} else {
			mine, err := q.PhoneHashByUser(ctx, uid)
			if db.IsNoRows(err) || (err == nil && (len(mine) == 0 || !bytes.Equal(mine, cur.InvitePhoneHash))) {
				return problem.New(genproblems.NotFound, "")
			}
			if err != nil {
				return err
			}
		}
		row, err = q.AcceptInvite(ctx, businessdb.AcceptInviteParams{UserID: uid, ID: cur.ID})
		if err != nil {
			return err
		}
		return s.writeAudit(ctx, p.UserID, "staff.accept", "staff", idString(row.ID), staffAudit(cur), staffAudit(row))
	})
	if db.IsUniqueViolation(err, "ux_staff_business_user") {
		return businessapi.StaffMember{}, problem.New(genproblems.Conflict, "you are already on this business's staff")
	}
	if err != nil {
		return businessapi.StaffMember{}, err
	}
	return s.memberView(ctx, bid, row.ID)
}

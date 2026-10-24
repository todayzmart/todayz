package business

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"

	businessapi "github.com/todayz/backend/gen/api/business"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// codeAttempts is how many random codes (referral, short code, slug suffix)
// a create tries before it gives up; a collision needs a full retry because
// a unique violation aborts the transaction.
const codeAttempts = 5

// referralCodeLen is the length of a business referral code (03).
const referralCodeLen = 6

// --- conversions ---------------------------------------------------------------

func idString(u pgtype.UUID) string {
	id, _ := ids.FromUUID(u)
	return id.String()
}

func optIDString(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := idString(u)
	return &s
}

func idStrings(us []pgtype.UUID) *[]string {
	if us == nil {
		return nil
	}
	out := make([]string, 0, len(us))
	for _, u := range us {
		out = append(out, idString(u))
	}
	return &out
}

// optUUID parses an optional ULID body field; a bad one is a 422 at path.
func optUUID(v *invalid, path string, s *string) pgtype.UUID {
	if s == nil {
		return pgtype.UUID{}
	}
	id, err := ids.Parse(*s)
	if err != nil {
		v.add(path, "must be an id")
		return pgtype.UUID{}
	}
	return ids.UUID(id)
}

// membership is the caller's active staff row in the business (the engine
// has already checked there is one).
func (s *Service) membership(ctx context.Context, uid, bid ids.ID) (authz.Membership, error) {
	m, ok, err := NewStaffResolver(s.db).Membership(ctx, uid, bid)
	if err != nil {
		return authz.Membership{}, err
	}
	if !ok {
		return authz.Membership{}, problem.New(genproblems.Forbidden, "")
	}
	return m, nil
}

func (s *Service) writeAudit(ctx context.Context, uid ids.ID, action, subjectType, subjectID string, before, after any) error {
	kind, id := actor(uid)
	if _, err := s.audit.Write(ctx, audit.Entry{
		ActorKind: kind, ActorID: id, Action: action,
		SubjectType: subjectType, SubjectID: subjectID, Before: before, After: after,
	}); err != nil {
		return fmt.Errorf("business: audit %s: %w", action, err)
	}
	return nil
}

func toBusiness(b businessdb.Business, role authz.Role) businessapi.Business {
	out := businessapi.Business{
		BrandName:    b.BrandName,
		CategoryId:   optIDString(b.CategoryID),
		CreatedAt:    b.CreatedAt.UTC(),
		Id:           idString(b.ID),
		LegalName:    b.LegalName,
		MyRole:       businessapi.StaffRole(role),
		ReferralCode: b.ReferralCode,
		Status:       businessapi.BusinessStatus(b.Status),
		SubmittedAt:  b.SubmittedAt,
		VerifiedAt:   b.VerifiedAt,
	}
	if b.Status == businessNeedsInfo {
		fields := b.NeedsInfoFields
		if fields == nil {
			fields = []string{}
		}
		out.InfoRequest = &businessapi.BusinessInfoRequest{Fields: fields, Note: b.NeedsInfoNote}
	}
	return out
}

// --- list ----------------------------------------------------------------------

func (s *Service) listBusinesses(ctx context.Context) (businessapi.BusinessList, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.BusinessList{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.BusinessList{}, err
	}
	q := s.q(ctx)
	uid := ids.UUID(p.UserID)
	rows, err := q.ListMyBusinesses(ctx, uid)
	if err != nil {
		return businessapi.BusinessList{}, fmt.Errorf("business: list: %w", err)
	}
	bids := make([]pgtype.UUID, 0, len(rows))
	for _, r := range rows {
		bids = append(bids, r.ID)
	}
	stores, err := q.ListStoresForBusinesses(ctx, bids)
	if err != nil {
		return businessapi.BusinessList{}, fmt.Errorf("business: list stores: %w", err)
	}
	out := businessapi.BusinessList{Items: make([]businessapi.BusinessSummary, 0, len(rows)), Invitations: []businessapi.StaffInvitation{}}
	for _, r := range rows {
		item := businessapi.BusinessSummary{
			BrandName: r.BrandName, Id: idString(r.ID), Role: businessapi.StaffRole(r.Role),
			Status: businessapi.BusinessStatus(r.Status), Stores: []businessapi.BizStoreRef{},
		}
		for _, st := range stores {
			if st.BusinessID != r.ID || (r.StoreIds != nil && !slices.Contains(r.StoreIds, st.ID)) {
				continue
			}
			item.Stores = append(item.Stores, businessapi.BizStoreRef{
				Id: idString(st.ID), Name: st.Name, Slug: st.Slug, Status: businessapi.StoreStatus(st.Status),
			})
		}
		out.Items = append(out.Items, item)
	}
	invites, err := q.ListMyInvitations(ctx, businessdb.ListMyInvitationsParams{
		UserID: uid, ExpiredBefore: s.clock.Now().Add(-inviteTTL),
	})
	if err != nil {
		return businessapi.BusinessList{}, fmt.Errorf("business: invitations: %w", err)
	}
	for _, in := range invites {
		out.Invitations = append(out.Invitations, businessapi.StaffInvitation{
			BrandName: in.BrandName, BusinessId: idString(in.BusinessID), InvitedAt: in.CreatedAt.UTC(),
			Role: businessapi.StaffRole(in.Role), StaffId: idString(in.ID),
		})
	}
	return out, nil
}

// --- create --------------------------------------------------------------------

func (s *Service) createBusiness(ctx context.Context, body *businessapi.BusinessCreate) (businessapi.Business, int32, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.Business{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.Business{}, 0, err
	}
	if body == nil {
		return businessapi.Business{}, 0, problem.New(genproblems.BadRequest, "")
	}
	var v invalid
	v.text("body.brandName", &body.BrandName)
	v.text("body.legalName", body.LegalName)
	category := optUUID(&v, "body.categoryId", body.CategoryId)
	if err := v.err(); err != nil {
		return businessapi.Business{}, 0, err
	}

	var b businessdb.Business
	for attempt := 0; ; attempt++ {
		code, err := randomString(codeAlphabet, referralCodeLen)
		if err != nil {
			return businessapi.Business{}, 0, err
		}
		err = s.db.InTx(ctx, func(ctx context.Context) error {
			q := s.q(ctx)
			now := s.clock.Now()
			id := ids.New(now)
			b, err = q.InsertBusiness(ctx, businessdb.InsertBusinessParams{
				ID: ids.UUID(id), BrandName: body.BrandName, LegalName: body.LegalName,
				CategoryID: category, ReferralCode: code, Now: now,
			})
			if err != nil {
				return err
			}
			if err := q.InsertKyc(ctx, businessdb.InsertKycParams{BusinessID: b.ID, Now: now}); err != nil {
				return fmt.Errorf("business: kyc row: %w", err)
			}
			if _, err := q.InsertStaff(ctx, businessdb.InsertStaffParams{
				ID: ids.UUID(ids.New(now)), BusinessID: b.ID, UserID: ids.UUID(p.UserID),
				Role: roleOwner, Status: staffActive, Now: now,
			}); err != nil {
				return fmt.Errorf("business: owner row: %w", err)
			}
			return s.writeAudit(ctx, p.UserID, "business.create", "business", id.String(), nil,
				map[string]any{"brandName": b.BrandName, "status": b.Status})
		})
		if db.IsUniqueViolation(err, "ux_businesses_referral_code") && attempt < codeAttempts-1 {
			continue
		}
		if unknown := unknownCategory(err); unknown != nil {
			return businessapi.Business{}, 0, unknown
		}
		if err != nil {
			return businessapi.Business{}, 0, fmt.Errorf("business: create: %w", err)
		}
		break
	}
	return toBusiness(b, authz.Owner), b.Version, nil
}

// --- get -------------------------------------------------------------------------

func (s *Service) getBusiness(ctx context.Context, bidStr string) (businessapi.Business, int32, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.Business{}, 0, err
	}
	dec, err := decision(ctx)
	if err != nil {
		return businessapi.Business{}, 0, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.Business{}, 0, err
	}
	b, err := s.q(ctx).GetBusiness(ctx, ids.UUID(bid))
	if db.IsNoRows(err) {
		return businessapi.Business{}, 0, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return businessapi.Business{}, 0, fmt.Errorf("business: get: %w", err)
	}
	return toBusiness(b, dec.Role), b.Version, nil
}

// --- update ----------------------------------------------------------------------

// updateBusiness changes the brand name, legal name or category.
//
// TODO(lead): only a draft or needs_info business may change here (the most
// conservative reading of 01 §3: what an admin verified stays verified). A
// verified business renaming itself would need a re-review flow.
func (s *Service) updateBusiness(ctx context.Context, bidStr string, ifMatchHdr *string, body *businessapi.BusinessUpdate) (businessapi.Business, int32, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.Business{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.Business{}, 0, err
	}
	dec, err := decision(ctx)
	if err != nil {
		return businessapi.Business{}, 0, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.Business{}, 0, err
	}
	version, err := ifMatch(ifMatchHdr)
	if err != nil {
		return businessapi.Business{}, 0, err
	}
	if body == nil {
		return businessapi.Business{}, 0, problem.New(genproblems.BadRequest, "")
	}
	params := businessdb.UpdateBusinessParams{ID: ids.UUID(bid), Version: version}
	var v invalid
	if sent(ctx, "brandName") && body.BrandName != nil {
		v.text("body.brandName", body.BrandName)
		params.SetBrandName, params.BrandName = true, *body.BrandName
	}
	if sent(ctx, "legalName") {
		v.text("body.legalName", body.LegalName)
		params.SetLegalName, params.LegalName = true, body.LegalName
	}
	if sent(ctx, "categoryId") {
		// An id that names no category fails the foreign key (D-211) and is
		// answered by unknownCategory.
		params.SetCategoryID, params.CategoryID = true, optUUID(&v, "body.categoryId", body.CategoryId)
	}
	if err := v.err(); err != nil {
		return businessapi.Business{}, 0, err
	}

	var b businessdb.Business
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		cur, err := q.GetBusiness(ctx, params.ID)
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return err
		}
		if cur.Version != version {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		if cur.Status != businessDraft && cur.Status != businessNeedsInfo {
			return problem.New(genproblems.InvalidTransition, "")
		}
		b, err = q.UpdateBusiness(ctx, params)
		if db.IsNoRows(err) {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		if err != nil {
			return err
		}
		before, after := map[string]any{}, map[string]any{}
		if params.SetBrandName {
			before["brandName"], after["brandName"] = cur.BrandName, b.BrandName
		}
		if params.SetLegalName {
			before["legalName"], after["legalName"] = cur.LegalName, b.LegalName
		}
		if params.SetCategoryID {
			before["categoryId"], after["categoryId"] = optIDString(cur.CategoryID), optIDString(b.CategoryID)
		}
		return s.writeAudit(ctx, p.UserID, "business.update", "business", bid.String(), before, after)
	})
	if unknown := unknownCategory(err); unknown != nil {
		return businessapi.Business{}, 0, unknown
	}
	if err != nil {
		return businessapi.Business{}, 0, err
	}
	return toBusiness(b, dec.Role), b.Version, nil
}

// unknownCategory turns the businesses.category_id foreign key failing (a
// well-formed id that names no category) into a 422 on body.categoryId; any
// other error gives nil.
func unknownCategory(err error) error {
	if !db.IsForeignKeyViolation(err, "fk_businesses_category_id") {
		return nil
	}
	var v invalid
	v.add("body.categoryId", "unknown category")
	return v.err()
}

// --- submit ----------------------------------------------------------------------

// storeGaps names what a store still needs before review (01 §3): address,
// city, pincode, map pin, phone and opening hours. prefix is "" for a store
// submit and "stores[i]." for a business submit.
func storeGaps(prefix string, st businessdb.CountSubmitStoresRow) []string {
	var out []string
	if st.AddressLine1 == nil {
		out = append(out, prefix+"addressLine1")
	}
	if st.City == nil {
		out = append(out, prefix+"city")
	}
	if st.Pincode == nil {
		out = append(out, prefix+"pincode")
	}
	if !st.HasLocation {
		out = append(out, prefix+"location")
	}
	if !st.HasPhone {
		out = append(out, prefix+"phone")
	}
	if h, err := ParseHours(st.Hours); err != nil || h.IsEmpty() {
		out = append(out, prefix+"hours")
	}
	return out
}

func incomplete(fields []string) error {
	p := problem.New(genproblems.SubmitIncomplete, "")
	for _, f := range fields {
		p = p.WithField(f, "required")
	}
	return p
}

// submitBusiness sends a draft or needs_info business for verification. It
// needs the category and at least one store with every detail a review needs.
func (s *Service) submitBusiness(ctx context.Context, bidStr string) (businessapi.Business, int32, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.Business{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.Business{}, 0, err
	}
	dec, err := decision(ctx)
	if err != nil {
		return businessapi.Business{}, 0, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.Business{}, 0, err
	}
	var b businessdb.Business
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		cur, err := q.GetBusiness(ctx, ids.UUID(bid))
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return err
		}
		if cur.Status != businessDraft && cur.Status != businessNeedsInfo {
			return problem.New(genproblems.InvalidTransition, "")
		}
		var missing []string
		if !cur.CategoryID.Valid {
			missing = append(missing, "categoryId")
		}
		stores, err := q.CountSubmitStores(ctx, cur.ID)
		if err != nil {
			return err
		}
		complete := false
		var storeMissing []string
		for i, st := range stores {
			gaps := storeGaps(fmt.Sprintf("stores[%d].", i), st)
			if len(gaps) == 0 {
				complete = true
				break
			}
			storeMissing = append(storeMissing, gaps...)
		}
		switch {
		case len(stores) == 0:
			missing = append(missing, "stores")
		case !complete:
			missing = append(missing, storeMissing...)
		}
		if len(missing) > 0 {
			return incomplete(missing)
		}
		b, err = q.SubmitBusiness(ctx, businessdb.SubmitBusinessParams{Now: s.clock.Now(), ID: cur.ID})
		if db.IsNoRows(err) {
			return problem.New(genproblems.InvalidTransition, "")
		}
		if err != nil {
			return err
		}
		return s.writeAudit(ctx, p.UserID, "business.submit", "business", bid.String(),
			map[string]any{"status": cur.Status}, map[string]any{"status": b.Status})
	})
	if err != nil {
		var pe *problem.Error
		if errors.As(err, &pe) {
			return businessapi.Business{}, 0, pe
		}
		return businessapi.Business{}, 0, fmt.Errorf("business: submit: %w", err)
	}
	return toBusiness(b, dec.Role), b.Version, nil
}

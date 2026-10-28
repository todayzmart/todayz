package admin

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	api "github.com/todayz/backend/gen/api/admin-businesses"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// A-03 Businesses (01 §13, D-40). The queue moves a business through
// submitted → verified | needs_info | rejected, and later suspended and back.
// A verify also puts the business's stores waiting for review live in the same
// transaction; a store added later is reviewed on A-05. KYC shows the last 4
// characters only; a reveal opens one field in full and is audited first.

// Business statuses (03 businesses.status).
const (
	bizSubmitted = "submitted"
	bizNeedsInfo = "needs_info"
	bizVerified  = "verified"
	bizRejected  = "rejected"
	bizSuspended = "suspended"
)

// kycSealField is the sealed column's field name for each reveal choice. The
// AAD is field + ":" + business id, as internal/business seals it
// (business.SealField); admin cannot import that package.
var kycSealField = map[api.AdminKycField]string{
	"pan":   "pan",
	"gstin": "gstin",
	"upi":   "upi_id",
	"bank":  "bank_account",
}

func sealAAD(field string, owner ids.ID) string { return field + ":" + owner.String() }

func (s *Service) listBusinesses(ctx context.Context, status *api.AdminBusinessStatus, limit *int, cursor *string) (api.AdminBusinessList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminBusinessList{}, err
	}
	lim, at, id, err := pageArgs(limit, cursor)
	if err != nil {
		return api.AdminBusinessList{}, err
	}
	var st *string
	if status != nil {
		v := string(*status)
		st = &v
	}
	rows, err := s.q(ctx).ListBusinesses(ctx, admindb.ListBusinessesParams{
		Status: st, BeforeAt: at, BeforeID: id,
		Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	})
	if err != nil {
		return api.AdminBusinessList{}, err
	}
	out := api.AdminBusinessList{Items: []api.AdminBusinessSummary{}, ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			last := rows[lim-1]
			lid, _ := ids.FromUUID(last.ID)
			c := encodeCursor(last.CreatedAt, lid)
			out.NextCursor = &c
			break
		}
		var city *string
		if r.City != "" {
			city = &r.City
		}
		out.Items = append(out.Items, api.AdminBusinessSummary{
			Id: idString(r.ID), BrandName: r.BrandName, LegalName: r.LegalName,
			Status: api.AdminBusinessStatus(r.Status), City: city, StoreCount: int(r.StoreCount),
			SubmittedAt: utcPtr(r.SubmittedAt), CreatedAt: r.CreatedAt.UTC(),
		})
	}
	return out, nil
}

func (s *Service) business(ctx context.Context, id ids.ID) (api.AdminBusiness, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminBusiness{}, err
	}
	q := s.q(ctx)
	u := ids.UUID(id)
	b, err := q.GetBusiness(ctx, u)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminBusiness{}, notFound()
	}
	if err != nil {
		return api.AdminBusiness{}, err
	}
	fields := b.NeedsInfoFields
	if fields == nil {
		fields = []string{}
	}
	out := api.AdminBusiness{
		Id: idString(b.ID), BrandName: b.BrandName, LegalName: b.LegalName, Status: api.AdminBusinessStatus(b.Status),
		NeedsInfoFields: fields, NeedsInfoNote: b.NeedsInfoNote, SubmittedAt: utcPtr(b.SubmittedAt),
		VerifiedAt: utcPtr(b.VerifiedAt), CheckStatus: api.AdminCheckStatus(b.CheckStatus), CheckedAt: utcPtr(b.CheckedAt),
		CheckNote: b.CheckNote, CreatedAt: b.CreatedAt.UTC(), ServerNow: s.now(),
		Kyc:    api.AdminKyc{Status: "missing"},
		Stores: []api.AdminBusinessStore{}, Staff: []api.AdminBusinessStaff{}, Documents: []api.AdminBusinessDocument{},
	}
	k, err := q.BusinessKyc(ctx, u)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return api.AdminBusiness{}, err
	default:
		out.Kyc = api.AdminKyc{
			Status: api.AdminKycStatus(k.Status), LegalName: k.LegalName, PanLast4: k.PanLast4, GstinLast4: k.GstinLast4,
			UpiLast4: k.UpiIDLast4, BankLast4: k.BankAccountLast4, BankIfsc: k.BankIfsc, Notes: k.Notes,
			VerifiedAt: utcPtr(k.VerifiedAt),
		}
	}
	stores, err := q.BusinessStores(ctx, u)
	if err != nil {
		return api.AdminBusiness{}, err
	}
	for _, r := range stores {
		out.Stores = append(out.Stores, api.AdminBusinessStore{
			Id: idString(r.ID), Name: r.Name, Status: api.AdminBusinessStoreStatus(r.Status),
			Locality: r.Locality, City: r.City, LiveAt: utcPtr(r.LiveAt), CreatedAt: r.CreatedAt.UTC(),
		})
	}
	staff, err := q.BusinessStaff(ctx, u)
	if err != nil {
		return api.AdminBusiness{}, err
	}
	for _, r := range staff {
		m := api.AdminBusinessStaff{Id: idString(r.ID), Role: api.AdminBusinessStaffRole(r.Role), Status: api.AdminBusinessStaffStatus(r.Status)}
		if r.Status == "invited" {
			// Only what the inviter typed, as the business app shows it; the
			// account behind the phone stays hidden until the invite is accepted.
			sid, _ := ids.FromUUID(r.ID)
			m.PhoneMasked = s.maskSealed(ctx, sealAAD("phone", sid), r.InvitePhoneEnc)
		} else {
			m.Name = r.Name
			m.PhoneMasked = s.maskedPhone(ctx, r.UserPhoneEnc)
		}
		out.Staff = append(out.Staff, m)
	}
	docs, err := q.BusinessDocuments(ctx, u)
	if err != nil {
		return api.AdminBusiness{}, err
	}
	for _, d := range docs {
		out.Documents = append(out.Documents, api.AdminBusinessDocument{
			Id: idString(d.ID), Kind: api.AdminBusinessDocumentKind(d.Kind), ContentType: api.AdminBusinessDocumentContentType(d.ContentType),
			SizeBytes: d.SizeBytes, StoreId: optID(d.StoreID), Status: api.AdminBusinessDocumentStatus(d.Status),
			RejectReason: d.RejectReason, CreatedAt: d.CreatedAt.UTC(), ReviewedAt: utcPtr(d.ReviewedAt),
		})
	}
	return out, nil
}

// businessMove locks the business, checks its status is one of from, runs
// change and writes the audit row, all in one transaction.
func (s *Service) businessMove(ctx context.Context, actor, id ids.ID, from []string, action string,
	change func(ctx context.Context, q *admindb.Queries, status string) (before, after map[string]any, err error),
) (api.AdminBusiness, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminBusiness{}, err
	}
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		status, err := q.LockBusiness(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if from != nil && !slices.Contains(from, status) {
			return invalidTransition("")
		}
		before, after, err := change(ctx, q, status)
		if err != nil {
			return err
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: action,
			SubjectType: "business", SubjectID: id.String(), Before: before, After: after,
		})
		return err
	})
	if err != nil {
		return api.AdminBusiness{}, err
	}
	return s.business(ctx, id)
}

// setBusinessStatus is reject, suspend and reinstate: a status move with a reason.
func (s *Service) setBusinessStatus(ctx context.Context, actor, id ids.ID, from []string, to, action, reason string) (api.AdminBusiness, error) {
	reason, err := cleanReason(reason)
	if err != nil {
		return api.AdminBusiness{}, err
	}
	return s.businessMove(ctx, actor, id, from, action, func(ctx context.Context, q *admindb.Queries, status string) (map[string]any, map[string]any, error) {
		if err := q.SetBusinessStatus(ctx, admindb.SetBusinessStatusParams{Status: to, ID: ids.UUID(id)}); err != nil {
			return nil, nil, err
		}
		return map[string]any{"status": status}, map[string]any{"status": to, "reason": reason}, nil
	})
}

func (s *Service) verifyBusiness(ctx context.Context, actor, id ids.ID, note *string) (api.AdminBusiness, error) {
	n, err := optNote(note, "body.note")
	if err != nil {
		return api.AdminBusiness{}, err
	}
	return s.businessMove(ctx, actor, id, []string{bizSubmitted}, "business.verify", func(ctx context.Context, q *admindb.Queries, status string) (map[string]any, map[string]any, error) {
		now := s.now()
		by := ids.UUID(actor)
		if err := q.VerifyBusiness(ctx, admindb.VerifyBusinessParams{Now: &now, By: by, ID: ids.UUID(id)}); err != nil {
			return nil, nil, err
		}
		live, err := q.GoLiveBusinessStores(ctx, admindb.GoLiveBusinessStoresParams{Now: &now, By: by, BusinessID: ids.UUID(id)})
		if err != nil {
			return nil, nil, err
		}
		after := map[string]any{"status": bizVerified, "storesLive": uuidStrings(live)}
		if n != nil {
			after["note"] = *n
		}
		return map[string]any{"status": status}, after, nil
	})
}

func (s *Service) requestBusinessInfo(ctx context.Context, actor, id ids.ID, body api.AdminBusinessInfoRequest) (api.AdminBusiness, error) {
	note := strings.TrimSpace(body.Note)
	if note == "" || len([]rune(note)) > maxReason {
		return api.AdminBusiness{}, fieldErr("body.note", "1 to 500 characters")
	}
	if err := noPhone("body.note", note); err != nil {
		return api.AdminBusiness{}, err
	}
	fields := make([]string, 0, len(body.Fields))
	for _, f := range body.Fields {
		if !slices.Contains(fields, string(f)) {
			fields = append(fields, string(f))
		}
	}
	if len(fields) == 0 {
		return api.AdminBusiness{}, fieldErr("body.fields", "at least one field")
	}
	return s.businessMove(ctx, actor, id, []string{bizSubmitted}, "business.request_info", func(ctx context.Context, q *admindb.Queries, status string) (map[string]any, map[string]any, error) {
		if err := q.RequestBusinessInfo(ctx, admindb.RequestBusinessInfoParams{Fields: fields, Note: &note, ID: ids.UUID(id)}); err != nil {
			return nil, nil, err
		}
		return map[string]any{"status": status}, map[string]any{"status": bizNeedsInfo, "fields": fields, "note": note}, nil
	})
}

// reviewKyc moves the KYC record as a whole (it has one status, 01 §13):
// submitted → verified | rejected, or rejected → verified after a fix.
func (s *Service) reviewKyc(ctx context.Context, actor, id ids.ID, body api.AdminKycReviewRequest) (api.AdminBusiness, error) {
	to := string(body.Decision)
	note, err := optNote(body.Note, "body.note")
	if err != nil {
		return api.AdminBusiness{}, err
	}
	if to == "rejected" && note == nil {
		return api.AdminBusiness{}, fieldErr("body.note", "required to reject")
	}
	return s.businessMove(ctx, actor, id, nil, "business.kyc_review", func(ctx context.Context, q *admindb.Queries, _ string) (map[string]any, map[string]any, error) {
		cur, err := q.LockBusinessKyc(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, invalidTransition("The business has no KYC details yet.")
		}
		if err != nil {
			return nil, nil, err
		}
		if cur != "submitted" && (cur != "rejected" || to != "verified") {
			return nil, nil, invalidTransition("")
		}
		var notes *string
		if to == "rejected" {
			notes = note
		}
		if err := q.ReviewBusinessKyc(ctx, admindb.ReviewBusinessKycParams{
			Status: to, Notes: notes, By: ids.UUID(actor), Now: s.now(), BusinessID: ids.UUID(id),
		}); err != nil {
			return nil, nil, err
		}
		after := map[string]any{"kycStatus": to}
		if note != nil {
			after["note"] = *note
		}
		return map[string]any{"kycStatus": cur}, after, nil
	})
}

// reviewDocument accepts or rejects one uploaded document; the other decision
// corrects an earlier one. A document still pending upload cannot be reviewed.
func (s *Service) reviewDocument(ctx context.Context, actor, id, doc ids.ID, body api.AdminDocumentReviewRequest) (api.AdminBusiness, error) {
	to := string(body.Decision)
	reason, err := optNote(body.Reason, "body.reason")
	if err != nil {
		return api.AdminBusiness{}, err
	}
	if to == "rejected" && reason == nil {
		return api.AdminBusiness{}, fieldErr("body.reason", "required to reject")
	}
	if to == "accepted" {
		reason = nil
	}
	return s.businessMove(ctx, actor, id, nil, "business.document_review", func(ctx context.Context, q *admindb.Queries, _ string) (map[string]any, map[string]any, error) {
		cur, err := q.LockBusinessDocument(ctx, admindb.LockBusinessDocumentParams{ID: ids.UUID(doc), BusinessID: ids.UUID(id)})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, notFound()
		}
		if err != nil {
			return nil, nil, err
		}
		if cur == "pending" || cur == to {
			return nil, nil, invalidTransition("")
		}
		now := s.now()
		if err := q.ReviewBusinessDocument(ctx, admindb.ReviewBusinessDocumentParams{
			Status: to, RejectReason: reason, By: ids.UUID(actor), Now: &now, ID: ids.UUID(doc),
		}); err != nil {
			return nil, nil, err
		}
		after := map[string]any{"documentId": doc.String(), "status": to}
		if reason != nil {
			after["reason"] = *reason
		}
		return map[string]any{"documentId": doc.String(), "status": cur}, after, nil
	})
}

// revealKyc opens one KYC field in full. The audit row (field name only) is
// committed before the value is returned.
func (s *Service) revealKyc(ctx context.Context, actor, id ids.ID, field api.AdminKycField) (api.AdminKycReveal, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminKycReveal{}, err
	}
	col, ok := kycSealField[field]
	if !ok {
		return api.AdminKycReveal{}, fieldErr("body.field", "one of pan, gstin, upi, bank")
	}
	if s.seal == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "admin: KYC reveal needs PII_ENC_KEY")
		return api.AdminKycReveal{}, unavailable()
	}
	var sealed []byte
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if _, err := q.LockBusiness(ctx, ids.UUID(id)); errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		} else if err != nil {
			return err
		}
		k, err := q.BusinessKycSealed(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		switch col {
		case "pan":
			sealed = k.PanEnc
		case "gstin":
			sealed = k.GstinEnc
		case "upi_id":
			sealed = k.UpiIDEnc
		case "bank_account":
			sealed = k.BankAccountEnc
		}
		if len(sealed) == 0 {
			return notFound()
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "business.kyc_reveal",
			SubjectType: "business", SubjectID: id.String(), After: map[string]any{"field": string(field)},
		})
		return err
	})
	if err != nil {
		return api.AdminKycReveal{}, err
	}
	plain, err := s.seal.Open(sealAAD(col, id), sealed)
	if err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "admin: a stored KYC field did not open", "field", string(field))
		return api.AdminKycReveal{}, err
	}
	return api.AdminKycReveal{Field: field, Value: string(plain)}, nil
}

// optNote trims an optional note; empty after trimming is no note.
func optNote(n *string, path string) (*string, error) {
	if n == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*n)
	if v == "" {
		return nil, nil
	}
	if len([]rune(v)) > maxReason {
		return nil, fieldErr(path, "at most 500 characters")
	}
	return &v, noPhone(path, v)
}

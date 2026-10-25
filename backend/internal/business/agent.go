package business

// Field agents (W3-06; A-04, A-15, 01 §14, D-39, D-64): /v1/admin/field/*
// and /v1/admin/field-agents. An agent makes a business for a shop from the
// admin console (businesses.onboarded_by is the agent), adds its store,
// photos and items, and the owner confirms with a code sent to their phone,
// which submits the business for verification.
//
// The engine's grant for a field agent is conditional and is applied here:
// Own is only a business whose onboarded_by is the caller (another agent's is
// 403); OwnWindow is that and only while the business is onboarding (draft,
// submitted, needs_info) or within field_agent_edit_window_days of its
// verification. Admin and super_admin are Allow. A field agent's change is
// audited as an admin's (audit.ActorAdmin), by user id.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	adminfieldapi "github.com/todayz/backend/gen/api/admin-field"
	businessapi "github.com/todayz/backend/gen/api/business"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
)

// FieldOTP is the owner code's rules: the sign-in code's (identity), passed
// in from cmd so the two stay the same.
type FieldOTP struct {
	Digits      int
	TTL         time.Duration
	MaxAttempts int
	ResendGap   time.Duration
	Window      time.Duration // the send limit's window
	PerPhone    int           // sends per number per Window
}

// FieldSMS sends the owner code (identity's sign-in sender).
type FieldSMS interface {
	SendOTP(ctx context.Context, e164, code string) error
}

// FieldAccount is the user of the owner's number.
type FieldAccount struct {
	UserID ids.ID
	Status string // users.status
}

// FieldAccounts finds or makes the owner's account by phone, inside the
// caller's transaction (identity.Accounts).
type FieldAccounts interface {
	FindOrCreateByPhone(ctx context.Context, phone string, now time.Time) (FieldAccount, error)
}

// CatalogueItem is a store item as the field routes show it (catalogue).
type CatalogueItem struct {
	ID, StoreID, ProductID ids.ID
	Name                   string
	Brand, Variant         *string
	PackSize               float64
	Unit                   string
	PricePaise             int64
	MrpPaise               *int64
	ProductMrpPaise        *int64
	ProductStatus          string
	OnHand                 int32
	IsListed               bool
	CreatedAt, UpdatedAt   time.Time
}

// CatalogueNewProduct is a product the master catalogue does not have yet.
type CatalogueNewProduct struct {
	Name       string
	CategoryID ids.ID
	Variant    *string
	PackSize   float64
	Unit       string
	MrpPaise   *int64
	Barcode    *string
}

// CatalogueItemCreate lists one product in a store.
type CatalogueItemCreate struct {
	StoreID      ids.ID
	ProductID    *ids.ID
	NewProduct   *CatalogueNewProduct
	PricePaise   int64
	MrpPaise     *int64
	OpeningStock int32
	IsListed     bool
	AgentID      ids.ID
}

// CatalogueItemUpdate changes an item's price, MRP or listing.
type CatalogueItemUpdate struct {
	StoreID, ItemID ids.ID
	SetPrice        bool
	PricePaise      int64
	SetMrp          bool
	MrpPaise        *int64
	SetListed       bool
	IsListed        bool
}

// CatalogueProduct is a master product an agent may list.
type CatalogueProduct struct {
	ID, CategoryID ids.ID
	Name           string
	Brand, Variant *string
	PackSize       float64
	Unit           string
	MrpPaise       *int64
	InStore        bool
}

// FieldCatalogue is what the field routes need from the catalogue. The
// writes run inside the caller's transaction.
type FieldCatalogue interface {
	SearchProducts(ctx context.Context, storeID ids.ID, q, barcode *string) ([]CatalogueProduct, error)
	ListItems(ctx context.Context, storeID, before ids.ID, limit int32) ([]CatalogueItem, bool, error)
	Item(ctx context.Context, storeID, itemID ids.ID) (CatalogueItem, error)
	CreateItem(ctx context.Context, in CatalogueItemCreate) (ids.ID, error)
	UpdateItem(ctx context.Context, in CatalogueItemUpdate) error
}

// FieldDeps is what the field routes need beyond the business service. Any
// of them may be missing in a route-only test: the routes then answer 503.
type FieldDeps struct {
	Settings  *settings.Cache
	SMS       FieldSMS
	Accounts  FieldAccounts
	Catalogue FieldCatalogue
	OTP       FieldOTP
	// AllowTestCode accepts 000000 for the reserved test numbers (local and
	// test only), as sign-in does.
	AllowTestCode bool
}

// Field implements the field agent routes.
type Field struct {
	s *Service
	d FieldDeps
}

// NewField builds the field routes on the business service.
func NewField(svc *Service, d FieldDeps) *Field { return &Field{s: svc, d: d} }

// fieldOps maps every field operation to its action (slot 15).
var fieldOps = map[string]string{
	"ListFieldBusinesses":  policy.AdminBusinessOnboard,
	"CreateFieldBusiness":  policy.AdminBusinessOnboard,
	"GetFieldBusiness":     policy.AdminBusinessOnboard,
	"UpdateFieldBusiness":  policy.AdminBusinessOnboard,
	"SendFieldOwnerCode":   policy.AdminBusinessOnboard,
	"VerifyFieldOwnerCode": policy.AdminBusinessOnboard,
	"CreateFieldDocument":  policy.AdminBusinessOnboard,
	"ConfirmFieldDocument": policy.AdminBusinessOnboard,
	"CreateFieldStore":     policy.AdminBusinessOnboard,
	"ListFieldItems":       policy.AdminBusinessOnboard,
	"SearchFieldProducts":  policy.AdminBusinessOnboard,
	"CreateFieldItem":      policy.AdminFieldItemsEdit,
	"UpdateFieldItem":      policy.AdminFieldItemsEdit,
	"GetFieldMyStats":      policy.AdminFieldOwnStats,
	"ListFieldAgents":      policy.AdminFieldAgentsView,
}

// RegisterField mounts /v1/admin/field/* and /v1/admin/field-agents on r.
func RegisterField(r chi.Router, f *Field, az authz.Authorizer) {
	adminfieldapi.Register(r.With(bodyKeys), fieldHandlers{f},
		adminfieldapi.StrictMiddlewareFunc(authz.Strict[adminfieldapi.StrictHandlerFunc](az, fieldOps)),
	)
}

var errFieldUnavailable = errors.New("business: field routes are not configured")

// fieldCall is the caller of a field route and the engine's decision.
type fieldCall struct {
	p   authn.Principal
	dec authz.Decision
}

// agent reports whether the caller's grant is a field agent's (conditional).
func (c fieldCall) agent() bool { return c.dec.Grant != authz.Allow }

func (f *Field) call(ctx context.Context) (fieldCall, error) {
	if err := f.s.ready(ctx); err != nil {
		return fieldCall{}, err
	}
	if f.d.Settings == nil || f.d.Catalogue == nil || f.d.Accounts == nil || f.d.SMS == nil {
		return fieldCall{}, problem.Wrap(genproblems.ServiceUnavailable, errFieldUnavailable)
	}
	p, err := principal(ctx)
	if err != nil {
		return fieldCall{}, err
	}
	dec, err := decision(ctx)
	if err != nil {
		return fieldCall{}, err
	}
	return fieldCall{p: p, dec: dec}, nil
}

// business loads a business the caller may work on: one an agent onboarded
// (a business the shop made itself is not on these routes, 404), and for a
// field agent only one they onboarded (403).
func (f *Field) business(ctx context.Context, c fieldCall, bid ids.ID) (businessdb.Business, error) {
	b, err := f.s.q(ctx).GetBusiness(ctx, ids.UUID(bid))
	if db.IsNoRows(err) || (err == nil && !b.OnboardedBy.Valid) {
		return businessdb.Business{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return businessdb.Business{}, fmt.Errorf("business: field business: %w", err)
	}
	if c.agent() && b.OnboardedBy != ids.UUID(c.p.UserID) {
		return businessdb.Business{}, problem.New(genproblems.Forbidden, "")
	}
	return b, nil
}

// window is field_agent_edit_window_days (D-39).
func (f *Field) window(ctx context.Context) (time.Duration, error) {
	w, err := f.d.Settings.Duration(ctx, settings.FieldAgentEditWindowDays)
	if err != nil {
		return 0, fmt.Errorf("business: edit window: %w", err)
	}
	return w, nil
}

// editable reports whether the caller may still change the business's items,
// stores and photos, and until when (verified_at plus the window, once
// verified). A business onboarding (draft, submitted, needs_info) is
// editable; a verified one by a field agent only up to and including the
// window's end, by an admin always; a rejected or suspended one by nobody.
func editable(b businessdb.Business, agent bool, window time.Duration, now time.Time) (bool, *time.Time) {
	var until *time.Time
	if b.VerifiedAt != nil {
		u := b.VerifiedAt.Add(window).UTC()
		until = &u
	}
	switch b.Status {
	case businessDraft, businessSubmitted, businessNeedsInfo:
		return true, until
	case businessVerified:
		return !agent || (until != nil && !now.After(*until)), until
	}
	return false, until
}

// mayEdit is editable as a 403 for the caller.
func (f *Field) mayEdit(ctx context.Context, c fieldCall, b businessdb.Business) error {
	w, err := f.window(ctx)
	if err != nil {
		return err
	}
	if ok, _ := editable(b, c.agent(), w, f.s.clock.Now()); !ok {
		if b.Status != businessVerified {
			return problem.New(genproblems.Forbidden, "this business is rejected or suspended")
		}
		return problem.New(genproblems.Forbidden, "the edit window for this business has ended")
	}
	return nil
}

// storeOf checks the store belongs to the business (404 otherwise).
func (f *Field) storeOf(ctx context.Context, bid, sid ids.ID) error {
	owner, err := f.s.q(ctx).StoreBusiness(ctx, ids.UUID(sid))
	if db.IsNoRows(err) || (err == nil && owner != ids.UUID(bid)) {
		return problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return fmt.Errorf("business: field store: %w", err)
	}
	return nil
}

// --- views -----------------------------------------------------------------------

// ownerPhone opens the owner's number: the account's once the owner is
// active, else the invite's. ok false means there is none to show.
func (s *Service) ownerPhone(o businessdb.FieldOwnersRow) (string, bool, error) {
	if o.Status == staffActive && len(o.UserPhoneEnc) > 0 {
		plain, err := s.seal.Open(identityPhoneField, o.UserPhoneEnc)
		if err != nil {
			return "", false, fmt.Errorf("business: open owner phone: %w", err)
		}
		return string(plain), true, nil
	}
	if len(o.InvitePhoneEnc) > 0 {
		sid, _ := ids.FromUUID(o.ID)
		plain, err := s.seal.Open(SealField(FieldPhone, sid), o.InvitePhoneEnc)
		if err != nil {
			return "", false, fmt.Errorf("business: open owner phone: %w", err)
		}
		return string(plain), true, nil
	}
	return "", false, nil
}

// views shapes businesses for the field routes, with their stores, photo
// counts and owner, three queries for the lot.
func (f *Field) views(ctx context.Context, c fieldCall, bs []businessdb.Business) ([]adminfieldapi.FieldBusiness, error) {
	out := make([]adminfieldapi.FieldBusiness, 0, len(bs))
	if len(bs) == 0 {
		return out, nil
	}
	w, err := f.window(ctx)
	if err != nil {
		return nil, err
	}
	now := f.s.clock.Now()
	keys := make([]pgtype.UUID, 0, len(bs))
	for _, b := range bs {
		keys = append(keys, b.ID)
	}
	q := f.s.q(ctx)
	stores, err := q.FieldStoreSummaries(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("business: field stores: %w", err)
	}
	photos, err := q.FieldPhotoCounts(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("business: field photos: %w", err)
	}
	owners, err := q.FieldOwners(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("business: field owners: %w", err)
	}
	storesOf := map[pgtype.UUID][]adminfieldapi.FieldStoreSummary{}
	for _, st := range stores {
		storesOf[st.BusinessID] = append(storesOf[st.BusinessID], adminfieldapi.FieldStoreSummary{
			Id: idString(st.ID), ItemCount: int(st.ItemCount), Locality: st.Locality, Name: st.Name,
			Status: adminfieldapi.FieldStoreStatus(st.Status),
		})
	}
	photosOf := map[pgtype.UUID]int{}
	for _, p := range photos {
		photosOf[p.BusinessID] = int(p.Photos)
	}
	ownerOf := map[pgtype.UUID]businessdb.FieldOwnersRow{}
	for _, o := range owners {
		ownerOf[o.BusinessID] = o
	}
	for _, b := range bs {
		ok, until := editable(b, c.agent(), w, now)
		v := adminfieldapi.FieldBusiness{
			BrandName: b.BrandName, CategoryId: optIDString(b.CategoryID), CreatedAt: b.CreatedAt.UTC(),
			Id: idString(b.ID), ItemsEditable: ok, LegalName: b.LegalName, OnboardedBy: optIDString(b.OnboardedBy),
			PhotoCount: photosOf[b.ID], Status: adminfieldapi.FieldBusinessStatus(b.Status),
			Stores: storesOf[b.ID], UpdatedAt: b.UpdatedAt.UTC(), VerifiedAt: b.VerifiedAt,
		}
		if b.Status == businessVerified {
			v.EditableUntil = until
		}
		if v.Stores == nil {
			v.Stores = []adminfieldapi.FieldStoreSummary{}
		}
		if o, found := ownerOf[b.ID]; found {
			v.OwnerConfirmed = o.Status == staffActive
			phone, has, err := f.s.ownerPhone(o)
			if err != nil {
				return nil, err
			}
			if has {
				v.OwnerPhoneMasked = ptr(pii.MaskPhone(phone))
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (f *Field) view(ctx context.Context, c fieldCall, b businessdb.Business) (adminfieldapi.FieldBusiness, error) {
	vs, err := f.views(ctx, c, []businessdb.Business{b})
	if err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	return vs[0], nil
}

// --- paging ------------------------------------------------------------------------

// fieldPage reads limit and cursor (the last id of the previous page).
func fieldPage(limit *int, cursor *string) (int32, ids.ID, error) {
	lim := int32(20) //settings:ok the contract's default page size
	if limit != nil {
		lim = int32(*limit) //nolint:gosec // the validator bounds limit to 1..100
	}
	if cursor == nil {
		return lim, ids.Zero, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(*cursor)
	if err == nil {
		var id ids.ID
		if id, err = ids.Parse(string(raw)); err == nil {
			return lim, id, nil
		}
	}
	return 0, ids.Zero, problem.New(genproblems.ValidationFailed, "").WithField("query.cursor", "not a cursor from this list")
}

func fieldCursor(id ids.ID) *string {
	c := base64.RawURLEncoding.EncodeToString([]byte(id.String()))
	return &c
}

func optUUIDOf(id ids.ID) pgtype.UUID {
	if id == ids.Zero {
		return pgtype.UUID{}
	}
	return ids.UUID(id)
}

// --- businesses ---------------------------------------------------------------------

func (f *Field) list(ctx context.Context, params adminfieldapi.ListFieldBusinessesParams) (adminfieldapi.FieldBusinessList, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldBusinessList{}, err
	}
	lim, before, err := fieldPage(params.Limit, params.Cursor)
	if err != nil {
		return adminfieldapi.FieldBusinessList{}, err
	}
	agent := pgtype.UUID{}
	if params.AgentId != nil {
		id, err := ids.Parse(*params.AgentId)
		if err != nil {
			return adminfieldapi.FieldBusinessList{}, problem.New(genproblems.ValidationFailed, "").WithField("query.agentId", "must be an id")
		}
		agent = ids.UUID(id)
	}
	if c.agent() {
		// A field agent sees their own only; asking for another's is 403.
		if agent.Valid && agent != ids.UUID(c.p.UserID) {
			return adminfieldapi.FieldBusinessList{}, problem.New(genproblems.Forbidden, "")
		}
		agent = ids.UUID(c.p.UserID)
	}
	var status *string
	if params.Status != nil {
		status = ptr(string(*params.Status))
	}
	rows, err := f.s.q(ctx).ListFieldBusinesses(ctx, businessdb.ListFieldBusinessesParams{
		AgentID: agent, Status: status, BeforeID: optUUIDOf(before), Lim: lim + 1,
	})
	if err != nil {
		return adminfieldapi.FieldBusinessList{}, fmt.Errorf("business: field list: %w", err)
	}
	out := adminfieldapi.FieldBusinessList{ServerNow: f.s.clock.Now().UTC()}
	if len(rows) > int(lim) {
		rows = rows[:lim]
		last, _ := ids.FromUUID(rows[len(rows)-1].ID)
		out.NextCursor = fieldCursor(last)
	}
	out.Items, err = f.views(ctx, c, rows)
	return out, err
}

func (f *Field) get(ctx context.Context, bidStr string) (adminfieldapi.FieldBusiness, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	b, err := f.business(ctx, c, bid)
	if err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	return f.view(ctx, c, b)
}

// ownerNumber reads the owner's number from a body: an Indian mobile, and
// not the caller's own (an agent cannot confirm their own business).
func (f *Field) ownerNumber(ctx context.Context, c fieldCall, phone string) (string, error) {
	e164, ok := pii.NormalizePhone(phone)
	if !ok {
		return "", problem.New(genproblems.ValidationFailed, "").WithField("body.ownerPhone", "must be an Indian mobile number")
	}
	mine, err := f.s.q(ctx).PhoneHashByUser(ctx, ids.UUID(c.p.UserID))
	if err != nil && !db.IsNoRows(err) {
		return "", fmt.Errorf("business: caller phone: %w", err)
	}
	if len(mine) > 0 && string(mine) == string(f.s.hash.Phone(e164)) {
		return "", problem.New(genproblems.ValidationFailed, "").WithField("body.ownerPhone", "must be the owner's number, not yours")
	}
	if err := f.ownerAccountOpen(ctx, e164); err != nil {
		return "", err
	}
	return e164, nil
}

// ownerAccountOpen refuses a number whose account is blocked (or deleted
// but still holding the number): it gets no invite and no code. A number
// with no account yet is fine; the right code makes one. The refusal is the
// same 422 as any other unusable owner number (errOwnerNumber), so it says
// no more about the account than that.
func (f *Field) ownerAccountOpen(ctx context.Context, e164 string) error {
	status, err := f.s.q(ctx).AccountStatusByPhone(ctx, f.s.hash.Phone(e164))
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("business: owner account status: %w", err)
	}
	if status != userActive {
		return errOwnerNumber()
	}
	return nil
}

// errOwnerNumber is the refusal of a number that cannot own a business.
func errOwnerNumber() error {
	return problem.New(genproblems.ValidationFailed, "").WithField("body.ownerPhone", "this number cannot own a business")
}

// sealOwnerPhone seals the owner's number for their staff row.
func (s *Service) sealOwnerPhone(staffID pgtype.UUID, e164 string) ([]byte, error) {
	sid, _ := ids.FromUUID(staffID)
	enc, err := s.seal.Seal(SealField(FieldPhone, sid), []byte(e164))
	if err != nil {
		return nil, fmt.Errorf("business: seal owner phone: %w", err)
	}
	return enc, nil
}

// create makes a draft business onboarded by the caller, its KYC row and the
// owner's invite (bound to the number, so the owner can also take over from
// the business app by accepting it).
func (f *Field) create(ctx context.Context, body *adminfieldapi.FieldBusinessCreate) (adminfieldapi.FieldBusiness, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	if body == nil {
		return adminfieldapi.FieldBusiness{}, problem.New(genproblems.BadRequest, "")
	}
	var v invalid
	v.text("body.brandName", &body.BrandName)
	v.text("body.legalName", body.LegalName)
	category := optUUID(&v, "body.categoryId", body.CategoryId)
	if err := v.err(); err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	e164, err := f.ownerNumber(ctx, c, body.OwnerPhone)
	if err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	hash := f.s.hash.Phone(e164)

	var b businessdb.Business
	for attempt := 0; ; attempt++ {
		code, err := randomString(codeAlphabet, referralCodeLen)
		if err != nil {
			return adminfieldapi.FieldBusiness{}, err
		}
		err = f.s.db.InTx(ctx, func(ctx context.Context) error {
			q := f.s.q(ctx)
			now := f.s.clock.Now()
			id := ids.New(now)
			b, err = q.InsertFieldBusiness(ctx, businessdb.InsertFieldBusinessParams{
				ID: ids.UUID(id), BrandName: body.BrandName, LegalName: body.LegalName, CategoryID: category,
				ReferralCode: code, OnboardedBy: ids.UUID(c.p.UserID), Now: now,
			})
			if err != nil {
				return err
			}
			if err := q.InsertKyc(ctx, businessdb.InsertKycParams{BusinessID: b.ID, Now: now}); err != nil {
				return fmt.Errorf("business: kyc row: %w", err)
			}
			staffID := ids.UUID(ids.New(now))
			enc, err := f.s.sealOwnerPhone(staffID, e164)
			if err != nil {
				return err
			}
			if _, err := q.InsertStaff(ctx, businessdb.InsertStaffParams{
				ID: staffID, BusinessID: b.ID, Role: roleOwner, Status: staffInvited,
				InvitedBy: ids.UUID(c.p.UserID), InvitePhoneHash: hash, InvitePhoneEnc: enc, InvitedAt: &now, Now: now,
			}); err != nil {
				return fmt.Errorf("business: owner invite: %w", err)
			}
			return f.s.writeAuditAs(ctx, audit.ActorAdmin, c.p.UserID, "business.create", "business", id.String(), nil,
				map[string]any{"brandName": b.BrandName, "status": b.Status, "onboardedBy": c.p.UserID.String()})
		})
		if db.IsUniqueViolation(err, "ux_businesses_referral_code") && attempt < codeAttempts-1 {
			continue
		}
		if unknown := unknownCategory(err); unknown != nil {
			return adminfieldapi.FieldBusiness{}, unknown
		}
		if err != nil {
			return adminfieldapi.FieldBusiness{}, fmt.Errorf("business: field create: %w", err)
		}
		break
	}
	return f.view(ctx, c, b)
}

// update changes a draft or needs_info business; the owner's number changes
// only before the owner confirmed, and a change ends any code already sent.
func (f *Field) update(ctx context.Context, bidStr string, body *adminfieldapi.FieldBusinessUpdate) (adminfieldapi.FieldBusiness, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	if body == nil {
		return adminfieldapi.FieldBusiness{}, problem.New(genproblems.BadRequest, "")
	}
	params := businessdb.UpdateBusinessParams{ID: ids.UUID(bid)}
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
		params.SetCategoryID, params.CategoryID = true, optUUID(&v, "body.categoryId", body.CategoryId)
	}
	if err := v.err(); err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	var e164 string
	if body.OwnerPhone != nil {
		if e164, err = f.ownerNumber(ctx, c, *body.OwnerPhone); err != nil {
			return adminfieldapi.FieldBusiness{}, err
		}
	}

	var b businessdb.Business
	err = f.s.db.InTx(ctx, func(ctx context.Context) error {
		q := f.s.q(ctx)
		cur, err := f.business(ctx, c, bid)
		if err != nil {
			return err
		}
		if cur.Status != businessDraft && cur.Status != businessNeedsInfo {
			return problem.New(genproblems.InvalidTransition, "")
		}
		b = cur
		before, after := map[string]any{}, map[string]any{}
		if params.SetBrandName || params.SetLegalName || params.SetCategoryID {
			params.Version = cur.Version
			b, err = q.UpdateBusiness(ctx, params)
			if db.IsNoRows(err) {
				return problem.New(genproblems.Conflict, "the business changed; try again")
			}
			if err != nil {
				return err
			}
			if params.SetBrandName {
				before["brandName"], after["brandName"] = cur.BrandName, b.BrandName
			}
			if params.SetLegalName {
				before["legalName"], after["legalName"] = cur.LegalName, b.LegalName
			}
			if params.SetCategoryID {
				before["categoryId"], after["categoryId"] = optIDString(cur.CategoryID), optIDString(b.CategoryID)
			}
		}
		if e164 != "" {
			owner, err := q.FieldOwnerForUpdate(ctx, cur.ID)
			if err != nil {
				return fmt.Errorf("business: owner row: %w", err)
			}
			if owner.Status != staffInvited {
				return problem.New(genproblems.InvalidTransition, "the owner has confirmed; their number cannot change here")
			}
			enc, err := f.s.sealOwnerPhone(owner.ID, e164)
			if err != nil {
				return err
			}
			now := f.s.clock.Now()
			if err := q.ReplaceOwnerInvite(ctx, businessdb.ReplaceOwnerInviteParams{
				InvitePhoneHash: f.s.hash.Phone(e164), InvitePhoneEnc: enc, Now: now, ID: owner.ID,
			}); err != nil {
				return err
			}
			if err := q.EndOwnerCodes(ctx, businessdb.EndOwnerCodesParams{Now: now, BusinessID: cur.ID}); err != nil {
				return err
			}
			// The number itself is never audited.
			after["ownerPhone"] = "changed"
		}
		if len(after) == 0 {
			return nil
		}
		return f.s.writeAuditAs(ctx, audit.ActorAdmin, c.p.UserID, "business.update", "business", bid.String(), before, after)
	})
	if unknown := unknownCategory(err); unknown != nil {
		return adminfieldapi.FieldBusiness{}, unknown
	}
	if err != nil {
		return adminfieldapi.FieldBusiness{}, err
	}
	return f.view(ctx, c, b)
}

// --- stores and photos -----------------------------------------------------------------

func fieldRanges(day *adminfieldapi.FieldHoursDay) *businessapi.HoursDay {
	if day == nil {
		return nil
	}
	out := make(businessapi.HoursDay, 0, len(*day))
	for _, r := range *day {
		out = append(out, businessapi.HoursRange{Open: r.Open, Close: r.Close})
	}
	return &out
}

// storeCreate is the business app's StoreCreate for a field store (the field
// form has no holidays).
func storeCreate(in *adminfieldapi.FieldStoreCreate) *businessapi.StoreCreate {
	out := &businessapi.StoreCreate{
		AddressLine1: in.AddressLine1, AddressLine2: in.AddressLine2, City: in.City, Landmark: in.Landmark,
		Locality: in.Locality, Name: in.Name, Phone: in.Phone, Pincode: in.Pincode,
	}
	if in.Location != nil {
		out.Location = &businessapi.StoreLocation{Lat: in.Location.Lat, Lng: in.Location.Lng}
	}
	if in.Hours != nil {
		w := in.Hours.Weekly
		out.Hours = &businessapi.StoreHours{Holidays: []businessapi.StoreHoliday{}, Weekly: businessapi.StoreWeek{
			Mon: fieldRanges(w.Mon), Tue: fieldRanges(w.Tue), Wed: fieldRanges(w.Wed), Thu: fieldRanges(w.Thu),
			Fri: fieldRanges(w.Fri), Sat: fieldRanges(w.Sat), Sun: fieldRanges(w.Sun),
		}}
	}
	return out
}

// guard is the in-transaction check of a store or photo add: the business is
// the caller's and, for a field agent, inside the edit window.
//
// TODO(lead): 02 §3.3 gives stores and photos admin.business.onboard (own,
// no window); this also applies the D-39 window to them, the conservative
// reading (an agent cannot add a store to a shop verified months ago).
func (f *Field) guard(c fieldCall, bid ids.ID) func(context.Context) error {
	return func(ctx context.Context) error {
		b, err := f.business(ctx, c, bid)
		if err != nil {
			return err
		}
		// Admins are not windowed, but a rejected or suspended business
		// takes no new store or photo from anyone.
		return f.mayEdit(ctx, c, b)
	}
}

func (f *Field) createStore(ctx context.Context, bidStr string, body *adminfieldapi.FieldStoreCreate) (adminfieldapi.FieldStore, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldStore{}, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return adminfieldapi.FieldStore{}, err
	}
	if body == nil {
		return adminfieldapi.FieldStore{}, problem.New(genproblems.BadRequest, "")
	}
	st, err := f.s.insertStore(ctx, audit.ActorAdmin, c.p.UserID, bid, storeCreate(body), f.guard(c, bid))
	if err != nil {
		return adminfieldapi.FieldStore{}, err
	}
	h, err := ParseHours(st.Hours)
	if err != nil {
		return adminfieldapi.FieldStore{}, fmt.Errorf("business: field store hours: %w", err)
	}
	return adminfieldapi.FieldStore{
		BusinessId: idString(st.BusinessID), City: st.City, CreatedAt: st.CreatedAt.UTC(),
		HasHours: !h.IsEmpty(), HasLocation: st.HasLocation, HasPhone: len(st.PhoneEnc) > 0,
		Id: idString(st.ID), Locality: st.Locality, Name: st.Name, Pincode: st.Pincode,
		Status: adminfieldapi.FieldStoreStatus(st.Status),
	}, nil
}

// createPhoto records a shop photo (shop_photo or storefront) and returns its
// presigned PUT, as the business app's document upload does.
func (f *Field) createPhoto(ctx context.Context, bidStr string, body *adminfieldapi.FieldDocumentCreate) (adminfieldapi.FieldDocumentUpload, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldDocumentUpload{}, err
	}
	if f.s.presign == nil {
		return adminfieldapi.FieldDocumentUpload{}, problem.Wrap(genproblems.ServiceUnavailable, errNoStorage)
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return adminfieldapi.FieldDocumentUpload{}, err
	}
	if body == nil {
		return adminfieldapi.FieldDocumentUpload{}, problem.New(genproblems.BadRequest, "")
	}
	var v invalid
	if body.Kind != adminfieldapi.ShopPhoto && body.Kind != adminfieldapi.Storefront {
		v.add("body.kind", "must be shop_photo or storefront")
	}
	store := optUUID(&v, "body.storeId", body.StoreId)
	size := int64(body.SizeBytes)
	if size < 1 || size > maxDocumentBytes {
		v.add("body.sizeBytes", "must be between 1 byte and 10 MB")
	}
	if err := v.err(); err != nil {
		return adminfieldapi.FieldDocumentUpload{}, err
	}
	doc, key, err := f.s.insertDocument(ctx, audit.ActorAdmin, c.p.UserID, bid, store, string(body.Kind), string(body.ContentType), size, true, f.guard(c, bid))
	if err != nil {
		return adminfieldapi.FieldDocumentUpload{}, err
	}
	now := f.s.clock.Now()
	return adminfieldapi.FieldDocumentUpload{
		Document: fieldDocument(doc),
		Upload: adminfieldapi.FieldUploadTarget{
			ExpiresAt: now.Add(uploadTTL).UTC(),
			Headers: adminfieldapi.FieldUploadHeaders{
				ContentType: body.ContentType, ContentLength: fmt.Sprint(doc.SizeBytes),
			},
			Method: adminfieldapi.PUT,
			Url:    f.s.presign.putURL(key, string(body.ContentType), doc.SizeBytes, now),
		},
	}, nil
}

// pendingLife is how long a field photo may stay pending (its confirm_by):
// the upload URL's life and a few minutes for a PUT that started just before
// it ran out. After it the photo no longer counts towards the cap, cannot be
// confirmed, and the sweep rejects it.
const pendingLife = uploadTTL + 5*time.Minute

func fieldDocument(doc businessdb.BusinessDocument) adminfieldapi.FieldDocument {
	return adminfieldapi.FieldDocument{
		ContentType: adminfieldapi.FieldPhotoContentType(doc.ContentType), CreatedAt: doc.CreatedAt.UTC(),
		Id: idString(doc.ID), Kind: adminfieldapi.FieldDocumentKind(doc.Kind),
		Status: adminfieldapi.FieldDocumentStatus(doc.Status), StoreId: optIDString(doc.StoreID),
	}
}

// confirmPhoto marks a shop photo uploaded once the bucket holds it with the
// declared type and at most the declared size (a signed HEAD, as support's
// claim does). Until then it is pending: not in the counts, not reviewable.
func (f *Field) confirmPhoto(ctx context.Context, bidStr, docStr string) (adminfieldapi.FieldDocument, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldDocument{}, err
	}
	if f.s.presign == nil {
		return adminfieldapi.FieldDocument{}, problem.Wrap(genproblems.ServiceUnavailable, errNoStorage)
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return adminfieldapi.FieldDocument{}, err
	}
	did, err := pathID(docStr)
	if err != nil {
		return adminfieldapi.FieldDocument{}, err
	}
	photo := func(ctx context.Context) (businessdb.BusinessDocument, error) {
		if err := f.guard(c, bid)(ctx); err != nil {
			return businessdb.BusinessDocument{}, err
		}
		doc, err := f.s.q(ctx).FieldDocumentForUpdate(ctx, businessdb.FieldDocumentForUpdateParams{ID: ids.UUID(did), BusinessID: ids.UUID(bid)})
		if db.IsNoRows(err) || (err == nil && doc.Kind != string(adminfieldapi.ShopPhoto) && doc.Kind != string(adminfieldapi.Storefront)) {
			return businessdb.BusinessDocument{}, problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return businessdb.BusinessDocument{}, fmt.Errorf("business: field photo: %w", err)
		}
		return doc, nil
	}
	var doc businessdb.BusinessDocument
	if err := f.s.db.InTx(ctx, func(ctx context.Context) error {
		doc, err = photo(ctx)
		return err
	}); err != nil {
		return adminfieldapi.FieldDocument{}, asProblem(err)
	}
	if doc.Status != "pending" {
		return fieldDocument(doc), nil
	}
	now := f.s.clock.Now()
	if doc.ConfirmBy != nil && now.After(*doc.ConfirmBy) {
		return adminfieldapi.FieldDocument{}, problem.New(genproblems.InvalidTransition, "the upload URL has expired; add the photo again")
	}
	// Outside the transaction: no row lock is held over the bucket call.
	obj, err := f.s.presign.head(ctx, doc.ObjectKey, now)
	switch {
	case errors.Is(err, errNoObject):
		return adminfieldapi.FieldDocument{}, problem.New(genproblems.ValidationFailed, "").WithField("upload", "the photo is not in storage yet")
	case err != nil:
		return adminfieldapi.FieldDocument{}, problem.Wrap(genproblems.ServiceUnavailable, fmt.Errorf("business: head field photo: %w", err))
	}
	if obj.size < 1 || obj.size > doc.SizeBytes || obj.contentType != doc.ContentType {
		return adminfieldapi.FieldDocument{}, problem.New(genproblems.ValidationFailed, "").WithField("upload", "the stored file is not the declared type and size")
	}
	err = f.s.db.InTx(ctx, func(ctx context.Context) error {
		cur, err := photo(ctx)
		if err != nil {
			return err
		}
		if cur.Status != "pending" {
			doc = cur
			return nil
		}
		doc, err = f.s.q(ctx).MarkDocumentUploaded(ctx, businessdb.MarkDocumentUploadedParams{Now: f.s.clock.Now(), ID: cur.ID})
		if err != nil {
			return fmt.Errorf("business: confirm field photo: %w", err)
		}
		return f.s.writeAuditAs(ctx, audit.ActorAdmin, c.p.UserID, "document.uploaded", "business_document", did.String(),
			map[string]any{"status": cur.Status}, map[string]any{"status": doc.Status, "sizeBytes": obj.size})
	})
	if err != nil {
		return adminfieldapi.FieldDocument{}, asProblem(err)
	}
	return fieldDocument(doc), nil
}

// trimmedPtr is s without surrounding spaces, nil when that leaves nothing.
func trimmedPtr(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

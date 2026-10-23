// Package business is shops on TodayZ (W1-04; 01 §3, 02 §3.3, 03, 04 §2.3):
// onboarding drafts and their submit, stores with a map pin and opening
// hours, staff invites and roles, KYC, verification documents, store
// settings and the site's lead form.
//
// Security rules this package keeps:
//
//   - PAN, GSTIN, UPI id, bank account, staff invite phones, store phones and
//     lead phones are stored sealed (pii.Sealer); only the last 4 characters
//     of a KYC field are kept in clear, for masked display. Nothing here logs
//     or audits one of those values.
//   - Every /v1/biz route is checked by the permission engine (slot 15) by
//     the role of the caller in the path's business and store; the staff
//     rules the engine cannot see (a manager may add only cashiers, the
//     owner's row never changes) are checked here and answer 403.
//   - KYC is shown masked: last 4 characters only, and a manager (grant
//     Masked) sees status and last 4 but not the IFSC or legal name.
package business

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// HumanVerifier checks a Turnstile token (the lead form, 04 §2.5). ok false
// means the token failed; err means the check could not be made. The identity
// service's verifier satisfies it.
type HumanVerifier interface {
	Verify(ctx context.Context, token, remoteIP string) (ok bool, err error)
}

// Deps is what the service needs. DB, the PII keys, Human and Authz may be
// missing in a route-only test: the operations that need them answer 503.
type Deps struct {
	Config *config.Config
	DB     *db.DB
	Clock  clock.Clock
	Log    *slog.Logger
	// Human checks the lead form's Turnstile token.
	Human HumanVerifier
	// Authz runs the second check of a staff or settings change that needs
	// more than the route's action (staff.manage, asks.manage).
	Authz authz.Authorizer
}

// Service implements the business routes and the lookups other domains use.
type Service struct {
	cfg     *config.Config
	db      *db.DB
	clock   clock.Clock
	log     *slog.Logger
	human   HumanVerifier
	az      authz.Authorizer
	seal    *pii.Sealer
	hash    *pii.Hasher
	audit   *audit.Writer
	presign *presigner // nil until R2_* (MinIO locally) is configured
}

// errUnavailable is why the service cannot serve (no database or keys).
var errUnavailable = errors.New("business: not configured")

// New builds the service. Missing keys do not fail here (route-only tests
// build the router without them); the operations answer 503 instead.
func New(d Deps) (*Service, error) {
	if d.Config == nil || d.Clock == nil {
		return nil, errors.New("business: config and clock are required")
	}
	s := &Service{cfg: d.Config, db: d.DB, clock: d.Clock, log: d.Log, human: d.Human, az: d.Authz}
	if k := d.Config.Keys.PIIEncKey; k.IsSet() {
		seal, err := pii.NewSealer(k.Bytes())
		if err != nil {
			return nil, errors.New("business: PII_ENC_KEY must be 32 bytes")
		}
		s.seal = seal
	}
	if k := d.Config.Keys.PIIHashKey; k.IsSet() {
		h, err := pii.NewHasher(k.Bytes())
		if err != nil {
			return nil, errors.New("business: PII_HASH_KEY must be 32 bytes")
		}
		s.hash = h
	}
	if d.DB != nil {
		var ipKey []byte
		if d.Config.Keys.PIIHashKey.IsSet() {
			ipKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		s.audit = audit.New(d.DB, d.Clock, ipKey)
	}
	s.presign = newPresigner(d.Config.Storage)
	return s, nil
}

// ready reports whether the business routes can run.
func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.seal == nil || s.hash == nil || s.audit == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "business: not configured (needs the database, PII_ENC_KEY and PII_HASH_KEY)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) q(ctx context.Context) *businessdb.Queries { return businessdb.New(s.db.Q(ctx)) }

// --- staff resolver (authz) ------------------------------------------------

// StaffResolver answers the permission engine's membership questions from
// the staff and stores tables (authz.StaffResolver).
type StaffResolver struct{ db *db.DB }

// NewStaffResolver returns the resolver the engine is built with.
func NewStaffResolver(d *db.DB) *StaffResolver { return &StaffResolver{db: d} }

var _ authz.StaffResolver = (*StaffResolver)(nil)

// Membership is the caller's active staff row in the business: its role and
// store scope (nil: every store).
func (r *StaffResolver) Membership(ctx context.Context, userID, businessID ids.ID) (authz.Membership, bool, error) {
	row, err := businessdb.New(r.db.Q(ctx)).Membership(ctx, businessdb.MembershipParams{UserID: ids.UUID(userID), BusinessID: ids.UUID(businessID)})
	if db.IsNoRows(err) {
		return authz.Membership{}, false, nil
	}
	if err != nil {
		return authz.Membership{}, false, fmt.Errorf("business: membership: %w", err)
	}
	m := authz.Membership{Role: authz.Role(row.Role)}
	if row.StoreIds != nil {
		m.StoreIDs = make([]ids.ID, 0, len(row.StoreIds))
		for _, u := range row.StoreIds {
			if id, ok := ids.FromUUID(u); ok {
				m.StoreIDs = append(m.StoreIDs, id)
			}
		}
	}
	return m, true, nil
}

// StoreBusiness is the business a store belongs to.
func (r *StaffResolver) StoreBusiness(ctx context.Context, storeID ids.ID) (ids.ID, bool, error) {
	u, err := businessdb.New(r.db.Q(ctx)).StoreBusiness(ctx, ids.UUID(storeID))
	if db.IsNoRows(err) {
		return ids.Zero, false, nil
	}
	if err != nil {
		return ids.Zero, false, fmt.Errorf("business: store business: %w", err)
	}
	id, ok := ids.FromUUID(u)
	return id, ok, nil
}

// --- lookups for other domains (W1-05 catalogue, W1-06 search, W2 orders) ---

// StoreInfo is a store as other domains need it.
type StoreInfo struct {
	ID             ids.ID
	BusinessID     ids.ID
	Name           string
	Slug           string
	ShortCode      string
	Status         string // draft, pending_review, live, paused, suspended
	BusinessStatus string // draft, submitted, needs_info, verified, rejected, suspended
	// Visible is true when customers may see and book the store: the store
	// is live and its business verified (01 §3).
	Visible         bool
	HasLocation     bool
	Lat, Lng        float64
	Hours           Hours
	ClosedUntil     *time.Time
	AcceptsPickup   bool
	AcceptsDelivery bool
	DeliveryRadiusM int32
	AutoAccept      bool
	CashbackBp      int32
}

// OpenAt reports whether the store is open at t by its hours and manual
// close. It ignores Status: a paused store's hours still say open.
func (si StoreInfo) OpenAt(t time.Time) bool { return si.Hours.OpenAt(t, si.ClosedUntil) }

// Store looks up one store. ok is false when there is no such store.
func (s *Service) Store(ctx context.Context, id ids.ID) (StoreInfo, bool, error) {
	if s.db == nil {
		return StoreInfo{}, false, errUnavailable
	}
	q := s.q(ctx)
	st, err := q.GetStore(ctx, ids.UUID(id))
	if db.IsNoRows(err) {
		return StoreInfo{}, false, nil
	}
	if err != nil {
		return StoreInfo{}, false, fmt.Errorf("business: store: %w", err)
	}
	b, err := q.GetBusiness(ctx, st.BusinessID)
	if err != nil {
		return StoreInfo{}, false, fmt.Errorf("business: store's business: %w", err)
	}
	h, err := ParseHours(st.Hours)
	if err != nil {
		return StoreInfo{}, false, err
	}
	sid, _ := ids.FromUUID(st.ID)
	bid, _ := ids.FromUUID(st.BusinessID)
	return StoreInfo{
		ID: sid, BusinessID: bid, Name: st.Name, Slug: st.Slug, ShortCode: st.ShortCode,
		Status: st.Status, BusinessStatus: b.Status,
		Visible:     st.Status == storeLive && b.Status == businessVerified,
		HasLocation: st.HasLocation, Lat: st.Lat, Lng: st.Lng,
		Hours: h, ClosedUntil: st.ClosedUntil,
		AcceptsPickup: st.AcceptsPickup, AcceptsDelivery: st.AcceptsDelivery,
		DeliveryRadiusM: st.DeliveryRadiusM, AutoAccept: st.AutoAccept, CashbackBp: st.CashbackBp,
	}, true, nil
}

// OwnsActiveBusiness reports whether the user is the active owner of a
// business that is not rejected (draft, submitted, needs_info, verified or
// suspended). W1-01 phase B's account deletion refuses such an owner with
// 409 conflict until the business is handed over or closed (01 §2). It only
// reads, through d.Q(ctx), so it joins the caller's transaction.
//
// TODO(lead): a draft counts as active here (the most conservative reading:
// its stores, staff and documents would lose their owner). If deletion should
// instead close a draft, narrow the query to submitted and later states.
func OwnsActiveBusiness(ctx context.Context, d *db.DB, userID ids.ID) (bool, error) {
	if d == nil {
		return false, errUnavailable
	}
	ok, err := businessdb.New(d.Q(ctx)).OwnsActiveBusiness(ctx, ids.UUID(userID))
	if err != nil {
		return false, fmt.Errorf("business: owns active business: %w", err)
	}
	return ok, nil
}

// Status values (03 businesses.status, stores.status, staff.role/status).
const (
	businessDraft      = "draft"
	businessNeedsInfo  = "needs_info"
	businessVerified   = "verified"
	businessRejected   = "rejected"
	businessSuspended  = "suspended"
	storeDraft         = "draft"
	storeLive          = "live"
	storePaused        = "paused"
	storePendingReview = "pending_review"
	staffActive        = "active"
	staffInvited       = "invited"
	staffRemoved       = "removed"
	roleOwner          = "owner"
	roleManager        = "manager"
	roleCashier        = "cashier"
)

// --- helpers ---------------------------------------------------------------

// businessOpen refuses a change to a rejected or suspended business's stores
// or KYC with 409 invalid_transition. It must run inside the change's
// transaction: the status stays locked (FOR SHARE) until it commits. An
// unknown business is 404 (authz has normally answered that already).
func (s *Service) businessOpen(ctx context.Context, bid ids.ID) error {
	st, err := s.q(ctx).BusinessStatusForShare(ctx, ids.UUID(bid))
	if db.IsNoRows(err) {
		return problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return fmt.Errorf("business: status: %w", err)
	}
	if st == businessRejected || st == businessSuspended {
		return problem.New(genproblems.InvalidTransition, "the business is "+st)
	}
	return nil
}

// principal is the signed-in caller; anonymous is 401.
func principal(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	return p, nil
}

// pathID parses a path ULID; the validator checked the pattern, so a miss is
// a 404 like any unknown id.
func pathID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, problem.New(genproblems.NotFound, "")
	}
	return id, nil
}

func etag(version int32) string { return `"v` + strconv.FormatInt(int64(version), 10) + `"` }

// parseETag reads `"v{version}"`; a version that does not fit is stale.
func parseETag(s string) (int32, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, `"v`) || !strings.HasSuffix(s, `"`) || len(s) < 4 {
		return 0, false
	}
	n, err := strconv.ParseInt(s[2:len(s)-1], 10, 32)
	if err != nil || n < 0 {
		return 0, false
	}
	return int32(n), true
}

// ifMatch reads a required If-Match: missing is 428, unreadable is 412.
func ifMatch(h *string) (int32, error) {
	if h == nil || strings.TrimSpace(*h) == "" {
		return 0, problem.New(genproblems.PreconditionRequired, "")
	}
	v, ok := parseETag(*h)
	if !ok {
		return 0, problem.New(genproblems.PreconditionFailed, "")
	}
	return v, nil
}

func ptr[T any](v T) *T { return &v }

// asProblem passes a problem through and wraps anything else as 500.
func asProblem(err error) error {
	var p *problem.Error
	if errors.As(err, &p) {
		return p
	}
	return problem.Wrap(genproblems.Internal, err)
}

// codeAlphabet is the referral and short-code alphabet (no 0, O, 1, I, L),
// the same as the customer referral codes (03 ck_businesses_referral_code).
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func randomString(alphabet string, n int) (string, error) {
	b := make([]byte, n)
	limit := big.NewInt(int64(len(alphabet)))
	for i := range b {
		v, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("business: random: %w", err)
		}
		b[i] = alphabet[v.Int64()]
	}
	return string(b), nil
}

// invalid collects 422 field reasons.
type invalid struct{ p *problem.Error }

func (v *invalid) add(path, reason string) {
	if v.p == nil {
		v.p = problem.New(genproblems.ValidationFailed, "")
	}
	v.p = v.p.WithField(path, reason)
}

func (v *invalid) addAll(errs []FieldError) {
	for _, e := range errs {
		v.add(e.Path, e.Reason)
	}
}

func (v *invalid) err() error {
	if v.p == nil {
		return nil
	}
	return v.p
}

// text checks free text the contract's pattern lets through: it must be
// valid UTF-8 with no control characters (a name with a NUL or a newline
// would break receipts, SMS and CSV exports).
func (v *invalid) text(path string, s *string) {
	if s == nil {
		return
	}
	if !utf8.ValidString(*s) {
		v.add(path, "must be valid text")
		return
	}
	for _, r := range *s {
		if unicode.IsControl(r) {
			v.add(path, "must not contain control characters")
			return
		}
	}
}

// decision is the engine's decision for this request (set by slot 15).
func decision(ctx context.Context) (authz.Decision, error) {
	d, ok := authz.DecisionFrom(ctx)
	if !ok {
		return authz.Decision{}, problem.Wrap(genproblems.Internal, errors.New("business: no authz decision"))
	}
	return d, nil
}

// also runs a second permission check in the request's scope (a manager
// inviting a manager also needs staff.manage).
func (s *Service) also(ctx context.Context, action string, scope authz.Scope) error {
	if s.az == nil {
		return problem.Wrap(genproblems.Internal, errors.New("business: no authorizer"))
	}
	p, _ := authn.FromContext(ctx)
	_, err := s.az.Check(ctx, p, action, scope)
	return err
}

// actor is the audit actor of a business-app caller: staff, by user id.
func actor(uid ids.ID) (string, ids.ID) { return audit.ActorStaff, uid }

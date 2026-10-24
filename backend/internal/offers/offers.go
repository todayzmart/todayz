package offers

// The offers service (W2-07; 01 §5.5, §12, D-94; 03 §9; 04 §2.6): a shop's
// offers (create, pause, end), the pricing every caller shares (Price, Redeem,
// Release in pricing.go; the engine in engine.go), and the "Offers near you"
// feed (feed.go).
//
// Rules this package keeps:
//
//   - One engine prices every cart. Orders quote, basket search and the POS
//     call Price; nothing else computes an offer's amount.
//   - An offer's terms never change after it is made (end it, make a new one);
//     only its status, title, channel, dates and limits do.
//   - A redemption is counted when the order is placed (Redeem, in the order's
//     transaction) and given back when it is cancelled, rejected or expires
//     (Release). The offer row lock serialises the claims of one offer.
//   - Expiry reads the injected clock; the scheduling job only tidies the
//     status label, so an offer ends on time even when the job runs late.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/offersdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/httpx/ifmatch"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// BannerCard is a live banner as the feed needs it. The banners domain
// supplies them through BannerSource (cmd/todayz adapts it), so this package
// never imports internal/banners.
type BannerCard struct {
	ID            ids.ID
	StoreID       ids.ID
	Headline      string
	ImageKey      *string
	Template      map[string]any
	TargetType    string
	TargetID      *ids.ID
	TargetItemIDs []ids.ID
}

// BannerSource lists the live, approved banners of stores at time t.
type BannerSource interface {
	LiveBanners(ctx context.Context, storeIDs []ids.ID, t time.Time) ([]BannerCard, error)
}

// Deps is what the service needs. DB and Settings may be missing in a
// route-only test: the operations that need them answer 503.
type Deps struct {
	Config   *config.Config
	DB       *db.DB
	Clock    clock.Clock
	Log      *slog.Logger
	Settings *settings.Cache
	// Banners fills the feed's banner cards; nil shows offers only.
	Banners BannerSource
	// Events validates and names the product events; nil emits none.
	Events *eventcat.Catalogue
}

// Service implements the offers routes and the pricing seams.
type Service struct {
	db       *db.DB
	clock    clock.Clock
	settings *settings.Cache
	banners  BannerSource
	events   *eventcat.Catalogue
	audit    *audit.Writer
	ids      *ids.Generator
}

var errUnavailable = errors.New("offers: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("offers: a clock is required")
	}
	s := &Service{
		db: d.DB, clock: d.Clock, settings: d.Settings, banners: d.Banners, events: d.Events, ids: ids.NewGenerator(),
	}
	if d.DB != nil {
		var ipKey []byte
		if d.Config != nil && d.Config.Keys.PIIHashKey.IsSet() {
			ipKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		s.audit = audit.New(d.DB, d.Clock, ipKey)
	}
	return s, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "offers: not configured (needs the database and settings)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) q(ctx context.Context) *offersdb.Queries { return offersdb.New(s.db.Q(ctx)) }

// enabled is the feature_offers_enabled switch (01 §17): when it is off no
// offer prices and no offer shows.
func (s *Service) enabled(ctx context.Context) (bool, error) {
	on, err := s.settings.Bool(ctx, settings.FeatureOffersEnabled)
	if err != nil {
		return false, fmt.Errorf("offers: feature_offers_enabled: %w", err)
	}
	return on, nil
}

// event validates props against the catalogue and writes the server event.
// A failure is logged and never fails the request.
func (s *Service) event(ctx context.Context, q *offersdb.Queries, user pgtype.UUID, name string, props map[string]any) {
	if s.events == nil {
		return
	}
	log := telemetry.Logger(ctx)
	ev, ok := s.events.Event(name)
	if !ok {
		log.ErrorContext(ctx, "offers: event not in the catalogue", slog.String("event", name))
		return
	}
	if err := ev.Validate(props); err != nil {
		log.ErrorContext(ctx, "offers: event props invalid", slog.String("event", name), slog.String("error", err.Error()))
		return
	}
	raw, err := json.Marshal(props)
	if err != nil {
		log.ErrorContext(ctx, "offers: event props", slog.String("event", name), slog.String("error", err.Error()))
		return
	}
	now := s.clock.Now()
	if err := q.InsertServerEvent(ctx, offersdb.InsertServerEventParams{
		ID: ids.UUID(s.ids.New(now)), Name: name, OccurredAt: now, UserID: user, Props: raw,
	}); err != nil {
		log.WarnContext(ctx, "offers: event insert failed", slog.String("event", name), slog.String("error", err.Error()))
	}
}

// --- helpers --------------------------------------------------------------------

func principal(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	return p, nil
}

func pathID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, problem.New(genproblems.NotFound, "")
	}
	return id, nil
}

func etag(version int32) string { return `"v` + strconv.FormatInt(int64(version), 10) + `"` }

// ifMatch reads a required If-Match: missing is 428, unreadable is 412.
func ifMatch(h *string) (int32, error) {
	if h == nil || strings.TrimSpace(*h) == "" {
		return 0, problem.New(genproblems.PreconditionRequired, "")
	}
	v, ok := ifmatch.Parse(*h)
	if !ok {
		return 0, problem.New(genproblems.PreconditionFailed, "")
	}
	return v, nil
}

func ptr[T any](v T) *T { return &v }

func asProblem(err error) error {
	var p *problem.Error
	if errors.As(err, &p) {
		return p
	}
	return problem.Wrap(genproblems.Internal, err)
}

func fieldErr(path, reason string) error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

// invalid collects 422 field reasons.
type invalid struct{ p *problem.Error }

func (v *invalid) add(path, reason string) {
	if v.p == nil {
		v.p = problem.New(genproblems.ValidationFailed, "")
	}
	v.p = v.p.WithField(path, reason)
}

func (v *invalid) err() error {
	if v.p == nil {
		return nil
	}
	return v.p
}

// text refuses invalid UTF-8, control characters and blank text.
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
	if strings.TrimSpace(*s) == "" {
		v.add(path, "must not be blank")
	}
}

func i32(n int) int32 { return int32(min(max(n, 0), 1<<30)) } //nolint:gosec // clamped

func ptrInt(v *int32) int {
	if v == nil {
		return 0
	}
	return int(*v)
}

func idOf(u pgtype.UUID) ids.ID {
	id, _ := ids.FromUUID(u)
	return id
}

func ulidOf(u pgtype.UUID) string { return idOf(u).String() }

func uuids(in []ids.ID) []pgtype.UUID {
	out := make([]pgtype.UUID, len(in))
	for i, id := range in {
		out[i] = ids.UUID(id)
	}
	return out
}

func idList(in []pgtype.UUID) []ids.ID {
	out := make([]ids.ID, 0, len(in))
	for _, u := range in {
		if id, ok := ids.FromUUID(u); ok {
			out = append(out, id)
		}
	}
	return out
}

// engineOffer turns a row into what the engine reads.
func engineOffer(r offersdb.Offer) Offer {
	o := Offer{
		ID: idOf(r.ID), Kind: Kind(r.Kind), Value: r.Value, Scope: Scope(r.Scope),
		StoreItemIDs: idList(r.StoreItemIds), Channels: Channels(r.Channels),
		PerCustomerLimit: ptrInt(r.PerCustomerLimit), MaxRedemptions: ptrInt(r.MaxRedemptions),
		Redemptions: int(r.Redemptions), StartsAt: r.StartsAt, Status: Status(r.Status),
		BuyQty: ptrInt(r.BuyQty), GetQty: ptrInt(r.GetQty),
	}
	if r.Title != nil {
		o.Title = *r.Title
	}
	if r.MinBillPaise != nil {
		o.MinBillPaise = *r.MinBillPaise
	}
	if id, ok := ids.FromUUID(r.CategoryID); ok {
		o.CategoryID = id
	}
	if r.EndsAt != nil {
		o.EndsAt = *r.EndsAt
	}
	return o
}

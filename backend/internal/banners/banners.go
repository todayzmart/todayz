package banners

// The banners service (W2-07; 01 §12, D-46, D-77; 03 §9; 04 §2.6): a shop's
// banners (a 16:9 image or a template, a headline of at most 40 characters and
// a target), the customer's tap, and the live banners the "Offers near you"
// feed shows (internal/offers reads them through a small interface that
// cmd/todayz adapts, so the two packages never import each other).
//
// Rules this package keeps:
//
//   - A shop has at most store_banners_max banners that are live, scheduled or
//     held for review. The count is taken under the store's row lock, so two
//     concurrent creates cannot both pass.
//   - A banner made from a template goes live (or scheduled) at once; an
//     uploaded image waits in pending_review until moderation clears it.
//   - The picture, headline and target never change; end it and make a new one.
//   - Only live, cleared banners of live, verified shops show to customers.

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
	"github.com/todayz/backend/gen/sqlc/bannersdb"
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

// Statuses and moderation states (03 §9).
const (
	statusPending   = "pending_review"
	statusLive      = "live"
	statusScheduled = "scheduled"
	statusPaused    = "paused"
	statusEnded     = "ended"
	statusRemoved   = "removed"

	modAuto  = "auto_ok"
	modNeeds = "needs_review"
)

// LiveBanner is a banner that shows to customers (what the feed needs).
type LiveBanner struct {
	ID            ids.ID
	StoreID       ids.ID
	Headline      string
	ImageKey      *string
	Template      map[string]any
	TargetType    string
	TargetID      *ids.ID
	TargetItemIDs []ids.ID
}

// Deps is what the service needs. DB and Settings may be missing in a
// route-only test: the operations that need them answer 503.
type Deps struct {
	Config   *config.Config
	DB       *db.DB
	Clock    clock.Clock
	Log      *slog.Logger
	Settings *settings.Cache
	// Events validates and names the product events; nil emits none.
	Events *eventcat.Catalogue
}

// Service implements the banners routes.
type Service struct {
	db       *db.DB
	clock    clock.Clock
	settings *settings.Cache
	events   *eventcat.Catalogue
	audit    *audit.Writer
	ids      *ids.Generator
}

var errUnavailable = errors.New("banners: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("banners: a clock is required")
	}
	s := &Service{db: d.DB, clock: d.Clock, settings: d.Settings, events: d.Events, ids: ids.NewGenerator()}
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
		telemetry.Logger(ctx).ErrorContext(ctx, "banners: not configured (needs the database and settings)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) q(ctx context.Context) *bannersdb.Queries { return bannersdb.New(s.db.Q(ctx)) }

// enabled is the feature_banners_enabled switch (01 §17).
func (s *Service) enabled(ctx context.Context) (bool, error) {
	on, err := s.settings.Bool(ctx, settings.FeatureBannersEnabled)
	if err != nil {
		return false, fmt.Errorf("banners: feature_banners_enabled: %w", err)
	}
	return on, nil
}

// event validates props against the catalogue and writes the server event.
// A failure is logged and never fails the request.
func (s *Service) event(ctx context.Context, q *bannersdb.Queries, user pgtype.UUID, name string, props map[string]any) {
	if s.events == nil {
		return
	}
	log := telemetry.Logger(ctx)
	ev, ok := s.events.Event(name)
	if !ok {
		log.ErrorContext(ctx, "banners: event not in the catalogue", slog.String("event", name))
		return
	}
	if err := ev.Validate(props); err != nil {
		log.ErrorContext(ctx, "banners: event props invalid", slog.String("event", name), slog.String("error", err.Error()))
		return
	}
	raw, err := json.Marshal(props)
	if err != nil {
		log.ErrorContext(ctx, "banners: event props", slog.String("event", name), slog.String("error", err.Error()))
		return
	}
	now := s.clock.Now()
	if err := q.InsertServerEvent(ctx, bannersdb.InsertServerEventParams{
		ID: ids.UUID(s.ids.New(now)), Name: name, OccurredAt: now, UserID: user, Props: raw,
	}); err != nil {
		log.WarnContext(ctx, "banners: event insert failed", slog.String("event", name), slog.String("error", err.Error()))
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
func (v *invalid) text(path, s string) {
	if !utf8.ValidString(s) {
		v.add(path, "must be valid text")
		return
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			v.add(path, "must not contain control characters")
			return
		}
	}
	if strings.TrimSpace(s) == "" {
		v.add(path, "must not be blank")
	}
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

// now is the injected clock's time.
func (s *Service) now() time.Time { return s.clock.Now() }

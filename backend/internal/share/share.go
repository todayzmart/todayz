// Package share is the public side of search proof (W3-16; 01 §11.1, §11.3,
// D-101): share cards for a store item (`/x/{code}`), the site's "What people
// near you are searching for" strip, and A-23 Demand (gaps and the strip's
// approval).
//
// Rules this package keeps:
//
//   - A card is only for an item of a live store of a verified business, and
//     reading one for a store that is no longer either is the same 410.
//   - A preview never carries a distance or anything about who shared it.
//   - New cards are counted per IP and per anonymous device by an HMAC of the
//     value (share_card_creates), never the value itself.
//   - Reads are public, so a read writes as little as it can: card reads and
//     re-creates are rate-limited per IP HMAC, share_opened is recorded once
//     per card, IP and IST day (share_card_opens), and a stale image is
//     redrawn by the share_card_render job, at most one queued per card in
//     renderCooldown, while the old image is served.
//   - The strip is master product names only, after the k-floor (distinct
//     signed-in searchers, never below 10) and the category denylist, and the
//     site sees only what an admin approved.
//   - Gaps are counts per city, never a customer, after their own k-floor.
package share

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/asksdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/ratelimit"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// kFloor is the least a configurable k-floor may be (01 §16: "never below
// 10"); a setting below it is read as 10.
const kFloor = 10 //settings:ok privacy floor of 01 §16, not a setting

// Denylist is the categories never proposed for the site strip (01 §17 fixed
// constants: medicines and health, personal and intimate care, tobacco,
// adult). A product is left out when its category or that category's parent
// is listed. "pharmacy" is the whole pharmacy tree (medicines, health and
// wellness, baby care): the conservative reading of "medicines and health".
var Denylist = []string{
	"pharmacy", "medicines", "health-wellness",
	"personal-care", "intimate-care", "sexual-wellness",
	"tobacco", "adult",
}

// ImageStore keeps rendered preview images in public object storage.
type ImageStore interface {
	// Put stores body under key with the given content type, replacing any
	// object there.
	Put(ctx context.Context, key, contentType string, body []byte) error
}

// Deps is what the service needs. DB and Settings may be missing in a
// route-only test: the operations that need them answer 503.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	// Events validates and names the product events; nil emits none.
	Events *eventcat.Catalogue
	// Hash keys the per-IP and per-device create counts (PII_HASH_KEY); nil
	// refuses every new card (503), since an uncounted create is unlimited.
	Hash *pii.Hasher
	// IPHashKey is PII_HASH_KEY for the audit log's ip_hash; may be nil.
	IPHashKey []byte
	// Images stores the preview PNGs; nil renders nothing (the URL is still
	// the card's key, which the Pages Function falls back from).
	Images ImageStore
	// Jobs queues share_card_render for a stale image; nil queues nothing
	// (the old image stays).
	Jobs platformjobs.Enqueuer
	// AppURL is the customer app's origin (https://app.todayz.in).
	AppURL string
	// ImageURL is the public images origin (https://img.todayz.in).
	ImageURL string
}

// Service implements the share and admin-demand routes and the strip build.
type Service struct {
	db       *db.DB
	clock    clock.Clock
	settings *settings.Cache
	events   *eventcat.Catalogue
	hash     *pii.Hasher
	images   ImageStore
	jobs     platformjobs.Enqueuer
	limit    *ratelimit.Limiter
	audit    *audit.Writer
	appURL   string
	imageURL string
	ids      *ids.Generator
}

var errUnavailable = errors.New("share: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("share: a clock is required")
	}
	if d.AppURL == "" || d.ImageURL == "" {
		return nil, errors.New("share: the app and image origins are required")
	}
	s := &Service{
		db: d.DB, clock: d.Clock, settings: d.Settings, events: d.Events, hash: d.Hash, images: d.Images, jobs: d.Jobs,
		limit:  ratelimit.New(d.Clock, map[string]ratelimit.Rule{readGroup: readRule}),
		appURL: d.AppURL, imageURL: d.ImageURL, ids: ids.NewGenerator(),
	}
	if d.DB != nil {
		s.audit = audit.New(d.DB, d.Clock, d.IPHashKey)
	}
	return s, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "share: not configured (needs the database and settings)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) q(ctx context.Context) *asksdb.Queries { return asksdb.New(s.db.Q(ctx)) }

func (s *Service) now() time.Time { return s.clock.Now().UTC() }

// floorSetting reads a k-floor setting, never below kFloor.
func (s *Service) floorSetting(ctx context.Context, k settings.Key) (int, error) {
	n, err := s.settings.Int(ctx, k)
	if err != nil {
		return 0, err
	}
	return max(int(n), kFloor), nil
}

// event validates props against the catalogue and writes the server event
// with no user. A failure is logged and never fails the request.
func (s *Service) event(ctx context.Context, name string, props map[string]any) {
	if s.events == nil {
		return
	}
	log := telemetry.Logger(ctx)
	ev, ok := s.events.Event(name)
	if !ok {
		log.ErrorContext(ctx, "share: event not in the catalogue", slog.String("event", name))
		return
	}
	if err := ev.Validate(props); err != nil {
		log.ErrorContext(ctx, "share: event props invalid", slog.String("event", name), slog.String("error", err.Error()))
		return
	}
	raw, err := json.Marshal(props)
	if err != nil {
		log.ErrorContext(ctx, "share: event props", slog.String("event", name), slog.String("error", err.Error()))
		return
	}
	now := s.now()
	if err := s.q(ctx).InsertServerEvent(ctx, asksdb.InsertServerEventParams{
		ID: ids.UUID(s.ids.New(now)), Name: name, OccurredAt: now, Props: raw,
	}); err != nil {
		log.WarnContext(ctx, "share: event insert failed", slog.String("event", name), slog.String("error", err.Error()))
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

func idOf(u pgtype.UUID) ids.ID {
	id, _ := ids.FromUUID(u)
	return id
}

func ulidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	v := idOf(u).String()
	return &v
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func pgDate(t time.Time) pgtype.Date {
	y, m, d := t.Date()
	return pgtype.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
}

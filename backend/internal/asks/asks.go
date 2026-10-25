// Package asks is "Ask shops nearby" (W3-16; 01 §11.1–§11.2, D-101; 04 §2.6):
// a customer's ask goes to the nearby shops that can take it, the shops answer
// Yes or No from their inbox (B-20), and the customer books from a Yes (C-26).
//
// Rules this package keeps:
//
//   - A shop never sees who asked: the shop side selects no customer column,
//     and the only distance it sees is a band. The customer's own replies show
//     a distance rounded to 500 m; the exact one is never stored, since exact
//     distances to three shops would place the customer.
//   - A store left out of an ask is never named: "Ask this shop" answers the
//     same 409 ask_store_unavailable whatever the reason, and "nearby" answers
//     409 ask_no_shops when none qualify.
//   - A reply never changes stock. A Yes on a listed item moves its
//     stock_updated_at the way a count with no change does, and writes no
//     movement.
//   - The replied price is per unit, holds for ask_price_valid_min and never
//     stacks with an item offer; orders (W2-02) reads it through the Pricer.
//   - The demand rollup keeps only cells and counts; asks and their replies
//     and blocks go after ask_retention_days, or with the customer's account.
package asks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/asksdb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

const (
	defaultLimit = 20  //settings:ok page size default, 04 §1
	maxLimit     = 100 //settings:ok page size cap, 04 §1
)

// Deps is what the service needs. DB and Settings may be missing in a
// route-only test: the operations that need them answer 503.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	// Notices writes the shop's "new ask" and the customer's first-Yes
	// notice; nil writes none.
	Notices notice.Sender
	// Events validates and names the product events; nil emits none.
	Events *eventcat.Catalogue
	// Authz answers the extra checks inside a reply (item.price.edit); nil
	// refuses every price change.
	Authz authz.Authorizer
	// Normalize is search's text normalisation (01 §11); nil lower-cases
	// and folds spaces.
	Normalize func(string) string
	// OpenAt says whether a store is open at t from its stored hours and
	// manual close (business.OpenAt); nil treats every store as open.
	OpenAt func(storedHours []byte, closedUntil *time.Time, t time.Time) bool
}

// Service implements the asks routes, the orders Pricer and the insights
// demand source.
type Service struct {
	db        *db.DB
	clock     clock.Clock
	settings  *settings.Cache
	notices   notice.Sender
	events    *eventcat.Catalogue
	az        authz.Authorizer
	normalize func(string) string
	openAt    func([]byte, *time.Time, time.Time) bool
	ids       *ids.Generator
}

var errUnavailable = errors.New("asks: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("asks: a clock is required")
	}
	s := &Service{
		db: d.DB, clock: d.Clock, settings: d.Settings, notices: d.Notices, events: d.Events, az: d.Authz,
		normalize: d.Normalize, openAt: d.OpenAt, ids: ids.NewGenerator(),
	}
	if s.normalize == nil {
		s.normalize = func(t string) string { return strings.Join(strings.Fields(strings.ToLower(t)), " ") }
	}
	if s.openAt == nil {
		s.openAt = func([]byte, *time.Time, time.Time) bool { return true }
	}
	return s, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "asks: not configured (needs the database and settings)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) q(ctx context.Context) *asksdb.Queries { return asksdb.New(s.db.Q(ctx)) }

func (s *Service) now() time.Time { return s.clock.Now().UTC() }

// event validates props against the catalogue and writes the server event.
// A failure is logged and never fails the request. user is never set for a
// shop's event, and props never carry a customer id.
func (s *Service) event(ctx context.Context, q *asksdb.Queries, user pgtype.UUID, name string, props map[string]any) {
	if s.events == nil {
		return
	}
	log := telemetry.Logger(ctx)
	ev, ok := s.events.Event(name)
	if !ok {
		log.ErrorContext(ctx, "asks: event not in the catalogue", slog.String("event", name))
		return
	}
	if err := ev.Validate(props); err != nil {
		log.ErrorContext(ctx, "asks: event props invalid", slog.String("event", name), slog.String("error", err.Error()))
		return
	}
	raw, err := json.Marshal(props)
	if err != nil {
		log.ErrorContext(ctx, "asks: event props", slog.String("event", name), slog.String("error", err.Error()))
		return
	}
	now := s.now()
	if err := q.InsertServerEvent(ctx, asksdb.InsertServerEventParams{
		ID: ids.UUID(s.ids.New(now)), Name: name, OccurredAt: now, UserID: user, Props: raw,
	}); err != nil {
		log.WarnContext(ctx, "asks: event insert failed", slog.String("event", name), slog.String("error", err.Error()))
	}
}

// --- bands and days -------------------------------------------------------------

// The distance bands a shop sees (01 §11.2): under 1 km, 1–3 km, 3–5 km,
// over 5 km. Fixed by the rule, not settings.
const (
	band1km = 1000 //settings:ok distance band edge, 01 §11.2
	band3km = 3000 //settings:ok distance band edge, 01 §11.2
	band5km = 5000 //settings:ok distance band edge, 01 §11.2

	// distanceStepM is the rounding of the stored distance (privacy, not a
	// product number): the nearest 500 m, never below 500.
	distanceStepM = 500 //settings:ok privacy rounding of the stored distance
)

// RoundDistance is the distance kept on a recipient row: m to the nearest
// distanceStepM, and never below it.
func RoundDistance(m int32) int32 {
	r := (m + distanceStepM/2) / distanceStepM * distanceStepM
	return max(r, distanceStepM)
}

// Band names the distance band of m metres.
func Band(m int32) string {
	switch {
	case m < band1km:
		return "lt_1km"
	case m < band3km:
		return "1_3km"
	case m < band5km:
		return "3_5km"
	default:
		return "gt_5km"
	}
}

// nextISTMidnight is the start of the IST day after t.
func nextISTMidnight(t time.Time) time.Time {
	return clock.StartOfDayIST(clock.StartOfDayIST(t).Add(36 * time.Hour))
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

func pageArgs(limit *int, cursor *string) (int, *time.Time, pgtype.UUID, error) {
	lim := defaultLimit
	if limit != nil {
		lim = min(max(*limit, 1), maxLimit)
	}
	if cursor == nil || *cursor == "" {
		return lim, nil, pgtype.UUID{}, nil
	}
	at, id, err := decodeCursor(*cursor)
	if err != nil {
		return 0, nil, pgtype.UUID{}, fieldErr("query.cursor", "not a cursor from this list")
	}
	return lim, &at, ids.UUID(id), nil
}

func encodeCursor(at time.Time, id ids.ID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixNano(), 10) + "." + id.String()))
}

func decodeCursor(c string) (time.Time, ids.ID, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	ns, sid, ok := strings.Cut(string(b), ".")
	if !ok {
		return time.Time{}, ids.Zero, errors.New("asks: cursor has no id")
	}
	n, err := strconv.ParseInt(ns, 10, 64)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	id, err := ids.Parse(sid)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	return time.Unix(0, n).UTC(), id, nil
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

func idOf(u pgtype.UUID) ids.ID {
	id, _ := ids.FromUUID(u)
	return id
}

func ulidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	return ptr(idOf(u).String())
}

func intPtr(v *int64) *int {
	if v == nil {
		return nil
	}
	return ptr(int(*v))
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	return ptr(t.UTC())
}

func i32(n int64) int32 { return int32(min(max(n, 0), 1<<30)) } //nolint:gosec // clamped

// itemName is the product's name and variant, else the asked words.
func itemName(name, variant *string, query string) string {
	if name == nil {
		return query
	}
	if variant != nil && strings.TrimSpace(*variant) != "" {
		return *name + " " + *variant
	}
	return *name
}

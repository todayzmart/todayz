// Package search is places, discovery and product search (W1-06; 01 §11,
// 02 §7, 04 §2.3, D-40, D-42, D-63, D-99, D-101).
//
// What it serves, all public and read-only except for the search log:
//
//   - places: PIN code, locality and city autocomplete and the reverse
//     geocode for the location bar (a curated set for Bengaluru, Hyderabad
//     and Chennai until the all-India import);
//   - discover: the StoreMap summary, the shop list, the shop page and its
//     catalogue;
//   - search: filter first (in stock, a live store of a verified business,
//     inside the radius), then the 01 §11 score with weights from
//     platform_settings, grouped by store, with the D-101 summary line,
//     freshness, badges and delivery fee, and suggestions while typing.
//
// Rules this package keeps:
//
//   - Only `live` stores of `verified` businesses are ever shown (D-40); the
//     queries say so literally, which also lets the planner use the partial
//     index ix_stores_location.
//   - Every ranking weight and the stale-stock window come from
//     platform_settings through settings.Cache; nothing here hard-codes one.
//   - Search never calls a model (01 §11.1). The refine-chip parser is not
//     built yet: `parse` is accepted and `chips` is always empty.
//   - The customer's coordinates are never stored or logged; search_log and
//     the search events carry no location (geo_cell stays NULL until the H3
//     cell is computed) and no query text in events.
//   - Logging a search never fails the search.
package search

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/searchdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// OpenChecker says whether a store is open at t from its stored hours
// (stores.hours JSON) and manual close. The business domain owns the hours
// rules (W1-04, business.OpenAt); cmd/todayz wires it in.
type OpenChecker interface {
	OpenAt(storedHours []byte, closedUntil *time.Time, t time.Time) bool
}

// OpenFunc adapts a function to OpenChecker.
type OpenFunc func(storedHours []byte, closedUntil *time.Time, t time.Time) bool

// OpenAt calls f.
func (f OpenFunc) OpenAt(storedHours []byte, closedUntil *time.Time, t time.Time) bool {
	return f(storedHours, closedUntil, t)
}

// OfferLookup says which store items and stores have a live item-level
// offer at t. Offers come with W2-07; until then NoOffers answers "none", so
// the has-offer chip returns nothing and has_offer scores 0 for everyone.
// A missing key means no offer.
type OfferLookup interface {
	ItemOffers(ctx context.Context, storeItemIDs []ids.ID, t time.Time) (map[ids.ID]bool, error)
	StoreOffers(ctx context.Context, storeIDs []ids.ID, t time.Time) (map[ids.ID]bool, error)
}

// NoOffers is the OfferLookup before offers exist: nothing has an offer.
type NoOffers struct{}

// ItemOffers returns no offers.
func (NoOffers) ItemOffers(context.Context, []ids.ID, time.Time) (map[ids.ID]bool, error) {
	return nil, nil
}

// StoreOffers returns no offers.
func (NoOffers) StoreOffers(context.Context, []ids.ID, time.Time) (map[ids.ID]bool, error) {
	return nil, nil
}

// Deps is what the service needs. DB and Settings may be nil in a
// route-only test: every operation then answers 503.
type Deps struct {
	DB       *db.DB
	Settings *settings.Cache
	Clock    clock.Clock
	Log      *slog.Logger
	// Open decides open-now; nil counts every store as closed.
	Open OpenChecker
	// Offers decides has-offer; nil is NoOffers.
	Offers OfferLookup
	// UserID returns the signed-in user's id (a ULID) or "" for an
	// anonymous caller; search_log and the events record it. nil is
	// always anonymous.
	UserID func(ctx context.Context) string
}

// Service implements the discover routes.
type Service struct {
	db       *db.DB
	settings *settings.Cache
	clock    clock.Clock
	log      *slog.Logger
	open     OpenChecker
	offers   OfferLookup
	userID   func(ctx context.Context) string
	events   *eventcat.Catalogue
}

// errUnavailable is why the service cannot serve (no database or settings).
var errUnavailable = errors.New("search: not configured")

// New builds the service. cat is the product event catalogue
// (eventcat.Load(events.FS)); it may be nil, and then no event is written.
func New(d Deps, cat *eventcat.Catalogue) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("search: clock is required")
	}
	s := &Service{
		db: d.DB, settings: d.Settings, clock: d.Clock, log: d.Log,
		open: d.Open, offers: d.Offers, userID: d.UserID, events: cat,
	}
	if s.open == nil {
		s.open = OpenFunc(func([]byte, *time.Time, time.Time) bool { return false })
	}
	if s.offers == nil {
		s.offers = NoOffers{}
	}
	if s.userID == nil {
		s.userID = func(context.Context) string { return "" }
	}
	return s, nil
}

// ready reports whether the routes can run and returns the queries.
func (s *Service) ready(ctx context.Context) (*searchdb.Queries, error) {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "search: not configured (needs the database and settings)")
		return nil, problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return searchdb.New(planPerCall{s.db.Q(ctx)}), nil
}

// planPerCall runs the discover reads as unnamed statements
// (pgx.QueryExecModeCacheDescribe: one round trip, description cached
// client-side), so Postgres plans each call with its own arguments.
//
// Under the default statement cache, Postgres switches a prepared statement
// to its generic plan after five runs. Every discover read hangs on a point,
// a radius and optional filters ($n IS NULL OR …), which a generic plan can
// only guess: for SearchItems it guessed one store in the radius and eleven
// products, and chose a nested loop that probed every (store, product) pair
// in the radius — 0.5 s at 5 km with the dev seed and 1,000 stores, against
// 60 ms for the plan made with the arguments. Planning costs under a
// millisecond per query. Writes keep the default mode.
type planPerCall struct{ searchdb.DBTX }

func (p planPerCall) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.DBTX.Query(ctx, sql, append([]any{pgx.QueryExecModeCacheDescribe}, args...)...)
}

func (p planPerCall) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return p.DBTX.QueryRow(ctx, sql, append([]any{pgx.QueryExecModeCacheDescribe}, args...)...)
}

// internal wraps an unexpected error as a 500 problem.
func internal(err error) error {
	return problem.Wrap(genproblems.Internal, err)
}

// now is the service's time (the injected clock; the test clock locally).
func (s *Service) now() time.Time { return s.clock.Now() }

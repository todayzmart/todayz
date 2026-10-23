// Package catalogue is what shops sell and how much they have (W1-05; 01 §4,
// 02 §3.3, 03 §3 and stock rules 1–8, 04 §2.6): the seeded category tree,
// the master products, a store's items with price, MRP and listing, the
// append-only stock ledger with its cached counters, and the expiry and cost
// batches that stock moves through (D-99).
//
// Rules this package keeps:
//
//   - store_items.on_hand is only ever moved by inserting a stock_movements
//     row (the trigger adds delta); a batch's quantity only by inserting a
//     stock_movement_batches row. Nothing here UPDATEs a counter, except the
//     reconcile job's repair (internal/jobs/catalogue.go).
//   - Every stock write goes through Writer, inside the caller's transaction,
//     and takes its locks in one order: the store's catalogue sequence (the
//     stores row), then the item, then the item's batches by id.
//   - Every /v1/biz route is checked by the permission engine (slot 15) in the
//     path's business and store; a movement kind or a price change that needs
//     more than the route's action is checked again here (02 §3.3).
//   - store_items.reserved belongs to W2-02 (stock_reservations, rules 2–4).
package catalogue

import (
	"context"
	"encoding/base64"
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
	"github.com/todayz/backend/gen/sqlc/cataloguedb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Deps is what the service needs. DB, Settings and Authz may be missing in a
// route-only test: the operations that need them answer 503.
type Deps struct {
	Config   *config.Config
	DB       *db.DB
	Clock    clock.Clock
	Log      *slog.Logger
	Settings *settings.Cache
	// Authz runs the second check of a movement kind or a price change.
	Authz authz.Authorizer
}

// Service implements the catalogue routes.
type Service struct {
	db       *db.DB
	clock    clock.Clock
	settings *settings.Cache
	az       authz.Authorizer
	audit    *audit.Writer
	writer   *Writer
	ids      *ids.Generator
}

var errUnavailable = errors.New("catalogue: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("catalogue: a clock is required")
	}
	s := &Service{db: d.DB, clock: d.Clock, settings: d.Settings, az: d.Authz, writer: NewWriter(d.Clock), ids: ids.NewGenerator()}
	if d.DB != nil {
		var ipKey []byte
		if d.Config != nil && d.Config.Keys.PIIHashKey.IsSet() {
			ipKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		s.audit = audit.New(d.DB, d.Clock, ipKey)
	}
	return s, nil
}

// Writer is the stock writer other domains use (orders, POS, voice).
func (s *Service) Writer() *Writer { return s.writer }

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "catalogue: not configured (needs the database and settings)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) q(ctx context.Context) *cataloguedb.Queries { return cataloguedb.New(s.db.Q(ctx)) }

// --- settings -----------------------------------------------------------------

func (s *Service) lowStockDefault(ctx context.Context) (int32, error) {
	n, err := s.settings.Int(ctx, settings.LowStockDefault)
	if err != nil {
		return 0, fmt.Errorf("catalogue: low_stock_default: %w", err)
	}
	return int32(min(max(n, 0), 1<<30)), nil //nolint:gosec // clamped
}

func (s *Service) staleAfter(ctx context.Context) (time.Duration, error) {
	d, err := s.settings.Duration(ctx, settings.StockStaleAfterHours)
	if err != nil {
		return 0, fmt.Errorf("catalogue: stock_stale_after_hours: %w", err)
	}
	return d, nil
}

// view is what turning an item row into the API shape needs.
type view struct {
	now        time.Time
	lowDefault int32
	staleAfter time.Duration
}

func (s *Service) itemView(ctx context.Context) (view, error) {
	low, err := s.lowStockDefault(ctx)
	if err != nil {
		return view{}, err
	}
	stale, err := s.staleAfter(ctx)
	if err != nil {
		return view{}, err
	}
	return view{now: s.clock.Now(), lowDefault: low, staleAfter: stale}, nil
}

// Stale reports whether stock last confirmed at t is stale at now (01 §4):
// never confirmed, or more than after ago.
func Stale(t *time.Time, now time.Time, after time.Duration) bool {
	return t == nil || now.Sub(*t) > after
}

// LowStock reports whether available is at or below the item's threshold,
// or the platform default when the item has none.
func LowStock(available int32, threshold *int32, def int32) bool {
	if threshold != nil {
		return available <= *threshold
	}
	return available <= def
}

// --- dates (IST calendar days) ----------------------------------------------------

// Today is the IST calendar date of t, as midnight UTC of that date (the
// form pgtype.Date uses).
func Today(t time.Time) time.Time {
	l := t.In(clock.IST)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
}

// --- helpers -----------------------------------------------------------------

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

// scope reads bid and sid (the engine already checked them).
func scope(bid, sid string) (authz.Scope, error) {
	b, err := pathID(bid)
	if err != nil {
		return authz.Scope{}, err
	}
	sc := authz.Scope{BusinessID: b}
	if sid != "" {
		if sc.StoreID, err = pathID(sid); err != nil {
			return authz.Scope{}, err
		}
	}
	return sc, nil
}

func etag(version int32) string { return `"v` + strconv.FormatInt(int64(version), 10) + `"` }

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

func asProblem(err error) error {
	var p *problem.Error
	if errors.As(err, &p) {
		return p
	}
	return problem.Wrap(genproblems.Internal, err)
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

// text refuses invalid UTF-8 and control characters in free text.
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

func fieldErr(path, reason string) error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

// also runs a second permission check in the request's scope.
func (s *Service) also(ctx context.Context, action string, sc authz.Scope) error {
	if s.az == nil {
		return problem.Wrap(genproblems.Internal, errors.New("catalogue: no authorizer"))
	}
	p, _ := authn.FromContext(ctx)
	_, err := s.az.Check(ctx, p, action, sc)
	return err
}

// likePattern is the ILIKE pattern for q with %, _ and \ escaped.
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// limitOf is the page size (default 20, at most 100; the validator already
// refused anything else).
func limitOf(l *int) int32 {
	if l == nil {
		return 20
	}
	return int32(min(max(*l, 1), 100)) //nolint:gosec // clamped
}

// cursor is "<key>\x00<ulid>", base64url: the keyset of the last row.
func encodeCursor(key string, id ids.ID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(key + "\x00" + id.String()))
}

func decodeCursor(c string) (string, ids.ID, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", ids.Zero, err
	}
	key, sid, ok := strings.Cut(string(b), "\x00")
	if !ok {
		return "", ids.Zero, errors.New("catalogue: cursor has no id")
	}
	id, err := ids.Parse(sid)
	if err != nil {
		return "", ids.Zero, err
	}
	return key, id, nil
}

func badCursor() error { return fieldErr("query.cursor", "not a cursor from this list") }

// uuidText reads a uuid in Postgres text form (as jsonb carries it).
func uuidText(s string) (ids.ID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return ids.Zero, fmt.Errorf("catalogue: uuid: %w", err)
	}
	id, _ := ids.FromUUID(u)
	return id, nil
}

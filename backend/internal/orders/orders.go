// Package orders is online booking (W2-02; 01 §5–6, 02 §3.3, 03 §3–4, 04
// §2.7): the checkout quote, placing an order, the shop's accept, reject,
// ready, hand-over and cancel, the customer's cancel, extension and new
// code, the accept and no-show timers, and the order reads both apps show.
//
// Rules this package keeps:
//
//   - Every status change runs in one transaction that holds the order row
//     (FOR UPDATE) first, checks the move against Machine (the Go copy of
//     trg_orders_guard), writes the order, an order_events row, the stream
//     events and the customer's notification, and commits them together.
//     Two requests on one order (BK-19, BK-20) serialise on that lock; the
//     second sees the first's status and answers 409 invalid_transition.
//   - Nothing is reserved at placing (D-01). Accept holds every line as a
//     stock_reservations row (the trigger moves store_items.reserved); an
//     end state releases them; hand-over sells each line through the stock
//     writer (sale_online) and marks its reservation consumed.
//   - Lock order: the order row, then (hand-over only, through the stock
//     writer) the stores row, then store_items in id order. Accept and the
//     release at an end state lock their items in id order before the
//     reservation rows move them, so two orders sharing items never wait on
//     each other in a cycle.
//   - The pickup code is 4 random digits, stored sealed (code_enc, AES-GCM
//     under ORDER_CODE_ENC_KEY) and as an HMAC (code_hmac, under
//     ORDER_CODE_HMAC_KEY) (D-04). It is compared in constant time against
//     this order only, and never appears in a log, an event, job arguments,
//     an error, a notification or any response except the owner's GET.
//   - Deadlines are strict: at or after accept_by a placed order cannot be
//     accepted, rejected or cancelled, and at or after hold_until a held one
//     cannot be handed over, cancelled or extended; the timer expires it.
//     A late or repeated timer run is a no-op (BK-21).
//   - Times come from the injected clock; numbers from platform_settings.
package orders

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Deps is what the service needs. Only Clock is required: an operation whose
// dependency is missing answers 503 (route-only tests build the service
// with nothing else).
type Deps struct {
	Config   *config.Config
	DB       *db.DB
	Clock    clock.Clock
	Log      *slog.Logger
	Settings *settings.Cache
	// Notices writes the store's "new order" and the customer's order
	// updates in the order's transaction (internal/notify).
	Notices notice.Sender
	// Jobs enqueues the accept and no-show timers in the order's
	// transaction.
	Jobs platformjobs.Enqueuer
	// Stock sells a collected line (the catalogue's stock writer).
	Stock Stock
	// Open reports whether a store is open at a time (business.OpenAt).
	Open OpenFunc
	// Pricer applies offers; nil means no offers (W3 brings them).
	Pricer Pricer
	// AskPricer prices a line from an ask reply; nil until W3-16 (D-101).
	AskPricer AskPricer
	// Credits holds and settles credits; nil means credits are not live.
	Credits Credits
	// CodeRand is the randomness pickup codes are drawn from; nil means
	// crypto/rand. Only tests set it, to force a code collision.
	CodeRand io.Reader
}

// Stock is what orders needs from the catalogue's stock writer.
type Stock interface {
	// Sell writes one sale_online movement of qty for the item inside tx
	// (the transaction in ctx) and returns the movement id. clientID makes
	// it idempotent: a second call with the same id returns the first
	// movement.
	Sell(ctx context.Context, tx pgx.Tx, s Sale) (ids.ID, error)
}

// Sale is one collected line.
type Sale struct {
	StoreID, ItemID ids.ID
	Qty             int32
	OrderID         ids.ID
	ClientID        ids.ID // the reservation id
	UserID          ids.ID // the staff member handing over
}

// OpenFunc reports whether a store with these stored hours and closed-until
// is open at t.
type OpenFunc func(hours []byte, closedUntil *time.Time, t time.Time) bool

// Service implements the orders routes, timers and cross-domain seams.
type Service struct {
	db        *db.DB
	clock     clock.Clock
	settings  *settings.Cache
	notices   notice.Sender
	jobs      platformjobs.Enqueuer
	stock     Stock
	open      OpenFunc
	pricer    Pricer
	askPricer AskPricer
	credits   Credits
	codes     *codeKeys // nil when either code key is missing
	phones    *pii.Sealer
	ids       *ids.Generator
}

var errUnavailable = errors.New("orders: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("orders: a clock is required")
	}
	s := &Service{
		db: d.DB, clock: d.Clock, settings: d.Settings, notices: d.Notices, jobs: d.Jobs,
		stock: d.Stock, open: d.Open, pricer: d.Pricer, askPricer: d.AskPricer, credits: d.Credits,
		ids: ids.NewGenerator(),
	}
	if s.pricer == nil {
		s.pricer = NoOffers{}
	}
	if s.credits == nil {
		s.credits = NoCredits{}
	}
	if d.Config != nil {
		k, err := newCodeKeys(d.Config.Keys.OrderCodeEncKey, d.Config.Keys.OrderCodeHMACKey)
		if err != nil {
			return nil, err
		}
		if k != nil && d.CodeRand != nil {
			k.rnd = d.CodeRand
		}
		s.codes = k
		if d.Config.Keys.PIIEncKey.IsSet() {
			ph, err := pii.NewSealer(d.Config.Keys.PIIEncKey.Bytes())
			if err != nil {
				return nil, fmt.Errorf("orders: PII_ENC_KEY: %w", err)
			}
			s.phones = ph
		}
	}
	return s, nil
}

// ready refuses with 503 when the database or settings are missing.
func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "orders: not configured (needs the database and settings)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

// readyToWrite also needs what a status change writes through: the
// notification sender, the timer queue and the stock writer.
func (s *Service) readyToWrite(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if s.notices == nil || s.jobs == nil || s.stock == nil || s.open == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "orders: not configured (needs the notification sender, the job queue, the stock writer and the opening hours)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

// now is the clock's time at the database's precision (microseconds), so a
// time written and read back compares equal (the no-show timer's args).
func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) q(ctx context.Context) *ordersdb.Queries { return ordersdb.New(s.db.Q(ctx)) }

// tx is the transaction db.InTx put in ctx.
func tx(ctx context.Context) (pgx.Tx, error) {
	t, ok := db.TxFrom(ctx)
	if !ok {
		return nil, errors.New("orders: no transaction in context")
	}
	return t, nil
}

// --- settings ------------------------------------------------------------------

func (s *Service) duration(ctx context.Context, k settings.Key) (time.Duration, error) {
	d, err := s.settings.Duration(ctx, k)
	if err != nil {
		return 0, fmt.Errorf("orders: %s: %w", k, err)
	}
	return d, nil
}

func (s *Service) count(ctx context.Context, k settings.Key) (int, error) {
	n, err := s.settings.Int(ctx, k)
	if err != nil {
		return 0, fmt.Errorf("orders: %s: %w", k, err)
	}
	return int(min(max(n, 0), 1<<30)), nil
}

// --- helpers -------------------------------------------------------------------

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

func asProblem(err error) error {
	var p *problem.Error
	if errors.As(err, &p) {
		return p
	}
	return problem.Wrap(genproblems.Internal, err)
}

func fieldErr(path, reason string) *problem.Error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

func invalidTransition() *problem.Error { return problem.New(genproblems.InvalidTransition, "") }

func notFound() *problem.Error { return problem.New(genproblems.NotFound, "") }

// mustID turns a non-null database id into an ids.ID.
func mustID(u pgtype.UUID) ids.ID {
	id, _ := ids.FromUUID(u)
	return id
}

func ptr[T any](v T) *T { return &v }

func i32(n int) int32 { return int32(min(max(n, -1<<31), 1<<31-1)) } //nolint:gosec // clamped

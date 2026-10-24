// Package pos is the shop counter (W2-04; 01 §6–8, 02 §4.3, 03 §5, 04 §2.6):
// the online sale and its reversal, the offline sync, the bill QR a customer
// claims, the counter code, receipt links and the customer's purchases.
//
// Rules this package keeps:
//
//   - A sale is idempotent by (store, clientId), the device's ULID for the
//     bill, online and at sync. Its money is checked by ck_pos_sales_money:
//     line_total = unit × qty − offer; subtotal = Σ line_total − bill offer;
//     eligible = subtotal − manual discounts; total = eligible − credits ×
//     100 + round-off to the nearest ₹1 (D-94).
//   - Online, a catalogued line must carry the item's price now (409
//     price_changed) and the server applies offers (the Pricer seam). At
//     sync a sale is recorded as the device sent it: prices are never
//     rewritten and stock may go below zero (02 §4.3); the item is then
//     "count needed".
//   - Every catalogued line is one sale_pos movement through the stock seam
//     (the catalogue's writer), idempotent on the line's client id; a
//     reversal puts each back as a pos_reversal movement.
//   - Credits are spent only through a bill QR, cashback and referral
//     qualification only on a bill_qr or counter_code link (D-07); both go
//     through the Credits and Referrals seams, whose defaults do nothing.
//   - The bill QR token is an HMAC (BILL_QR_SIGNING_KEY) over the claim, the
//     store, the amount, the expiry and a stored nonce; it expires after
//     120 s and the first confirm wins (the claim row is held FOR UPDATE).
//   - The counter code is TOTP (RFC 6238: SHA-1, 60 s, 4 digits) on a
//     per-device secret sealed under DEVICE_SECRET_ENC_KEY, checked at the
//     current step and one either side; a (device, step) is accepted once.
//     Five wrong codes lock the bill. Codes, tokens and phones are never
//     logged or returned.
//   - Times come from the injected clock; numbers from platform_settings.
package pos

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/posdb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
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
	// Authz re-checks a sync entry as its own action and a manual discount
	// as pos.discount (D-06).
	Authz authz.Authorizer
	// Stock writes the sale, reversal and stock-change movements (the
	// catalogue's stock writer, jobs.PosStock).
	Stock Stock
	// Pricer applies offers to an online sale; nil means no offers.
	Pricer Pricer
	// Credits holds, spends and returns credits and posts cashback; nil
	// means credits are not live.
	Credits Credits
	// Referrals qualifies and revokes a referral; nil means none.
	Referrals Referrals
	// Rand is the randomness for nonces, secrets and receipt tokens; nil
	// means crypto/rand.
	Rand io.Reader
}

// Service implements the pos, sync and receipts routes.
type Service struct {
	db        *db.DB
	clock     clock.Clock
	settings  *settings.Cache
	az        authz.Authorizer
	stock     Stock
	pricer    Pricer
	credits   Credits
	referrals Referrals
	rnd       io.Reader
	ids       *ids.Generator

	billKey []byte      // nil when BILL_QR_SIGNING_KEY is missing
	devices *pii.Sealer // DEVICE_SECRET_ENC_KEY: counter-code secrets
	phones  *pii.Sealer // PII_ENC_KEY: a typed phone on a sale
	hasher  *pii.Hasher // PII_HASH_KEY: the phone lookup
}

var errUnavailable = errors.New("pos: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("pos: a clock is required")
	}
	s := &Service{
		db: d.DB, clock: d.Clock, settings: d.Settings, az: d.Authz, stock: d.Stock,
		pricer: d.Pricer, credits: d.Credits, referrals: d.Referrals, rnd: d.Rand,
		ids: ids.NewGenerator(),
	}
	if s.pricer == nil {
		s.pricer = NoOffers{}
	}
	if s.credits == nil {
		s.credits = NoCredits{}
	}
	if s.referrals == nil {
		s.referrals = NoReferrals{}
	}
	if s.rnd == nil {
		s.rnd = rand.Reader
	}
	if d.Config != nil {
		k := d.Config.Keys
		if k.BillQRSigningKey.IsSet() {
			s.billKey = k.BillQRSigningKey.Bytes()
		}
		var err error
		if k.DeviceSecretEncKey.IsSet() {
			if s.devices, err = pii.NewSealer(k.DeviceSecretEncKey.Bytes()); err != nil {
				return nil, fmt.Errorf("pos: DEVICE_SECRET_ENC_KEY: %w", err)
			}
		}
		if k.PIIEncKey.IsSet() {
			if s.phones, err = pii.NewSealer(k.PIIEncKey.Bytes()); err != nil {
				return nil, fmt.Errorf("pos: PII_ENC_KEY: %w", err)
			}
		}
		if k.PIIHashKey.IsSet() {
			if s.hasher, err = pii.NewHasher(k.PIIHashKey.Bytes()); err != nil {
				return nil, fmt.Errorf("pos: PII_HASH_KEY: %w", err)
			}
		}
	}
	return s, nil
}

// ready refuses with 503 when the database or settings are missing.
func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "pos: not configured (needs the database and settings)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

// readyToSell also needs what a sale writes through: the stock writer and
// the phone keys (a typed phone is hashed and sealed).
func (s *Service) readyToSell(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if s.stock == nil || s.phones == nil || s.hasher == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "pos: not configured (needs the stock writer, PII_ENC_KEY and PII_HASH_KEY)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

// now is the clock's time at the database's precision (microseconds).
func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) q(ctx context.Context) *posdb.Queries { return posdb.New(s.db.Q(ctx)) }

// tx is the transaction db.InTx put in ctx.
func tx(ctx context.Context) (pgx.Tx, error) {
	t, ok := db.TxFrom(ctx)
	if !ok {
		return nil, errors.New("pos: no transaction in context")
	}
	return t, nil
}

// also checks a second action for the signed-in staff member in the
// route's scope (pos.discount on a manual discount, a sync entry's kind).
func (s *Service) also(ctx context.Context, action string, sc authz.Scope) error {
	if s.az == nil {
		return problem.Wrap(genproblems.Internal, errors.New("pos: no authorizer"))
	}
	p, _ := authn.FromContext(ctx)
	_, err := s.az.Check(ctx, p, action, sc)
	return err
}

// random fills n bytes from the service's randomness.
func (s *Service) random(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(s.rnd, b); err != nil {
		return nil, fmt.Errorf("pos: random: %w", err)
	}
	return b, nil
}

// --- settings ------------------------------------------------------------------

func (s *Service) duration(ctx context.Context, k settings.Key) (time.Duration, error) {
	d, err := s.settings.Duration(ctx, k)
	if err != nil {
		return 0, fmt.Errorf("pos: %s: %w", k, err)
	}
	return d, nil
}

func (s *Service) count(ctx context.Context, k settings.Key) (int, error) {
	n, err := s.settings.Int(ctx, k)
	if err != nil {
		return 0, fmt.Errorf("pos: %s: %w", k, err)
	}
	return int(min(max(n, 0), 1<<30)), nil
}

func (s *Service) paise(ctx context.Context, k settings.Key) (int64, error) {
	p, err := s.settings.Paise(ctx, k)
	if err != nil {
		return 0, fmt.Errorf("pos: %s: %w", k, err)
	}
	return int64(p), nil
}

func (s *Service) flag(ctx context.Context, k settings.Key) (bool, error) {
	b, err := s.settings.Bool(ctx, k)
	if err != nil {
		return false, fmt.Errorf("pos: %s: %w", k, err)
	}
	return b, nil
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

func notFound() *problem.Error { return problem.New(genproblems.NotFound, "") }

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// mustID turns a non-null database id into an ids.ID.
func mustID(u pgtype.UUID) ids.ID {
	id, _ := ids.FromUUID(u)
	return id
}

// optID is a nullable database id, ids.Zero when null.
func optID(u pgtype.UUID) ids.ID {
	if !u.Valid {
		return ids.Zero
	}
	return mustID(u)
}

// nullID is id as a nullable database id (null for ids.Zero).
func nullID(id ids.ID) pgtype.UUID {
	if id == ids.Zero {
		return pgtype.UUID{}
	}
	return ids.UUID(id)
}

func idString(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	return ptr(mustID(u).String())
}

func ptr[T any](v T) *T { return &v }

func i32(n int64) int32 { return int32(min(max(n, -1<<31), 1<<31-1)) } //nolint:gosec // clamped

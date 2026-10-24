// Package referrals is the referral programme (W2-03; 01 §8, D-13, D-26,
// D-32, D-72; 03 referrals, referral_clicks; 04 /v1/public/ref,
// /v1/me/referrals, /v1/biz/businesses/{bid}/referrals).
//
// Rules this package keeps:
//
//   - Three kinds: retailer_customer (a store's standee or link → a
//     customer), customer_customer, any_shop (a customer or a business → a
//     new business). Each kind's reward amounts are read from settings at
//     claim time and kept on the row (RF-10).
//   - Attribution: opening a link records a click and sets the
//     __Secure-tz_ref cookie (the click id). First touch wins: a cookie whose
//     click is still inside ref_attribution_window_days is kept, and at the
//     claim a valid click beats a typed code. The click must be before
//     sign-up and at most the window before it (RF-08).
//   - The claim: once per account (unique referee), within the window of
//     sign-up, before the first completed order. Self (own code, a store of
//     a business the caller works for, businesses sharing staff) is 422
//     referral_self; a second claim or a phone already used is 409
//     referral_already_claimed.
//   - Same device (D-13): a claim from a device tied to the referrer's
//     users, or used by another account in the last 90 days (a fixed
//     constant, 01 §17), is recorded rejected_same_device and never
//     qualifies. A device that has carried 3+ accounts raises a risk flag.
//   - Qualification: a customer's first completed order or verified POS
//     sale with qualifying amount ≥ ref_qualifying_min_order_paise; a new
//     shop's verification plus ref_shop_qualifying_orders completed orders
//     or verified linked sales. The qualifying purchase's transaction
//     marks the referral qualified and enqueues the referral_reward job;
//     each side posts after it commits, in its own transaction (D-169). A
//     shop's reward goes to its business's primary store (oldest live) and
//     waits for one.
//   - A reversed purchase revokes a qualification whose rewards have not
//     posted; after posting the rewards stay and a risk flag is raised (01 §7).
package referrals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/referralsdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Kinds (referrals.kind).
const (
	KindRetailerCustomer = "retailer_customer"
	KindCustomerCustomer = "customer_customer"
	KindAnyShop          = "any_shop"
)

// Party types (referrer_type, referee_type).
const (
	partyCustomer = "customer"
	partyStore    = "store"
	partyBusiness = "business"
)

// Statuses (referrals.status).
const (
	statusPending            = "pending"
	statusQualified          = "qualified"
	statusRewarded           = "rewarded"
	statusRejected           = "rejected"
	statusRejectedSameDevice = "rejected_same_device"
)

// Sources (referrals.source).
const (
	sourceStandee = "qr_standee"
	sourceLink    = "link"
	sourceCode    = "code"
)

const (
	// sameDeviceWindow is the D-13 window: a fixed constant (01 §17), not a setting.
	sameDeviceWindow = 90 * 24 * time.Hour
	// deviceAccountsFlag raises referral.device_accounts (01 §8: 3 or more accounts).
	deviceAccountsFlag = 3
	// maxRewardCredits is the referrals CHECK bound on a reward snapshot.
	maxRewardCredits = 100_000 //settings:ok the ck_referrals_credits bound, not a setting
	// paisePerCredit: one credit is ₹1 (01 §7).
	paisePerCredit = 100 //settings:ok ₹1 per credit, a unit not a setting
	// sweepBatch bounds one sweep pass per list.
	sweepBatch = 200
)

// Deps is what the service needs. Clock and Ledger are required: without
// the database, settings or the hash key every operation answers 503.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	// Hasher keys the device, phone, ip and user-agent hashes (PII_HASH_KEY).
	Hasher *pii.Hasher
	// Ledger posts rewards (required: a missing ledger is a wiring bug,
	// never a silent switch-off).
	Ledger Ledger
	// Notices sends "referral unlocked"; nil sends nothing.
	Notices notice.Sender
	// Jobs enqueues referral_reward in the qualifying purchase's
	// transaction; nil leaves the posting to the sweep (router tests, a
	// process without a queue).
	Jobs platformjobs.Enqueuer
	// PublicURL is the API's public base URL (the share links).
	PublicURL string
	// CustomerApp and BusinessApp are the apps' origins (the redirects).
	CustomerApp string
	BusinessApp string
}

// Service implements the referrals routes, the purchase hooks, the sweep and
// the account-deletion hook.
type Service struct {
	db          *db.DB
	clock       clock.Clock
	settings    *settings.Cache
	hasher      *pii.Hasher
	ledger      Ledger
	notices     notice.Sender
	jobs        platformjobs.Enqueuer
	publicURL   string
	customerApp string
	businessApp string
	ids         *ids.Generator
}

var errUnavailable = errors.New("referrals: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("referrals: a clock is required")
	}
	if d.Ledger == nil {
		return nil, errors.New("referrals: a ledger is required")
	}
	s := &Service{
		db: d.DB, clock: d.Clock, settings: d.Settings, hasher: d.Hasher, ledger: d.Ledger, notices: d.Notices, jobs: d.Jobs,
		publicURL:   strings.TrimRight(d.PublicURL, "/"),
		customerApp: strings.TrimRight(d.CustomerApp, "/"),
		businessApp: strings.TrimRight(d.BusinessApp, "/"),
		ids:         ids.NewGenerator(),
	}
	return s, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil || s.hasher == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "referrals: not configured (needs the database, settings and PII_HASH_KEY)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) q(ctx context.Context) *referralsdb.Queries { return referralsdb.New(s.db.Q(ctx)) }

// window is ref_attribution_window_days.
func (s *Service) window(ctx context.Context) (time.Duration, error) {
	w, err := s.settings.Duration(ctx, settings.RefAttributionWindowDays)
	if err != nil {
		return 0, fmt.Errorf("referrals: window: %w", err)
	}
	return max(w, 0), nil
}

// rewardKeys are each kind's settings, referrer then referee (01 §8).
var rewardKeys = map[string][2]settings.Key{
	KindRetailerCustomer: {settings.RefRetailerCustomerReferrerCredits, settings.RefRetailerCustomerRefereeCredits},
	KindCustomerCustomer: {settings.RefCustomerCustomerReferrerCredits, settings.RefCustomerCustomerRefereeCredits},
	KindAnyShop:          {settings.RefAnyShopReferrerCredits, settings.RefAnyShopRefereeCredits},
}

// rewards reads a kind's amounts now.
func (s *Service) rewards(ctx context.Context, kind string) (referrer, referee int32, err error) {
	keys, ok := rewardKeys[kind]
	if !ok {
		return 0, 0, fmt.Errorf("referrals: unknown kind %q", kind)
	}
	var out [2]int32
	for i, k := range keys {
		n, err := s.settings.Int(ctx, k)
		if err != nil {
			return 0, 0, fmt.Errorf("referrals: %s: %w", k, err)
		}
		out[i] = int32(min(max(n, 0), maxRewardCredits))
	}
	return out[0], out[1], nil
}

// OnAccountDelete closes the pending referral the user joined through (it
// can no longer qualify). Referrals the user made stay; a reward due to a
// deleted referrer is marked done without posting. It runs in identity's
// deletion transaction and is idempotent. The device and phone hashes stay
// on the row for the same-device and same-phone rules (TODO(lead): their
// retention).
func (s *Service) OnAccountDelete(ctx context.Context, userID ids.ID) error {
	if s.db == nil {
		return errUnavailable
	}
	q := s.q(ctx)
	rows, err := q.PendingRefereeReferralOf(ctx, ids.UUID(userID))
	if err != nil {
		return err
	}
	reason := "account_deleted"
	for _, id := range rows {
		if err := q.RejectReferral(ctx, referralsdb.RejectReferralParams{Reason: &reason, Now: s.now(), ID: id}); err != nil {
			return err
		}
	}
	return nil
}

// --- small helpers -------------------------------------------------------------

func idOf(u pgtype.UUID) ids.ID {
	id, _ := ids.FromUUID(u)
	return id
}

func optUUID(id ids.ID) pgtype.UUID {
	if id == ids.Zero {
		return pgtype.UUID{}
	}
	return ids.UUID(id)
}

// publicStatus is the status a client sees: rejected_same_device is shown as
// rejected (the reason is not shown). TODO(lead): confirm hiding the reason.
func publicStatus(st string) string {
	if st == statusRejectedSameDevice {
		return statusRejected
	}
	return st
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

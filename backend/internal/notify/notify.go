// Package notify is the notify domain (W2-05, 01 §15): the in-app inboxes
// (customer C-20, business B-16), per-kind switches, and the outside
// channels (SMS, WhatsApp; web push is a later slice) with delivery tracking
// and a cost metric.
//
// Domains notify through notice.Sender (Service implements it) inside their
// own transaction: the inbox row, its stream event and the job that sends an
// outside message commit or roll back with the change.
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/todayz/backend/gen/sqlc/notifydb"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/realtime"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// KindDeliver is the job that sends one outside message (event kind).
const KindDeliver = "notify_deliver"

// sealField is the PII sealer's field name for a phone (as identity uses).
const sealField = "phone"

// Deps are what the service needs. DB and Clock are required; the rest may
// be nil, and then what needs them is skipped (and counted) rather than
// failing the caller's transaction.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	Config   *config.Config
	Log      *slog.Logger
	// Jobs enqueues notify_deliver in the caller's transaction (the API's
	// River-backed enqueuer, a Fake in tests). Nil: outside messages are
	// skipped.
	Jobs platformjobs.Enqueuer
	// Staff limits the business inbox to the stores a member may see.
	Staff authz.StaffResolver
	// Providers overrides the providers built from Config (tests).
	Providers map[notice.Channel]Provider
	// CostPerUnitPaise is the estimated price of one billable unit per
	// channel, for tz_notify_cost_paise_total and the delivery row.
	// TODO(lead): the AWS SMS price for India (D-212) is not in the plan; until it is,
	// the cost is 0 and only tz_notify_units_total is meaningful. It is an
	// accounting estimate, not a product rule, so it is not a setting.
	CostPerUnitPaise map[notice.Channel]int64
}

// Service implements notice.Sender, the inbox API and the deliver and
// escalation jobs.
type Service struct {
	d         Deps
	sealer    *pii.Sealer
	providers map[notice.Channel]Provider
}

var _ notice.Sender = (*Service)(nil)

// New builds the service. A PII key that is set but malformed is an error;
// an unset key (tests that never send outside) leaves the sealer nil, and
// outside messages are then skipped.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("notify: a clock is required")
	}
	s := &Service{d: d, providers: d.Providers}
	if s.providers == nil {
		s.providers = providersFromConfig(d.Config)
	}
	if d.Config != nil && len(d.Config.Keys.PIIEncKey.Bytes()) > 0 {
		sl, err := pii.NewSealer(d.Config.Keys.PIIEncKey.Bytes())
		if err != nil {
			return nil, fmt.Errorf("notify: %w", err)
		}
		s.sealer = sl
	}
	return s, nil
}

// Errors from Send. They are programming errors of the calling domain (a
// kind it did not add to the catalogue, a phone in a param); the caller's
// transaction should fail.
var (
	ErrUnknownKind = errors.New("notify: kind is not in the catalogue")
	ErrRecipient   = errors.New("notify: exactly one of UserID and BusinessID, matching the kind's app")
	ErrStore       = errors.New("notify: the store is not in the business")
	ErrParams      = errors.New("notify: bad params")
	ErrRef         = errors.New("notify: bad ref")
)

// deliverArgs is notify_deliver's job argument: only the delivery id (the
// phone stays sealed in the row).
type deliverArgs struct {
	DeliveryID ids.ID `json:"deliveryId"`
}

// Send writes n's inbox row inside tx, announces it on the stream
// (inbox.updated on the user's channel, or on the store's, or on every store
// of the business for a business-wide row) and queues each allowed outside
// message. It returns the notification id.
func (s *Service) Send(ctx context.Context, tx pgx.Tx, n notice.Notification) (ids.ID, error) {
	if tx == nil {
		return ids.ID{}, realtime.ErrNoTx
	}
	def, ok := catalogue[n.Kind]
	if !ok {
		return ids.ID{}, fmt.Errorf("%w: %q", ErrUnknownKind, n.Kind)
	}
	var zero ids.ID
	userSet, bizSet := n.UserID != zero, n.BusinessID != zero
	switch {
	case userSet == bizSet:
		return ids.ID{}, ErrRecipient
	case userSet && def.App != AppCustomer, bizSet && def.App != AppBusiness:
		return ids.ID{}, fmt.Errorf("%w: kind %s is for the %s app", ErrRecipient, n.Kind, def.App)
	case n.StoreID != zero && !bizSet:
		return ids.ID{}, ErrRecipient
	}
	if key, ok := validParams(def, n.Params); !ok {
		return ids.ID{}, fmt.Errorf("%w: %q", ErrParams, key)
	}
	if n.Ref != nil && (!reRefType.MatchString(n.Ref.Type) || n.Ref.ID == zero) {
		return ids.ID{}, ErrRef
	}
	q := notifydb.New(tx)
	if n.StoreID != zero {
		in, err := q.StoreInBusiness(ctx, notifydb.StoreInBusinessParams{ID: ids.UUID(n.StoreID), BusinessID: ids.UUID(n.BusinessID)})
		if err != nil {
			return ids.ID{}, fmt.Errorf("notify: store check: %w", err)
		}
		if !in {
			return ids.ID{}, ErrStore
		}
	}

	now := s.d.Clock.Now()
	id := ids.New(now)
	params := n.Params
	if params == nil {
		params = map[string]string{}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return ids.ID{}, fmt.Errorf("notify: encode params: %w", err)
	}
	row := notifydb.InsertNotificationParams{
		ID: ids.UUID(id), App: def.App, Kind: n.Kind, Params: raw, CreatedAt: now,
	}
	if userSet {
		row.UserID = ids.UUID(n.UserID)
	} else {
		row.BusinessID = ids.UUID(n.BusinessID)
	}
	if n.StoreID != zero {
		row.StoreID = ids.UUID(n.StoreID)
	}
	if n.Ref != nil {
		t := n.Ref.Type
		row.RefType, row.RefID = &t, ids.UUID(n.Ref.ID)
	}
	if err := q.InsertNotification(ctx, row); err != nil {
		return ids.ID{}, fmt.Errorf("notify: insert: %w", err)
	}
	countInbox(ctx, def.App, n.Kind)

	if err := s.announce(ctx, tx, q, id, n); err != nil {
		return ids.ID{}, err
	}
	for _, o := range n.Outside {
		if err := s.queueOutside(ctx, tx, q, id, def, n, o); err != nil {
			return ids.ID{}, err
		}
	}
	return id, nil
}

// announce publishes inbox.updated for the new row. The event is the row's
// id and version 1; the client refetches the list and the unread count.
func (s *Service) announce(ctx context.Context, tx pgx.Tx, q *notifydb.Queries, id ids.ID, n notice.Notification) error {
	ev := realtime.Event{Type: realtime.InboxUpdated, ID: id, Version: 1}
	var zero ids.ID
	switch {
	case n.UserID != zero:
		return realtime.Publish(ctx, tx, realtime.UserChannel(n.UserID), ev)
	case n.StoreID != zero:
		return realtime.Publish(ctx, tx, realtime.StoreChannel(n.StoreID), ev)
	}
	stores, err := q.BusinessStores(ctx, ids.UUID(n.BusinessID))
	if err != nil {
		return fmt.Errorf("notify: business stores: %w", err)
	}
	for _, u := range stores {
		sid, ok := ids.FromUUID(u)
		if !ok {
			continue
		}
		if err := realtime.Publish(ctx, tx, realtime.StoreChannel(sid), ev); err != nil {
			return err
		}
	}
	return nil
}

// queueOutside writes a queued delivery and its job, or counts why not. A
// skip is never an error: the inbox row stands on its own (01 §15).
func (s *Service) queueOutside(ctx context.Context, tx pgx.Tx, q *notifydb.Queries, id ids.ID, def kindDef, n notice.Notification, o notice.Outside) error {
	prov := s.providers[o.Channel]
	provName := "none"
	if prov != nil {
		provName = prov.Name()
	}
	skip := func(outcome string) error {
		countDelivery(ctx, o.Channel, provName, n.Kind, outcome)
		return nil
	}
	if !channelAllowed(def, o.Channel) {
		return skip(OutcomeSkippedKind)
	}
	phone, ok := pii.NormalizePhone(o.Phone)
	if !ok {
		return skip(OutcomeSkippedPhone)
	}
	on, err := s.channelOn(ctx, o.Channel)
	if err != nil {
		return err
	}
	if !on {
		return skip(OutcomeSkippedFlag)
	}
	if prov == nil || s.sealer == nil {
		return skip(OutcomeSkippedProvider)
	}
	prefUser := n.PrefUser
	if prefUser == (ids.ID{}) {
		prefUser = n.UserID
	}
	if prefUser != (ids.ID{}) {
		enabled, err := q.PrefEnabled(ctx, notifydb.PrefEnabledParams{
			UserID: ids.UUID(prefUser), App: def.App, Kind: n.Kind, Channel: string(o.Channel),
		})
		if err != nil {
			return fmt.Errorf("notify: pref: %w", err)
		}
		if !enabled {
			return skip(OutcomeSkippedPref)
		}
	}
	if s.d.Jobs == nil {
		telemetry.LoggerOr(ctx, s.d.Log).WarnContext(ctx, "notify: no job queue; outside message skipped",
			"kind", n.Kind, "channel", string(o.Channel))
		return skip(OutcomeSkippedJobs)
	}
	sealed, err := s.sealer.Seal(sealField, []byte(phone))
	if err != nil {
		return fmt.Errorf("notify: seal: %w", err)
	}
	now := s.d.Clock.Now()
	did := ids.New(now)
	if err := q.InsertDelivery(ctx, notifydb.InsertDeliveryParams{
		ID: ids.UUID(did), NotificationID: ids.UUID(id), Channel: string(o.Channel),
		Provider: provName, Lang: pickLang(o.Lang), ToEnc: sealed, Now: now,
	}); err != nil {
		return fmt.Errorf("notify: insert delivery: %w", err)
	}
	if err := s.d.Jobs.EnqueueTx(ctx, tx, KindDeliver, deliverArgs{DeliveryID: did},
		platformjobs.EnqueueOpts{UniqueKey: did.String()}); err != nil {
		return fmt.Errorf("notify: enqueue: %w", err)
	}
	return nil
}

func channelAllowed(def kindDef, ch notice.Channel) bool {
	for _, c := range def.Channels {
		if c == ch {
			return true
		}
	}
	return false
}

// channelOn reads notify_<channel>_enabled. Without settings nothing goes
// outside (the conservative reading).
func (s *Service) channelOn(ctx context.Context, ch notice.Channel) (bool, error) {
	if s.d.Settings == nil {
		return false, nil
	}
	var key settings.Key
	switch ch {
	case notice.SMS:
		key = settings.NotifySmsEnabled
	case notice.WhatsApp:
		key = settings.NotifyWhatsappEnabled
	case notice.Push:
		key = settings.NotifyPushEnabled
	default:
		return false, nil
	}
	on, err := s.d.Settings.Bool(ctx, key)
	if err != nil {
		return false, fmt.Errorf("notify: setting %s: %w", key, err)
	}
	return on, nil
}

// usable reports whether an outside message on ch would be attempted now
// (flag on, provider and sealer present). Escalation uses it to choose
// WhatsApp or SMS (01 §15: WhatsApp when enabled, SMS the fallback).
func (s *Service) usable(ctx context.Context, ch notice.Channel) (bool, error) {
	if s.providers[ch] == nil || s.sealer == nil {
		return false, nil
	}
	return s.channelOn(ctx, ch)
}

package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/todayz/backend/internal/notify"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/platform/realtime"
)

// KindRealtimeOutboxCleanup deletes outbox_events rows past the 24 h replay
// window and raises the watermark (02 §5; maintenance class).
const KindRealtimeOutboxCleanup = "realtime_outbox_cleanup"

// outboxCleanupBatches bounds one cleanup run (5,000 rows a batch).
const outboxCleanupBatches = 40

// EscalationSources returns the "new order not seen" inputs: the orders not
// yet seen by the store (W2-02) and the store contacts (W1-04). Nil, nil
// leaves notify_order_unseen unregistered (the default until both land).
//
// TODO(lead): wire W2-02's UnseenOrders and W1-04's StoreContacts here when
// they land; the job, its claim and its tests are ready (internal/notify).
var EscalationSources = func(Deps) (notice.UnseenOrders, notice.StoreContacts) { return nil, nil }

// registernotify declares the notify domain's job kinds: the outside-message
// sender (event), the realtime outbox cleanup (hourly) and, once its sources
// are wired, the "new order not seen" escalation (every minute).
func registernotify(r *Registry, d Deps) {
	svc := func() (*notify.Service, error) {
		nd := notify.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log}
		if d.Pool != nil {
			nd.DB = db.New(d.Pool)
		}
		return notify.New(nd)
	}

	platformjobs.Define(r, platformjobs.Kind{
		Name: notify.KindDeliver, Owner: "W2-05", Class: platformjobs.ClassNotify,
		Description: "Sends one queued SMS or WhatsApp message (at most once: a retry finds it claimed and sends nothing)",
		Timeout:     30 * time.Second, MaxAttempts: 3,
		Handler: func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			var args struct {
				DeliveryID ids.ID `json:"deliveryId"`
			}
			if err := run.Decode(&args); err != nil {
				return platformjobs.Result{}, err
			}
			s, err := svc()
			if err != nil {
				return platformjobs.Result{}, err
			}
			res, err := s.Deliver(ctx, args.DeliveryID)
			if err != nil {
				return platformjobs.Result{}, err
			}
			sum := map[string]int64{"claimed": 0, "sent": 0, "failed": 0}
			if res.Claimed {
				sum["claimed"] = 1
			}
			switch res.Status {
			case notify.StatusSent:
				sum["sent"] = 1
			case notify.StatusFailed:
				sum["failed"] = 1
			}
			return platformjobs.Result{Summary: sum}, nil
		},
	})

	platformjobs.Define(r, platformjobs.Kind{
		Name: KindRealtimeOutboxCleanup, Owner: "W2-05", Class: platformjobs.ClassMaintenance,
		Description: "Deletes realtime outbox events older than 24 h and raises the replay watermark",
		Schedule:    "17 * * * *", UniqueBy: platformjobs.PerTick, Timeout: 60 * time.Second, MaxAttempts: 3,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			if d.Pool == nil {
				return platformjobs.Result{}, errors.New("notify: no database")
			}
			res, err := realtime.Cleanup(ctx, d.Pool, d.Clock.Now(), outboxCleanupBatches)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"deleted": res.Deleted, "batches": res.Batches}}, nil
		},
	})

	unseen, contacts := EscalationSources(d)
	if unseen == nil || contacts == nil {
		return
	}
	var enq platformjobs.Enqueuer
	if d.Pool != nil {
		if c, err := queue.NewInsertClient(d.Pool, queue.Options{Log: d.Log}); err == nil {
			enq = platformjobs.NewEnqueuer(r, c, d.Clock)
		}
	}
	platformjobs.Define(r, platformjobs.Kind{
		Name: notify.KindOrderUnseen, Owner: "W2-05", Class: platformjobs.ClassNotify,
		Description: "Messages the store once per order still unseen order_unseen_sms_after_min minutes after it was placed",
		Schedule:    "* * * * *", UniqueBy: platformjobs.PerTick, Timeout: 50 * time.Second, MaxAttempts: 1,
		Handler: func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			nd := notify.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log, Jobs: enq}
			if d.Pool != nil {
				nd.DB = db.New(d.Pool)
			}
			s, err := notify.New(nd)
			if err != nil {
				return platformjobs.Result{}, err
			}
			res, err := s.Escalate(ctx, d.Clock.Now(), unseen, contacts)
			sum := map[string]int64{
				"unseen": res.Unseen, "escalated": res.Escalated, "already": res.Already,
				"no_contact": res.NoContact, "failed": res.Failed, "no_outside": res.NoOutside, "truncated": res.TruncatedP,
			}
			return platformjobs.Result{Summary: sum}, err
		},
	})
}

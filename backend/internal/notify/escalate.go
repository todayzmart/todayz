package notify

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/notifydb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// KindOrderUnseen is the scheduled job of the "new order not seen" SMS.
const KindOrderUnseen = "notify_order_unseen"

// Escalation paging: one tick looks at most escalatePage × escalatePages
// orders, oldest first; the rest wait for the next minute.
const (
	escalatePage  = 100
	escalatePages = 20
)

// EscalateResult counts one escalation run.
type EscalateResult struct {
	Unseen     int64 // orders the source returned
	Escalated  int64 // notifications written (an SMS queued when a channel was usable)
	Already    int64 // orders escalated by an earlier run
	NoContact  int64 // stores the contact source did not know
	Failed     int64 // orders whose transaction failed (retried next tick)
	NoOutside  int64 // escalated without an outside message (no phone or no channel)
	TruncatedP int64 // 1 when the page budget ran out
}

// Escalate sends the "new order not seen" message (01 §15) for every order
// still unseen order_unseen_sms_after_min minutes after it was placed: an
// inbox row for the business and one outside message to the store contact
// phone (the owner's login phone when the store has none), WhatsApp when it
// is enabled and configured, SMS otherwise, in the business language with an
// English fallback. Once per order: the escalation row is the claim, written
// in the same transaction as the notification and its job.
func (s *Service) Escalate(ctx context.Context, now time.Time, unseen notice.UnseenOrders, contacts notice.StoreContacts) (EscalateResult, error) {
	var res EscalateResult
	if s.d.DB == nil || unseen == nil || contacts == nil {
		return res, errors.New("notify: escalation needs the database, the unseen-orders source and the contacts source")
	}
	if s.d.Settings == nil {
		return res, errors.New("notify: escalation needs settings")
	}
	mins, err := s.d.Settings.Int(ctx, settings.OrderUnseenSmsAfterMin)
	if err != nil {
		return res, fmt.Errorf("notify: %s: %w", settings.OrderUnseenSmsAfterMin, err)
	}
	if mins < 0 {
		mins = 0
	}
	q := notifydb.New(s.d.DB.Pool)
	query := notice.UnseenQuery{CreatedBefore: now.Add(-time.Duration(mins) * time.Minute), Limit: escalatePage}
	var errs []error
	for page := 0; page < escalatePages; page++ {
		orders, err := unseen.Unseen(ctx, query)
		if err != nil {
			return res, errors.Join(append(errs, fmt.Errorf("notify: unseen orders: %w", err))...)
		}
		if len(orders) == 0 {
			break
		}
		res.Unseen += int64(len(orders))
		done, err := escalatedSet(ctx, q, orders)
		if err != nil {
			return res, errors.Join(append(errs, err)...)
		}
		for _, o := range orders {
			if done[o.OrderID] {
				res.Already++
				continue
			}
			if err := s.escalateOne(ctx, o, mins, contacts, &res); err != nil {
				res.Failed++
				errs = append(errs, err)
				telemetry.LoggerOr(ctx, s.d.Log).ErrorContext(ctx, "notify: escalation failed",
					"order_id", o.OrderID.String(), "err", err)
			}
		}
		if len(orders) < escalatePage {
			break
		}
		last := orders[len(orders)-1]
		query.After = &last
		if page == escalatePages-1 {
			res.TruncatedP = 1
		}
	}
	return res, errors.Join(errs...)
}

func escalatedSet(ctx context.Context, q *notifydb.Queries, orders []notice.UnseenOrder) (map[ids.ID]bool, error) {
	list := make([]pgtype.UUID, len(orders))
	for i, o := range orders {
		list[i] = ids.UUID(o.OrderID)
	}
	got, err := q.EscalatedOrders(ctx, list)
	if err != nil {
		return nil, fmt.Errorf("notify: escalated orders: %w", err)
	}
	out := make(map[ids.ID]bool, len(got))
	for _, u := range got {
		if id, ok := ids.FromUUID(u); ok {
			out[id] = true
		}
	}
	return out, nil
}

// errAlready rolls back an escalation another run claimed first.
var errAlready = errors.New("notify: already escalated")

func (s *Service) escalateOne(ctx context.Context, o notice.UnseenOrder, mins int64, contacts notice.StoreContacts, res *EscalateResult) error {
	c, ok, err := contacts.Contact(ctx, o.StoreID)
	if err != nil {
		return fmt.Errorf("notify: store contact: %w", err)
	}
	if !ok || c.BusinessID == (ids.ID{}) {
		res.NoContact++
		return nil
	}
	n := notice.Notification{
		Kind: notice.KindOrderUnseen, BusinessID: c.BusinessID, StoreID: o.StoreID,
		Params:   map[string]string{"store": c.StoreName, "minutes": strconv.FormatInt(mins, 10)},
		Ref:      &notice.Ref{Type: "order", ID: o.OrderID},
		PrefUser: c.OwnerUserID,
	}
	if len([]rune(c.StoreName)) > maxParamRunes {
		n.Params["store"] = string([]rune(c.StoreName)[:maxParamRunes])
	}
	if c.Phone != "" {
		ch, err := s.escalationChannel(ctx)
		if err != nil {
			return err
		}
		if ch != "" {
			n.Outside = []notice.Outside{{Channel: ch, Phone: c.Phone, Lang: c.Lang}}
		}
	}
	err = s.d.DB.InTx(ctx, func(ctx context.Context) error {
		tx, _ := db.TxFrom(ctx)
		id, err := s.Send(ctx, tx, n)
		if err != nil {
			return err
		}
		_, err = notifydb.New(tx).ClaimEscalation(ctx, notifydb.ClaimEscalationParams{
			OrderID: ids.UUID(o.OrderID), StoreID: ids.UUID(o.StoreID), NotificationID: ids.UUID(id), Now: s.d.Clock.Now(),
		})
		if db.IsNoRows(err) {
			return errAlready
		}
		return err
	})
	switch {
	case errors.Is(err, errAlready):
		res.Already++
		return nil
	case err != nil:
		return err
	}
	res.Escalated++
	if len(n.Outside) == 0 {
		res.NoOutside++
	}
	return nil
}

// escalationChannel is WhatsApp when it is on and configured, else SMS when
// it is, else none (the inbox row is still written). The choice is made when
// the message is queued; a WhatsApp send that then fails is not retried on
// SMS (at most one outside message per order).
func (s *Service) escalationChannel(ctx context.Context) (notice.Channel, error) {
	for _, ch := range []notice.Channel{notice.WhatsApp, notice.SMS} {
		ok, err := s.usable(ctx, ch)
		if err != nil {
			return "", err
		}
		if ok {
			return ch, nil
		}
	}
	return "", nil
}

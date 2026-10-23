package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/todayz/backend/gen/sqlc/notifydb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
)

// Delivery statuses (notification_deliveries.status).
const (
	StatusQueued  = "queued"
	StatusSending = "sending"
	StatusSent    = "sent"
	StatusFailed  = "failed"
)

// maxErrorLen bounds the stored error (ck_notification_deliveries_error).
const maxErrorLen = 200

// DeliverResult says what Deliver did.
type DeliverResult struct {
	// Claimed is false when the delivery was not queued any more (a retry of
	// a job whose first attempt already claimed it): nothing was sent.
	Claimed bool
	Status  string
}

// Deliver sends one queued delivery. The row moves queued → sending in its
// own statement before the provider is called, so a retried job (after a
// crash or a cancel) finds it past queued and never calls the provider a
// second time: an outside message is sent at most once. A provider failure
// is final (status failed) for the same reason; it is counted, not retried.
func (s *Service) Deliver(ctx context.Context, deliveryID ids.ID) (DeliverResult, error) {
	if s.d.DB == nil {
		return DeliverResult{}, errors.New("notify: no database")
	}
	q := notifydb.New(s.d.DB.Pool)
	row, err := q.ClaimDelivery(ctx, notifydb.ClaimDeliveryParams{Now: s.d.Clock.Now(), ID: ids.UUID(deliveryID)})
	if err != nil {
		if db.IsNoRows(err) {
			return DeliverResult{}, nil
		}
		return DeliverResult{}, fmt.Errorf("notify: claim delivery: %w", err)
	}
	ch := notice.Channel(row.Channel)
	// The claim is made; whatever happens next, the row must end, even if
	// the job's context was cancelled.
	finishCtx := context.WithoutCancel(ctx)
	fail := func(provider, reason string) (DeliverResult, error) {
		countDelivery(ctx, ch, provider, row.Kind, OutcomeFailed)
		if err := s.finish(finishCtx, deliveryID, StatusFailed, "", reason, 0); err != nil {
			return DeliverResult{Claimed: true}, err
		}
		return DeliverResult{Claimed: true, Status: StatusFailed}, nil
	}

	def, ok := catalogue[row.Kind]
	if !ok {
		return fail(row.Provider, "kind is not in the catalogue")
	}
	prov := s.providers[ch]
	if prov == nil {
		return fail(row.Provider, "provider not configured")
	}
	if s.sealer == nil || len(row.ToEnc) == 0 {
		return fail(prov.Name(), "no recipient")
	}
	plain, err := s.sealer.Open(sealField, row.ToEnc)
	if err != nil {
		return fail(prov.Name(), "recipient could not be opened")
	}
	var params map[string]string
	if err := json.Unmarshal(row.Params, &params); err != nil {
		return fail(prov.Name(), "params could not be read")
	}
	tpl, lang := templateFor(def, pickLang(row.Lang))
	t, _ := textFor(row.Kind, lang, params)
	msg := Message{To: string(plain), Template: tpl, Lang: lang, Text: t.Body}
	for _, name := range def.Params {
		msg.Vars = append(msg.Vars, Var{Name: name, Value: params[name]})
	}

	start := s.d.Clock.Now()
	res, err := prov.Send(ctx, msg)
	observeLatency(ctx, ch, prov.Name(), s.d.Clock.Now().Sub(start))
	if err != nil {
		// The error carries the provider's status and type, never the request.
		return fail(prov.Name(), err.Error())
	}
	units := max(res.Units, 1)
	cost := int64(units) * s.d.CostPerUnitPaise[ch]
	countDelivery(ctx, ch, prov.Name(), row.Kind, OutcomeSent)
	countCost(ctx, ch, prov.Name(), units, cost)
	if err := s.finish(finishCtx, deliveryID, StatusSent, res.ProviderID, "", cost); err != nil {
		return DeliverResult{Claimed: true}, err
	}
	return DeliverResult{Claimed: true, Status: StatusSent}, nil
}

func (s *Service) finish(ctx context.Context, id ids.ID, status, providerID, reason string, cost int64) error {
	p := notifydb.FinishDeliveryParams{Status: status, CostPaise: cost, Now: s.d.Clock.Now(), ID: ids.UUID(id)}
	if providerID != "" {
		v := clip(providerID, 128)
		p.ProviderID = &v
	}
	if reason != "" {
		v := sanitizeError(reason)
		p.Error = &v
	}
	if err := notifydb.New(s.d.DB.Pool).FinishDelivery(ctx, p); err != nil {
		return fmt.Errorf("notify: finish delivery: %w", err)
	}
	return nil
}

// sanitizeError keeps a stored error short, printable and free of digit
// runs that could be a phone (a provider that echoed one back).
func sanitizeError(s string) string {
	var b strings.Builder
	digits := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
			if digits > 4 {
				continue
			}
		case r < 0x20 || r == 0x7f:
			digits = 0
			r = ' '
		default:
			digits = 0
		}
		b.WriteRune(r)
		if b.Len() >= maxErrorLen-4 {
			break
		}
	}
	out := b.String()
	for utf8.RuneCountInString(out) > maxErrorLen {
		_, n := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-n]
	}
	return out
}

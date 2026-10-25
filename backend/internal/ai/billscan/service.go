package billscan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/gen/sqlc/airetaildb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// inputMeta is the draft's input_meta: small facts, never the photo.
type inputMeta struct {
	Bytes     int    `json:"bytes"`
	MediaType string `json:"mediaType"`
}

// Create stores a processing draft for one bill photo and reads it in the background.
func (s *Service) Create(ctx context.Context, sc authz.Scope, actor ids.ID, img client.Image) (ids.ID, error) {
	return s.d.Kit.Start(ctx, retail.New{
		Feature: Feature, Store: sc.StoreID, Actor: actor, PromptVersion: prompts.Version(Feature), Scan: true,
		Meta: inputMeta{Bytes: len(img.Data), MediaType: img.MediaType},
	}, func(ctx context.Context, id ids.ID) { s.process(ctx, id, sc.StoreID, img) })
}

func (s *Service) process(ctx context.Context, id, storeID ids.ID, img client.Image) {
	log := telemetry.Logger(ctx)
	model, err := s.d.Settings.String(ctx, settings.AiModelSmart)
	if err != nil {
		log.ErrorContext(ctx, "billscan: model setting", slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, client.Response{})
		return
	}
	r, err := ReadBill(ctx, s.d.LLM, model, id.String(), img)
	s.d.Kit.Settle(ctx, Feature, r.Resp)
	switch {
	case err != nil:
		log.ErrorContext(ctx, "billscan: model call", slog.String("draft_id", id.String()), slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, r.Resp)
		return
	case r.Reject != "":
		s.d.Kit.Fail(ctx, Feature, id, r.Reject, r.Resp)
		return
	case len(r.Output.Lines) == 0:
		s.d.Kit.Fail(ctx, Feature, id, retail.FailEmpty, r.Resp)
		return
	}
	out := r.Output
	if err := s.match(ctx, storeID, out.Lines); err != nil {
		log.ErrorContext(ctx, "billscan: match", slog.String("draft_id", id.String()), slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, r.Resp)
		return
	}
	s.d.Kit.Ready(ctx, Feature, id, out, r.Resp)
}

// match names the closest store item of every line, when one is close enough.
func (s *Service) match(ctx context.Context, storeID ids.ID, lines []Line) error {
	names := make([]string, len(lines))
	for i, l := range lines {
		names[i] = l.Name
	}
	rows, err := airetaildb.New(s.d.DB.Q(ctx)).MatchStoreItems(ctx, airetaildb.MatchStoreItemsParams{
		Names: names, StoreID: ids.UUID(storeID), MinScore: matchMinScore,
	})
	if err != nil {
		return err
	}
	for _, r := range rows {
		i := int(r.Ord) - 1
		id, ok := ids.FromUUID(r.ID)
		if i < 0 || i >= len(lines) || !ok {
			continue
		}
		sid, name := id.String(), retail.ItemName(r.Name, r.Variant, r.PackSize, r.Unit)
		lines[i].StoreItemID, lines[i].ItemName = &sid, &name
	}
	return nil
}

// Get answers one draft of the caller's store, with each matched item's stock now.
func (s *Service) Get(ctx context.Context, sc authz.Scope, id ids.ID) (aiapi.SupplierBillDraft, error) {
	d, err := s.d.Kit.Load(ctx, Feature, sc.StoreID, id)
	if err != nil {
		return aiapi.SupplierBillDraft{}, err
	}
	v := aiapi.SupplierBillDraft{
		DraftId: d.ID.String(), Feature: aiapi.SupplierBillDraftFeature(Feature), Status: aiapi.AiDraftStatus(d.Status),
		PromptVersion: d.PromptVersion, CreatedAt: d.CreatedAt.UTC(), Failure: retail.Failure(d),
		Lines: []aiapi.SupplierBillLine{}, Outcomes: retail.Outcomes(d.Lines),
	}
	if len(d.Output) == 0 {
		return v, nil
	}
	var out Output
	if err := d.Decode(&out); err != nil {
		return aiapi.SupplierBillDraft{}, err
	}
	v.Supplier, v.BillDate = out.Supplier, apiDate(out.BillDate)
	items, err := s.items(ctx, sc.StoreID, out.Lines)
	if err != nil {
		return aiapi.SupplierBillDraft{}, err
	}
	for _, l := range out.Lines {
		al := aiapi.SupplierBillLine{
			LineId: l.LineID, Name: l.Name, Quantity: l.Quantity, CostPaise: intp(l.CostPaise), MrpPaise: intp(l.MrpPaise),
			ExpiresOn: apiDate(l.ExpiresOn), Confidence: float32(l.Confidence),
		}
		if l.StoreItemID != nil {
			if it, ok := items[*l.StoreItemID]; ok {
				al.Match = &aiapi.SupplierBillMatch{StoreItemId: *l.StoreItemID, ItemName: it.name, OnHand: int(it.onHand)}
			}
		}
		v.Lines = append(v.Lines, al)
	}
	return v, nil
}

type item struct {
	name     string
	onHand   int32
	mrpPaise *int64
}

// items reads the store's items the lines name (an item since removed is missing).
func (s *Service) items(ctx context.Context, storeID ids.ID, lines []Line) (map[string]item, error) {
	var want []pgtype.UUID
	for _, l := range lines {
		if l.StoreItemID != nil {
			if id, err := ids.Parse(*l.StoreItemID); err == nil {
				want = append(want, ids.UUID(id))
			}
		}
	}
	return s.itemsByUUID(ctx, storeID, want)
}

func (s *Service) itemsByUUID(ctx context.Context, storeID ids.ID, want []pgtype.UUID) (map[string]item, error) {
	out := map[string]item{}
	if len(want) == 0 {
		return out, nil
	}
	rows, err := airetaildb.New(s.d.DB.Q(ctx)).StoreItemsByIDs(ctx, airetaildb.StoreItemsByIDsParams{StoreID: ids.UUID(storeID), Ids: want})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if id, ok := ids.FromUUID(r.ID); ok {
			out[id.String()] = item{name: retail.ItemName(r.Name, r.Variant, r.PackSize, r.Unit), onHand: r.OnHand, mrpPaise: r.MrpPaise}
		}
	}
	return out, nil
}

// Confirm applies the person's decisions: one supplier_bill stock in per accepted or
// edited line, in the draft's transaction. A line that does not fit (no such item, a past
// expiry, a cost above the MRP …) is refused alone, not_permitted with its reason, and the
// draft stays ready so the person can edit it and confirm it again; the other lines are
// applied. A repeat answers the first result.
func (s *Service) Confirm(ctx context.Context, sc authz.Scope, p authn.Principal, draftID ids.ID, in []aiapi.SupplierBillDecision) (drafts.Result, error) {
	if err := s.d.Kit.Guard.Require(ctx, Feature); err != nil {
		return drafts.Result{}, err
	}
	lineIDs := make([]string, 0, len(in))
	ds := make([]drafts.Decision, 0, len(in))
	for _, dc := range in {
		var v any
		if dc.Value != nil {
			v = dc.Value
		}
		d, err := retail.Decision(dc.LineId, dc.Outcome, v)
		if err != nil {
			return drafts.Result{}, err
		}
		lineIDs = append(lineIDs, dc.LineId)
		ds = append(ds, d)
	}
	index := retail.Index(lineIDs)
	return s.d.Kit.Drafts.Confirm(ctx, drafts.Confirmation{
		Feature: Feature, DraftID: draftID, By: p.UserID, Decisions: ds, Check: retail.OwnStore(sc.StoreID), KeepOpen: true,
		Apply: func(ctx context.Context, d drafts.Draft, ds []drafts.Decision) ([]drafts.LineResult, error) {
			return s.apply(ctx, sc, p, d, ds, index)
		},
	})
}

// change is one decided line as it will be written.
type change struct {
	lineID  string
	outcome drafts.Outcome
	item    ids.ID
	qty     int
	cost    *int64
	expires *time.Time
}

func (s *Service) apply(ctx context.Context, sc authz.Scope, p authn.Principal, d drafts.Draft, ds []drafts.Decision, index map[string]int) ([]drafts.LineResult, error) {
	var out Output
	if err := d.Decode(&out); err != nil {
		return nil, err
	}
	byID := make(map[string]Line, len(out.Lines))
	for _, l := range out.Lines {
		byID[l.LineID] = l
	}
	today := clock.StartOfDayIST(s.d.Clock.Now()).In(clock.IST)
	results := make([]drafts.LineResult, 0, len(ds))
	todo := make([]change, 0, len(ds))
	var want []pgtype.UUID
	for _, dc := range ds {
		c, err := decide(dc, byID[dc.LineID], fmt.Sprintf("body.lines[%d]", index[dc.LineID]), today)
		if r, ok := retail.Refused(dc.LineID, err); ok {
			results = append(results, r)
			continue
		}
		if err != nil {
			return nil, err
		}
		todo = append(todo, c)
		want = append(want, ids.UUID(c.item))
	}
	items, err := s.itemsByUUID(ctx, sc.StoreID, want)
	if err != nil {
		return nil, err
	}
	for _, c := range todo {
		err := s.write(ctx, sc, p, d, c, items)
		if r, ok := retail.Refused(c.lineID, err); ok {
			results = append(results, r)
			continue
		}
		if err != nil {
			return nil, err
		}
		results = append(results, drafts.LineResult{LineID: c.lineID, Outcome: c.outcome})
	}
	return results, nil
}

// write is one line's stock in, in a savepoint of the confirm's transaction, so a line the
// catalogue refuses rolls back alone.
func (s *Service) write(ctx context.Context, sc authz.Scope, p authn.Principal, d drafts.Draft, c change, items map[string]item) error {
	it, ok := items[c.item.String()]
	if !ok {
		return retail.RefuseItem
	}
	if c.cost != nil && it.mrpPaise != nil && *c.cost > *it.mrpPaise {
		return retail.RefuseCostAboveMRP
	}
	return s.d.DB.InTx(ctx, func(ctx context.Context) error {
		err := s.d.Stock.AddFromBill(ctx, StockIn{
			StoreID: sc.StoreID, ItemID: c.item, DraftID: d.ID, Who: p.UserID,
			Qty: c.qty, CostPaise: c.cost, ExpiresOn: c.expires,
		})
		switch {
		case errors.Is(err, ErrItemGone):
			return retail.RefuseItem
		case errors.Is(err, ErrQuantity):
			return retail.RefuseQuantity
		}
		return err
	})
}

// decide is one decision as a change: an accepted line as the model read it (it must have a
// matched item), an edited line as the person wrote it. A value that does not fit is a
// retail.Refusal (the line alone is refused); only a value that is not the line's shape is a
// 422 for the request.
func decide(dc drafts.Decision, l Line, path string, today time.Time) (change, error) {
	c := change{lineID: dc.LineID, outcome: dc.Outcome}
	switch dc.Outcome {
	case drafts.Accepted:
		if l.StoreItemID == nil {
			return change{}, retail.RefuseItem
		}
		id, err := ids.Parse(*l.StoreItemID)
		if err != nil {
			return change{}, retail.RefuseItem
		}
		c.item, c.qty, c.cost = id, l.Quantity, l.CostPaise
		if l.ExpiresOn != nil {
			t, err := time.Parse(dateLayout, *l.ExpiresOn)
			if err != nil {
				return change{}, retail.RefuseExpiry
			}
			c.expires = &t
		}
	case drafts.Edited:
		var v aiapi.SupplierBillLineValue
		if err := json.Unmarshal(dc.Value, &v); err != nil {
			return change{}, retail.FieldErr(path+".value", "invalid")
		}
		id, err := ids.Parse(v.StoreItemId)
		if err != nil {
			return change{}, retail.RefuseItem
		}
		c.item, c.qty = id, v.Quantity
		if v.CostPaise != nil {
			cp := int64(*v.CostPaise)
			c.cost = &cp
		}
		if v.ExpiresOn != nil {
			t := time.Date(v.ExpiresOn.Year(), v.ExpiresOn.Month(), v.ExpiresOn.Day(), 0, 0, 0, 0, time.UTC)
			c.expires = &t
		}
	default:
		return change{}, fmt.Errorf("billscan: outcome %q", dc.Outcome)
	}
	if c.qty < 1 || c.qty > MaxQuantity {
		return change{}, retail.RefuseQuantity
	}
	if c.cost != nil && (*c.cost < 1 || *c.cost > MaxPaise) {
		return change{}, retail.RefuseCost
	}
	if c.expires != nil && c.expires.Before(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
		return change{}, retail.RefuseExpiry
	}
	return c, nil
}

func apiDate(s *string) *aiapi.AiDate {
	if s == nil {
		return nil
	}
	t, err := time.Parse(dateLayout, *s)
	if err != nil {
		return nil
	}
	return &openapi_types.Date{Time: t}
}

func intp(p *int64) *int {
	if p == nil {
		return nil
	}
	n := int(*p)
	return &n
}

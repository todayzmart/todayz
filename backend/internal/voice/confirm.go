package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	voiceapi "github.com/todayz/backend/gen/api/voice"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/voicedb"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// KindAction is the permission each line kind needs at confirm (D-06, D-65).
var KindAction = map[string]string{
	KindSale:     policy.PosSell,
	KindCount:    policy.StockCount,
	KindRestock:  policy.StockRestock,
	KindSetPrice: policy.ItemPriceEdit,
}

func fieldErr(path, reason string) *problem.Error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

func notFound() *problem.Error { return problem.New(genproblems.NotFound, "") }

// permits answers, per line kind, whether p may confirm it in sc (one check per kind).
func (s *Service) permits(ctx context.Context, p authn.Principal, sc authz.Scope) func(kind string) (bool, error) {
	memo := map[string]bool{}
	return func(kind string) (bool, error) {
		if ok, seen := memo[kind]; seen {
			return ok, nil
		}
		action, known := KindAction[kind]
		if !known {
			return false, fmt.Errorf("voice: no action for kind %q", kind)
		}
		_, err := s.d.Authz.Check(ctx, p, action, sc)
		switch {
		case err == nil:
			memo[kind] = true
		case problem.CodeOf(err) == genproblems.Forbidden:
			memo[kind] = false
		default:
			return false, err
		}
		return memo[kind], nil
	}
}

// owns is the draft ownership rule: a draft of the caller's store, spoken by the caller or
// read by the store's owner or a manager (voice.draft.others). Anything else is the common 404.
func (s *Service) owns(ctx context.Context, p authn.Principal, sc authz.Scope, d drafts.Draft) error {
	if d.Store != sc.StoreID {
		return notFound()
	}
	if d.Actor == p.UserID {
		return nil
	}
	_, err := s.d.Authz.Check(ctx, p, policy.VoiceDraftOthers, sc)
	if problem.CodeOf(err) == genproblems.Forbidden {
		return notFound()
	}
	return err
}

// change is one decided line as it will be applied.
type change struct {
	lineID  string
	path    string // the request path of the decision, for 422 fields
	outcome drafts.Outcome
	kind    string
	item    ids.ID
	qty     int
	price   int64
}

// Confirm applies the person's decisions (B-06): every line is checked against the
// confirmer's permission, accepted and edited lines are applied through their domains in
// the draft's transaction (restocks, counts and prices first, then one sale), and the
// outcomes are stored on the draft. A repeat answers the first result.
func (s *Service) Confirm(ctx context.Context, sc authz.Scope, p authn.Principal, draftID ids.ID, in []voiceapi.VoiceStockDecision) (drafts.Result, error) {
	if err := s.d.Guard.Require(ctx, Feature); err != nil {
		return drafts.Result{}, err
	}
	index := make(map[string]int, len(in))
	ds := make([]drafts.Decision, 0, len(in))
	for i, dc := range in {
		if _, dup := index[dc.LineId]; !dup {
			index[dc.LineId] = i
		}
		d := drafts.Decision{LineID: dc.LineId, Outcome: drafts.Outcome(dc.Outcome)}
		if dc.Value != nil {
			b, err := json.Marshal(dc.Value)
			if err != nil {
				return drafts.Result{}, err
			}
			d.Value = b
		}
		ds = append(ds, d)
	}
	return s.d.Drafts.Confirm(ctx, drafts.Confirmation{
		Feature: Feature, DraftID: draftID, By: p.UserID, Decisions: ds,
		Check: func(d drafts.Draft) error { return s.owns(ctx, p, sc, d) },
		Apply: func(ctx context.Context, d drafts.Draft, ds []drafts.Decision) ([]drafts.LineResult, error) {
			return s.apply(ctx, sc, p, d, ds, index)
		},
	})
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
	may := s.permits(ctx, p, sc)
	results := make([]drafts.LineResult, 0, len(ds))
	var todo []change
	for _, dc := range ds {
		c, err := decide(dc, byID[dc.LineID], fmt.Sprintf("body.lines[%d]", index[dc.LineID]))
		if err != nil {
			return nil, err
		}
		ok, err := may(c.kind)
		if err != nil {
			return nil, err
		}
		if !ok {
			results = append(results, drafts.LineResult{LineID: dc.LineID, Outcome: drafts.NotPermitted})
			continue
		}
		todo = append(todo, c)
		results = append(results, drafts.LineResult{LineID: dc.LineID, Outcome: dc.Outcome})
	}
	if len(todo) == 0 {
		return results, nil
	}

	// Every item must still be in the store.
	have, err := s.items(ctx, sc.StoreID, todo)
	if err != nil {
		return nil, err
	}
	for _, c := range todo {
		if _, ok := have[c.item]; !ok {
			return nil, fieldErr(c.itemPath(), "not_found")
		}
	}

	// Restocks, counts and prices first, in the order spoken.
	for _, c := range todo {
		var err error
		switch c.kind {
		case KindRestock:
			err = s.d.Stock.Restock(ctx, sc.StoreID, c.item, c.qty, d.ID, p.UserID)
		case KindCount:
			err = s.d.Stock.Count(ctx, sc.StoreID, c.item, c.qty, p.UserID)
		case KindSetPrice:
			err = s.d.Stock.SetPrice(ctx, sc.StoreID, c.item, c.price)
		default:
			continue
		}
		switch {
		case errors.Is(err, ErrAboveMRP):
			return nil, fieldErr(c.valuePath("pricePaise"), "above_mrp")
		case errors.Is(err, ErrItemGone):
			return nil, fieldErr(c.itemPath(), "not_found")
		case errors.Is(err, ErrQuantity):
			return nil, fieldErr(c.valuePath("quantity"), "range")
		case err != nil:
			return nil, err
		}
	}

	// Then one sale with every sale line, at the prices now (after this draft's own price
	// changes).
	var sale []change
	for _, c := range todo {
		if c.kind == KindSale {
			sale = append(sale, c)
		}
	}
	if len(sale) > 0 {
		now, err := s.items(ctx, sc.StoreID, sale)
		if err != nil {
			return nil, err
		}
		lines := make([]SaleLine, 0, len(sale))
		for _, c := range sale {
			lines = append(lines, SaleLine{ItemID: c.item, Quantity: c.qty, UnitPricePaise: now[c.item].PricePaise})
		}
		if err := s.d.Seller.Sell(ctx, sc, d.ID, lines); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// decide turns one decision into the change it asks for, or a 422.
func decide(dc drafts.Decision, line Line, path string) (change, error) {
	c := change{lineID: dc.LineID, path: path, outcome: dc.Outcome}
	switch dc.Outcome {
	case drafts.Accepted:
		if !line.Matched() {
			return c, fieldErr(path+".outcome", "pick_item")
		}
		id, err := ids.Parse(*line.StoreItemID)
		if err != nil {
			return c, err
		}
		c.kind, c.item = line.Kind, id
		if line.Quantity != nil {
			c.qty = *line.Quantity
		}
		if line.PricePaise != nil {
			c.price = *line.PricePaise
		}
		return c, checkChange(c, line.Quantity, line.PricePaise)
	case drafts.Edited:
		var v voiceapi.VoiceStockLineValue
		if err := json.Unmarshal(dc.Value, &v); err != nil {
			return c, fieldErr(path+".value", "invalid")
		}
		id, err := ids.Parse(v.StoreItemId)
		if err != nil {
			return c, fieldErr(path+".value.storeItemId", "ulid")
		}
		c.kind, c.item = string(v.Kind), id
		var price *int64
		if v.PricePaise != nil {
			p := int64(*v.PricePaise)
			price, c.price = &p, p
		}
		if v.Quantity != nil {
			c.qty = *v.Quantity
		}
		return c, checkChange(c, v.Quantity, price)
	}
	return c, fmt.Errorf("voice: outcome %q reached apply", dc.Outcome)
}

// checkChange is the per-kind range rule (VoiceStockLineValue).
func checkChange(c change, qty *int, price *int64) error {
	switch c.kind {
	case KindSale, KindRestock, KindCount:
		lo := 1
		if c.kind == KindCount {
			lo = 0
		}
		if qty == nil {
			return fieldErr(c.valuePath("quantity"), "required")
		}
		if *qty < lo || *qty > MaxQuantity {
			return fieldErr(c.valuePath("quantity"), "range")
		}
		if price != nil {
			return fieldErr(c.valuePath("pricePaise"), "set_price_only")
		}
	case KindSetPrice:
		if price == nil {
			return fieldErr(c.valuePath("pricePaise"), "required")
		}
		if *price < 1 || *price > MaxPricePaise {
			return fieldErr(c.valuePath("pricePaise"), "range")
		}
		if qty != nil {
			return fieldErr(c.valuePath("quantity"), "not_for_set_price")
		}
	default:
		return fieldErr(c.valuePath("kind"), "enum")
	}
	return nil
}

func (c change) valuePath(field string) string {
	if c.outcome == drafts.Edited {
		return c.path + ".value." + field
	}
	return c.path + ".outcome"
}

func (c change) itemPath() string {
	if c.outcome == drafts.Edited {
		return c.path + ".value.storeItemId"
	}
	return c.path + ".outcome"
}

// items reads the changes' items in the store now (inside the confirm's transaction).
func (s *Service) items(ctx context.Context, storeID ids.ID, cs []change) (map[ids.ID]voicedb.ItemsByIDsRow, error) {
	want := make([]pgtype.UUID, 0, len(cs))
	for _, c := range cs {
		want = append(want, ids.UUID(c.item))
	}
	return s.itemsByIDs(ctx, storeID, want)
}

func (s *Service) itemsByIDs(ctx context.Context, storeID ids.ID, want []pgtype.UUID) (map[ids.ID]voicedb.ItemsByIDsRow, error) {
	out := map[ids.ID]voicedb.ItemsByIDsRow{}
	if len(want) == 0 {
		return out, nil
	}
	rows, err := voicedb.New(s.d.DB.Q(ctx)).ItemsByIDs(ctx, voicedb.ItemsByIDsParams{StoreID: ids.UUID(storeID), Ids: want})
	if err != nil {
		return nil, fmt.Errorf("voice: items: %w", err)
	}
	for _, r := range rows {
		id, ok := ids.FromUUID(r.ID)
		if !ok {
			return nil, errNullID
		}
		out[id] = r
	}
	return out, nil
}

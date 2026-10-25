package shelfscan

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/gen/sqlc/airetaildb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

type inputMeta struct {
	Bytes     int    `json:"bytes"`
	MediaType string `json:"mediaType"`
}

// Create stores a processing draft for one shelf photo and reads it in the background.
func (s *Service) Create(ctx context.Context, sc authz.Scope, actor ids.ID, img client.Image) (ids.ID, error) {
	return s.d.Kit.Start(ctx, retail.New{
		Feature: Feature, Store: sc.StoreID, Actor: actor, PromptVersion: prompts.Version(Feature),
		Meta: inputMeta{Bytes: len(img.Data), MediaType: img.MediaType},
	}, func(ctx context.Context, id ids.ID) { s.process(ctx, id, img) })
}

func (s *Service) process(ctx context.Context, id ids.ID, img client.Image) {
	log := telemetry.Logger(ctx)
	model, err := s.d.Settings.String(ctx, settings.AiModelSmart)
	if err != nil {
		log.ErrorContext(ctx, "shelfscan: model setting", slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, client.Response{})
		return
	}
	r, err := ReadShelf(ctx, s.d.LLM, model, id.String(), img)
	s.d.Kit.Settle(ctx, Feature, r.Resp)
	switch {
	case err != nil:
		log.ErrorContext(ctx, "shelfscan: model call", slog.String("draft_id", id.String()), slog.Any("err", err))
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
	if err := s.match(ctx, out.Lines); err != nil {
		log.ErrorContext(ctx, "shelfscan: match", slog.String("draft_id", id.String()), slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, r.Resp)
		return
	}
	s.d.Kit.Ready(ctx, Feature, id, out, r.Resp)
}

// match names the closest active master product of every line, when one is close enough.
func (s *Service) match(ctx context.Context, lines []Line) error {
	names := make([]string, len(lines))
	for i, l := range lines {
		names[i] = MatchName(l)
	}
	rows, err := airetaildb.New(s.d.DB.Q(ctx)).MatchProducts(ctx, airetaildb.MatchProductsParams{
		Names: names, StoreID: pgtype.UUID{}, MinScore: matchMinScore,
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
		pid := id.String()
		lines[i].ProductID = &pid
	}
	return nil
}

type product struct {
	view   aiapi.ShelfPhotoProduct
	mrp    *int64
	active bool
	listed bool
}

// products reads the master products the ids name, with whether the store sells each now.
func (s *Service) products(ctx context.Context, storeID ids.ID, want []string) (map[string]product, error) {
	out := map[string]product{}
	var us []pgtype.UUID
	for _, w := range want {
		if id, err := ids.Parse(w); err == nil {
			us = append(us, ids.UUID(id))
		}
	}
	if len(us) == 0 {
		return out, nil
	}
	rows, err := airetaildb.New(s.d.DB.Q(ctx)).ProductsByIDs(ctx, airetaildb.ProductsByIDsParams{StoreID: ids.UUID(storeID), Ids: us})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		id, ok := ids.FromUUID(r.ID)
		if !ok {
			continue
		}
		size := retail.ItemName("", nil, r.PackSize, r.Unit)
		v := aiapi.ShelfPhotoProduct{ProductId: id.String(), Name: retail.ItemName(r.Name, r.Variant, "", ""), Brand: r.BrandName, MrpPaise: intp(r.MrpPaise)}
		if size != "" {
			v.Size = &size
		}
		out[id.String()] = product{view: v, mrp: r.MrpPaise, active: r.Status == "active", listed: r.Listed}
	}
	return out, nil
}

// Get answers one draft of the caller's store, each matched product as the catalogue has it
// now.
func (s *Service) Get(ctx context.Context, sc authz.Scope, id ids.ID) (aiapi.ShelfPhotoDraft, error) {
	d, err := s.d.Kit.Load(ctx, Feature, sc.StoreID, id)
	if err != nil {
		return aiapi.ShelfPhotoDraft{}, err
	}
	v := aiapi.ShelfPhotoDraft{
		DraftId: d.ID.String(), Feature: aiapi.ShelfPhotoDraftFeature(Feature), Status: aiapi.AiDraftStatus(d.Status),
		PromptVersion: d.PromptVersion, CreatedAt: d.CreatedAt.UTC(), Failure: retail.Failure(d),
		Lines: []aiapi.ShelfPhotoLine{}, Outcomes: retail.Outcomes(d.Lines),
	}
	if len(d.Output) == 0 {
		return v, nil
	}
	var out Output
	if err := d.Decode(&out); err != nil {
		return aiapi.ShelfPhotoDraft{}, err
	}
	var want []string
	for _, l := range out.Lines {
		if l.ProductID != nil {
			want = append(want, *l.ProductID)
		}
	}
	ps, err := s.products(ctx, sc.StoreID, want)
	if err != nil {
		return aiapi.ShelfPhotoDraft{}, err
	}
	for _, l := range out.Lines {
		al := aiapi.ShelfPhotoLine{LineId: l.LineID, Name: l.Name, Brand: l.Brand, Size: l.Size, Confidence: float32(l.Confidence)}
		if l.ProductID != nil {
			if p, ok := ps[*l.ProductID]; ok && p.active {
				pv := p.view
				al.Product, al.Listed, al.PricePaise = &pv, p.listed, intp(p.mrp)
			}
		}
		v.Lines = append(v.Lines, al)
	}
	return v, nil
}

// Confirm lists the accepted and edited lines as store items in one transaction.
func (s *Service) Confirm(ctx context.Context, sc authz.Scope, p authn.Principal, draftID ids.ID, in []aiapi.ShelfPhotoDecision) (drafts.Result, error) {
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
		Feature: Feature, DraftID: draftID, By: p.UserID, Decisions: ds, Check: retail.OwnStore(sc.StoreID),
		Apply: func(ctx context.Context, d drafts.Draft, ds []drafts.Decision) ([]drafts.LineResult, error) {
			return s.apply(ctx, sc, d, ds, index)
		},
	})
}

type change struct {
	lineID  string
	path    string
	outcome drafts.Outcome
	product string
	price   *int64 // nil: the product's MRP
	opening int
}

func (s *Service) apply(ctx context.Context, sc authz.Scope, d drafts.Draft, ds []drafts.Decision, index map[string]int) ([]drafts.LineResult, error) {
	var out Output
	if err := d.Decode(&out); err != nil {
		return nil, err
	}
	byID := make(map[string]Line, len(out.Lines))
	for _, l := range out.Lines {
		byID[l.LineID] = l
	}
	todo := make([]change, 0, len(ds))
	want := make([]string, 0, len(ds))
	for _, dc := range ds {
		c, err := decide(dc, byID[dc.LineID], fmt.Sprintf("body.lines[%d]", index[dc.LineID]))
		if err != nil {
			return nil, err
		}
		todo = append(todo, c)
		want = append(want, c.product)
	}
	ps, err := s.products(ctx, sc.StoreID, want)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	results := make([]drafts.LineResult, 0, len(todo))
	for _, c := range todo {
		p, ok := ps[c.product]
		switch {
		case !ok || !p.active:
			return nil, retail.FieldErr(c.path+".value.productId", "no_product")
		case p.listed:
			return nil, retail.FieldErr(c.path+".value.productId", "already_listed")
		case seen[c.product]:
			return nil, retail.FieldErr(c.path+".value.productId", "duplicate")
		}
		seen[c.product] = true
		price := c.price
		if price == nil {
			if p.mrp == nil {
				return nil, retail.FieldErr(c.path+".value.pricePaise", "required")
			}
			price = p.mrp
		}
		if p.mrp != nil && *price > *p.mrp {
			return nil, retail.FieldErr(c.path+".value.pricePaise", "above_mrp")
		}
		pid, _ := ids.Parse(c.product)
		if err := s.d.Items.Add(ctx, sc, NewItem{ProductID: pid, PricePaise: *price, OpeningStock: c.opening}); err != nil {
			return nil, err
		}
		results = append(results, drafts.LineResult{LineID: c.lineID, Outcome: c.outcome})
	}
	return results, nil
}

// decide is one decision as a change: an accepted line at its matched product's MRP with no
// opening stock, an edited line as the person wrote it.
func decide(dc drafts.Decision, l Line, path string) (change, error) {
	c := change{lineID: dc.LineID, path: path, outcome: dc.Outcome}
	switch dc.Outcome {
	case drafts.Accepted:
		if l.ProductID == nil {
			return change{}, retail.FieldErr(path+".value.productId", "required")
		}
		c.product = *l.ProductID
	case drafts.Edited:
		var v aiapi.ShelfPhotoLineValue
		if err := json.Unmarshal(dc.Value, &v); err != nil {
			return change{}, retail.FieldErr(path+".value", "invalid")
		}
		if _, err := ids.Parse(v.ProductId); err != nil {
			return change{}, retail.FieldErr(path+".value.productId", "no_product")
		}
		price := int64(v.PricePaise)
		c.product, c.price = v.ProductId, &price
		if v.OpeningStock != nil {
			c.opening = *v.OpeningStock
		}
		if price < 1 || price > MaxPaise {
			return change{}, retail.FieldErr(path+".value.pricePaise", "range")
		}
		if c.opening < 0 || c.opening > MaxOpening {
			return change{}, retail.FieldErr(path+".value.openingStock", "range")
		}
	default:
		return change{}, fmt.Errorf("shelfscan: outcome %q", dc.Outcome)
	}
	return c, nil
}

func intp(p *int64) *int {
	if p == nil {
		return nil
	}
	n := int(*p)
	return &n
}

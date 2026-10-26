package catalogue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	catapi "github.com/todayz/backend/gen/api/admin-catalogue"
	aiapi "github.com/todayz/backend/gen/api/ai"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/aiadmindb"
	"github.com/todayz/backend/internal/ai/assist"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// lineAction is the A-08 action each line's button needs.
var lineAction = map[string]string{
	KindNormalise:     policy.AdminCatalogueManage,
	KindDuplicate:     policy.AdminCatalogueReview,
	KindCategoryNames: policy.AdminCatalogueManage,
}

// inputMeta is what the draft row keeps about its product: counts only.
type inputMeta struct {
	Candidates  int  `json:"candidates"`
	NamesWanted bool `json:"namesWanted"`
}

// Create checks the product is waiting for review and drafts its clean-up in the background.
func (s *Service) Create(ctx context.Context, p authn.Principal, pendingID ids.ID) (ids.ID, error) {
	if err := s.d.Kit.Guard.Require(ctx, Feature); err != nil {
		return ids.Zero, err
	}
	in, err := s.input(ctx, pendingID)
	if db.IsNoRows(err) {
		return ids.Zero, retail.FieldErr("body.pendingId", "no_pending_product")
	}
	if err != nil {
		return ids.Zero, err
	}
	return s.d.Kit.Start(ctx, retail.New{
		Feature: Feature, Store: ids.Zero, Actor: p.UserID, InputRef: pendingID.String(),
		PromptVersion: prompts.Version(Feature), Meta: inputMeta{Candidates: len(in.Candidates), NamesWanted: in.NamesWanted},
	}, func(ctx context.Context, id ids.ID) { s.process(ctx, id, pendingID) })
}

// input reads the model's view of one pending product (no rows: it is not waiting for review).
func (s *Service) input(ctx context.Context, pendingID ids.ID) (Input, error) {
	q := aiadmindb.New(s.d.DB.Q(ctx))
	row, err := q.PendingForAssist(ctx, ids.UUID(pendingID))
	if err != nil {
		return Input{}, err
	}
	cats, err := q.AssistCategories(ctx)
	if err != nil {
		return Input{}, err
	}
	in := Input{Candidates: []Candidate{}, Categories: make([]Category, 0, len(cats))}
	in.CategoryID, _ = ids.FromUUID(row.CategoryID)
	current := ""
	for i, c := range cats {
		id, _ := ids.FromUUID(c.ID)
		ref := fmt.Sprintf("c%d", i+1)
		if id == in.CategoryID {
			current = ref
		}
		in.Categories = append(in.Categories, Category{ID: id, Ref: ref, Name: c.Name})
	}
	in.Product = NewProduct(row.Name, row.BrandName, row.Variant, row.PackSize, row.Unit, current)
	cands, err := q.DuplicateCandidates(ctx, aiadmindb.DuplicateCandidatesParams{Name: row.Name, ID: row.ID})
	if err != nil {
		return Input{}, err
	}
	for i, c := range cands {
		id, _ := ids.FromUUID(c.ID)
		in.Candidates = append(in.Candidates, Candidate{ID: id, Ref: fmt.Sprintf("p%d", i+1), Name: c.Name, BrandName: c.BrandName,
			Variant: c.Variant, PackSize: c.PackSize, Unit: c.Unit, Similarity: round2(c.Similarity)})
	}
	now := map[string]string{}
	if len(row.CategoryNames) > 0 {
		if err := json.Unmarshal(row.CategoryNames, &now); err != nil {
			return Input{}, err
		}
	}
	return in.WithNames(now), nil
}

// round2 keeps a similarity to two places, as the screen shows it.
func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

func (s *Service) process(ctx context.Context, id, pendingID ids.ID) {
	log := telemetry.Logger(ctx)
	fail := func(what string, err error) {
		log.ErrorContext(ctx, "catalogue: "+what, slog.String("draft_id", id.String()), slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, client.Response{})
	}
	model, err := s.d.Settings.String(ctx, settings.AiModelFast)
	if err != nil {
		fail("model setting", err)
		return
	}
	in, err := s.input(ctx, pendingID)
	if err != nil {
		fail("product", err)
		return
	}
	r, err := Write(ctx, s.d.LLM, model, id.String(), in)
	s.d.Kit.Settle(ctx, Feature, r.Resp)
	switch {
	case err != nil:
		log.ErrorContext(ctx, "catalogue: model call", slog.String("draft_id", id.String()), slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, r.Resp)
	case r.Reject != "":
		s.d.Kit.Fail(ctx, Feature, id, r.Reject, r.Resp)
	default:
		s.d.Kit.Ready(ctx, Feature, id, r.Output, r.Resp)
	}
}

// Get answers one of the caller's own drafts, with canApply from the caller's own actions.
func (s *Service) Get(ctx context.Context, p authn.Principal, id ids.ID) (aiapi.CatalogueCleanupDraft, error) {
	d, err := assist.Load(ctx, s.d.Kit, Feature, p.UserID, id)
	if err != nil {
		return aiapi.CatalogueCleanupDraft{}, err
	}
	v := aiapi.CatalogueCleanupDraft{
		DraftId: d.ID.String(), Feature: aiapi.CatalogueCleanupDraftFeature(Feature), Status: aiapi.AiDraftStatus(d.Status),
		PromptVersion: d.PromptVersion, CreatedAt: d.CreatedAt.UTC(), Failure: retail.Failure(d), PendingId: d.InputRef,
		Outcomes: retail.Outcomes(d.Lines),
	}
	if len(d.Output) == 0 {
		return v, nil
	}
	var out Output
	if err := d.Decode(&out); err != nil {
		return aiapi.CatalogueCleanupDraft{}, err
	}
	can := func(kind string) bool { return assist.Can(ctx, s.d.Authz, p, lineAction[kind]) }
	for _, l := range out.Lines {
		switch l.Kind {
		case KindNormalise:
			v.Normalise = &aiapi.CatalogueCleanupNormalise{
				LineId: l.LineID, Name: l.Name, BrandName: l.BrandName, Variant: l.Variant, PackSize: l.PackSize,
				Unit: aiapi.AdminAiUnit(l.Unit), CategoryId: l.CategoryID, CategoryName: l.CategoryName, CanApply: can(l.Kind),
			}
		case KindDuplicate:
			v.Duplicate = &aiapi.CatalogueCleanupDuplicate{
				LineId: l.LineID, ProductId: l.ProductID, ProductName: l.ProductName, Similarity: l.Similarity,
				Reason: l.Reason, CanApply: can(l.Kind),
			}
		case KindCategoryNames:
			if l.Names != nil {
				v.CategoryNames = &aiapi.CatalogueCleanupCategoryNames{
					LineId: l.LineID, CategoryId: l.CategoryID, CanApply: can(l.Kind),
					Names: aiapi.AdminAiCategoryNames{En: l.Names.En, Hi: l.Names.Hi, Kn: l.Names.Kn, Te: l.Names.Te, Ta: l.Names.Ta},
				}
			}
		}
	}
	return v, nil
}

// Confirm applies the accepted or edited lines through A-08's writer. A line the caller may
// not apply is not_permitted and the draft stays open for someone who may.
func (s *Service) Confirm(ctx context.Context, p authn.Principal, draftID ids.ID, in []aiapi.CatalogueCleanupDecision) (drafts.Result, error) {
	if err := s.d.Kit.Guard.Require(ctx, Feature); err != nil {
		return drafts.Result{}, err
	}
	ds := make([]drafts.Decision, 0, len(in))
	idx := make([]string, 0, len(in))
	for _, dc := range in {
		var v any
		if dc.Value != nil {
			v = dc.Value
		}
		d, err := retail.Decision(dc.LineId, dc.Outcome, v)
		if err != nil {
			return drafts.Result{}, err
		}
		ds = append(ds, d)
		idx = append(idx, dc.LineId)
	}
	at := retail.Index(idx)
	return s.d.Kit.Drafts.Confirm(ctx, drafts.Confirmation{
		Feature: Feature, DraftID: draftID, By: p.UserID, Decisions: ds, Check: assist.Own(p.UserID), KeepOpen: true,
		Apply: func(ctx context.Context, d drafts.Draft, ds []drafts.Decision) ([]drafts.LineResult, error) {
			return s.apply(ctx, p, d, ds, at)
		},
	})
}

// step is one validated line, ready to write.
type step struct {
	dc      drafts.Decision
	line    Line
	product catapi.AdminProductUpdate
	into    ids.ID
	names   catapi.AdminCategoryNames
	cat     ids.ID
	allowed bool
}

func (s *Service) apply(ctx context.Context, p authn.Principal, d drafts.Draft, ds []drafts.Decision, at map[string]int) ([]drafts.LineResult, error) {
	var out Output
	if err := d.Decode(&out); err != nil {
		return nil, retail.NotFound()
	}
	pendingID, err := ids.Parse(d.InputRef)
	if err != nil {
		return nil, retail.NotFound()
	}
	path := func(lineID, rest string) string {
		return fmt.Sprintf("body.lines[%d].%s", at[lineID], rest)
	}
	byID := map[string]Line{}
	for _, l := range out.Lines {
		byID[l.LineID] = l
	}
	// Merge or edit, not both: also against a line a partial confirm already applied.
	applies := map[string]bool{}
	for _, l := range d.Lines {
		if l.Outcome == drafts.Accepted || l.Outcome == drafts.Edited {
			applies[byID[l.LineID].Kind] = true
		}
	}
	for _, dc := range ds {
		applies[byID[dc.LineID].Kind] = true
	}
	if applies[KindNormalise] && applies[KindDuplicate] {
		return nil, retail.FieldErr("body.lines", "merge_or_edit")
	}
	if _, err := aiadmindb.New(s.d.DB.Q(ctx)).PendingForAssist(ctx, ids.UUID(pendingID)); db.IsNoRows(err) {
		return nil, problem.New(genproblems.InvalidTransition, "The product is no longer waiting for review.")
	} else if err != nil {
		return nil, err
	}
	// Validate every decision before writing anything, so a 422 leaves the catalogue as it was.
	steps := make([]step, 0, len(ds))
	for _, dc := range ds {
		l, ok := byID[dc.LineID]
		if !ok {
			return nil, retail.NotFound()
		}
		st := step{dc: dc, line: l, allowed: assist.Can(ctx, s.d.Authz, p, lineAction[l.Kind])}
		var v aiapi.CatalogueCleanupLineValue
		if dc.Outcome == drafts.Edited {
			if err := json.Unmarshal(dc.Value, &v); err != nil {
				return nil, retail.FieldErr(path(dc.LineID, "value"), "invalid")
			}
		}
		switch l.Kind {
		case KindNormalise:
			st.product, err = normalise(l, dc.Outcome == drafts.Edited, v, func(rest string) string { return path(dc.LineID, rest) })
			if err != nil {
				return nil, err
			}
		case KindDuplicate:
			into := l.ProductID
			if dc.Outcome == drafts.Edited {
				if v.ProductId == nil || v.Name != nil || v.Variant != nil || v.PackSize != nil || v.Unit != nil || v.CategoryId != nil || v.Names != nil {
					return nil, retail.FieldErr(path(dc.LineID, "value.productId"), "required")
				}
				into = *v.ProductId
			}
			if !oneOf(into, out.Candidates) {
				return nil, retail.FieldErr(path(dc.LineID, "value.productId"), "not_a_candidate")
			}
			if st.into, err = ids.Parse(into); err != nil {
				return nil, retail.FieldErr(path(dc.LineID, "value.productId"), "invalid")
			}
		case KindCategoryNames:
			if l.Names == nil {
				return nil, retail.NotFound()
			}
			n := *l.Names
			if dc.Outcome == drafts.Edited {
				if v.Names == nil || v.Name != nil || v.Variant != nil || v.PackSize != nil || v.Unit != nil || v.CategoryId != nil || v.ProductId != nil {
					return nil, retail.FieldErr(path(dc.LineID, "value.names"), "required")
				}
				n = Names{En: v.Names.En, Hi: v.Names.Hi, Kn: v.Names.Kn, Te: v.Names.Te, Ta: v.Names.Ta}
			}
			for _, lang := range Languages {
				if !fits(n.Get(lang), MaxNameLang) {
					return nil, retail.FieldErr(path(dc.LineID, "value.names."+lang), "length")
				}
			}
			st.names = catapi.AdminCategoryNames{En: strings.TrimSpace(n.En), Hi: opt(n.Hi), Kn: opt(n.Kn), Te: opt(n.Te), Ta: opt(n.Ta)}
			if st.cat, err = ids.Parse(l.CategoryID); err != nil {
				return nil, retail.NotFound()
			}
		default:
			return nil, retail.NotFound()
		}
		steps = append(steps, st)
	}
	res := make([]drafts.LineResult, 0, len(steps))
	for _, st := range steps {
		if !st.allowed {
			res = append(res, drafts.LineResult{LineID: st.dc.LineID, Outcome: drafts.NotPermitted, Reason: string(retail.RefuseNotPermitted)})
			continue
		}
		var err error
		switch st.line.Kind {
		case KindNormalise:
			err = s.d.Catalogue.AssistUpdateProduct(ctx, p.UserID, pendingID, st.product)
		case KindDuplicate:
			err = s.d.Catalogue.AssistMergePending(ctx, p.UserID, pendingID, st.into)
		case KindCategoryNames:
			err = s.d.Catalogue.AssistCategoryNames(ctx, p.UserID, st.cat, st.names)
		}
		if err != nil {
			return nil, err
		}
		res = append(res, drafts.LineResult{LineID: st.dc.LineID, Outcome: st.dc.Outcome})
	}
	return res, nil
}

// normalise is the product edit of a normalise line: the drafted fields, or the reviewer's
// edited ones (at least one) over them.
func normalise(l Line, edited bool, v aiapi.CatalogueCleanupLineValue, path func(string) string) (catapi.AdminProductUpdate, error) {
	name, pack, unit, cat := l.Name, l.PackSize, l.Unit, l.CategoryID
	variant := l.Variant
	if edited {
		if v.ProductId != nil || v.Names != nil {
			return catapi.AdminProductUpdate{}, retail.FieldErr(path("value"), "not_for_normalise")
		}
		if v.Name == nil && v.Variant == nil && v.PackSize == nil && v.Unit == nil && v.CategoryId == nil {
			return catapi.AdminProductUpdate{}, retail.FieldErr(path("value"), "at_least_one_field")
		}
		if v.Name != nil {
			name = *v.Name
		}
		if v.Variant != nil {
			variant = v.Variant
		}
		if v.PackSize != nil {
			pack = *v.PackSize
		}
		if v.Unit != nil {
			unit = string(*v.Unit)
		}
		if v.CategoryId != nil {
			cat = *v.CategoryId
		}
	}
	if !oneOf(unit, Units) {
		return catapi.AdminProductUpdate{}, retail.FieldErr(path("value.unit"), "invalid")
	}
	if pack <= 0 || pack > MaxPack {
		return catapi.AdminProductUpdate{}, retail.FieldErr(path("value.packSize"), "range")
	}
	u := catapi.AdminProductUnit(unit)
	return catapi.AdminProductUpdate{Name: &name, Variant: variant, PackSize: &pack, Unit: &u, CategoryId: &cat}, nil
}

func opt(s string) *string {
	t := strings.TrimSpace(s)
	return &t
}

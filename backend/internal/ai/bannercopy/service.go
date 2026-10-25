package bannercopy

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/gen/sqlc/airetaildb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

type inputMeta struct {
	Offer    bool   `json:"offer"`
	Items    int    `json:"items"`
	Language string `json:"language"`
}

// Create checks the offer or items (this store's) and drafts the copy in the background.
func (s *Service) Create(ctx context.Context, sc authz.Scope, actor ids.ID, req aiapi.BannerCopyDraftRequest) (ids.ID, error) {
	if err := s.d.Kit.Guard.Require(ctx, Feature); err != nil {
		return ids.Zero, err
	}
	f, ref, err := s.facts(ctx, sc.StoreID, req)
	if err != nil {
		return ids.Zero, err
	}
	return s.d.Kit.Start(ctx, retail.New{
		Feature: Feature, Store: sc.StoreID, Actor: actor, InputRef: ref, PromptVersion: prompts.Version(Feature),
		Meta: inputMeta{Offer: f.Offer != nil, Items: len(f.Items), Language: f.Language},
	}, func(ctx context.Context, id ids.ID) { s.process(ctx, id, f) })
}

// facts reads what the model is given: the shop, its language, the offer's terms and the
// items with their prices.
func (s *Service) facts(ctx context.Context, storeID ids.ID, req aiapi.BannerCopyDraftRequest) (Facts, string, error) {
	hasItems := req.ItemIds != nil
	if (req.OfferId == nil) == !hasItems {
		return Facts{}, "", retail.FieldErr("body", "one_of_offer_items")
	}
	q := airetaildb.New(s.d.DB.Q(ctx))
	var f Facts
	var err error
	if f.Shop, err = q.StoreName(ctx, ids.UUID(storeID)); err != nil {
		return Facts{}, "", err
	}
	if f.Language, err = q.StoreLang(ctx, ids.UUID(storeID)); err != nil {
		return Facts{}, "", err
	}
	var want []pgtype.UUID
	ref := ""
	if req.OfferId != nil {
		oid, err := ids.Parse(*req.OfferId)
		if err != nil {
			return Facts{}, "", retail.FieldErr("body.offerId", "no_offer")
		}
		o, err := q.OfferForBanner(ctx, airetaildb.OfferForBannerParams{ID: ids.UUID(oid), StoreID: ids.UUID(storeID)})
		if db.IsNoRows(err) || (err == nil && o.Status == "ended") {
			return Facts{}, "", retail.FieldErr("body.offerId", "no_offer")
		}
		if err != nil {
			return Facts{}, "", err
		}
		f.Offer = offerFacts(o)
		ref = oid.String()
		want = o.StoreItemIds
		if len(want) > MaxItems {
			want = want[:MaxItems]
		}
	} else {
		if len(*req.ItemIds) == 0 || len(*req.ItemIds) > MaxItems {
			return Facts{}, "", retail.FieldErr("body.itemIds", "count")
		}
		for i, s := range *req.ItemIds {
			id, err := ids.Parse(s)
			if err != nil {
				return Facts{}, "", retail.FieldErr("body.itemIds["+strconv.Itoa(i)+"]", "not_in_store")
			}
			want = append(want, ids.UUID(id))
		}
	}
	if len(want) > 0 {
		rows, err := q.ItemsForBanner(ctx, airetaildb.ItemsForBannerParams{StoreID: ids.UUID(storeID), Ids: want})
		if err != nil {
			return Facts{}, "", err
		}
		if !hasItems || len(rows) == len(uniq(want)) {
			for _, r := range rows {
				id, _ := ids.FromUUID(r.ID)
				it := Item{N: len(f.Items) + 1, Name: retail.ItemName(r.Name, r.Variant, r.PackSize, r.Unit), Price: Rupees(r.PricePaise), ID: id.String()}
				if r.MrpPaise != nil && *r.MrpPaise > r.PricePaise {
					m := Rupees(*r.MrpPaise)
					it.MRP = &m
				}
				f.Items = append(f.Items, it)
			}
		} else {
			return Facts{}, "", retail.FieldErr("body.itemIds", "not_in_store")
		}
	}
	return f, ref, nil
}

func offerFacts(o airetaildb.OfferForBannerRow) *Offer {
	of := &Offer{Title: o.Title, Kind: o.Kind, AllItems: o.Scope == "store", Buy: o.BuyQty, Get: o.GetQty}
	switch o.Kind {
	case "percent":
		v := o.Value
		of.PercentOff = &v
	case "flat", "bill_min":
		r := Rupees(o.Value)
		of.RupeesOff = &r
	}
	if o.MinBillPaise != nil {
		m := Rupees(*o.MinBillPaise)
		of.MinBill = &m
	}
	if o.EndsAt != nil {
		e := o.EndsAt.In(clock.IST).Format("2 Jan")
		of.EndsOn = &e
	}
	return of
}

func (s *Service) process(ctx context.Context, id ids.ID, f Facts) {
	log := telemetry.Logger(ctx)
	model, err := s.d.Settings.String(ctx, settings.AiModelFast)
	if err != nil {
		log.ErrorContext(ctx, "bannercopy: model setting", slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, client.Response{})
		return
	}
	r, err := Write(ctx, s.d.LLM, model, id.String(), f)
	s.d.Kit.Settle(ctx, Feature, r.Resp)
	switch {
	case err != nil:
		log.ErrorContext(ctx, "bannercopy: model call", slog.String("draft_id", id.String()), slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, r.Resp)
	case r.Reject != "":
		s.d.Kit.Fail(ctx, Feature, id, r.Reject, r.Resp)
	default:
		s.d.Kit.Ready(ctx, Feature, id, r.Output, r.Resp)
	}
}

// Get answers one draft of the caller's store.
func (s *Service) Get(ctx context.Context, sc authz.Scope, id ids.ID) (aiapi.BannerCopyDraft, error) {
	d, err := s.d.Kit.Load(ctx, Feature, sc.StoreID, id)
	if err != nil {
		return aiapi.BannerCopyDraft{}, err
	}
	var meta inputMeta
	_ = json.Unmarshal(d.InputMeta, &meta)
	if meta.Language == "" {
		meta.Language = "en"
	}
	v := aiapi.BannerCopyDraft{
		DraftId: d.ID.String(), Feature: aiapi.BannerCopyDraftFeature(Feature), Status: aiapi.AiDraftStatus(d.Status),
		PromptVersion: d.PromptVersion, CreatedAt: d.CreatedAt.UTC(), Failure: retail.Failure(d),
		Language: aiapi.AiLanguage(meta.Language), Lines: []aiapi.BannerCopyLine{}, Outcomes: retail.Outcomes(d.Lines),
	}
	if len(d.Output) == 0 {
		return v, nil
	}
	var out Output
	if err := d.Decode(&out); err != nil {
		return aiapi.BannerCopyDraft{}, err
	}
	for _, l := range out.Lines {
		v.Lines = append(v.Lines, aiapi.BannerCopyLine{
			LineId: l.LineID, Headline: l.Headline, Subline: l.Subline, HeadlineEn: l.HeadlineEn, SublineEn: l.SublineEn,
			Tint: aiapi.BannerTint(l.Tint), StoreItemId: l.StoreItemID,
		})
	}
	return v, nil
}

// Confirm records the pick (at most one option accepted or edited) and answers its copy; it
// writes no banner.
func (s *Service) Confirm(ctx context.Context, sc authz.Scope, p authn.Principal, draftID ids.ID, in []aiapi.BannerCopyDecision) (aiapi.BannerCopyConfirmResult, error) {
	if err := s.d.Kit.Guard.Require(ctx, Feature); err != nil {
		return aiapi.BannerCopyConfirmResult{}, err
	}
	ds := make([]drafts.Decision, 0, len(in))
	picked := -1
	for i, dc := range in {
		var v any
		if dc.Value != nil {
			v = dc.Value
		}
		if dc.Outcome != aiapi.AiConfirmOutcomeDiscarded {
			if picked >= 0 {
				return aiapi.BannerCopyConfirmResult{}, retail.FieldErr("body.lines["+strconv.Itoa(i)+"].outcome", "one_pick")
			}
			picked = i
		}
		d, err := retail.Decision(dc.LineId, dc.Outcome, v)
		if err != nil {
			return aiapi.BannerCopyConfirmResult{}, err
		}
		ds = append(ds, d)
	}
	var copyOut *aiapi.BannerCopyValue
	res, err := s.d.Kit.Drafts.Confirm(ctx, drafts.Confirmation{
		Feature: Feature, DraftID: draftID, By: p.UserID, Decisions: ds, Check: retail.OwnStore(sc.StoreID),
		Apply: func(ctx context.Context, d drafts.Draft, ds []drafts.Decision) ([]drafts.LineResult, error) {
			var out Output
			if err := d.Decode(&out); err != nil {
				return nil, err
			}
			results := make([]drafts.LineResult, 0, len(ds))
			for _, dc := range ds {
				c, err := s.pick(ctx, sc.StoreID, out, dc, picked)
				if err != nil {
					return nil, err
				}
				copyOut = &c
				results = append(results, drafts.LineResult{LineID: dc.LineID, Outcome: dc.Outcome})
			}
			return results, nil
		},
	})
	if err != nil {
		return aiapi.BannerCopyConfirmResult{}, err
	}
	if copyOut == nil && res.Status == drafts.StatusConfirmed && picked >= 0 {
		// A repeat: the stored result, and the copy rebuilt from this request's pick.
		if d, err := s.d.Kit.Drafts.Get(ctx, Feature, draftID); err == nil {
			var out Output
			if d.Decode(&out) == nil {
				if c, err := s.pick(ctx, sc.StoreID, out, ds[picked], picked); err == nil {
					copyOut = &c
				}
			}
		}
	}
	return aiapi.BannerCopyConfirmResult{
		DraftId: res.DraftID.String(), Status: aiapi.BannerCopyConfirmResultStatus(res.Status),
		Lines: retail.Outcomes(res.Lines), Copy: copyOut,
	}, nil
}

// pick is the copy of one accepted (as drafted) or edited (as sent) option.
func (s *Service) pick(ctx context.Context, storeID ids.ID, out Output, dc drafts.Decision, at int) (aiapi.BannerCopyValue, error) {
	path := "body.lines[" + strconv.Itoa(at) + "].value"
	if dc.Outcome == drafts.Accepted {
		for _, l := range out.Lines {
			if l.LineID == dc.LineID {
				return aiapi.BannerCopyValue{Headline: l.Headline, Subline: l.Subline, Tint: aiapi.BannerTint(l.Tint), StoreItemId: l.StoreItemID}, nil
			}
		}
		return aiapi.BannerCopyValue{}, retail.NotFound()
	}
	var v aiapi.BannerCopyValue
	if err := json.Unmarshal(dc.Value, &v); err != nil {
		return aiapi.BannerCopyValue{}, retail.FieldErr(path, "invalid")
	}
	v.Headline, v.Subline = strings.TrimSpace(v.Headline), strings.TrimSpace(v.Subline)
	switch {
	case v.Headline == "" || utf8.RuneCountInString(v.Headline) > MaxHeadline:
		return aiapi.BannerCopyValue{}, retail.FieldErr(path+".headline", "length")
	case utf8.RuneCountInString(v.Subline) > MaxSubline:
		return aiapi.BannerCopyValue{}, retail.FieldErr(path+".subline", "length")
	case !slices.Contains(Tints, string(v.Tint)):
		return aiapi.BannerCopyValue{}, retail.FieldErr(path+".tint", "enum")
	}
	if v.StoreItemId != nil {
		id, err := ids.Parse(*v.StoreItemId)
		if err != nil {
			return aiapi.BannerCopyValue{}, retail.FieldErr(path+".storeItemId", "not_in_store")
		}
		rows, err := airetaildb.New(s.d.DB.Q(ctx)).ItemsForBanner(ctx, airetaildb.ItemsForBannerParams{StoreID: ids.UUID(storeID), Ids: []pgtype.UUID{ids.UUID(id)}})
		if err != nil {
			return aiapi.BannerCopyValue{}, err
		}
		if len(rows) == 0 {
			return aiapi.BannerCopyValue{}, retail.FieldErr(path+".storeItemId", "not_in_store")
		}
	}
	return v, nil
}

func uniq(us []pgtype.UUID) []pgtype.UUID {
	seen := map[[16]byte]bool{}
	var out []pgtype.UUID
	for _, u := range us {
		if !seen[u.Bytes] {
			seen[u.Bytes] = true
			out = append(out, u)
		}
	}
	return out
}

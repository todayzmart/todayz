package main

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	aiapi "github.com/todayz/backend/gen/api/ai"
	catalogueapi "github.com/todayz/backend/gen/api/catalogue"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/ai/bannercopy"
	"github.com/todayz/backend/internal/ai/billscan"
	aicatalogue "github.com/todayz/backend/internal/ai/catalogue"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/ai/listparse"
	"github.com/todayz/backend/internal/ai/querynorm"
	"github.com/todayz/backend/internal/ai/replydraft"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/ai/riskexplain"
	"github.com/todayz/backend/internal/ai/shelfscan"
	"github.com/todayz/backend/internal/ai/triage"
	"github.com/todayz/backend/internal/ai/voicesearch"
	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/reviews"
	"github.com/todayz/backend/internal/voice"
)

// aiRetailSeams lets a test swap the retail AI kit and model (a fake model, a
// guard that counts the providers as configured, an inline runner).
var aiRetailSeams func(d *deps, kit *retail.Kit, llm *client.LLM)

// aiConsumerSeams lets a test swap voice search's speech client (W3-12); the
// model and guard come from aiRetailSeams.
var aiConsumerSeams func(d *deps, stt *client.STT)

// mountAI mounts the ai domain's routes. Only the work packages that build
// internal/ai edit this file (D-50). W3-10 serves no route of its own; W3-11
// mounts the retailer helpers under {S}/ai (04 §2.8): supplier bill (AI-R2),
// shelf photo (AI-R3), review reply (AI-R6) and banner copy (AI-R9) drafts,
// each confirmed by a person before anything is written (11 §1 rule 1).
// W3-12 mounts the customer's own helpers under /v1/me/ai (D-101): list parse
// (AI-C1, a draft the customer confirms line by line), search assist (AI-C2)
// and voice search (AI-C5), which keep nothing.
// Without a database or config (a route-only test) every call is 503.
func mountAI(r chi.Router, d *deps) {
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	ops := aiRetailOps()
	mws := []aiapi.StrictMiddlewareFunc{aiapi.StrictMiddlewareFunc(authz.Strict[aiapi.StrictHandlerFunc](az, ops))}
	if d.DB == nil || d.Config == nil || d.Settings == nil {
		mws = append(mws, func(aiapi.StrictHandlerFunc, string) aiapi.StrictHandlerFunc {
			return func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
				return nil, problem.New(genproblems.ServiceUnavailable, "")
			}
		})
		aiapi.Register(r, aiServer{}, mws...)
		return
	}
	k := aiKitFor(d)
	kit, llm := retail.Kit{Guard: k.Guard, Drafts: k.Drafts}, k.LLM
	if aiRetailSeams != nil {
		aiRetailSeams(d, &kit, &llm)
	}
	cd := catalogue.Deps{Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log, Settings: d.Settings, Authz: d.Authz}
	cat, err := catalogue.New(cd)
	if err != nil {
		panic("ai: catalogue: " + err.Error())
	}
	rv, err := reviews.New(reviews.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, Sales: posForReviews{newPos(d)}})
	if err != nil {
		panic("ai: reviews: " + err.Error())
	}
	bill, err := billscan.New(billscan.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, LLM: llm, Kit: kit,
		Stock: aiBillStock{w: catalogue.NewWriter(d.Clock), db: d.DB}})
	if err != nil {
		panic("ai: billscan: " + err.Error())
	}
	shelf, err := shelfscan.New(shelfscan.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, LLM: llm, Kit: kit,
		Items: aiShelfItems{cat}})
	if err != nil {
		panic("ai: shelfscan: " + err.Error())
	}
	reply, err := replydraft.New(replydraft.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, LLM: llm, Kit: kit, Replies: rv})
	if err != nil {
		panic("ai: replydraft: " + err.Error())
	}
	banner, err := bannercopy.New(bannercopy.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, LLM: llm, Kit: kit})
	if err != nil {
		panic("ai: bannercopy: " + err.Error())
	}
	list, err := listparse.New(listparse.Deps{Clock: d.Clock, Settings: d.Settings, LLM: llm, Guard: kit.Guard, Drafts: kit.Drafts, Go: kit.Go})
	if err != nil {
		panic("ai: listparse: " + err.Error())
	}
	assist, err := querynorm.New(querynorm.Deps{DB: d.DB, Settings: d.Settings, LLM: llm, Guard: kit.Guard})
	if err != nil {
		panic("ai: querynorm: " + err.Error())
	}
	stt := k.STT(voicesearch.Feature)
	if aiConsumerSeams != nil {
		aiConsumerSeams(d, &stt)
	}
	speech, err := voicesearch.New(voicesearch.Deps{DB: d.DB, Guard: kit.Guard, STT: stt})
	if err != nil {
		panic("ai: voicesearch: " + err.Error())
	}
	aiapi.Register(r, aiServer{
		Handlers: billscan.Handlers{S: bill},
		shelf:    shelfscan.Handlers{S: shelf},
		reply:    replydraft.Handlers{S: reply},
		banner:   bannercopy.Handlers{S: banner},
		list:     listparse.Handlers{S: list},
		assist:   querynorm.Handlers{S: assist},
		speech:   voicesearch.Handlers{S: speech},
		admin:    newAIAdmin(d, kit, llm),
		guard:    kit.Guard,
	}, mws...)
}

// aiRetailOps is every ai operation's route check.
func aiRetailOps() map[string]string {
	ops := map[string]string{"GetStoreAiRetailFeatures": policy.InsightsView, "GetMyAiFeatures": policy.MeAiUse}
	for _, m := range []map[string]string{
		billscan.Ops, shelfscan.Ops, replydraft.Ops, bannercopy.Ops, listparse.Ops, querynorm.Ops, voicesearch.Ops,
		aicatalogue.Ops, triage.Ops, riskexplain.Ops,
	} {
		maps.Copy(ops, m)
	}
	return ops
}

// aiServer is the ai domain's strict server: one embedded handler set per
// feature, plus the features switch read.
type aiServer struct {
	billscan.Handlers
	shelf  shelfscan.Handlers
	reply  replydraft.Handlers
	banner bannercopy.Handlers
	list   listparse.Handlers
	assist querynorm.Handlers
	speech voicesearch.Handlers
	admin  aiAdmin // W3-13: the admin assistants (ai_admin.go)
	guard  *guard.Guard
}

var _ aiapi.StrictServerInterface = aiServer{}

func (s aiServer) GetStoreAiRetailFeatures(ctx context.Context, _ aiapi.GetStoreAiRetailFeaturesRequestObject) (aiapi.GetStoreAiRetailFeaturesResponseObject, error) {
	on := func(feature string) (bool, error) { return s.guard.Available(ctx, feature) }
	out := aiapi.AiRetailFeatures{MaxPhotoBytes: retail.MaxPhotoBytes, PhotoTypes: retail.PhotoTypes}
	for _, f := range []struct {
		name string
		to   *bool
	}{
		{billscan.Feature, &out.SupplierBill}, {shelfscan.Feature, &out.ShelfPhoto},
		{replydraft.Feature, &out.ReviewReply}, {bannercopy.Feature, &out.BannerCopy},
	} {
		v, err := on(f.name)
		if err != nil {
			return nil, err
		}
		*f.to = v
	}
	return aiapi.GetStoreAiRetailFeatures200JSONResponse(out), nil
}

// GetMyAiFeatures is which customer helpers are on (D-231) and the recording
// limits; it counts nothing against the cap.
func (s aiServer) GetMyAiFeatures(ctx context.Context, _ aiapi.GetMyAiFeaturesRequestObject) (aiapi.GetMyAiFeaturesResponseObject, error) {
	if _, err := querynorm.Customer(ctx); err != nil {
		return nil, err
	}
	out := aiapi.AiCustomerFeatures{MaxAudioSeconds: voice.MaxSeconds, MaxAudioBytes: voice.MaxAudioBytes, AudioTypes: voice.MediaTypes}
	for _, f := range []struct {
		name string
		to   *bool
	}{
		{listparse.Feature, &out.ListParse}, {voicesearch.Feature, &out.VoiceSearch}, {querynorm.Feature, &out.SearchAssist},
	} {
		v, err := s.guard.Available(ctx, f.name)
		if err != nil {
			return nil, err
		}
		*f.to = v
	}
	return aiapi.GetMyAiFeatures200JSONResponse(out), nil
}

func (s aiServer) CreateListParseDraft(ctx context.Context, req aiapi.CreateListParseDraftRequestObject) (aiapi.CreateListParseDraftResponseObject, error) {
	return s.list.CreateListParseDraft(ctx, req)
}

func (s aiServer) GetListParseDraft(ctx context.Context, req aiapi.GetListParseDraftRequestObject) (aiapi.GetListParseDraftResponseObject, error) {
	return s.list.GetListParseDraft(ctx, req)
}

func (s aiServer) ConfirmListParseDraft(ctx context.Context, req aiapi.ConfirmListParseDraftRequestObject) (aiapi.ConfirmListParseDraftResponseObject, error) {
	return s.list.ConfirmListParseDraft(ctx, req)
}

func (s aiServer) SearchAssist(ctx context.Context, req aiapi.SearchAssistRequestObject) (aiapi.SearchAssistResponseObject, error) {
	return s.assist.SearchAssist(ctx, req)
}

func (s aiServer) VoiceSearch(ctx context.Context, req aiapi.VoiceSearchRequestObject) (aiapi.VoiceSearchResponseObject, error) {
	return s.speech.VoiceSearch(ctx, req)
}

func (s aiServer) CreateShelfPhotoDraft(ctx context.Context, req aiapi.CreateShelfPhotoDraftRequestObject) (aiapi.CreateShelfPhotoDraftResponseObject, error) {
	return s.shelf.CreateShelfPhotoDraft(ctx, req)
}

func (s aiServer) GetShelfPhotoDraft(ctx context.Context, req aiapi.GetShelfPhotoDraftRequestObject) (aiapi.GetShelfPhotoDraftResponseObject, error) {
	return s.shelf.GetShelfPhotoDraft(ctx, req)
}

func (s aiServer) ConfirmShelfPhotoDraft(ctx context.Context, req aiapi.ConfirmShelfPhotoDraftRequestObject) (aiapi.ConfirmShelfPhotoDraftResponseObject, error) {
	return s.shelf.ConfirmShelfPhotoDraft(ctx, req)
}

func (s aiServer) CreateReviewReplyDraft(ctx context.Context, req aiapi.CreateReviewReplyDraftRequestObject) (aiapi.CreateReviewReplyDraftResponseObject, error) {
	return s.reply.CreateReviewReplyDraft(ctx, req)
}

func (s aiServer) GetReviewReplyDraft(ctx context.Context, req aiapi.GetReviewReplyDraftRequestObject) (aiapi.GetReviewReplyDraftResponseObject, error) {
	return s.reply.GetReviewReplyDraft(ctx, req)
}

func (s aiServer) ConfirmReviewReplyDraft(ctx context.Context, req aiapi.ConfirmReviewReplyDraftRequestObject) (aiapi.ConfirmReviewReplyDraftResponseObject, error) {
	return s.reply.ConfirmReviewReplyDraft(ctx, req)
}

func (s aiServer) CreateBannerCopyDraft(ctx context.Context, req aiapi.CreateBannerCopyDraftRequestObject) (aiapi.CreateBannerCopyDraftResponseObject, error) {
	return s.banner.CreateBannerCopyDraft(ctx, req)
}

func (s aiServer) GetBannerCopyDraft(ctx context.Context, req aiapi.GetBannerCopyDraftRequestObject) (aiapi.GetBannerCopyDraftResponseObject, error) {
	return s.banner.GetBannerCopyDraft(ctx, req)
}

func (s aiServer) ConfirmBannerCopyDraft(ctx context.Context, req aiapi.ConfirmBannerCopyDraftRequestObject) (aiapi.ConfirmBannerCopyDraftResponseObject, error) {
	return s.banner.ConfirmBannerCopyDraft(ctx, req)
}

// aiBillStock writes a confirmed bill line as a supplier_bill stock in, in
// the confirm's transaction, referencing the draft.
type aiBillStock struct {
	w  *catalogue.Writer
	db *db.DB
}

func (a aiBillStock) AddFromBill(ctx context.Context, in billscan.StockIn) error {
	_, err := a.w.AddStock(ctx, a.db.Q(ctx), catalogue.StockIn{
		StoreID: in.StoreID, ItemID: in.ItemID, Qty: int32(in.Qty), // #nosec G115 -- bounded by billscan.MaxQuantity
		Source: catalogue.SourceSupplierBill, ExpiresOn: in.ExpiresOn, CostPaise: in.CostPaise,
		RefType: "ai_draft", RefID: in.DraftID, Who: catalogue.Who{UserID: in.Who},
	})
	switch {
	case errors.Is(err, catalogue.ErrItemNotFound):
		return billscan.ErrItemGone
	case errors.Is(err, catalogue.ErrQuantity):
		return billscan.ErrQuantity
	}
	return err
}

// aiShelfItems creates a confirmed shelf line through the catalogue's own
// create (its price and permission checks), in a savepoint of the confirm's
// transaction. What the catalogue refuses for this line alone (no
// item.price.edit, the product, the listing, the price) refuses the line.
type aiShelfItems struct{ s *catalogue.Service }

func (a aiShelfItems) Add(ctx context.Context, sc authz.Scope, it shelfscan.NewItem) error {
	price, opening := int(it.PricePaise), it.OpeningStock
	_, err := a.s.CreateItem(ctx, sc.BusinessID.String(), sc.StoreID.String(), &catalogueapi.StoreItemCreate{
		ProductId: it.ProductID.String(), PricePaise: price, OpeningStock: &opening,
	})
	return shelfRefusal(err)
}

// shelfRefusal is the catalogue's answer for one shelf line as a line refusal: a missing
// permission (a createItem without item.price.edit) is not_permitted, a listed product, a
// price above MRP or a bad product is that line's invalid_* code. Anything else fails the
// confirm.
func shelfRefusal(err error) error {
	var pe *problem.Error
	switch {
	case err == nil:
		return nil
	case problem.CodeOf(err) == genproblems.Forbidden:
		return retail.RefuseNotPermitted
	case problem.CodeOf(err) == genproblems.Conflict:
		return retail.RefuseListed
	case errors.As(err, &pe) && pe.Code == genproblems.ValidationFailed:
		if _, ok := pe.Fields["body.pricePaise"]; ok {
			return retail.RefusePriceAboveMRP
		}
		if _, ok := pe.Fields["body.productId"]; ok {
			return retail.RefuseProduct
		}
	}
	return err
}

// aiProviderTimeout bounds one provider call; a feature with a tighter budget
// (search assist, 3 s) sets a context deadline.
const aiProviderTimeout = 30 * time.Second //settings:ok an HTTP client guard, not a product number

// aiKit is the AI platform a feature package is built from (W3-10).
type aiKit struct {
	LLM    client.LLM // metered Anthropic client (the mock in local and test)
	Guard  *guard.Guard
	Drafts *drafts.Service
	stt    client.STT
	d      *deps
}

// STT is the metered speech client for feature (voice_stock or voice_search).
func (k *aiKit) STT(feature string) client.STT {
	return client.NewMeteredSTT(k.stt, k.d.Clock, feature)
}

var (
	aiKitsMu sync.Mutex
	aiKits   = map[*deps]*aiKit{}
)

// aiKitFor returns the kit for d, built once, so every feature shares one
// guard (one cost gauge) and one HTTP client.
func aiKitFor(d *deps) *aiKit {
	aiKitsMu.Lock()
	defer aiKitsMu.Unlock()
	if k, ok := aiKits[d]; ok {
		return k
	}
	p := d.Config.Providers
	hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: d.Config.NoEgress, Timeout: aiProviderTimeout})
	k := &aiKit{
		LLM:    client.NewMetered(client.NewAnthropic(p.LLMAPIURL, p.AnthropicAPIKey, hc), d.Clock),
		Guard:  guard.New(d.DB, d.Settings, d.Clock, guard.ProvidersFrom(d.Config)),
		Drafts: drafts.New(d.DB, d.Clock),
		stt:    client.NewSarvam(p.STTAPIURL, p.SarvamAPIKey, hc),
		d:      d,
	}
	aiKits[d] = k
	return k
}

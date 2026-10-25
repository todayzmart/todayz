package main

import (
	"context"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/admin"
	aicatalogue "github.com/todayz/backend/internal/ai/catalogue"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/ai/riskexplain"
	"github.com/todayz/backend/internal/ai/triage"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/risk"
	"github.com/todayz/backend/internal/support"
)

// aiAdmin is W3-13's admin assistants (AI-A1…A3, 11 §4): the catalogue clean-up on A-08,
// the ticket triage on A-14 and the risk summary on A-13. Each confirm writes through its
// screen's own service (admin, support), as the screen's buttons would.
type aiAdmin struct {
	catalogue aicatalogue.Handlers
	triage    triage.Handlers
	risk      riskexplain.Handlers
}

// newAIAdmin builds the admin assistants on the shared AI kit.
func newAIAdmin(d *deps, kit retail.Kit, llm client.LLM) aiAdmin {
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	var hashKey []byte
	if d.Config != nil {
		hashKey = d.Config.Keys.PIIHashKey.Bytes()
	}
	aud := audit.New(d.DB, d.Clock, hashKey)
	ad, err := admin.New(admin.Deps{DB: d.DB, Clock: d.Clock, Audit: aud, Authz: az})
	if err != nil {
		panic("ai: admin: " + err.Error())
	}
	sup, err := support.New(support.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, Audit: aud})
	if err != nil {
		panic("ai: support: " + err.Error())
	}
	cat, err := aicatalogue.New(aicatalogue.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, LLM: llm, Kit: kit,
		Catalogue: ad, Authz: az})
	if err != nil {
		panic("ai: catalogue clean-up: " + err.Error())
	}
	tri, err := triage.New(triage.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, LLM: llm, Kit: kit, Tickets: sup})
	if err != nil {
		panic("ai: triage: " + err.Error())
	}
	rx, err := riskexplain.New(riskexplain.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, LLM: llm, Kit: kit,
		Audit: aud, Facts: riskFacts})
	if err != nil {
		panic("ai: risk summary: " + err.Error())
	}
	return aiAdmin{catalogue: aicatalogue.Handlers{S: cat}, triage: triage.Handlers{S: tri}, risk: riskexplain.Handlers{S: rx}}
}

// riskFacts is risk.Facts as the summary's facts: the flag's details as A-13 shows them.
func riskFacts(details []byte) []riskexplain.Fact {
	fs := risk.Facts(details)
	out := make([]riskexplain.Fact, 0, len(fs))
	for _, f := range fs {
		out = append(out, riskexplain.Fact{Key: f.Key, Value: f.Value})
	}
	return out
}

func (s aiServer) CreateCatalogueCleanupDraft(ctx context.Context, req aiapi.CreateCatalogueCleanupDraftRequestObject) (aiapi.CreateCatalogueCleanupDraftResponseObject, error) {
	return s.admin.catalogue.CreateCatalogueCleanupDraft(ctx, req)
}

func (s aiServer) GetCatalogueCleanupDraft(ctx context.Context, req aiapi.GetCatalogueCleanupDraftRequestObject) (aiapi.GetCatalogueCleanupDraftResponseObject, error) {
	return s.admin.catalogue.GetCatalogueCleanupDraft(ctx, req)
}

func (s aiServer) ConfirmCatalogueCleanupDraft(ctx context.Context, req aiapi.ConfirmCatalogueCleanupDraftRequestObject) (aiapi.ConfirmCatalogueCleanupDraftResponseObject, error) {
	return s.admin.catalogue.ConfirmCatalogueCleanupDraft(ctx, req)
}

func (s aiServer) CreateTicketTriageDraft(ctx context.Context, req aiapi.CreateTicketTriageDraftRequestObject) (aiapi.CreateTicketTriageDraftResponseObject, error) {
	return s.admin.triage.CreateTicketTriageDraft(ctx, req)
}

func (s aiServer) GetTicketTriageDraft(ctx context.Context, req aiapi.GetTicketTriageDraftRequestObject) (aiapi.GetTicketTriageDraftResponseObject, error) {
	return s.admin.triage.GetTicketTriageDraft(ctx, req)
}

func (s aiServer) ConfirmTicketTriageDraft(ctx context.Context, req aiapi.ConfirmTicketTriageDraftRequestObject) (aiapi.ConfirmTicketTriageDraftResponseObject, error) {
	return s.admin.triage.ConfirmTicketTriageDraft(ctx, req)
}

func (s aiServer) CreateRiskSummaryDraft(ctx context.Context, req aiapi.CreateRiskSummaryDraftRequestObject) (aiapi.CreateRiskSummaryDraftResponseObject, error) {
	return s.admin.risk.CreateRiskSummaryDraft(ctx, req)
}

func (s aiServer) GetRiskSummaryDraft(ctx context.Context, req aiapi.GetRiskSummaryDraftRequestObject) (aiapi.GetRiskSummaryDraftResponseObject, error) {
	return s.admin.risk.GetRiskSummaryDraft(ctx, req)
}

func (s aiServer) ConfirmRiskSummaryDraft(ctx context.Context, req aiapi.ConfirmRiskSummaryDraftRequestObject) (aiapi.ConfirmRiskSummaryDraftResponseObject, error) {
	return s.admin.risk.ConfirmRiskSummaryDraft(ctx, req)
}

package guard

import "github.com/todayz/backend/internal/platform/settings"

// Provider is what a feature calls.
type Provider int

// Providers.
const (
	LLM Provider = 1 << iota
	STT
)

// Spec is one feature's switches (11 §1 rule 2).
type Spec struct {
	// Flags must all be on.
	Flags []settings.Key
	// Needs are the providers whose key must be set (or mocked in local and test).
	Needs Provider
}

// Features maps each feature name (client.Features) to its flag and providers. Voice stock
// update also needs the business-wide voice switch; voice search needs only the speech key
// (D-101). ai_selftest is the eval suite and is never served.
var Features = map[string]Spec{
	"voice_stock":       {Flags: []settings.Key{settings.AiVoiceStockEnabled, settings.FeatureVoiceEnabled}, Needs: LLM | STT},
	"supplier_bill":     {Flags: []settings.Key{settings.AiBillscanEnabled}, Needs: LLM},
	"shelf_photo":       {Flags: []settings.Key{settings.AiShelfscanEnabled}, Needs: LLM},
	"insight_text":      {Flags: []settings.Key{settings.AiInsighttextEnabled}, Needs: LLM},
	"coach_card":        {Flags: []settings.Key{settings.AiCoachEnabled}, Needs: LLM},
	"review_reply":      {Flags: []settings.Key{settings.AiReplydraftEnabled}, Needs: LLM},
	"banner_copy":       {Flags: []settings.Key{settings.AiBannercopyEnabled}, Needs: LLM},
	"image_check":       {Flags: []settings.Key{settings.AiImagecheckEnabled}, Needs: LLM},
	"list_parse":        {Flags: []settings.Key{settings.AiListparseEnabled}, Needs: LLM},
	"search_assist":     {Flags: []settings.Key{settings.AiQuerynormEnabled}, Needs: LLM},
	"voice_search":      {Flags: []settings.Key{settings.AiVoicesearchEnabled}, Needs: STT},
	"catalogue_cleanup": {Flags: []settings.Key{settings.AiCatalogueEnabled}, Needs: LLM},
	"ticket_triage":     {Flags: []settings.Key{settings.AiTriageEnabled}, Needs: LLM},
	"risk_summary":      {Flags: []settings.Key{settings.AiRiskexplainEnabled}, Needs: LLM},
}

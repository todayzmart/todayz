// Package riskexplain drafts a short summary of a risk flag's evidence for A-13 (AI-A3,
// W3-13, D-233): what the rule counted, in plain words, for the reviewer who decides.
//
// Rules this package holds:
//   - The summary is a draft (11 §1 rule 1). Confirm attaches it to the flag as an audit
//     entry (risk.flag_summary); it never clears, confirms or changes the flag, and the
//     flag's own tables (W3-02) are not touched.
//   - Every number in the summary is one of the flag's own: its facts, score and the
//     subject's other flags by status (D-230). A draft with any other number fails as
//     grounding; an edited summary with one is 422.
//   - The model sees the rule, the subject's type, the score, the status, the facts
//     (scrubbed, ids left out) and the history counts; never a name, phone or id.
//   - The common 404 while ai_riskexplain_enabled is off or the key is missing; the route
//     needs admin.risk.view (A-13's own policy), and a draft is its creator's only.
package riskexplain

import (
	"errors"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/settings"
)

// Feature is the drafts feature name.
const Feature = "risk_summary"

// MaxSummary bounds the summary (RiskSummaryLine.text maxLength).
const MaxSummary = 600

// Fact is one number or code the rule recorded (the risk domain's Fact).
type Fact struct{ Key, Value string }

// Deps are the service's dependencies.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	LLM      client.LLM
	Kit      retail.Kit
	Audit    *audit.Writer
	// Facts flattens a flag's details the way A-13 shows them (risk.Facts).
	Facts func(details []byte) []Fact
}

// Service is the risk summary draft.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil || d.Audit == nil || d.Facts == nil {
		return nil, errors.New("riskexplain: a clock, an audit writer and the facts reader are required")
	}
	d.Kit.Clock, d.Kit.DB = d.Clock, d.DB
	return &Service{d: d}, nil
}

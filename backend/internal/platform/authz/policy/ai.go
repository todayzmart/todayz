package policy

import "github.com/todayz/backend/internal/platform/authz"

// Consumer AI actions (W3-12, 11 §1 AI-C1, AI-C2, AI-C5). The shop's AI routes are under
// the domain whose data they draft (review.reply, banner.manage, …); these are the
// signed-in customer's own helpers, each switched off by its flag (the common 404).
const (
	// KindCustomer: the signed-in customer (/v1/me), on their own drafts only.
	MeAiUse = "me.ai.use"
)

func init() {
	Register(authz.Table{Domain: "ai", Actions: []authz.Action{
		{Name: MeAiUse, Kind: authz.KindCustomer, Doc: "GET /v1/me/ai/features; list parse drafts (C-22), voice search and search assist (C-05): own drafts only"},
	}})
}

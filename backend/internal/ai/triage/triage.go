// Package triage drafts an A-14 ticket's triage (AI-A2, W3-13, D-233): a category, a
// priority and a guess at the order it is about; a summary of a long thread; and a reply
// drafted from the order's timeline. The agent edits and sends.
//
// Rules this package holds:
//   - Everything is a draft (11 §1 rule 1). Confirm applies the classify line through the
//     support domain's own triage write (closed tickets are final, the order must be the
//     opener's, a priority change moves the SLA, audited) and sends the reply line as the
//     agent's A-14 reply. The summary line is feedback only: nothing is stored from it.
//   - The model sees the ticket's subject, its replies (never an internal note), its opener's
//     kind and the opener's recent orders by reference (o1…): number, status, total, date and
//     timeline. Never a name, phone, address, pickup code or id; every free text is scrubbed
//     first, and nothing the opener wrote is logged or stored by this package.
//   - The order guess is one of the opener's orders the model was shown, else none. Every
//     number in the reason, summary and reply is one the model was given (D-230).
//   - The common 404 while ai_triage_enabled is off or the key is missing; the route needs
//     admin.support.tickets (A-14's own policy), and a draft is its creator's only.
package triage

import (
	"context"
	"errors"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// Feature is the drafts feature name.
const Feature = "ticket_triage"

// The bounds of the drafted lines (TicketTriageClassify.reason, TicketTriageText.text) and of
// an agent's edited reply (TicketTriageLineValue.text).
const (
	MaxReason   = 300
	MaxText     = 1000
	MaxReplyLen = 4000
)

// SummaryMinReplies is the shortest thread that gets a summary line.
// TODO(lead): 11 §4 says "summarise long threads" without a number; 4 replies is the
// conservative reading (a short thread is read faster than a summary), not a product setting.
const SummaryMinReplies = 4

// TimelineOrders is how many of the opener's latest orders carry their timeline (the linked
// order always does), so the prompt stays small.
const TimelineOrders = 3

// Tickets is the support domain's writes the confirm applies (support.Service).
type Tickets interface {
	AssistTriage(ctx context.Context, actor, ticket ids.ID, category, priority string, orderID *ids.ID) error
	AssistReply(ctx context.Context, actor, ticket ids.ID, text string) error
}

// Deps are the service's dependencies.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	LLM      client.LLM
	Kit      retail.Kit
	Tickets  Tickets
}

// Service is the ticket triage draft.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil || d.Tickets == nil {
		return nil, errors.New("triage: a clock and the support writer are required")
	}
	d.Kit.Clock, d.Kit.DB = d.Clock, d.DB
	return &Service{d: d}, nil
}

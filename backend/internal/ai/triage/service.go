package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/gen/sqlc/aiadmindb"
	"github.com/todayz/backend/internal/ai/assist"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// inputMeta is what the draft row keeps about its ticket: counts only, never its words.
type inputMeta struct {
	Replies int `json:"replies"`
	Orders  int `json:"orders"`
}

// Create checks the ticket and drafts its triage in the background. A closed ticket is final
// in A-14, so it gets no draft.
func (s *Service) Create(ctx context.Context, p authn.Principal, ticketID ids.ID) (ids.ID, error) {
	if err := s.d.Kit.Guard.Require(ctx, Feature); err != nil {
		return ids.Zero, err
	}
	t, err := s.ticket(ctx, ticketID)
	if db.IsNoRows(err) {
		return ids.Zero, retail.FieldErr("body.ticketId", "no_ticket")
	}
	if err != nil {
		return ids.Zero, err
	}
	if t.Status == "closed" {
		return ids.Zero, retail.FieldErr("body.ticketId", "closed")
	}
	return s.d.Kit.Start(ctx, retail.New{
		Feature: Feature, Store: ids.Zero, Actor: p.UserID, InputRef: ticketID.String(),
		PromptVersion: prompts.Version(Feature), Meta: inputMeta{Replies: len(t.Thread), Orders: len(t.Orders)},
	}, func(ctx context.Context, id ids.ID) { s.process(ctx, id, ticketID) })
}

// ticket reads the model's view of one ticket: its replies (never a note) and the opener's
// recent orders, the linked one and the latest few with their timelines.
func (s *Service) ticket(ctx context.Context, ticketID ids.ID) (Ticket, error) {
	q := aiadmindb.New(s.d.DB.Q(ctx))
	row, err := q.TicketForTriage(ctx, ids.UUID(ticketID))
	if err != nil {
		return Ticket{}, err
	}
	t := Ticket{ID: ticketID, Number: deref(row.DisplayNumber), Subject: row.Subject, Opener: row.OpenerType,
		Category: row.Category, Priority: row.Priority, Status: row.Status}
	replies, err := q.TicketReplies(ctx, row.ID)
	if err != nil {
		return Ticket{}, err
	}
	for _, r := range replies {
		t.Thread = append(t.Thread, Message{From: FromOpener(r.AuthorType), Text: r.Body})
	}
	var by aiadmindb.OpenerOrdersParams
	if row.OpenerType == "business" {
		by.BusinessID = row.BusinessID
	} else {
		by.CustomerID = row.OpenerUserID
	}
	orders, err := q.OpenerOrders(ctx, by)
	if err != nil {
		return Ticket{}, err
	}
	linked, hasLinked := ids.FromUUID(row.OrderID)
	found := false
	for i, o := range orders {
		id, _ := ids.FromUUID(o.ID)
		total := o.TotalPaise
		ord := Order{ID: id, Ref: fmt.Sprintf("o%d", i+1), Number: deref(o.DisplayNumber), Status: o.Status,
			Fulfilment: o.Fulfilment, TotalPaise: &total, PlacedOn: On(o.PlacedAt), Linked: hasLinked && id == linked}
		found = found || ord.Linked
		t.Orders = append(t.Orders, ord)
	}
	if hasLinked && !found {
		// The linked order is older than the opener's latest ten: shown by number and timeline.
		t.Orders = append(t.Orders, Order{ID: linked, Ref: fmt.Sprintf("o%d", len(t.Orders)+1), Number: deref(row.OrderNumber), Linked: true})
	}
	for i := range t.Orders {
		o := &t.Orders[i]
		if !o.Linked && i >= TimelineOrders {
			continue
		}
		evs, err := q.TriageOrderEvents(ctx, ids.UUID(o.ID))
		if err != nil {
			return Ticket{}, err
		}
		for _, e := range evs {
			o.Timeline = append(o.Timeline, Event{From: e.FromStatus, To: e.ToStatus, By: e.ActorKind, Reason: e.Reason, At: At(e.CreatedAt)})
		}
		if o.Status == "" && len(evs) > 0 {
			o.Status = evs[len(evs)-1].ToStatus
		}
	}
	return t.Finish(), nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Service) process(ctx context.Context, id, ticketID ids.ID) {
	log := telemetry.Logger(ctx)
	fail := func(what string, err error) {
		log.ErrorContext(ctx, "triage: "+what, slog.String("draft_id", id.String()), slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, client.Response{})
	}
	model, err := s.d.Settings.String(ctx, settings.AiModelFast)
	if err != nil {
		fail("model setting", err)
		return
	}
	t, err := s.ticket(ctx, ticketID)
	if err != nil {
		fail("ticket", err)
		return
	}
	r, err := Write(ctx, s.d.LLM, model, id.String(), t)
	s.d.Kit.Settle(ctx, Feature, r.Resp)
	switch {
	case err != nil:
		log.ErrorContext(ctx, "triage: model call", slog.String("draft_id", id.String()), slog.Any("err", err))
		s.d.Kit.Fail(ctx, Feature, id, client.OutcomeError, r.Resp)
	case r.Reject != "":
		s.d.Kit.Fail(ctx, Feature, id, r.Reject, r.Resp)
	default:
		s.d.Kit.Ready(ctx, Feature, id, r.Output, r.Resp)
	}
}

// Get answers one of the caller's own drafts.
func (s *Service) Get(ctx context.Context, p authn.Principal, id ids.ID) (aiapi.TicketTriageDraft, error) {
	d, err := assist.Load(ctx, s.d.Kit, Feature, p.UserID, id)
	if err != nil {
		return aiapi.TicketTriageDraft{}, err
	}
	v := aiapi.TicketTriageDraft{
		DraftId: d.ID.String(), Feature: aiapi.TicketTriageDraftFeature(Feature), Status: aiapi.AiDraftStatus(d.Status),
		PromptVersion: d.PromptVersion, CreatedAt: d.CreatedAt.UTC(), Failure: retail.Failure(d), TicketId: d.InputRef,
		Outcomes: retail.Outcomes(d.Lines),
	}
	if len(d.Output) == 0 {
		return v, nil
	}
	var out Output
	if err := d.Decode(&out); err != nil {
		return aiapi.TicketTriageDraft{}, err
	}
	for _, l := range out.Lines {
		switch l.Kind {
		case KindClassify:
			v.Classify = &aiapi.TicketTriageClassify{
				LineId: l.LineID, Category: aiapi.AdminAiTicketCategory(l.Category), Priority: aiapi.AdminAiTicketPriority(l.Priority),
				OrderId: l.OrderID, OrderNumber: l.OrderNumber, Reason: l.Reason,
			}
		case KindSummary:
			v.Summary = &aiapi.TicketTriageText{LineId: l.LineID, Text: l.Text}
		case KindReply:
			v.Reply = &aiapi.TicketTriageText{LineId: l.LineID, Text: l.Text}
		}
	}
	return v, nil
}

// Confirm applies the accepted or edited lines: the classify line through the support
// domain's triage write, the reply as the agent's A-14 reply. The summary line is recorded as
// feedback only.
func (s *Service) Confirm(ctx context.Context, p authn.Principal, draftID ids.ID, in []aiapi.TicketTriageDecision) (drafts.Result, error) {
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
		Feature: Feature, DraftID: draftID, By: p.UserID, Decisions: ds, Check: assist.Own(p.UserID),
		Apply: func(ctx context.Context, d drafts.Draft, ds []drafts.Decision) ([]drafts.LineResult, error) {
			return s.apply(ctx, p, d, ds, at)
		},
	})
}

func (s *Service) apply(ctx context.Context, p authn.Principal, d drafts.Draft, ds []drafts.Decision, at map[string]int) ([]drafts.LineResult, error) {
	var out Output
	if err := d.Decode(&out); err != nil {
		return nil, retail.NotFound()
	}
	ticketID, err := ids.Parse(d.InputRef)
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
	// Validate every decision before writing anything, so a 422 leaves the ticket as it was.
	type step struct {
		dc       drafts.Decision
		line     Line
		category string
		priority string
		order    *ids.ID
		text     string
	}
	steps := make([]step, 0, len(ds))
	for _, dc := range ds {
		l, ok := byID[dc.LineID]
		if !ok {
			return nil, retail.NotFound()
		}
		st := step{dc: dc, line: l}
		var v aiapi.TicketTriageLineValue
		if dc.Outcome == drafts.Edited {
			if err := json.Unmarshal(dc.Value, &v); err != nil {
				return nil, retail.FieldErr(path(dc.LineID, "value"), "invalid")
			}
		}
		switch l.Kind {
		case KindClassify:
			st.category, st.priority = l.Category, l.Priority
			if l.OrderID != nil {
				id, err := ids.Parse(*l.OrderID)
				if err != nil {
					return nil, retail.NotFound()
				}
				st.order = &id
			}
			if dc.Outcome == drafts.Edited {
				if v.Text != nil {
					return nil, retail.FieldErr(path(dc.LineID, "value.text"), "not_for_classify")
				}
				if v.Category != nil {
					st.category = string(*v.Category)
				}
				if v.Priority != nil {
					st.priority = string(*v.Priority)
				}
				if v.OrderId != nil {
					id, err := ids.Parse(*v.OrderId)
					if err != nil {
						return nil, retail.FieldErr(path(dc.LineID, "value.orderId"), "invalid")
					}
					st.order = &id
				}
			}
			if !oneOf(st.category, Categories) {
				return nil, retail.FieldErr(path(dc.LineID, "value.category"), "invalid")
			}
			if !oneOf(st.priority, Priorities) {
				return nil, retail.FieldErr(path(dc.LineID, "value.priority"), "invalid")
			}
		case KindSummary:
			if dc.Outcome == drafts.Edited {
				return nil, retail.FieldErr(path(dc.LineID, "value"), "summary_takes_no_value")
			}
		case KindReply:
			st.text = l.Text
			if dc.Outcome == drafts.Edited {
				if v.Category != nil || v.Priority != nil || v.OrderId != nil || v.Text == nil {
					return nil, retail.FieldErr(path(dc.LineID, "value.text"), "required")
				}
				// The agent's own words, sent as their A-14 reply: A-14's rules, no grounding.
				st.text = strings.TrimSpace(*v.Text)
			}
			if st.text == "" || utf8.RuneCountInString(st.text) > MaxReplyLen {
				return nil, retail.FieldErr(path(dc.LineID, "value.text"), "length")
			}
		default:
			return nil, retail.NotFound()
		}
		steps = append(steps, st)
	}
	res := make([]drafts.LineResult, 0, len(steps))
	for _, st := range steps {
		switch st.line.Kind {
		case KindClassify:
			if err := s.d.Tickets.AssistTriage(ctx, p.UserID, ticketID, st.category, st.priority, st.order); err != nil {
				return nil, err
			}
		case KindReply:
			if err := s.d.Tickets.AssistReply(ctx, p.UserID, ticketID, st.text); err != nil {
				return nil, err
			}
		}
		res = append(res, drafts.LineResult{LineID: st.dc.LineID, Outcome: st.dc.Outcome})
	}
	return res, nil
}

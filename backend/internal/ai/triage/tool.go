package triage

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/ai/schema"
	"github.com/todayz/backend/internal/ai/scrub"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

//go:embed prompt.md
var promptFS embed.FS

func init() { prompts.MustRegister(Feature, promptFS) }

// Categories and Priorities are A-14's (AdminAiTicketCategory, AdminAiTicketPriority).
var (
	Categories = []string{"order", "credits", "payouts", "catalogue", "account", "verification", "app", "other"}
	Priorities = []string{"normal", "high", "urgent"}
)

// MaxOrders is how many of the opener's orders the model is shown (OpenerOrders' limit), and
// so the refs it may answer.
const MaxOrders = 10

// NoOrder is the orderRef for "not sure which order".
const NoOrder = "none"

func refs() []string {
	out := []string{NoOrder}
	for i := 1; i <= MaxOrders+1; i++ { // +1: a linked order older than the latest ten
		out = append(out, fmt.Sprintf("o%d", i))
	}
	return out
}

// Tool is the strict tool the model answers with.
var Tool = client.Tool{
	Name:        "triage_ticket",
	Description: "Record the ticket's triage: category, priority, order, reason, summary and a reply draft.",
	Schema: schema.Object("", map[string]schema.S{
		"category": schema.Enum("The ticket's category", Categories...),
		"priority": schema.Enum("The ticket's priority", Priorities...),
		"orderRef": schema.Enum("The ref of the order the ticket is about, or none", refs()...),
		"reason":   schema.String("Why, in one sentence, for the agent", MaxReason),
		"summary":  schema.Nullable(schema.String("The thread in two to four sentences, or null for a short thread", MaxText)),
		"reply":    schema.String("The reply draft, in the opener's language", MaxText),
	}),
}

// ToolOutput is the tool input.
type ToolOutput struct {
	Category string  `json:"category"`
	Priority string  `json:"priority"`
	OrderRef string  `json:"orderRef"`
	Reason   string  `json:"reason"`
	Summary  *string `json:"summary"`
	Reply    string  `json:"reply"`
}

// Validate is the bounds the strict schema only describes.
func (o *ToolOutput) Validate() error {
	if !oneOf(o.Category, Categories) || !oneOf(o.Priority, Priorities) {
		return fmt.Errorf("category or priority")
	}
	if !between(o.Reason, MaxReason) || !between(o.Reply, MaxText) {
		return fmt.Errorf("reason or reply length")
	}
	if o.Summary != nil && strings.TrimSpace(*o.Summary) != "" && utf8.RuneCountInString(*o.Summary) > MaxText {
		return fmt.Errorf("summary length")
	}
	return nil
}

func oneOf(s string, vals []string) bool {
	for _, v := range vals {
		if s == v {
			return true
		}
	}
	return false
}

func between(s string, maxLen int) bool {
	s = strings.TrimSpace(s)
	return s != "" && utf8.RuneCountInString(s) <= maxLen
}

// Message is one reply of the thread, scrubbed.
type Message struct {
	From string `json:"from"` // opener or support
	Text string `json:"text"`
}

// Event is one step of an order's timeline.
type Event struct {
	From   *string `json:"from"`
	To     string  `json:"to"`
	By     string  `json:"by"`
	Reason *string `json:"reason,omitempty"`
	At     string  `json:"at"` // IST, minutes
}

// Order is one of the opener's orders, by reference.
type Order struct {
	ID         ids.ID  `json:"-"`
	Ref        string  `json:"ref"`
	Number     string  `json:"number"`
	Status     string  `json:"status"`
	Fulfilment string  `json:"fulfilment,omitempty"`
	TotalPaise *int64  `json:"totalPaise,omitempty"`
	PlacedOn   string  `json:"placedOn,omitempty"` // IST date
	Linked     bool    `json:"linkedToTicket"`
	Timeline   []Event `json:"timeline,omitempty"`
}

// Ticket is what the model is shown of one ticket.
type Ticket struct {
	ID       ids.ID    `json:"-"`
	Number   string    `json:"number"`
	Subject  string    `json:"subject"`
	Opener   string    `json:"opener"` // customer or business
	Category string    `json:"category"`
	Priority string    `json:"priority"`
	Status   string    `json:"-"`
	Thread   []Message `json:"replies"`
	Orders   []Order   `json:"orders"`
}

// keep are the strings scrub must not cut: the order numbers ("TZ-123456" reads as a PIN code).
func (t Ticket) keep() []string {
	out := make([]string, 0, len(t.Orders))
	for _, o := range t.Orders {
		if o.Number != "" {
			out = append(out, o.Number)
		}
	}
	return append(out, t.Number)
}

// Scrub is scrub.Text that leaves the ticket's own order and ticket numbers whole.
func Scrub(s string, keep []string) string {
	holders := map[string]string{}
	for i, k := range keep {
		if k == "" || !strings.Contains(s, k) {
			continue
		}
		h := fmt.Sprintf("\x00%d\x00", i)
		holders[h] = k
		s = strings.ReplaceAll(s, k, h)
	}
	s = scrub.Text(s, scrub.Options{}).Text
	for h, k := range holders {
		s = strings.ReplaceAll(s, h, k)
	}
	return strings.TrimSpace(s)
}

// FromOpener maps a reply's author type to who wrote it, from the agent's side.
func FromOpener(authorType string) string {
	if authorType == "admin" {
		return "support"
	}
	return "opener"
}

// At is a timeline time as the model reads it (IST, to the minute).
func At(t time.Time) string { return t.In(clock.IST).Format("2006-01-02 15:04") }

// On is an order's date as the model reads it (IST).
func On(t time.Time) string { return t.In(clock.IST).Format("2006-01-02") }

// Finish scrubs every free text of t (subject, replies, timeline reasons) once its orders are
// known, so the order numbers survive.
func (t Ticket) Finish() Ticket {
	keep := t.keep()
	t.Subject = Scrub(t.Subject, keep)
	for i := range t.Thread {
		t.Thread[i].Text = Scrub(t.Thread[i].Text, keep)
	}
	for i := range t.Orders {
		for j, e := range t.Orders[i].Timeline {
			if e.Reason != nil {
				r := Scrub(*e.Reason, keep)
				t.Orders[i].Timeline[j].Reason = &r
			}
		}
	}
	if t.Thread == nil {
		t.Thread = []Message{}
	}
	if t.Orders == nil {
		t.Orders = []Order{}
	}
	return t
}

// JSON is the facts JSON the model is given and the text lines are grounded against.
func (t Ticket) JSON() []byte {
	b, _ := json.Marshal(t) // strings, ints and slices of them always marshal
	return b
}

// Grounded reports whether text holds only numbers the model was given and nothing scrub
// would cut (the ticket's own order and ticket numbers aside).
func (t Ticket) Grounded(text string) bool {
	return len(schema.UngroundedFacts(text, t.JSON())) == 0 && Scrub(text, t.keep()) == strings.TrimSpace(text)
}

// Order finds an order by its ref; ok is false for none or a ref the model was not shown.
func (t Ticket) Order(ref string) (Order, bool) {
	for _, o := range t.Orders {
		if o.Ref == ref {
			return o, true
		}
	}
	return Order{}, false
}

// OrderByID finds an order the model was shown by its id.
func (t Ticket) OrderByID(id ids.ID) (Order, bool) {
	for _, o := range t.Orders {
		if o.ID == id {
			return o, true
		}
	}
	return Order{}, false
}

// The line kinds.
const (
	KindClassify = "classify"
	KindSummary  = "summary"
	KindReply    = "reply"
)

// Line is one stored draft line: l1 classify, l2 summary (a long thread only), l3 reply.
type Line struct {
	LineID      string  `json:"lineId"`
	Kind        string  `json:"kind"`
	Category    string  `json:"category,omitempty"`
	Priority    string  `json:"priority,omitempty"`
	OrderID     *string `json:"orderId,omitempty"`
	OrderNumber *string `json:"orderNumber,omitempty"`
	Reason      string  `json:"reason,omitempty"`
	Text        string  `json:"text,omitempty"`
}

// Output is a ready draft's output.
type Output struct {
	Lines []Line `json:"lines"`
}

// Line returns the line of a kind.
func (o Output) Line(kind string) (Line, bool) {
	for _, l := range o.Lines {
		if l.Kind == kind {
			return l, true
		}
	}
	return Line{}, false
}

// Draft is what the model made of one ticket.
type Draft struct {
	Output Output
	Resp   client.Response
	Reject string
}

// Write runs the model on one ticket. An order ref it was not shown fails as schema; a reason,
// summary or reply with a number it was not given, or with a phone or code, as grounding.
func Write(ctx context.Context, llm client.LLM, model, ref string, t Ticket) (Draft, error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Draft{}, err
	}
	var out ToolOutput
	resp, reject, err := retail.Call(ctx, llm, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys, Tool: &Tool, MaxTokens: 1024,
		Messages: []client.Message{{Role: client.RoleUser, Content: []client.Block{
			{Text: "The ticket (JSON):\n" + string(t.JSON())},
		}}},
	}, &out)
	if err != nil || reject != "" {
		return Draft{Resp: resp, Reject: reject}, err
	}
	classify := Line{LineID: "l1", Kind: KindClassify, Category: out.Category, Priority: out.Priority, Reason: strings.TrimSpace(out.Reason)}
	if out.OrderRef != NoOrder {
		o, ok := t.Order(out.OrderRef)
		if !ok {
			client.RecordRejected(ctx, Feature, client.RejectSchema)
			return Draft{Resp: resp, Reject: client.RejectSchema}, nil
		}
		id, num := o.ID.String(), o.Number
		classify.OrderID, classify.OrderNumber = &id, &num
	}
	lines := []Line{classify}
	texts := []string{classify.Reason}
	if out.Summary != nil && len(t.Thread) >= SummaryMinReplies {
		if s := strings.TrimSpace(*out.Summary); s != "" {
			lines = append(lines, Line{LineID: "l2", Kind: KindSummary, Text: s})
			texts = append(texts, s)
		}
	}
	reply := strings.TrimSpace(out.Reply)
	lines = append(lines, Line{LineID: "l3", Kind: KindReply, Text: reply})
	texts = append(texts, reply)
	for _, s := range texts {
		if !t.Grounded(s) {
			client.RecordRejected(ctx, Feature, client.RejectGrounding)
			return Draft{Resp: resp, Reject: client.RejectGrounding}, nil
		}
	}
	return Draft{Output: Output{Lines: lines}, Resp: resp}, nil
}

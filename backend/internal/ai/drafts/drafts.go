// Package drafts keeps AI drafts (11 §1 rules 1, 9, 10; D-06, D-46): every feature that
// proposes a change stores the proposal here, and nothing changes stock, money, orders,
// reviews or KYC until a person confirms it.
//
// Life of a draft: Create (processing) → MarkReady with the model's checked output, or
// MarkFailed → Confirm, which runs the feature's Apply inside the same transaction and closes
// the draft confirmed, or discarded when every line was discarded. Confirm is idempotent: a
// repeat returns the outcomes stored the first time and applies nothing again.
//
// A feature whose lines need different roles may confirm in parts (Confirmation.KeepOpen):
// while any line comes back not_permitted the draft stays ready, the applied and discarded
// lines are stored as settled, and a later confirm by someone allowed decides only the lines
// still open. A settled line is never applied again.
//
// Output convention: a draft's output is a JSON object. A feature with several lines puts
// them in "lines", each with a "lineId" (l1…l999, AiDraftLineId); an output without "lines"
// is one line, l1. Confirm refuses a decision for a line the draft does not have, and a line
// with no decision is recorded as discarded.
//
// Apply writes through the owning domain with the confirmer's permissions, re-validating
// every line against the current state (stock, prices, the confirmer's role); a line it
// refuses is not_permitted, never an error for the whole draft (D-06, 01 §17).
package drafts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/aidb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Statuses (ai_drafts.status).
const (
	StatusProcessing = "processing"
	StatusReady      = "ready"
	StatusConfirmed  = "confirmed"
	StatusDiscarded  = "discarded"
	StatusFailed     = "failed"
)

// Outcome is what happened to one line (AiLineOutcome).
type Outcome string

// Outcomes. A person chooses accepted, edited or discarded; Apply may answer not_permitted.
const (
	Accepted     Outcome = "accepted"
	Edited       Outcome = "edited"
	Discarded    Outcome = "discarded"
	NotPermitted Outcome = "not_permitted"
)

// MaxLines is the most lines one draft may carry (AiConfirmResult.lines maxItems).
const MaxLines = 200

func init() {
	telemetry.RegisterLabelValues("outcome", string(Accepted), string(Edited), string(Discarded), string(NotPermitted))
}

// Decision is the person's choice for one line. Value is the edited line (Edited only), in
// the feature's own line shape.
type Decision struct {
	LineID  string          `json:"lineId"`
	Outcome Outcome         `json:"outcome"`
	Value   json.RawMessage `json:"value,omitempty"`
}

// LineResult is one line's final outcome.
type LineResult struct {
	LineID  string  `json:"lineId"`
	Outcome Outcome `json:"outcome"`
	// Reason is why a not_permitted line was refused (the feature's code); empty otherwise.
	Reason string `json:"reason,omitempty"`
}

// Draft is one ai_drafts row.
type Draft struct {
	ID            ids.ID
	Feature       string
	Store         ids.ID // ids.Zero for customer and admin features
	Actor         ids.ID
	InputRef      string
	InputMeta     json.RawMessage
	PromptVersion string
	Model         string
	Status        string
	Output        json.RawMessage // nil while processing and when failed
	Lines         []LineResult    // set once confirmed or discarded; on a ready draft, the lines settled by a partial confirm (the open ones not_permitted)
	Failure       string
	ConfirmedBy   ids.ID
	ConfirmedAt   *time.Time
	CostPaise     int64
	CreatedAt     time.Time
}

// Decode unmarshals the output into out.
func (d Draft) Decode(out any) error {
	if d.Output == nil {
		return errors.New("drafts: the draft has no output")
	}
	return json.Unmarshal(d.Output, out)
}

// Service reads and writes drafts.
type Service struct {
	db    *db.DB
	clock clock.Clock
}

// New returns the service.
func New(d *db.DB, c clock.Clock) *Service { return &Service{db: d, clock: c} }

// NewDraft is what Create stores.
type NewDraft struct {
	Feature       string
	Store         ids.ID
	Actor         ids.ID
	InputRef      string // a pointer to the input (an upload key), never the input itself
	InputMeta     any    // small, PII-free facts about the input (language, duration, size)
	PromptVersion string // prompts.Version(feature)
}

// Create stores a processing draft and returns its id.
func (s *Service) Create(ctx context.Context, n NewDraft) (ids.ID, error) {
	if !client.KnownFeature(n.Feature) {
		return ids.Zero, fmt.Errorf("drafts: unknown feature %q", n.Feature)
	}
	if n.Actor == ids.Zero || n.PromptVersion == "" {
		return ids.Zero, errors.New("drafts: actor and prompt version are required")
	}
	meta := json.RawMessage(`{}`)
	if n.InputMeta != nil {
		b, err := json.Marshal(n.InputMeta)
		if err != nil {
			return ids.Zero, fmt.Errorf("drafts: input meta: %w", err)
		}
		meta = b
	}
	now := s.clock.Now()
	id := ids.New(now)
	err := aidb.New(s.db.Q(ctx)).InsertDraft(ctx, aidb.InsertDraftParams{
		ID: ids.UUID(id), Feature: n.Feature, StoreID: optUUID(n.Store), ActorUserID: ids.UUID(n.Actor),
		InputRef: optString(n.InputRef), InputMeta: meta, PromptVersion: n.PromptVersion, Now: now,
	})
	if err != nil {
		return ids.Zero, fmt.Errorf("drafts: create: %w", err)
	}
	return id, nil
}

// Get reads a draft of feature. Another feature's draft, or none, is the common 404; the
// caller then checks the draft belongs to the caller's store or user.
func (s *Service) Get(ctx context.Context, feature string, id ids.ID) (Draft, error) {
	row, err := aidb.New(s.db.Q(ctx)).GetDraft(ctx, ids.UUID(id))
	if db.IsNoRows(err) || (err == nil && row.Feature != feature) {
		return Draft{}, problem.New(problems.NotFound, "")
	}
	if err != nil {
		return Draft{}, fmt.Errorf("drafts: get: %w", err)
	}
	return fromRow(row)
}

// MarkReady stores the checked output of a processing draft. It reports false when the draft
// was not processing (a retried job), changing nothing.
func (s *Service) MarkReady(ctx context.Context, id ids.ID, output any, r client.Response) (bool, error) {
	b, err := json.Marshal(output)
	if err != nil {
		return false, fmt.Errorf("drafts: output: %w", err)
	}
	if len(b) == 0 || b[0] != '{' {
		return false, errors.New("drafts: output must be a JSON object")
	}
	if _, err := lineIDs(b); err != nil {
		return false, err
	}
	n, err := aidb.New(s.db.Q(ctx)).MarkDraftReady(ctx, aidb.MarkDraftReadyParams{
		Output: b, Model: r.Model, InputTokens: clamp32(r.Usage.InputTokens), OutputTokens: clamp32(r.Usage.OutputTokens),
		CostPaise: r.CostPaise, LatencyMs: clamp32(int(r.Latency.Milliseconds())), Now: s.clock.Now(), ID: ids.UUID(id),
	})
	if err != nil {
		return false, fmt.Errorf("drafts: mark ready: %w", err)
	}
	return n == 1, nil
}

// MarkFailed closes a processing draft as failed. reason is a short code (client.Outcome of
// the error, or schema / grounding), never model text or user input.
func (s *Service) MarkFailed(ctx context.Context, id ids.ID, reason string, r client.Response) (bool, error) {
	if reason == "" {
		reason = client.OutcomeError
	}
	n, err := aidb.New(s.db.Q(ctx)).MarkDraftFailed(ctx, aidb.MarkDraftFailedParams{
		Failure: &reason, Model: r.Model, InputTokens: clamp32(r.Usage.InputTokens), OutputTokens: clamp32(r.Usage.OutputTokens),
		CostPaise: r.CostPaise, LatencyMs: clamp32(int(r.Latency.Milliseconds())), Now: s.clock.Now(), ID: ids.UUID(id),
	})
	if err != nil {
		return false, fmt.Errorf("drafts: mark failed: %w", err)
	}
	return n == 1, nil
}

// ApplyFunc writes the accepted and edited lines through the owning domain, inside Confirm's
// transaction, and returns every decided line's outcome: the person's outcome, or
// not_permitted for a line the confirmer may not make or that no longer fits the current
// state, with a Reason the person reads (W3-11). An error rolls the whole confirm back. It is not called when every line is discarded.
type ApplyFunc func(ctx context.Context, d Draft, decisions []Decision) ([]LineResult, error)

// Confirmation is one confirm request.
type Confirmation struct {
	Feature   string
	DraftID   ids.ID
	By        ids.ID
	Decisions []Decision
	// Check runs on the locked draft before anything else, also on a repeat: the caller's
	// ownership check (the draft's store is the caller's store, or its actor is the caller).
	// An error is returned as it is (the caller answers 404 for another store's draft).
	Check func(Draft) error
	Apply ApplyFunc
	// KeepOpen keeps the draft ready while any line is not_permitted (the package doc). A
	// decision for a line already settled is ignored: the line keeps its first outcome.
	KeepOpen bool
}

// Result is a draft's outcomes after a confirm (AiConfirmResult).
type Result struct {
	DraftID ids.ID
	Status  string // confirmed or discarded; ready after a partial confirm (KeepOpen)
	Lines   []LineResult
}

// Confirm closes a ready draft with the person's decisions (or, with KeepOpen, settles the
// lines it may and leaves the rest open).
func (s *Service) Confirm(ctx context.Context, c Confirmation) (Result, error) {
	if c.Apply == nil || c.By == ids.Zero {
		return Result{}, errors.New("drafts: confirm needs Apply and By")
	}
	var res Result
	var fresh bool
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := aidb.New(s.db.Q(ctx))
		row, err := q.LockDraft(ctx, ids.UUID(c.DraftID))
		if db.IsNoRows(err) || (err == nil && row.Feature != c.Feature) {
			return problem.New(problems.NotFound, "")
		}
		if err != nil {
			return fmt.Errorf("drafts: lock: %w", err)
		}
		d, err := fromRow(row)
		if err != nil {
			return err
		}
		if c.Check != nil {
			if err := c.Check(d); err != nil {
				return err
			}
		}
		switch d.Status {
		case StatusConfirmed, StatusDiscarded:
			res = Result{DraftID: d.ID, Status: d.Status, Lines: d.Lines}
			return nil
		case StatusReady:
		default:
			return problem.New(problems.InvalidTransition, "The draft is "+d.Status+".")
		}
		known, err := lineIDs(d.Output)
		if err != nil {
			return err
		}
		if _, err := checkDecisions(c.Decisions, known); err != nil {
			return err
		}
		// Lines a partial confirm settled keep their outcome; only the open ones are decided.
		settled := map[string]Outcome{}
		if c.KeepOpen {
			for _, l := range d.Lines {
				if l.Outcome != NotPermitted {
					settled[l.LineID] = l.Outcome
				}
			}
		}
		decided := map[string]Decision{}
		var toApply []Decision
		for _, dc := range c.Decisions {
			if _, done := settled[dc.LineID]; done {
				continue
			}
			decided[dc.LineID] = dc
			if dc.Outcome != Discarded {
				toApply = append(toApply, dc)
			}
		}
		if len(decided) == 0 {
			// Every decision was for a settled line (a repeat of the partial confirm).
			res = Result{DraftID: d.ID, Status: d.Status, Lines: d.Lines}
			return nil
		}
		applied := map[string]LineResult{}
		if len(toApply) > 0 {
			out, err := c.Apply(ctx, d, toApply)
			if err != nil {
				return err
			}
			for _, r := range out {
				if r.Outcome != NotPermitted {
					r.Reason = ""
				}
				applied[r.LineID] = r
			}
			for _, dc := range toApply {
				r, ok := applied[dc.LineID]
				if o := r.Outcome; !ok || (o != dc.Outcome && o != NotPermitted) {
					return fmt.Errorf("drafts: %s apply returned no valid outcome for %s", c.Feature, dc.LineID)
				}
			}
			if len(out) != len(toApply) {
				return fmt.Errorf("drafts: %s apply returned %d outcomes for %d lines", c.Feature, len(out), len(toApply))
			}
		}
		status, open := StatusDiscarded, false
		lines := make([]LineResult, 0, len(known))
		for _, id := range known {
			o, done := settled[id]
			var reason string
			if !done {
				o = Discarded
				if dc, ok := decided[id]; ok && dc.Outcome != Discarded {
					o, reason = applied[id].Outcome, applied[id].Reason
				}
			}
			switch o {
			case Accepted, Edited:
				status = StatusConfirmed
			case NotPermitted:
				open = true
			}
			lines = append(lines, LineResult{LineID: id, Outcome: o, Reason: reason})
		}
		b, err := json.Marshal(lines)
		if err != nil {
			return err
		}
		now := s.clock.Now()
		if c.KeepOpen && open {
			if err := q.SettleDraftLines(ctx, aidb.SettleDraftLinesParams{LineOutcomes: b, Now: now, ID: row.ID}); err != nil {
				return fmt.Errorf("drafts: settle: %w", err)
			}
			res = Result{DraftID: d.ID, Status: StatusReady, Lines: lines}
			return nil
		}
		if err := q.CloseDraft(ctx, aidb.CloseDraftParams{Status: status, LineOutcomes: b, ConfirmedBy: ids.UUID(c.By), Now: &now, ID: row.ID}); err != nil {
			return fmt.Errorf("drafts: close: %w", err)
		}
		res, fresh = Result{DraftID: d.ID, Status: status, Lines: lines}, true
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	if fresh {
		recordLines(ctx, c.Feature, res.Lines)
	}
	return res, nil
}

var reLineID = regexp.MustCompile(`^l[0-9]{1,3}$`)

// checkDecisions validates the request against the draft's lines.
func checkDecisions(ds []Decision, known []string) (map[string]Decision, error) {
	if len(ds) == 0 {
		return nil, problem.New(problems.ValidationFailed, "Choose what to do with at least one line.")
	}
	ok := make(map[string]bool, len(known))
	for _, id := range known {
		ok[id] = true
	}
	seen := make(map[string]Decision, len(ds))
	for _, d := range ds {
		if !reLineID.MatchString(d.LineID) || !ok[d.LineID] {
			return nil, problem.New(problems.ValidationFailed, "The draft has no line "+d.LineID+".")
		}
		if _, dup := seen[d.LineID]; dup {
			return nil, problem.New(problems.ValidationFailed, "Line "+d.LineID+" is listed twice.")
		}
		switch d.Outcome {
		case Accepted, Discarded:
			if len(d.Value) > 0 {
				return nil, problem.New(problems.ValidationFailed, "Only an edited line carries a value.")
			}
		case Edited:
			if len(d.Value) == 0 || string(d.Value) == "null" {
				return nil, problem.New(problems.ValidationFailed, "An edited line needs its value.")
			}
		default:
			return nil, problem.New(problems.ValidationFailed, "Unknown outcome for line "+d.LineID+".")
		}
		seen[d.LineID] = d
	}
	return seen, nil
}

// lineIDs lists the output's lines (the package doc's convention).
func lineIDs(output json.RawMessage) ([]string, error) {
	var env struct {
		Lines *[]struct {
			LineID string `json:"lineId"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(output, &env); err != nil {
		return nil, fmt.Errorf("drafts: output: %w", err)
	}
	if env.Lines == nil {
		return []string{"l1"}, nil
	}
	if len(*env.Lines) == 0 || len(*env.Lines) > MaxLines {
		return nil, fmt.Errorf("drafts: output has %d lines (1…%d)", len(*env.Lines), MaxLines)
	}
	out := make([]string, 0, len(*env.Lines))
	seen := map[string]bool{}
	for _, l := range *env.Lines {
		if !reLineID.MatchString(l.LineID) || seen[l.LineID] {
			return nil, fmt.Errorf("drafts: output line id %q missing, malformed or repeated", l.LineID)
		}
		seen[l.LineID] = true
		out = append(out, l.LineID)
	}
	return out, nil
}

func fromRow(r aidb.AiDraft) (Draft, error) {
	d := Draft{
		Feature: r.Feature, InputMeta: r.InputMeta, PromptVersion: r.PromptVersion, Model: r.Model,
		Status: r.Status, ConfirmedAt: r.ConfirmedAt, CostPaise: r.CostPaise, CreatedAt: r.CreatedAt,
	}
	d.ID, _ = ids.FromUUID(r.ID)
	d.Store, _ = ids.FromUUID(r.StoreID)
	d.Actor, _ = ids.FromUUID(r.ActorUserID)
	d.ConfirmedBy, _ = ids.FromUUID(r.ConfirmedBy)
	if r.InputRef != nil {
		d.InputRef = *r.InputRef
	}
	if r.Failure != nil {
		d.Failure = *r.Failure
	}
	if r.Output != nil {
		d.Output = json.RawMessage(r.Output)
	}
	if r.LineOutcomes != nil {
		if err := json.Unmarshal(r.LineOutcomes, &d.Lines); err != nil {
			return Draft{}, fmt.Errorf("drafts: stored outcomes: %w", err)
		}
	}
	return d, nil
}

func optUUID(id ids.ID) pgtype.UUID {
	if id == ids.Zero {
		return pgtype.UUID{}
	}
	return ids.UUID(id)
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func clamp32(n int) int32 {
	switch {
	case n < 0:
		return 0
	case n > 1<<31-1:
		return 1<<31 - 1
	}
	return int32(n)
}

var (
	linesOnce sync.Once
	mLines    metric.Int64Counter
)

// recordLines counts confirmed lines per outcome (tz_ai_draft_lines_total, dashboard 14's
// accept/edit/discard rates).
func recordLines(ctx context.Context, feature string, lines []LineResult) {
	linesOnce.Do(func() {
		mLines, _ = otel.Meter("todayz/ai").Int64Counter("tz_ai_draft_lines_total",
			metric.WithDescription("Lines of closed AI drafts, by feature and outcome (accepted, edited, discarded, not_permitted)"))
	})
	if mLines == nil {
		return
	}
	f := telemetry.LabelValue("feature", feature)
	for _, l := range lines {
		mLines.Add(ctx, 1, metric.WithAttributes(attribute.String("feature", f),
			attribute.String("outcome", telemetry.LabelValue("outcome", string(l.Outcome)))))
	}
}

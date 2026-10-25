// Package retail is what the shop's draft-and-confirm AI features share (W3-11: supplier
// bill AI-R2, shelf photo AI-R3, review reply AI-R6, banner copy AI-R9): the caller's store,
// the photo upload, the background start of a draft, the read of one draft and the problems.
//
// Every feature follows 11 §1 rule 1: the model's output is a draft (ai_drafts) and nothing
// changes stock, items, reviews or banners until a person confirms it. Create answers 202 at
// once; the model call runs in the background with its own deadline, and a draft still
// processing StaleAfter after it was made is closed as failed (timeout) when it is read.
package retail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	aiapi "github.com/todayz/backend/gen/api/ai"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/airetaildb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/ai/schema"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Fixed numbers (the contract's limits and the background work's bounds, not settings).
const (
	// MaxPhotoBytes bounds one photo (AiPhotoUpload.photo maxLength).
	MaxPhotoBytes = 4 << 20 //settings:ok the contract's AiPhotoUpload maxLength
	// ProcessTimeout bounds the model call in the background.
	ProcessTimeout = 90 * time.Second
	// StaleAfter closes a draft still processing this long after it was made.
	StaleAfter = 3 * time.Minute
	// MaxScansInFlight bounds a store's photo scans processing at once (bill and shelf
	// together); a third start is 429 until one finishes or goes stale.
	// A constant, not a setting: 01 does not mark it configurable (D-230).
	MaxScansInFlight = 2
)

// ScanFeatures are the photo features MaxScansInFlight counts together.
var ScanFeatures = []string{"supplier_bill", "shelf_photo"}

// PhotoTypes are the photo media types the contract takes (the model reads all three).
var PhotoTypes = []string{"image/jpeg", "image/png", "image/webp"}

// FailEmpty is the failure code of a draft the model found nothing in.
const FailEmpty = "empty"

// Kit is the drafts plumbing a feature runs on.
type Kit struct {
	// DB holds the in-flight count of a scan start (New.Scan).
	DB     *db.DB
	Clock  clock.Clock
	Guard  *guard.Guard
	Drafts *drafts.Service
	// Go runs the background work of one draft; nil starts a goroutine. Tests pass a runner
	// that waits.
	Go func(func())
}

// Scope is the business and store the route's check passed, and the caller.
func Scope(ctx context.Context) (authz.Scope, authn.Principal, error) {
	d, ok := authz.DecisionFrom(ctx)
	p, signed := authn.FromContext(ctx)
	if !ok || d.Scope.StoreID.IsZero() || !signed || p.IsAnonymous() {
		return authz.Scope{}, authn.Principal{}, problem.Wrap(genproblems.Internal, errors.New("retail: no store or caller in the authz decision"))
	}
	return d.Scope, p, nil
}

// PathID parses a path id; a malformed one is the common 404.
func PathID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, NotFound()
	}
	return id, nil
}

// FieldErr is a 422 with one field.
func FieldErr(path, reason string) *problem.Error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

// NotFound is the common 404.
func NotFound() *problem.Error { return problem.New(genproblems.NotFound, "") }

// ReadPhoto reads an AiPhotoUpload form: exactly one photo part of an allowed type and at
// most MaxPhotoBytes, whose bytes are that type (the part's Content-Type is not trusted
// alone). Any other part, or a second photo, is refused. The photo is never stored.
func ReadPhoto(mr *multipart.Reader) (client.Image, error) {
	if mr == nil {
		return client.Image{}, FieldErr("body", "required")
	}
	var img client.Image
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return client.Image{}, problem.New(genproblems.BadRequest, "")
		}
		if part.FormName() != "photo" {
			return client.Image{}, FieldErr("body", "additional_property")
		}
		if img.Data != nil {
			return client.Image{}, FieldErr("body.photo", "duplicate")
		}
		mt, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil || !slices.Contains(PhotoTypes, mt) {
			return client.Image{}, FieldErr("body.photo", "media_type")
		}
		b, err := io.ReadAll(io.LimitReader(part, MaxPhotoBytes+1))
		if err != nil {
			return client.Image{}, problem.New(genproblems.BadRequest, "")
		}
		if len(b) > MaxPhotoBytes {
			return client.Image{}, problem.New(genproblems.PayloadTooLarge, "")
		}
		if len(b) == 0 {
			return client.Image{}, FieldErr("body.photo", "required")
		}
		if Sniff(b) != mt {
			return client.Image{}, FieldErr("body.photo", "media_type")
		}
		img = client.Image{MediaType: mt, Data: b}
	}
	if img.Data == nil {
		return client.Image{}, FieldErr("body.photo", "required")
	}
	return img, nil
}

// Sniff is the photo type b's first bytes say (JPEG, PNG or WebP), or "".
func Sniff(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image/png"
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "image/webp"
	}
	return ""
}

// New is one draft to start.
type New struct {
	Feature       string
	Store, Actor  ids.ID
	InputRef      string // what the draft is about (a review or offer id); "" for a photo
	Meta          any    // small facts, never the photo or a person's words
	PromptVersion string
	// Scan counts the draft against MaxScansInFlight.
	Scan bool
}

// Start checks the switches and the daily cap, stores a processing draft and runs work in
// the background. It returns the draft id at once.
func (k Kit) Start(ctx context.Context, n New, work func(ctx context.Context, id ids.ID)) (ids.ID, error) {
	if err := k.Guard.Require(ctx, n.Feature); err != nil {
		return ids.Zero, err
	}
	create := func(ctx context.Context) (ids.ID, error) {
		if err := k.Guard.Reserve(ctx, n.Feature, guard.Subject{Store: n.Store}); err != nil {
			var ce *guard.CapError
			if errors.As(err, &ce) {
				return ids.Zero, ce.Problem()
			}
			return ids.Zero, err
		}
		return k.Drafts.Create(ctx, drafts.NewDraft{
			Feature: n.Feature, Store: n.Store, Actor: n.Actor, InputRef: n.InputRef,
			InputMeta: n.Meta, PromptVersion: n.PromptVersion,
		})
	}
	var id ids.ID
	var err error
	if n.Scan {
		// The count, the reserve and the insert hold the store's scan lock, so a refused
		// start takes nothing from the daily cap and two starts cannot both pass.
		err = k.DB.InTx(ctx, func(ctx context.Context) error {
			q := airetaildb.New(k.DB.Q(ctx))
			if err := q.LockStoreScans(ctx, "ai_scans:"+n.Store.String()); err != nil {
				return err
			}
			busy, err := q.ScansInFlight(ctx, airetaildb.ScansInFlightParams{
				StoreID: ids.UUID(n.Store), Features: ScanFeatures, Since: k.Clock.Now().Add(-StaleAfter),
			})
			if err != nil {
				return err
			}
			if busy >= MaxScansInFlight {
				return problem.New(genproblems.RateLimited, "two scans are still being read").
					WithHeader("Retry-After", strconv.Itoa(int(ProcessTimeout/time.Second)))
			}
			id, err = create(ctx)
			return err
		})
	} else {
		id, err = create(ctx)
	}
	if err != nil {
		return ids.Zero, err
	}
	// The work outlives the request: it keeps the request's values (logger, trace) but not
	// its cancel, and has its own deadline.
	bg := context.WithoutCancel(ctx)
	run := k.Go
	if run == nil {
		run = func(f func()) { go f() }
	}
	run(func() {
		ctx, cancel := context.WithTimeout(bg, ProcessTimeout)
		defer cancel()
		work(ctx, id)
	})
	return id, nil
}

// Ready stores a draft's checked output, or fails it when that cannot be stored.
func (k Kit) Ready(ctx context.Context, feature string, id ids.ID, out any, resp client.Response) {
	if _, err := k.Drafts.MarkReady(ctx, id, out, resp); err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, feature+": mark ready", slog.String("draft_id", id.String()), slog.Any("err", err))
		k.Fail(ctx, feature, id, client.OutcomeError, resp)
	}
}

// Fail closes a draft as failed with a short code.
func (k Kit) Fail(ctx context.Context, feature string, id ids.ID, reason string, resp client.Response) {
	if _, err := k.Drafts.MarkFailed(ctx, id, reason, resp); err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, feature+": mark failed", slog.String("draft_id", id.String()), slog.Any("err", err))
	}
}

// Settle books a call's cost against the daily caps.
func (k Kit) Settle(ctx context.Context, feature string, resp client.Response) {
	if err := k.Guard.Settle(ctx, resp.CostPaise); err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, feature+": settle", slog.Any("err", err))
	}
}

// Load reads one draft of the caller's store (another store's is the common 404) and closes
// it as failed (timeout) first when it has been processing longer than StaleAfter.
func (k Kit) Load(ctx context.Context, feature string, store, id ids.ID) (drafts.Draft, error) {
	if err := k.Guard.Require(ctx, feature); err != nil {
		return drafts.Draft{}, err
	}
	d, err := k.Drafts.Get(ctx, feature, id)
	if err != nil {
		return drafts.Draft{}, err
	}
	if d.Store != store {
		return drafts.Draft{}, NotFound()
	}
	if d.Status == drafts.StatusProcessing && k.Clock.Now().Sub(d.CreatedAt) > StaleAfter {
		if _, err := k.Drafts.MarkFailed(ctx, id, client.OutcomeTimeout, client.Response{}); err != nil {
			telemetry.Logger(ctx).ErrorContext(ctx, feature+": close stale draft", slog.String("draft_id", id.String()), slog.Any("err", err))
		} else if d, err = k.Drafts.Get(ctx, feature, id); err != nil {
			return drafts.Draft{}, err
		}
	}
	return d, nil
}

// OwnStore is the Check of a confirm: a draft of another store is the common 404.
func OwnStore(store ids.ID) func(drafts.Draft) error {
	return func(d drafts.Draft) error {
		if d.Store != store {
			return NotFound()
		}
		return nil
	}
}

// Failure is a failed draft's code, or nil.
func Failure(d drafts.Draft) *aiapi.AiDraftFailure {
	if d.Failure == "" {
		return nil
	}
	f := d.Failure
	return &f
}

// Refusal is why a confirm refused one line (AiLineRefusal). A line refused is not_permitted
// with this reason; the other lines are applied, and the draft stays ready so the person can
// edit the refused line and confirm it again.
type Refusal string

// The refusal codes.
const (
	RefuseNotPermitted  Refusal = "not_permitted"
	RefuseItem          Refusal = "invalid_item"
	RefuseQuantity      Refusal = "invalid_quantity"
	RefuseCost          Refusal = "invalid_cost"
	RefuseCostAboveMRP  Refusal = "invalid_cost_above_mrp"
	RefuseExpiry        Refusal = "invalid_expiry"
	RefuseProduct       Refusal = "invalid_product"
	RefuseListed        Refusal = "invalid_listed"
	RefuseDuplicate     Refusal = "invalid_duplicate"
	RefusePrice         Refusal = "invalid_price"
	RefusePriceAboveMRP Refusal = "invalid_price_above_mrp"
	RefuseOpeningStock  Refusal = "invalid_opening_stock"
)

func (r Refusal) Error() string { return "ai: line refused: " + string(r) }

// Refused is the line's not_permitted result when err is a Refusal.
func Refused(lineID string, err error) (drafts.LineResult, bool) {
	var r Refusal
	if !errors.As(err, &r) {
		return drafts.LineResult{}, false
	}
	return drafts.LineResult{LineID: lineID, Outcome: drafts.NotPermitted, Reason: string(r)}, true
}

// Outcomes are the stored line outcomes of a draft: every line once confirmed or discarded,
// the lines a partial confirm settled while it is ready.
func Outcomes(lines []drafts.LineResult) aiapi.AiOutcomes {
	out := make(aiapi.AiOutcomes, 0, len(lines))
	for _, l := range lines {
		r := aiapi.AiLineResult{LineId: l.LineID, Outcome: aiapi.AiLineOutcome(l.Outcome)}
		if l.Reason != "" {
			reason := aiapi.AiLineRefusal(l.Reason)
			r.Reason = &reason
		}
		out = append(out, r)
	}
	return out
}

// Result is a confirm's answer.
func Result(res drafts.Result) aiapi.AiConfirmResult {
	return aiapi.AiConfirmResult{DraftId: res.DraftID.String(), Status: aiapi.AiConfirmResultStatus(res.Status), Lines: Outcomes(res.Lines)}
}

// Decision builds one drafts.Decision from a request line; value is nil unless edited.
func Decision(lineID string, outcome aiapi.AiConfirmOutcome, value any) (drafts.Decision, error) {
	d := drafts.Decision{LineID: lineID, Outcome: drafts.Outcome(outcome)}
	if value != nil {
		b, err := json.Marshal(value)
		if err != nil {
			return drafts.Decision{}, err
		}
		d.Value = b
	}
	return d, nil
}

// Index maps each line id to its first position in the request, for 422 field paths.
func Index(lineIDs []string) map[string]int {
	m := make(map[string]int, len(lineIDs))
	for i, id := range lineIDs {
		if _, dup := m[id]; !dup {
			m[id] = i
		}
	}
	return m
}

// Call runs one strict-tool call and decodes the tool input into out. A provider error the
// request caused (4xx other than 429) is returned as an error; every other failure is a
// reject code (a client outcome or schema) so the draft fails with it.
func Call(ctx context.Context, llm client.LLM, req client.Request, out any) (client.Response, string, error) {
	resp, err := llm.Complete(ctx, req)
	if err != nil {
		var pe *client.ProviderError
		if errors.As(err, &pe) && !pe.Retryable() && pe.Status != 429 {
			return resp, "", err
		}
		return resp, client.Outcome(err), nil
	}
	if derr := schema.Decode(resp.ToolInput, out); derr != nil {
		client.RecordRejected(ctx, req.Feature, client.RejectSchema)
		return resp, client.RejectSchema, nil //nolint:nilerr // the draft fails with "schema"
	}
	return resp, "", nil
}

// ItemName is an item's display name: the name, then the variant and the pack unless the
// name already says them ("Amul Butter 100 g").
func ItemName(name string, variant *string, packSize, unit string) string {
	name = strings.TrimSpace(name)
	parts := []string{name}
	low := strings.ToLower(name)
	if variant != nil && strings.TrimSpace(*variant) != "" && !strings.Contains(low, strings.ToLower(*variant)) {
		parts = append(parts, strings.TrimSpace(*variant))
	}
	size := strings.TrimSpace(packSize)
	if strings.Contains(size, ".") {
		size = strings.TrimRight(strings.TrimRight(size, "0"), ".")
	}
	unit = strings.TrimSpace(unit)
	if pack := strings.TrimSpace(size + " " + unit); pack != "" &&
		!strings.Contains(low, strings.ToLower(pack)) && !strings.Contains(low, strings.ToLower(size+unit)) {
		parts = append(parts, pack)
	}
	return strings.Join(parts, " ")
}

// Truncate cuts s to n runes.
func Truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

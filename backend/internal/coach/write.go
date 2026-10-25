package coach

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/todayz/backend/gen/sqlc/airetaildb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/schema"
	"github.com/todayz/backend/internal/coach/detectors"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Feature is the AI feature name of the coach's wording (11 §2.1 step 4).
const Feature = "coach_card"

//go:embed prompt.md
var promptFS embed.FS

func init() { prompts.MustRegister(Feature, promptFS) }

// Tool is the strict tool the model answers through.
var Tool = client.Tool{
	Name:        "write_coach_card",
	Description: "Write the coach card's title and body.",
	Schema: schema.Object("", map[string]schema.S{
		"title": schema.String("The card's title", titleMax),
		"body":  schema.String("The card's body", bodyMax),
	}),
}

// Output is the tool input.
type Output struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Validate is the bounds the strict schema only describes.
func (o *Output) Validate() error {
	t, b := strings.TrimSpace(o.Title), strings.TrimSpace(o.Body)
	switch {
	case t == "" || len([]rune(t)) > titleMax:
		return errors.New("title length")
	case b == "" || len([]rune(b)) > bodyMax:
		return errors.New("body length")
	}
	return nil
}

// cardMaxTokens bounds one reply (a title and a body).
const cardMaxTokens = 400 //settings:ok a provider request bound, not a product number

type generated struct {
	By            string
	PromptVersion *string
	Model         *string
}

// Input is what the model sees for one card: no store name, no person.
// Detector is not sent; it picks the units Numbers is grounded in.
type Input struct {
	Detector string          `json:"-"`
	Lang     string          `json:"lang"`
	Kind     string          `json:"kind"`
	Title    string          `json:"title"`
	Body     string          `json:"body"`
	Numbers  json.RawMessage `json:"numbers"`
}

// Rewrite asks the model for the card's wording and checks it. reject is ""
// when the output is used, else why it was not (a client outcome, schema or
// grounding); the caller then keeps the template.
func Rewrite(ctx context.Context, llm client.LLM, model, ref string, in Input) (out Output, resp client.Response, reject string, err error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Output{}, resp, "", err
	}
	msg, err := json.Marshal(in)
	if err != nil {
		return Output{}, resp, "", err
	}
	resp, err = llm.Complete(ctx, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys,
		Messages: []client.Message{client.UserText(string(msg))}, Tool: &Tool, MaxTokens: cardMaxTokens,
	})
	if err != nil {
		var pe *client.ProviderError
		if errors.As(err, &pe) && !pe.Retryable() {
			return Output{}, resp, "", err
		}
		return Output{}, resp, client.Outcome(err), nil
	}
	if schema.Decode(resp.ToolInput, &out) != nil {
		client.RecordRejected(ctx, Feature, client.RejectSchema)
		return Output{}, resp, client.RejectSchema, nil //nolint:nilerr // the card keeps its template
	}
	out.Title, out.Body = strings.TrimSpace(out.Title), strings.TrimSpace(out.Body)
	// Every number in the card must be one of the fact's, in its unit (11 §1 rule 6, D-230).
	if len(schema.UngroundedFactsIn(out.Title+"\n"+out.Body, in.Numbers, factUnits[in.Detector])) > 0 {
		client.RecordRejected(ctx, Feature, client.RejectGrounding)
		return Output{}, resp, client.RejectGrounding, nil
	}
	return out, resp, "", nil
}

// factUnits is the unit of every number each detector's card carries (D-230), so grounding
// does not depend on a key's name saying it: expiry_penalty's threshold is a percent ("limit
// 30%"), which its name alone would read as a plain count. Text-only keys (item, itemA,
// itemB, leaders, weekday) are not listed; their numbers keep their own marks.
// TestFactUnitsCoverEveryNumber keeps this complete against the templates and the goldens.
var factUnits = map[string]schema.Units{
	"top_seller_low_days_of_cover": {"days": schema.Plain, "qty": schema.Plain, "sold": schema.Plain, "periodDays": schema.Plain},
	"slow_mover_21d":               {"days": schema.Plain, "onHand": schema.Plain, "pct": schema.Percent},
	"expiring_batch_value":         {"count": schema.Plain, "withinDays": schema.Plain},
	"wanted_nearby":                {"searches": schema.Plain, "km": schema.Plain, "periodDays": schema.Plain},
	"missed_sales":                 {"searches": schema.Plain, "zeroDays": schema.Plain},
	"expired_on_shelf":             {"qty": schema.Plain, "daysAgo": schema.Plain},
	"selling_below_cost":           {"price": schema.Money, "cost": schema.Money},
	"stale_stock_share":            {"count": schema.Plain, "days": schema.Plain},
	"bought_together":              {"oneIn": schema.Plain},
	"regulars_slipping":            {"count": schema.Plain, "lapseDays": schema.Plain},
	"missed_orders_peak_hour":      {"count": schema.Plain, "minutes": schema.Plain, "window": schema.Plain},
	"best_day_record":              {"weeks": schema.Plain, "sales": schema.Money, "pct": schema.Percent},
	"reviews_unanswered":           {"count": schema.Plain, "days": schema.Plain},
	"weekly_summary":               {"sales": schema.Money, "bills": schema.Plain, "pct": schema.Percent, "weeks": schema.Plain},
	"rating_drop":                  {"now": schema.Plain, "before": schema.Plain, "count": schema.Plain, "days": schema.Plain},
	"accept_time_vs_benchmark":     {"mine": schema.Plain, "nearby": schema.Plain},
	"setup_incomplete":             {},
	"expiry_penalty": {"pct": schema.Percent, "threshold": schema.Percent, "days": schema.Plain,
		"expired": schema.Plain, "orders": schema.Plain},
}

// writeAI returns the model's wording when the feature is on, the caps allow
// and the output passes; ok false keeps the template.
func (s *Service) writeAI(ctx context.Context, storeID ids.ID, f detectors.Fact, lang string, tpl Text, nums []byte) (Text, generated, bool) {
	if s.d.LLM == nil || s.d.Guard == nil {
		return Text{}, generated{}, false
	}
	log := telemetry.Logger(ctx)
	on, err := s.d.Guard.Available(ctx, Feature)
	if err != nil || !on {
		return Text{}, generated{}, false
	}
	in := Input{Detector: f.Detector, Lang: lang, Kind: string(f.Kind), Title: tpl.Title, Body: tpl.Body, Numbers: nums}
	ref, hash, err := missKey(f, in)
	if err != nil {
		return Text{}, generated{}, false
	}
	q := airetaildb.New(s.d.DB.Q(ctx))
	key := airetaildb.AiTextMissedParams{Feature: Feature, StoreID: ids.UUID(storeID), Ref: ref, Lang: lang, FactsHash: hash}
	if missed, err := q.AiTextMissed(ctx, key); err != nil || missed {
		return Text{}, generated{}, false // these facts failed before (D-228)
	}
	// Background wording has its own budget (D-228); when it is spent the
	// template is used.
	if err := s.d.Guard.Reserve(ctx, Feature, guard.Subject{Store: storeID, Background: true}); err != nil {
		var ce *guard.CapError
		if !errors.As(err, &ce) {
			log.WarnContext(ctx, "coach: guard", slog.String("error", err.Error()))
		}
		return Text{}, generated{}, false
	}
	model, err := s.d.Settings.String(ctx, settings.AiModelFast)
	if err != nil {
		return Text{}, generated{}, false
	}
	out, resp, reject, err := Rewrite(ctx, s.d.LLM, model, storeID.String(), in)
	if serr := s.d.Guard.Settle(ctx, resp.CostPaise); serr != nil {
		log.WarnContext(ctx, "coach: settle", slog.String("error", serr.Error()))
	}
	if err != nil || reject != "" {
		if err != nil {
			log.WarnContext(ctx, "coach: model call", slog.String("error", err.Error()))
		}
		if reason, ok := missReason(reject); ok {
			if err := q.UpsertAiTextMiss(ctx, airetaildb.UpsertAiTextMissParams{
				Feature: Feature, StoreID: key.StoreID, Ref: ref, Lang: lang, FactsHash: hash, Reason: reason, Now: s.d.Clock.Now().UTC(),
			}); err != nil {
				log.WarnContext(ctx, "coach: keep miss", slog.String("error", err.Error()))
			}
		}
		return Text{}, generated{}, false
	}
	v := prompts.Version(Feature)
	m := resp.Model
	if m == "" {
		m = model
	}
	return Text(out), generated{By: "ai", PromptVersion: &v, Model: &m}, true
}

// missKey is a card's ref and facts hash in ai_text_misses: the detector and
// subject, and a hash of everything the model saw.
func missKey(f detectors.Fact, in Input) (ref, hash string, err error) {
	b, err := json.Marshal(in)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(b)
	return f.Detector + "/" + f.Subject, hex.EncodeToString(sum[:]), nil
}

// missReason is the ai_text_misses reason of a reject, or false for a
// transient one (an error or a timeout), which may ask again.
func missReason(reject string) (string, bool) {
	switch reject {
	case client.RejectGrounding:
		return "grounding", true
	case client.RejectSchema, client.OutcomeInvalid, client.OutcomeTruncated:
		return "schema", true
	case client.OutcomeRefused:
		return "refused", true
	}
	return "", false
}

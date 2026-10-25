// Package coach is the shop coach (11 §2.1, B-19): detectors find facts in a
// store's own numbers, the chooser keeps the top few a day, the writer turns
// each into a short card (the model's wording, or the rule template when the
// model is off, capped or wrong), and the feed shows them on Overview and the
// coach screen. A card only prefills a screen; nothing is applied from it
// (11 §1 rule 1).
package coach

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/coachdb"
	"github.com/todayz/backend/gen/sqlc/insightsdb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/coach/detectors"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Pusher sends a high card as a push. TODO(lead): notify (W2-05) has no coach
// notification kind yet; until it does Deps.Pusher is nil and no card is
// pushed (pushes count 0).
type Pusher interface {
	PushCoachCard(ctx context.Context, storeID, cardID ids.ID, t Text) error
}

// Deps is what the service needs. LLM and Guard may be nil: every card is
// then the rule template.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	LLM      client.LLM
	Guard    *guard.Guard
	Pusher   Pusher
}

// Service runs the coach.
type Service struct {
	d   Deps
	ids *ids.Generator
}

// New returns the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("coach: a clock is required")
	}
	return &Service{d: d, ids: ids.NewGenerator()}, nil
}

// Weight changes of 11 §2.1 step 5 (10000 = neutral, kept in 2500–20000 by
// the query). TODO(lead): 01 has no keys for these; conservative reading,
// Not useful counts more than Done and ignoring counts a little.
const (
	weightDone    = 1500  //settings:ok coach weight step, see TODO(lead)
	weightDismiss = -2500 //settings:ok coach weight step, see TODO(lead)
	weightIgnored = -500  //settings:ok coach weight step, see TODO(lead)
	weightNeutral = 10000 //settings:ok basis points per whole
)

// validFor is how long a card of each job stays live: a daily card until the
// next day's run has passed, a weekly one until the next Monday's, a realtime
// one a day (it is rewritten while the rule still fires).
var validFor = map[detectors.Job]time.Duration{
	detectors.Daily:    48 * time.Hour,     //settings:ok two daily runs
	detectors.Weekly:   8 * 24 * time.Hour, //settings:ok one weekly run and a day
	detectors.Realtime: 24 * time.Hour,     //settings:ok one IST day
}

// Summary counts one run (job_runs.summary).
type Summary struct {
	Stores, Created, Skipped, AI, Template, Pushes, Failed int64
}

// Add adds o.
func (s *Summary) Add(o Summary) {
	s.Stores += o.Stores
	s.Created += o.Created
	s.Skipped += o.Skipped
	s.AI += o.AI
	s.Template += o.Template
	s.Pushes += o.Pushes
	s.Failed += o.Failed
}

// Map is the job_runs summary.
func (s Summary) Map() map[string]int64 {
	return map[string]int64{
		"stores": s.Stores, "cards_created": s.Created, "cards_skipped": s.Skipped,
		"cards_ai": s.AI, "cards_template": s.Template, "pushes": s.Pushes, "failed": s.Failed,
	}
}

func key(det, subj string) string { return det + "|" + subj }

// RunStore runs job j's detectors for one store and writes its new cards.
func (s *Service) RunStore(ctx context.Context, j detectors.Job, storeID ids.ID) (Summary, error) {
	var sum Summary
	now := s.d.Clock.Now()
	sid := ids.UUID(storeID)
	q := coachdb.New(s.d.DB.Q(ctx))
	prof, err := q.StoreProfile(ctx, sid)
	if err != nil {
		return sum, fmt.Errorf("coach: store profile: %w", err)
	}
	sum.Stores = 1
	in := &detectors.Input{
		Q: q, Settings: s.d.Settings, Store: detectors.Store{ID: storeID, StoreProfileRow: prof},
		Now: now, Today: facts.DayOf(now),
	}

	expired, err := q.ExpireDue(ctx, coachdb.ExpireDueParams{Now: now, StoreID: sid})
	if err != nil {
		return sum, err
	}
	if err := s.ignored(ctx, q, sid, expired, now); err != nil {
		return sum, err
	}

	quietRows, err := q.QuietSubjects(ctx, coachdb.QuietSubjectsParams{StoreID: sid, Now: &now})
	if err != nil {
		return sum, err
	}
	quiet := map[string]bool{}
	for _, r := range quietRows {
		quiet[key(r.Detector, r.SubjectRef)] = true
	}

	dets := detectors.For(j)
	if detectors.NeedsFacts(j) {
		if err := s.loadFacts(ctx, in, dets, quietRows); err != nil {
			return sum, err
		}
	}

	var all []detectors.Fact
	names := make([]string, 0, len(dets))
	firing := []string{}
	for _, d := range dets {
		names = append(names, d.Name)
		fs, err := d.Run(ctx, in)
		if err != nil {
			// One broken detector does not stop the others; its live cards
			// stay (it is not "no longer firing").
			telemetry.Logger(ctx).ErrorContext(ctx, "coach: detector failed",
				slog.String("detector", d.Name), slog.String("error", err.Error()))
			sum.Failed++
			names = names[:len(names)-1]
			continue
		}
		for _, f := range fs {
			firing = append(firing, key(f.Detector, f.Subject))
		}
		all = append(all, fs...)
	}
	gone, err := q.ExpireNotFiring(ctx, coachdb.ExpireNotFiringParams{Now: now, StoreID: sid, Detectors: names, Firing: firing})
	if err != nil {
		return sum, err
	}
	if err := s.ignored(ctx, q, sid, gone, now); err != nil {
		return sum, err
	}

	picked, skipped, err := s.choose(ctx, q, sid, in.Today, all, quiet)
	if err != nil {
		return sum, err
	}
	sum.Skipped += skipped
	for _, f := range picked {
		w, err := s.write(ctx, j, in, f)
		if err != nil {
			return sum, err
		}
		sum.Add(w)
	}
	return sum, nil
}

func (s *Service) loadFacts(ctx context.Context, in *detectors.Input, dets []detectors.Detector, coachQuiet []coachdb.QuietSubjectsRow) error {
	iq := insightsdb.New(s.d.DB.Q(ctx))
	cfg, err := facts.LoadSettings(ctx, s.d.Settings)
	if err != nil {
		return err
	}
	// One "Not useful" quiets the subject on both screens (D-99): the
	// Demand insights quiets and the coach's, mapped to the insight kind.
	fq := facts.Quiet{}
	rows, err := iq.QuietSubjects(ctx, insightsdb.QuietSubjectsParams{StoreID: ids.UUID(in.Store.ID), Now: in.Now})
	if err != nil {
		return err
	}
	for _, r := range rows {
		fq.Add(facts.Kind(r.Kind), r.SubjectRef)
	}
	kindOf := map[string]facts.Kind{}
	for _, d := range dets {
		if d.FromFacts != "" {
			kindOf[d.Name] = d.FromFacts
		}
	}
	for _, r := range coachQuiet {
		if k, ok := kindOf[r.Detector]; ok {
			fq.Add(k, r.SubjectRef)
		}
	}
	res, err := facts.ComputeWith(ctx, facts.Source{Q: iq, Settings: s.d.Settings, Asks: facts.NoAsks{}}, cfg, in.Store.ID, in.Now, fq)
	if err != nil {
		return fmt.Errorf("coach: demand insights: %w", err)
	}
	in.Facts, in.FactsLoaded, in.FactsCfg = res, true, cfg
	return nil
}

// ignored lowers the weight of detectors whose cards expired unacted.
func (s *Service) ignored(ctx context.Context, q *coachdb.Queries, sid pgtype.UUID, dets []string, now time.Time) error {
	for _, d := range dets {
		if err := q.BumpWeight(ctx, coachdb.BumpWeightParams{StoreID: sid, Detector: d, DeltaBp: weightIgnored, Ignored: 1, Now: now}); err != nil {
			return err
		}
	}
	return nil
}

// choose keeps the facts a card may be written for, best first, within the
// day's cap (11 §2.1 steps 2–3). skipped counts facts dropped by the cap.
func (s *Service) choose(ctx context.Context, q *coachdb.Queries, sid pgtype.UUID, today facts.Day,
	all []detectors.Fact, quiet map[string]bool) ([]detectors.Fact, int64, error) {
	live, err := q.LiveCardKeys(ctx, sid)
	if err != nil {
		return nil, 0, err
	}
	isLive := map[string]bool{}
	for _, r := range live {
		isLive[key(r.Detector, r.SubjectRef)] = true
	}
	wrows, err := q.Weights(ctx, sid)
	if err != nil {
		return nil, 0, err
	}
	weight := map[string]int64{}
	for _, r := range wrows {
		weight[r.Detector] = int64(r.WeightBp)
	}
	var keep []detectors.Fact
	for _, f := range all {
		k := key(f.Detector, f.Subject)
		switch {
		case f.AboutOthers && f.Others < detectors.KFloor: // D-71, never lower
		case quiet[k], isLive[k]:
		default:
			keep = append(keep, f)
		}
	}
	score := func(f detectors.Fact) int64 {
		w, ok := weight[f.Detector]
		if !ok {
			w = weightNeutral
		}
		return int64(f.Severity) * w * 100 / int64(1+f.AgeDays)
	}
	detectors.SortByStake(keep)
	sort.SliceStable(keep, func(a, b int) bool { return score(keep[a]) > score(keep[b]) })

	maxCards, err := s.d.Settings.Int(ctx, settings.CoachMaxCardsPerDay)
	if err != nil {
		return nil, 0, err
	}
	made, err := q.CardsCreatedOn(ctx, coachdb.CardsCreatedOnParams{StoreID: sid, Day: today.PG()})
	if err != nil {
		return nil, 0, err
	}
	room := max(maxCards-made, 0)
	if int64(len(keep)) <= room {
		return keep, 0, nil
	}
	return keep[:room], int64(len(keep)) - room, nil
}

// write writes one card and pushes it when it is high and the day's pushes
// allow.
func (s *Service) write(ctx context.Context, j detectors.Job, in *detectors.Input, f detectors.Fact) (Summary, error) {
	var sum Summary
	now := in.Now
	sid := ids.UUID(in.Store.ID)
	lang := langOf(in.Store.Lang)
	tpl, err := render(f, lang)
	if err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "coach: template", slog.String("detector", f.Detector), slog.String("error", err.Error()))
		sum.Failed++
		return sum, nil
	}
	nums, err := json.Marshal(f.Numbers)
	if err != nil {
		return sum, err
	}
	text, gen := tpl, generated{By: "rule"}
	if t, g, ok := s.writeAI(ctx, in.Store.ID, f, lang, tpl, nums); ok {
		text, gen = t, g
	}
	payload, err := json.Marshal(nonNil(f.Action.Payload))
	if err != nil {
		return sum, err
	}
	act := f.Action.Type
	if act == "" {
		act = detectors.ActNone
	}
	id := s.ids.New(now)
	_, err = coachdb.New(s.d.DB.Q(ctx)).InsertCard(ctx, coachdb.InsertCardParams{
		ID: ids.UUID(id), StoreID: sid, Detector: f.Detector, SubjectRef: f.Subject,
		Kind: string(f.Kind), Topic: f.Topic, Title: text.Title, Body: text.Body, Numbers: nums,
		ActionType: act, ActionPayload: payload, Priority: int32(f.Severity), //nolint:gosec // 1–3
		GeneratedBy: gen.By, PromptVersion: gen.PromptVersion, Model: gen.Model,
		Lang: lang, Job: string(j), Day: in.Today.PG(), ValidUntil: now.Add(validFor[j]), Now: now,
	})
	if db.IsNoRows(err) {
		sum.Skipped++ // a concurrent run wrote it first
		return sum, nil
	}
	if err != nil {
		return sum, err
	}
	sum.Created++
	if gen.By == "ai" {
		sum.AI++
	} else {
		sum.Template++
	}
	q := coachdb.New(s.d.DB.Q(ctx))
	if err := q.BumpWeight(ctx, coachdb.BumpWeightParams{StoreID: sid, Detector: f.Detector, Shown: 1, Now: now}); err != nil {
		return sum, err
	}
	pushed, err := s.push(ctx, q, in.Store.ID, id, f, text, now)
	if err != nil {
		// A failed push never loses the card; it stays on the feed.
		telemetry.Logger(ctx).WarnContext(ctx, "coach: push failed", slog.String("detector", f.Detector), slog.String("error", err.Error()))
	}
	if pushed {
		sum.Pushes++
	}
	return sum, nil
}

func (s *Service) push(ctx context.Context, q *coachdb.Queries, storeID, cardID ids.ID, f detectors.Fact, t Text, now time.Time) (bool, error) {
	if s.d.Pusher == nil || f.Severity < 3 {
		return false, nil
	}
	maxPush, err := s.d.Settings.Int(ctx, settings.CoachMaxPushPerDay)
	if err != nil {
		return false, err
	}
	since := clock.StartOfDayIST(now)
	n, err := q.PushesSince(ctx, coachdb.PushesSinceParams{StoreID: ids.UUID(storeID), Since: &since})
	if err != nil || n >= maxPush {
		return false, err
	}
	if err := s.d.Pusher.PushCoachCard(ctx, storeID, cardID, t); err != nil {
		return false, err
	}
	_, err = q.MarkPushed(ctx, coachdb.MarkPushedParams{Now: &now, ID: ids.UUID(cardID)})
	return err == nil, err
}

func nonNil(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

package coach

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	coachapi "github.com/todayz/backend/gen/api/coach"
	"github.com/todayz/backend/gen/sqlc/coachdb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/coach/detectors"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/seed"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// The dev seed's stores and owner (internal/seed/business.go).
var (
	storeSL  = mustID("01KE0S1W6W8ZV7T1C3A6MS0001")
	storeSL2 = mustID("01KE0S1W6W8ZV7T1C3A6MS0002")
	owner    = mustID("01KE0S1W6W8ZV7T1C3A6M9N011")
)

func mustID(s string) ids.ID {
	id, err := ids.Parse(s)
	if err != nil {
		panic(err)
	}
	return id
}

type env struct {
	tdb   *testkit.DB
	d     *db.DB
	st    *settings.Cache
	clk   *clock.Fake
	ctx   context.Context
	push  *fakePusher
	llm   *fakeLLM
	guard *guard.Guard
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	enc, hash := make([]byte, 32), make([]byte, 32)
	_, err := rand.Read(enc)
	require.NoError(t, err)
	_, err = rand.Read(hash)
	require.NoError(t, err)
	keys := config.AppKeys{PIIEncKey: config.NewSecret(enc), PIIHashKey: config.NewSecret(hash)}
	d := db.New(tdb.Pool)
	require.NoError(t, seed.Run(ctx, config.EnvTest, seed.Dev, seed.Deps{DB: d, Keys: keys}))
	st := settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	clk := clock.NewFake(seed.Epoch)
	return &env{tdb: tdb, d: d, st: st, clk: clk, ctx: ctx, push: &fakePusher{}, llm: &fakeLLM{},
		guard: guard.New(d, st, clk, guard.Providers{LLM: true})}
}

func (e *env) setting(t *testing.T, k settings.Key, v string) {
	t.Helper()
	_, err := e.tdb.OwnerPool.Exec(e.ctx, `UPDATE platform_settings SET value = $2 WHERE key = $1`, string(k), v)
	require.NoError(t, err)
	e.st.Invalidate()
}

func (e *env) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	_, err := e.tdb.OwnerPool.Exec(e.ctx, sql, args...)
	require.NoError(t, err, sql)
}

// service is the coach with the fake pusher, and the fake model when ai is set.
func (e *env) service(t *testing.T, ai bool) *Service {
	t.Helper()
	d := Deps{DB: e.d, Clock: e.clk, Settings: e.st, Pusher: e.push}
	if ai {
		d.LLM, d.Guard = e.llm, e.guard
	}
	s, err := New(d)
	require.NoError(t, err)
	return s
}

func (e *env) cards(t *testing.T, store ids.ID) []coachdb.CoachSuggestion {
	t.Helper()
	rows, err := coachdb.New(e.tdb.Pool).ListCards(e.ctx, coachdb.ListCardsParams{
		StoreID: ids.UUID(store), Statuses: []string{"new", "seen", "done", "dismissed", "expired"}, Lim: 100})
	require.NoError(t, err)
	return rows
}

type fakePusher struct {
	mu    sync.Mutex
	cards []ids.ID
	err   error
}

func (p *fakePusher) PushCoachCard(_ context.Context, _, card ids.ID, _ Text) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.cards = append(p.cards, card)
	return nil
}

// fakeLLM answers with reply(req) as the write_coach_card tool input.
type fakeLLM struct {
	mu    sync.Mutex
	calls int
	reply func(in Input) (Output, error)
}

func (f *fakeLLM) Complete(_ context.Context, req client.Request) (client.Response, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	var in Input
	if err := json.Unmarshal([]byte(req.Messages[0].Content[0].Text), &in); err != nil {
		return client.Response{}, err
	}
	out, err := f.reply(in)
	if err != nil {
		return client.Response{}, err
	}
	b, _ := json.Marshal(out)
	return client.Response{Model: "fake-fast", ToolInput: b, CostPaise: 1}, nil
}

// TestCardCap: a run writes at most coach_max_cards_per_day cards, highest
// severity first, and a second run the same day writes none.
func TestCardCap(t *testing.T) {
	e := newEnv(t)
	e.setting(t, settings.CoachMaxCardsPerDay, "3")
	s := e.service(t, false)
	sum, err := s.RunStore(e.ctx, detectors.Daily, storeSL2)
	require.NoError(t, err)
	require.EqualValues(t, 3, sum.Created)
	require.Positive(t, sum.Skipped)
	require.EqualValues(t, 3, sum.Template)
	cs := e.cards(t, storeSL2)
	require.Len(t, cs, 3)
	for _, c := range cs {
		require.Equal(t, "rule", c.GeneratedBy)
		require.EqualValues(t, 3, c.Priority, "the cap keeps the high-severity facts")
	}

	e.clk.Advance(2 * time.Hour)
	sum, err = s.RunStore(e.ctx, detectors.Daily, storeSL2)
	require.NoError(t, err)
	require.Zero(t, sum.Created)
	require.Len(t, e.cards(t, storeSL2), 3)
}

// TestPushCap: only severity-3 cards push, at most coach_max_push_per_day a
// day; the rest stay on the feed.
func TestPushCap(t *testing.T) {
	e := newEnv(t)
	s := e.service(t, false)
	sum, err := s.RunStore(e.ctx, detectors.Daily, storeSL2)
	require.NoError(t, err)
	require.EqualValues(t, 1, sum.Pushes)
	require.Len(t, e.push.cards, 1)
	var pushed, high int
	for _, c := range e.cards(t, storeSL2) {
		if c.Priority == 3 {
			high++
		}
		if c.PushedAt != nil {
			pushed++
			require.EqualValues(t, 3, c.Priority)
		}
	}
	require.Equal(t, 1, pushed)
	require.Greater(t, high, 1, "more high cards than the push cap")

	// Another store has its own count.
	e.exec(t, `UPDATE stores SET unaccepted_expiry_rate_30d = 0.4 WHERE id = $1`, ids.UUID(storeSL))
	sum, err = s.RunStore(e.ctx, detectors.Realtime, storeSL)
	require.NoError(t, err)
	require.EqualValues(t, 1, sum.Pushes)

	// Same store, same IST day: no second push.
	e.clk.Advance(time.Hour)
	e.exec(t, `UPDATE coach_suggestions SET status = 'expired' WHERE store_id = $1 AND detector = 'expiry_penalty'`, ids.UUID(storeSL))
	sum, err = s.RunStore(e.ctx, detectors.Realtime, storeSL)
	require.NoError(t, err)
	require.EqualValues(t, 1, sum.Created)
	require.Zero(t, sum.Pushes)

	// A failed push keeps the card.
	e2 := newEnv(t)
	e2.push.err = errors.New("push down")
	sum, err = e2.service(t, false).RunStore(e2.ctx, detectors.Daily, storeSL2)
	require.NoError(t, err)
	require.Zero(t, sum.Pushes)
	require.Positive(t, sum.Created)
}

// TestDismissQuiet: a dismissed subject is not written again for
// coach_dismiss_quiet_days, then may come back.
func TestDismissQuiet(t *testing.T) {
	e := newEnv(t)
	s := e.service(t, false)
	_, err := s.RunStore(e.ctx, detectors.Daily, storeSL)
	require.NoError(t, err)
	cs := e.cards(t, storeSL)
	require.NotEmpty(t, cs)
	target := cs[0]
	tid, _ := ids.FromUUID(target.ID)
	c, err := s.Dismiss(e.ctx, owner, storeSL, tid)
	require.NoError(t, err)
	require.Equal(t, coachapi.CoachCardStatus("dismissed"), c.Status)

	// Rerun later the same day with room for more cards: not rewritten.
	e.setting(t, settings.CoachMaxCardsPerDay, "10")
	e.clk.Advance(time.Hour)
	_, err = s.RunStore(e.ctx, detectors.Daily, storeSL)
	require.NoError(t, err)
	n := 0
	for _, x := range e.cards(t, storeSL) {
		if x.Detector == target.Detector && x.SubjectRef == target.SubjectRef {
			n++
		}
	}
	require.Equal(t, 1, n, "a dismissed subject stays quiet")

	days, err := e.st.Int(e.ctx, settings.CoachDismissQuietDays)
	require.NoError(t, err)
	q := coachdb.New(e.tdb.Pool)
	quietAt := func(at time.Time) bool {
		rows, err := q.QuietSubjects(e.ctx, coachdb.QuietSubjectsParams{StoreID: ids.UUID(storeSL), Now: &at})
		require.NoError(t, err)
		for _, r := range rows {
			if r.Detector == target.Detector && r.SubjectRef == target.SubjectRef {
				return true
			}
		}
		return false
	}
	dismissedAt := seed.Epoch
	require.True(t, quietAt(dismissedAt.Add(time.Duration(days)*24*time.Hour-time.Second)))
	require.False(t, quietAt(dismissedAt.Add(time.Duration(days)*24*time.Hour+time.Second)))

	// Not useful lowers the detector's weight.
	ws, err := q.Weights(e.ctx, ids.UUID(storeSL))
	require.NoError(t, err)
	for _, w := range ws {
		if w.Detector == target.Detector {
			require.Less(t, w.WeightBp, int32(weightNeutral))
		}
	}
}

// TestKFloor: a fact about other people or shops behind fewer than 10 is
// never chosen, whatever its score (D-71).
func TestKFloor(t *testing.T) {
	e := newEnv(t)
	s := e.service(t, false)
	q := coachdb.New(e.tdb.Pool)
	mk := func(subj string, others int64) detectors.Fact {
		return detectors.Fact{Detector: "accept_time_vs_benchmark", Subject: subj, Kind: detectors.Low, Topic: detectors.TopicOrders,
			Severity: 3, AboutOthers: true, Others: others, Numbers: map[string]any{"mine": 9, "nearby": 2}}
	}
	got, skipped, err := s.choose(e.ctx, q, ids.UUID(storeSL), facts.DayOf(seed.Epoch),
		[]detectors.Fact{mk("a", 0), mk("b", detectors.KFloor-1), mk("c", detectors.KFloor)}, map[string]bool{})
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, got, 1)
	require.Equal(t, "c", got[0].Subject)
}

// TestAIWriter: the model's wording is used only when the feature is on and
// every number in it is one of the fact's; otherwise the template is.
func TestAIWriter(t *testing.T) {
	grounded := func(in Input) (Output, error) {
		return Output{Title: "Note: " + in.Title, Body: in.Body}, nil
	}
	cases := []struct {
		name  string
		on    bool
		reply func(Input) (Output, error)
		want  string
		calls bool
		miss  string // the ai_text_misses reason kept per card, "" for none
	}{
		{"off", false, grounded, "rule", false, ""},
		{"grounded", true, grounded, "ai", true, ""},
		{"ungrounded", true, func(in Input) (Output, error) {
			return Output{Title: in.Title, Body: in.Body + " Sales up 99,999%."}, nil
		}, "rule", true, "grounding"},
		{"schema", true, func(Input) (Output, error) { return Output{Title: "", Body: "x"}, nil }, "rule", true, "schema"},
		{"provider_down", true, func(Input) (Output, error) { return Output{}, errors.New("connection refused") }, "rule", true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.setting(t, settings.AiCoachEnabled, map[bool]string{true: "true", false: "false"}[c.on])
			e.llm.reply = c.reply
			sum, err := e.service(t, true).RunStore(e.ctx, detectors.Daily, storeSL)
			require.NoError(t, err)
			require.Positive(t, sum.Created)
			require.Equal(t, c.calls, e.llm.calls > 0)
			var misses int
			require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT count(*) FROM ai_text_misses WHERE feature = 'coach_card' AND reason = $1`, c.miss).Scan(&misses))
			if c.miss != "" {
				require.Equal(t, e.llm.calls, misses, "every failed card is kept as a miss")
			}
			var bg, owner int
			require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT coalesce(sum(calls) FILTER (WHERE scope = 'store_background'), 0)::int, coalesce(sum(calls) FILTER (WHERE scope = 'store'), 0)::int FROM ai_usage_daily`).Scan(&bg, &owner))
			require.Equal(t, e.llm.calls, bg, "coach wording counts against the background budget (D-228)")
			require.Zero(t, owner)
			for _, x := range e.cards(t, storeSL) {
				require.Equal(t, c.want, x.GeneratedBy, x.Detector)
				if c.want == "ai" {
					require.Contains(t, x.Title, "Note: ")
					require.Equal(t, "fake-fast", *x.Model)
					require.NotNil(t, x.PromptVersion)
				} else {
					require.Nil(t, x.Model)
					require.NotContains(t, x.Body, "99,999")
				}
			}
		})
	}
}

// D-228: a card whose wording failed is not sent again while its facts hold;
// changed numbers ask once more. A spent background budget keeps the template.
func TestAIWriterMissCache(t *testing.T) {
	e := newEnv(t)
	e.setting(t, settings.AiCoachEnabled, "true")
	e.llm.reply = func(in Input) (Output, error) {
		return Output{Title: in.Title, Body: in.Body + " Up 99,999%."}, nil
	}
	s := e.service(t, true)
	f := detectors.Fact{Detector: "selling_fast", Subject: "item-1", Kind: "restock"}
	tpl := Text{Title: "Restock Amul Butter", Body: "3 days of stock left."}
	nums := []byte(`{"days":3}`)
	_, _, ok := s.writeAI(e.ctx, storeSL, f, "en", tpl, nums)
	require.False(t, ok)
	require.Equal(t, 1, e.llm.calls)
	_, _, ok = s.writeAI(e.ctx, storeSL, f, "en", tpl, nums)
	require.False(t, ok)
	require.Equal(t, 1, e.llm.calls, "a cached miss is not asked again")

	e.llm.reply = func(in Input) (Output, error) { return Output{Title: in.Title, Body: in.Body}, nil }
	tpl2 := Text{Title: tpl.Title, Body: "2 days of stock left."}
	_, _, ok = s.writeAI(e.ctx, storeSL, f, "en", tpl2, []byte(`{"days":2}`))
	require.True(t, ok)
	require.Equal(t, 2, e.llm.calls)

	e.setting(t, settings.AiDailyCallsStoreBackground, "0")
	_, _, ok = s.writeAI(e.ctx, storeSL, f, "en", Text{Title: tpl.Title, Body: "1 day of stock left."}, []byte(`{"days":1}`))
	require.False(t, ok, "a spent budget keeps the template")
	require.Equal(t, 2, e.llm.calls)
}

// A card closed because its condition resolved is neutral for the
// detector's weight; only a card that ran out unacted counts as ignored.
func TestResolvedIsNotIgnored(t *testing.T) {
	e := newEnv(t)
	s := e.service(t, false)
	_, err := s.RunStore(e.ctx, detectors.Daily, storeSL)
	require.NoError(t, err)
	live := e.cards(t, storeSL)
	require.GreaterOrEqual(t, len(live), 2)
	resolved, due := live[0], live[1]
	ignored := func(det string) (n int) {
		require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT coalesce((SELECT ignored FROM coach_weights WHERE store_id = $1 AND detector = $2), 0)`, ids.UUID(storeSL), det).Scan(&n))
		return n
	}
	weight := func(det string) (n int) {
		require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT coalesce((SELECT weight_bp FROM coach_weights WHERE store_id = $1 AND detector = $2), 10000)`, ids.UUID(storeSL), det).Scan(&n))
		return n
	}
	w0, i0 := weight(resolved.Detector), ignored(resolved.Detector)

	// The subject no longer fires: the card closes, the weight stays.
	e.exec(t, `UPDATE coach_suggestions SET subject_ref = 'resolved-subject' WHERE id = $1`, resolved.ID)
	_, err = s.RunStore(e.ctx, detectors.Daily, storeSL)
	require.NoError(t, err)
	var status string
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT status FROM coach_suggestions WHERE id = $1`, resolved.ID).Scan(&status))
	require.Equal(t, "expired", status)
	require.Equal(t, w0, weight(resolved.Detector))
	require.Equal(t, i0, ignored(resolved.Detector))

	// A card that ran out unacted is an ignore.
	i1 := ignored(due.Detector)
	e.exec(t, `UPDATE coach_suggestions SET valid_until = $2 WHERE id = $1`, due.ID, e.clk.Now().Add(-time.Second))
	_, err = s.RunStore(e.ctx, detectors.Daily, storeSL)
	require.NoError(t, err)
	require.Equal(t, i1+1, ignored(due.Detector))
}

// TestFeed: List marks new cards seen; Done and Dismiss are one-way, repeat
// safely and answer not_found for another store's card.
func TestFeed(t *testing.T) {
	e := newEnv(t)
	s := e.service(t, false)
	_, err := s.RunStore(e.ctx, detectors.Daily, storeSL)
	require.NoError(t, err)

	live := coachapi.CoachCardFilter("live")
	l, err := s.List(e.ctx, storeSL, live, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, l.Items)
	for _, x := range e.cards(t, storeSL) {
		require.Equal(t, "seen", x.Status)
		require.NotNil(t, x.SeenAt)
	}

	one := 1
	p1, err := s.List(e.ctx, storeSL, live, &one, nil)
	require.NoError(t, err)
	require.Len(t, p1.Items, 1)
	if len(l.Items) > 1 {
		require.NotNil(t, p1.NextCursor)
		p2, err := s.List(e.ctx, storeSL, live, &one, p1.NextCursor)
		require.NoError(t, err)
		require.Len(t, p2.Items, 1)
		require.NotEqual(t, p1.Items[0].Id, p2.Items[0].Id)
	}

	id := mustID(l.Items[0].Id)
	c, err := s.Done(e.ctx, owner, storeSL, id)
	require.NoError(t, err)
	require.Equal(t, coachapi.CoachCardStatus("done"), c.Status)
	c, err = s.Done(e.ctx, owner, storeSL, id)
	require.NoError(t, err, "done twice is the same answer")
	require.Equal(t, coachapi.CoachCardStatus("done"), c.Status)

	_, err = s.Dismiss(e.ctx, owner, storeSL, id)
	var pe *problem.Error
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "invalid_transition", pe.Code)

	_, err = s.Done(e.ctx, owner, storeSL2, id)
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "not_found", pe.Code)

	d, err := s.List(e.ctx, storeSL, coachapi.CoachCardFilter("done"), nil, nil)
	require.NoError(t, err)
	require.Len(t, d.Items, 1)
}

// TestRunBatchedCheckpoint: a batched run covers every live store once per
// period; a rerun of the same period resumes after the last one.
func TestRunBatchedCheckpoint(t *testing.T) {
	e := newEnv(t)
	s := e.service(t, false)
	sum, err := s.RunBatched(e.ctx, detectors.Daily, "2026-01-05")
	require.NoError(t, err)
	require.GreaterOrEqual(t, sum.Stores, int64(2))
	require.Positive(t, sum.Created)
	cp, err := coachdb.New(e.tdb.Pool).Checkpoint(e.ctx, coachdb.CheckpointParams{Job: string(detectors.Daily), Period: "2026-01-05"})
	require.NoError(t, err)
	require.EqualValues(t, sum.Stores, cp.Stores)

	again, err := s.RunBatched(e.ctx, detectors.Daily, "2026-01-05")
	require.NoError(t, err)
	require.Zero(t, again.Stores)
	require.Zero(t, again.Created)
}

// TestRealtime: the realtime run reaches only stores over the expiry
// threshold and writes the penalty nudge.
func TestRealtime(t *testing.T) {
	e := newEnv(t)
	s := e.service(t, false)
	sum, err := s.RunRealtime(e.ctx)
	require.NoError(t, err)
	require.Zero(t, sum.Created)

	e.exec(t, `UPDATE stores SET unaccepted_expiry_rate_30d = 0.4 WHERE id = $1`, ids.UUID(storeSL))
	sum, err = s.RunRealtime(e.ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, sum.Stores)
	require.EqualValues(t, 1, sum.Created)
	cs := e.cards(t, storeSL)
	require.Len(t, cs, 1)
	require.Equal(t, "expiry_penalty", cs[0].Detector)
	require.Equal(t, "turn_on_auto_accept", cs[0].ActionType)

	// Back under the threshold: the card expires on the next run.
	e.exec(t, `UPDATE stores SET unaccepted_expiry_rate_30d = 0.1 WHERE id = $1`, ids.UUID(storeSL))
	e.clk.Advance(15 * time.Minute)
	_, err = s.RunStore(e.ctx, detectors.Realtime, storeSL)
	require.NoError(t, err)
	require.Equal(t, "expired", e.cards(t, storeSL)[0].Status)
}

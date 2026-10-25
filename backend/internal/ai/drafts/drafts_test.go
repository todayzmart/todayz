package drafts

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

var t0 = time.Date(2026, 10, 25, 6, 0, 0, 0, time.UTC)

type env struct {
	t     *testing.T
	tdb   *testkit.DB
	s     *Service
	actor ids.ID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tdb := testkit.NewDB(t)
	e := &env{t: t, tdb: tdb, s: New(db.New(tdb.Pool), clock.NewFake(t0))}
	e.actor = tdb.InsertUser(t, ids.Zero)
	return e
}

type line struct {
	LineID string `json:"lineId"`
	Name   string `json:"name"`
	Qty    int    `json:"qty"`
}

type out struct {
	Lines []line `json:"lines"`
}

var threeLines = out{Lines: []line{{"l1", "rice", 10}, {"l2", "dal", 2}, {"l3", "oil", 1}}}

// ready creates a draft and marks it ready with output.
func (e *env) ready(output any) ids.ID {
	e.t.Helper()
	ctx := context.Background()
	id, err := e.s.Create(ctx, NewDraft{Feature: "voice_stock", Actor: e.actor, InputRef: "uploads/a.webm",
		InputMeta: map[string]any{"lang": "hi"}, PromptVersion: "1-abcdef12"})
	require.NoError(e.t, err)
	ok, err := e.s.MarkReady(ctx, id, output, client.Response{Model: "claude-haiku-5-5", CostPaise: 3,
		Usage: client.Usage{InputTokens: 900, OutputTokens: 120}, Latency: 1200 * time.Millisecond})
	require.NoError(e.t, err)
	require.True(e.t, ok)
	return id
}

func code(t *testing.T, err error) string {
	t.Helper()
	var pe *problem.Error
	require.ErrorAs(t, err, &pe)
	return pe.Code
}

// echo applies every decision as asked and counts its calls.
type echo struct{ calls int }

func (a *echo) apply(_ context.Context, _ Draft, ds []Decision) ([]LineResult, error) {
	a.calls++
	r := make([]LineResult, len(ds))
	for i, d := range ds {
		r[i] = LineResult{LineID: d.LineID, Outcome: d.Outcome}
	}
	return r, nil
}

// SC-AI-03: a draft changes nothing until a person confirms it, line by line. Lines the person
// did not mention are discarded, and a repeated confirm returns the same outcomes without
// applying again.
func TestConfirmFlow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.ready(threeLines)

	d, err := e.s.Get(ctx, "voice_stock", id)
	require.NoError(t, err)
	assert.Equal(t, StatusReady, d.Status)
	assert.Equal(t, "claude-haiku-5-5", d.Model)
	assert.Equal(t, int64(3), d.CostPaise)
	var o out
	require.NoError(t, d.Decode(&o))
	assert.Equal(t, threeLines, o)

	a := &echo{}
	c := Confirmation{Feature: "voice_stock", DraftID: id, By: e.actor, Apply: a.apply, Decisions: []Decision{
		{LineID: "l1", Outcome: Accepted},
		{LineID: "l2", Outcome: Edited, Value: json.RawMessage(`{"qty":3}`)},
	}}
	res, err := e.s.Confirm(ctx, c)
	require.NoError(t, err)
	assert.Equal(t, StatusConfirmed, res.Status)
	assert.Equal(t, []LineResult{{"l1", Accepted}, {"l2", Edited}, {"l3", Discarded}}, res.Lines)
	assert.Equal(t, 1, a.calls)

	again, err := e.s.Confirm(ctx, c)
	require.NoError(t, err)
	assert.Equal(t, res.Lines, again.Lines)
	assert.Equal(t, 1, a.calls, "a repeat does not apply again")

	d, err = e.s.Get(ctx, "voice_stock", id)
	require.NoError(t, err)
	assert.Equal(t, StatusConfirmed, d.Status)
	assert.Equal(t, e.actor, d.ConfirmedBy)
	require.NotNil(t, d.ConfirmedAt)

	// A retried job cannot overwrite a closed draft.
	ok, err := e.s.MarkReady(ctx, id, threeLines, client.Response{})
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = e.s.MarkFailed(ctx, id, "", client.Response{})
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestAllDiscarded(t *testing.T) {
	e := newEnv(t)
	id := e.ready(threeLines)
	a := &echo{}
	res, err := e.s.Confirm(context.Background(), Confirmation{Feature: "voice_stock", DraftID: id, By: e.actor, Apply: a.apply,
		Decisions: []Decision{{LineID: "l2", Outcome: Discarded}}})
	require.NoError(t, err)
	assert.Equal(t, StatusDiscarded, res.Status)
	assert.Zero(t, a.calls, "nothing to apply")
	assert.Equal(t, []LineResult{{"l1", Discarded}, {"l2", Discarded}, {"l3", Discarded}}, res.Lines)
}

// A line the confirmer may not make is recorded as not_permitted; if no line went through the
// draft is discarded.
func TestNotPermitted(t *testing.T) {
	e := newEnv(t)
	id := e.ready(threeLines)
	deny := func(_ context.Context, _ Draft, ds []Decision) ([]LineResult, error) {
		r := make([]LineResult, len(ds))
		for i, d := range ds {
			r[i] = LineResult{LineID: d.LineID, Outcome: NotPermitted}
		}
		return r, nil
	}
	res, err := e.s.Confirm(context.Background(), Confirmation{Feature: "voice_stock", DraftID: id, By: e.actor, Apply: deny,
		Decisions: []Decision{{LineID: "l1", Outcome: Accepted}}})
	require.NoError(t, err)
	assert.Equal(t, StatusDiscarded, res.Status)
	assert.Equal(t, NotPermitted, res.Lines[0].Outcome)

	// An Apply that answers a different outcome is a bug and rolls back.
	id = e.ready(threeLines)
	wrong := func(_ context.Context, _ Draft, ds []Decision) ([]LineResult, error) {
		return []LineResult{{LineID: ds[0].LineID, Outcome: Edited}}, nil
	}
	_, err = e.s.Confirm(context.Background(), Confirmation{Feature: "voice_stock", DraftID: id, By: e.actor, Apply: wrong,
		Decisions: []Decision{{LineID: "l1", Outcome: Accepted}}})
	require.Error(t, err)
	d, err := e.s.Get(context.Background(), "voice_stock", id)
	require.NoError(t, err)
	assert.Equal(t, StatusReady, d.Status)
}

func TestConfirmRefusals(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.ready(threeLines)
	a := &echo{}
	conf := func(ds ...Decision) error {
		_, err := e.s.Confirm(ctx, Confirmation{Feature: "voice_stock", DraftID: id, By: e.actor, Apply: a.apply, Decisions: ds})
		return err
	}
	assert.Equal(t, problems.ValidationFailed, code(t, conf()))
	assert.Equal(t, problems.ValidationFailed, code(t, conf(Decision{LineID: "l9", Outcome: Accepted})))
	assert.Equal(t, problems.ValidationFailed, code(t, conf(Decision{LineID: "l1", Outcome: Accepted}, Decision{LineID: "l1", Outcome: Discarded})))
	assert.Equal(t, problems.ValidationFailed, code(t, conf(Decision{LineID: "l1", Outcome: Edited})))
	assert.Equal(t, problems.ValidationFailed, code(t, conf(Decision{LineID: "l1", Outcome: Accepted, Value: json.RawMessage(`1`)})))
	assert.Equal(t, problems.ValidationFailed, code(t, conf(Decision{LineID: "l1", Outcome: NotPermitted})))
	assert.Zero(t, a.calls)

	// Another feature's draft is the common 404, also through Get.
	_, err := e.s.Confirm(ctx, Confirmation{Feature: "supplier_bill", DraftID: id, By: e.actor, Apply: a.apply,
		Decisions: []Decision{{LineID: "l1", Outcome: Accepted}}})
	assert.Equal(t, problems.NotFound, code(t, err))
	_, err = e.s.Get(ctx, "supplier_bill", id)
	assert.Equal(t, problems.NotFound, code(t, err))
	_, err = e.s.Get(ctx, "voice_stock", ids.New(t0))
	assert.Equal(t, problems.NotFound, code(t, err))

	// The caller's ownership check runs first.
	_, err = e.s.Confirm(ctx, Confirmation{Feature: "voice_stock", DraftID: id, By: e.actor, Apply: a.apply,
		Check:     func(Draft) error { return problem.New(problems.NotFound, "") },
		Decisions: []Decision{{LineID: "l1", Outcome: Accepted}}})
	assert.Equal(t, problems.NotFound, code(t, err))

	// A processing or failed draft cannot be confirmed.
	pid, err := e.s.Create(ctx, NewDraft{Feature: "voice_stock", Actor: e.actor, PromptVersion: "1-abcdef12"})
	require.NoError(t, err)
	_, err = e.s.Confirm(ctx, Confirmation{Feature: "voice_stock", DraftID: pid, By: e.actor, Apply: a.apply,
		Decisions: []Decision{{LineID: "l1", Outcome: Accepted}}})
	assert.Equal(t, problems.InvalidTransition, code(t, err))
	ok, err := e.s.MarkFailed(ctx, pid, "grounding", client.Response{Model: "claude-haiku-5-5"})
	require.NoError(t, err)
	assert.True(t, ok)
	d, err := e.s.Get(ctx, "voice_stock", pid)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, d.Status)
	assert.Equal(t, "grounding", d.Failure)
	_, err = e.s.Confirm(ctx, Confirmation{Feature: "voice_stock", DraftID: pid, By: e.actor, Apply: a.apply,
		Decisions: []Decision{{LineID: "l1", Outcome: Accepted}}})
	assert.Equal(t, problems.InvalidTransition, code(t, err))
}

func TestOutputShapes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	// An output with no lines is one line, l1.
	id := e.ready(map[string]any{"text": "Thank you for the review."})
	res, err := e.s.Confirm(ctx, Confirmation{Feature: "voice_stock", DraftID: id, By: e.actor, Apply: (&echo{}).apply,
		Decisions: []Decision{{LineID: "l1", Outcome: Edited, Value: json.RawMessage(`{"text":"Thanks!"}`)}}})
	require.NoError(t, err)
	assert.Equal(t, StatusConfirmed, res.Status)

	pid, err := e.s.Create(ctx, NewDraft{Feature: "voice_stock", Actor: e.actor, PromptVersion: "1-abcdef12"})
	require.NoError(t, err)
	for name, bad := range map[string]any{
		"not an object": []int{1},
		"no lines":      out{Lines: []line{}},
		"repeated id":   out{Lines: []line{{"l1", "a", 1}, {"l1", "b", 2}}},
		"bad id":        out{Lines: []line{{"x1", "a", 1}}},
	} {
		_, err := e.s.MarkReady(ctx, pid, bad, client.Response{})
		assert.Error(t, err, name)
	}

	_, err = e.s.Create(ctx, NewDraft{Feature: "not_a_feature", Actor: e.actor, PromptVersion: "1"})
	assert.Error(t, err)
	_, err = e.s.Create(ctx, NewDraft{Feature: "voice_stock", PromptVersion: "1"})
	assert.Error(t, err)
}

// byRole applies the lines whose name is in allowed and answers not_permitted for the rest,
// recording every line it was asked to apply.
type byRole struct {
	allowed map[string]bool
	seen    []string
}

func (a *byRole) apply(_ context.Context, d Draft, ds []Decision) ([]LineResult, error) {
	var o out
	if err := d.Decode(&o); err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, l := range o.Lines {
		names[l.LineID] = l.Name
	}
	r := make([]LineResult, len(ds))
	for i, dc := range ds {
		a.seen = append(a.seen, dc.LineID)
		r[i] = LineResult{LineID: dc.LineID, Outcome: dc.Outcome}
		if !a.allowed[names[dc.LineID]] {
			r[i].Outcome = NotPermitted
		}
	}
	return r, nil
}

// KeepOpen: a confirm that leaves not_permitted lines keeps the draft ready with the applied
// lines settled; a later confirm decides only the open lines (a settled line is never applied
// again, whatever the request says), and the draft closes once nothing is open.
func TestKeepOpen(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.ready(threeLines)
	all := []Decision{{LineID: "l1", Outcome: Accepted}, {LineID: "l2", Outcome: Accepted}, {LineID: "l3", Outcome: Discarded}}

	cashier := &byRole{allowed: map[string]bool{"rice": true}}
	c := Confirmation{Feature: "voice_stock", DraftID: id, By: e.actor, Apply: cashier.apply, Decisions: all, KeepOpen: true}
	res, err := e.s.Confirm(ctx, c)
	require.NoError(t, err)
	assert.Equal(t, StatusReady, res.Status)
	assert.Equal(t, []LineResult{{"l1", Accepted}, {"l2", NotPermitted}, {"l3", Discarded}}, res.Lines)
	assert.Equal(t, []string{"l1", "l2"}, cashier.seen)
	d, err := e.s.Get(ctx, "voice_stock", id)
	require.NoError(t, err)
	assert.Equal(t, StatusReady, d.Status)
	assert.Equal(t, res.Lines, d.Lines)
	assert.Nil(t, d.ConfirmedAt)

	// The same request again: l1 is settled, l2 is still refused; nothing changes.
	again, err := e.s.Confirm(ctx, c)
	require.NoError(t, err)
	assert.Equal(t, res, again)
	assert.Equal(t, []string{"l1", "l2", "l2"}, cashier.seen, "the settled line is not applied again")

	// Only settled lines in the request: answered as they stand, nothing applied.
	c.Decisions = []Decision{{LineID: "l1", Outcome: Discarded}}
	same, err := e.s.Confirm(ctx, c)
	require.NoError(t, err)
	assert.Equal(t, res, same)

	// A manager sends every line again; only l2 is applied, and the draft closes.
	manager := &byRole{allowed: map[string]bool{"rice": true, "dal": true, "oil": true}}
	by := e.tdb.InsertUser(t, ids.Zero)
	m := Confirmation{Feature: "voice_stock", DraftID: id, By: by, Apply: manager.apply, KeepOpen: true,
		Decisions: []Decision{{LineID: "l1", Outcome: Accepted}, {LineID: "l2", Outcome: Edited, Value: json.RawMessage(`{"qty":4}`)}, {LineID: "l3", Outcome: Accepted}}}
	done, err := e.s.Confirm(ctx, m)
	require.NoError(t, err)
	assert.Equal(t, StatusConfirmed, done.Status)
	assert.Equal(t, []LineResult{{"l1", Accepted}, {"l2", Edited}, {"l3", Discarded}}, done.Lines)
	assert.Equal(t, []string{"l2"}, manager.seen)
	d, err = e.s.Get(ctx, "voice_stock", id)
	require.NoError(t, err)
	assert.Equal(t, StatusConfirmed, d.Status)
	assert.Equal(t, by, d.ConfirmedBy)

	// A replay after the close answers the closed result.
	replay, err := e.s.Confirm(ctx, c)
	require.NoError(t, err)
	assert.Equal(t, done, replay)
	assert.Equal(t, []string{"l2"}, manager.seen)

	// Discarding the open lines also closes it; with nothing applied it is discarded.
	id = e.ready(threeLines)
	none := &byRole{}
	_, err = e.s.Confirm(ctx, Confirmation{Feature: "voice_stock", DraftID: id, By: e.actor, Apply: none.apply, KeepOpen: true,
		Decisions: []Decision{{LineID: "l1", Outcome: Accepted}}})
	require.NoError(t, err)
	closed, err := e.s.Confirm(ctx, Confirmation{Feature: "voice_stock", DraftID: id, By: e.actor, Apply: none.apply, KeepOpen: true,
		Decisions: []Decision{{LineID: "l1", Outcome: Discarded}}})
	require.NoError(t, err)
	assert.Equal(t, StatusDiscarded, closed.Status)
	assert.Equal(t, []LineResult{{"l1", Discarded}, {"l2", Discarded}, {"l3", Discarded}}, closed.Lines)
}

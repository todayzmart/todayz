package business

// A-15: an agent's own numbers, and every agent's for admins. Seven queries
// for any number of agents.

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	adminfieldapi "github.com/todayz/backend/gen/api/admin-field"
	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// statWeeks is how many IST weeks (Monday first) the weekly series covers,
// this week included.
const statWeeks = 8 //settings:ok A-15's chart length, not a product rule

// weekStartIST is the Monday 00:00 IST of t's week.
func weekStartIST(t time.Time) time.Time {
	d := clock.StartOfDayIST(t).In(clock.IST)
	back := (int(d.Weekday()) + 6) % 7 // Monday is 0
	return d.AddDate(0, 0, -back)
}

func (f *Field) stats(ctx context.Context, agents []pgtype.UUID, names map[pgtype.UUID]*string) ([]adminfieldapi.FieldAgentStats, error) {
	out := make([]adminfieldapi.FieldAgentStats, 0, len(agents))
	if len(agents) == 0 {
		return out, nil
	}
	w, err := f.window(ctx)
	if err != nil {
		return nil, err
	}
	now := f.s.clock.Now()
	first := weekStartIST(now).AddDate(0, 0, -7*(statWeeks-1))
	q := f.s.q(ctx)

	counts, err := q.FieldStatusCounts(ctx, agents)
	if err != nil {
		return nil, fmt.Errorf("business: field status counts: %w", err)
	}
	live, err := q.FieldStoresLive(ctx, agents)
	if err != nil {
		return nil, fmt.Errorf("business: field stores live: %w", err)
	}
	items, err := q.FieldItemsAdded(ctx, agents)
	if err != nil {
		return nil, fmt.Errorf("business: field items added: %w", err)
	}
	inWindow, err := q.FieldInWindow(ctx, businessdb.FieldInWindowParams{Agents: agents, VerifiedAfter: now.Add(-w)})
	if err != nil {
		return nil, fmt.Errorf("business: field in window: %w", err)
	}
	orders, err := q.FieldFirstOrders(ctx, agents)
	if err != nil {
		return nil, fmt.Errorf("business: field first orders: %w", err)
	}
	weekly, err := q.FieldWeekly(ctx, businessdb.FieldWeeklyParams{Agents: agents, Since: first})
	if err != nil {
		return nil, fmt.Errorf("business: field weekly: %w", err)
	}

	byAgent := map[pgtype.UUID]*adminfieldapi.FieldAgentStats{}
	weekIndex := map[string]int{}
	for i := range statWeeks {
		weekIndex[first.AddDate(0, 0, 7*i).Format(time.DateOnly)] = i
	}
	for _, a := range agents {
		id, _ := ids.FromUUID(a)
		st := adminfieldapi.FieldAgentStats{AgentId: id.String(), Name: names[a], Weekly: make([]adminfieldapi.FieldWeek, statWeeks)}
		for i := range st.Weekly {
			st.Weekly[i].WeekStart = openapi_types.Date{Time: first.AddDate(0, 0, 7*i)}
		}
		out = append(out, st)
	}
	for i, a := range agents {
		byAgent[a] = &out[i]
	}
	for _, r := range counts {
		st := byAgent[r.AgentID]
		if st == nil {
			continue
		}
		n := int(r.N)
		st.Businesses.Total += n
		switch r.Status {
		case businessDraft:
			st.Businesses.Draft += n
		case businessSubmitted:
			st.Businesses.Submitted += n
		case businessNeedsInfo:
			st.Businesses.NeedsInfo += n
		case businessVerified:
			st.Businesses.Verified += n
		case businessRejected:
			st.Businesses.Rejected += n
		case businessSuspended:
			st.Businesses.Suspended += n
		}
	}
	for _, r := range live {
		if st := byAgent[r.AgentID]; st != nil {
			st.StoresLive = int(r.N)
		}
	}
	for _, r := range items {
		if st := byAgent[r.AgentID]; st != nil {
			st.ItemsAdded = int(r.N)
		}
	}
	for _, r := range inWindow {
		if st := byAgent[r.AgentID]; st != nil {
			st.InWindow = int(r.N)
		}
	}
	for _, r := range orders {
		if st := byAgent[r.AgentID]; st != nil && r.Verified > 0 {
			rate := float32(r.WithOrder) / float32(r.Verified)
			st.FirstOrderRate = &rate
		}
	}
	for _, r := range weekly {
		st := byAgent[r.AgentID]
		if st == nil || !r.WeekStart.Valid {
			continue
		}
		i, ok := weekIndex[r.WeekStart.Time.Format(time.DateOnly)]
		if !ok {
			continue
		}
		switch r.Kind {
		case "onboarded":
			st.Weekly[i].Onboarded = int(r.N)
		case "verified":
			st.Weekly[i].Verified = int(r.N)
		case "items":
			st.Weekly[i].ItemsAdded = int(r.N)
		}
	}
	return out, nil
}

func (f *Field) myStats(ctx context.Context) (adminfieldapi.FieldMyStats, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldMyStats{}, err
	}
	me := ids.UUID(c.p.UserID)
	name, err := f.s.q(ctx).UserName(ctx, me)
	if err != nil {
		return adminfieldapi.FieldMyStats{}, fmt.Errorf("business: field name: %w", err)
	}
	st, err := f.stats(ctx, []pgtype.UUID{me}, map[pgtype.UUID]*string{me: name})
	if err != nil {
		return adminfieldapi.FieldMyStats{}, err
	}
	return adminfieldapi.FieldMyStats{ServerNow: f.s.clock.Now().UTC(), Stats: st[0]}, nil
}

// agents is every field agent's numbers (admins only, by the policy).
func (f *Field) agents(ctx context.Context) (adminfieldapi.FieldAgentStatsList, error) {
	if _, err := f.call(ctx); err != nil {
		return adminfieldapi.FieldAgentStatsList{}, err
	}
	rows, err := f.s.q(ctx).FieldAgents(ctx)
	if err != nil {
		return adminfieldapi.FieldAgentStatsList{}, fmt.Errorf("business: field agents: %w", err)
	}
	agents := make([]pgtype.UUID, 0, len(rows))
	names := map[pgtype.UUID]*string{}
	for _, r := range rows {
		agents = append(agents, r.UserID)
		names[r.UserID] = r.Name
	}
	st, err := f.stats(ctx, agents, names)
	if err != nil {
		return adminfieldapi.FieldAgentStatsList{}, err
	}
	return adminfieldapi.FieldAgentStatsList{Items: st, ServerNow: f.s.clock.Now().UTC()}, nil
}

package insights

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	insightsapi "github.com/todayz/backend/gen/api/insights"
	"github.com/todayz/backend/gen/sqlc/insightsdb"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// ListOpts are the GET {S}/insights parameters.
type ListOpts struct {
	All    bool
	Kind   *facts.Kind
	Limit  *int
	Cursor *string
}

// quiet is the store's subjects still quiet at now.
func quiet(ctx context.Context, q *insightsdb.Queries, store pgtype.UUID, now time.Time) (facts.Quiet, error) {
	rows, err := q.QuietSubjects(ctx, insightsdb.QuietSubjectsParams{StoreID: store, Now: now})
	if err != nil {
		return nil, fmt.Errorf("insights: quiet subjects: %w", err)
	}
	out := facts.Quiet{}
	for _, r := range rows {
		if k, ok := facts.KindOf(r.Kind); ok {
			out.Add(k, r.SubjectRef)
		}
	}
	return out, nil
}

// List is the B-03 card (default) or the B-12 Insights tab (All).
func (s *Service) List(ctx context.Context, storeID ids.ID, o ListOpts) (insightsapi.InsightList, error) {
	if err := s.ready(ctx); err != nil {
		return insightsapi.InsightList{}, err
	}
	lim, off, err := page(o.Limit, o.Cursor)
	if err != nil {
		return insightsapi.InsightList{}, err
	}
	now := s.now()
	qt, err := quiet(ctx, s.q(ctx), ids.UUID(storeID), now)
	if err != nil {
		return insightsapi.InsightList{}, err
	}
	res, err := facts.Compute(ctx, s.source(ctx), storeID, now, qt)
	if err != nil {
		return insightsapi.InsightList{}, err
	}
	rows, needs := res.Rows, res.Needs
	if o.Kind != nil {
		rows = filter(rows, func(r facts.Row) bool { return r.Kind == *o.Kind })
		needs = filter(needs, func(n facts.Need) bool { return n.Kind == *o.Kind })
	}
	out := insightsapi.InsightList{
		Items:     []insightsapi.Insight{},
		Period:    insightsapi.InsightPeriod{Days: int(res.PeriodDays), Label: fmt.Sprintf("Last %d days", res.PeriodDays)},
		ServerNow: now,
	}
	if !o.All {
		// The card: firing rows only, never a needs-data line (IN-18).
		for _, r := range facts.Card(rows, cardRows) {
			out.Items = append(out.Items, rowOut(r))
		}
		return out, nil
	}
	all := grouped(rows, needs)
	from, to, next := window(len(all), lim, off)
	out.Items = append(out.Items, all[from:to]...)
	out.NextCursor = next
	return out, nil
}

// grouped is every firing row grouped by kind in the 01 §12 order (rows of a
// kind in their sorted order), with one needs-data entry for a kind that has
// no row and is below its minimum evidence.
func grouped(rows []facts.Row, needs []facts.Need) []insightsapi.Insight {
	byKind := map[facts.Kind][]facts.Row{}
	for _, r := range rows {
		byKind[r.Kind] = append(byKind[r.Kind], r)
	}
	need := map[facts.Kind]facts.Need{}
	for _, n := range needs {
		if _, ok := need[n.Kind]; !ok {
			need[n.Kind] = n
		}
	}
	var out []insightsapi.Insight
	for _, k := range facts.Kinds {
		if rs := byKind[k]; len(rs) > 0 {
			for _, r := range rs {
				out = append(out, rowOut(r))
			}
			continue
		}
		if n, ok := need[k]; ok {
			out = append(out, needOut(n))
		}
	}
	return out
}

func rowOut(r facts.Row) insightsapi.Insight {
	sub, sev, f := r.SubjectRef, insightsapi.InsightSeverity(r.Severity), r.Facts
	if f == nil {
		f = map[string]any{}
	}
	payload := r.Action.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	return insightsapi.Insight{
		Kind: insightsapi.InsightKind(r.Kind), SubjectRef: &sub, Severity: &sev,
		Evidence: insightsapi.InsightEvidence{State: insightsapi.Ready},
		Facts:    &f,
		Action:   &insightsapi.InsightAction{Type: insightsapi.InsightActionType(r.Action.Type), Payload: payload},
	}
}

func needOut(n facts.Need) insightsapi.Insight {
	params := n.Params
	if params == nil {
		params = map[string]any{}
	}
	return insightsapi.Insight{
		Kind: insightsapi.InsightKind(n.Kind),
		Evidence: insightsapi.InsightEvidence{
			State: insightsapi.NeedsData, Needs: &insightsapi.InsightNeeds{Key: n.Key, Params: params},
		},
	}
}

func filter[T any](xs []T, keep func(T) bool) []T {
	var out []T
	for _, x := range xs {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}

// Dismiss is "Not useful" on one subject of one kind: that subject stays
// out of the store's insights for coach_dismiss_quiet_days (D-99). A
// going-to-waste row quiets every batch it lists.
func (s *Service) Dismiss(ctx context.Context, userID, storeID ids.ID, kind, subject string) (time.Time, error) {
	if err := s.ready(ctx); err != nil {
		return time.Time{}, err
	}
	k, ok := facts.KindOf(kind)
	if !ok {
		return time.Time{}, fieldErr("body.kind", "not an insight kind")
	}
	if !k.Dismissible() {
		return time.Time{}, fieldErr("body.kind", "expired on shelf cannot be dismissed")
	}
	now := s.now()
	q := s.q(ctx)
	store := ids.UUID(storeID)
	if err := validSubject(ctx, q, store, k, subject); err != nil {
		return time.Time{}, err
	}
	subjects := []string{subject}
	if k == facts.GoingToWaste {
		// The row's subject is its first batch; quiet every batch the row lists.
		res, err := facts.Compute(ctx, s.source(ctx), storeID, now, nil)
		if err != nil {
			return time.Time{}, err
		}
		for _, r := range res.Rows {
			if r.Kind == k && contains(r.Quiets, subject) {
				subjects = r.Quiets
				break
			}
		}
	}
	quietFor, err := s.settings.Duration(ctx, settings.CoachDismissQuietDays)
	if err != nil {
		return time.Time{}, fmt.Errorf("insights: %s: %w", settings.CoachDismissQuietDays, err)
	}
	var until time.Time
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		for _, sub := range subjects {
			u, err := q.UpsertFeedback(ctx, insightsdb.UpsertFeedbackParams{
				ID: ids.UUID(s.ids.New(now)), StoreID: store, UserID: ids.UUID(userID),
				Kind: string(k), SubjectRef: sub, QuietUntil: now.Add(quietFor), Now: now,
			})
			if err != nil {
				return fmt.Errorf("insights: feedback: %w", err)
			}
			if sub == subject {
				until = u
			}
		}
		return nil
	})
	if err != nil {
		return time.Time{}, err
	}
	return until, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// validSubject checks the subject's shape for the kind and that it is this
// store's: an item, a batch, a pair "a:b", a product or "q:<query>" for
// wanted nearby, or "store" for the store-wide kinds.
func validSubject(ctx context.Context, q *insightsdb.Queries, store pgtype.UUID, k facts.Kind, subject string) error {
	const path = "body.subjectRef"
	bad := fieldErr(path, "not a subject of this kind in this store")
	if k.StoreWide() {
		if subject != facts.StoreSubject {
			return bad
		}
		return nil
	}
	if subject == facts.StoreSubject {
		return bad
	}
	item := func(s string) (bool, error) {
		if !ids.Valid(s) {
			return false, nil
		}
		id, _ := ids.Parse(s)
		if id.String() != s {
			return false, nil
		}
		ok, err := q.StoreItemExists(ctx, insightsdb.StoreItemExistsParams{ID: ids.UUID(id), StoreID: store})
		if err != nil {
			return false, fmt.Errorf("insights: item: %w", err)
		}
		return ok, nil
	}
	var ok bool
	var err error
	switch k {
	case facts.GoingToWaste:
		id, perr := ids.Parse(subject)
		if perr != nil || id.String() != subject {
			return bad
		}
		rows, qerr := q.StoreBatchItems(ctx, insightsdb.StoreBatchItemsParams{StoreID: store, Ids: []pgtype.UUID{ids.UUID(id)}})
		if qerr != nil {
			return fmt.Errorf("insights: batch: %w", qerr)
		}
		ok = len(rows) == 1
	case facts.BoughtTogether:
		a, b, cut := strings.Cut(subject, ":")
		if !cut {
			return bad
		}
		ia, pa := ids.Parse(a)
		ib, pb := ids.Parse(b)
		if pa != nil || pb != nil || ia == ib || facts.PairSubject(ia, ib) != subject {
			return bad
		}
		if ok, err = item(a); err == nil && ok {
			ok, err = item(b)
		}
	case facts.WantedNearby:
		if query, isQuery := strings.CutPrefix(subject, "q:"); isQuery {
			ok = strings.TrimSpace(query) != ""
			break
		}
		id, perr := ids.Parse(subject)
		if perr != nil || id.String() != subject {
			return bad
		}
		ok, err = q.ProductExists(ctx, ids.UUID(id))
		if err != nil {
			err = fmt.Errorf("insights: product: %w", err)
		}
	default:
		ok, err = item(subject)
	}
	if err != nil {
		return err
	}
	if !ok {
		return bad
	}
	return nil
}

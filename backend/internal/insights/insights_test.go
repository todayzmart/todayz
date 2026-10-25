package insights

import (
	"context"
	"crypto/rand"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	insightsapi "github.com/todayz/backend/gen/api/insights"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/seed"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// The dev seed's stores and owner (internal/seed/business.go, identity.go).
var (
	storeSL  = mustID("01KE0S1W6W8ZV7T1C3A6MS0001")
	storeSL2 = mustID("01KE0S1W6W8ZV7T1C3A6MS0002")
	owner    = mustID("01KE0S1W6W8ZV7T1C3A6M9N011")
)

// seeded is the dev seed with an insights service at the seed's now.
type seeded struct {
	svc *Service
	tdb *testkit.DB
	clk *clock.Fake
	ctx context.Context
}

func newSeeded(t *testing.T) *seeded {
	t.Helper()
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	enc, hash := make([]byte, 32), make([]byte, 32)
	_, err := rand.Read(enc)
	require.NoError(t, err)
	_, err = rand.Read(hash)
	require.NoError(t, err)
	keys := config.AppKeys{PIIEncKey: config.NewSecret(enc), PIIHashKey: config.NewSecret(hash)}
	require.NoError(t, seed.Run(ctx, config.EnvTest, seed.Dev, seed.Deps{DB: db.New(tdb.Pool), Keys: keys}))
	seal, err := pii.NewSealer(enc)
	require.NoError(t, err)
	clk := clock.NewFake(seed.Epoch)
	svc, err := New(Deps{DB: db.New(tdb.Pool), Clock: clk, Seal: seal,
		Settings: settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})})
	require.NoError(t, err)
	return &seeded{svc: svc, tdb: tdb, clk: clk, ctx: ctx}
}

func (s *seeded) all(t *testing.T, store ids.ID) []insightsapi.Insight {
	t.Helper()
	l, err := s.svc.List(s.ctx, store, ListOpts{All: true, Limit: ptr(maxLimit)})
	require.NoError(t, err)
	require.Nil(t, l.NextCursor)
	return l.Items
}

func ptr[T any](v T) *T { return &v }

func kindsOf(items []insightsapi.Insight) map[insightsapi.InsightKind]int {
	out := map[insightsapi.InsightKind]int{}
	for _, it := range items {
		out[it.Kind]++
	}
	return out
}

func requireField(t *testing.T, err error, path string) {
	t.Helper()
	var p *problem.Error
	require.True(t, errors.As(err, &p), "err = %v", err)
	require.Equal(t, genproblems.ValidationFailed, p.Code)
	require.Contains(t, p.Fields, path)
}

func mustID(s string) ids.ID {
	id, err := ids.Parse(s)
	if err != nil {
		panic(err)
	}
	return id
}

// The card is at most 4 firing rows, one per kind; the tab is every row in
// the 01 §12 kind order, paged by an opaque cursor.
func TestListCardAndTab(t *testing.T) {
	s := newSeeded(t)
	card, err := s.svc.List(s.ctx, storeSL2, ListOpts{})
	require.NoError(t, err)
	require.Len(t, card.Items, cardRows)
	require.Equal(t, 7, card.Period.Days)
	for _, it := range card.Items {
		require.Equal(t, insightsapi.Ready, it.Evidence.State)
		require.NotNil(t, it.Action)
	}

	all := s.all(t, storeSL2)
	require.Len(t, all, 13)
	last := -1
	for _, it := range all {
		k, ok := facts.KindOf(string(it.Kind))
		require.True(t, ok)
		require.GreaterOrEqual(t, k.Order(), last, "kinds out of order at %s", it.Kind)
		last = k.Order()
	}

	var paged []insightsapi.Insight
	var cur *string
	for range 10 {
		l, err := s.svc.List(s.ctx, storeSL2, ListOpts{All: true, Limit: ptr(5), Cursor: cur})
		require.NoError(t, err)
		require.LessOrEqual(t, len(l.Items), 5)
		paged = append(paged, l.Items...)
		if cur = l.NextCursor; cur == nil {
			break
		}
	}
	require.Equal(t, all, paged)

	kk := facts.SittingOnShelf
	l, err := s.svc.List(s.ctx, storeSL2, ListOpts{All: true, Kind: &kk})
	require.NoError(t, err)
	require.Len(t, l.Items, 2)

	// SL is below the bill and linked-customer minimums: the tab says what
	// is missing; the card never shows a needs-data line (IN-18).
	sl := s.all(t, storeSL)
	needs := map[insightsapi.InsightKind]bool{}
	for _, it := range sl {
		if it.Evidence.State == insightsapi.NeedsData {
			require.NotNil(t, it.Evidence.Needs)
			needs[it.Kind] = true
		}
	}
	require.Equal(t, map[insightsapi.InsightKind]bool{
		insightsapi.InsightKind(facts.BoughtTogether): true, insightsapi.InsightKind(facts.RegularsSlipping): true,
	}, needs)
	slCard, err := s.svc.List(s.ctx, storeSL, ListOpts{})
	require.NoError(t, err)
	for _, it := range slCard.Items {
		require.Equal(t, insightsapi.Ready, it.Evidence.State)
	}

	_, err = s.svc.List(s.ctx, storeSL2, ListOpts{All: true, Cursor: ptr("bm90IGEgY3Vyc29y")})
	requireField(t, err, "query.cursor")
}

func subjectOf(t *testing.T, items []insightsapi.Insight, k facts.Kind) string {
	t.Helper()
	for _, it := range items {
		if string(it.Kind) == string(k) && it.SubjectRef != nil {
			return *it.SubjectRef
		}
	}
	t.Fatalf("no %s row", k)
	return ""
}

// "Not useful" quiets one subject for coach_dismiss_quiet_days; a going to
// waste row quiets every batch it lists; expired on shelf is never quieted.
func TestDismiss(t *testing.T) {
	s := newSeeded(t)
	all := s.all(t, storeSL2)

	shelf := subjectOf(t, all, facts.SittingOnShelf)
	until, err := s.svc.Dismiss(s.ctx, owner, storeSL2, string(facts.SittingOnShelf), shelf)
	require.NoError(t, err)
	require.True(t, seed.Epoch.Add(7*24*time.Hour).Equal(until), "until = %s", until) //settings:ok coach_dismiss_quiet_days default
	// A repeat while quiet keeps the first quiet_until.
	s.clk.Advance(time.Hour)
	again, err := s.svc.Dismiss(s.ctx, owner, storeSL2, string(facts.SittingOnShelf), shelf)
	require.NoError(t, err)
	require.True(t, until.Equal(again), "again = %s", again)

	waste := subjectOf(t, all, facts.GoingToWaste)
	_, err = s.svc.Dismiss(s.ctx, owner, storeSL2, string(facts.GoingToWaste), waste)
	require.NoError(t, err)
	_, err = s.svc.Dismiss(s.ctx, owner, storeSL2, string(facts.RegularsSlipping), facts.StoreSubject)
	require.NoError(t, err)

	after := kindsOf(s.all(t, storeSL2))
	require.Equal(t, 1, after[insightsapi.InsightKind(facts.SittingOnShelf)])
	require.Zero(t, after[insightsapi.InsightKind(facts.GoingToWaste)], "every listed batch is quiet")
	require.Zero(t, after[insightsapi.InsightKind(facts.RegularsSlipping)])
	require.Equal(t, 1, after[insightsapi.InsightKind(facts.ExpiredOnShelf)])

	// The quiet period ends.
	s.clk.Set(until.Add(time.Minute))
	back := kindsOf(s.all(t, storeSL2))
	require.Equal(t, 2, back[insightsapi.InsightKind(facts.SittingOnShelf)])

	// The SL store's quiet subjects are its own.
	require.Equal(t, 1, kindsOf(s.all(t, storeSL))[insightsapi.InsightKind(facts.SittingOnShelf)])

	slItem := subjectOf(t, s.all(t, storeSL), facts.SittingOnShelf)
	expired := subjectOf(t, all, facts.ExpiredOnShelf)
	pair := subjectOf(t, all, facts.BoughtTogether)
	a, _, _ := strings.Cut(pair, ":")
	for name, c := range map[string]struct{ kind, subject, path string }{
		"unknown kind":        {"selling_slow", shelf, "body.kind"},
		"expired":             {string(facts.ExpiredOnShelf), expired, "body.kind"},
		"store-wide subject":  {string(facts.StockCheckDue), shelf, "body.subjectRef"},
		"item kind on store":  {string(facts.SellingFast), facts.StoreSubject, "body.subjectRef"},
		"another store item":  {string(facts.SittingOnShelf), slItem, "body.subjectRef"},
		"not an id":           {string(facts.SellingFast), "tata", "body.subjectRef"},
		"pair of one":         {string(facts.BoughtTogether), a + ":" + a, "body.subjectRef"},
		"item as batch":       {string(facts.GoingToWaste), shelf, "body.subjectRef"},
		"blank query":         {string(facts.WantedNearby), "q: ", "body.subjectRef"},
		"lowercase id":        {string(facts.SellingFast), strings.ToLower(shelf), "body.subjectRef"},
		"unknown product":     {string(facts.WantedNearby), ids.New(seed.Epoch).String(), "body.subjectRef"},
		"unknown store item":  {string(facts.MissedSales), ids.New(seed.Epoch).String(), "body.subjectRef"},
		"wanted nearby store": {string(facts.WantedNearby), facts.StoreSubject, "body.subjectRef"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.svc.Dismiss(s.ctx, owner, storeSL2, c.kind, c.subject)
			requireField(t, err, c.path)
		})
	}
	_, err = s.svc.Dismiss(s.ctx, owner, storeSL2, string(facts.WantedNearby), "q:horlicks")
	require.NoError(t, err, "a search with no product is dismissed by its query")
}

// B-09: slipping is the regulars behind the insight, by spend, with a
// masked phone and never a full name or number.
func TestCustomers(t *testing.T) {
	s := newSeeded(t)
	l, err := s.svc.Customers(s.ctx, storeSL2, insightsapi.Slipping, nil, nil)
	require.NoError(t, err)
	require.True(t, l.Enough)
	require.Equal(t, 8, l.Total)
	require.Len(t, l.Items, 8)
	require.Equal(t, 21, l.LapseDays)
	for i, c := range l.Items {
		if i > 0 {
			require.GreaterOrEqual(t, l.Items[i-1].SpendPaise, c.SpendPaise)
		}
		require.GreaterOrEqual(t, c.Visits, 3)
		require.True(t, c.LastVisitAt.Before(seed.Epoch.Add(-21*24*time.Hour)))
		require.NotNil(t, c.PhoneMasked)
		require.NotContains(t, *c.PhoneMasked, "9000000", "the number is masked")
	}

	all, err := s.svc.Customers(s.ctx, storeSL2, insightsapi.All, ptr(4), nil)
	require.NoError(t, err)
	require.Len(t, all.Items, 4)
	require.NotNil(t, all.NextCursor)
	require.GreaterOrEqual(t, all.Total, 11)
	for i := 1; i < len(all.Items); i++ {
		require.False(t, all.Items[i].LastVisitAt.After(all.Items[i-1].LastVisitAt))
	}

	// SL has too few linked customers: the slipping list stays empty.
	sl, err := s.svc.Customers(s.ctx, storeSL, insightsapi.Slipping, nil, nil)
	require.NoError(t, err)
	require.False(t, sl.Enough)
	require.Empty(t, sl.Items)
}

func TestOverviewAndAnalytics(t *testing.T) {
	s := newSeeded(t)
	o, err := s.svc.Overview(s.ctx, owner, storeSL2, insightsapi.OverviewRangeToday)
	require.NoError(t, err)
	require.Equal(t, "Lakshmi", o.OwnerName)
	require.Equal(t, "Sri Lakshmi 2", o.StoreName)
	require.Positive(t, o.ListedItems)
	require.Positive(t, o.Kpis.SellOutSoon, "Tata Tea runs out tomorrow")

	w, err := s.svc.Overview(s.ctx, owner, storeSL2, insightsapi.OverviewRangeN7d)
	require.NoError(t, err)
	require.Positive(t, w.Kpis.Bills)
	require.Positive(t, w.Kpis.SalesPaise)
	require.Equal(t, w.Kpis.SalesPaise/w.Kpis.Bills, w.Kpis.AvgBillPaise)

	a, err := s.svc.Analytics(s.ctx, storeSL2, insightsapi.AnalyticsRangeN30d, nil)
	require.NoError(t, err)
	require.NotNil(t, a.Daily)
	require.Len(t, *a.Daily, 30)
	m := insightsapi.Items
	top, err := s.svc.Analytics(s.ctx, storeSL2, insightsapi.AnalyticsRangeN7d, &m)
	require.NoError(t, err)
	require.NotNil(t, top.TopItems)
	require.NotEmpty(t, *top.TopItems)
}

// A service without the database answers 503 on every operation.
func TestUnconfigured(t *testing.T) {
	svc, err := New(Deps{Clock: clock.NewFake(seed.Epoch)})
	require.NoError(t, err)
	ctx := context.Background()
	_, err = svc.List(ctx, storeSL2, ListOpts{})
	var p *problem.Error
	require.True(t, errors.As(err, &p))
	require.Equal(t, genproblems.ServiceUnavailable, p.Code)
	_, err = New(Deps{})
	require.Error(t, err)
}

// Every operation has an action; owner and manager only (the cashier's
// refusal is in the policy golden, testdata/insights.golden).
func TestOpsCoverEveryOperation(t *testing.T) {
	it := reflect.TypeOf((*insightsapi.StrictServerInterface)(nil)).Elem()
	require.Equal(t, it.NumMethod(), len(Ops))
	for i := 0; i < it.NumMethod(); i++ {
		a := Ops[it.Method(i).Name]
		require.Contains(t, []string{policy.InsightsView, policy.CustomersView}, a, it.Method(i).Name)
	}
}

func TestHandlers(t *testing.T) {
	h := handlers{s: nil}
	_, err := h.GetStoreOverview(context.Background(), insightsapi.GetStoreOverviewRequestObject{Sid: storeSL2.String()})
	var p *problem.Error
	require.True(t, errors.As(err, &p))
	require.Equal(t, genproblems.Unauthorized, p.Code, "no principal")

	bad := insightsapi.InsightKind("selling_slow")
	_, err = h.ListStoreInsights(context.Background(), insightsapi.ListStoreInsightsRequestObject{
		Sid: storeSL2.String(), Params: insightsapi.ListStoreInsightsParams{Kind: &bad},
	})
	requireField(t, err, "query.kind")

	_, err = h.ListStoreInsights(context.Background(), insightsapi.ListStoreInsightsRequestObject{Sid: "nope"})
	require.True(t, errors.As(err, &p))
	require.Equal(t, genproblems.NotFound, p.Code)
}

func TestPage(t *testing.T) {
	lim, off, err := page(nil, nil)
	require.NoError(t, err)
	require.Equal(t, defaultLimit, lim)
	require.Zero(t, off)
	lim, _, _ = page(ptr(500), nil)
	require.Equal(t, maxLimit, lim)
	lim, _, _ = page(ptr(0), nil)
	require.Equal(t, 1, lim)

	from, to, next := window(13, 5, 0)
	require.Equal(t, [2]int{0, 5}, [2]int{from, to})
	require.NotNil(t, next)
	_, off, err = page(ptr(5), next)
	require.NoError(t, err)
	require.Equal(t, 5, off)
	from, to, next = window(13, 5, 10)
	require.Equal(t, [2]int{10, 13}, [2]int{from, to})
	require.Nil(t, next)
	from, to, _ = window(3, 5, 10)
	require.Equal(t, [2]int{3, 3}, [2]int{from, to}, "an offset past the end is an empty page")

	for _, c := range []string{"!!", "eDox", "bzotMQ", "bzp4"} { // bad base64, "x:1", "o:-1", "o:x"
		_, _, err := page(nil, &c)
		requireField(t, err, "query.cursor")
	}
}

func TestShortName(t *testing.T) {
	for in, want := range map[string]string{
		"":                 "",
		"Ananya":           "Ananya",
		"Ananya Rao":       "Ananya R.",
		"  Ravi  Kumar S ": "Ravi S.",
		"Priya शर्मा":      "Priya श.",
		"ராஜா குமார்":      "ராஜா கு.",
	} {
		require.Equal(t, want, shortName(in), in)
	}
	require.Equal(t, "Lakshmi", firstName(" Lakshmi Devi"))
}

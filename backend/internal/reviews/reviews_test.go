package reviews

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	reviewsapi "github.com/todayz/backend/gen/api/reviews"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

// Reviews and ratings (08 SC-RV and RV-01; 01 §10; D-41, D-59, D-60).

func TestMain(m *testing.M) { testkit.Main(m) }

var testNow = time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)

// fakeSales stands in for W2-04's pos_sales lookup.
type fakeSales map[ids.ID]Sale

func (f fakeSales) Sale(_ context.Context, id ids.ID) (Sale, bool, error) {
	s, ok := f[id]
	return s, ok, nil
}

type env struct {
	t     *testing.T
	tdb   *testkit.DB
	svc   *Service
	cache *settings.Cache
	clk   *clock.Fake
	sales fakeSales
	ctx   context.Context
	store ids.ID
	biz   ids.ID
	slug  string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	e := &env{t: t, tdb: tdb, ctx: context.Background(), clk: clock.NewFake(testNow), sales: fakeSales{}}
	e.cache = settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	d := db.New(tdb.Pool)
	var err error
	e.svc, err = New(Deps{DB: d, Clock: e.clk, Settings: e.cache, Audit: audit.New(d, e.clk, nil), Sales: e.sales})
	require.NoError(t, err)
	// Orders are another domain's table: the tests insert them in any status, so W2-02's
	// transition guard is off in this per-test database.
	e.exec(`ALTER TABLE orders DISABLE TRIGGER trg_orders_guard`)
	e.biz, e.store, e.slug = e.newStore("lakshmi-traders", "ABCDEH", "ABCDEF", "verified", "live")
	return e
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.tdb.OwnerPool.Exec(e.ctx, sql, args...)
	require.NoError(e.t, err, sql)
}

func (e *env) newStore(slug, code, ref, bizStatus, status string) (biz, store ids.ID, s string) {
	biz, store = ids.New(testNow), ids.New(testNow)
	e.exec(`INSERT INTO businesses (id, brand_name, referral_code, status) VALUES ($1, 'Lakshmi Traders', $2, $3)`,
		ids.UUID(biz), ref, bizStatus)
	e.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, status, live_at) VALUES ($1, $2, $3, $4, 'Lakshmi Traders', $5, $6)`,
		ids.UUID(store), ids.UUID(biz), slug, code, status, testNow.Add(-24*time.Hour))
	return biz, store, slug
}

func (e *env) user() ids.ID {
	id := ids.New(testNow)
	e.exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(id))
	return id
}

func (e *env) orderIn(store, customer ids.ID, status string) ids.ID {
	id := ids.New(testNow)
	reason := any(nil)
	switch status {
	case "rejected":
		reason = "out_of_stock"
	case "cancelled":
		reason = "changed_mind"
	}
	e.exec(`INSERT INTO orders (id, customer_id, store_id, fulfilment, status, status_reason, subtotal_paise, items_after_offers_paise,
	  item_subtotal_paise, total_paise, code_enc, code_hmac, placed_at)
	  VALUES ($1, $2, $3, 'pickup', $4, $5, 1000, 1000, 1000, 1000, '\x00', sha256($7::text::bytea), $6)`,
		ids.UUID(id), ids.UUID(customer), ids.UUID(store), status, reason, testNow, id.String())
	return id
}

func (e *env) order(customer ids.ID, status string) ids.ID {
	return e.orderIn(e.store, customer, status)
}

func ulid(id ids.ID) *string { s := id.String(); return &s }

func (e *env) review(customer ids.ID, stars int) reviewsapi.MyReview {
	e.t.Helper()
	return e.reviewIn(e.store, customer, stars)
}

func (e *env) reviewIn(store, customer ids.ID, stars int) reviewsapi.MyReview {
	e.t.Helper()
	r, err := e.svc.create(e.ctx, customer, reviewsapi.ReviewCreate{OrderId: ulid(e.orderIn(store, customer, "collected")), Stars: stars})
	require.NoError(e.t, err)
	return r
}

func mustParse(t *testing.T, s string) ids.ID {
	t.Helper()
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return id
}

func (e *env) rating(store ids.ID) (avg *float64, count int) {
	e.t.Helper()
	var a pgtype.Numeric
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT rating_avg, rating_count FROM stores WHERE id = $1`, ids.UUID(store)).Scan(&a, &count))
	if !a.Valid {
		return nil, count
	}
	f, err := a.Float64Value()
	require.NoError(e.t, err)
	return &f.Float64, count
}

func (e *env) requireRating(store ids.ID, hundredths, count int) {
	e.t.Helper()
	avg, n := e.rating(store)
	require.NotNil(e.t, avg)
	assert.InDelta(e.t, float64(hundredths)/100, *avg, 0.0001)
	assert.Equal(e.t, count, n)
}

func requireProblem(t *testing.T, err error, code, path string) {
	t.Helper()
	require.Error(t, err)
	var p *problem.Error
	require.ErrorAs(t, err, &p)
	require.Equal(t, code, p.Code, err.Error())
	if path != "" {
		require.Contains(t, p.Fields, path)
	}
}

func (e *env) auditActions(subject string) []string {
	e.t.Helper()
	rows, err := e.tdb.OwnerPool.Query(e.ctx, `SELECT action FROM audit_log WHERE subject_id = $1 ORDER BY created_at, id`, subject)
	require.NoError(e.t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		require.NoError(e.t, rows.Scan(&a))
		out = append(out, a)
	}
	return out
}

// --- the formula ---------------------------------------------------------------------

func TestBayesAvg(t *testing.T) {
	_, ok := BayesAvg(0, 0)
	assert.False(t, ok, "no review, no rating")
	for _, c := range []struct{ n, total, want int }{
		{1, 5, 417},  // (20+5)/6 = 4.1666…
		{1, 1, 350},  // (20+1)/6 = 3.5
		{2, 10, 429}, // 30/7 = 4.2857…
		{3, 3, 288},  // 23/8 = 2.875 rounds half up
		{10, 50, 467},
		{100, 500, 495},
	} {
		got, ok := BayesAvg(c.n, c.total)
		assert.True(t, ok)
		assert.Equal(t, c.want, got, "n=%d total=%d", c.n, c.total)
	}
}

// the rating the database writes is the Go oracle's, for every shape of store.
func TestRatingMatchesOracle(t *testing.T) {
	e := newEnv(t)
	shapes := [][]int{{5}, {1}, {5, 5, 5}, {1, 2, 3}, {3, 3, 3}, {4, 5, 4, 5, 3, 1, 2}, {5, 5, 5, 5, 5, 5, 5, 5, 5, 4}}
	for i, stars := range shapes {
		_, store, _ := e.newStore(fmt.Sprintf("shape-%d", i), fmt.Sprintf("ABCD%dJ", i+2), fmt.Sprintf("REF%dKM", i+2), "verified", "live")
		total := 0
		for _, n := range stars {
			e.reviewIn(store, e.user(), n)
			total += n
		}
		want, _ := BayesAvg(len(stars), total)
		e.requireRating(store, want, len(stars))
	}
}

// --- eligibility -----------------------------------------------------

func TestEligibility(t *testing.T) {
	e := newEnv(t)
	u := e.user()

	for _, status := range []string{"collected", "delivered"} {
		r, err := e.svc.create(e.ctx, u, reviewsapi.ReviewCreate{OrderId: ulid(e.order(u, status)), Stars: 4})
		require.NoError(t, err, status)
		assert.Equal(t, 4, r.Stars)
		assert.False(t, r.Hidden)
	}
	for _, status := range []string{"placed", "accepted", "ready", "out_for_delivery", "rejected", "expired", "cancelled", "delivery_failed"} {
		_, err := e.svc.create(e.ctx, u, reviewsapi.ReviewCreate{OrderId: ulid(e.order(u, status)), Stars: 4})
		requireProblem(t, err, genproblems.ReviewNotEligible, "")
		assert.Equal(t, 409, problem.Status(err), status)
	}
	// Another customer's order and an unknown id both look like no order.
	_, err := e.svc.create(e.ctx, u, reviewsapi.ReviewCreate{OrderId: ulid(e.order(e.user(), "collected")), Stars: 4})
	requireProblem(t, err, genproblems.NotFound, "")
	_, err = e.svc.create(e.ctx, u, reviewsapi.ReviewCreate{OrderId: ulid(ids.New(testNow)), Stars: 4})
	requireProblem(t, err, genproblems.NotFound, "")
	// Exactly one of orderId and posSaleId.
	_, err = e.svc.create(e.ctx, u, reviewsapi.ReviewCreate{Stars: 4})
	requireProblem(t, err, genproblems.ValidationFailed, "body.orderId")
	_, err = e.svc.create(e.ctx, u, reviewsapi.ReviewCreate{OrderId: ulid(e.order(u, "collected")), PosSaleId: ulid(ids.New(testNow)), Stars: 4})
	requireProblem(t, err, genproblems.ValidationFailed, "body.orderId")
}

func TestOneReviewPerOrder(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	oid := e.order(u, "delivered")
	_, err := e.svc.create(e.ctx, u, reviewsapi.ReviewCreate{OrderId: ulid(oid), Stars: 5})
	require.NoError(t, err)
	_, err = e.svc.create(e.ctx, u, reviewsapi.ReviewCreate{OrderId: ulid(oid), Stars: 1})
	requireProblem(t, err, genproblems.ReviewExists, "")
	assert.Equal(t, 409, problem.Status(err))
	e.requireRating(e.store, 417, 1) // the second attempt changed nothing
}

func TestPOSSaleEligibility(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	sale := func(method string, completed bool, customer ids.ID) ids.ID {
		id := ids.New(testNow)
		e.sales[id] = Sale{StoreID: e.store, CustomerID: customer, LinkMethod: method, Completed: completed}
		return id
	}
	post := func(id ids.ID) error {
		_, err := e.svc.create(e.ctx, u, reviewsapi.ReviewCreate{PosSaleId: ulid(id), Stars: 5})
		return err
	}

	require.NoError(t, post(sale("bill_qr", true, u)))
	require.NoError(t, post(sale("counter_code", true, u)))
	requireProblem(t, post(sale("phone_unverified", true, u)), genproblems.ReviewNotEligible, "")
	requireProblem(t, post(sale("bill_qr", false, u)), genproblems.ReviewNotEligible, "")
	requireProblem(t, post(sale("bill_qr", true, e.user())), genproblems.NotFound, "")
	requireProblem(t, post(ids.New(testNow)), genproblems.NotFound, "")

	dup := sale("bill_qr", true, u)
	require.NoError(t, post(dup))
	requireProblem(t, post(dup), genproblems.ReviewExists, "")
	e.requireRating(e.store, 438, 3) // (20 + 15)/8 = 4.375 → half up

	// W2-04 not wired: a sale is never eligible.
	d := db.New(e.tdb.Pool)
	bare, err := New(Deps{DB: d, Clock: e.clk, Settings: e.cache})
	require.NoError(t, err)
	_, err = bare.create(e.ctx, u, reviewsapi.ReviewCreate{PosSaleId: ulid(dup), Stars: 5})
	requireProblem(t, err, genproblems.ReviewNotEligible, "")
}

func TestValidation(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	create := func(in reviewsapi.ReviewCreate) error {
		in.OrderId = ulid(e.order(u, "collected"))
		_, err := e.svc.create(e.ctx, u, in)
		return err
	}
	tags := func(t ...reviewsapi.ReviewTag) *reviewsapi.ReviewTags { r := t; return &r }
	long := strings.Repeat("a", 1001)
	ok := strings.Repeat("ऄ", 1000) // 1000 characters, more bytes

	requireProblem(t, create(reviewsapi.ReviewCreate{Stars: 0}), genproblems.ValidationFailed, "body.stars")
	requireProblem(t, create(reviewsapi.ReviewCreate{Stars: 6}), genproblems.ValidationFailed, "body.stars")
	requireProblem(t, create(reviewsapi.ReviewCreate{Stars: 3, Text: &long}), genproblems.ValidationFailed, "body.text")
	requireProblem(t, create(reviewsapi.ReviewCreate{Stars: 3, Tags: tags("fresh_stock", "slow")}), genproblems.ValidationFailed, "body.tags")
	requireProblem(t, create(reviewsapi.ReviewCreate{Stars: 3, Tags: tags("fresh_stock", "fresh_stock")}), genproblems.ValidationFailed, "body.tags[1]")
	requireProblem(t, create(reviewsapi.ReviewCreate{Stars: 3, Tags: tags("nonsense")}), genproblems.ValidationFailed, "body.tags[0]")
	require.NoError(t, create(reviewsapi.ReviewCreate{Stars: 3, Text: &ok, Tags: tags("slow", "rude")}))
	require.NoError(t, create(reviewsapi.ReviewCreate{Stars: 5, Tags: tags("fresh_stock", "fair_price", "quick_service", "friendly", "stock_was_right", "well_packed")}))
}

// --- edit window ------------------------------------------------------------

func TestEditWindowRV01(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	r := e.review(u, 2)
	id := mustParse(t, r.Id)
	assert.Equal(t, testNow.Add(7*24*time.Hour), r.EditableUntil.UTC())

	newText := "better than I thought"
	e.clk.Advance(3 * 24 * time.Hour)
	got, err := e.svc.update(e.ctx, u, id, reviewsapi.ReviewPatch{Stars: ptr(4), Text: &newText})
	require.NoError(t, err)
	assert.Equal(t, 4, got.Stars)
	require.NotNil(t, got.Text)
	assert.Equal(t, newText, *got.Text)
	assert.NotNil(t, got.EditedAt)
	e.requireRating(e.store, 400, 1) // (20+4)/6

	// A later change to the setting never reopens or closes this review.
	e.exec(`UPDATE platform_settings SET value = '1' WHERE key = 'review_edit_window_days'`)
	e.cache.Invalidate()
	_, err = e.svc.update(e.ctx, u, id, reviewsapi.ReviewPatch{Stars: ptr(5)})
	require.NoError(t, err)

	// The boundary: allowed up to and including editable-until, refused after.
	e.clk.Set(testNow.Add(7 * 24 * time.Hour))
	_, err = e.svc.update(e.ctx, u, id, reviewsapi.ReviewPatch{Stars: ptr(3)})
	require.NoError(t, err)
	e.clk.Advance(time.Second)
	_, err = e.svc.update(e.ctx, u, id, reviewsapi.ReviewPatch{Stars: ptr(1)})
	requireProblem(t, err, genproblems.ReviewEditWindowPassed, "")
	assert.Equal(t, 409, problem.Status(err))
	e.requireRating(e.store, 383, 1) // still the 3-star edit: 23/6
	var stored string
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT text FROM reviews WHERE id = $1`, ids.UUID(id)).Scan(&stored))
	assert.Equal(t, newText, stored, "a refused edit leaves the stored text unchanged")

	// The new window applies to the next review.
	r2 := e.review(e.user(), 5)
	assert.Equal(t, e.clk.Now().Add(24*time.Hour), r2.EditableUntil.UTC())
}

func TestEditRules(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	r := e.review(u, 2)
	id := mustParse(t, r.Id)

	_, err := e.svc.update(e.ctx, u, id, reviewsapi.ReviewPatch{})
	requireProblem(t, err, genproblems.ValidationFailed, "body")
	_, err = e.svc.update(e.ctx, u, id, reviewsapi.ReviewPatch{Stars: ptr(9)})
	requireProblem(t, err, genproblems.ValidationFailed, "body.stars")
	// Somebody else's review is no review.
	_, err = e.svc.update(e.ctx, e.user(), id, reviewsapi.ReviewPatch{Stars: ptr(5)})
	requireProblem(t, err, genproblems.NotFound, "")

	// Text can be set, then cleared with an empty string; tags are replaced.
	txt := "fine"
	got, err := e.svc.update(e.ctx, u, id, reviewsapi.ReviewPatch{Text: &txt, Tags: &reviewsapi.ReviewTags{"slow"}})
	require.NoError(t, err)
	require.NotNil(t, got.Text)
	empty := ""
	got, err = e.svc.update(e.ctx, u, id, reviewsapi.ReviewPatch{Text: &empty, Tags: &reviewsapi.ReviewTags{}})
	require.NoError(t, err)
	assert.Nil(t, got.Text)
	assert.Empty(t, got.Tags)
	assert.Equal(t, 2, got.Stars, "a patch leaves the other fields alone")

	// A hidden review is out of the customer's hands.
	_, err = e.svc.hide(e.ctx, e.user(), id, "abusive")
	require.NoError(t, err)
	_, err = e.svc.update(e.ctx, u, id, reviewsapi.ReviewPatch{Stars: ptr(5)})
	requireProblem(t, err, genproblems.InvalidTransition, "")
}

// --- reply --------------------------------------------------------------------

func TestReply(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	r := e.review(e.user(), 3)
	id := mustParse(t, r.Id)

	_, err := e.svc.reply(e.ctx, owner, e.store, id, "   ")
	requireProblem(t, err, genproblems.ValidationFailed, "")
	_, err = e.svc.reply(e.ctx, owner, e.store, id, strings.Repeat("x", 1001))
	requireProblem(t, err, genproblems.ValidationFailed, "body.text")

	rp, err := e.svc.reply(e.ctx, owner, e.store, id, "Thank you, we will do better")
	require.NoError(t, err)
	assert.Equal(t, "Thank you, we will do better", rp.Text)
	assert.Nil(t, rp.EditedAt)

	_, err = e.svc.reply(e.ctx, owner, e.store, id, "again")
	requireProblem(t, err, genproblems.ReviewReplyExists, "")
	assert.Equal(t, 409, problem.Status(err))

	e.clk.Advance(time.Hour)
	rp, err = e.svc.editReply(e.ctx, e.store, id, "Thanks, sorry about the wait")
	require.NoError(t, err)
	assert.Equal(t, "Thanks, sorry about the wait", rp.Text)
	assert.NotNil(t, rp.EditedAt)

	// The customer and the public see it.
	page, err := e.svc.listPublic(e.ctx, e.slug, nil, nil)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.Items[0].Reply)
	assert.Equal(t, "Thanks, sorry about the wait", page.Items[0].Reply.Text)

	// Another store's owner cannot reply to this review.
	_, otherStore, _ := e.newStore("ganesh", "ABCDEJ", "ABCDEG", "verified", "live")
	_, err = e.svc.reply(e.ctx, owner, otherStore, id, "mine")
	requireProblem(t, err, genproblems.NotFound, "")
	_, err = e.svc.editReply(e.ctx, otherStore, id, "mine")
	requireProblem(t, err, genproblems.NotFound, "")
}

// --- reports --------------------------------------------------------------------

func TestReports(t *testing.T) {
	e := newEnv(t)
	author := e.user()
	r := e.review(author, 1)
	id := mustParse(t, r.Id)
	other := e.user()

	_, err := e.svc.reportAsCustomer(e.ctx, other, id, "")
	requireProblem(t, err, genproblems.ValidationFailed, "")
	_, err = e.svc.reportAsCustomer(e.ctx, author, id, "spam")
	requireProblem(t, err, genproblems.ValidationFailed, "body.reviewId")
	_, err = e.svc.reportAsCustomer(e.ctx, other, ids.New(testNow), "spam")
	requireProblem(t, err, genproblems.NotFound, "")

	first, err := e.svc.reportAsCustomer(e.ctx, other, id, "fake review")
	require.NoError(t, err)
	assert.Equal(t, "open", string(first.Status))
	again, err := e.svc.reportAsCustomer(e.ctx, other, id, "fake review, again")
	require.NoError(t, err)
	assert.Equal(t, first.Id, again.Id, "reporting twice returns the first report")

	ownerID := e.user()
	st, err := e.svc.reportAsStore(e.ctx, ownerID, e.store, id, "never came to the shop")
	require.NoError(t, err)
	assert.NotEqual(t, first.Id, st.Id)
	var n int
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT count(*) FROM review_reports WHERE review_id = $1`, ids.UUID(id)).Scan(&n))
	assert.Equal(t, 2, n)

	// The shop's list says which reviews it already reported; the public one has no such field.
	page, err := e.svc.listForStore(e.ctx, e.store, nil, nil)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.True(t, page.Items[0].Reported)

	_, otherStore, _ := e.newStore("ganesh", "ABCDEJ", "ABCDEG", "verified", "live")
	_, err = e.svc.reportAsStore(e.ctx, ownerID, otherStore, id, "not my customer")
	requireProblem(t, err, genproblems.NotFound, "")
}

// --- moderation ------------------------------------------------------

func TestHideRestoreRecalculates(t *testing.T) {
	e := newEnv(t)
	admin := e.user()
	a := e.review(e.user(), 5)
	b := e.review(e.user(), 1)
	e.requireRating(e.store, 371, 2) // (20+6)/7 = 3.714…

	got, err := e.svc.hide(e.ctx, admin, mustParse(t, b.Id), "abusive language")
	require.NoError(t, err)
	assert.True(t, got.Hidden)
	require.NotNil(t, got.HideReason)
	assert.Equal(t, "abusive language", *got.HideReason)
	e.requireRating(e.store, 417, 1)

	// A hidden review is gone from every list and from the rating.
	pub, err := e.svc.listPublic(e.ctx, e.slug, nil, nil)
	require.NoError(t, err)
	require.Len(t, pub.Items, 1)
	assert.Equal(t, a.Id, pub.Items[0].Id)
	assert.Equal(t, 1, pub.Rating.Count)
	shop, err := e.svc.listForStore(e.ctx, e.store, nil, nil)
	require.NoError(t, err)
	assert.Len(t, shop.Items, 1)

	// Hiding the last visible review leaves no rating at all.
	_, err = e.svc.hide(e.ctx, admin, mustParse(t, a.Id), "duplicate")
	require.NoError(t, err)
	avg, n := e.rating(e.store)
	assert.Nil(t, avg)
	assert.Zero(t, n)
	pub, err = e.svc.listPublic(e.ctx, e.slug, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, pub.Items)
	assert.Nil(t, pub.Rating.Avg)

	// Restore brings the review and the rating back.
	got, err = e.svc.restore(e.ctx, admin, mustParse(t, b.Id), "customer clarified")
	require.NoError(t, err)
	assert.False(t, got.Hidden)
	assert.Nil(t, got.HideReason)
	e.requireRating(e.store, 350, 1)
	_, err = e.svc.restore(e.ctx, admin, mustParse(t, a.Id), "reviewed again")
	require.NoError(t, err)
	e.requireRating(e.store, 371, 2)
}

func TestModerationRules(t *testing.T) {
	e := newEnv(t)
	admin := e.user()
	r := e.review(e.user(), 4)
	id := mustParse(t, r.Id)

	_, err := e.svc.hide(e.ctx, admin, id, "  ")
	requireProblem(t, err, genproblems.ValidationFailed, "")
	_, err = e.svc.hide(e.ctx, admin, id, strings.Repeat("x", 501))
	requireProblem(t, err, genproblems.ValidationFailed, "")
	_, err = e.svc.hide(e.ctx, admin, ids.New(testNow), "x")
	requireProblem(t, err, genproblems.NotFound, "")
	_, err = e.svc.restore(e.ctx, admin, id, "not hidden")
	requireProblem(t, err, genproblems.InvalidTransition, "")
	_, err = e.svc.restore(e.ctx, admin, id, "")
	requireProblem(t, err, genproblems.ValidationFailed, "")

	_, err = e.svc.hide(e.ctx, admin, id, "spam")
	require.NoError(t, err)
	_, err = e.svc.hide(e.ctx, admin, id, "spam again")
	requireProblem(t, err, genproblems.InvalidTransition, "")
	assert.Equal(t, 409, problem.Status(err))
	_, err = e.svc.restore(e.ctx, admin, id, "fine after all")
	require.NoError(t, err)
}

func TestModerationIsAudited(t *testing.T) {
	e := newEnv(t)
	admin := e.user()
	r := e.review(e.user(), 4)
	id := mustParse(t, r.Id)
	_, err := e.svc.reply(e.ctx, e.user(), e.store, id, "thanks")
	require.NoError(t, err)

	_, err = e.svc.hide(e.ctx, admin, id, "spam")
	require.NoError(t, err)
	_, err = e.svc.restore(e.ctx, admin, id, "appeal accepted")
	require.NoError(t, err)
	_, err = e.svc.hideReply(e.ctx, admin, id, "offensive reply")
	require.NoError(t, err)
	_, err = e.svc.restoreReply(e.ctx, admin, id, "ok")
	require.NoError(t, err)

	assert.Equal(t, []string{"review.hide", "review.restore", "review.reply_hide", "review.reply_restore"}, e.auditActions(r.Id))

	var actor pgtype.UUID
	var after []byte
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx,
		`SELECT actor_id, after FROM audit_log WHERE subject_id = $1 AND action = 'review.hide'`, r.Id).Scan(&actor, &after))
	assert.Equal(t, ids.UUID(admin), actor)
	assert.Contains(t, string(after), "spam")

	// A refused action leaves no audit row and no change.
	before := len(e.auditActions(r.Id))
	_, err = e.svc.restore(e.ctx, admin, id, "not hidden")
	require.Error(t, err)
	assert.Len(t, e.auditActions(r.Id), before)
}

func TestHideReply(t *testing.T) {
	e := newEnv(t)
	admin := e.user()
	owner := e.user()
	cust := e.user()
	r := e.review(cust, 4)
	id := mustParse(t, r.Id)

	_, err := e.svc.hideReply(e.ctx, admin, id, "no reply yet")
	requireProblem(t, err, genproblems.NotFound, "")
	_, err = e.svc.reply(e.ctx, owner, e.store, id, "thanks")
	require.NoError(t, err)
	_, err = e.svc.hideReply(e.ctx, admin, id, "rude")
	require.NoError(t, err)

	pub, err := e.svc.listPublic(e.ctx, e.slug, nil, nil)
	require.NoError(t, err)
	require.Len(t, pub.Items, 1)
	assert.Nil(t, pub.Items[0].Reply, "a hidden reply is not shown")
	mine, err := e.svc.listMine(e.ctx, cust, nil, nil)
	require.NoError(t, err)
	require.Len(t, mine.Items, 1)
	assert.Nil(t, mine.Items[0].Reply)

	// The shop cannot post or edit around the takedown.
	_, err = e.svc.editReply(e.ctx, e.store, id, "edited")
	requireProblem(t, err, genproblems.InvalidTransition, "")
	_, err = e.svc.reply(e.ctx, owner, e.store, id, "new reply")
	requireProblem(t, err, genproblems.ReviewReplyExists, "")

	_, err = e.svc.restoreReply(e.ctx, admin, id, "ok")
	require.NoError(t, err)
	pub, err = e.svc.listPublic(e.ctx, e.slug, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, pub.Items[0].Reply)
}

func TestHideUpholdsOpenReports(t *testing.T) {
	e := newEnv(t)
	admin := e.user()
	r := e.review(e.user(), 1)
	id := mustParse(t, r.Id)
	rcpt, err := e.svc.reportAsCustomer(e.ctx, e.user(), id, "fake")
	require.NoError(t, err)
	_, err = e.svc.reportAsStore(e.ctx, e.user(), e.store, id, "never visited")
	require.NoError(t, err)

	list, err := e.svc.listReports(e.ctx, ptr("open"), nil, nil)
	require.NoError(t, err)
	require.Len(t, list.Items, 2)
	kinds := map[string]bool{}
	for _, it := range list.Items {
		kinds[string(it.ReporterKind)] = true
	}
	assert.Equal(t, map[string]bool{"customer": true, "store": true}, kinds)

	flagged, err := e.svc.listAdmin(e.ctx, ptr("reported"), nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, flagged.Items, 1)
	assert.Equal(t, 2, flagged.Items[0].OpenReports)

	_, err = e.svc.hide(e.ctx, admin, id, "fake review")
	require.NoError(t, err)
	list, err = e.svc.listReports(e.ctx, ptr("open"), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, list.Items)
	done, err := e.svc.listReports(e.ctx, ptr("resolved"), nil, nil)
	require.NoError(t, err)
	require.Len(t, done.Items, 2)
	for _, it := range done.Items {
		require.NotNil(t, it.Resolution)
		assert.Equal(t, "upheld", string(*it.Resolution))
	}
	hidden, err := e.svc.listAdmin(e.ctx, ptr("hidden"), nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, hidden.Items, 1)
	vis, err := e.svc.listAdmin(e.ctx, ptr("visible"), nil, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, vis.Items)

	// Resolving an already resolved report is refused.
	_, err = e.svc.resolveReport(e.ctx, admin, mustParse(t, rcpt.Id), "dismissed", nil)
	requireProblem(t, err, genproblems.InvalidTransition, "")
}

func TestResolveReport(t *testing.T) {
	e := newEnv(t)
	admin := e.user()
	r := e.review(e.user(), 5)
	id := mustParse(t, r.Id)
	rcpt, err := e.svc.reportAsCustomer(e.ctx, e.user(), id, "dislike")
	require.NoError(t, err)

	_, err = e.svc.resolveReport(e.ctx, admin, mustParse(t, rcpt.Id), "maybe", nil)
	require.Error(t, err)
	note := "reads as genuine"
	out, err := e.svc.resolveReport(e.ctx, admin, mustParse(t, rcpt.Id), "dismissed", &note)
	require.NoError(t, err)
	assert.Equal(t, "resolved", string(out.Status))
	require.NotNil(t, out.ResolutionNote)
	assert.Equal(t, note, *out.ResolutionNote)
	assert.Equal(t, []string{"review.report_resolve"}, e.auditActions(rcpt.Id))
	// Dismissing leaves the review up.
	e.requireRating(e.store, 417, 1)
	_, err = e.svc.resolveReport(e.ctx, admin, ids.New(testNow), "dismissed", nil)
	requireProblem(t, err, genproblems.NotFound, "")
}

// --- lists ----------------------------------------------------------------------------

func TestPublicListVisibility(t *testing.T) {
	e := newEnv(t)
	e.review(e.user(), 5)

	_, err := e.svc.listPublic(e.ctx, "no-such-shop", nil, nil)
	requireProblem(t, err, genproblems.NotFound, "")
	for _, c := range []struct{ slug, biz, store string }{
		{"paused-shop", "verified", "paused"},
		{"draft-shop", "verified", "draft"},
		{"unverified-shop", "submitted", "live"},
	} {
		_, _, slug := e.newStore(c.slug, "ABCD"+strings.ToUpper(c.slug[:1])+"Q", "R"+strings.ToUpper(c.slug[:1])+"23XY", c.biz, c.store)
		_, err := e.svc.listPublic(e.ctx, slug, nil, nil)
		requireProblem(t, err, genproblems.NotFound, "")
	}
}

func TestPaging(t *testing.T) {
	e := newEnv(t)
	for i := range 5 {
		e.clk.Advance(time.Minute)
		e.review(e.user(), 1+i)
	}
	lim := 2
	var seen []string
	var cursor *string
	for pages := 0; pages < 5; pages++ {
		p, err := e.svc.listPublic(e.ctx, e.slug, &lim, cursor)
		require.NoError(t, err)
		for _, it := range p.Items {
			seen = append(seen, it.Id)
		}
		assert.Equal(t, 5, p.Rating.Count)
		if p.NextCursor == nil {
			break
		}
		c := *p.NextCursor
		cursor = &c
	}
	require.Len(t, seen, 5)
	assert.Len(t, uniq(seen), 5, "no review twice")

	// Newest first.
	first, err := e.svc.listPublic(e.ctx, e.slug, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 5, first.Items[0].Stars)
	assert.Equal(t, 1, first.Items[4].Stars)
	assert.Nil(t, first.NextCursor)

	bad := "!!"
	_, err = e.svc.listPublic(e.ctx, e.slug, nil, &bad)
	requireProblem(t, err, genproblems.ValidationFailed, "query.cursor")
}

func uniq(in []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range in {
		m[s] = true
	}
	return m
}

func TestMyReviews(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.review(u, 5)
	e.review(u, 2)
	e.review(e.user(), 4)

	mine, err := e.svc.listMine(e.ctx, u, nil, nil)
	require.NoError(t, err)
	assert.Len(t, mine.Items, 2)
	for _, it := range mine.Items {
		assert.Equal(t, "Lakshmi Traders", it.StoreName)
	}
	// An admin takedown shows as hidden to its author.
	_, err = e.svc.hide(e.ctx, e.user(), mustParse(t, mine.Items[0].Id), "spam")
	require.NoError(t, err)
	mine, err = e.svc.listMine(e.ctx, u, nil, nil)
	require.NoError(t, err)
	hidden := 0
	for _, it := range mine.Items {
		if it.Hidden {
			hidden++
		}
	}
	assert.Equal(t, 1, hidden)
}

// --- the hourly job heals drift (D-59) ---------------------------------------------------------------------

func TestRecomputeAllHealsDrift(t *testing.T) {
	e := newEnv(t)
	e.review(e.user(), 5)
	e.review(e.user(), 4)
	_, empty, _ := e.newStore("empty-shop", "ABCD2J", "REF2KM", "verified", "live")

	e.exec(`UPDATE stores SET rating_avg = 1.00, rating_count = 99 WHERE id = $1`, ids.UUID(e.store))
	e.exec(`UPDATE stores SET rating_avg = 4.50, rating_count = 3 WHERE id = $1`, ids.UUID(empty))
	_, err := e.svc.RecomputeAll(e.ctx)
	require.NoError(t, err)

	want, _ := BayesAvg(2, 9)
	e.requireRating(e.store, want, 2)
	avg, n := e.rating(empty)
	assert.Nil(t, avg, "no visible review: no rating")
	assert.Zero(t, n)
}

// Writes to one store race: every create, edit and hide recomputes under the store's lock, so the
// rating that is stored is the oracle's for whatever ended up visible (D-59).
func TestConcurrentWritesKeepTheRatingExact(t *testing.T) {
	e := newEnv(t)
	admin := e.user()
	const n = 12
	errs := make(chan error, n)
	for i := range n {
		u := e.user()
		oid := e.order(u, "collected")
		go func() {
			stars := 1 + i%5
			r, err := e.svc.create(e.ctx, u, reviewsapi.ReviewCreate{OrderId: ulid(oid), Stars: stars})
			if err != nil {
				errs <- err
				return
			}
			id, err := ids.Parse(r.Id)
			if err != nil {
				errs <- err
				return
			}
			if i%4 == 0 { // some are hidden while others are still being written
				_, err = e.svc.hide(e.ctx, admin, id, "spam")
			} else if i%3 == 0 {
				stars = 1 + (stars+1)%5
				_, err = e.svc.update(e.ctx, u, id, reviewsapi.ReviewPatch{Stars: &stars})
			}
			errs <- err
		}()
	}
	for range n {
		require.NoError(t, <-errs)
	}
	var count, total int
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx,
		`SELECT count(*), COALESCE(sum(stars), 0) FROM reviews WHERE store_id = $1 AND hidden_at IS NULL`, ids.UUID(e.store)).Scan(&count, &total))
	want, ok := BayesAvg(count, total)
	require.True(t, ok)
	e.requireRating(e.store, want, count)
}

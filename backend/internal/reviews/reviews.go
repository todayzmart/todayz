// Package reviews is the customer's review of a shop, the shop's reply, the
// reports and the admin moderation of all three (W2-06; 01 §10, 03 reviews,
// review_replies and review_reports, 04 §2.4, D-41, D-59, D-60, D-92).
//
// Rules this package keeps:
//
//   - A review needs a finished sale: the caller's own order in `collected` or
//     `delivered`, or a POS sale linked to the caller by `bill_qr` or
//     `counter_code` (SaleSource, W2-04). Someone else's or an unknown order is
//     404; an order that is not finished is 409 review_not_eligible. One review
//     per order or sale (409 review_exists; the unique indexes are the proof).
//   - A review is published at once. The customer edits it until edited_until,
//     stored at creation as created + review_edit_window_days, so a later
//     settings change never reopens or closes an old review. After that an edit
//     is 409 review_edit_window_passed.
//   - The shop replies once and may edit the reply. Owner and manager only.
//   - A customer or a shop may report a visible review; one report per reporter,
//     a second one returns the first. Hiding a review closes its open reports as
//     upheld.
//   - Moderation is a soft delete (hidden_at / deleted_at with the actor and a
//     reason), restorable with a reason, and every hide and restore is an
//     audit_log row written in the same transaction (D-41).
//   - stores.rating_avg and rating_count have one writer, this package (D-59):
//     the Bayesian average over visible reviews (prior 4.0, weight 5, D-92),
//     NULL while there is none. Create, edit, hide and restore recompute it under
//     a per-store advisory lock; the hourly store_rating job heals any drift.
package reviews

import (
	"context"
	"encoding/base64"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	reviewsapi "github.com/todayz/backend/gen/api/reviews"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/reviewsdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Fixed constants (D-92): they define the rating formula and the contract, so
// they change through code review, not in the console.
const (
	// PriorAvg and PriorWeight are the Bayesian prior of the store rating (01 §10).
	PriorAvg    = "4.0" //settings:ok D-92 fixed constant, 01 §10 prior
	PriorWeight = 5     //settings:ok D-92 fixed constant, 01 §10 prior weight

	maxText = 1000 //settings:ok D-92 fixed constant, 01 §10 text length
	maxTags = 6    //settings:ok contract bound (ReviewTags maxItems)

	defaultLimit = 20  //settings:ok page size default, 04 §1
	maxLimit     = 100 //settings:ok page size cap, 04 §1
)

// SaleSource answers the one question reviews has for POS sales (W2-04 owns
// pos_sales): is this sale the caller's, and was it linked by bill_qr or
// counter_code? A nil SaleSource makes every POS sale not eligible.
type SaleSource interface {
	// Sale returns the sale, or ok false when there is none.
	Sale(ctx context.Context, saleID ids.ID) (s Sale, ok bool, err error)
}

// Sale is the part of a POS sale a review needs.
type Sale struct {
	StoreID    ids.ID
	CustomerID ids.ID // zero when no customer was linked
	// LinkMethod is pos_sales.link_method: bill_qr, counter_code or phone_unverified.
	LinkMethod string
	// Completed is false for a reversed sale.
	Completed bool
}

// Deps is what the service needs. Only Clock is required: without the
// database, settings or (for moderation) the audit writer the operations that
// need them answer 503.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	Audit    *audit.Writer
	// Sales is W2-04's lookup; nil until it lands.
	Sales SaleSource
}

// Service implements the customer, business and admin routes and the hourly job.
type Service struct {
	db       *db.DB
	clock    clock.Clock
	settings *settings.Cache
	audit    *audit.Writer
	sales    SaleSource
	ids      *ids.Generator
}

var errUnavailable = errors.New("reviews: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("reviews: a clock is required")
	}
	return &Service{db: d.DB, clock: d.Clock, settings: d.Settings, audit: d.Audit, sales: d.Sales, ids: ids.NewGenerator()}, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "reviews: not configured (needs the database and settings)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) q(ctx context.Context) *reviewsdb.Queries { return reviewsdb.New(s.db.Q(ctx)) }

// lockStore serialises the review writes of one store, so the rating
// recompute that follows sees every committed change.
func (s *Service) lockStore(ctx context.Context, storeID pgtype.UUID) error {
	return s.q(ctx).LockStoreReviews(ctx, idString(storeID))
}

// recompute writes stores.rating_avg and rating_count from the visible reviews.
func (s *Service) recompute(ctx context.Context, storeID pgtype.UUID) error {
	_, err := s.q(ctx).RecomputeStoreRating(ctx, reviewsdb.RecomputeStoreRatingParams{
		StoreID: storeID, PriorWeight: PriorWeight, PriorAvg: PriorAvg,
	})
	return err
}

// RecomputeAll heals every store's rating (the hourly store_rating job) and
// returns how many stores it changed.
func (s *Service) RecomputeAll(ctx context.Context) (int64, error) {
	if s.db == nil {
		return 0, errUnavailable
	}
	return s.q(ctx).RecomputeAllStoreRatings(ctx, reviewsdb.RecomputeAllStoreRatingsParams{
		PriorWeight: PriorWeight, PriorAvg: PriorAvg,
	})
}

// BayesAvg is the store rating the SQL computes, in hundredths (4.40 is 440),
// rounded half up, for n visible reviews whose stars add up to total. ok is
// false with no review (the rating is then NULL). The tests hold the SQL to it.
func BayesAvg(n, total int) (hundredths int, ok bool) {
	if n <= 0 {
		return 0, false
	}
	prior, _ := new(big.Rat).SetString(PriorAvg)
	num := new(big.Rat).Mul(prior, big.NewRat(PriorWeight, 1))
	num.Add(num, big.NewRat(int64(total), 1))
	num.Mul(num, big.NewRat(100, 1))
	num.Quo(num, big.NewRat(int64(PriorWeight+n), 1))
	// round half up on the exact value
	twice := new(big.Rat).Mul(num, big.NewRat(2, 1))
	f := new(big.Int).Div(twice.Num(), twice.Denom())
	f.Add(f, big.NewInt(1))
	f.Div(f, big.NewInt(2))
	return int(f.Int64()), true
}

// --- tags and text ---------------------------------------------------------------

var (
	positiveTags = map[string]bool{
		"fresh_stock": true, "fair_price": true, "quick_service": true,
		"friendly": true, "stock_was_right": true, "well_packed": true,
	}
	negativeTags = map[string]bool{
		"item_missing": true, "slow": true, "rude": true,
		"wrong_item": true, "expired_item": true, "overpriced": true,
	}
)

// cleanTags checks the tag list: known tags, no repeats, at most six, all
// positive or all negative (04 §2.4).
func cleanTags(in []reviewsapi.ReviewTag, path string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	var pos, neg bool
	for i, t := range in {
		v := string(t)
		switch {
		case positiveTags[v]:
			pos = true
		case negativeTags[v]:
			neg = true
		default:
			return nil, fieldErr(path+"["+strconv.Itoa(i)+"]", "unknown tag")
		}
		if seen[v] {
			return nil, fieldErr(path+"["+strconv.Itoa(i)+"]", "repeated")
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) > maxTags {
		return nil, fieldErr(path, "too many")
	}
	if pos && neg {
		return nil, fieldErr(path, "mixed positive and negative")
	}
	return out, nil
}

// cleanText trims the text; blank is no text (nil). Longer than maxText is 422.
func cleanText(t string, path string) (*string, error) {
	t = strings.TrimSpace(t)
	if t == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(t) > maxText {
		return nil, fieldErr(path, "too long")
	}
	return &t, nil
}

func cleanStars(n int, path string) (int16, error) {
	if n < 1 || n > 5 { //settings:ok contract bound (1–5 stars)
		return 0, fieldErr(path, "1 to 5")
	}
	return int16(n), nil //nolint:gosec // 1..5
}

// --- customer: create, edit, list --------------------------------------------------

func (s *Service) create(ctx context.Context, userID ids.ID, in reviewsapi.ReviewCreate) (reviewsapi.MyReview, error) {
	if err := s.ready(ctx); err != nil {
		return reviewsapi.MyReview{}, err
	}
	if (in.OrderId == nil) == (in.PosSaleId == nil) {
		return reviewsapi.MyReview{}, fieldErr("body.orderId", "send exactly one of orderId and posSaleId")
	}
	stars, err := cleanStars(in.Stars, "body.stars")
	if err != nil {
		return reviewsapi.MyReview{}, err
	}
	var text *string
	if in.Text != nil {
		if text, err = cleanText(*in.Text, "body.text"); err != nil {
			return reviewsapi.MyReview{}, err
		}
	}
	var tags []string
	if in.Tags != nil {
		if tags, err = cleanTags(*in.Tags, "body.tags"); err != nil {
			return reviewsapi.MyReview{}, err
		}
	}
	if tags == nil {
		tags = []string{}
	}
	days, err := s.settings.Int(ctx, settings.ReviewEditWindowDays)
	if err != nil {
		return reviewsapi.MyReview{}, errors.Join(errors.New("reviews: "+string(settings.ReviewEditWindowDays)), err)
	}
	now := s.now()
	until := now.Add(time.Duration(min(max(days, 0), 3650)) * 24 * time.Hour) //nolint:gosec // clamped

	var id ids.ID
	var out reviewsapi.MyReview
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		storeID, orderID, saleID, err := s.subject(ctx, userID, in)
		if err != nil {
			return err
		}
		if err := s.lockStore(ctx, storeID); err != nil {
			return err
		}
		id = s.ids.New(now)
		p := reviewsdb.InsertReviewParams{
			ID: ids.UUID(id), StoreID: storeID, CustomerID: ids.UUID(userID), OrderID: orderID, PosSaleID: saleID,
			Stars: stars, Text: text, Tags: tags, EditedUntil: until, Now: now,
		}
		if _, err := s.q(ctx).InsertReview(ctx, p); err != nil {
			if errors.Is(err, pgx.ErrNoRows) { // ON CONFLICT DO NOTHING: this order or sale has its review
				return problem.New(genproblems.ReviewExists, "")
			}
			return err
		}
		if err := s.recompute(ctx, storeID); err != nil {
			return err
		}
		out, err = s.myReview(ctx, userID, id)
		return err
	})
	if err != nil {
		return reviewsapi.MyReview{}, err
	}
	return out, nil
}

// subject resolves the order or POS sale a review is about and checks that
// the caller may review it. It returns the store and the order or sale id
// (the other one invalid).
func (s *Service) subject(ctx context.Context, userID ids.ID, in reviewsapi.ReviewCreate) (store, order, sale pgtype.UUID, err error) {
	if in.OrderId != nil {
		oid, perr := ids.Parse(*in.OrderId)
		if perr != nil {
			return store, order, sale, fieldErr("body.orderId", "invalid")
		}
		o, gerr := s.q(ctx).GetOrderForReview(ctx, ids.UUID(oid))
		if errors.Is(gerr, pgx.ErrNoRows) {
			return store, order, sale, notFound()
		}
		if gerr != nil {
			return store, order, sale, gerr
		}
		if o.CustomerID != ids.UUID(userID) {
			return store, order, sale, notFound() // another customer's order looks like no order
		}
		if o.Status != "collected" && o.Status != "delivered" {
			return store, order, sale, problem.New(genproblems.ReviewNotEligible, "")
		}
		return o.StoreID, ids.UUID(oid), pgtype.UUID{}, nil
	}
	sid, perr := ids.Parse(*in.PosSaleId)
	if perr != nil {
		return store, order, sale, fieldErr("body.posSaleId", "invalid")
	}
	if s.sales == nil {
		return store, order, sale, problem.New(genproblems.ReviewNotEligible, "")
	}
	ps, ok, gerr := s.sales.Sale(ctx, sid)
	if gerr != nil {
		return store, order, sale, gerr
	}
	if !ok || ps.CustomerID != userID {
		return store, order, sale, notFound()
	}
	if !ps.Completed || (ps.LinkMethod != "bill_qr" && ps.LinkMethod != "counter_code") {
		return store, order, sale, problem.New(genproblems.ReviewNotEligible, "")
	}
	return ids.UUID(ps.StoreID), pgtype.UUID{}, ids.UUID(sid), nil
}

func (s *Service) update(ctx context.Context, userID, reviewID ids.ID, in reviewsapi.ReviewPatch) (reviewsapi.MyReview, error) {
	if err := s.ready(ctx); err != nil {
		return reviewsapi.MyReview{}, err
	}
	if in.Stars == nil && in.Text == nil && in.Tags == nil {
		return reviewsapi.MyReview{}, fieldErr("body", "nothing to change")
	}
	var stars *int16
	if in.Stars != nil {
		v, err := cleanStars(*in.Stars, "body.stars")
		if err != nil {
			return reviewsapi.MyReview{}, err
		}
		stars = &v
	}
	var text **string // nil: leave; non-nil: set (the inner nil clears)
	if in.Text != nil {
		t, err := cleanText(*in.Text, "body.text")
		if err != nil {
			return reviewsapi.MyReview{}, err
		}
		text = &t
	}
	var tags []string
	if in.Tags != nil {
		t, err := cleanTags(*in.Tags, "body.tags")
		if err != nil {
			return reviewsapi.MyReview{}, err
		}
		tags = t
	}
	now := s.now()
	var out reviewsapi.MyReview
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.q(ctx).LockMyReview(ctx, reviewsdb.LockMyReviewParams{ID: ids.UUID(reviewID), CustomerID: ids.UUID(userID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if cur.HiddenAt != nil {
			return problem.New(genproblems.InvalidTransition, "") // an admin took it down; the customer cannot change it
		}
		if now.After(cur.EditedUntil) {
			return problem.New(genproblems.ReviewEditWindowPassed, "")
		}
		p := reviewsdb.UpdateMyReviewParams{ID: cur.ID, Stars: cur.Stars, Text: cur.Text, Tags: cur.Tags, Now: &now}
		if stars != nil {
			p.Stars = *stars
		}
		if text != nil {
			p.Text = *text
		}
		if in.Tags != nil {
			p.Tags = tags
		}
		if p.Tags == nil {
			p.Tags = []string{}
		}
		if err := s.lockStore(ctx, cur.StoreID); err != nil {
			return err
		}
		if err := s.q(ctx).UpdateMyReview(ctx, p); err != nil {
			return err
		}
		if err := s.recompute(ctx, cur.StoreID); err != nil {
			return err
		}
		out, err = s.myReview(ctx, userID, reviewID)
		return err
	})
	if err != nil {
		return reviewsapi.MyReview{}, err
	}
	return out, nil
}

func (s *Service) myReview(ctx context.Context, userID, id ids.ID) (reviewsapi.MyReview, error) {
	r, err := s.q(ctx).GetMyReview(ctx, reviewsdb.GetMyReviewParams{ID: ids.UUID(id), CustomerID: ids.UUID(userID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewsapi.MyReview{}, notFound()
	}
	if err != nil {
		return reviewsapi.MyReview{}, err
	}
	return myReviewOf(r.ID, r.StoreID, r.StoreName, r.OrderID, r.PosSaleID, r.Stars, r.Text, r.Tags,
		r.CreatedAt, r.EditedAt, r.EditedUntil, r.HiddenAt, r.ReplyText, r.ReplyCreatedAt, r.ReplyEditedAt, r.ReplyDeletedAt), nil
}

func (s *Service) listMine(ctx context.Context, userID ids.ID, limit *int, cursor *string) (reviewsapi.MyReviewList, error) {
	if err := s.ready(ctx); err != nil {
		return reviewsapi.MyReviewList{}, err
	}
	lim, at, id, err := pageArgs(limit, cursor)
	if err != nil {
		return reviewsapi.MyReviewList{}, err
	}
	rows, err := s.q(ctx).ListMyReviews(ctx, reviewsdb.ListMyReviewsParams{
		CustomerID: ids.UUID(userID), BeforeAt: at, BeforeID: id, Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	})
	if err != nil {
		return reviewsapi.MyReviewList{}, err
	}
	out := reviewsapi.MyReviewList{Items: make([]reviewsapi.MyReview, 0, len(rows)), ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			out.NextCursor = ptr(encodeCursor(rows[lim-1].CreatedAt, mustID(rows[lim-1].ID)))
			break
		}
		out.Items = append(out.Items, myReviewOf(r.ID, r.StoreID, r.StoreName, r.OrderID, r.PosSaleID, r.Stars, r.Text, r.Tags,
			r.CreatedAt, r.EditedAt, r.EditedUntil, r.HiddenAt, r.ReplyText, r.ReplyCreatedAt, r.ReplyEditedAt, r.ReplyDeletedAt))
	}
	return out, nil
}

func myReviewOf(id, store pgtype.UUID, storeName string, order, sale pgtype.UUID, stars int16, text *string, tags []string,
	created time.Time, edited *time.Time, until time.Time, hidden *time.Time,
	replyText *string, replyAt, replyEdited, replyDeleted *time.Time) reviewsapi.MyReview {
	m := reviewsapi.MyReview{
		Id: idString(id), StoreId: idString(store), StoreName: storeName,
		Stars: int(stars), Text: text, Tags: tagsOf(tags),
		CreatedAt: created.UTC(), EditedAt: utcPtr(edited), EditableUntil: until.UTC(), Hidden: hidden != nil,
	}
	if order.Valid {
		m.OrderId = ptr(idString(order))
	}
	if sale.Valid {
		m.PosSaleId = ptr(idString(sale))
	}
	// A reply an admin hid is not shown to the customer either.
	if replyText != nil && replyDeleted == nil && replyAt != nil {
		m.Reply = &reviewsapi.ReviewReply{Text: *replyText, CreatedAt: replyAt.UTC(), EditedAt: utcPtr(replyEdited)}
	}
	return m
}

// --- public and shop lists -----------------------------------------------------------

func (s *Service) listPublic(ctx context.Context, slug string, limit *int, cursor *string) (reviewsapi.PublicReviewList, error) {
	if err := s.ready(ctx); err != nil {
		return reviewsapi.PublicReviewList{}, err
	}
	lim, at, id, err := pageArgs(limit, cursor)
	if err != nil {
		return reviewsapi.PublicReviewList{}, err
	}
	st, err := s.q(ctx).GetLiveStoreBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewsapi.PublicReviewList{}, notFound()
	}
	if err != nil {
		return reviewsapi.PublicReviewList{}, err
	}
	rows, next, err := s.storePage(ctx, st.ID, pgtype.UUID{}, lim, at, id)
	if err != nil {
		return reviewsapi.PublicReviewList{}, err
	}
	out := reviewsapi.PublicReviewList{
		Rating: reviewsapi.StoreRating{Avg: ratingOf(st.RatingAvg), Count: int(st.RatingCount)},
		Items:  make([]reviewsapi.PublicReview, 0, len(rows)), NextCursor: next, ServerNow: s.now(),
	}
	for _, r := range rows {
		out.Items = append(out.Items, reviewsapi.PublicReview{
			Id: idString(r.ID), Stars: int(r.Stars), Text: r.Text, Tags: tagsOf(r.Tags),
			CustomerName: firstName(r.CustomerName), CreatedAt: r.CreatedAt.UTC(), EditedAt: utcPtr(r.EditedAt),
			Reply: replyOf(r.ReplyText, r.ReplyCreatedAt, r.ReplyEditedAt),
		})
	}
	return out, nil
}

func (s *Service) listForStore(ctx context.Context, storeID ids.ID, limit *int, cursor *string) (reviewsapi.StoreReviewList, error) {
	if err := s.ready(ctx); err != nil {
		return reviewsapi.StoreReviewList{}, err
	}
	lim, at, id, err := pageArgs(limit, cursor)
	if err != nil {
		return reviewsapi.StoreReviewList{}, err
	}
	rating, err := s.q(ctx).GetStoreRating(ctx, ids.UUID(storeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewsapi.StoreReviewList{}, notFound()
	}
	if err != nil {
		return reviewsapi.StoreReviewList{}, err
	}
	rows, next, err := s.storePage(ctx, ids.UUID(storeID), ids.UUID(storeID), lim, at, id)
	if err != nil {
		return reviewsapi.StoreReviewList{}, err
	}
	out := reviewsapi.StoreReviewList{
		Rating: reviewsapi.StoreRating{Avg: ratingOf(rating.RatingAvg), Count: int(rating.RatingCount)},
		Items:  make([]reviewsapi.StoreReview, 0, len(rows)), NextCursor: next, ServerNow: s.now(),
	}
	for _, r := range rows {
		out.Items = append(out.Items, reviewsapi.StoreReview{
			Id: idString(r.ID), Stars: int(r.Stars), Text: r.Text, Tags: tagsOf(r.Tags),
			CustomerName: firstName(r.CustomerName), CreatedAt: r.CreatedAt.UTC(), EditedAt: utcPtr(r.EditedAt),
			Reply: replyOf(r.ReplyText, r.ReplyCreatedAt, r.ReplyEditedAt), Reported: r.Reported,
		})
	}
	return out, nil
}

// storePage reads one page of a store's visible reviews (lim rows and the next cursor).
func (s *Service) storePage(ctx context.Context, store, reporting pgtype.UUID, lim int, at *time.Time, id pgtype.UUID) ([]reviewsdb.ListStoreReviewsRow, *string, error) {
	rows, err := s.q(ctx).ListStoreReviews(ctx, reviewsdb.ListStoreReviewsParams{
		ReportingStore: reporting, StoreID: store, BeforeAt: at, BeforeID: id, Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	})
	if err != nil {
		return nil, nil, err
	}
	if len(rows) > lim {
		return rows[:lim], ptr(encodeCursor(rows[lim-1].CreatedAt, mustID(rows[lim-1].ID))), nil
	}
	return rows, nil, nil
}

// --- reports -------------------------------------------------------------------------

func cleanReason(r string, path string) (string, error) {
	r = strings.TrimSpace(r)
	if r == "" || utf8.RuneCountInString(r) > 500 { //settings:ok contract bound (ReportText ≤ 500)
		return "", fieldErr(path, "1 to 500 characters")
	}
	return r, nil
}

func (s *Service) reportAsCustomer(ctx context.Context, userID, reviewID ids.ID, reason string) (reviewsapi.ReviewReportReceipt, error) {
	if err := s.ready(ctx); err != nil {
		return reviewsapi.ReviewReportReceipt{}, err
	}
	reason, err := cleanReason(reason, "body.reason")
	if err != nil {
		return reviewsapi.ReviewReportReceipt{}, err
	}
	var out reviewsapi.ReviewReportReceipt
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		r, err := s.q(ctx).GetReviewForReport(ctx, ids.UUID(reviewID))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if r.CustomerID == ids.UUID(userID) {
			return fieldErr("body.reviewId", "your own review")
		}
		out, err = s.report(ctx, r.ID, ids.UUID(userID), pgtype.UUID{}, reason)
		return err
	})
	return out, err
}

func (s *Service) reportAsStore(ctx context.Context, userID, storeID, reviewID ids.ID, reason string) (reviewsapi.ReviewReportReceipt, error) {
	if err := s.ready(ctx); err != nil {
		return reviewsapi.ReviewReportReceipt{}, err
	}
	reason, err := cleanReason(reason, "body.reason")
	if err != nil {
		return reviewsapi.ReviewReportReceipt{}, err
	}
	var out reviewsapi.ReviewReportReceipt
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		rid, err := s.q(ctx).GetStoreReviewForReply(ctx, reviewsdb.GetStoreReviewForReplyParams{ID: ids.UUID(reviewID), StoreID: ids.UUID(storeID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		out, err = s.report(ctx, rid, ids.UUID(userID), ids.UUID(storeID), reason)
		return err
	})
	return out, err
}

// report inserts the reporter's report, or returns the one they already sent.
func (s *Service) report(ctx context.Context, review, reporter, reporterStore pgtype.UUID, reason string) (reviewsapi.ReviewReportReceipt, error) {
	now := s.now()
	row, err := s.q(ctx).InsertReviewReport(ctx, reviewsdb.InsertReviewReportParams{
		ID: ids.UUID(s.ids.New(now)), ReviewID: review, ReporterUserID: reporter, ReporterStoreID: reporterStore, Reason: reason, Now: now,
	})
	if errors.Is(err, pgx.ErrNoRows) { // already reported by this user: return the first
		first, gerr := s.q(ctx).GetReviewReportByReporter(ctx, reviewsdb.GetReviewReportByReporterParams{ReviewID: review, ReporterUserID: reporter})
		if gerr != nil {
			return reviewsapi.ReviewReportReceipt{}, gerr
		}
		return receiptOf(first.ID, first.ReviewID, first.Status, first.CreatedAt), nil
	}
	if err != nil {
		return reviewsapi.ReviewReportReceipt{}, err
	}
	return receiptOf(row.ID, row.ReviewID, row.Status, row.CreatedAt), nil
}

func receiptOf(id, review pgtype.UUID, status string, at time.Time) reviewsapi.ReviewReportReceipt {
	return reviewsapi.ReviewReportReceipt{
		Id: idString(id), ReviewId: idString(review), Status: reviewsapi.ReviewReportReceiptStatus(status), CreatedAt: at.UTC(),
	}
}

// --- shop reply -----------------------------------------------------------------------

func cleanReply(t string) (string, error) {
	t = strings.TrimSpace(t)
	if t == "" || utf8.RuneCountInString(t) > maxText {
		return "", fieldErr("body.text", "1 to 1000 characters")
	}
	return t, nil
}

func (s *Service) reply(ctx context.Context, userID, storeID, reviewID ids.ID, text string) (reviewsapi.ReviewReply, error) {
	if err := s.ready(ctx); err != nil {
		return reviewsapi.ReviewReply{}, err
	}
	text, err := cleanReply(text)
	if err != nil {
		return reviewsapi.ReviewReply{}, err
	}
	var out reviewsapi.ReviewReply
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		rid, err := s.q(ctx).GetStoreReviewForReply(ctx, reviewsdb.GetStoreReviewForReplyParams{ID: ids.UUID(reviewID), StoreID: ids.UUID(storeID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		row, err := s.q(ctx).InsertReply(ctx, reviewsdb.InsertReplyParams{ReviewID: rid, AuthorUserID: ids.UUID(userID), Text: text, Now: s.now()})
		if errors.Is(err, pgx.ErrNoRows) { // ON CONFLICT DO NOTHING: one reply per review
			return problem.New(genproblems.ReviewReplyExists, "")
		}
		if err != nil {
			return err
		}
		out = reviewsapi.ReviewReply{Text: row.Text, CreatedAt: row.CreatedAt.UTC(), EditedAt: utcPtr(row.EditedAt)}
		return nil
	})
	return out, err
}

func (s *Service) editReply(ctx context.Context, storeID, reviewID ids.ID, text string) (reviewsapi.ReviewReply, error) {
	if err := s.ready(ctx); err != nil {
		return reviewsapi.ReviewReply{}, err
	}
	text, err := cleanReply(text)
	if err != nil {
		return reviewsapi.ReviewReply{}, err
	}
	var out reviewsapi.ReviewReply
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		rid, err := s.q(ctx).GetStoreReviewForReply(ctx, reviewsdb.GetStoreReviewForReplyParams{ID: ids.UUID(reviewID), StoreID: ids.UUID(storeID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		cur, err := s.q(ctx).LockReply(ctx, rid)
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if cur.DeletedAt != nil {
			return problem.New(genproblems.InvalidTransition, "") // an admin hid it; only an admin restores it
		}
		now := s.now()
		row, err := s.q(ctx).UpdateReply(ctx, reviewsdb.UpdateReplyParams{ReviewID: rid, Text: text, Now: &now})
		if err != nil {
			return err
		}
		out = reviewsapi.ReviewReply{Text: row.Text, CreatedAt: row.CreatedAt.UTC(), EditedAt: utcPtr(row.EditedAt)}
		return nil
	})
	return out, err
}

// --- helpers --------------------------------------------------------------------------

func replyOf(text *string, at, edited *time.Time) *reviewsapi.ReviewReply {
	if text == nil || at == nil {
		return nil
	}
	return &reviewsapi.ReviewReply{Text: *text, CreatedAt: at.UTC(), EditedAt: utcPtr(edited)}
}

func tagsOf(in []string) reviewsapi.ReviewTags {
	out := make(reviewsapi.ReviewTags, 0, len(in))
	for _, t := range in {
		out = append(out, reviewsapi.ReviewTag(t))
	}
	return out
}

// ratingOf converts rating_avg (numeric(3,2), NULL with no review).
func ratingOf(n pgtype.Numeric) *float32 {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	v := float32(f.Float64)
	return &v
}

// firstName is the customer's first name only (04 §2.4); nil when they gave none.
func firstName(n *string) *string {
	if n == nil {
		return nil
	}
	f := strings.Fields(*n)
	if len(f) == 0 {
		return nil
	}
	return &f[0]
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func ptr[T any](v T) *T { return &v }

// pageArgs reads limit and cursor. A cursor that does not decode is 422 on query.cursor.
func pageArgs(limit *int, cursor *string) (int, *time.Time, pgtype.UUID, error) {
	lim := defaultLimit
	if limit != nil {
		lim = min(max(*limit, 1), maxLimit)
	}
	if cursor == nil || *cursor == "" {
		return lim, nil, pgtype.UUID{}, nil
	}
	at, id, err := decodeCursor(*cursor)
	if err != nil {
		return 0, nil, pgtype.UUID{}, fieldErr("query.cursor", "not a cursor from this list")
	}
	return lim, &at, ids.UUID(id), nil
}

// encodeCursor makes the opaque cursor after (at, id).
func encodeCursor(at time.Time, id ids.ID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixNano(), 10) + "." + id.String()))
}

func decodeCursor(c string) (time.Time, ids.ID, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	ns, sid, ok := strings.Cut(string(b), ".")
	if !ok {
		return time.Time{}, ids.Zero, errors.New("reviews: cursor has no id")
	}
	n, err := strconv.ParseInt(ns, 10, 64)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	id, err := ids.Parse(sid)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	return time.Unix(0, n).UTC(), id, nil
}

func principal(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	return p, nil
}

// pathID parses a ULID path parameter; one that is not a ULID names nothing (404).
func pathID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, notFound()
	}
	return id, nil
}

func idString(u pgtype.UUID) string {
	id, _ := ids.FromUUID(u)
	return id.String()
}

func mustID(u pgtype.UUID) ids.ID {
	id, _ := ids.FromUUID(u)
	return id
}

func asProblem(err error) error {
	var p *problem.Error
	if errors.As(err, &p) {
		return p
	}
	return problem.Wrap(genproblems.Internal, err)
}

func fieldErr(path, reason string) *problem.Error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

func notFound() *problem.Error { return problem.New(genproblems.NotFound, "") }

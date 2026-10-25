package asks

// Creating an ask (C-25; 01 §11.2): who may ask, which shops get it, in what
// order, and what each shop is told.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	asksapi "github.com/todayz/backend/gen/api/asks"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/asksdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/geo/h3"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/realtime"
	"github.com/todayz/backend/internal/platform/settings"
)

// candidateCap bounds the stores read before the open-now check, which runs
// in Go on each store's hours; the first ask_max_stores open ones get it.
const candidateCap = 200 //settings:ok read bound, not a product rule

// maxParamRunes bounds a notice parameter (the store's name).
const maxParamRunes = 60 //settings:ok notice text bound

// Recipient is one store an ask may go to, as recipient selection reads it.
type Recipient struct {
	ID          ids.ID
	BusinessID  ids.ID
	Name        string
	DistanceM   int32
	Tier        int32
	Hours       []byte
	ClosedUntil *time.Time
}

// SelectRecipients keeps the stores open at now, in the order the query gave
// (tier, then distance), at most max. The query already left out stores that
// are not live, unverified, muted, blocked for this customer or full.
func SelectRecipients(cands []Recipient, now time.Time, max int, openAt func([]byte, *time.Time, time.Time) bool) []Recipient {
	out := make([]Recipient, 0, min(len(cands), max))
	for _, c := range cands {
		if len(out) >= max {
			break
		}
		if !openAt(c.Hours, c.ClosedUntil, now) {
			continue
		}
		out = append(out, c)
	}
	return out
}

type createLimits struct {
	daily, radius, maxStores, storeDaily, lowAfter, lowMax int64
	window                                                 time.Duration
}

func (s *Service) createLimits(ctx context.Context) (createLimits, error) {
	var l createLimits
	var err error
	for _, x := range []struct {
		k settings.Key
		v *int64
	}{
		{settings.AskDailyLimitCustomer, &l.daily}, {settings.AskRadiusM, &l.radius},
		{settings.AskMaxStores, &l.maxStores}, {settings.AskDailyLimitStore, &l.storeDaily},
		{settings.AskLowYieldAfterCount, &l.lowAfter}, {settings.AskLowYieldMaxStores, &l.lowMax},
	} {
		if *x.v, err = s.settings.Int(ctx, x.k); err != nil {
			return l, fmt.Errorf("asks: %s: %w", x.k, err)
		}
	}
	if l.window, err = s.settings.Duration(ctx, settings.AskReplyWindowMin); err != nil {
		return l, fmt.Errorf("asks: %s: %w", settings.AskReplyWindowMin, err)
	}
	return l, nil
}

func (s *Service) create(ctx context.Context, body *asksapi.AskCreate) (asksapi.AskDetail, error) {
	p, err := principal(ctx)
	if err != nil {
		return asksapi.AskDetail{}, err
	}
	if err := s.ready(ctx); err != nil {
		return asksapi.AskDetail{}, err
	}
	if body == nil {
		return asksapi.AskDetail{}, fieldErr("body", "required")
	}
	if (body.ProductId == nil) == (body.Query == nil) {
		return asksapi.AskDetail{}, fieldErr("body.productId", "send exactly one of productId and query")
	}
	var productID ids.ID
	var query string
	if body.ProductId != nil {
		if productID, err = ids.Parse(*body.ProductId); err != nil {
			return asksapi.AskDetail{}, fieldErr("body.productId", "not a product")
		}
	} else {
		if reason := badText(*body.Query); reason != "" {
			return asksapi.AskDetail{}, fieldErr("body.query", reason)
		}
		query = s.normalize(*body.Query)
		if query == "" || utf8.RuneCountInString(query) > 200 { //settings:ok contract maxLength
			return asksapi.AskDetail{}, fieldErr("body.query", "must have words to ask for")
		}
	}
	lim, err := s.createLimits(ctx)
	if err != nil {
		return asksapi.AskDetail{}, err
	}
	lat, lng := float64(body.Lat), float64(body.Lng)
	now := s.now()
	askID := s.ids.New(now)
	customer := ids.UUID(p.UserID)

	var recips []Recipient
	var lowYield bool
	var cell *int64
	var cityID pgtype.UUID
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := q.LockCustomerAsks(ctx, p.UserID.String()); err != nil {
			return err
		}
		st, err := q.CustomerStatus(ctx, customer)
		if err != nil {
			return err
		}
		if st != "active" {
			return problem.New(genproblems.Forbidden, "")
		}
		n, err := q.CountAsksSince(ctx, asksdb.CountAsksSinceParams{CustomerID: customer, Since: clock.StartOfDayIST(now)})
		if err != nil {
			return err
		}
		if int64(n) >= lim.daily {
			wait := int64(nextISTMidnight(now).Sub(now).Seconds()) + 1
			// extensions.limit is the setting, for "You can send {n} asks a day." (C-25).
			return problem.New(genproblems.AskLimitReached, "").WithHeader("Retry-After", strconv.FormatInt(wait, 10)).
				WithExtension("limit", lim.daily)
		}

		var categoryID pgtype.UUID
		if body.ProductId != nil {
			pr, err := q.ProductForAsk(ctx, ids.UUID(productID))
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && pr.Status != "active") {
				return fieldErr("body.productId", "not a product")
			}
			if err != nil {
				return err
			}
			categoryID = pr.CategoryID
			query = s.normalize(itemName(&pr.Name, pr.Variant, ""))
			if query == "" {
				query = productID.String()
			}
			if r := []rune(query); len(r) > 200 { //settings:ok column bound
				query = string(r[:200]) //settings:ok column bound
			}
		}
		open, err := q.OpenAskFor(ctx, asksdb.OpenAskForParams{CustomerID: customer, ProductID: nullID(productID), QueryNorm: query})
		if err == nil {
			return problem.New(genproblems.AskAlreadyOpen, "").WithExtension("askId", idOf(open).String())
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		var target pgtype.UUID
		if body.StoreSlug != nil {
			sid, err := q.StoreBySlug(ctx, *body.StoreSlug)
			if errors.Is(err, pgx.ErrNoRows) {
				return problem.New(genproblems.AskStoreUnavailable, "")
			}
			if err != nil {
				return err
			}
			target = sid
		}

		maxStores := lim.maxStores
		if lim.lowAfter > 0 {
			recent, err := q.RecentAskYes(ctx, asksdb.RecentAskYesParams{CustomerID: customer, N: i32(lim.lowAfter)})
			if err != nil {
				return err
			}
			lowYield = LowYield(recent, int(lim.lowAfter))
			if lowYield {
				maxStores = min(maxStores, lim.lowMax)
			}
		}
		if target.Valid {
			maxStores = 1
		}

		rows, err := q.CandidateStores(ctx, asksdb.CandidateStoresParams{
			Lng: lng, Lat: lat, ProductID: nullID(productID), QueryNorm: query, CategoryID: categoryID,
			CustomerID: customer, DayStart: clock.StartOfDayIST(now), StoreDailyLimit: i32(lim.storeDaily),
			TargetStoreID: target, RadiusM: i32(lim.radius), MaxCandidates: candidateCap,
		})
		if err != nil {
			return err
		}
		cands := make([]Recipient, len(rows))
		for i, r := range rows {
			cands[i] = Recipient{ID: idOf(r.ID), BusinessID: idOf(r.BusinessID), Name: r.Name, DistanceM: r.DistanceM,
				Tier: r.Tier, Hours: r.Hours, ClosedUntil: r.ClosedUntil}
		}
		recips = SelectRecipients(cands, now, int(maxStores), s.openAt)
		if len(recips) == 0 {
			if target.Valid {
				return problem.New(genproblems.AskStoreUnavailable, "")
			}
			return problem.New(genproblems.AskNoShops, "")
		}

		if c, err := h3.LatLngToCell(lat, lng, h3.AnalyticsRes); err == nil {
			cell = ptr(c.Int64())
		}
		if cityID, err = q.CityAt(ctx, asksdb.CityAtParams{Lng: lng, Lat: lat}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var needBy *string
		if body.NeedBy != nil {
			needBy = ptr(string(*body.NeedBy))
		}
		if err := q.InsertAsk(ctx, asksdb.InsertAskParams{
			ID: ids.UUID(askID), CustomerID: customer, ProductID: nullID(productID), QueryNorm: query,
			Quantity: i32(int64(body.Quantity)), NeedBy: needBy, GeoCell: cell, CityPlaceID: cityID,
			RadiusM: i32(lim.radius), TargetStoreID: target, RecipientsCount: i32(int64(len(recips))),
			LowYield: lowYield, Now: now,
		}); err != nil {
			return err
		}
		tx, _ := db.TxFrom(ctx)
		for _, r := range recips {
			if err := q.InsertRecipient(ctx, asksdb.InsertRecipientParams{
				ID: ids.UUID(s.ids.New(now)), AskID: ids.UUID(askID), StoreID: ids.UUID(r.ID), Now: now,
				DistanceM: RoundDistance(r.DistanceM), DistanceBand: Band(r.DistanceM), ReplyBy: now.Add(lim.window),
			}); err != nil {
				return err
			}
			if err := realtime.Publish(ctx, tx, realtime.StoreChannel(r.ID), realtime.Event{Type: realtime.AskCreated, ID: askID, Version: 1}); err != nil {
				return err
			}
			if s.notices != nil {
				if _, err := s.notices.Send(ctx, tx, notice.Notification{
					Kind: notice.KindNewAsk, BusinessID: r.BusinessID, StoreID: r.ID,
					Params:  map[string]string{"store": clip(r.Name, maxParamRunes)},
					Ref:     &notice.Ref{Type: "ask", ID: askID},
					Outside: []notice.Outside{{Channel: notice.Push}},
				}); err != nil {
					return fmt.Errorf("asks: new ask notice: %w", err)
				}
			}
		}
		props := map[string]any{
			"target": "nearby", "has_product": body.ProductId != nil, "quantity": body.Quantity,
			"need_by": "unset", "recipients": len(recips), "low_yield": lowYield,
		}
		if target.Valid {
			props["target"] = "one_shop"
		}
		if needBy != nil {
			props["need_by"] = *needBy
		}
		if cell != nil {
			props["geo_cell"] = h3.FromInt64(*cell).String()
		}
		if cityID.Valid {
			props["city_id"] = idOf(cityID).String()
		}
		s.event(ctx, q, pgtype.UUID{}, "ask_created", props)
		return nil
	})
	if err != nil {
		return asksapi.AskDetail{}, err
	}
	return s.detail(ctx, s.q(ctx), askID, p.UserID)
}

// LowYield reports whether the customer's last n asks since their last
// completed online order all got no Yes (01 §11.2 abuse controls). Fewer than
// n asks is never low-yield.
func LowYield(recent []asksdb.RecentAskYesRow, n int) bool {
	if n <= 0 || len(recent) < n {
		return false
	}
	for _, r := range recent[:n] {
		if r.HadYes {
			return false
		}
	}
	return true
}

func nullID(id ids.ID) pgtype.UUID {
	if id == ids.Zero {
		return pgtype.UUID{}
	}
	return ids.UUID(id)
}

// badText refuses invalid UTF-8, control characters and blank text.
func badText(s string) string {
	if !utf8.ValidString(s) {
		return "must be valid text"
	}
	blank := true
	for _, r := range s {
		if unicode.IsControl(r) {
			return "must not contain control characters"
		}
		if !unicode.IsSpace(r) {
			blank = false
		}
	}
	if blank {
		return "must not be blank"
	}
	return ""
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

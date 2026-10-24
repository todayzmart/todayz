package offers

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	offersapi "github.com/todayz/backend/gen/api/offers"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/offersdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// The most items one offer may list (the table's CHECK, ck_offers_scope_terms).
const maxOfferItems = 50

// --- conversions --------------------------------------------------------------------

func (s *Service) toAPI(r offersdb.Offer) offersapi.Offer {
	eo := engineOffer(r)
	o := offersapi.Offer{
		Id: ulidOf(r.ID), StoreId: ulidOf(r.StoreID), Title: TitleOf(eo),
		Kind: offersapi.OfferKind(r.Kind), Value: int(r.Value),
		Scope: offersapi.OfferScope(r.Scope), StoreItemIds: make([]string, 0, len(r.StoreItemIds)),
		Channels: offersapi.OfferChannels(r.Channels), Redemptions: int(r.Redemptions),
		StartsAt: r.StartsAt, EndsAt: r.EndsAt, Status: offersapi.OfferStatus(r.Status),
		Source: offersapi.OfferSource(r.Source), Version: int(r.Version),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	for _, id := range r.StoreItemIds {
		o.StoreItemIds = append(o.StoreItemIds, ulidOf(id))
	}
	if r.BuyQty != nil {
		o.BuyQty = ptr(int(*r.BuyQty))
	}
	if r.GetQty != nil {
		o.GetQty = ptr(int(*r.GetQty))
	}
	if r.MinBillPaise != nil {
		o.MinBillPaise = ptr(int(*r.MinBillPaise))
	}
	if r.CategoryID.Valid {
		o.CategoryId = ptr(ulidOf(r.CategoryID))
	}
	if r.PerCustomerLimit != nil {
		o.PerCustomerLimit = ptr(int(*r.PerCustomerLimit))
	}
	if r.MaxRedemptions != nil {
		o.MaxRedemptions = ptr(int(*r.MaxRedemptions))
	}
	if r.BatchID.Valid {
		o.BatchId = ptr(ulidOf(r.BatchID))
	}
	return o
}

func i32p(v *int) *int32 {
	if v == nil {
		return nil
	}
	return ptr(i32(*v))
}

func i64p(v *int) *int64 {
	if v == nil {
		return nil
	}
	return ptr(int64(*v))
}

// --- reads ----------------------------------------------------------------------------

func (s *Service) getOffer(ctx context.Context, sid, offerID string) (offersapi.Offer, int32, error) {
	if err := s.ready(ctx); err != nil {
		return offersapi.Offer{}, 0, err
	}
	store, err := pathID(sid)
	if err != nil {
		return offersapi.Offer{}, 0, err
	}
	id, err := pathID(offerID)
	if err != nil {
		return offersapi.Offer{}, 0, err
	}
	r, err := s.q(ctx).GetOffer(ctx, offersdb.GetOfferParams{ID: ids.UUID(id), StoreID: ids.UUID(store)})
	if db.IsNoRows(err) {
		return offersapi.Offer{}, 0, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return offersapi.Offer{}, 0, fmt.Errorf("offers: get offer: %w", err)
	}
	return s.toAPI(r), r.Version, nil
}

func (s *Service) listOffers(ctx context.Context, sid string, p offersapi.ListOffersParams) (offersapi.OfferList, error) {
	if err := s.ready(ctx); err != nil {
		return offersapi.OfferList{}, err
	}
	store, err := pathID(sid)
	if err != nil {
		return offersapi.OfferList{}, err
	}
	lim := int32(20)
	if p.Limit != nil {
		lim = int32(min(max(*p.Limit, 1), 100)) //nolint:gosec // clamped
	}
	arg := offersdb.ListOffersParams{StoreID: ids.UUID(store), Lim: lim + 1}
	if p.Status != nil {
		arg.Status = ptr(string(*p.Status))
	}
	if p.Cursor != nil {
		raw, err := base64.RawURLEncoding.DecodeString(*p.Cursor)
		after, perr := ids.Parse(string(raw))
		if err != nil || perr != nil {
			return offersapi.OfferList{}, fieldErr("query.cursor", "not a cursor from this list")
		}
		arg.AfterID = ids.UUID(after)
	}
	rows, err := s.q(ctx).ListOffers(ctx, arg)
	if err != nil {
		return offersapi.OfferList{}, fmt.Errorf("offers: list offers: %w", err)
	}
	out := offersapi.OfferList{Items: make([]offersapi.Offer, 0, min(len(rows), int(lim)))}
	for i, r := range rows {
		if i == int(lim) {
			out.NextCursor = ptr(base64.RawURLEncoding.EncodeToString([]byte(ulidOf(rows[i-1].ID))))
			break
		}
		out.Items = append(out.Items, s.toAPI(r))
	}
	return out, nil
}

// --- terms ------------------------------------------------------------------------------

// terms is an offer's validated terms, ready to price or to insert.
type terms struct {
	kind         Kind
	value        int64
	buyQty       *int32
	getQty       *int32
	minBill      *int64
	scope        Scope
	items        []ids.ID
	category     ids.ID
	channels     Channels
	perCustomer  *int32
	maxRedeem    *int32
	title        *string
	startsAt     time.Time
	endsAt       *time.Time
	status       Status
	source       string
	batch        ids.ID
	flashItemEnd time.Time
}

// absent says a field is not allowed for the kind.
func absent(v *invalid, path string, present bool, kind Kind) {
	if present {
		v.add(path, "does not apply to a "+string(kind)+" offer")
	}
}

// endOfDayIST is the instant the IST calendar day of date ends.
func endOfDayIST(d pgtype.Date) time.Time {
	return time.Date(d.Time.Year(), d.Time.Month(), d.Time.Day()+1, 0, 0, 0, 0, clock.IST)
}

// validate checks an OfferCreate against the rules and the shop's catalogue
// and returns the terms. It reads, never writes, so a preview shares it.
func (s *Service) validate(ctx context.Context, q *offersdb.Queries, store ids.ID, b *offersapi.OfferCreate, base string, now time.Time) (terms, error) {
	var v invalid
	t := terms{kind: Kind(b.Kind), scope: Scope(b.Scope), channels: ChannelsBoth, source: "manual"}
	if b.Channels != nil {
		t.channels = Channels(*b.Channels)
	}
	v.text(base+".title", b.Title)
	if b.Title != nil {
		t.title = ptr(strings.TrimSpace(*b.Title))
	}
	switch t.kind {
	case KindPercent:
		if b.Value == nil {
			v.add(base+".value", "required")
		} else if *b.Value < 1 || *b.Value > MaxPercent {
			v.add(base+".value", fmt.Sprintf("must be from 1 to %d", MaxPercent))
		}
		absent(&v, base+".buyQty", b.BuyQty != nil, t.kind)
		absent(&v, base+".getQty", b.GetQty != nil, t.kind)
		absent(&v, base+".minBillPaise", b.MinBillPaise != nil, t.kind)
	case KindFlat:
		if b.Value == nil {
			v.add(base+".value", "required")
		}
		absent(&v, base+".buyQty", b.BuyQty != nil, t.kind)
		absent(&v, base+".getQty", b.GetQty != nil, t.kind)
		absent(&v, base+".minBillPaise", b.MinBillPaise != nil, t.kind)
	case KindBXGY:
		if b.BuyQty == nil {
			v.add(base+".buyQty", "required")
		}
		if b.GetQty == nil {
			v.add(base+".getQty", "required")
		}
		absent(&v, base+".value", b.Value != nil, t.kind)
		absent(&v, base+".minBillPaise", b.MinBillPaise != nil, t.kind)
	case KindBillMin:
		if b.Value == nil {
			v.add(base+".value", "required")
		}
		if b.MinBillPaise == nil {
			v.add(base+".minBillPaise", "required")
		}
		if b.Value != nil && b.MinBillPaise != nil && *b.Value >= *b.MinBillPaise {
			v.add(base+".value", "must be less than minBillPaise")
		}
		if t.scope != ScopeStore {
			v.add(base+".scope", "a minimum-bill offer covers the whole shop")
		}
		absent(&v, base+".buyQty", b.BuyQty != nil, t.kind)
		absent(&v, base+".getQty", b.GetQty != nil, t.kind)
	}
	if b.Value != nil {
		t.value = int64(*b.Value)
	}
	t.buyQty, t.getQty, t.minBill = i32p(b.BuyQty), i32p(b.GetQty), i64p(b.MinBillPaise)
	t.perCustomer, t.maxRedeem = i32p(b.PerCustomerLimit), i32p(b.MaxRedemptions)

	// Scope.
	switch t.scope {
	case ScopeStore:
		if b.StoreItemIds != nil {
			v.add(base+".storeItemIds", "does not apply to a whole-shop offer")
		}
		if b.CategoryId != nil {
			v.add(base+".categoryId", "does not apply to a whole-shop offer")
		}
	case ScopeItems:
		if b.CategoryId != nil {
			v.add(base+".categoryId", "does not apply to an items offer")
		}
		if b.StoreItemIds != nil {
			if len(*b.StoreItemIds) > maxOfferItems {
				v.add(base+".storeItemIds", fmt.Sprintf("at most %d items", maxOfferItems))
			}
			for i, raw := range *b.StoreItemIds {
				id, err := ids.Parse(raw)
				if err != nil {
					v.add(fmt.Sprintf("%s.storeItemIds[%d]", base, i), "not an item id")
					continue
				}
				if !slices.Contains(t.items, id) {
					t.items = append(t.items, id)
				}
			}
		}
	case ScopeCategory:
		if b.StoreItemIds != nil {
			v.add(base+".storeItemIds", "does not apply to a category offer")
		}
		if b.CategoryId == nil {
			v.add(base+".categoryId", "required")
		} else if id, err := ids.Parse(*b.CategoryId); err != nil {
			v.add(base+".categoryId", "not a category id")
		} else {
			t.category = id
		}
	}

	// Dates.
	t.startsAt = now
	if b.StartsAt != nil && b.StartsAt.After(now) {
		t.startsAt = *b.StartsAt
	}
	if b.EndsAt != nil {
		if !b.EndsAt.After(t.startsAt) {
			v.add(base+".endsAt", "must be after the start")
		}
		e := *b.EndsAt
		t.endsAt = &e
	}
	t.status = StatusLive
	if t.startsAt.After(now) {
		t.status = StatusScheduled
	}
	if err := v.err(); err != nil {
		return terms{}, err
	}

	// Flash offer on an expiring batch.
	if b.BatchId != nil {
		if err := s.flash(ctx, q, store, b, base, now, &t); err != nil {
			return terms{}, err
		}
	}

	// The shop's own items and the category.
	if len(t.items) > 0 {
		rows, err := q.ItemsOfStore(ctx, offersdb.ItemsOfStoreParams{StoreID: ids.UUID(store), Ids: uuids(t.items)})
		if err != nil {
			return terms{}, fmt.Errorf("offers: items of store: %w", err)
		}
		have := map[ids.ID]bool{}
		for _, r := range rows {
			have[idOf(r.ID)] = true
		}
		for i, id := range t.items {
			if !have[id] {
				v.add(fmt.Sprintf("%s.storeItemIds[%d]", base, i), "not an item of this shop")
			}
		}
	}
	if t.scope == ScopeItems && len(t.items) == 0 {
		v.add(base+".storeItemIds", "needs at least one item")
	}
	if t.scope == ScopeCategory && t.category != ids.Zero {
		ok, err := q.CategoryExists(ctx, ids.UUID(t.category))
		if err != nil {
			return terms{}, fmt.Errorf("offers: category: %w", err)
		}
		if !ok {
			v.add(base+".categoryId", "no such category")
		}
	}
	return t, v.err()
}

// flash applies a batch to the terms: one item, ending no later than the
// batch's expiry day (IST), one open flash offer per batch.
func (s *Service) flash(ctx context.Context, q *offersdb.Queries, store ids.ID, b *offersapi.OfferCreate, base string, now time.Time, t *terms) error {
	batch, err := ids.Parse(*b.BatchId)
	if err != nil {
		return fieldErr(base+".batchId", "not a batch id")
	}
	row, err := q.BatchForFlash(ctx, offersdb.BatchForFlashParams{BatchID: ids.UUID(batch), StoreID: ids.UUID(store)})
	if db.IsNoRows(err) {
		return fieldErr(base+".batchId", "no such batch in this shop")
	}
	if err != nil {
		return fmt.Errorf("offers: batch: %w", err)
	}
	if !row.ExpiresOn.Valid {
		return fieldErr(base+".batchId", "the batch has no expiry date")
	}
	item := idOf(row.StoreItemID)
	if t.scope != ScopeItems {
		return fieldErr(base+".scope", "a flash offer covers the batch's item")
	}
	if len(t.items) == 0 {
		t.items = []ids.ID{item}
	}
	if len(t.items) != 1 || t.items[0] != item {
		return fieldErr(base+".storeItemIds", "a flash offer covers only the batch's item")
	}
	end := endOfDayIST(row.ExpiresOn)
	if !end.After(now) {
		return fieldErr(base+".batchId", "the batch has already expired")
	}
	switch {
	case t.endsAt == nil:
		t.endsAt = &end
	case t.endsAt.After(end):
		return fieldErr(base+".endsAt", "must not be after the batch's expiry day")
	}
	if !t.endsAt.After(t.startsAt) {
		return fieldErr(base+".startsAt", "the offer would start after the batch expires")
	}
	if t.kind == KindBillMin {
		return fieldErr(base+".kind", "a flash offer cannot be a minimum-bill offer")
	}
	t.batch, t.source, t.flashItemEnd = batch, "flash_expiry", end
	return nil
}

// --- create ----------------------------------------------------------------------------

func (s *Service) createOffer(ctx context.Context, sid string, b *offersapi.OfferCreate) (offersapi.Offer, int32, error) {
	if err := s.ready(ctx); err != nil {
		return offersapi.Offer{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return offersapi.Offer{}, 0, err
	}
	store, err := pathID(sid)
	if err != nil {
		return offersapi.Offer{}, 0, err
	}
	if b == nil {
		return offersapi.Offer{}, 0, problem.New(genproblems.BadRequest, "")
	}
	now := s.clock.Now()
	var created offersdb.Offer
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		t, err := s.validate(ctx, q, store, b, "body", now)
		if err != nil {
			return err
		}
		if t.batch != ids.Zero {
			if _, err := q.ActiveFlashForBatch(ctx, ids.UUID(t.batch)); err == nil {
				return problem.New(genproblems.Conflict, "the batch already has a flash offer")
			} else if !db.IsNoRows(err) {
				return fmt.Errorf("offers: active flash: %w", err)
			}
		}
		id := s.ids.New(now)
		// The store's catalogue sequence first, like every catalogue writer.
		if _, err := q.RecordCatalogueChange(ctx, offersdb.RecordCatalogueChangeParams{
			StoreID: ids.UUID(store), EntityID: ids.UUID(id), Op: "upsert", Now: now,
		}); err != nil {
			return fmt.Errorf("offers: catalogue change: %w", err)
		}
		arg := offersdb.InsertOfferParams{
			ID: ids.UUID(id), StoreID: ids.UUID(store), Title: t.title, Kind: string(t.kind), Value: t.value,
			BuyQty: t.buyQty, GetQty: t.getQty, MinBillPaise: t.minBill, Scope: string(t.scope),
			StoreItemIds: uuids(t.items), Channels: string(t.channels), PerCustomerLimit: t.perCustomer,
			MaxRedemptions: t.maxRedeem, StartsAt: t.startsAt, EndsAt: t.endsAt, Status: string(t.status),
			Source: t.source, CreatedBy: ids.UUID(p.UserID), Now: now,
		}
		if t.category != ids.Zero {
			arg.CategoryID = ids.UUID(t.category)
		}
		if t.batch != ids.Zero {
			arg.BatchID = ids.UUID(t.batch)
		}
		if created, err = q.InsertOffer(ctx, arg); err != nil {
			if db.IsUniqueViolation(err, "ux_offers_batch") {
				return problem.New(genproblems.Conflict, "the batch already has a flash offer")
			}
			return fmt.Errorf("offers: insert offer: %w", err)
		}
		if _, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorUser, ActorID: p.UserID, Action: "offers.create", SubjectType: "offer",
			SubjectID: id.String(), After: auditView(created),
		}); err != nil {
			return err
		}
		s.event(ctx, q, ids.UUID(p.UserID), "offer_created", map[string]any{
			"kind": string(t.kind), "scope": string(t.scope), "channels": string(t.channels), "source": t.source,
		})
		return nil
	})
	if err != nil {
		return offersapi.Offer{}, 0, err
	}
	return s.toAPI(created), created.Version, nil
}

// auditView is the part of an offer the audit log keeps.
func auditView(r offersdb.Offer) map[string]any {
	return map[string]any{
		"kind": r.Kind, "value": r.Value, "scope": r.Scope, "channels": r.Channels, "status": r.Status,
		"starts_at": r.StartsAt, "ends_at": r.EndsAt, "per_customer_limit": r.PerCustomerLimit,
		"max_redemptions": r.MaxRedemptions, "source": r.Source,
	}
}

// --- update ----------------------------------------------------------------------------

// patch is the sent fields of an OfferUpdate: a key sent as null clears the
// field; a key left out keeps it.
type patch struct {
	status                                  bool
	title, channels, perCustomer, maxRedeem bool
	startsAt, endsAt                        bool
}

func (s *Service) updateOffer(ctx context.Context, sid, offerID string, ifm *string, b *offersapi.OfferUpdate, sent func(string) bool) (offersapi.Offer, int32, error) {
	if err := s.ready(ctx); err != nil {
		return offersapi.Offer{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return offersapi.Offer{}, 0, err
	}
	store, err := pathID(sid)
	if err != nil {
		return offersapi.Offer{}, 0, err
	}
	id, err := pathID(offerID)
	if err != nil {
		return offersapi.Offer{}, 0, err
	}
	version, err := ifMatch(ifm)
	if err != nil {
		return offersapi.Offer{}, 0, err
	}
	if b == nil {
		return offersapi.Offer{}, 0, problem.New(genproblems.BadRequest, "")
	}
	pt := patch{
		status: sent("status"), title: sent("title"), channels: sent("channels"),
		perCustomer: sent("perCustomerLimit"), maxRedeem: sent("maxRedemptions"),
		startsAt: sent("startsAt"), endsAt: sent("endsAt"),
	}
	var v invalid
	if pt.status && b.Status == nil {
		v.add("body.status", "must not be null")
	}
	if pt.channels && b.Channels == nil {
		v.add("body.channels", "must not be null")
	}
	if pt.startsAt && b.StartsAt == nil {
		v.add("body.startsAt", "must not be null")
	}
	v.text("body.title", b.Title)
	if err := v.err(); err != nil {
		return offersapi.Offer{}, 0, err
	}
	now := s.clock.Now()
	var out offersdb.Offer
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		// The store's catalogue sequence first (lock order, see createOffer).
		if _, err := q.RecordCatalogueChange(ctx, offersdb.RecordCatalogueChangeParams{
			StoreID: ids.UUID(store), EntityID: ids.UUID(id), Op: "upsert", Now: now,
		}); err != nil {
			return fmt.Errorf("offers: catalogue change: %w", err)
		}
		cur, err := q.LockOffer(ctx, offersdb.LockOfferParams{ID: ids.UUID(id), StoreID: ids.UUID(store)})
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return fmt.Errorf("offers: lock offer: %w", err)
		}
		if cur.Version != version {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		if cur.Status == string(StatusEnded) {
			return problem.New(genproblems.InvalidTransition, "an ended offer cannot change")
		}
		arg, err := s.nextState(cur, b, pt, now)
		if err != nil {
			return err
		}
		if out, err = q.UpdateOffer(ctx, arg); err != nil {
			return fmt.Errorf("offers: update offer: %w", err)
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorUser, ActorID: p.UserID, Action: "offers.update", SubjectType: "offer",
			SubjectID: id.String(), Before: auditView(cur), After: auditView(out),
		})
		return err
	})
	if err != nil {
		return offersapi.Offer{}, 0, err
	}
	return s.toAPI(out), out.Version, nil
}

// nextState applies the sent fields to the locked row and checks the
// transition: live and scheduled pause and resume, any open offer ends (final),
// the terms never change, the start moves only before it starts, a flash
// offer's end never moves later, and the cap cannot drop below the uses.
func (s *Service) nextState(cur offersdb.Offer, b *offersapi.OfferUpdate, pt patch, now time.Time) (offersdb.UpdateOfferParams, error) {
	arg := offersdb.UpdateOfferParams{
		ID: cur.ID, StoreID: cur.StoreID, Title: cur.Title, Value: cur.Value, BuyQty: cur.BuyQty, GetQty: cur.GetQty,
		MinBillPaise: cur.MinBillPaise, Scope: cur.Scope, StoreItemIds: cur.StoreItemIds, CategoryID: cur.CategoryID,
		Channels: cur.Channels, PerCustomerLimit: cur.PerCustomerLimit, MaxRedemptions: cur.MaxRedemptions,
		StartsAt: cur.StartsAt, EndsAt: cur.EndsAt, Status: cur.Status, Now: now,
	}
	var v invalid
	if pt.title {
		arg.Title = nil
		if b.Title != nil {
			arg.Title = ptr(strings.TrimSpace(*b.Title))
		}
	}
	if pt.channels && b.Channels != nil {
		arg.Channels = string(*b.Channels)
	}
	if pt.perCustomer {
		arg.PerCustomerLimit = i32p(b.PerCustomerLimit)
	}
	if pt.maxRedeem {
		arg.MaxRedemptions = i32p(b.MaxRedemptions)
		if arg.MaxRedemptions != nil && *arg.MaxRedemptions < cur.Redemptions {
			v.add("body.maxRedemptions", "must not be below the uses so far")
		}
	}
	started := !cur.StartsAt.After(now)
	if pt.startsAt && b.StartsAt != nil {
		if started {
			v.add("body.startsAt", "an offer that has started keeps its start")
		} else {
			arg.StartsAt = *b.StartsAt
			if arg.StartsAt.Before(now) {
				arg.StartsAt = now
			}
		}
	}
	if pt.endsAt {
		switch {
		case b.EndsAt == nil:
			if cur.BatchID.Valid {
				v.add("body.endsAt", "a flash offer keeps its end")
			}
			arg.EndsAt = nil
		case !b.EndsAt.After(now):
			v.add("body.endsAt", "must be in the future")
		case cur.BatchID.Valid && (cur.EndsAt == nil || b.EndsAt.After(*cur.EndsAt)):
			v.add("body.endsAt", "a flash offer's end cannot move later")
		default:
			e := *b.EndsAt
			arg.EndsAt = &e
		}
	}
	if arg.EndsAt != nil && !arg.EndsAt.After(arg.StartsAt) {
		v.add("body.endsAt", "must be after the start")
	}
	if err := v.err(); err != nil {
		return arg, err
	}

	// Status: the label follows the dates unless the shop paused or ended it.
	want := Status(cur.Status)
	if pt.status && b.Status != nil {
		switch *b.Status {
		case offersapi.OfferUpdateStatusEnded:
			want = StatusEnded
		case offersapi.OfferUpdateStatusPaused:
			if want == StatusPaused {
				break
			}
			want = StatusPaused
		case offersapi.OfferUpdateStatusLive:
			if want == StatusPaused {
				want = StatusLive
				if arg.EndsAt != nil && !arg.EndsAt.After(now) {
					return arg, problem.New(genproblems.InvalidTransition, "the offer's end has passed")
				}
			}
		}
	}
	if want == StatusLive || want == StatusScheduled {
		want = StatusLive
		if arg.StartsAt.After(now) {
			want = StatusScheduled
		}
	}
	arg.Status = string(want)
	return arg, nil
}

// --- preview ----------------------------------------------------------------------------

func (s *Service) previewOffer(ctx context.Context, sid string, b *offersapi.PreviewOfferJSONRequestBody) (offersapi.OfferPreview, error) {
	if err := s.ready(ctx); err != nil {
		return offersapi.OfferPreview{}, err
	}
	store, err := pathID(sid)
	if err != nil {
		return offersapi.OfferPreview{}, err
	}
	if b == nil {
		return offersapi.OfferPreview{}, problem.New(genproblems.BadRequest, "")
	}
	now := s.clock.Now()
	q := s.q(ctx)
	t, err := s.validate(ctx, q, store, &b.Offer, "body.offer", now)
	if err != nil {
		return offersapi.OfferPreview{}, err
	}
	// The sample cart: this shop's items at their list price.
	var v invalid
	items := make([]ids.ID, 0, len(b.Lines))
	for i, l := range b.Lines {
		id, err := ids.Parse(l.StoreItemId)
		if err != nil {
			v.add(fmt.Sprintf("body.lines[%d].storeItemId", i), "not an item id")
			continue
		}
		items = append(items, id)
	}
	if err := v.err(); err != nil {
		return offersapi.OfferPreview{}, err
	}
	rows, err := q.ItemsOfStore(ctx, offersdb.ItemsOfStoreParams{StoreID: ids.UUID(store), Ids: uuids(items)})
	if err != nil {
		return offersapi.OfferPreview{}, fmt.Errorf("offers: preview items: %w", err)
	}
	price := make(map[ids.ID]int64, len(rows))
	for _, r := range rows {
		price[idOf(r.ID)] = r.PricePaise
	}
	cart := Cart{StoreID: store, Channel: ChannelOnline, At: now}
	if t.channels == ChannelsPOS {
		cart.Channel = ChannelPOS
	}
	for i, l := range b.Lines {
		p, ok := price[items[i]]
		if !ok {
			v.add(fmt.Sprintf("body.lines[%d].storeItemId", i), "not an item of this shop")
			continue
		}
		cart.Lines = append(cart.Lines, Line{StoreItemID: items[i], UnitPricePaise: p, Quantity: l.Quantity})
	}
	if err := v.err(); err != nil {
		return offersapi.OfferPreview{}, err
	}

	existing, err := q.PricingOffers(ctx, offersdb.PricingOffersParams{StoreID: ids.UUID(store), At: now})
	if err != nil {
		return offersapi.OfferPreview{}, fmt.Errorf("offers: preview offers: %w", err)
	}
	in := input{cart: cart, now: now}
	byCategory := t.scope == ScopeCategory
	for _, r := range existing {
		o := engineOffer(r)
		in.offers = append(in.offers, o)
		byCategory = byCategory || o.Scope == ScopeCategory
	}
	if byCategory {
		if in.cats, err = s.categories(ctx, q, cart.Lines); err != nil {
			return offersapi.OfferPreview{}, err
		}
	}
	// The new offer as it will price once it runs: live now, no caps (the
	// preview has no customer, so a per-customer limit would hide it).
	temp := Offer{
		ID: s.ids.New(now), Kind: t.kind, Value: t.value, Scope: t.scope, StoreItemIDs: t.items,
		CategoryID: t.category, Channels: t.channels, StartsAt: now, Status: StatusLive,
		BuyQty: ptrInt(t.buyQty), GetQty: ptrInt(t.getQty),
	}
	if t.title != nil {
		temp.Title = *t.title
	}
	if t.minBill != nil {
		temp.MinBillPaise = *t.minBill
	}
	without := compute(in)
	in.offers = append(slices.Clone(in.offers), temp)
	with := compute(in)

	out := offersapi.OfferPreview{
		Title: TitleOf(temp), Lines: make([]offersapi.OfferPreviewLine, 0, len(with.Lines)),
		GrossPaise: int(with.GrossPaise), ItemDiscountPaise: int(with.ItemDiscountPaise),
		BillDiscountPaise: int(with.BillDiscountPaise), TotalPaise: int(with.TotalPaise),
		TotalWithoutPaise: int(without.TotalPaise),
	}
	for i, l := range with.Lines {
		line := cart.Lines[i]
		out.Lines = append(out.Lines, offersapi.OfferPreviewLine{
			StoreItemId: line.StoreItemID.String(), Quantity: line.Quantity, UnitPricePaise: int(line.UnitPricePaise),
			GrossPaise: int(l.GrossPaise), DiscountPaise: int(l.DiscountPaise),
			TotalPaise: int(l.GrossPaise - l.DiscountPaise), FreeUnits: l.FreeUnits, ByThisOffer: l.OfferID == temp.ID,
		})
	}
	for _, a := range with.Applied {
		if a.OfferID == temp.ID {
			out.Applies = true
		}
	}
	return out, nil
}

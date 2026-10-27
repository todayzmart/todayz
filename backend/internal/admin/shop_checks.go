package admin

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	api "github.com/todayz/backend/gen/api/admin-businesses"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// The shop check (D-269, founder 27 Oct). A business and its stores go live
// as soon as onboarding is filled in; nobody approves it first. A field
// agent, admin or super_admin later checks the shop from a daily queue,
// newest first. Checked releases its payouts (the monthly close holds an
// unchecked business's payout as shop_unchecked). Rejected hides the shop at
// once and cancels its open online orders with the system reason
// shop_rejected (internal/orders, in the same transaction). Both are audited.

// Check statuses (businesses.check_status).
const (
	checkUnchecked = "unchecked"
	checkChecked   = "checked"
	checkRejected  = "rejected"
)

// The contract's AdminShopCheck.stores and .photos maxItems.
const (
	maxShopStores = 100 //settings:ok contract bound (AdminShopCheck.stores ≤ 100)
	maxShopPhotos = 20  //settings:ok contract bound (AdminShopCheck.photos ≤ 20)
)

// ShopOrderCanceller cancels every open online order of a business's stores
// with the system reason shop_rejected, in the caller's transaction, and
// returns how many it cancelled (internal/orders implements it).
type ShopOrderCanceller interface {
	CancelShopOrders(ctx context.Context, businessID ids.ID) (int, error)
}

func (s *Service) listShopChecks(ctx context.Context, limit *int, cursor *string) (api.AdminShopCheckList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminShopCheckList{}, err
	}
	lim, at, id, err := pageArgs(limit, cursor)
	if err != nil {
		return api.AdminShopCheckList{}, err
	}
	rows, err := s.q(ctx).ShopCheckQueue(ctx, admindb.ShopCheckQueueParams{
		BeforeAt: at, BeforeID: id,
		Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	})
	if err != nil {
		return api.AdminShopCheckList{}, err
	}
	out := api.AdminShopCheckList{Items: []api.AdminShopCheck{}, ServerNow: s.now()}
	if len(rows) > lim {
		last := rows[lim-1]
		lid, _ := ids.FromUUID(last.ID)
		c := encodeCursor(*last.VerifiedAt, lid)
		out.NextCursor = &c
		rows = rows[:lim]
	}
	page := make([]api.AdminShopCheck, 0, len(rows))
	for _, r := range rows {
		page = append(page, shopCheck(r.ID, r.BrandName, r.LegalName, r.Status, r.CheckStatus, r.CheckedAt, r.CheckNote,
			optText(r.Category), r.VerifiedAt, r.CreatedAt, r.ByFieldAgent, r.KycStatus))
	}
	if err := s.fillShopChecks(ctx, page); err != nil {
		return api.AdminShopCheckList{}, err
	}
	out.Items = page
	return out, nil
}

// shopCheckOf reads one business as the check shows it.
func (s *Service) shopCheckOf(ctx context.Context, id ids.ID) (api.AdminShopCheck, error) {
	r, err := s.q(ctx).ShopCheck(ctx, ids.UUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminShopCheck{}, notFound()
	}
	if err != nil {
		return api.AdminShopCheck{}, err
	}
	page := []api.AdminShopCheck{shopCheck(r.ID, r.BrandName, r.LegalName, r.Status, r.CheckStatus, r.CheckedAt, r.CheckNote,
		optText(r.Category), r.VerifiedAt, r.CreatedAt, r.ByFieldAgent, r.KycStatus)}
	if err := s.fillShopChecks(ctx, page); err != nil {
		return api.AdminShopCheck{}, err
	}
	return page[0], nil
}

func shopCheck(id pgtype.UUID, brand string, legal *string, status, check string, checkedAt *time.Time, note, category *string,
	liveAt *time.Time, createdAt time.Time, byAgent bool, kyc string,
) api.AdminShopCheck {
	return api.AdminShopCheck{
		Id: idString(id), BrandName: brand, LegalName: legal, Status: api.AdminBusinessStatus(status),
		CheckStatus: api.AdminCheckStatus(check), CheckedAt: utcPtr(checkedAt), CheckNote: note, Category: category,
		LiveAt: utcPtr(liveAt), CreatedAt: createdAt.UTC(), ByFieldAgent: byAgent,
		KycStatus: api.AdminShopCheckKycStatus(kyc),
		Stores:    []api.AdminShopCheckStore{}, Photos: []api.AdminShopCheckPhoto{},
	}
}

// optText is nil for an empty string (a business without a category).
func optText(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// fillShopChecks adds each shop's stores and photos with two queries for the
// whole page. A photo's preview is a presigned GET of the private object.
func (s *Service) fillShopChecks(ctx context.Context, page []api.AdminShopCheck) error {
	if len(page) == 0 {
		return nil
	}
	at := make(map[string]int, len(page))
	bids := make([]pgtype.UUID, 0, len(page))
	for i, c := range page {
		at[c.Id] = i
		id, _ := ids.Parse(c.Id)
		bids = append(bids, ids.UUID(id))
	}
	q := s.q(ctx)
	stores, err := q.ShopCheckStores(ctx, bids)
	if err != nil {
		return err
	}
	for _, r := range stores {
		i := at[idString(r.BusinessID)]
		if len(page[i].Stores) == maxShopStores {
			continue
		}
		st := api.AdminShopCheckStore{
			Id: idString(r.ID), Name: r.Name, Status: api.AdminShopCheckStoreStatus(r.Status),
			AddressLine1: r.AddressLine1, Locality: r.Locality, City: r.City, Pincode: r.Pincode, LiveAt: utcPtr(r.LiveAt),
		}
		if r.HasLocation {
			lat, lng := r.Lat, r.Lng
			st.Lat, st.Lng = &lat, &lng
		}
		page[i].Stores = append(page[i].Stores, st)
	}
	photos, err := q.ShopCheckPhotos(ctx, bids)
	if err != nil {
		return err
	}
	now := s.now()
	for _, r := range photos {
		i := at[idString(r.BusinessID)]
		if len(page[i].Photos) == maxShopPhotos {
			continue
		}
		p := api.AdminShopCheckPhoto{
			Id: idString(r.ID), Kind: api.AdminShopCheckPhotoKind(r.Kind), StoreId: optID(r.StoreID),
			Status: api.AdminShopCheckPhotoStatus(r.Status), CreatedAt: r.CreatedAt.UTC(),
		}
		if s.storage != nil {
			u := s.storage.GetURL(r.ObjectKey, now)
			p.PreviewUrl = &u
		}
		page[i].Photos = append(page[i].Photos, p)
	}
	return nil
}

// checkShop marks a live, unchecked shop checked. Payouts already held as
// shop_unchecked stay held until finance releases them on A-11; a close never
// lifts a hold, and later closes no longer hold.
func (s *Service) checkShop(ctx context.Context, actor, id ids.ID, note *string) (api.AdminShopCheck, error) {
	n, err := optNote(note, "body.note")
	if err != nil {
		return api.AdminShopCheck{}, err
	}
	err = s.shopCheckMove(ctx, actor, id, "business.check", func(ctx context.Context, q *admindb.Queries, status string) (map[string]any, error) {
		if status != bizVerified {
			return nil, invalidTransition("Only a live shop can be checked.")
		}
		now := s.now()
		if err := q.CheckBusiness(ctx, admindb.CheckBusinessParams{Now: &now, By: ids.UUID(actor), Note: n, ID: ids.UUID(id)}); err != nil {
			return nil, err
		}
		after := map[string]any{"checkStatus": checkChecked}
		if n != nil {
			after["note"] = *n
		}
		return after, nil
	})
	if err != nil {
		return api.AdminShopCheck{}, err
	}
	return s.shopCheckOf(ctx, id)
}

// rejectShop hides an unchecked shop and cancels its open online orders, in
// one transaction: no strike, held stock, credits and offers go back and each
// customer is told (internal/orders' cancel). Counter sales are untouched.
func (s *Service) rejectShop(ctx context.Context, actor, id ids.ID, reason string) (api.AdminShopRejected, error) {
	reason, err := cleanReason(reason)
	if err != nil {
		return api.AdminShopRejected{}, err
	}
	if s.shopOrders == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "admin: a shop reject needs the orders canceller")
		return api.AdminShopRejected{}, unavailable()
	}
	cancelled := 0
	err = s.shopCheckMove(ctx, actor, id, "business.check_reject", func(ctx context.Context, q *admindb.Queries, status string) (map[string]any, error) {
		if status != bizVerified {
			return nil, invalidTransition("Only a live shop can be rejected here; A-03 handles the others.")
		}
		now := s.now()
		if err := q.RejectBusinessCheck(ctx, admindb.RejectBusinessCheckParams{Now: &now, By: ids.UUID(actor), Note: &reason, ID: ids.UUID(id)}); err != nil {
			return nil, err
		}
		n, err := s.shopOrders.CancelShopOrders(ctx, id)
		if err != nil {
			return nil, err
		}
		cancelled = n
		return map[string]any{"status": bizRejected, "checkStatus": checkRejected, "reason": reason, "cancelledOrders": n}, nil
	})
	if err != nil {
		return api.AdminShopRejected{}, err
	}
	shop, err := s.shopCheckOf(ctx, id)
	if err != nil {
		return api.AdminShopRejected{}, err
	}
	return api.AdminShopRejected{Shop: shop, CancelledOrders: cancelled}, nil
}

// shopCheckMove locks the business, refuses anything not unchecked, runs
// change and writes the audit row, all in one transaction. A checked shop is
// suspended on A-03 instead; a rejected one stays rejected.
func (s *Service) shopCheckMove(ctx context.Context, actor, id ids.ID, action string,
	change func(ctx context.Context, q *admindb.Queries, status string) (after map[string]any, err error),
) error {
	if err := s.writeReady(ctx); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		cur, err := q.LockBusinessCheck(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if cur.CheckStatus != checkUnchecked {
			return invalidTransition("The shop has already been checked or rejected.")
		}
		after, err := change(ctx, q, cur.Status)
		if err != nil {
			return err
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: action,
			SubjectType: "business", SubjectID: id.String(),
			Before: map[string]any{"status": cur.Status, "checkStatus": cur.CheckStatus}, After: after,
		})
		return err
	})
}

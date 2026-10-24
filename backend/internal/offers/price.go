package offers

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/offersdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
)

var (
	_ Pricer   = (*Service)(nil)
	_ Redeemer = (*Service)(nil)
)

// --- Price --------------------------------------------------------------------

// Price implements Pricer: it reads the store's offers and the customer's use
// of them, then runs the engine. With feature_offers_enabled off it prices the
// cart with no offers.
func (s *Service) Price(ctx context.Context, cart Cart) (Quote, error) {
	if err := s.ready(ctx); err != nil {
		return Quote{}, err
	}
	at := cart.At
	if at.IsZero() {
		at = s.clock.Now()
	}
	on, err := s.enabled(ctx)
	if err != nil {
		return Quote{}, err
	}
	if !on || len(cart.Lines) == 0 {
		return compute(input{cart: cart, now: at}), nil
	}
	q := s.q(ctx)
	rows, err := q.PricingOffers(ctx, offersdb.PricingOffersParams{StoreID: ids.UUID(cart.StoreID), At: at})
	if err != nil {
		return Quote{}, fmt.Errorf("offers: pricing offers: %w", err)
	}
	in := input{cart: cart, now: at, offers: make([]Offer, 0, len(rows))}
	var limited []pgtype.UUID
	byCategory := false
	for _, r := range rows {
		o := engineOffer(r)
		in.offers = append(in.offers, o)
		if o.PerCustomerLimit > 0 {
			limited = append(limited, r.ID)
		}
		byCategory = byCategory || o.Scope == ScopeCategory
	}
	if byCategory {
		if in.cats, err = s.categories(ctx, q, cart.Lines); err != nil {
			return Quote{}, err
		}
	}
	if cart.CustomerID != ids.Zero && len(limited) > 0 {
		use, err := q.CustomerUsage(ctx, offersdb.CustomerUsageParams{CustomerID: ids.UUID(cart.CustomerID), OfferIds: limited})
		if err != nil {
			return Quote{}, fmt.Errorf("offers: customer usage: %w", err)
		}
		in.used = make(map[ids.ID]int, len(use))
		for _, u := range use {
			in.used[idOf(u.OfferID)] = int(u.Used)
		}
	}
	return compute(in), nil
}

// categories is, per cart line, the line's category and its ancestors.
func (s *Service) categories(ctx context.Context, q *offersdb.Queries, lines []Line) ([][]ids.ID, error) {
	items := make([]pgtype.UUID, len(lines))
	for i, l := range lines {
		items[i] = ids.UUID(l.StoreItemID)
	}
	rows, err := q.ItemCategoryChains(ctx, items)
	if err != nil {
		return nil, fmt.Errorf("offers: category chains: %w", err)
	}
	byItem := make(map[ids.ID][]ids.ID, len(rows))
	for _, r := range rows {
		byItem[idOf(r.StoreItemID)] = idList(r.CategoryIds)
	}
	out := make([][]ids.ID, len(lines))
	for i, l := range lines {
		out[i] = byItem[l.StoreItemID]
	}
	return out, nil
}

// --- Redeem and Release ---------------------------------------------------------

func (r Ref) channel() string {
	if r.POSSaleID != ids.Zero {
		return string(ChannelPOS)
	}
	return string(ChannelOnline)
}

func (r Ref) orderUUID() pgtype.UUID {
	if r.OrderID == ids.Zero {
		return pgtype.UUID{}
	}
	return ids.UUID(r.OrderID)
}

func (r Ref) saleUUID() pgtype.UUID {
	if r.POSSaleID == ids.Zero {
		return pgtype.UUID{}
	}
	return ids.UUID(r.POSSaleID)
}

// Redeem implements Redeemer. It runs in the caller's transaction (the
// context's), so an order that does not commit uses nothing. The offers are
// claimed in id order, so two orders never wait on each other in a cycle; the
// claim takes the offer's row lock, which also serialises the per-customer
// count below. Redeeming a ref twice is a no-op.
func (s *Service) Redeem(ctx context.Context, ref Ref, customerID ids.ID, quote Quote) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if (ref.OrderID == ids.Zero) == (ref.POSSaleID == ids.Zero) {
		return fmt.Errorf("offers: redeem needs exactly one of an order and a POS sale")
	}
	if len(quote.Applied) == 0 {
		return nil
	}
	q := s.q(ctx)
	held, err := q.RedemptionsOfRef(ctx, offersdb.RedemptionsOfRefParams{OrderID: ref.orderUUID(), PosSaleID: ref.saleUUID()})
	if err != nil {
		return fmt.Errorf("offers: redemptions of ref: %w", err)
	}
	if len(held) > 0 {
		return nil
	}
	applied := slices.Clone(quote.Applied)
	slices.SortFunc(applied, func(a, b Applied) int { return slices.Compare(a.OfferID[:], b.OfferID[:]) })
	now := s.clock.Now()
	var claimed []pgtype.UUID
	giveBack := func() {
		for _, id := range claimed {
			// Best effort: the caller rolls back on ErrUnavailable anyway.
			_ = q.GiveBackUse(ctx, id)
		}
	}
	user := pgtype.UUID{}
	if customerID != ids.Zero {
		user = ids.UUID(customerID)
	}
	for _, a := range applied {
		id := ids.UUID(a.OfferID)
		c, err := q.ClaimOffer(ctx, offersdb.ClaimOfferParams{ID: id, At: now})
		if db.IsNoRows(err) {
			giveBack()
			return ErrUnavailable
		}
		if err != nil {
			giveBack()
			return fmt.Errorf("offers: claim offer: %w", err)
		}
		claimed = append(claimed, id)
		if c.PerCustomerLimit != nil {
			if customerID == ids.Zero {
				giveBack()
				return ErrUnavailable
			}
			uses, err := q.CustomerUses(ctx, offersdb.CustomerUsesParams{OfferID: id, CustomerID: user})
			if err != nil {
				giveBack()
				return fmt.Errorf("offers: customer uses: %w", err)
			}
			if uses >= *c.PerCustomerLimit {
				giveBack()
				return ErrUnavailable
			}
		}
		if err := q.InsertRedemption(ctx, offersdb.InsertRedemptionParams{
			ID: ids.UUID(s.ids.New(now)), OfferID: id, OrderID: ref.orderUUID(), PosSaleID: ref.saleUUID(),
			CustomerID: user, DiscountPaise: a.DiscountPaise, Now: now,
		}); err != nil {
			giveBack()
			return fmt.Errorf("offers: insert redemption: %w", err)
		}
		s.event(ctx, q, user, "offer_redeemed", map[string]any{
			"offer_id": a.OfferID.String(), "channel": ref.channel(), "discount_paise": a.DiscountPaise,
		})
	}
	return nil
}

// Release implements Redeemer: it gives back every unreleased redemption of
// ref and lowers the offers' counters. A ref that redeemed nothing, or was
// already released, is a no-op.
func (s *Service) Release(ctx context.Context, ref Ref) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if (ref.OrderID == ids.Zero) == (ref.POSSaleID == ids.Zero) {
		return fmt.Errorf("offers: release needs exactly one of an order and a POS sale")
	}
	q := s.q(ctx)
	now := s.clock.Now()
	offerIDs, err := q.ReleaseRedemptions(ctx, offersdb.ReleaseRedemptionsParams{
		OrderID: ref.orderUUID(), PosSaleID: ref.saleUUID(), Now: &now,
	})
	if err != nil {
		return fmt.Errorf("offers: release redemptions: %w", err)
	}
	// Lock order: by offer id, like Redeem.
	slices.SortFunc(offerIDs, func(a, b pgtype.UUID) int { return slices.Compare(a.Bytes[:], b.Bytes[:]) })
	for _, id := range offerIDs {
		if err := q.GiveBackUse(ctx, id); err != nil {
			return fmt.Errorf("offers: give back use: %w", err)
		}
	}
	return nil
}

// --- has-offer lookups (basket and item search) -------------------------------------

// ItemOffers reports, per store item, whether a live online item-level offer
// covers it at time t (search.OfferLookup). A minimum-bill offer is the shop's,
// not the item's, so it counts for StoreOffers only.
func (s *Service) ItemOffers(ctx context.Context, itemIDs []ids.ID, t time.Time) (map[ids.ID]bool, error) {
	out := map[ids.ID]bool{}
	if len(itemIDs) == 0 {
		return out, nil
	}
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if on, err := s.enabled(ctx); err != nil || !on {
		return out, err
	}
	q := s.q(ctx)
	prices, err := q.ItemPrices(ctx, uuids(itemIDs))
	if err != nil {
		return nil, fmt.Errorf("offers: item stores: %w", err)
	}
	storeOf := make(map[ids.ID]ids.ID, len(prices))
	var stores []pgtype.UUID
	seen := map[ids.ID]bool{}
	for _, p := range prices {
		sid := idOf(p.StoreID)
		storeOf[idOf(p.ID)] = sid
		if !seen[sid] {
			seen[sid] = true
			stores = append(stores, p.StoreID)
		}
	}
	offs, err := s.liveByStore(ctx, q, stores, t)
	if err != nil {
		return nil, err
	}
	var chains map[ids.ID][]ids.ID
	for _, id := range itemIDs {
		sid, ok := storeOf[id]
		if !ok {
			continue
		}
		for _, o := range offs[sid] {
			if o.Kind == KindBillMin {
				continue
			}
			covers := o.Scope != ScopeCategory && o.covers(id, nil)
			if o.Scope == ScopeCategory {
				if chains == nil {
					if chains, err = s.chainsOf(ctx, q, itemIDs); err != nil {
						return nil, err
					}
				}
				covers = o.covers(id, chains[id])
			}
			if covers {
				out[id] = true
				break
			}
		}
	}
	return out, nil
}

// StoreOffers reports, per store, whether it has any live online offer at
// time t, a minimum-bill offer included (search.OfferLookup).
func (s *Service) StoreOffers(ctx context.Context, storeIDs []ids.ID, t time.Time) (map[ids.ID]bool, error) {
	out := map[ids.ID]bool{}
	if len(storeIDs) == 0 {
		return out, nil
	}
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if on, err := s.enabled(ctx); err != nil || !on {
		return out, err
	}
	offs, err := s.liveByStore(ctx, s.q(ctx), uuids(storeIDs), t)
	if err != nil {
		return nil, err
	}
	for sid, list := range offs {
		out[sid] = len(list) > 0
	}
	return out, nil
}

// liveByStore is the offers that can apply online at t, by store.
func (s *Service) liveByStore(ctx context.Context, q *offersdb.Queries, stores []pgtype.UUID, t time.Time) (map[ids.ID][]Offer, error) {
	rows, err := q.OffersOfStores(ctx, offersdb.OffersOfStoresParams{StoreIds: stores, At: t})
	if err != nil {
		return nil, fmt.Errorf("offers: offers of stores: %w", err)
	}
	out := map[ids.ID][]Offer{}
	for _, r := range rows {
		o := engineOffer(r)
		if !o.Live(t, ChannelOnline) {
			continue
		}
		sid := idOf(r.StoreID)
		out[sid] = append(out[sid], o)
	}
	return out, nil
}

func (s *Service) chainsOf(ctx context.Context, q *offersdb.Queries, items []ids.ID) (map[ids.ID][]ids.ID, error) {
	rows, err := q.ItemCategoryChains(ctx, uuids(items))
	if err != nil {
		return nil, fmt.Errorf("offers: category chains: %w", err)
	}
	out := make(map[ids.ID][]ids.ID, len(rows))
	for _, r := range rows {
		out[idOf(r.StoreItemID)] = idList(r.CategoryIds)
	}
	return out, nil
}

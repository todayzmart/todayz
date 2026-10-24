package orders

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	ordersapi "github.com/todayz/backend/gen/api/orders"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Price sources (order_items.price_source).
const (
	sourceCatalogue = "catalogue"
	sourceAskReply  = "ask_reply"
)

// cart is a checkout request, the same for a quote and a create.
type cart struct {
	storeID    string
	fulfilment string
	items      []ordersapi.CheckoutItem
	askReplyID *string
	credits    int
}

// quoted is a priced cart.
type quoted struct {
	store ordersdb.StoreForBookingRow
	now   time.Time
	lines []quotedLine

	subtotal, lineDiscounts, discount, itemsAfterOffers, itemSubtotal, fee, total int64

	creditsRequested, creditsAllowed, creditsUsed int
	creditsLimit                                  string
	offers                                        []AppliedOffer

	askReplyID   *ids.ID // the reply asked for, whether usable or not
	askUsable    bool    // the reply priced a line
	askPrice     AskPrice
	validUntil   *time.Time
	firstShort   int // index of the first line not fully in stock, -1 if none
	notOrderable bool
}

type quotedLine struct {
	row         ordersdb.ItemsForBookingRow
	id          ids.ID
	productID   ids.ID
	qty         int
	unitPrice   int64
	priceSource string
	available   int
	subtotal    int64
	discount    int64
	total       int64
	validUntil  *time.Time
}

func (l quotedLine) availability() ordersapi.LineAvailability {
	switch {
	case l.available >= l.qty:
		return ordersapi.LineAvailabilityInStock
	case l.available > 0:
		return ordersapi.LineAvailabilityShort
	default:
		return ordersapi.LineAvailabilityOutOfStock
	}
}

// bookableStore loads the cart's store and refuses one that cannot take an
// order: unknown (404), not live or not verified, no pickup, or a delivery
// cart (delivery is not built yet, BK-23–25): 409 store_not_bookable.
func (s *Service) bookableStore(ctx context.Context, c cart) (ordersdb.StoreForBookingRow, ids.ID, error) {
	sid, err := ids.Parse(c.storeID)
	if err != nil {
		return ordersdb.StoreForBookingRow{}, ids.Zero, fieldErr("body.storeId", "not an id")
	}
	st, err := s.q(ctx).StoreForBooking(ctx, ids.UUID(sid))
	if db.IsNoRows(err) {
		return ordersdb.StoreForBookingRow{}, ids.Zero, notFound()
	}
	if err != nil {
		return ordersdb.StoreForBookingRow{}, ids.Zero, fmt.Errorf("orders: store: %w", err)
	}
	if st.Status != "live" || !st.BusinessVerified || !st.AcceptsPickup || c.fulfilment != Pickup {
		return ordersdb.StoreForBookingRow{}, ids.Zero, problem.New(genproblems.StoreNotBookable, "")
	}
	return st, sid, nil
}

// price prices the cart's lines at the store: catalogue prices (or the ask
// reply's), what is available now, the offers and the credits. It writes
// nothing. A reply that cannot price a line is ignored here (the quote's
// rule); create refuses it.
func (s *Service) price(ctx context.Context, customerID ids.ID, st ordersdb.StoreForBookingRow, c cart, now time.Time) (quoted, error) {
	q := quoted{store: st, now: now, creditsRequested: max(c.credits, 0), firstShort: -1}
	storeID := mustID(st.ID)

	// The items: each a valid id, at most once, of this store.
	want := make([]ids.ID, len(c.items))
	seen := make(map[ids.ID]bool, len(c.items))
	keys := make([]ids.ID, 0, len(c.items))
	for i, it := range c.items {
		id, err := ids.Parse(it.StoreItemId)
		if err != nil {
			return quoted{}, fieldErr(fmt.Sprintf("body.items[%d].storeItemId", i), "not an id")
		}
		if seen[id] {
			return quoted{}, fieldErr(fmt.Sprintf("body.items[%d].storeItemId", i), "this item is already in the cart")
		}
		if it.Quantity < 1 || it.Quantity > maxLineQuantity {
			return quoted{}, fieldErr(fmt.Sprintf("body.items[%d].quantity", i), "out of range")
		}
		seen[id] = true
		want[i] = id
		keys = append(keys, id)
	}
	uuids := make([]pgtype.UUID, len(keys))
	for i, id := range keys {
		uuids[i] = ids.UUID(id)
	}
	rows, err := s.q(ctx).ItemsForBooking(ctx, ordersdb.ItemsForBookingParams{StoreID: st.ID, Ids: uuids})
	if err != nil {
		return quoted{}, fmt.Errorf("orders: items: %w", err)
	}
	byID := make(map[ids.ID]ordersdb.ItemsForBookingRow, len(rows))
	for _, r := range rows {
		byID[mustID(r.ID)] = r
	}

	// The ask reply: a usable one prices its item at the shop's reply.
	if c.askReplyID != nil {
		rid, err := ids.Parse(*c.askReplyID)
		if err != nil {
			return quoted{}, fieldErr("body.askReplyId", "not an id")
		}
		q.askReplyID = &rid
		if s.askPricer != nil {
			p, ok, err := s.askPricer.AskPrice(ctx, customerID, storeID, rid, now)
			if err != nil {
				return quoted{}, fmt.Errorf("orders: ask price: %w", err)
			}
			if ok && p.UnitPricePaise > 0 && now.Before(p.ValidUntil) && seen[p.StoreItemID] {
				q.askUsable, q.askPrice = true, p
			}
		}
		if q.askUsable {
			used, err := s.q(ctx).ActiveOrderForAskReply(ctx, ids.UUID(rid))
			if err != nil && !db.IsNoRows(err) {
				return quoted{}, fmt.Errorf("orders: ask reply in use: %w", err)
			}
			if err == nil {
				return quoted{}, problem.New(genproblems.AskReplyUsed, "").WithExtension("orderId", mustID(used).String())
			}
		}
	}

	plines := make([]PriceLine, len(want))
	for i, id := range want {
		r, ok := byID[id]
		if !ok {
			return quoted{}, fieldErr(fmt.Sprintf("body.items[%d].storeItemId", i), "not an item of this store")
		}
		l := quotedLine{row: r, id: id, productID: mustID(r.ProductID), qty: c.items[i].Quantity,
			unitPrice: r.PricePaise, priceSource: sourceCatalogue}
		if q.askUsable && q.askPrice.StoreItemID == id {
			l.unitPrice, l.priceSource = q.askPrice.UnitPricePaise, sourceAskReply
			vu := q.askPrice.ValidUntil
			l.validUntil, q.validUntil = &vu, &vu
		}
		l.available = int(r.OnHand - r.Reserved)
		if !r.IsListed || (r.ProductStatus != "active" && r.ProductStatus != "pending_review") {
			l.available = 0
		}
		l.subtotal = l.unitPrice * int64(l.qty)
		q.lines = append(q.lines, l)
		plines[i] = PriceLine{StoreItemID: id, ProductID: l.productID, UnitPricePaise: l.unitPrice, Quantity: l.qty,
			Fixed: l.priceSource == sourceAskReply}
	}

	// Offers, clamped so nothing goes below zero.
	pr, err := s.pricer.Price(ctx, customerID, storeID, plines)
	if err != nil {
		return quoted{}, fmt.Errorf("orders: offers: %w", err)
	}
	for i := range q.lines {
		l := &q.lines[i]
		if i < len(pr.LineDiscounts) {
			l.discount = min(max(pr.LineDiscounts[i], 0), l.subtotal)
		}
		l.total = l.subtotal - l.discount
		q.subtotal += l.subtotal
		q.lineDiscounts += l.discount
		if l.availability() != ordersapi.LineAvailabilityInStock && q.firstShort < 0 {
			q.firstShort = i
		}
	}
	q.itemSubtotal = q.subtotal - q.lineDiscounts
	bill := min(max(pr.BillDiscount, 0), q.itemSubtotal)
	q.discount = q.lineDiscounts + bill
	q.itemsAfterOffers = q.subtotal - q.discount
	q.offers = pr.Offers

	// Credits (W2-01): what the wallet, the per-order max and the caps allow.
	al, err := s.credits.Allow(ctx, customerID, storeID, q.creditsRequested, q.itemsAfterOffers)
	if err != nil {
		return quoted{}, fmt.Errorf("orders: credits: %w", err)
	}
	q.creditsAllowed = max(al.Allowed, 0)
	q.creditsUsed = min(q.creditsRequested, q.creditsAllowed, int(q.itemsAfterOffers/100))
	if q.creditsRequested > q.creditsUsed {
		q.creditsLimit = al.Limit
	}
	q.fee = 0 // pickup only
	q.total = q.itemsAfterOffers - int64(q.creditsUsed)*100 + q.fee
	q.notOrderable = q.firstShort >= 0
	return q, nil
}

// maxLineQuantity is the contract's per-line maximum (CheckoutItem.quantity).
const maxLineQuantity = 99

// view is the quote as the contract shows it.
func (q quoted) view() ordersapi.CheckoutQuote {
	out := ordersapi.CheckoutQuote{
		StoreId: mustID(q.store.ID).String(), Fulfilment: ordersapi.Pickup, ServerNow: q.now,
		Lines: make([]ordersapi.QuoteLine, len(q.lines)), OffersApplied: []ordersapi.OfferApplied{},
		SubtotalPaise: int(q.subtotal), DiscountPaise: int(q.discount), ItemSubtotalPaise: int(q.itemSubtotal),
		ItemsAfterOffersPaise: int(q.itemsAfterOffers), DeliveryFeePaise: int(q.fee), TotalPaise: int(q.total),
		CreditsRequested: q.creditsRequested, CreditsAllowed: q.creditsAllowed, CreditsUsed: q.creditsUsed,
		Orderable: !q.notOrderable, ValidUntil: q.validUntil,
	}
	if q.creditsLimit != "" {
		l := ordersapi.CreditsLimit(q.creditsLimit)
		out.CreditsLimit = &l
	}
	for _, o := range q.offers {
		out.OffersApplied = append(out.OffersApplied, ordersapi.OfferApplied{
			OfferId: o.OfferID.String(), Title: o.Title, DiscountPaise: int(o.DiscountPaise),
		})
	}
	for i, l := range q.lines {
		ql := ordersapi.QuoteLine{
			StoreItemId: l.id.String(), ProductId: l.productID.String(), Name: l.row.Name, Variant: l.row.Variant,
			PackSize: float32(l.row.PackSize), Unit: l.row.Unit, ImageKey: l.row.ImageKey,
			Quantity: l.qty, AvailableQuantity: max(l.available, 0), Availability: l.availability(),
			UnitPricePaise: int(l.unitPrice), LineSubtotalPaise: int(l.subtotal), DiscountPaise: int(l.discount),
			LineTotalPaise: int(l.total), PriceSource: ordersapi.PriceSource(l.priceSource), ValidUntil: l.validUntil,
		}
		if l.row.MrpPaise != nil {
			m := int(*l.row.MrpPaise)
			ql.MrpPaise = &m
		}
		out.Lines[i] = ql
	}
	return out
}

// quoteCart prices a cart (C-11). It writes nothing.
func (s *Service) quoteCart(ctx context.Context, c cart) (ordersapi.CheckoutQuote, error) {
	if err := s.ready(ctx); err != nil {
		return ordersapi.CheckoutQuote{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return ordersapi.CheckoutQuote{}, err
	}
	st, _, err := s.bookableStore(ctx, c)
	if err != nil {
		return ordersapi.CheckoutQuote{}, asProblem(err)
	}
	q, err := s.price(ctx, p.UserID, st, c, s.now())
	if err != nil {
		return ordersapi.CheckoutQuote{}, asProblem(err)
	}
	return q.view(), nil
}

// displayNumber is the order number customers and shops read.
func displayNumber(o ordersdb.Order) string {
	if o.DisplayNumber != nil {
		return *o.DisplayNumber
	}
	return strconv.FormatInt(o.Number, 10)
}

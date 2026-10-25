package orders

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	ordersapi "github.com/todayz/backend/gen/api/orders"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Page sizes of the order lists (04 §1: at most 100).
const (
	defaultLimit = 20
	maxLimit     = 100
)

// withCode says whether a customer view may carry the pickup code and the
// shop's phone (D-220): only the owner's GET of their order (never an action's
// response, which the idempotency layer may store).
type withCode bool

// --- one order ------------------------------------------------------------------

// detail is an order with what both views show.
type detail struct {
	o        ordersdb.Order
	lines    []ordersdb.OrderLinesRow
	timeline []ordersdb.OrderTimelineRow
}

func (s *Service) loadDetail(ctx context.Context, o ordersdb.Order) (detail, error) {
	q := s.q(ctx)
	lines, err := q.OrderLines(ctx, o.ID)
	if err != nil {
		return detail{}, fmt.Errorf("orders: lines: %w", err)
	}
	tl, err := q.OrderTimeline(ctx, o.ID)
	if err != nil {
		return detail{}, fmt.Errorf("orders: timeline: %w", err)
	}
	return detail{o: o, lines: lines, timeline: tl}, nil
}

// customerOrder reads one of the customer's orders (C-12). A missing order
// and another customer's are the same 404.
func (s *Service) customerOrder(ctx context.Context, userID, orderID ids.ID, code withCode) (ordersapi.Order, error) {
	if err := s.ready(ctx); err != nil {
		return ordersapi.Order{}, err
	}
	o, err := s.q(ctx).GetOrder(ctx, ids.UUID(orderID))
	if db.IsNoRows(err) || (err == nil && mustID(o.CustomerID) != userID) {
		return ordersapi.Order{}, notFound()
	}
	if err != nil {
		return ordersapi.Order{}, problem.Wrap(genproblems.Internal, err)
	}
	d, err := s.loadDetail(ctx, o)
	if err != nil {
		return ordersapi.Order{}, problem.Wrap(genproblems.Internal, err)
	}
	st, err := s.q(ctx).OrderStore(ctx, o.StoreID)
	if err != nil {
		return ordersapi.Order{}, problem.Wrap(genproblems.Internal, err)
	}
	return s.customerView(ctx, d, st, code), nil
}

func (s *Service) customerView(ctx context.Context, d detail, st ordersdb.OrderStoreRow, code withCode) ordersapi.Order {
	o, now := d.o, s.now()
	out := ordersapi.Order{
		Id: mustID(o.ID).String(), Number: displayNumber(o), Status: ordersapi.OrderStatus(o.Status),
		StatusReason: o.StatusReason, StatusNote: o.StatusNote, Fulfilment: ordersapi.Fulfilment(o.Fulfilment),
		Store:         orderStore(st),
		SubtotalPaise: int(o.SubtotalPaise), DiscountPaise: int(o.DiscountPaise), ItemSubtotalPaise: int(o.ItemSubtotalPaise),
		ItemsAfterOffersPaise: int(o.ItemsAfterOffersPaise), DeliveryFeePaise: int(o.DeliveryFeePaise),
		TotalPaise: int(o.TotalPaise), CreditsUsed: int(o.CreditsUsed), CashbackCredits: int(o.CashbackCredits),
		PaymentStatus: ordersapi.PaymentStatus(o.PaymentStatus), PaymentMethod: paymentMethod(o.PaymentMethod),
		PlacedAt: o.PlacedAt.UTC(), AcceptBy: utc(o.AcceptBy), AcceptedAt: utc(o.AcceptedAt),
		PartialConfirmBy: utc(o.PartialConfirmBy), HoldUntil: utc(o.HoldUntil), ExtendedAt: utc(o.ExtendedAt),
		CollectedAt: utc(o.CollectedAt), CodeLocked: o.CodeLockedAt != nil,
		CanExtend:  o.Fulfilment == Pickup && handable(o.Status) && o.ExtendedAt == nil && o.HoldUntil != nil && now.Before(*o.HoldUntil),
		DispatchBy: utc(o.DispatchBy), DeliveryFailedAt: utc(o.DeliveryFailedAt), ReturnOutcome: returnOutcome(o.ReturnOutcome),
		ReturnConfirmedAt: utc(o.ReturnConfirmedAt), DeliveryAddress: readAddress(o.AddressSnapshot),
		Lines: make([]ordersapi.OrderLine, 0, len(d.lines)), Timeline: timeline(statusChanges(d.timeline)),
		ServerNow: now, Version: int(o.Version),
	}
	if id, ok := ids.FromUUID(o.AskReplyID); ok {
		out.AskReplyId = ptr(id.String())
	}
	for _, l := range d.lines {
		sn := readSnapshot(l.ProductSnapshot)
		out.Lines = append(out.Lines, ordersapi.OrderLine{
			Id: mustID(l.ID).String(), StoreItemId: mustID(l.StoreItemID).String(), Name: sn.Name, Variant: sn.Variant,
			PackSize: float32(sn.PackSize), Unit: sn.Unit, ImageKey: sn.ImageKey, UnitPricePaise: int(l.UnitPricePaise),
			QuantityRequested: int(l.QuantityRequested), QuantityAccepted: intPtr(l.QuantityAccepted),
			LineTotalPaise: int(l.LineTotalPaise), PriceSource: ordersapi.PriceSource(l.PriceSource),
		})
	}
	if bool(code) && codeShown(o.Status) && o.CodeLockedAt == nil && s.codes != nil && len(o.CodeEnc) > 0 {
		c, err := s.codes.open(mustID(o.ID), o.CodeEnc)
		if err != nil {
			// The order is still shown; the error names no code.
			telemetry.Logger(ctx).ErrorContext(ctx, "orders: pickup code does not open", "order_id", mustID(o.ID).String())
		} else {
			out.Code = &c
		}
	}
	if bool(code) && slices.Contains(openStatuses, o.Status) {
		out.Store.Phone = s.storePhone(ctx, st)
	}
	return out
}

// storeOrder reads one order of the store (B-04) and marks a new order seen.
func (s *Service) storeOrder(ctx context.Context, storeID, orderID ids.ID) (ordersapi.StoreOrder, error) {
	if err := s.ready(ctx); err != nil {
		return ordersapi.StoreOrder{}, err
	}
	o, err := s.q(ctx).GetOrder(ctx, ids.UUID(orderID))
	if db.IsNoRows(err) || (err == nil && mustID(o.StoreID) != storeID) {
		return ordersapi.StoreOrder{}, notFound()
	}
	if err != nil {
		return ordersapi.StoreOrder{}, problem.Wrap(genproblems.Internal, err)
	}
	if o.Status == StatusPlaced && o.SeenAt == nil {
		now := s.now()
		if err := s.q(ctx).MarkSeen(ctx, ordersdb.MarkSeenParams{Now: &now, StoreID: o.StoreID, Ids: []pgtype.UUID{o.ID}}); err != nil {
			return ordersapi.StoreOrder{}, problem.Wrap(genproblems.Internal, err)
		}
	}
	return s.storeOrderView(ctx, o)
}

// storeOrderView is the order as its shop sees it.
func (s *Service) storeOrderView(ctx context.Context, o ordersdb.Order) (ordersapi.StoreOrder, error) {
	d, err := s.loadDetail(ctx, o)
	if err != nil {
		return ordersapi.StoreOrder{}, problem.Wrap(genproblems.Internal, err)
	}
	cu, err := s.q(ctx).CustomerForStore(ctx, o.CustomerID)
	if err != nil && !db.IsNoRows(err) {
		return ordersapi.StoreOrder{}, problem.Wrap(genproblems.Internal, err)
	}
	out := ordersapi.StoreOrder{
		Id: mustID(o.ID).String(), Number: displayNumber(o), Status: ordersapi.OrderStatus(o.Status),
		StatusReason: o.StatusReason, StatusNote: o.StatusNote, Fulfilment: ordersapi.Fulfilment(o.Fulfilment),
		Customer: ordersapi.OrderCustomer{
			Name: cu.Name, PhoneMasked: s.maskedPhone(ctx, cu.PhoneEnc), Phone: s.deliveryPhone(ctx, o.Status, cu.PhoneEnc),
		},
		SubtotalPaise: int(o.SubtotalPaise), DiscountPaise: int(o.DiscountPaise), ItemSubtotalPaise: int(o.ItemSubtotalPaise),
		ItemsAfterOffersPaise: int(o.ItemsAfterOffersPaise), DeliveryFeePaise: int(o.DeliveryFeePaise),
		TotalPaise: int(o.TotalPaise), CreditsUsed: int(o.CreditsUsed),
		PaymentStatus: ordersapi.PaymentStatus(o.PaymentStatus), PaymentMethod: paymentMethod(o.PaymentMethod),
		PlacedAt: o.PlacedAt.UTC(), AcceptBy: utc(o.AcceptBy), AcceptedAt: utc(o.AcceptedAt),
		PartialConfirmBy: utc(o.PartialConfirmBy), HoldUntil: utc(o.HoldUntil), ExtendedAt: utc(o.ExtendedAt),
		CollectedAt: utc(o.CollectedAt), CodeLocked: o.CodeLockedAt != nil,
		CodeAttemptsLeft: max(codeTries-int(o.CodeAttempts), 0),
		DispatchBy:       utc(o.DispatchBy), DeliveryFailedAt: utc(o.DeliveryFailedAt), ReturnOutcome: returnOutcome(o.ReturnOutcome),
		ReturnConfirmedAt: utc(o.ReturnConfirmedAt), DeliveryAddress: readAddress(o.AddressSnapshot),
		Lines: make([]ordersapi.StoreOrderLine, 0, len(d.lines)), Timeline: timeline(d.timeline),
		ServerNow: s.now(), Version: int(o.Version),
	}
	for _, l := range d.lines {
		sn := readSnapshot(l.ProductSnapshot)
		out.Lines = append(out.Lines, ordersapi.StoreOrderLine{
			Id: mustID(l.ID).String(), StoreItemId: mustID(l.StoreItemID).String(), Name: sn.Name, Variant: sn.Variant,
			PackSize: float32(sn.PackSize), Unit: sn.Unit, ImageKey: sn.ImageKey, UnitPricePaise: int(l.UnitPricePaise),
			QuantityRequested: int(l.QuantityRequested), QuantityAccepted: intPtr(l.QuantityAccepted),
			LineTotalPaise: int(l.LineTotalPaise), PriceSource: ordersapi.PriceSource(l.PriceSource),
			Available: int(max(l.Available, 0)),
		})
	}
	return out, nil
}

// storeOrderAfter reads the order a staff action just changed, for its
// response (the action's transaction has committed).
func (s *Service) storeOrderAfter(ctx context.Context, orderID ids.ID) (ordersapi.StoreOrder, error) {
	o, err := s.q(ctx).GetOrder(ctx, ids.UUID(orderID))
	if err != nil {
		return ordersapi.StoreOrder{}, problem.Wrap(genproblems.Internal, err)
	}
	return s.storeOrderView(ctx, o)
}

// deliveryPhone is the customer's full phone while their delivery order is
// out for delivery, so the rider can call (BK-27); null in every other
// status and without the PII key. Never logged.
func (s *Service) deliveryPhone(ctx context.Context, status string, enc []byte) *string {
	if status != StatusOutForDelivery || s.phones == nil || len(enc) == 0 {
		return nil
	}
	plain, err := s.phones.Open("phone", enc)
	if err != nil {
		telemetry.Logger(ctx).WarnContext(ctx, "orders: customer phone does not open")
		return nil
	}
	p := string(plain)
	return &p
}

// storePhone is the shop's own contact phone (stores.phone_enc, sealed by
// business under "phone:<store id>"), or null when it has none or it will not
// open. Never the owner's or a staff login phone (D-220). Never logged.
func (s *Service) storePhone(ctx context.Context, st ordersdb.OrderStoreRow) *string {
	if s.phones == nil || len(st.PhoneEnc) == 0 {
		return nil
	}
	plain, err := s.phones.Open("phone:"+mustID(st.ID).String(), st.PhoneEnc)
	if err != nil {
		telemetry.Logger(ctx).WarnContext(ctx, "orders: store phone does not open")
		return nil
	}
	p := string(plain)
	return &p
}

// maskedPhone is the customer's phone as a shop sees it (D-60): masked, or
// null without the PII key.
func (s *Service) maskedPhone(ctx context.Context, enc []byte) *string {
	if s.phones == nil || len(enc) == 0 {
		return nil
	}
	plain, err := s.phones.Open("phone", enc)
	if err != nil {
		telemetry.Logger(ctx).WarnContext(ctx, "orders: customer phone does not open")
		return nil
	}
	m := pii.MaskPhone(string(plain))
	return &m
}

func orderStore(st ordersdb.OrderStoreRow) ordersapi.OrderStore {
	var parts []string
	for _, p := range []*string{st.AddressLine1, st.AddressLine2} {
		if p != nil && strings.TrimSpace(*p) != "" {
			parts = append(parts, strings.TrimSpace(*p))
		}
	}
	return ordersapi.OrderStore{
		Id: mustID(st.ID).String(), Slug: st.Slug, Name: st.Name, Locality: st.Locality,
		AddressLine: strings.Join(parts, ", "),
	}
}

// statusChanges keeps the events that move the status: the customer's
// timeline leaves out a store-side note on the same status, such as a
// failed delivery's return being settled (returned, damaged, unconfirmed).
func statusChanges(rows []ordersdb.OrderTimelineRow) []ordersdb.OrderTimelineRow {
	out := make([]ordersdb.OrderTimelineRow, 0, len(rows))
	for _, r := range rows {
		if r.FromStatus != nil && *r.FromStatus == r.ToStatus {
			continue
		}
		out = append(out, r)
	}
	return out
}

func timeline(rows []ordersdb.OrderTimelineRow) []ordersapi.OrderTimelineEvent {
	out := make([]ordersapi.OrderTimelineEvent, len(rows))
	for i, r := range rows {
		ev := ordersapi.OrderTimelineEvent{
			Actor: ordersapi.OrderActor(r.ActorKind), At: r.CreatedAt.UTC(), ToStatus: ordersapi.OrderStatus(r.ToStatus),
			Reason: r.Reason,
		}
		if r.FromStatus != nil {
			ev.FromStatus = ptr(ordersapi.OrderStatus(*r.FromStatus))
		}
		out[i] = ev
	}
	return out
}

func readSnapshot(raw json.RawMessage) snapshot {
	var sn snapshot
	_ = json.Unmarshal(raw, &sn) // written by create; a bad row shows blanks
	return sn
}

func returnOutcome(o *string) *ordersapi.ReturnOutcome {
	if o == nil {
		return nil
	}
	return ptr(ordersapi.ReturnOutcome(*o))
}

// readAddress is a delivery order's address snapshot; nil for pickup.
func readAddress(raw []byte) *ordersapi.OrderAddress {
	if len(raw) == 0 {
		return nil
	}
	var a ordersapi.OrderAddress
	if json.Unmarshal(raw, &a) != nil {
		return nil // written by create; a bad row shows none
	}
	return &a
}

func paymentMethod(m *string) *ordersapi.PaymentMethod {
	if m == nil {
		return nil
	}
	return ptr(ordersapi.PaymentMethod(*m))
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	return ptr(t.UTC())
}

func intPtr(n *int32) *int {
	if n == nil {
		return nil
	}
	return ptr(int(*n))
}

// --- lists ------------------------------------------------------------------------

// storeTab is one B-04 tab: its statuses and, for delivery_failed, whether
// the return is settled.
type storeTab struct {
	statuses []string
	settled  *bool
}

var storeTabs = map[ordersapi.ListStoreOrdersParamsStatus]storeTab{
	ordersapi.ListStoreOrdersParamsStatusNew:            {statuses: []string{StatusPlaced}},
	ordersapi.ListStoreOrdersParamsStatusAccepted:       {statuses: []string{StatusAwaitingCustomer, StatusAccepted}},
	ordersapi.ListStoreOrdersParamsStatusReady:          {statuses: []string{StatusReady}},
	ordersapi.ListStoreOrdersParamsStatusOutForDelivery: {statuses: []string{StatusOutForDelivery}},
	ordersapi.ListStoreOrdersParamsStatusReturns:        {statuses: []string{StatusDeliveryFailed}, settled: ptr(false)},
	ordersapi.ListStoreOrdersParamsStatusDone:           {statuses: []string{StatusCollected, StatusDelivered, StatusDeliveryFailed}, settled: ptr(true)},
	ordersapi.ListStoreOrdersParamsStatusMissed:         {statuses: []string{StatusRejected, StatusExpired, StatusCancelled}},
}

// storeOrders lists one tab of the store's orders, newest first. Listing the
// new tab marks the orders shown as seen.
func (s *Service) storeOrders(ctx context.Context, storeID ids.ID, p ordersapi.ListStoreOrdersParams) (ordersapi.StoreOrderList, error) {
	if err := s.ready(ctx); err != nil {
		return ordersapi.StoreOrderList{}, err
	}
	tabName := ordersapi.ListStoreOrdersParamsStatusNew
	if p.Status != nil {
		tabName = *p.Status
	}
	tab, ok := storeTabs[tabName]
	if !ok {
		return ordersapi.StoreOrderList{}, fieldErr("query.status", "not a tab")
	}
	lim, at, before, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return ordersapi.StoreOrderList{}, err
	}
	q := s.q(ctx)
	rows, err := q.ListStoreOrders(ctx, ordersdb.ListStoreOrdersParams{
		StoreID: ids.UUID(storeID), Statuses: tab.statuses, Settled: tab.settled, BeforeAt: at, BeforeID: before, Lim: i32(lim + 1),
	})
	if err != nil {
		return ordersapi.StoreOrderList{}, problem.Wrap(genproblems.Internal, err)
	}
	now := s.now()
	out := ordersapi.StoreOrderList{Items: []ordersapi.StoreOrderSummary{}, ServerNow: now}
	more := len(rows) > lim
	if more {
		rows = rows[:lim]
	}
	var unseen []pgtype.UUID
	for _, r := range rows {
		if r.Status == StatusPlaced && r.SeenAt == nil {
			unseen = append(unseen, r.ID)
		}
		out.Items = append(out.Items, ordersapi.StoreOrderSummary{
			Id: mustID(r.ID).String(), Number: number(r.DisplayNumber, r.ID), Status: ordersapi.OrderStatus(r.Status),
			StatusReason: r.StatusReason, Fulfilment: ordersapi.Fulfilment(r.Fulfilment), TotalPaise: int(r.TotalPaise),
			PlacedAt: r.PlacedAt.UTC(), AcceptBy: utc(r.AcceptBy), HoldUntil: utc(r.HoldUntil), Version: int(r.Version),
			Seen: r.SeenAt != nil, CustomerName: r.CustomerName, ItemCount: int(r.ItemCount),
			CustomerPhone: s.deliveryPhone(ctx, r.Status, r.PhoneEnc), DispatchBy: utc(r.DispatchBy),
			DeliveryFailedAt: utc(r.DeliveryFailedAt), ReturnOutcome: returnOutcome(r.ReturnOutcome),
		})
	}
	if len(unseen) > 0 {
		if err := q.MarkSeen(ctx, ordersdb.MarkSeenParams{Now: &now, StoreID: ids.UUID(storeID), Ids: unseen}); err != nil {
			return ordersapi.StoreOrderList{}, problem.Wrap(genproblems.Internal, err)
		}
	}
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		out.NextCursor = ptr(encodeCursor(last.PlacedAt, mustID(last.ID)))
	}
	return out, nil
}

// customerOrders lists the customer's active or past orders, newest first.
func (s *Service) customerOrders(ctx context.Context, userID ids.ID, p ordersapi.ListMyOrdersParams) (ordersapi.OrderList, error) {
	if err := s.ready(ctx); err != nil {
		return ordersapi.OrderList{}, err
	}
	statuses := openStatuses
	if p.Status != nil {
		switch *p.Status {
		case ordersapi.Active:
		case ordersapi.Past:
			statuses = pastStatuses
		default:
			return ordersapi.OrderList{}, fieldErr("query.status", "active or past")
		}
	}
	lim, at, before, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return ordersapi.OrderList{}, err
	}
	rows, err := s.q(ctx).ListCustomerOrders(ctx, ordersdb.ListCustomerOrdersParams{
		CustomerID: ids.UUID(userID), Statuses: statuses, BeforeAt: at, BeforeID: before, Lim: i32(lim + 1),
	})
	if err != nil {
		return ordersapi.OrderList{}, problem.Wrap(genproblems.Internal, err)
	}
	out := ordersapi.OrderList{Items: []ordersapi.OrderSummary{}, ServerNow: s.now()}
	more := len(rows) > lim
	if more {
		rows = rows[:lim]
	}
	for _, r := range rows {
		out.Items = append(out.Items, ordersapi.OrderSummary{
			Id: mustID(r.ID).String(), Number: number(r.DisplayNumber, r.ID), Status: ordersapi.OrderStatus(r.Status),
			StatusReason: r.StatusReason, Fulfilment: ordersapi.Fulfilment(r.Fulfilment), TotalPaise: int(r.TotalPaise),
			PlacedAt: r.PlacedAt.UTC(), AcceptBy: utc(r.AcceptBy), HoldUntil: utc(r.HoldUntil), Version: int(r.Version),
			ItemCount: int(r.ItemCount),
			Store: orderStore(ordersdb.OrderStoreRow{
				ID: r.StoreID, Slug: r.StoreSlug, Name: r.StoreName, AddressLine1: r.AddressLine1,
				AddressLine2: r.AddressLine2, Locality: r.StoreLocality,
			}),
		})
	}
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		out.NextCursor = ptr(encodeCursor(last.PlacedAt, mustID(last.ID)))
	}
	return out, nil
}

// number is a list row's order number: the display number, which the
// database always sets; the id's tail only if it somehow is not.
func number(display *string, id pgtype.UUID) string {
	if display != nil {
		return *display
	}
	s := mustID(id).String()
	return s[len(s)-6:]
}

// pageArgs reads limit and cursor. A cursor that does not decode is 422 on
// query.cursor.
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

// encodeCursor makes the opaque cursor after (placedAt, id).
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
		return time.Time{}, ids.Zero, errors.New("orders: cursor has no id")
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

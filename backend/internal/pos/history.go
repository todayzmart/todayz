package pos

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	posapi "github.com/todayz/backend/gen/api/pos"
	"github.com/todayz/backend/gen/sqlc/posdb"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// exportBatch is how many rows the CSV export reads at a time.
const exportBatch = 500 //settings:ok a read batch size, not a product number

// SalesFilter is the Counter sales filters (D-268), the same on the list,
// the summary and the export. From and To are IST calendar days, inclusive.
type SalesFilter struct {
	From, To *openapi_types.Date
	Payment  *posapi.PosPaymentMethod
	Status   *posapi.PosSaleStatus
	Link     *posapi.PosSaleLinkFilter
	Offline  *bool
	Cashier  *string
}

// salesWhere is a SalesFilter as the queries take it: the days as
// [fromAt, toAt) in IST.
type salesWhere struct {
	fromAt, toAt          *time.Time
	payment, status, link *string
	offline               *bool
	cashier               pgtype.UUID
}

// dayStart is the start of the IST calendar day d.
func dayStart(d openapi_types.Date) time.Time {
	y, m, dd := d.Date()
	return time.Date(y, m, dd, 0, 0, 0, 0, clock.IST)
}

func (f SalesFilter) where() (salesWhere, error) {
	var w salesWhere
	if f.From != nil {
		w.fromAt = ptr(dayStart(*f.From))
	}
	if f.To != nil {
		w.toAt = ptr(dayStart(*f.To).AddDate(0, 0, 1))
	}
	if w.fromAt != nil && w.toAt != nil && !w.toAt.After(*w.fromAt) {
		return w, fieldErr("query.to", "before from")
	}
	if f.Payment != nil {
		w.payment = ptr(string(*f.Payment))
	}
	if f.Status != nil {
		w.status = ptr(string(*f.Status))
	}
	if f.Link != nil {
		w.link = ptr(string(*f.Link))
	}
	w.offline = f.Offline
	if f.Cashier != nil {
		id, err := ids.Parse(*f.Cashier)
		if err != nil {
			return w, fieldErr("query.cashier", "not an id")
		}
		w.cashier = ids.UUID(id)
	}
	return w, nil
}

// Sales are the store's sales, online and synced, newest first by sold_at
// (GET {S}/pos/sales, B-05 sales history, W2-16; Counter sales filters,
// D-268), with each link's status and, for a failed offline counter code,
// why. The customer is named only for a bill_qr or counter_code link; no
// phone or receipt token is read.
func (s *Service) Sales(ctx context.Context, sc authz.Scope, limit *int, cursor *string, f SalesFilter) (posapi.PosSaleList, error) {
	if err := s.ready(ctx); err != nil {
		return posapi.PosSaleList{}, err
	}
	w, err := f.where()
	if err != nil {
		return posapi.PosSaleList{}, err
	}
	lim := defaultLimit
	if limit != nil {
		lim = min(max(*limit, 1), maxLimit)
	}
	var beforeAt *time.Time
	var beforeID pgtype.UUID
	if cursor != nil && *cursor != "" {
		at, id, err := decodeCursor(*cursor)
		if err != nil {
			return posapi.PosSaleList{}, fieldErr("query.cursor", "not a cursor from this list")
		}
		beforeAt, beforeID = &at, ids.UUID(id)
	}
	rows, err := s.salesPage(ctx, sc, w, beforeAt, beforeID, lim+1)
	if err != nil {
		return posapi.PosSaleList{}, err
	}
	out := posapi.PosSaleList{Items: make([]posapi.PosSaleSummary, 0, min(len(rows), lim))}
	if len(rows) > lim {
		last := rows[lim-1]
		out.NextCursor = ptr(encodeCursor(last.SoldAt, mustID(last.ID)))
		rows = rows[:lim]
	}
	for _, r := range rows {
		v := posapi.PosSaleSummary{
			Id: mustID(r.ID).String(), ClientId: mustID(r.ClientID).String(), SoldAt: r.SoldAt, ReceivedAt: r.ReceivedAt,
			TotalPaise: int(r.TotalPaise), LineCount: int(r.LineCount), UnitCount: int(r.UnitCount),
			PaymentMethod: posapi.PosPaymentMethod(r.PaymentMethod), CreditsUsed: int(r.CreditsUsed),
			CashbackCredits: int(r.CashbackCredits), Status: posapi.PosSaleStatus(r.Status), ReversedAt: r.ReversedAt,
			Offline: r.DeviceSeq != nil, Cashier: cashierView(r.CashierUserID, r.CashierName),
		}
		if r.LinkMethod != nil {
			v.Link = &posapi.PosSaleLink{
				Method: posapi.PosLinkMethod(*r.LinkMethod), Status: posapi.PosLinkStatus(deref(r.LinkStatus)),
				CustomerName: optString(r.CustomerName), Failure: linkFailure(r.LinkFailReason),
			}
		}
		out.Items = append(out.Items, v)
	}
	out.ServerNow = s.now()
	return out, nil
}

func (s *Service) salesPage(ctx context.Context, sc authz.Scope, w salesWhere, beforeAt *time.Time, beforeID pgtype.UUID, lim int) ([]posdb.StoreSalesRow, error) {
	rows, err := s.q(ctx).StoreSales(ctx, posdb.StoreSalesParams{
		StoreID: ids.UUID(sc.StoreID), FromAt: w.fromAt, ToAt: w.toAt, Payment: w.payment, Status: w.status,
		Link: w.link, Offline: w.offline, Cashier: w.cashier,
		BeforeAt: beforeAt, BeforeID: beforeID, Lim: int32(lim), //nolint:gosec // at most 501
	})
	if err != nil {
		return nil, asProblem(fmt.Errorf("pos: store sales: %w", err))
	}
	return rows, nil
}

// SalesSummary is the Counter sales stats for the filters (GET
// {S}/pos/sales-summary, D-268): completed sales as sales, reversed apart,
// split by payment method and by cashier.
func (s *Service) SalesSummary(ctx context.Context, sc authz.Scope, f SalesFilter) (posapi.PosSalesSummary, error) {
	if err := s.ready(ctx); err != nil {
		return posapi.PosSalesSummary{}, err
	}
	w, err := f.where()
	if err != nil {
		return posapi.PosSalesSummary{}, err
	}
	q := s.q(ctx)
	store := ids.UUID(sc.StoreID)
	t, err := q.StoreSalesTotals(ctx, posdb.StoreSalesTotalsParams{
		StoreID: store, FromAt: w.fromAt, ToAt: w.toAt, Payment: w.payment, Status: w.status, Link: w.link, Offline: w.offline, Cashier: w.cashier,
	})
	if err != nil {
		return posapi.PosSalesSummary{}, asProblem(fmt.Errorf("pos: sales totals: %w", err))
	}
	pay, err := q.StoreSalesByPayment(ctx, posdb.StoreSalesByPaymentParams{
		StoreID: store, FromAt: w.fromAt, ToAt: w.toAt, Payment: w.payment, Status: w.status, Link: w.link, Offline: w.offline, Cashier: w.cashier,
	})
	if err != nil {
		return posapi.PosSalesSummary{}, asProblem(fmt.Errorf("pos: sales by payment: %w", err))
	}
	people, err := q.StoreSalesByCashier(ctx, posdb.StoreSalesByCashierParams{
		StoreID: store, FromAt: w.fromAt, ToAt: w.toAt, Payment: w.payment, Status: w.status, Link: w.link, Offline: w.offline, Cashier: w.cashier,
	})
	if err != nil {
		return posapi.PosSalesSummary{}, asProblem(fmt.Errorf("pos: sales by cashier: %w", err))
	}
	out := posapi.PosSalesSummary{
		From: f.From, To: f.To,
		SalesPaise: int(t.SalesPaise), BillCount: int(t.BillCount), UnitCount: int(t.UnitCount),
		ReversedPaise: int(t.ReversedPaise), ReversedCount: int(t.ReversedCount),
		CreditsUsed: int(t.CreditsUsed), CashbackCredits: int(t.CashbackCredits),
		OfflineCount: int(t.OfflineCount), LinkedCount: int(t.LinkedCount),
		ByPayment: make([]posapi.PosSalesPaymentTotal, len(pay)),
		ByCashier: make([]posapi.PosSalesCashierTotal, len(people)),
		ServerNow: s.now(),
	}
	if t.BillCount > 0 {
		out.AverageBillPaise = int(t.SalesPaise / int64(t.BillCount))
	}
	for i, p := range pay {
		out.ByPayment[i] = posapi.PosSalesPaymentTotal{Method: posapi.PosPaymentMethod(p.PaymentMethod), BillCount: int(p.BillCount), TotalPaise: int(p.TotalPaise)}
	}
	for i, p := range people {
		out.ByCashier[i] = posapi.PosSalesCashierTotal{Cashier: cashierView(p.CashierUserID, p.CashierName), BillCount: int(p.BillCount), TotalPaise: int(p.TotalPaise)}
	}
	return out, nil
}

// SaleDetail is one sale of the store with its lines, cashier, device and
// reversal (GET {S}/pos/sales/{saleId}, D-268). No phone or receipt token;
// another store's sale is 404.
func (s *Service) SaleDetail(ctx context.Context, sc authz.Scope, saleID ids.ID) (posapi.PosSaleDetail, error) {
	if err := s.ready(ctx); err != nil {
		return posapi.PosSaleDetail{}, err
	}
	q := s.q(ctx)
	row, err := q.GetSale(ctx, posdb.GetSaleParams{ID: ids.UUID(saleID), StoreID: ids.UUID(sc.StoreID)})
	if isNoRows(err) {
		return posapi.PosSaleDetail{}, notFound()
	}
	if err != nil {
		return posapi.PosSaleDetail{}, asProblem(fmt.Errorf("pos: sale: %w", err))
	}
	lines, err := q.SaleLines(ctx, row.ID)
	if err != nil {
		return posapi.PosSaleDetail{}, asProblem(fmt.Errorf("pos: sale lines: %w", err))
	}
	base, err := s.saleView(ctx, row, lines, nil)
	if err != nil {
		return posapi.PosSaleDetail{}, err
	}
	person := func(u pgtype.UUID) (posapi.PosCashier, error) {
		name, err := q.UserName(ctx, u)
		if err != nil && !isNoRows(err) {
			return posapi.PosCashier{}, asProblem(fmt.Errorf("pos: staff name: %w", err))
		}
		return cashierView(u, name), nil
	}
	cashier, err := person(row.CashierUserID)
	if err != nil {
		return posapi.PosSaleDetail{}, err
	}
	out := posapi.PosSaleDetail{
		Id: base.Id, ClientId: base.ClientId, Lines: base.Lines,
		ItemSubtotalPaise: base.ItemSubtotalPaise, BillOfferDiscountPaise: base.BillOfferDiscountPaise,
		SubtotalPaise: base.SubtotalPaise, DiscountPaise: base.DiscountPaise, RoundOffPaise: base.RoundOffPaise,
		CreditsUsed: base.CreditsUsed, CashbackCredits: base.CashbackCredits, TotalPaise: base.TotalPaise,
		PaymentMethod: base.PaymentMethod, Link: base.Link, Status: base.Status, Offline: row.DeviceSeq != nil,
		Cashier: cashier, DeviceId: mustID(row.DeviceID).String(), SoldAt: row.SoldAt, ReceivedAt: row.ReceivedAt,
		ReversedAt: row.ReversedAt, ReversalReason: row.ReversalReason, Version: base.Version, ServerNow: base.ServerNow,
	}
	if row.ReversedBy.Valid {
		by, err := person(row.ReversedBy)
		if err != nil {
			return posapi.PosSaleDetail{}, err
		}
		out.ReversedBy = &by
	}
	if row.Status == "completed" {
		window, err := s.duration(ctx, settings.PosReversalWindowHours)
		if err != nil {
			return posapi.PosSaleDetail{}, asProblem(err)
		}
		// As ReverseSale: an offline sale's window runs from when it reached
		// the server (D-131).
		from := row.SoldAt
		if row.DeviceSeq != nil {
			from = row.ReceivedAt
		}
		out.ReversibleUntil = ptr(from.Add(window))
	}
	return out, nil
}

// ExportSales is every sale the filters match as CSV, newest first, times in
// IST (GET {S}/pos/sales-export, D-268). Text cells are formula-safe (D-93);
// no phone, code or receipt token is read.
func (s *Service) ExportSales(ctx context.Context, sc authz.Scope, f SalesFilter) ([]byte, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	w, err := f.where()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	c := csv.NewWriter(&b)
	_ = c.Write([]string{
		"sale_id", "sold_at_ist", "received_at_ist", "status", "payment_method", "lines", "units", "discount_inr",
		"credits_used", "cashback_credits", "total_inr", "offline", "link_method", "link_status", "customer_name",
		"cashier", "reversed_at_ist", "reversal_reason",
	}) // a bytes.Buffer never fails
	var beforeAt *time.Time
	var beforeID pgtype.UUID
	for {
		rows, err := s.salesPage(ctx, sc, w, beforeAt, beforeID, exportBatch)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			reversedAt := ""
			if r.ReversedAt != nil {
				reversedAt = istStamp(*r.ReversedAt)
			}
			_ = c.Write([]string{
				mustID(r.ID).String(), istStamp(r.SoldAt), istStamp(r.ReceivedAt), r.Status, r.PaymentMethod,
				strconv.Itoa(int(r.LineCount)), strconv.Itoa(int(r.UnitCount)), inr(r.DiscountPaise + r.BillOfferDiscountPaise),
				strconv.Itoa(int(r.CreditsUsed)), strconv.Itoa(int(r.CashbackCredits)), inr(r.TotalPaise),
				strconv.FormatBool(r.DeviceSeq != nil), deref(r.LinkMethod), deref(r.LinkStatus), safeCell(r.CustomerName),
				safeCell(r.CashierName), reversedAt, safeCell(deref(r.ReversalReason)),
			})
		}
		if len(rows) < exportBatch {
			break
		}
		last := rows[len(rows)-1]
		beforeAt, beforeID = &last.SoldAt, last.ID
	}
	c.Flush()
	return b.Bytes(), nil
}

func cashierView(u pgtype.UUID, name string) posapi.PosCashier {
	return posapi.PosCashier{Id: mustID(u).String(), Name: optString(name)}
}

// istStamp is a time as the export writes it: "2026-10-24 10:30" in IST.
func istStamp(t time.Time) string { return t.In(clock.IST).Format("2006-01-02 15:04") }

// inr formats non-negative paise as rupees with two decimals ("300.00").
func inr(p int64) string { return fmt.Sprintf("%d.%02d", p/100, p%100) }

// safeCell stops a spreadsheet reading a text cell as a formula (D-93): one
// starting with =, +, -, @, a tab or a carriage return gets a leading
// apostrophe.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

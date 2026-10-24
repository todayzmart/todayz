package pos

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	receiptsapi "github.com/todayz/backend/gen/api/receipts"
	"github.com/todayz/backend/gen/sqlc/posdb"
	"github.com/todayz/backend/internal/platform/ids"
)

// The purchases list's page sizes (common/parameters.yaml Limit).
const (
	defaultLimit = 20  //settings:ok the contract's default page size, not a product number
	maxLimit     = 100 //settings:ok the contract's maximum page size, not a product number
)

// Receipt is a POS receipt by its link's token (GET /v1/public/receipts/{token},
// D-45): items, totals and the store only. An unknown, malformed or expired
// token is the same 404 after the same one lookup.
func (s *Service) Receipt(ctx context.Context, token string) (receiptsapi.Receipt, error) {
	if err := s.ready(ctx); err != nil {
		return receiptsapi.Receipt{}, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != receiptBytes {
		// Hash something anyway so a malformed token costs the same lookup.
		raw = make([]byte, receiptBytes)
	}
	h := sha256.Sum256(raw)
	q := s.q(ctx)
	r, err := q.ReceiptByHash(ctx, posdb.ReceiptByHashParams{TokenHash: h[:], Now: s.now()})
	if err != nil || len(token) != receiptChars {
		if err != nil && !isNoRows(err) {
			return receiptsapi.Receipt{}, asProblem(fmt.Errorf("pos: receipt: %w", err))
		}
		return receiptsapi.Receipt{}, notFound()
	}
	lines, err := q.SaleLines(ctx, r.ID)
	if err != nil {
		return receiptsapi.Receipt{}, asProblem(fmt.Errorf("pos: receipt lines: %w", err))
	}
	out := receiptsapi.Receipt{
		Store: receiptsapi.ReceiptStore{Name: r.StoreName, AddressLine: r.AddressLine1, Locality: r.Locality},
		Lines: make([]receiptsapi.ReceiptLine, len(lines)),
		// Line totals are after their offers; the bill's offers and the
		// cashier's discounts are the receipt's discount.
		ItemSubtotalPaise: int(r.ItemSubtotalPaise),
		DiscountPaise:     int(r.BillOfferDiscountPaise + r.DiscountPaise),
		CreditsUsed:       int(r.CreditsUsed),
		RoundOffPaise:     int(r.RoundOffPaise),
		TotalPaise:        int(r.TotalPaise),
		PaymentMethod:     receiptsapi.ReceiptPaymentMethod(r.PaymentMethod),
		Status:            receiptsapi.ReceiptStatus(r.Status),
		SoldAt:            r.SoldAt,
	}
	for i, l := range lines {
		out.Lines[i] = receiptsapi.ReceiptLine{
			Name: l.NameSnapshot, UnitPricePaise: int(l.UnitPricePaise), Quantity: int(l.Quantity),
			DiscountPaise: int(l.OfferDiscountPaise), LineTotalPaise: int(l.LineTotalPaise),
		}
	}
	return out, nil
}

// Purchases are the signed-in customer's orders and linked POS sales,
// newest first (GET /v1/me/purchases, D-75). A phone_unverified sale never
// appears (the query keeps bill_qr and counter_code links only).
func (s *Service) Purchases(ctx context.Context, limit *int, cursor *string) (receiptsapi.PurchaseList, error) {
	if err := s.ready(ctx); err != nil {
		return receiptsapi.PurchaseList{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return receiptsapi.PurchaseList{}, err
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
			return receiptsapi.PurchaseList{}, fieldErr("query.cursor", "not a cursor from this list")
		}
		beforeAt, beforeID = &at, ids.UUID(id)
	}
	rows, err := s.q(ctx).MyPurchases(ctx, posdb.MyPurchasesParams{
		CustomerID: ids.UUID(p.UserID), BeforeAt: beforeAt, BeforeID: beforeID, Lim: int32(lim + 1), //nolint:gosec // at most 101
	})
	if err != nil {
		return receiptsapi.PurchaseList{}, asProblem(fmt.Errorf("pos: purchases: %w", err))
	}
	out := receiptsapi.PurchaseList{Items: make([]receiptsapi.Purchase, 0, min(len(rows), lim))}
	if len(rows) > lim {
		last := rows[lim-1]
		out.NextCursor = ptr(encodeCursor(last.At, mustID(last.ID)))
		rows = rows[:lim]
	}
	for _, r := range rows {
		out.Items = append(out.Items, receiptsapi.Purchase{
			Kind: receiptsapi.PurchaseKind(r.Kind), Id: mustID(r.ID).String(),
			Store:     receiptsapi.PurchaseStore{Id: mustID(r.StoreID).String(), Name: r.StoreName},
			ItemCount: int(r.ItemCount), TotalPaise: int(r.TotalPaise), Status: r.Status, At: r.At,
		})
	}
	out.ServerNow = s.now()
	return out, nil
}

// encodeCursor is the opaque cursor after (at, id).
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
		return time.Time{}, ids.Zero, errors.New("pos: cursor has no id")
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

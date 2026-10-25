package pos

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	posapi "github.com/todayz/backend/gen/api/pos"
	"github.com/todayz/backend/gen/sqlc/posdb"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/ids"
)

// Sales are the store's sales, online and synced, newest first by sold_at
// (GET {S}/pos/sales, B-05 sales history, W2-16), with each link's status
// and, for a failed offline counter code, why. The customer is named only
// for a bill_qr or counter_code link; no phone or receipt token is read.
func (s *Service) Sales(ctx context.Context, sc authz.Scope, limit *int, cursor *string) (posapi.PosSaleList, error) {
	if err := s.ready(ctx); err != nil {
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
	rows, err := s.q(ctx).StoreSales(ctx, posdb.StoreSalesParams{
		StoreID: ids.UUID(sc.StoreID), BeforeAt: beforeAt, BeforeID: beforeID, Lim: int32(lim + 1), //nolint:gosec // at most 101
	})
	if err != nil {
		return posapi.PosSaleList{}, asProblem(fmt.Errorf("pos: store sales: %w", err))
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
			Offline: r.DeviceSeq != nil,
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

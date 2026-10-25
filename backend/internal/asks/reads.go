package asks

// The customer's side (C-26): their asks, one ask with its Yes replies, and
// closing it. Progress and close also go out as ask.updated on /v1/me/stream.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	asksapi "github.com/todayz/backend/gen/api/asks"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/asksdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/realtime"
)

// Close reasons (asks.close_reason).
const (
	ReasonAllReplied     = "all_replied"
	ReasonWindowEnded    = "window_ended"
	ReasonCustomerClosed = "customer_closed"
)

type askRow struct {
	id                                pgtype.UUID
	productID                         pgtype.UUID
	query                             string
	quantity                          int32
	needBy, status, closeReason       *string
	recipients, replied, yes, version int32
	createdAt, windowEnds             time.Time
	closedAt                          *time.Time
	productName, productVariant       *string
	bestPrice                         int64
}

func toAsk(r askRow) asksapi.Ask {
	a := asksapi.Ask{
		AskId: idOf(r.id).String(), ItemName: itemName(r.productName, r.productVariant, r.query),
		ProductId: ulidPtr(r.productID), Quantity: int(r.quantity), Status: asksapi.AskStatus(*r.status),
		Progress: asksapi.AskProgress{Asked: int(r.recipients), Replied: int(r.replied)},
		YesCount: int(r.yes), CreatedAt: r.createdAt.UTC(), ClosedAt: utcPtr(r.closedAt),
		WindowEndsAt: ptr(r.windowEnds.UTC()), Version: int(r.version),
	}
	if r.bestPrice > 0 {
		a.Progress.BestPricePaise = ptr(int(r.bestPrice))
	}
	if r.needBy != nil {
		a.NeedBy = ptr(asksapi.AskNeedBy(*r.needBy))
	}
	if r.closeReason != nil {
		a.CloseReason = ptr(asksapi.AskCloseReason(*r.closeReason))
	}
	return a
}

func fromGet(r asksdb.GetMyAskRow) askRow {
	return askRow{
		id: r.ID, productID: r.ProductID, query: r.QueryNorm, quantity: r.Quantity, needBy: r.NeedBy,
		status: &r.Status, closeReason: r.CloseReason, recipients: r.RecipientsCount, replied: r.Replied,
		yes: r.YesCount, version: r.Version, createdAt: r.CreatedAt, windowEnds: r.WindowEndsAt,
		closedAt: r.ClosedAt, productName: r.ProductName, productVariant: r.ProductVariant, bestPrice: r.BestPricePaise,
	}
}

func fromList(r asksdb.ListMyAsksRow) askRow {
	return askRow{
		id: r.ID, productID: r.ProductID, query: r.QueryNorm, quantity: r.Quantity, needBy: r.NeedBy,
		status: &r.Status, closeReason: r.CloseReason, recipients: r.RecipientsCount, replied: r.Replied,
		yes: r.YesCount, version: r.Version, createdAt: r.CreatedAt, windowEnds: r.WindowEndsAt,
		closedAt: r.ClosedAt, productName: r.ProductName, productVariant: r.ProductVariant, bestPrice: r.BestPricePaise,
	}
}

// detail is one of the customer's asks with its Yes replies; another
// customer's ask is 404.
func (s *Service) detail(ctx context.Context, q *asksdb.Queries, askID, customerID ids.ID) (asksapi.AskDetail, error) {
	row, err := q.GetMyAsk(ctx, asksdb.GetMyAskParams{ID: ids.UUID(askID), CustomerID: ids.UUID(customerID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return asksapi.AskDetail{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return asksapi.AskDetail{}, err
	}
	reps, err := q.YesReplies(ctx, ids.UUID(askID))
	if err != nil {
		return asksapi.AskDetail{}, err
	}
	out := asksapi.AskDetail{Ask: toAsk(fromGet(row)), Replies: make([]asksapi.AskReply, 0, len(reps)), ServerNow: s.now()}
	for _, r := range reps {
		out.Replies = append(out.Replies, toReply(r))
	}
	return out, nil
}

func toReply(r asksdb.YesRepliesRow) asksapi.AskReply {
	inStore := !r.AcceptsPickup && !r.AcceptsDelivery
	rep := asksapi.AskReply{
		AskReplyId: idOf(r.ID).String(), StoreId: idOf(r.StoreID).String(), StoreSlug: r.Slug, StoreName: r.Name, DistanceM: int(max(r.DistanceM, 0)),
		PricePaise: intPtr(r.PricePaise), ReadyBy: utcPtr(r.ReadyBy), PriceValidUntil: utcPtr(r.PriceValidUntil),
		StoreItemId: ulidPtr(r.StoreItemID), InStoreOnly: inStore, OrderId: ulidPtr(r.OrderID),
		Bookable: r.StoreBookable && !inStore && r.StoreItemID.Valid,
	}
	if r.RepliedAt != nil {
		rep.RepliedAt = r.RepliedAt.UTC()
	}
	if r.StoreItemID.Valid && r.ListedPricePaise > 0 {
		rep.ListedPricePaise = ptr(int(r.ListedPricePaise))
	}
	return rep
}

func (s *Service) getMine(ctx context.Context, askID string) (asksapi.AskDetail, error) {
	p, err := principal(ctx)
	if err != nil {
		return asksapi.AskDetail{}, err
	}
	id, err := pathID(askID)
	if err != nil {
		return asksapi.AskDetail{}, err
	}
	if err := s.ready(ctx); err != nil {
		return asksapi.AskDetail{}, err
	}
	return s.detail(ctx, s.q(ctx), id, p.UserID)
}

func (s *Service) listMine(ctx context.Context, params asksapi.ListMyAsksParams) (asksapi.AskList, error) {
	p, err := principal(ctx)
	if err != nil {
		return asksapi.AskList{}, err
	}
	if err := s.ready(ctx); err != nil {
		return asksapi.AskList{}, err
	}
	lim, at, before, err := pageArgs(params.Limit, params.Cursor)
	if err != nil {
		return asksapi.AskList{}, err
	}
	var status *string
	if params.Status != nil {
		status = ptr(string(*params.Status))
	}
	rows, err := s.q(ctx).ListMyAsks(ctx, asksdb.ListMyAsksParams{
		CustomerID: ids.UUID(p.UserID), Status: status, BeforeAt: at, BeforeID: before, Lim: i32(int64(lim + 1)),
	})
	if err != nil {
		return asksapi.AskList{}, err
	}
	out := asksapi.AskList{Asks: make([]asksapi.Ask, 0, min(len(rows), lim))}
	for i, r := range rows {
		if i == lim {
			last := rows[lim-1]
			out.NextCursor = ptr(encodeCursor(last.CreatedAt, idOf(last.ID)))
			break
		}
		out.Asks = append(out.Asks, toAsk(fromList(r)))
	}
	return out, nil
}

func (s *Service) closeMine(ctx context.Context, askID string) (asksapi.AskDetail, error) {
	p, err := principal(ctx)
	if err != nil {
		return asksapi.AskDetail{}, err
	}
	id, err := pathID(askID)
	if err != nil {
		return asksapi.AskDetail{}, err
	}
	if err := s.ready(ctx); err != nil {
		return asksapi.AskDetail{}, err
	}
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		own, err := q.AskOwner(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && idOf(own.CustomerID) != p.UserID) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return err
		}
		closed, err := s.closeAsk(ctx, q, id, ReasonCustomerClosed)
		if err != nil {
			return err
		}
		if !closed {
			return problem.New(genproblems.InvalidTransition, "")
		}
		return nil
	})
	if err != nil {
		return asksapi.AskDetail{}, err
	}
	return s.detail(ctx, s.q(ctx), id, p.UserID)
}

// closeAsk closes an open ask in the caller's transaction: the customer's
// stream gets ask.updated, every recipient store ask.closed. It reports false
// when the ask was already closed.
func (s *Service) closeAsk(ctx context.Context, q *asksdb.Queries, askID ids.ID, reason string) (bool, error) {
	now := s.now()
	row, err := q.CloseAsk(ctx, asksdb.CloseAskParams{CloseReason: &reason, Now: &now, ID: ids.UUID(askID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	tx, ok := db.TxFrom(ctx)
	if !ok {
		return false, errors.New("asks: close outside a transaction")
	}
	if err := realtime.Publish(ctx, tx, realtime.UserChannel(idOf(row.CustomerID)),
		realtime.Event{Type: realtime.AskUpdated, ID: askID, Version: int64(row.Version)}); err != nil {
		return false, err
	}
	stores, err := q.AskRecipientStores(ctx, ids.UUID(askID))
	if err != nil {
		return false, err
	}
	for _, st := range stores {
		if err := realtime.Publish(ctx, tx, realtime.StoreChannel(idOf(st)),
			realtime.Event{Type: realtime.AskClosed, ID: askID, Version: int64(row.Version)}); err != nil {
			return false, err
		}
	}
	yes, err := q.AskYesCount(ctx, ids.UUID(askID))
	if err != nil {
		return false, fmt.Errorf("asks: yes count: %w", err)
	}
	s.event(ctx, q, pgtype.UUID{}, "ask_closed", map[string]any{
		"reason": reason, "yes_count": int(yes), "recipients": int(row.RecipientsCount),
	})
	return true, nil
}

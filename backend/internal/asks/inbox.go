package asks

// The shop's side (B-20): the Asks inbox, the Yes/No reply and "Report this
// ask". Nothing here selects or returns a customer field; the distance is a
// band.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	asksapi "github.com/todayz/backend/gen/api/asks"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/asksdb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/realtime"
	"github.com/todayz/backend/internal/platform/settings"
)

// reportScore is the risk score of an ask_reported flag. A flag never acts by
// itself (D-225); the console sorts by it.
const reportScore = 40 //settings:ok D-225 fixed score

type storeRow struct {
	replyID, askID, productID, storeItemID pgtype.UUID
	band, query, askStatus                 string
	replyBy, sentAt                        time.Time
	answer, needBy                         *string
	price, listed, itemMrp, productMrp     *int64
	readyBy, repliedAt, reportedAt         *time.Time
	quantity, available                    int32
	productName, productVariant            *string
}

func toStoreAsk(r storeRow, now time.Time) asksapi.StoreAsk {
	a := asksapi.StoreAsk{
		AskId: idOf(r.askID).String(), ItemName: itemName(r.productName, r.productVariant, r.query),
		ProductId: ulidPtr(r.productID), StoreItemId: ulidPtr(r.storeItemID), Quantity: int(r.quantity),
		DistanceBand: asksapi.DistanceBand(r.band), SentAt: r.sentAt.UTC(), ReplyBy: r.replyBy.UTC(),
		PricePaise: intPtr(r.price), ReadyBy: utcPtr(r.readyBy), RepliedAt: utcPtr(r.repliedAt),
		Reported: r.reportedAt != nil, ListedPricePaise: intPtr(r.listed),
	}
	switch {
	case r.answer != nil:
		a.Status = "replied"
		a.Answer = ptr(asksapi.StoreAskAnswer(*r.answer))
	case !now.Before(r.replyBy) || r.askStatus == "closed":
		a.Status = "missed"
	default:
		a.Status = "open"
	}
	if r.needBy != nil {
		a.NeedBy = ptr(asksapi.StoreAskNeedBy(*r.needBy))
	}
	if r.storeItemID.Valid {
		a.Available = ptr(int(r.available))
	}
	if r.itemMrp != nil {
		a.MrpPaise = intPtr(r.itemMrp)
	} else {
		a.MrpPaise = intPtr(r.productMrp)
	}
	return a
}

func fromStoreList(r asksdb.ListStoreAsksRow) storeRow {
	return storeRow{
		replyID: r.ReplyID, askID: r.AskID, productID: r.ProductID, storeItemID: r.StoreItemID, band: r.DistanceBand,
		query: r.QueryNorm, askStatus: r.AskStatus, replyBy: r.ReplyBy, sentAt: r.SentAt, answer: r.Answer,
		needBy: r.NeedBy, price: r.PricePaise, listed: r.ListedPricePaise, itemMrp: r.ItemMrpPaise,
		productMrp: r.ProductMrpPaise, readyBy: r.ReadyBy, repliedAt: r.RepliedAt, reportedAt: r.ReportedAt,
		quantity: r.Quantity, available: r.Available, productName: r.ProductName, productVariant: r.ProductVariant,
	}
}

func (s *Service) listStore(ctx context.Context, sid string, params asksapi.ListStoreAsksParams) (asksapi.StoreAskList, error) {
	storeID, err := pathID(sid)
	if err != nil {
		return asksapi.StoreAskList{}, err
	}
	if err := s.ready(ctx); err != nil {
		return asksapi.StoreAskList{}, err
	}
	lim, at, before, err := pageArgs(params.Limit, params.Cursor)
	if err != nil {
		return asksapi.StoreAskList{}, err
	}
	view := "open"
	if params.Status != nil {
		view = string(*params.Status)
	}
	now := s.now()
	q := s.q(ctx)
	rows, err := q.ListStoreAsks(ctx, asksdb.ListStoreAsksParams{
		StoreID: ids.UUID(storeID), View: view, Now: now, BeforeAt: at, BeforeID: before, Lim: i32(int64(lim + 1)),
	})
	if err != nil {
		return asksapi.StoreAskList{}, err
	}
	open, err := q.CountOpenStoreAsks(ctx, asksdb.CountOpenStoreAsksParams{StoreID: ids.UUID(storeID), Now: now})
	if err != nil {
		return asksapi.StoreAskList{}, err
	}
	out := asksapi.StoreAskList{Asks: make([]asksapi.StoreAsk, 0, min(len(rows), lim)), OpenCount: int(open), ServerNow: now}
	for i, r := range rows {
		if i == lim {
			last := rows[lim-1]
			out.NextCursor = ptr(encodeCursor(last.SentAt, idOf(last.ReplyID)))
			break
		}
		out.Asks = append(out.Asks, toStoreAsk(fromStoreList(r), now))
	}
	return out, nil
}

// ReplyPriceRule is what decides a Yes's price (01 §11.2): the listed price
// is the default; only a role with item.price.edit changes it or prices an
// unlisted item. It may be above the MRP (D-245).
type ReplyPriceRule struct {
	Listed      bool
	ListedPaise int64
	MrpPaise    *int64
	CanEdit     bool
}

// Decide returns the reply's price (nil: a Yes with no price) or the problem.
func (r ReplyPriceRule) Decide(sent *int) (*int64, error) {
	var price *int64
	switch {
	case sent == nil && r.Listed:
		price = ptr(r.ListedPaise)
	case sent == nil:
		return nil, nil
	case r.Listed && int64(*sent) == r.ListedPaise:
		price = ptr(r.ListedPaise)
	case !r.CanEdit:
		return nil, problem.New(genproblems.Forbidden, "").WithField("body.pricePaise", "your role cannot change the price")
	default:
		price = ptr(int64(*sent))
	}
	return price, nil
}

// ReadyByOK checks "ready by" (01 §11.2): from now to the end of today IST, or
// of tomorrow when the ask is for tomorrow. A minute of clock skew is allowed.
func ReadyByOK(readyBy, now time.Time, needBy *string) bool {
	end := nextISTMidnight(now)
	if needBy != nil && *needBy == "tomorrow" {
		end = nextISTMidnight(end)
	}
	return !readyBy.Before(now.Add(-time.Minute)) && readyBy.Before(end)
}

func (s *Service) reply(ctx context.Context, sid, askID string, body *asksapi.AskReplyCreate) (asksapi.StoreAsk, error) {
	p, err := principal(ctx)
	if err != nil {
		return asksapi.StoreAsk{}, err
	}
	storeID, err := pathID(sid)
	if err != nil {
		return asksapi.StoreAsk{}, err
	}
	aid, err := pathID(askID)
	if err != nil {
		return asksapi.StoreAsk{}, err
	}
	if err := s.ready(ctx); err != nil {
		return asksapi.StoreAsk{}, err
	}
	if body == nil {
		return asksapi.StoreAsk{}, fieldErr("body", "required")
	}
	yes := body.Answer == asksapi.AskReplyCreateAnswerYes
	if !yes && (body.PricePaise != nil || body.ReadyBy != nil) {
		return asksapi.StoreAsk{}, fieldErr("body.pricePaise", "only with a Yes")
	}
	dec, _ := authz.DecisionFrom(ctx)
	validFor, err := s.settings.Duration(ctx, settings.AskPriceValidMin)
	if err != nil {
		return asksapi.StoreAsk{}, fmt.Errorf("asks: %s: %w", settings.AskPriceValidMin, err)
	}
	now := s.now()
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		rec, err := q.LockRecipient(ctx, asksdb.LockRecipientParams{AskID: ids.UUID(aid), StoreID: ids.UUID(storeID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return err
		}
		if rec.Answer != nil {
			return problem.New(genproblems.InvalidTransition, "")
		}
		if rec.AskStatus != "open" || !now.Before(rec.ReplyBy) {
			return problem.New(genproblems.AskClosed, "")
		}
		var item *asksdb.StoreItemPriceRow
		if rec.ProductID.Valid {
			it, err := q.StoreItemPrice(ctx, asksdb.StoreItemPriceParams{StoreID: ids.UUID(storeID), ProductID: rec.ProductID})
			if err == nil {
				item = &it
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		listed := item != nil && item.IsListed
		params := asksdb.ReplyRecipientParams{Answer: ptr(string(body.Answer)), Now: &now, RepliedBy: ids.UUID(p.UserID), ID: rec.ID}
		if yes {
			rule := ReplyPriceRule{Listed: listed, MrpPaise: rec.ProductMrpPaise}
			if item != nil {
				rule.ListedPaise = item.PricePaise
				if item.MrpPaise != nil {
					rule.MrpPaise = item.MrpPaise
				}
				params.StoreItemID = item.ID
			}
			if body.PricePaise != nil && (!listed || int64(*body.PricePaise) != rule.ListedPaise) {
				rule.CanEdit = s.canEditPrice(ctx, p, dec.Scope)
			}
			if params.PricePaise, err = rule.Decide(body.PricePaise); err != nil {
				return err
			}
			if params.PricePaise != nil {
				params.PriceValidUntil = ptr(now.Add(validFor))
			}
			if body.ReadyBy != nil {
				if !ReadyByOK(*body.ReadyBy, now, rec.NeedBy) {
					return fieldErr("body.readyBy", "must be today, or by the end of tomorrow when the ask is for tomorrow")
				}
				params.ReadyBy = ptr(body.ReadyBy.UTC())
			}
		}
		r, err := q.ReplyRecipient(ctx, params)
		if errors.Is(err, pgx.ErrNoRows) {
			return problem.New(genproblems.InvalidTransition, "")
		}
		if err != nil {
			return err
		}
		if yes && listed {
			if err := q.ConfirmItemStock(ctx, asksdb.ConfirmItemStockParams{Now: now, ID: item.ID, StoreID: ids.UUID(storeID)}); err != nil {
				return err
			}
		}
		version, err := q.BumpAsk(ctx, rec.AskID)
		if err != nil {
			return err
		}
		tx, _ := db.TxFrom(ctx)
		customer := idOf(rec.CustomerID)
		if err := realtime.Publish(ctx, tx, realtime.UserChannel(customer),
			realtime.Event{Type: realtime.AskUpdated, ID: aid, Version: int64(version)}); err != nil {
			return err
		}
		if yes && s.notices != nil {
			// The one push for an ask is at the first Yes (01 §11.2, §15): the
			// conditional update claims it, so two Yeses at once send one.
			n, err := q.MarkFirstYesNotified(ctx, asksdb.MarkFirstYesNotifiedParams{ID: rec.AskID, Now: &now})
			if err != nil {
				return err
			}
			if n == 1 {
				st, err := q.StoreForAsk(ctx, ids.UUID(storeID))
				if err != nil {
					return err
				}
				if _, err := s.notices.Send(ctx, tx, notice.Notification{
					Kind: notice.KindAskReply, UserID: customer,
					Params: map[string]string{
						"store": clip(st.Name, maxParamRunes), "product": clip(itemName(rec.ProductName, nil, rec.QueryNorm), maxParamRunes),
					},
					Ref:     &notice.Ref{Type: "ask", ID: aid},
					Outside: []notice.Outside{{Channel: notice.Push}},
				}); err != nil {
					return fmt.Errorf("asks: ask reply notice: %w", err)
				}
			}
		}
		left, err := q.UnansweredOpen(ctx, asksdb.UnansweredOpenParams{AskID: rec.AskID, Now: now})
		if err != nil {
			return err
		}
		if left == 0 {
			if err := s.closeEnded(ctx, q, aid); err != nil {
				return err
			}
		}
		s.event(ctx, q, pgtype.UUID{}, "ask_replied", map[string]any{
			"answer": string(body.Answer), "seconds_to_reply": int(max(now.Sub(rec.SentAt), 0) / time.Second),
			"priced": r.PricePaise != nil, "listed": listed, "by_role": string(dec.Role),
		})
		return nil
	})
	if err != nil {
		return asksapi.StoreAsk{}, err
	}
	return s.storeAsk(ctx, storeID, aid)
}

// storeAsk is the shop's view of one ask, the same shape the inbox lists.
func (s *Service) storeAsk(ctx context.Context, storeID, askID ids.ID) (asksapi.StoreAsk, error) {
	now := s.now()
	rows, err := s.q(ctx).ListStoreAsks(ctx, asksdb.ListStoreAsksParams{
		StoreID: ids.UUID(storeID), AskID: ids.UUID(askID), View: "all", Now: now, Lim: 1,
	})
	if err != nil {
		return asksapi.StoreAsk{}, err
	}
	if len(rows) == 0 {
		return asksapi.StoreAsk{}, problem.New(genproblems.NotFound, "")
	}
	return toStoreAsk(fromStoreList(rows[0]), now), nil
}

// closeEnded closes an ask nobody can still answer: all_replied when every
// store answered, else window_ended.
func (s *Service) closeEnded(ctx context.Context, q *asksdb.Queries, askID ids.ID) error {
	all, err := q.AskAllReplied(ctx, ids.UUID(askID))
	if err != nil {
		return err
	}
	reason := ReasonWindowEnded
	if all != nil && *all {
		reason = ReasonAllReplied
	}
	_, err = s.closeAsk(ctx, q, askID, reason)
	return err
}

// canEditPrice is the item.price.edit check in the store's scope.
func (s *Service) canEditPrice(ctx context.Context, p authn.Principal, sc authz.Scope) bool {
	if s.az == nil {
		return false
	}
	_, err := s.az.Check(ctx, p, policy.ItemPriceEdit, sc)
	return err == nil
}

func (s *Service) report(ctx context.Context, sid, askID string, body *asksapi.AskReportCreate) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	storeID, err := pathID(sid)
	if err != nil {
		return err
	}
	aid, err := pathID(askID)
	if err != nil {
		return err
	}
	if err := s.ready(ctx); err != nil {
		return err
	}
	if body == nil {
		return fieldErr("body", "required")
	}
	now := s.now()
	return s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		rec, err := q.LockRecipient(ctx, asksdb.LockRecipientParams{AskID: ids.UUID(aid), StoreID: ids.UUID(storeID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return err
		}
		if _, err := q.ReportRecipient(ctx, asksdb.ReportRecipientParams{Now: &now, ReportedBy: ids.UUID(p.UserID), ID: rec.ID}); errors.Is(err, pgx.ErrNoRows) {
			return problem.New(genproblems.InvalidTransition, "")
		} else if err != nil {
			return err
		}
		if err := q.UpsertBlock(ctx, asksdb.UpsertBlockParams{StoreID: ids.UUID(storeID), CustomerID: rec.CustomerID, AskID: rec.AskID, Now: now}); err != nil {
			return err
		}
		details, err := json.Marshal(map[string]any{"askId": aid.String(), "storeId": storeID.String(), "reason": string(body.Reason)})
		if err != nil {
			return err
		}
		if err := q.InsertAskReportedFlag(ctx, asksdb.InsertAskReportedFlagParams{
			ID: ids.UUID(s.ids.New(now)), CustomerID: rec.CustomerID, Score: reportScore, Details: details, Now: now,
		}); err != nil {
			return err
		}
		s.event(ctx, q, pgtype.UUID{}, "ask_reported", map[string]any{"reason": string(body.Reason)})
		return nil
	})
}

package asks

// The seams other domains read through cmd/todayz adapters: the replied price
// for orders (W2-02), unanswered demand for insights (W3-03), and the
// account-deletion hook for identity.

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/asksdb"
	"github.com/todayz/backend/internal/platform/ids"
)

// Price is a Yes reply as a booking reads it. UnitPaise 0 is a Yes with no
// price: the line books at the catalogue price. MaxQuantity is the asked
// quantity, the most units the reply line may carry.
type Price struct {
	StoreItemID ids.ID
	UnitPaise   int64
	ValidUntil  time.Time
	MaxQuantity int
}

// AskPrice reads the customer's Yes reply from the store. ok is false when
// there is no such Yes (another customer's, another store's, a No, or gone)
// or the reply has no store item yet. Validity is the caller's to check
// against now, at placement.
func (s *Service) AskPrice(ctx context.Context, customerID, storeID, replyID ids.ID, _ time.Time) (Price, bool, error) {
	if s.db == nil {
		return Price{}, false, errUnavailable
	}
	r, err := s.q(ctx).ReplyPrice(ctx, asksdb.ReplyPriceParams{
		ID: ids.UUID(replyID), StoreID: ids.UUID(storeID), CustomerID: ids.UUID(customerID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Price{}, false, nil
	}
	if err != nil {
		return Price{}, false, err
	}
	if !r.StoreItemID.Valid {
		return Price{}, false, nil
	}
	p := Price{StoreItemID: idOf(r.StoreItemID), MaxQuantity: int(r.Quantity)}
	if r.PricePaise != nil && r.PriceValidUntil != nil {
		p.UnitPaise, p.ValidUntil = *r.PricePaise, r.PriceValidUntil.UTC()
	}
	return p, true, nil
}

// DemandCount is one day of a store's unanswered asks for a product (or, with
// no product, the asked words).
type DemandCount struct {
	Day       time.Time
	ProductID ids.ID
	HasProd   bool
	QueryNorm string
	Count     int64
}

// Demand reads the unanswered asks a store received from the daily rollup,
// from and to inclusive (IST days). The rollup is per receiving store, so
// every ask counts once for each store that got it (01 §11.2).
func (s *Service) Demand(ctx context.Context, storeID ids.ID, from, to time.Time) ([]DemandCount, error) {
	if s.db == nil {
		return nil, errUnavailable
	}
	rows, err := s.q(ctx).StoreAskDemand(ctx, asksdb.StoreAskDemandParams{
		StoreID: ids.UUID(storeID), FromDay: pgDate(from), ToDay: pgDate(to),
	})
	if err != nil {
		return nil, err
	}
	out := make([]DemandCount, 0, len(rows))
	for _, r := range rows {
		c := DemandCount{Day: r.Date.Time, QueryNorm: r.QueryKey, Count: r.Asks}
		if r.ProductID.Valid {
			c.ProductID, c.HasProd = idOf(r.ProductID), true
		}
		out = append(out, c)
	}
	return out, nil
}

// OnAccountDelete deletes the customer's asks with their replies and the
// store blocks that came from them, and any block naming the customer (01
// §16, D-101). Orders booked from a reply keep their lines; the foreign key
// clears their link. It runs in identity's deletion transaction and is
// idempotent.
func (s *Service) OnAccountDelete(ctx context.Context, userID ids.ID) error {
	if s.db == nil {
		return errUnavailable
	}
	q := s.q(ctx)
	if err := q.DeleteCustomerBlocks(ctx, ids.UUID(userID)); err != nil {
		return err
	}
	_, err := q.DeleteCustomerAsks(ctx, ids.UUID(userID))
	return err
}

func pgDate(t time.Time) pgtype.Date {
	y, m, d := t.Date()
	return pgtype.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
}

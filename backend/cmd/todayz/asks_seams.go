package main

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/asks"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/ids"
)

// The asks domain meets orders (the reply price) and insights (unanswered
// demand) through interfaces each caller owns; these adapters are the only
// place that knows both sides.

// asksForOrders adapts asks.Service to orders.AskPricer.
type asksForOrders struct{ s *asks.Service }

var _ orders.AskPricer = asksForOrders{}

// AskPrice implements orders.AskPricer.
func (a asksForOrders) AskPrice(ctx context.Context, customerID, storeID, replyID ids.ID, now time.Time) (orders.AskPrice, bool, error) {
	p, ok, err := a.s.AskPrice(ctx, customerID, storeID, replyID, now)
	if err != nil || !ok {
		return orders.AskPrice{}, ok, err
	}
	return orders.AskPrice{
		StoreItemID: p.StoreItemID, UnitPricePaise: p.UnitPaise, ValidUntil: p.ValidUntil, MaxQuantity: p.MaxQuantity,
	}, true, nil
}

// asksForInsights adapts asks.Service to facts.AskDemand. The rollup counts
// each ask once per store it was sent to, which is already "near the store",
// so the radius is not applied again.
type asksForInsights struct{ s *asks.Service }

var _ facts.AskDemand = asksForInsights{}

// Asks implements facts.AskDemand.
func (a asksForInsights) Asks(ctx context.Context, storeID ids.ID, from, to facts.Day, _ int64) ([]facts.AskCount, error) {
	rows, err := a.s.Demand(ctx, storeID, time.Time(from), time.Time(to))
	if err != nil {
		return nil, err
	}
	out := make([]facts.AskCount, len(rows))
	for i, r := range rows {
		out[i] = facts.AskCount{Day: facts.Day(r.Day), ProductID: r.ProductID, HasProd: r.HasProd, QueryNorm: r.QueryNorm, Count: r.Count}
	}
	return out, nil
}

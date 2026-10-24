package orders

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/settings"
)

// KindStoreReliability is the hourly store_reliability job (05 §3, D-67):
// it recomputes each store's 30-day accept and unaccepted-expiry rates and
// its reliability score, writing only those fields, and raises the
// store_strikes risk flag for a store at store_strike_risk_threshold.
const KindStoreReliability = "store_reliability"

// storeWindow is the store-strike and reliability window. It is fixed, not
// a setting (01 §17 "Fixed", D-85).
const storeWindow = 30 * 24 * time.Hour //settings:ok D-85 fixed 30-day store window

// strikeStore records a strike against the order's store, once per order,
// and raises the store_strikes flag when the store reaches threshold. The
// reliability score drops on the job's next run (BK-46). No automatic
// pause: the store stays live (D-67).
func (s *Service) strikeStore(ctx context.Context, q *ordersdb.Queries, o ordersdb.Order, reason string, now time.Time, threshold int) error {
	n, err := q.InsertStrike(ctx, ordersdb.InsertStrikeParams{
		ID: ids.UUID(s.ids.New(now)), SubjectType: "store", SubjectID: o.StoreID,
		OrderID: o.ID, Reason: reason, CreatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("orders: store strike: %w", err)
	}
	if n == 0 {
		return nil
	}
	_, err = s.flagStores(ctx, q, o.StoreID, false, now, threshold)
	return err
}

// flagStores raises the store_strikes flag for the store (or, with every,
// each store) with threshold or more unreset strikes in the window, and
// returns how many flags it raised.
func (s *Service) flagStores(ctx context.Context, q *ordersdb.Queries, store pgtype.UUID, every bool, now time.Time, threshold int) (int64, error) {
	t := int32(min(max(threshold, 1), math.MaxInt32)) //nolint:gosec // clamped just above
	hits, err := q.StoresAtStrikeThreshold(ctx, ordersdb.StoresAtStrikeThresholdParams{
		Since: now.Add(-storeWindow), Threshold: t, Every: every, StoreID: store,
	})
	if err != nil {
		return 0, fmt.Errorf("orders: store strikes: %w", err)
	}
	var raised int64
	for _, h := range hits {
		details, err := json.Marshal(map[string]any{
			"strikes": h.N, "threshold": t, "windowDays": int(storeWindow / (24 * time.Hour)), "oldestStrikeAt": h.Oldest,
		})
		if err != nil {
			return raised, err
		}
		n, err := q.InsertStoreStrikeFlag(ctx, ordersdb.InsertStoreStrikeFlagParams{
			ID: ids.UUID(s.ids.New(now)), StoreID: h.StoreID, Score: strikeScore(h.N, t),
			Details: details, Now: now, Newest: h.Newest,
		})
		if err != nil {
			return raised, fmt.Errorf("orders: store strike flag: %w", err)
		}
		raised += n
	}
	return raised, nil
}

// strikeScore is the flag's score: 50 at the threshold, 100 at twice it,
// the same scale the reliability score loses on.
func strikeScore(n, threshold int32) int32 {
	return min(100, n*50/threshold)
}

// StoreReliability is the KindStoreReliability handler.
// TODO(lead): 01 §5.4 and 05 §3 give the inputs (accept rate, expiry
// penalty, store strikes) but not the formula; the one in
// RecomputeStoreReliability is the conservative reading. Confirm it.
func (s *Service) StoreReliability(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
	if err := s.ready(ctx); err != nil {
		return platformjobs.Result{}, err
	}
	threshold, err := s.count(ctx, settings.StoreStrikeRiskThreshold)
	if err != nil {
		return platformjobs.Result{}, err
	}
	expiryBp, err := s.settings.BP(ctx, settings.ShopExpiryPenaltyThresholdBp)
	if err != nil {
		return platformjobs.Result{}, fmt.Errorf("orders: %s: %w", settings.ShopExpiryPenaltyThresholdBp, err)
	}
	now := s.now()
	q := s.q(ctx)
	n, err := q.RecomputeStoreReliability(ctx, ordersdb.RecomputeStoreReliabilityParams{
		Now: now, Since: now.Add(-storeWindow),
		ExpiryBp: int32(min(max(expiryBp, 0), 10000)), Threshold: int32(max(threshold, 1)), //nolint:gosec // both clamped
	})
	if err != nil {
		return platformjobs.Result{}, fmt.Errorf("orders: store reliability: %w", err)
	}
	flags, err := s.flagStores(ctx, q, pgtype.UUID{}, true, now, threshold)
	if err != nil {
		return platformjobs.Result{}, err
	}
	return platformjobs.Result{Summary: map[string]int64{"stores": n, "flags": flags}}, nil
}

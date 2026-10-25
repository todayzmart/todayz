package asks

// The asks timers (docs/runbooks/asks-timers.md): closing asks whose window
// is over, retention, and the daily demand rollup. Every run is idempotent.

import (
	"context"
	"time"

	"github.com/todayz/backend/gen/sqlc/asksdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/geo/h3"
	"github.com/todayz/backend/internal/platform/settings"
)

// maintenanceBatch bounds one run's work so it stays under its timeout; the
// next tick takes the rest.
const maintenanceBatch = 500 //settings:ok batch size, not a product rule

// CloseEnded closes the open asks every recipient has answered or whose last
// reply window is over, and reports how many it closed.
func (s *Service) CloseEnded(ctx context.Context) (int64, error) {
	if s.db == nil {
		return 0, errUnavailable
	}
	rows, err := s.q(ctx).EndedOpenAsks(ctx, asksdb.EndedOpenAsksParams{Now: s.now(), Lim: maintenanceBatch})
	if err != nil {
		return 0, err
	}
	var n int64
	for _, r := range rows {
		reason := ReasonWindowEnded
		if r.AllReplied != nil && *r.AllReplied {
			reason = ReasonAllReplied
		}
		var closed bool
		err := s.db.InTx(ctx, func(ctx context.Context) error {
			var err error
			closed, err = s.closeAsk(ctx, s.q(ctx), idOf(r.ID), reason)
			return err
		})
		if err != nil {
			return n, err
		}
		if closed {
			n++
		}
	}
	return n, nil
}

// Retain deletes asks older than ask_retention_days with their recipients,
// replies and the blocks that came from them (01 §11.2). Booked orders keep
// their lines; the reply link clears.
func (s *Service) Retain(ctx context.Context) (int64, error) {
	if s.db == nil || s.settings == nil {
		return 0, errUnavailable
	}
	days, err := s.settings.Int(ctx, settings.AskRetentionDays)
	if err != nil {
		return 0, err
	}
	before := s.now().Add(-time.Duration(days) * 24 * time.Hour)
	var total int64
	for {
		n, err := s.q(ctx).DeleteOldAsks(ctx, asksdb.DeleteOldAsksParams{Before: before, Lim: maintenanceBatch})
		if err != nil {
			return total, err
		}
		total += n
		if n < maintenanceBatch {
			return total, nil
		}
	}
}

// RollupDay recomputes one IST day of ask demand (the day starting at
// clock.StartOfDayIST(day)) and gives insight_geo_cells the centre of every
// new cell it uses. It reports the rows written.
func (s *Service) RollupDay(ctx context.Context, day time.Time) (int64, error) {
	if s.db == nil {
		return 0, errUnavailable
	}
	from := clock.StartOfDayIST(day)
	to := nextISTMidnight(from)
	d := pgDate(from.In(clock.IST))
	var n int64
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := q.DeleteAskDemandDay(ctx, d); err != nil {
			return err
		}
		var err error
		if n, err = q.RollupAskDemand(ctx, asksdb.RollupAskDemandParams{Day: d, FromAt: from.UTC(), ToAt: to.UTC()}); err != nil {
			return err
		}
		cells, err := q.AskDemandCells(ctx, d)
		if err != nil {
			return err
		}
		for _, c := range cells {
			lat, lng, err := h3.CellToLatLng(h3.FromInt64(c))
			if err != nil {
				continue // not a cell index: nothing to place
			}
			if err := q.InsertAskGeoCell(ctx, asksdb.InsertAskGeoCellParams{GeoCell: c, Lat: lat, Lng: lng}); err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}

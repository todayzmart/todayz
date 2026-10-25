package detectors_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/gen/sqlc/coachdb"
	"github.com/todayz/backend/internal/coach/detectors"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/seed"
)

// scenario is rows written on top of the dev seed (in a transaction rolled
// back afterwards) that make one own-SQL detector fire, or stop it firing.
type scenario struct {
	name     string
	detector string
	store    ids.ID
	setup    func(t *testing.T, tx pgx.Tx, s *sc)
	// none: the detector must not fire (a guard such as the k ≥ 10 floor).
	none bool
}

// sc holds what a setup needs: the store, the IST day before Epoch and an exec.
type sc struct {
	e     *env
	tx    pgx.Tx
	store ids.ID
	y     facts.Day
}

func (s *sc) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	tag, err := s.tx.Exec(s.e.ctx, sql, args...)
	require.NoError(t, err, sql)
	if strings.HasPrefix(sql, "UPDATE") {
		require.Positive(t, tag.RowsAffected(), sql)
	}
}

// sales writes one rollup_daily_store row (sales in paise, p50 accept seconds or 0).
func (s *sc) sales(t *testing.T, store ids.ID, d facts.Day, paise int64, bills int, p50 int) {
	t.Helper()
	var p any
	if p50 > 0 {
		p = p50
	}
	s.exec(t, `INSERT INTO rollup_daily_store (date, store_id, gmv_paise, completed, p50_accept_seconds, computed_at)
	           VALUES ($1, $2, $3, $4, $5, $6)
	           ON CONFLICT (date, store_id) DO UPDATE SET gmv_paise = EXCLUDED.gmv_paise, pos_gmv_paise = 0,
	             completed = EXCLUDED.completed, pos_sales = 0, p50_accept_seconds = EXCLUDED.p50_accept_seconds`,
		d.PG(), ids.UUID(store), paise, bills, p, seed.Epoch)
}

// order writes one zero-value pickup order of the store placed at; expired
// moves it on to expired unanswered, as the accept timer does.
func (s *sc) order(t *testing.T, expired bool, at time.Time) {
	t.Helper()
	var id string
	require.NoError(t, s.tx.QueryRow(s.e.ctx, `INSERT INTO orders (id, customer_id, store_id, fulfilment, subtotal_paise,
	             items_after_offers_paise, item_subtotal_paise, total_paise, code_enc, code_hmac, placed_at)
	           VALUES (gen_random_uuid(), (SELECT id FROM users ORDER BY id LIMIT 1), $1, 'pickup', 0, 0, 0, 0, '\x00'::bytea,
	                   decode(md5(random()::text) || md5(random()::text), 'hex'), $2) RETURNING id::text`,
		ids.UUID(s.store), at).Scan(&id))
	if expired {
		s.exec(t, `UPDATE orders SET status = 'expired', status_reason = 'not_accepted' WHERE id = $1::uuid`, id)
	}
}

// review writes one visible review of the store with text, created at.
func (s *sc) review(t *testing.T, stars int, at time.Time) {
	t.Helper()
	s.exec(t, `INSERT INTO reviews (id, store_id, customer_id, order_id, stars, text, edited_until, created_at, updated_at)
	           VALUES (gen_random_uuid(), $1, (SELECT id FROM users ORDER BY id LIMIT 1), gen_random_uuid(), $2,
	                   'Test review', $3, $3, $3)`, ids.UUID(s.store), stars, at)
}

// shops adds n live stores of n other businesses at the store's location, each
// with a median accept time of p50 seconds on day y.
func (s *sc) shops(t *testing.T, n, p50 int) {
	t.Helper()
	const alpha = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	for i := 0; i < n; i++ {
		code := "KF" + string(alpha[i/len(alpha)]) + "AA" + string(alpha[i%len(alpha)])
		var biz, store string
		require.NoError(t, s.tx.QueryRow(s.e.ctx, `INSERT INTO businesses (id, brand_name, referral_code)
		  VALUES (gen_random_uuid(), $1, $2) RETURNING id::text`, fmt.Sprintf("Bench %d", i), code).Scan(&biz))
		require.NoError(t, s.tx.QueryRow(s.e.ctx, `INSERT INTO stores (id, business_id, slug, short_code, name, location, status, live_at)
		  VALUES (gen_random_uuid(), $1, $2, $3, $4, (SELECT location FROM stores WHERE id = $5), 'live', $6) RETURNING id::text`,
			biz, fmt.Sprintf("bench-%d", i), code, fmt.Sprintf("Bench %d", i), ids.UUID(s.store), seed.Epoch).Scan(&store))
		s.exec(t, `INSERT INTO rollup_daily_store (date, store_id, p50_accept_seconds, computed_at) VALUES ($1, $2::uuid, $3, $4)`,
			s.y.PG(), store, p50, seed.Epoch)
	}
}

func scenarios() []scenario {
	return []scenario{
		{name: "missed_orders_peak_hour", detector: "missed_orders_peak_hour", store: storeSL,
			setup: func(t *testing.T, _ pgx.Tx, s *sc) {
				// Three orders expired unanswered yesterday: two at 7 pm, one at
				// 8 pm IST; one still waiting at 9 pm does not count.
				for i, h := range []int{19, 19, 20} {
					s.order(t, true, s.y.Start().Add(time.Duration(h)*time.Hour+time.Duration(i)*time.Minute))
				}
				s.order(t, false, s.y.Start().Add(21*time.Hour))
			}},
		{name: "best_day_record", detector: "best_day_record", store: storeSL,
			setup: func(t *testing.T, _ pgx.Tx, s *sc) {
				s.exec(t, `DELETE FROM rollup_daily_store WHERE store_id = $1`, ids.UUID(s.store))
				s.sales(t, s.store, s.y, 1824000, 40, 0)
				for w := 1; w <= 6; w++ {
					s.sales(t, s.store, s.y.Add(-7*w), int64(1000000+w*50000), 30, 0)
				}
			}},
		{name: "best_day_record_too_new", detector: "best_day_record", store: storeSL, none: true,
			setup: func(t *testing.T, _ pgx.Tx, s *sc) {
				// Only three earlier same weekdays on record: not a record yet.
				s.exec(t, `DELETE FROM rollup_daily_store WHERE store_id = $1`, ids.UUID(s.store))
				s.sales(t, s.store, s.y, 1824000, 40, 0)
				for w := 1; w <= 3; w++ {
					s.sales(t, s.store, s.y.Add(-7*w), 1000000, 30, 0)
				}
			}},
		{name: "reviews_unanswered", detector: "reviews_unanswered", store: storeSL,
			setup: func(t *testing.T, _ pgx.Tx, s *sc) {
				s.exec(t, `ALTER TABLE reviews DROP CONSTRAINT fk_reviews_order_id`)
				s.review(t, 2, s.y.Start().Add(10*time.Hour))
				s.review(t, 4, s.y.Add(-1).Start().Add(10*time.Hour))
			}},
		{name: "weekly_summary", detector: "weekly_summary", store: storeSL,
			setup: func(t *testing.T, _ pgx.Tx, s *sc) {
				// Epoch is a Monday: last week is the 7 days before it, 4 weeks before that.
				s.exec(t, `DELETE FROM rollup_daily_store WHERE store_id = $1`, ids.UUID(s.store))
				for d := 1; d <= 35; d++ {
					paise := int64(200000)
					if d <= 7 {
						paise = 250000
					}
					s.sales(t, s.store, s.y.Add(1-d), paise, 10, 0)
				}
			}},
		{name: "rating_drop", detector: "rating_drop", store: storeSL,
			setup: func(t *testing.T, _ pgx.Tx, s *sc) {
				s.exec(t, `ALTER TABLE reviews DROP CONSTRAINT fk_reviews_order_id`)
				s.exec(t, `WITH x AS (SELECT 1) UPDATE reviews SET hidden_at = $2, hidden_by = (SELECT id FROM users ORDER BY id LIMIT 1), hide_reason = 'test'
				           WHERE store_id = $1 AND hidden_at IS NULL`, ids.UUID(s.store), seed.Epoch)
				for i := 0; i < 20; i++ {
					s.review(t, 3, s.y.Add(-i).Start().Add(9*time.Hour))
					s.review(t, 5, s.y.Add(-40-i).Start().Add(9*time.Hour))
				}
			}},
		{name: "accept_time_vs_benchmark", detector: "accept_time_vs_benchmark", store: storeSL,
			setup: func(t *testing.T, _ pgx.Tx, s *sc) {
				s.sales(t, s.store, s.y, 100000, 5, 900)
				s.shops(t, detectors.KFloor, 120)
			}},
		{name: "accept_time_below_k", detector: "accept_time_vs_benchmark", store: storeSL, none: true,
			setup: func(t *testing.T, _ pgx.Tx, s *sc) {
				// Nine other businesses: below k ≥ 10, so nothing is said about them.
				s.sales(t, s.store, s.y, 100000, 5, 900)
				s.shops(t, detectors.KFloor-1, 120)
			}},
		{name: "setup_incomplete", detector: "setup_incomplete", store: storeSL,
			setup: func(t *testing.T, _ pgx.Tx, s *sc) {
				s.exec(t, `UPDATE stores SET hours = '{}'::jsonb WHERE id = $1`, ids.UUID(s.store))
			}},
		{name: "expiry_penalty", detector: "expiry_penalty", store: storeSL,
			setup: func(t *testing.T, _ pgx.Tx, s *sc) {
				s.exec(t, `UPDATE stores SET unaccepted_expiry_rate_30d = 0.4 WHERE id = $1`, ids.UUID(s.store))
			}},
		{name: "expiry_penalty_under", detector: "expiry_penalty", store: storeSL, none: true,
			setup: func(t *testing.T, _ pgx.Tx, s *sc) {
				s.exec(t, `UPDATE stores SET unaccepted_expiry_rate_30d = 0 WHERE id = $1`, ids.UUID(s.store))
			}},
	}
}

func detectorNamed(t *testing.T, name string) detectors.Detector {
	t.Helper()
	for _, d := range detectors.All() {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no detector %s", name)
	return detectors.Detector{}
}

// TestScenarios: every own-SQL detector fires on rows that should make it
// fire (golden numbers, severity, action) and stays quiet under its guards.
func TestScenarios(t *testing.T) {
	e := newSeeded(t)
	covered := map[string]bool{}
	for _, c := range scenarios() {
		t.Run(c.name, func(t *testing.T) {
			tx, err := e.tdb.OwnerPool.Begin(e.ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(e.ctx) }()
			s := &sc{e: e, tx: tx, store: c.store, y: facts.DayOf(seed.Epoch).Add(-1)}
			c.setup(t, tx, s)
			d := detectorNamed(t, c.detector)
			fs, err := d.Run(e.ctx, e.input(t, coachdb.New(tx), c.store, seed.Epoch))
			require.NoError(t, err)
			if c.none {
				require.Empty(t, fs)
				return
			}
			require.NotEmpty(t, fs)
			for _, f := range fs {
				require.Equal(t, c.detector, f.Detector)
				if f.AboutOthers {
					require.GreaterOrEqual(t, f.Others, int64(detectors.KFloor))
				}
			}
			covered[c.detector] = true
			checkGolden(t, "scenario_"+c.name, goldenOf(fs))
		})
	}
	for _, d := range detectors.All() {
		if d.FromFacts == "" {
			require.True(t, covered[d.Name], "no firing scenario for %s", d.Name)
		}
	}
}

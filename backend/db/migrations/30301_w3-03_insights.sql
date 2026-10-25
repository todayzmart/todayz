-- W3-03 Demand insights (01 §12, D-99; 03 insight_feedback).
--
-- insight_feedback: "Not useful" on one subject of a Demand insights row. The
-- subject, not the whole kind, stays quiet until quiet_until
-- (coach_dismiss_quiet_days). A repeat dismissal while quiet_until is in the
-- future keeps it; after it has passed the row is renewed (the query's upsert).
-- A going-to-waste row writes one row per batch in it. expired_on_shelf is
-- never written: it cannot be dismissed. user_id is a plain uuid (D-35).
-- updated_at is set from the injected clock by the queries.
--
-- insight_geo_cells: the centre of each H3 resolution-8 cell that analytics
-- stores searches under (D-81). "Wanted nearby" and "Missed sales" add up the
-- cells whose centre is within insight_wanted_nearby_radius_m of the store.
-- The backend has no H3 library yet, so search_log.geo_cell is written NULL
-- (internal/search) and the only cells are the seeded W, S2 and M; the writer
-- that computes a search's cell fills this table with the cells it uses.
-- TODO(lead): an H3 library (or the cell computed by the client) to fill
-- search_log.geo_cell and this table outside the seed.

-- +goose Up

CREATE TABLE insight_feedback (
  id          uuid PRIMARY KEY,
  store_id    uuid NOT NULL REFERENCES stores (id),
  user_id     uuid NOT NULL,
  kind        text NOT NULL,
  subject_ref text NOT NULL,
  reason      text NOT NULL DEFAULT 'not_useful',
  quiet_until timestamptz NOT NULL,
  created_at  timestamptz NOT NULL,
  updated_at  timestamptz NOT NULL,
  CONSTRAINT ux_insight_feedback UNIQUE (store_id, kind, subject_ref),
  CONSTRAINT ck_insight_feedback_kind CHECK (kind IN ('selling_fast', 'sitting_on_shelf', 'going_to_waste',
    'wanted_nearby', 'missed_sales', 'selling_below_cost', 'stock_check_due', 'bought_together',
    'regulars_slipping', 'priced_above_nearby')),
  CONSTRAINT ck_insight_feedback_subject CHECK (char_length(subject_ref) BETWEEN 1 AND 210),
  CONSTRAINT ck_insight_feedback_reason CHECK (reason IN ('not_useful')),
  CONSTRAINT ck_insight_feedback_quiet CHECK (quiet_until > created_at)
);
CREATE INDEX ix_insight_feedback_store_quiet ON insight_feedback (store_id, quiet_until);

CREATE TABLE insight_geo_cells (
  geo_cell bigint PRIMARY KEY,
  centre   geography(Point, 4326) NOT NULL
);
CREATE INDEX ix_insight_geo_cells_centre ON insight_geo_cells USING gist (centre);

-- +goose Down
DROP TABLE IF EXISTS insight_geo_cells;
DROP TABLE IF EXISTS insight_feedback;

package detectors_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/gen/sqlc/coachdb"
	"github.com/todayz/backend/gen/sqlc/insightsdb"
	"github.com/todayz/backend/internal/coach/detectors"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/seed"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

var update = flag.Bool("update", false, "rewrite testdata/*.golden.json")

// The dev seed's stores (internal/seed/business.go).
var (
	storeSL  = mustID("01KE0S1W6W8ZV7T1C3A6MS0001")
	storeSL2 = mustID("01KE0S1W6W8ZV7T1C3A6MS0002")
)

func mustID(s string) ids.ID {
	id, err := ids.Parse(s)
	if err != nil {
		panic(err)
	}
	return id
}

type env struct {
	tdb *testkit.DB
	st  *settings.Cache
	ctx context.Context
}

func newSeeded(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	enc, hash := make([]byte, 32), make([]byte, 32)
	_, err := rand.Read(enc)
	require.NoError(t, err)
	_, err = rand.Read(hash)
	require.NoError(t, err)
	keys := config.AppKeys{PIIEncKey: config.NewSecret(enc), PIIHashKey: config.NewSecret(hash)}
	require.NoError(t, seed.Run(ctx, config.EnvTest, seed.Dev, seed.Deps{DB: db.New(tdb.Pool), Keys: keys}))
	return &env{tdb: tdb, st: settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{}), ctx: ctx}
}

// input is what a detector reads for store at now, through q.
func (e *env) input(t *testing.T, q *coachdb.Queries, store ids.ID, now time.Time) *detectors.Input {
	t.Helper()
	prof, err := q.StoreProfile(e.ctx, ids.UUID(store))
	require.NoError(t, err)
	return &detectors.Input{Q: q, Settings: e.st, Store: detectors.Store{ID: store, StoreProfileRow: prof}, Now: now, Today: facts.DayOf(now)}
}

// run runs job j's detectors for store at now, as the coach does.
func (e *env) run(t *testing.T, j detectors.Job, store ids.ID, now time.Time) []detectors.Fact {
	t.Helper()
	in := e.input(t, coachdb.New(e.tdb.Pool), store, now)
	if detectors.NeedsFacts(j) {
		cfg, err := facts.LoadSettings(e.ctx, e.st)
		require.NoError(t, err)
		res, err := facts.ComputeWith(e.ctx, facts.Source{Q: insightsdb.New(e.tdb.Pool), Settings: e.st, Asks: facts.NoAsks{}}, cfg, store, now, nil)
		require.NoError(t, err)
		in.Facts, in.FactsLoaded, in.FactsCfg = res, true, cfg
	}
	var out []detectors.Fact
	for _, d := range detectors.For(j) {
		fs, err := d.Run(e.ctx, in)
		require.NoError(t, err, d.Name)
		out = append(out, fs...)
	}
	return out
}

// golden is the stable view of a fact (numbers rendered as JSON).
type golden struct {
	Detector    string         `json:"detector"`
	Subject     string         `json:"subject"`
	Kind        string         `json:"kind"`
	Topic       string         `json:"topic"`
	Severity    int            `json:"severity"`
	Stake       int64          `json:"stake"`
	AgeDays     int            `json:"ageDays"`
	Variant     string         `json:"variant,omitempty"`
	Numbers     map[string]any `json:"numbers"`
	Action      string         `json:"action"`
	Payload     map[string]any `json:"payload,omitempty"`
	AboutOthers bool           `json:"aboutOthers,omitempty"`
	Others      int64          `json:"others,omitempty"`
}

func goldenOf(fs []detectors.Fact) []golden {
	out := make([]golden, 0, len(fs))
	for _, f := range fs {
		out = append(out, golden{
			Detector: f.Detector, Subject: f.Subject, Kind: string(f.Kind), Topic: f.Topic, Severity: f.Severity,
			Stake: f.Stake, AgeDays: f.AgeDays, Variant: f.Variant, Numbers: f.Numbers,
			Action: f.Action.Type, Payload: f.Action.Payload, AboutOthers: f.AboutOthers, Others: f.Others,
		})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Detector != out[b].Detector {
			return out[a].Detector < out[b].Detector
		}
		return out[a].Subject < out[b].Subject
	})
	return out
}

func checkGolden(t *testing.T, name string, got any) {
	t.Helper()
	b, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)
	// Round-trip so ints and floats compare the way the file holds them.
	path := filepath.Join("testdata", name+".golden.json")
	if *update {
		require.NoError(t, os.WriteFile(path, append(b, '\n'), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run go test ./internal/coach/detectors -update")
	require.JSONEq(t, string(want), string(b))
}

// TestGoldenOnTheDevSeed runs every job's detectors on both dev stores at the
// seed's now and compares every fact (numbers, severity, action) with the
// golden file.
func TestGoldenOnTheDevSeed(t *testing.T) {
	e := newSeeded(t)
	for _, c := range []struct {
		j   detectors.Job
		now time.Time
	}{{detectors.Daily, seed.Epoch}, {detectors.Weekly, seed.Epoch}, {detectors.Realtime, seed.Epoch}} {
		for name, store := range map[string]ids.ID{"sl": storeSL, "sl2": storeSL2} {
			t.Run(string(c.j)+"_"+name, func(t *testing.T) {
				checkGolden(t, string(c.j)+"_"+name, goldenOf(e.run(t, c.j, store, c.now)))
			})
		}
	}
}

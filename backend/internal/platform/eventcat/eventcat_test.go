package eventcat

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

const good = `domain: app
owner: W1-07
events:
  app_opened:
    emitter: client
    props:
      launch: { type: enum, values: [cold, warm] }
      n: { type: int, min: 0, max: 10, optional: true }
  thing_done:
    emitter: server
    props:
      id: { type: ulid }
      feature: { type: token }
      ok: { type: bool }
      ratio: { type: number, min: 0, max: 1 }
`

func load(t *testing.T, files map[string]string) (*Catalogue, error) {
	t.Helper()
	m := fstest.MapFS{}
	for n, c := range files {
		m[n] = &fstest.MapFile{Data: []byte(c)}
	}
	return Load(m)
}

func TestLoadAndValidate(t *testing.T) {
	c, err := load(t, map[string]string{"app.yaml": good})
	require.NoError(t, err)
	require.Equal(t, []string{"app_opened", "thing_done"}, c.Names())
	e, _ := c.Event("app_opened")
	require.NoError(t, e.Validate(map[string]any{"launch": "cold"}))
	require.NoError(t, e.Validate(map[string]any{"launch": "warm", "n": float64(3)}))
	for name, props := range map[string]map[string]any{
		"missing":   {},
		"bad enum":  {"launch": "lukewarm"},
		"unknown":   {"launch": "cold", "query": "milk near me"},
		"range":     {"launch": "cold", "n": float64(11)},
		"fraction":  {"launch": "cold", "n": 1.5},
		"wrong typ": {"launch": 1},
	} {
		require.Error(t, e.Validate(props), name)
	}
	th, _ := c.Event("thing_done")
	require.NoError(t, th.Validate(map[string]any{"id": "01JB0S1W6W8ZV7T1C3A6M9N4PQ", "feature": "voice_stock", "ok": true, "ratio": 0.5}))
	require.Error(t, th.Validate(map[string]any{"id": "not-a-ulid", "feature": "x", "ok": true, "ratio": 0.5}))
	require.Error(t, th.Validate(map[string]any{"id": "01JB0S1W6W8ZV7T1C3A6M9N4PQ", "feature": "Free text with spaces", "ok": true, "ratio": 0.5}))
}

// TestErrorsNeverEchoTheValue: a hostile prop value is not repeated back.
func TestErrorsNeverEchoTheValue(t *testing.T) {
	c, _ := load(t, map[string]string{"app.yaml": good})
	e, _ := c.Event("app_opened")
	err := e.Validate(map[string]any{"launch": "9876543210"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "9876543210")
}

func TestDuplicateEventNameAcrossFilesFails(t *testing.T) {
	_, err := load(t, map[string]string{
		"app.yaml":    good,
		"orders.yaml": "domain: orders\nowner: W2-01\nevents:\n  app_opened:\n    emitter: server\n    props: {}\n",
	})
	require.ErrorContains(t, err, "already defined")
}

func TestRejectsFreeTextAndBadShapes(t *testing.T) {
	for name, y := range map[string]string{
		"string type":  "domain: app\nowner: W\nevents:\n  e:\n    emitter: client\n    props:\n      q: { type: string }\n",
		"bad emitter":  "domain: app\nowner: W\nevents:\n  e:\n    emitter: both\n    props: {}\n",
		"bad name":     "domain: app\nowner: W\nevents:\n  Bad-Name:\n    emitter: client\n    props: {}\n",
		"unknown key":  "domain: app\nowner: W\nevents:\n  e:\n    emitter: client\n    extra: 1\n    props: {}\n",
		"wrong domain": "domain: other\nowner: W\nevents: {}\n",
		"enum empty":   "domain: app\nowner: W\nevents:\n  e:\n    emitter: client\n    props:\n      p: { type: enum }\n",
	} {
		_, err := load(t, map[string]string{"app.yaml": y})
		require.Error(t, err, name)
	}
}

// TestRepoCatalogueLoads loads the real files (skipped outside the monorepo).
func TestRepoCatalogueLoads(t *testing.T) {
	dir := "../../../../shared/telemetry/events"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("shared/ not present")
	}
	c, err := Load(os.DirFS(dir))
	require.NoError(t, err)
	require.NotZero(t, c.Len())
}

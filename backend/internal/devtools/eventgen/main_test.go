package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunGeneratesGoAndTS(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in")
	require.NoError(t, os.MkdirAll(in, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(in, "app.yaml"), []byte(`domain: app
owner: W1-07
events:
  app_opened:
    emitter: client
    props:
      launch: { type: enum, values: [cold, warm] }
      n: { type: int, optional: true }
  thing_done:
    emitter: server
    props: {}
`), 0o600))
	goOut, tsOut := filepath.Join(dir, "gen", "events"), filepath.Join(dir, "ts", "events.ts")
	require.NoError(t, run(in, goOut, tsOut))
	require.FileExists(t, filepath.Join(goOut, "embed.go"))
	require.FileExists(t, filepath.Join(goOut, "app.yaml"))
	b, err := os.ReadFile(tsOut)
	require.NoError(t, err)
	ts := string(b)
	require.Contains(t, ts, "app_opened: { launch: 'cold' | 'warm'; n?: number; };")
	require.Contains(t, ts, "thing_done: { };")
	require.Contains(t, ts, "CLIENT_EVENTS = ['app_opened'] as const")
}

func TestRunFailsOnDuplicateName(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in")
	require.NoError(t, os.MkdirAll(in, 0o750))
	ev := "events:\n  same:\n    emitter: client\n    props: {}\n"
	require.NoError(t, os.WriteFile(filepath.Join(in, "a.yaml"), []byte("domain: a\nowner: W\n"+ev), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(in, "b.yaml"), []byte("domain: b\nowner: W\n"+ev), 0o600))
	require.ErrorContains(t, run(in, filepath.Join(dir, "g"), filepath.Join(dir, "e.ts")), "already defined")
}

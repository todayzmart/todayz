package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
)

// SC-AI-08: `todayz ai eval all` replays the recorded fixtures with no key and no network
// (make check), and --live without the user's own key names what is missing, never a value.
func TestAIEvalRecorded(t *testing.T) {
	code, out, stderr := runCmd([]string{"ai", "eval", "all"}, nil)
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, "ai_selftest")
	assert.Contains(t, out, "recorded 12/12")
	assert.Contains(t, out, " ok ")

	code, out, _ = runCmd([]string{"ai", "eval", "ai_selftest"}, nil)
	require.Equal(t, 0, code)
	assert.Contains(t, out, "ai_selftest")
}

func TestAIEvalUsage(t *testing.T) {
	for _, args := range [][]string{{"ai"}, {"ai", "run"}, {"ai", "eval"}, {"ai", "eval", "a", "b"}, {"ai", "eval", "no_such_suite"}} {
		code, _, _ := runCmd(args, nil)
		assert.Equal(t, 2, code, args)
	}
}

func TestAIEvalLiveNeedsOwnKey(t *testing.T) {
	e := appKeys()
	e["APP_ENV"] = "local"
	var out bytes.Buffer
	err := runAI(context.Background(), []string{"eval", "all", "--live"}, env(e), &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ANTHROPIC_API_KEY")
	assert.Contains(t, err.Error(), "LLM_API_URL")
}

// Every feature of one router shares one kit (one guard, one cost gauge).
func TestAIKitShared(t *testing.T) {
	d := &deps{Config: &config.Config{Env: config.EnvTest}, Clock: clock.Real{}}
	k := aiKitFor(d)
	assert.Same(t, k, aiKitFor(d))
	assert.NotNil(t, k.STT("voice_stock"))
	assert.NotSame(t, k, aiKitFor(&deps{Config: d.Config, Clock: d.Clock}))
}

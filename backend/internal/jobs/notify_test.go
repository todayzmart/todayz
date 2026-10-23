package jobs

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/notify"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

func piiConfig(t *testing.T) *config.Config {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return &config.Config{Env: config.EnvTest, Keys: config.AppKeys{PIIEncKey: config.NewSecret(key)}}
}

// TestEscalationJobIsRegisteredWhenWired: with a pool and PII_ENC_KEY the
// default sources are the orders service and the business contacts, and the
// "not seen" job is defined; without either it stays off.
func TestEscalationJobIsRegisteredWhenWired(t *testing.T) {
	// A pool connects lazily: nothing is dialled here.
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/none")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	clk := clock.NewFake(clock.Real{}.Now())

	register := func(d Deps) *platformjobs.Registry {
		r := platformjobs.NewRegistry()
		Register(r, d)
		return r
	}

	on := register(Deps{Pool: pool, Clock: clk, Config: piiConfig(t)})
	k, ok := on.Lookup(notify.KindOrderUnseen)
	require.True(t, ok, "registered once the sources are wired")
	require.Equal(t, "* * * * *", k.Schedule)

	for name, d := range map[string]Deps{
		"no pool": {Clock: clk, Config: piiConfig(t)},
		"no key":  {Pool: pool, Clock: clk, Config: &config.Config{Env: config.EnvTest}},
	} {
		_, ok := register(d).Lookup(notify.KindOrderUnseen)
		require.False(t, ok, name)
	}
}

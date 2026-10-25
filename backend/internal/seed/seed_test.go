package seed

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// domains is the 07 W0-03 list; internal/jobs has the same test.
var domains = []string{
	"identity", "business", "catalogue", "search", "telemetry",
	"credits", "orders", "referrals", "pos", "notify",
	"reviews", "offers", "banners", "support", "payouts",
	"risk", "insights", "voice", "admin", "rollups",
	"ai", "coach", "standee", "baskets", "asks",
}

func TestOneLineAndOneFilePerDomain(t *testing.T) {
	names := make([]string, 0, len(Domains))
	for _, d := range Domains {
		names = append(names, d.Name)
		_, err := os.Stat(d.Name + ".go")
		require.NoError(t, err, "internal/seed/%s.go holds the %s domain's seed", d.Name, d.Name)
	}
	require.Equal(t, domains, names)
}

func TestRefusedInStagingAndProd(t *testing.T) {
	for _, env := range []config.Env{config.EnvStaging, config.EnvProd, "nope"} {
		err := Run(context.Background(), env, Dev, Deps{})
		require.ErrorAs(t, err, &ErrRefused{}, env)
	}
	require.NoError(t, Allowed(config.EnvLocal))
	require.NoError(t, Allowed(config.EnvTest))
}

func TestRunEveryProfileAsAppRole(t *testing.T) {
	tdb := testkit.NewDB(t)
	for _, p := range Profiles {
		require.NoError(t, Run(context.Background(), config.EnvTest, p, Deps{DB: db.New(tdb.Pool)}), p)
	}
	require.Error(t, Run(context.Background(), config.EnvLocal, "big", Deps{DB: db.New(tdb.Pool)}))
}

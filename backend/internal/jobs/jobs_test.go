package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// domains is the 07 W0-03 list; internal/seed has the same test.
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
		require.NoError(t, err, "internal/jobs/%s.go holds the %s domain's jobs", d.Name, d.Name)
	}
	require.Equal(t, domains, names)
}

// TestSQLCPackagePerDomain (review D3): db/sqlc.yaml has one entry per
// domain, each with its own queries folder and package <domain>db.
func TestSQLCPackagePerDomain(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "db", "sqlc.yaml"))
	require.NoError(t, err)
	cfg := string(b)
	for _, d := range append([]string{"platform"}, domains...) {
		require.Equal(t, 1, strings.Count(cfg, "queries: queries/"+d+"\n"), d)
		require.Equal(t, 1, strings.Count(cfg, "package: "+d+"db\n"), d)
		require.Equal(t, 1, strings.Count(cfg, "out: ../gen/sqlc/"+d+"db\n"), d)
		files, err := filepath.Glob(filepath.Join("..", "..", "db", "queries", d, "*.sql"))
		require.NoError(t, err)
		require.NotEmpty(t, files, "db/queries/%s/ has a query file", d)
	}
	loose, err := filepath.Glob(filepath.Join("..", "..", "db", "queries", "*.sql"))
	require.NoError(t, err)
	require.Empty(t, loose, "queries live in db/queries/<domain>/")
}

func TestRegisterAddsPlatformJobs(t *testing.T) {
	r := platformjobs.NewRegistry()
	Register(r, Deps{Clock: clock.NewFake(clock.Real{}.Now())})
	names := r.Names()
	for _, k := range []string{"platform_heartbeat", "platform_maintenance", "jobs_watchdog", "job_runs_retention"} {
		require.Contains(t, names, k)
		sched, ok := r.ScheduleOf(k)
		require.True(t, ok, "%s is scheduled", k)
		require.NotNil(t, sched)
	}
}

// TestIdentityPurgeIsAKind: W1-01's purge runs hourly through jobs.Define
// (it was a River periodic job, identity.purge, before W1-11).
func TestIdentityPurgeIsAKind(t *testing.T) {
	r := platformjobs.NewRegistry()
	Register(r, Deps{Clock: clock.NewFake(clock.Real{}.Now())})
	k, ok := r.Lookup(KindIdentityPurge)
	require.True(t, ok)
	require.Equal(t, "identity_purge", k.Name)
	require.Equal(t, "W1-01", k.Owner)
	require.Equal(t, platformjobs.ClassMaintenance, k.Class)
	require.Equal(t, "0 * * * *", k.Schedule)
	require.Equal(t, platformjobs.PerTick, k.UniqueBy)
	sched, ok := r.ScheduleOf(KindIdentityPurge)
	require.True(t, ok)
	at := time.Date(2026, time.October, 23, 10, 17, 0, 0, clock.IST)
	require.Equal(t, time.Date(2026, time.October, 23, 11, 0, 0, 0, clock.IST), sched.Next(at).In(clock.IST))
}

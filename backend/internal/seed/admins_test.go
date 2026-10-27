package seed

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/testkit"
)

// repoFixture is the committed fixture, read by tests only while the repo is
// one tree (after the split the test that needs it skips).
const repoFixture = "../../../e2e/fixtures/admins.yaml"

// withEnv replaces getenv for one test.
func withEnv(t *testing.T, env map[string]string) {
	t.Helper()
	prev := getenv
	getenv = func(k string) string { return env[k] }
	t.Cleanup(func() { getenv = prev })
}

func adminKeys(t *testing.T) config.AppKeys {
	t.Helper()
	return testKeys(t)
}

func logTo(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func fixture(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat(repoFixture); err != nil {
		t.Skip("e2e/fixtures/admins.yaml is not in this tree")
	}
	return repoFixture
}

// TestRepoFixtureComposition checks the committed fixture against 08 §2: an
// admin per D-64 role, two each of finance and super_admin, and the
// never-signed-in admin.unenrolled.
func TestRepoFixtureComposition(t *testing.T) {
	admins, err := loadSeedAdmins(fixture(t))
	require.NoError(t, err)
	by := map[string]int{}
	keys := map[string]bool{}
	for _, a := range admins {
		keys[a.Key] = true
		for _, r := range a.Roles {
			by[r]++
		}
	}
	require.True(t, keys["admin.unenrolled"])
	for _, r := range authz.AdminRoles {
		require.GreaterOrEqual(t, by[string(r)], 1, r)
	}
	require.Equal(t, 2, by[string(authz.Finance)])
	require.Equal(t, 2, by[string(authz.SuperAdmin)])
}

func TestSeedAdmins(t *testing.T) {
	path := fixture(t)
	withEnv(t, map[string]string{seedAdminsFileEnv: path, "APP_ENV": "test"})
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	keys := adminKeys(t)
	var logs bytes.Buffer
	d := Deps{DB: db.New(tdb.Pool), Keys: keys, Log: logTo(&logs)}
	require.NoError(t, Run(ctx, config.EnvTest, Test, d))
	require.NoError(t, Run(ctx, config.EnvTest, Dev, d), "a second run is a no-op")

	admins, err := loadSeedAdmins(path)
	require.NoError(t, err)
	var n int
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&n))
	require.Equal(t, len(admins), n)

	hash, err := pii.NewHasher(keys.PIIHashKey.Bytes())
	require.NoError(t, err)
	for _, a := range admins {
		var roles []string
		require.NoError(t, tdb.Pool.QueryRow(ctx,
			`SELECT roles FROM admin_users WHERE user_id = $1`,
			ids.UUID(mustID(a.ID))).Scan(&roles), a.Key)
		require.Equal(t, a.Roles, roles, a.Key)

		var ph []byte
		require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT phone_hash FROM phone_identities WHERE user_id = $1`,
			ids.UUID(mustID(a.ID))).Scan(&ph), a.Key)
		require.Equal(t, hash.Phone(a.Phone), ph, "%s: sign-in finds the admin by phone", a.Key)

		// No phone reaches the log.
		require.NotContains(t, logs.String(), a.Phone)
	}
	require.Contains(t, logs.String(), "admins seeded")
}

func TestSeedAdminsUnsetLogsTheName(t *testing.T) {
	withEnv(t, map[string]string{"APP_ENV": "local"})
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	var logs bytes.Buffer
	require.NoError(t, Run(ctx, config.EnvTest, Dev, Deps{DB: db.New(tdb.Pool), Keys: adminKeys(t), Log: logTo(&logs)}))
	var n int
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&n))
	require.Zero(t, n)
	require.Contains(t, logs.String(), "SEED_ADMINS_FILE unset")
}

func TestSeedAdminsRefusedOutsideLocalAndTest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admins.yaml")
	require.NoError(t, os.WriteFile(path, []byte(validAdminYAML), 0o600))
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	for _, env := range []string{"", "staging", "prod", "dev"} {
		withEnv(t, map[string]string{seedAdminsFileEnv: path, "APP_ENV": env})
		err := Run(ctx, config.EnvTest, Dev, Deps{DB: db.New(tdb.Pool), Keys: adminKeys(t)})
		require.ErrorContains(t, err, "refused", env)
	}
	var n int
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n))
	require.Zero(t, n, "the refusal rolls the whole seed back")
}

const validAdminYAML = `admins:
  - key: t.one
    id: 01KE0S1W6W8ZV7T1C3A6M9N091
    phone: "+919000000091"
    roles: [support]
`

func TestLoadSeedAdminsRefusesBadFiles(t *testing.T) {
	entry := func(key, id, phone, roles string) string {
		return "  - key: " + key + "\n    id: " + id + "\n    phone: \"" + phone + "\"\n    roles: " + roles + "\n"
	}
	ok := entry("t.one", "01KE0S1W6W8ZV7T1C3A6M9N091", "+919000000091", "[support]")
	cases := map[string]string{
		"empty":           "admins: []\n",
		"unknown field":   "admins:\n  - key: t.one\n    password: x\n",
		"not yaml":        "admins: [\n",
		"no key":          "admins:\n" + entry("", "01KE0S1W6W8ZV7T1C3A6M9N091", "+919000000091", "[support]"),
		"dup key":         "admins:\n" + ok + entry("t.one", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000092", "[support]"),
		"user key":        "admins:\n" + entry("customer.asha", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000092", "[support]"),
		"bad id":          "admins:\n" + entry("t.two", "not-a-ulid", "+919000000092", "[support]"),
		"id without N":    "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9Q092", "+919000000092", "[support]"),
		"id of a user":    "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N001", "+919000000092", "[support]"),
		"dup id":          "admins:\n" + ok + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N091", "+919000000092", "[support]"),
		"real phone":      "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919876543210", "[support]"),
		"phone of a user": "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000001", "[support]"),
		"dup phone":       "admins:\n" + ok + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000091", "[support]"),
		"no roles":        "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000092", "[]"),
		"business role":   "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000092", "[owner]"),
		"repeated role":   "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000092", "[support, support]"),
		// D-264: the authenticator is gone; a fixture still naming a
		// secret is refused (KnownFields), not silently ignored.
		"old totp_secret": "admins:\n" + ok[:len(ok)-1] + "\n    totp_secret: not-a-secret\n",
	}
	dir := t.TempDir()
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".yaml")
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
			_, err := loadSeedAdmins(path)
			require.Error(t, err)
			msg := err.Error()
			require.NotContains(t, msg, "GEZDGNBVGY3TQOJQ", "an error never quotes a value")
			require.NotContains(t, msg, "9876543210", "an error never quotes a phone")
		})
	}

	big := filepath.Join(dir, "big.yaml")
	require.NoError(t, os.WriteFile(big, bytes.Repeat([]byte("#"), maxSeedAdminsFile+1), 0o600))
	_, err := loadSeedAdmins(big)
	require.ErrorContains(t, err, "larger than")

	_, err = loadSeedAdmins(filepath.Join(dir, "missing.yaml"))
	require.Error(t, err)

	good := filepath.Join(dir, "good.yaml")
	require.NoError(t, os.WriteFile(good, []byte(validAdminYAML), 0o600))
	admins, err := loadSeedAdmins(good)
	require.NoError(t, err)
	require.Len(t, admins, 1)
	require.Equal(t, "en", admins[0].Lang, "lang defaults to en")
}

package seed

import (
	"bytes"
	"context"
	"crypto/rand"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/identity"
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
	k := testKeys(t)
	totp := make([]byte, 32)
	_, err := rand.Read(totp)
	require.NoError(t, err)
	k.TOTPEncKey = config.NewSecret(totp)
	return k
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

// TestRepoFixtureComposition checks the committed fixture against 08 §2: one
// enrolled admin per D-64 role, two each of finance and super_admin, and one
// unenrolled admin.
func TestRepoFixtureComposition(t *testing.T) {
	admins, err := loadSeedAdmins(fixture(t))
	require.NoError(t, err)
	enrolledBy := map[string]int{}
	unenrolled := 0
	for _, a := range admins {
		if a.TOTPSecret == "" {
			unenrolled++
			continue
		}
		for _, r := range a.Roles {
			enrolledBy[r]++
		}
	}
	require.Equal(t, 1, unenrolled)
	for _, r := range authz.AdminRoles {
		require.GreaterOrEqual(t, enrolledBy[string(r)], 1, r)
	}
	require.Equal(t, 2, enrolledBy[string(authz.Finance)])
	require.Equal(t, 2, enrolledBy[string(authz.SuperAdmin)])
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

	totpSeal, err := pii.NewSealer(keys.TOTPEncKey.Bytes())
	require.NoError(t, err)
	hash, err := pii.NewHasher(keys.PIIHashKey.Bytes())
	require.NoError(t, err)
	for _, a := range admins {
		var roles []string
		var enc []byte
		var enabled *string
		require.NoError(t, tdb.Pool.QueryRow(ctx,
			`SELECT roles, totp_secret_enc, totp_enabled_at::text FROM admin_users WHERE user_id = $1`,
			ids.UUID(mustID(a.ID))).Scan(&roles, &enc, &enabled), a.Key)
		require.Equal(t, a.Roles, roles, a.Key)

		var ph []byte
		require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT phone_hash FROM phone_identities WHERE user_id = $1`,
			ids.UUID(mustID(a.ID))).Scan(&ph), a.Key)
		require.Equal(t, hash.Phone(a.Phone), ph, "%s: sign-in finds the admin by phone", a.Key)

		if a.TOTPSecret == "" {
			require.Nil(t, enc, a.Key)
			require.Nil(t, enabled, a.Key)
			continue
		}
		require.NotNil(t, enabled, a.Key)
		want, err := identity.DecodeTOTPSecret(a.TOTPSecret)
		require.NoError(t, err)
		got, err := totpSeal.Open(identity.TOTPSealField, enc)
		require.NoError(t, err, "%s: sealed the way identity opens it", a.Key)
		require.Equal(t, want, got, a.Key)
		require.False(t, bytes.Contains(enc, want), "%s: stored sealed", a.Key)

		// Nothing secret reaches the log.
		require.NotContains(t, logs.String(), a.TOTPSecret)
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

func TestSeedAdminsWithoutTOTPKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admins.yaml")
	require.NoError(t, os.WriteFile(path, []byte(validAdminYAML), 0o600))
	withEnv(t, map[string]string{seedAdminsFileEnv: path, "APP_ENV": "local"})
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	var logs bytes.Buffer
	require.NoError(t, Run(ctx, config.EnvTest, Dev, Deps{DB: db.New(tdb.Pool), Keys: testKeys(t), Log: logTo(&logs)}))
	var n int
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&n))
	require.Zero(t, n)
	require.Contains(t, logs.String(), "TOTP_ENC_KEY unset")
}

// A made-up secret for the validation tests (base32 of 20 bytes).
const testSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" // gitleaks:allow (made-up test value)

const validAdminYAML = `admins:
  - key: t.one
    id: 01KE0S1W6W8ZV7T1C3A6M9N091
    phone: "+919000000091"
    roles: [support]
    totp_secret: ` + testSecret + `
`

func TestLoadSeedAdminsRefusesBadFiles(t *testing.T) {
	entry := func(key, id, phone, roles, secret string) string {
		return "  - key: " + key + "\n    id: " + id + "\n    phone: \"" + phone + "\"\n    roles: " + roles +
			"\n    totp_secret: \"" + secret + "\"\n"
	}
	ok := entry("t.one", "01KE0S1W6W8ZV7T1C3A6M9N091", "+919000000091", "[support]", testSecret)
	cases := map[string]string{
		"empty":            "admins: []\n",
		"unknown field":    "admins:\n  - key: t.one\n    password: x\n",
		"not yaml":         "admins: [\n",
		"no key":           "admins:\n" + entry("", "01KE0S1W6W8ZV7T1C3A6M9N091", "+919000000091", "[support]", testSecret),
		"dup key":          "admins:\n" + ok + entry("t.one", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000092", "[support]", ""),
		"user key":         "admins:\n" + entry("customer.asha", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000092", "[support]", ""),
		"bad id":           "admins:\n" + entry("t.two", "not-a-ulid", "+919000000092", "[support]", ""),
		"id without N":     "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9Q092", "+919000000092", "[support]", ""),
		"id of a user":     "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N001", "+919000000092", "[support]", ""),
		"dup id":           "admins:\n" + ok + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N091", "+919000000092", "[support]", ""),
		"real phone":       "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919876543210", "[support]", ""),
		"phone of a user":  "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000001", "[support]", ""),
		"dup phone":        "admins:\n" + ok + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000091", "[support]", ""),
		"no roles":         "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000092", "[]", ""),
		"business role":    "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000092", "[owner]", ""),
		"repeated role":    "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000092", "[support, support]", ""),
		"secret not b32":   "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000092", "[support]", "not!base32"),
		"secret too short": "admins:\n" + entry("t.two", "01KE0S1W6W8ZV7T1C3A6M9N092", "+919000000092", "[support]", "GEZDGNBVGY3TQOJQ"),
	}
	dir := t.TempDir()
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".yaml")
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
			_, err := loadSeedAdmins(path)
			require.Error(t, err)
			msg := err.Error()
			require.NotContains(t, msg, testSecret, "an error never quotes a secret")
			require.NotContains(t, msg, "9876543210", "an error never quotes a phone")
			require.NotContains(t, msg, "not!base32")
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

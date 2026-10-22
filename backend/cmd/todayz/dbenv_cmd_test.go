package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/testkit"
)

// TestDBEnvRefusals (W4): the database records the APP_ENV it was migrated
// for, and the commands that destroy or fake state refuse when it says
// staging or prod, says nothing, or disagrees with APP_ENV. Each refusal
// leaves the database untouched.
func TestDBEnvRefusals(t *testing.T) {
	db := testkit.NewEmptyDB(t)
	ctx := context.Background()
	e := localEnv(db) // APP_ENV=test

	code, _, stderr := runCmd([]string{"migrate", "up"}, e)
	require.Equal(t, 0, code, stderr)
	var env string
	require.NoError(t, db.OwnerPool.QueryRow(ctx, `SELECT env FROM platform_environment`).Scan(&env))
	require.Equal(t, "test", env, "migrate up recorded APP_ENV")

	// The app role can read the row but never write it.
	_, err := db.Pool.Exec(ctx, `UPDATE platform_environment SET env = 'local'`)
	require.ErrorContains(t, err, "permission denied")
	_, err = db.Pool.Exec(ctx, `DELETE FROM platform_environment`)
	require.ErrorContains(t, err, "permission denied")

	refused := func(t *testing.T, e map[string]string, args ...string) string {
		t.Helper()
		code, out, stderr := runCmd(args, e)
		require.Equal(t, 1, code, "%v ran: %s", args, out)
		require.Contains(t, stderr, "refused", args)
		return stderr
	}
	migrations := func() int {
		var n int
		require.NoError(t, db.OwnerPool.QueryRow(ctx, `SELECT count(*) FROM goose_db_version`).Scan(&n))
		return n
	}
	applied := migrations()

	// APP_ENV disagrees with the database.
	other := localEnv(db)
	other["APP_ENV"] = "local"
	refused(t, other, "migrate", "up")
	refused(t, other, "migrate", "down")
	refused(t, other, "seed", "dev")

	// The database says staging, then prod (an owner wrote it; a laptop with
	// APP_ENV=test pointed at that database).
	for _, deployed := range []string{"staging", "prod"} {
		_, err = db.OwnerPool.Exec(ctx, `UPDATE platform_environment SET env = $1`, deployed)
		require.NoError(t, err)
		require.Contains(t, refused(t, e, "migrate", "down"), "APP_ENV="+deployed)
		require.Contains(t, refused(t, e, "seed", "dev"), "APP_ENV="+deployed)
		refused(t, e, "migrate", "up")
	}

	// The database records nothing (migrated before the table existed).
	_, err = db.OwnerPool.Exec(ctx, `DELETE FROM platform_environment`)
	require.NoError(t, err)
	refused(t, e, "migrate", "down")
	refused(t, e, "seed", "dev")

	require.Equal(t, applied, migrations(), "no refusal rolled anything back")

	// migrate up on a database that records nothing records APP_ENV again.
	code, _, stderr = runCmd([]string{"migrate", "up"}, e)
	require.Equal(t, 0, code, stderr)
	require.NoError(t, db.OwnerPool.QueryRow(ctx, `SELECT env FROM platform_environment`).Scan(&env))
	require.Equal(t, "test", env)
}

// TestTestClockRefusedOnDeployedDB (W4): an APP_ENV=test api pointed at a
// database that says staging runs on the real clock, without the route.
func TestTestClockRefusedOnDeployedDB(t *testing.T) {
	db := testkit.NewDB(t) // migrated, records "test"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := db.OwnerPool.Exec(ctx, `UPDATE platform_environment SET env = 'staging'`)
	require.NoError(t, err)

	e := localEnv(db)
	addr := freeAddr(t)
	e["API_ADDR"] = addr
	var out syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"api"}, env(e), &out, &out) }()
	waitOK(t, "http://"+addr+"/readyz")

	resp, err := testHTTP.Get("http://" + addr + "/v1/test/clock")
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	cancel()
	require.Equal(t, 0, <-done, out.String())
	require.Contains(t, out.String(), `"test clock disabled"`)
	require.Contains(t, out.String(), "test clock refused")
}

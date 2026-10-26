package imageimport

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/storage"
	"github.com/todayz/backend/internal/platform/storage/storagetest"
	"github.com/todayz/backend/internal/seed"
	"github.com/todayz/backend/internal/testkit"
)

// TestLiveTrial is the one small trial against the real databases (D-252):
// at most 20 products of the dev seed, on a throwaway test database and the
// storage fake, never the local stack. It runs only when
// TODAYZ_IMAGE_TRIAL_DIR names a directory for the report and the copies;
// make check never sets it (and refuses egress anyway).
func TestLiveTrial(t *testing.T) {
	dir := os.Getenv("TODAYZ_IMAGE_TRIAL_DIR")
	if dir == "" {
		t.Skip("set TODAYZ_IMAGE_TRIAL_DIR to run the live trial")
	}
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	enc, hash := make([]byte, 32), make([]byte, 32)
	_, _ = rand.Read(enc)
	_, _ = rand.Read(hash)
	keys := config.AppKeys{PIIEncKey: config.NewSecret(enc), PIIHashKey: config.NewSecret(hash)}
	require.NoError(t, seed.Run(ctx, config.EnvTest, seed.Dev, seed.Deps{DB: db.New(tdb.Pool), Keys: keys,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}))

	fake := storagetest.New(t)
	im, err := New(Deps{
		DB: db.New(tdb.Pool), Store: storage.New(fake.Config(), fake.Client()),
		Client: httpx.NewOutboundClient(httpx.OutboundOptions{Timeout: 30 * time.Second}),
		Clock:  clock.Real{}, Log: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}, Options{UserAgent: UserAgent("trial", "https://todayz.in"), Limit: 20})
	require.NoError(t, err)
	rep, err := im.Run(ctx)
	require.NoError(t, err)

	f, err := os.Create(filepath.Join(dir, "trial.md")) //nolint:gosec // the operator's directory
	require.NoError(t, err)
	require.NoError(t, rep.WriteMarkdown(f))
	require.NoError(t, f.Close())
	// Keep the copies to look at before anyone approves them.
	rows, err := tdb.OwnerPool.Query(ctx, `SELECT pi.object_key, p.name FROM product_images pi JOIN products p ON p.id = pi.product_id`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var key, name string
		require.NoError(t, rows.Scan(&key, &name))
		body, ok := fake.Private(key)
		require.True(t, ok)
		require.NoError(t, os.WriteFile(filepath.Join(dir, filepath.Base(name)+filepath.Ext(key)), body, 0o600))
	}
	require.NoError(t, rows.Err())
}

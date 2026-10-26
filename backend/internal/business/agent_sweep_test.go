package business

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbschema "github.com/todayz/backend/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/testkit"
)

// sweepBucket is the private bucket: it answers DELETEs, failing the keys
// in fail, and records the keys deleted.
type sweepBucket struct {
	mu      sync.Mutex
	fail    map[string]bool
	deleted []string
}

func (b *sweepBucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := strings.TrimPrefix(r.URL.Path, "/private/")
	if r.Method != http.MethodDelete || b.fail[key] {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	b.deleted = append(b.deleted, key)
	w.WriteHeader(http.StatusNoContent)
}

// W3-06 sweep (backend risk fixes): rows are claimed, objects deleted after
// the claim commits, a failed delete is counted and backs off without
// blocking the rest, and is retried once its backoff has passed.
func TestSweepFieldPhotosDB(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	s, clk := leadService(t, tdb, nil)
	bucket := &sweepBucket{fail: map[string]bool{}}
	srv := httptest.NewServer(bucket)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	s.presign = &presigner{scheme: "http", host: u.Host, bucket: "private", accessKey: "k", secretKey: "s", client: srv.Client()}

	now := clk.Now()
	bid := ids.New(now)
	_, err = tdb.OwnerPool.Exec(ctx, `INSERT INTO businesses (id, brand_name, status, referral_code) VALUES ($1, 'Sweep Shop', 'draft', 'AAAAAC')`, ids.UUID(bid))
	require.NoError(t, err)
	doc := func(confirmBy *time.Time) (ids.ID, string) {
		id := ids.New(now)
		key := "businesses/" + bid.String() + "/documents/" + id.String()
		_, err := tdb.OwnerPool.Exec(ctx, `INSERT INTO business_documents (id, business_id, kind, object_key, content_type, size_bytes, confirm_by, created_at)
		  VALUES ($1, $2, 'storefront', $3, 'image/jpeg', 2048, $4, $5)`, ids.UUID(id), ids.UUID(bid), key, confirmBy, now)
		require.NoError(t, err)
		return id, key
	}
	row := func(id ids.ID) (status string, attempts int, retry *time.Time) {
		require.NoError(t, tdb.OwnerPool.QueryRow(ctx, `SELECT status, sweep_attempts, sweep_retry_at FROM business_documents WHERE id = $1`, ids.UUID(id)).Scan(&status, &attempts, &retry))
		return
	}
	past := now.Add(-time.Minute)
	stuck, stuckKey := doc(&past) // its delete fails
	okIDs := []ids.ID{}
	for range 3 {
		id, _ := doc(&past)
		okIDs = append(okIDs, id)
	}
	future := now.Add(30 * time.Minute)
	fresh, _ := doc(&future) // not stale yet
	owner, _ := doc(nil)     // an owner upload: never swept
	bucket.fail[stuckKey] = true

	res, err := s.SweepFieldPhotos(ctx)
	require.NoError(t, err)
	assert.Equal(t, SweepResult{Rejected: 3, Failed: 1}, res, "the failed delete does not block the others")
	for _, id := range okIDs {
		st, _, _ := row(id)
		assert.Equal(t, "rejected", st)
	}
	st, n, retry := row(stuck)
	assert.Equal(t, "pending", st)
	assert.Equal(t, 1, n)
	require.NotNil(t, retry)
	assert.WithinDuration(t, now.Add(sweepRetryBase), *retry, time.Second)
	for _, id := range []ids.ID{fresh, owner} {
		st, _, _ := row(id)
		assert.Equal(t, "pending", st)
	}

	// Before its backoff passes the failed row is not taken again.
	clk.Advance(sweepRetryBase - time.Minute)
	res, err = s.SweepFieldPhotos(ctx)
	require.NoError(t, err)
	assert.Equal(t, SweepResult{Rejected: 1}, res, "only the photo that went stale since")
	// Still failing after its backoff: the next one doubles.
	clk.Advance(time.Minute)
	res, err = s.SweepFieldPhotos(ctx)
	require.NoError(t, err)
	assert.Equal(t, SweepResult{Failed: 1}, res)
	_, n, retry = row(stuck)
	assert.Equal(t, 2, n)
	assert.WithinDuration(t, clk.Now().Add(2*sweepRetryBase), *retry, time.Second)
	// The bucket recovers: the row is rejected once its backoff passes.
	bucket.fail[stuckKey] = false
	clk.Advance(2 * sweepRetryBase)
	res, err = s.SweepFieldPhotos(ctx)
	require.NoError(t, err)
	assert.Equal(t, SweepResult{Rejected: 1}, res)
	st, _, _ = row(stuck)
	assert.Equal(t, "rejected", st)
	assert.Contains(t, bucket.deleted, stuckKey)
	st, _, _ = row(owner)
	assert.Equal(t, "pending", st, "owner uploads are never swept")
}

// A claimed row is skipped by a second sweep until its lease ends.
func TestSweepClaimLeaseDB(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	s, clk := leadService(t, tdb, nil)
	now := clk.Now()
	bid := ids.New(now)
	_, err := tdb.OwnerPool.Exec(ctx, `INSERT INTO businesses (id, brand_name, status, referral_code) VALUES ($1, 'Lease Shop', 'draft', 'AAAAAD')`, ids.UUID(bid))
	require.NoError(t, err)
	past := now.Add(-time.Minute)
	id := ids.New(now)
	_, err = tdb.OwnerPool.Exec(ctx, `INSERT INTO business_documents (id, business_id, kind, object_key, content_type, size_bytes, confirm_by, created_at)
	  VALUES ($1, $2, 'storefront', 'businesses/x/documents/lease', 'image/jpeg', 2048, $3, $4)`, ids.UUID(id), ids.UUID(bid), past, now)
	require.NoError(t, err)

	claim := func() int {
		rows, err := s.q(ctx).ClaimStaleFieldDocuments(ctx, claimParams(clk.Now()))
		require.NoError(t, err)
		return len(rows)
	}
	assert.Equal(t, 1, claim())
	assert.Equal(t, 0, claim(), "claimed: a second sweep skips it")
	clk.Advance(sweepLease)
	assert.Equal(t, 1, claim(), "a sweep that died leaves it to a later run")
}

// Migration 49802 sets confirm_by on the field photos left pending without
// one (created before 30603): those with an admin document.create audit row,
// matched on the ULID text of their id. Owner uploads keep it null.
func TestConfirmByBackfillDB(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	ctx := context.Background()
	tdb := testkit.NewDB(t)
	src, err := dbschema.Migrations.ReadFile("migrations/49802_w4-98_field_photo_confirm_by_backfill.sql")
	require.NoError(t, err)
	up := string(src)
	up = up[strings.Index(up, "-- +goose Up"):strings.Index(up, "-- +goose Down")]

	now := time.Date(2026, 10, 25, 6, 0, 0, 0, time.UTC)
	bid := ids.New(now)
	_, err = tdb.OwnerPool.Exec(ctx, `INSERT INTO businesses (id, brand_name, status, referral_code) VALUES ($1, 'Backfill Shop', 'draft', 'AAAAAE')`, ids.UUID(bid))
	require.NoError(t, err)
	doc := func(status, actor string) ids.ID {
		id := ids.New(now)
		_, err := tdb.OwnerPool.Exec(ctx, `INSERT INTO business_documents (id, business_id, kind, object_key, content_type, size_bytes, status, created_at)
		  VALUES ($1, $2, 'storefront', $3, 'image/jpeg', 2048, $4, $5)`, ids.UUID(id), ids.UUID(bid), "k/"+id.String(), status, now)
		require.NoError(t, err)
		_, err = tdb.OwnerPool.Exec(ctx, `INSERT INTO audit_log (id, actor_kind, action, subject_type, subject_id, created_at)
		  VALUES ($1, $2, 'document.create', 'business_document', $3, $4)`, ids.UUID(ids.New(now)), actor, id.String(), now)
		require.NoError(t, err)
		return id
	}
	confirmBy := func(id ids.ID) *time.Time {
		var c *time.Time
		require.NoError(t, tdb.OwnerPool.QueryRow(ctx, `SELECT confirm_by FROM business_documents WHERE id = $1`, ids.UUID(id)).Scan(&c))
		return c
	}
	field := []ids.ID{doc("pending", "admin"), doc("pending", "admin")}
	ownerUpload := doc("pending", "staff")
	confirmed := doc("uploaded", "admin")

	_, err = tdb.OwnerPool.Exec(ctx, up)
	require.NoError(t, err)
	for _, id := range field {
		c := confirmBy(id)
		require.NotNil(t, c, id.String())
		assert.True(t, c.Equal(now.Add(pendingLife)), "created_at plus pendingLife: %v", c)
	}
	assert.Nil(t, confirmBy(ownerUpload), "owner uploads keep it null")
	assert.Nil(t, confirmBy(confirmed), "only pending rows")
}

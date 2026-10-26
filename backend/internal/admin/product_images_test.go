package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	api "github.com/todayz/backend/gen/api/admin-catalogue"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/storage"
	"github.com/todayz/backend/internal/platform/storage/storagetest"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

var imgNow = time.Date(2026, 10, 26, 6, 30, 0, 0, time.UTC)

const catGrocery = "0190c47e-0000-7000-8000-000000000101"

type imageWorld struct {
	t     *testing.T
	tdb   *testkit.DB
	svc   *Service
	fake  *storagetest.Fake
	ctx   context.Context
	admin ids.ID
	store ids.ID
}

func newImageWorld(t *testing.T) *imageWorld {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	w := &imageWorld{t: t, tdb: tdb, fake: storagetest.New(t), ctx: context.Background(), admin: ids.New(imgNow), store: ids.New(imgNow)}
	biz := ids.New(imgNow)
	w.exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(w.admin))
	w.exec(`INSERT INTO businesses (id, brand_name, referral_code) VALUES ($1, 'B', 'ABCDEF')`, ids.UUID(biz))
	w.exec(`INSERT INTO stores (id, business_id, slug, short_code, name) VALUES ($1, $2, 's1', 'ABCDE2', 'S1')`, ids.UUID(w.store), ids.UUID(biz))
	clk := clock.NewFake(imgNow)
	d := db.New(tdb.Pool)
	var err error
	w.svc, err = New(Deps{DB: d, Clock: clk, Audit: audit.New(d, clk, nil), Storage: storage.New(w.fake.Config(), w.fake.Client())})
	require.NoError(t, err)
	return w
}

func (w *imageWorld) exec(sql string, args ...any) {
	w.t.Helper()
	_, err := w.tdb.OwnerPool.Exec(context.Background(), sql, args...)
	require.NoError(w.t, err, sql)
}

func (w *imageWorld) scalar(sql string, args ...any) any {
	w.t.Helper()
	var v any
	require.NoError(w.t, w.tdb.OwnerPool.QueryRow(context.Background(), sql, args...).Scan(&v), sql)
	return v
}

func (w *imageWorld) product(status string) ids.ID {
	id := ids.New(imgNow)
	w.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, status, created_by_store_id) VALUES ($1, 'Haldi', $2, 100, 'g', $3, $4)`,
		ids.UUID(id), catGrocery, status, ids.UUID(w.store))
	return id
}

// pending stores a waiting image of product, as a shop upload or an import
// leaves it.
func (w *imageWorld) pending(product ids.ID, source string) ids.ID {
	id := ids.New(imgNow)
	key := "product-images/" + product.String() + "/" + id.String() + ".jpg"
	w.fake.PutPrivate(key, "image/jpeg", []byte("jpeg "+id.String()))
	if source == "retailer" {
		w.exec(`INSERT INTO product_images (id, product_id, source, status, object_key, content_type, size_bytes, store_id)
		  VALUES ($1, $2, 'retailer', 'pending', $3, 'image/jpeg', 31, $4)`, ids.UUID(id), ids.UUID(product), key, ids.UUID(w.store))
	} else {
		w.exec(`INSERT INTO product_images (id, product_id, source, status, object_key, content_type, size_bytes,
		    source_ref, source_url, page_url, licence, attribution, match)
		  VALUES ($1, $2, $3, 'pending', $4, 'image/jpeg', 31, '8901063010390',
		    'https://images.openfoodfacts.org/images/products/890/106/301/0390/front_en.3.400.jpg',
		    'https://world.openfoodfacts.org/product/8901063010390', 'CC BY-SA 3.0', 'Open Food Facts contributors', 'barcode')`,
			ids.UUID(id), ids.UUID(product), source, key)
	}
	return id
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var p *problem.Error
	require.True(t, errors.As(err, &p), "not a problem: %v", err)
	return p.Code
}

func TestImageReviewApproveReplaceTakedown(t *testing.T) {
	w := newImageWorld(t)
	prod := w.product("active")
	first := w.pending(prod, "off")

	list, err := w.svc.listImages(w.ctx, api.ListAdminProductImagesParams{})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	got := list.Items[0]
	require.Equal(t, first.String(), got.Id)
	require.NotNil(t, got.PreviewUrl)
	require.Equal(t, "CC BY-SA 3.0", *got.Licence)
	require.Equal(t, "barcode", *got.Match)

	img, err := w.svc.approveImage(w.ctx, w.admin, first, nil)
	require.NoError(t, err)
	require.Equal(t, api.AdminProductImageStatus("approved"), img.Status)
	require.NotNil(t, img.ImageKey)
	firstKey := *img.ImageKey
	body, ct, ok := w.fake.Image(firstKey)
	require.True(t, ok, "published to the images bucket")
	require.Equal(t, "image/jpeg", ct)
	require.Equal(t, "jpeg "+first.String(), string(body))
	require.Equal(t, firstKey, w.scalar(`SELECT image_key FROM products WHERE id = $1`, ids.UUID(prod)))
	require.EqualValues(t, 1, w.scalar(`SELECT count(*) FROM audit_log WHERE action = 'catalogue.image.approve' AND subject_type = 'product' AND subject_id = $1`, prod.String()))

	// Approving twice is refused.
	_, err = w.svc.approveImage(w.ctx, w.admin, first, nil)
	require.Equal(t, genproblems.InvalidTransition, codeOf(t, err))

	// A second approval replaces the first; the old copy stays published.
	second := w.pending(prod, "retailer")
	img, err = w.svc.approveImage(w.ctx, w.admin, second, nil)
	require.NoError(t, err)
	require.Equal(t, "replaced", w.scalar(`SELECT status FROM product_images WHERE id = $1`, ids.UUID(first)))
	secondKey := *img.ImageKey
	require.Equal(t, secondKey, w.scalar(`SELECT image_key FROM products WHERE id = $1`, ids.UUID(prod)))
	_, _, ok = w.fake.Image(firstKey)
	require.True(t, ok)

	// Rejecting the approved image takes it down.
	img, err = w.svc.rejectImage(w.ctx, w.admin, second, api.AdminProductImageReject{Reason: "Not this product"})
	require.NoError(t, err)
	require.Equal(t, api.AdminProductImageStatus("rejected"), img.Status)
	require.Nil(t, w.scalar(`SELECT image_key FROM products WHERE id = $1`, ids.UUID(prod)))
	require.Nil(t, w.scalar(`SELECT image_id FROM products WHERE id = $1`, ids.UUID(prod)))
	require.Nil(t, img.ImageKey)
	_, _, ok = w.fake.Image(secondKey)
	require.False(t, ok, "the public copy is deleted")

	// A rejected image cannot be rejected again.
	_, err = w.svc.rejectImage(w.ctx, w.admin, second, api.AdminProductImageReject{Reason: "again"})
	require.Equal(t, genproblems.InvalidTransition, codeOf(t, err))
}

func TestImageBulkApprove(t *testing.T) {
	w := newImageWorld(t)
	a, b := w.product("active"), w.product("active")
	ia, ib := w.pending(a, "off"), w.pending(b, "obf")
	gone := w.product("retired")
	ig := w.pending(gone, "off")

	res, err := w.svc.approveImages(w.ctx, w.admin, api.AdminProductImageBulkApprove{
		ImageIds: []string{ia.String(), ib.String(), ia.String(), ig.String(), "nope", ids.New(imgNow).String()},
	})
	require.NoError(t, err)
	require.Len(t, res.Approved, 2)
	require.Len(t, res.Failed, 3)
	codes := map[string]string{}
	for _, f := range res.Failed {
		codes[f.ImageId] = f.Code
	}
	require.Equal(t, genproblems.InvalidTransition, codes[ig.String()], "a retired product takes no image")
	require.Equal(t, genproblems.NotFound, codes["nope"])
	require.Equal(t, "pending", w.scalar(`SELECT status FROM product_images WHERE id = $1`, ids.UUID(ig)))

	_, err = w.svc.approveImages(w.ctx, w.admin, api.AdminProductImageBulkApprove{ImageIds: []string{}})
	require.Equal(t, genproblems.ValidationFailed, codeOf(t, err))
}

func TestAdminImageUpload(t *testing.T) {
	w := newImageWorld(t)
	prod := w.product("active")
	body := []byte("\x89PNG an admin photo")

	up, err := w.svc.createImage(w.ctx, w.admin, prod, api.AdminProductImageCreate{ContentType: "image/png", SizeBytes: int64(len(body))})
	require.NoError(t, err)
	require.Equal(t, api.AdminProductImageStatus("uploading"), up.Image.Status)
	id, err := ids.Parse(up.Image.Id)
	require.NoError(t, err)

	_, err = w.svc.confirmImage(w.ctx, w.admin, id, api.AdminProductImageApprove{})
	require.Equal(t, genproblems.ValidationFailed, codeOf(t, err), "nothing uploaded yet")

	w.fake.PutURL(up.Upload.Url, "image/png", body)
	img, err := w.svc.confirmImage(w.ctx, w.admin, id, api.AdminProductImageApprove{})
	require.NoError(t, err)
	require.Equal(t, api.AdminProductImageStatus("approved"), img.Status)
	got, ct, ok := w.fake.Image(*img.ImageKey)
	require.True(t, ok)
	require.Equal(t, "image/png", ct)
	require.Equal(t, body, got)
	require.Equal(t, *img.ImageKey, w.scalar(`SELECT image_key FROM products WHERE id = $1`, ids.UUID(prod)))
	require.EqualValues(t, 1, w.scalar(`SELECT count(*) FROM audit_log WHERE action = 'catalogue.image.upload'`))

	_, err = w.svc.confirmImage(w.ctx, w.admin, id, api.AdminProductImageApprove{})
	require.Equal(t, genproblems.InvalidTransition, codeOf(t, err))

	// A pending import is not an admin upload to confirm.
	imp := w.pending(prod, "off")
	_, err = w.svc.confirmImage(w.ctx, w.admin, imp, api.AdminProductImageApprove{})
	require.Equal(t, genproblems.NotFound, codeOf(t, err))
}

func TestPendingDecisionsCloseImages(t *testing.T) {
	w := newImageWorld(t)
	rejected := w.product("pending_review")
	ir := w.pending(rejected, "retailer")
	_, err := w.svc.rejectPending(w.ctx, w.admin, rejected, api.AdminPendingReject{Reason: "Duplicate listing"})
	require.NoError(t, err)
	require.Equal(t, "rejected", w.scalar(`SELECT status FROM product_images WHERE id = $1`, ids.UUID(ir)))

	merged, into := w.product("pending_review"), w.product("active")
	im := w.pending(merged, "retailer")
	_, err = w.svc.mergePending(w.ctx, w.admin, merged, api.AdminPendingMerge{IntoProductId: into.String()})
	require.NoError(t, err)
	require.Equal(t, "rejected", w.scalar(`SELECT status FROM product_images WHERE id = $1`, ids.UUID(im)))
}

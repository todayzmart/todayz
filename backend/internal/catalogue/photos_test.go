package catalogue

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	catalogueapi "github.com/todayz/backend/gen/api/catalogue"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/storage"
	"github.com/todayz/backend/internal/platform/storage/storagetest"
	"github.com/todayz/backend/internal/testkit"
)

func problemCode(t *testing.T, err error) string {
	t.Helper()
	var p *problem.Error
	require.True(t, errors.As(err, &p), "not a problem: %v", err)
	return p.Code
}

// photoWorld is a store that created a pending product and lists it, a
// second store of the same business, and the storage fake.
type photoWorld struct {
	t                 *testing.T
	tdb               *testkit.DB
	svc               *Service
	fake              *storagetest.Fake
	ctx               context.Context
	biz, store, other ids.ID
	product, item     ids.ID
}

func newPhotoWorld(t *testing.T) *photoWorld {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	fake := storagetest.New(t)
	gen := ids.NewGenerator()
	w := &photoWorld{t: t, tdb: tdb, fake: fake, biz: gen.New(t0), store: gen.New(t0), other: gen.New(t0), product: gen.New(t0), item: gen.New(t0)}
	user := gen.New(t0)
	w.ctx = authn.WithPrincipal(context.Background(), authn.Principal{UserID: user, App: authn.AppBusiness})
	w.exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(user))
	w.exec(`INSERT INTO businesses (id, brand_name, referral_code) VALUES ($1, 'B', 'ABCDEF')`, ids.UUID(w.biz))
	w.exec(`INSERT INTO stores (id, business_id, slug, short_code, name) VALUES ($1, $3, 's1', 'ABCDE2', 'S1'), ($2, $3, 's2', 'ABCDE3', 'S2')`,
		ids.UUID(w.store), ids.UUID(w.other), ids.UUID(w.biz))
	w.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, status, created_by_store_id)
	  VALUES ($1, 'Haldi powder', '0190c47e-0000-7000-8000-000000000101', 100, 'g', 'pending_review', $2)`, ids.UUID(w.product), ids.UUID(w.store))
	w.exec(`INSERT INTO store_items (id, store_id, product_id, price_paise) VALUES ($1, $2, $3, 4500)`, ids.UUID(w.item), ids.UUID(w.store), ids.UUID(w.product))
	var err error
	w.svc, err = New(Deps{
		DB: db.New(tdb.Pool), Clock: clock.NewFake(t0),
		Settings: settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{}),
		Storage:  storage.New(fake.Config(), fake.Client()),
	})
	require.NoError(t, err)
	return w
}

func (w *photoWorld) exec(sql string, args ...any) {
	w.t.Helper()
	_, err := w.tdb.OwnerPool.Exec(context.Background(), sql, args...)
	require.NoError(w.t, err, sql)
}

func (w *photoWorld) create(store ids.ID, body []byte) (catalogueapi.ProductPhotoUpload, error) {
	return w.svc.createPhoto(w.ctx, w.biz.String(), store.String(), w.product.String(),
		&catalogueapi.ProductPhotoCreate{ContentType: "image/jpeg", SizeBytes: int64(len(body))})
}

func TestProductPhotoFlow(t *testing.T) {
	w := newPhotoWorld(t)
	body := []byte("\xff\xd8\xff a small jpeg")

	up, err := w.create(w.store, body)
	require.NoError(t, err)
	require.Equal(t, catalogueapi.ProductPhotoStatus("uploading"), up.Photo.Status)
	require.Nil(t, up.Photo.PreviewUrl, "nothing to preview before the upload")
	require.Equal(t, catalogueapi.PUT, up.Upload.Method)
	require.Equal(t, "image/jpeg", string(up.Upload.Headers.ContentType))

	// Confirming before the file is there is refused on upload.
	_, err = w.svc.confirmPhoto(w.ctx, w.biz.String(), w.store.String(), w.product.String(), up.Photo.Id)
	require.Equal(t, genproblems.ValidationFailed, problemCode(t, err))

	w.fake.PutURL(up.Upload.Url, "image/jpeg", body)
	got, err := w.svc.confirmPhoto(w.ctx, w.biz.String(), w.store.String(), w.product.String(), up.Photo.Id)
	require.NoError(t, err)
	require.Equal(t, catalogueapi.ProductPhotoStatus("pending"), got.Status)
	require.NotNil(t, got.PreviewUrl)

	// A second confirm answers the photo as it is.
	again, err := w.svc.confirmPhoto(w.ctx, w.biz.String(), w.store.String(), w.product.String(), up.Photo.Id)
	require.NoError(t, err)
	require.Equal(t, got.Status, again.Status)

	// The shop sees its waiting photo on the item; the product has no image yet.
	it, _, err := w.svc.item(w.ctx, w.store, w.item)
	require.NoError(t, err)
	require.Nil(t, it.ImageKey)
	require.NotNil(t, it.Photo)
	require.Equal(t, catalogueapi.StoreItemPhotoStatus("pending"), it.Photo.Status)
	require.Equal(t, up.Photo.Id, it.Photo.Id)

	// Turned down: the shop sees why.
	w.exec(`UPDATE product_images SET status = 'rejected', reject_reason = 'Blurred' WHERE product_id = $1`, ids.UUID(w.product))
	it, _, err = w.svc.item(w.ctx, w.store, w.item)
	require.NoError(t, err)
	require.Equal(t, catalogueapi.StoreItemPhotoStatus("rejected"), it.Photo.Status)
	require.Equal(t, "Blurred", *it.Photo.RejectReason)
}

func TestProductPhotoRefusals(t *testing.T) {
	w := newPhotoWorld(t)

	// Only the store that created the product adds its photo.
	_, err := w.create(w.other, []byte("x"))
	require.Equal(t, genproblems.NotFound, problemCode(t, err))

	// Size and type limits.
	_, err = w.svc.createPhoto(w.ctx, w.biz.String(), w.store.String(), w.product.String(),
		&catalogueapi.ProductPhotoCreate{ContentType: "image/jpeg", SizeBytes: photoMaxBytes + 1})
	require.Equal(t, genproblems.ValidationFailed, problemCode(t, err))
	_, err = w.svc.createPhoto(w.ctx, w.biz.String(), w.store.String(), w.product.String(),
		&catalogueapi.ProductPhotoCreate{ContentType: "image/gif", SizeBytes: 10})
	require.Equal(t, genproblems.ValidationFailed, problemCode(t, err))

	// The uploaded file must be what was declared.
	up, err := w.create(w.store, []byte("12345"))
	require.NoError(t, err)
	w.fake.PutURL(up.Upload.Url, "image/jpeg", []byte("1234"))
	_, err = w.svc.confirmPhoto(w.ctx, w.biz.String(), w.store.String(), w.product.String(), up.Photo.Id)
	require.Equal(t, genproblems.ValidationFailed, problemCode(t, err))

	// Another store cannot confirm it.
	_, err = w.svc.confirmPhoto(w.ctx, w.biz.String(), w.other.String(), w.product.String(), up.Photo.Id)
	require.Equal(t, genproblems.NotFound, problemCode(t, err))

	// A merged product takes no photo.
	w.exec(`UPDATE products SET status = 'retired' WHERE id = $1`, ids.UUID(w.product))
	_, err = w.create(w.store, []byte("x"))
	require.Equal(t, genproblems.InvalidTransition, problemCode(t, err))

	// Without storage the route is unavailable.
	w.svc.storage = nil
	_, err = w.create(w.store, []byte("x"))
	require.Equal(t, genproblems.ServiceUnavailable, problemCode(t, err))
}

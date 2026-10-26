package catalogue

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	catalogueapi "github.com/todayz/backend/gen/api/catalogue"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/cataloguedb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/storage"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// A shop's photo of a product it created (D-252). The app resizes the photo,
// asks for a presigned PUT into the private bucket, uploads it and confirms;
// the photo then waits in the admin image queue (internal/admin) and only an
// approval copies it to the public images bucket and makes it the product's
// image. Until then the shop sees its own photo through a signed link.

// photoMaxBytes is the contract's limit on a product photo (ProductPhotoCreate).
const photoMaxBytes = 5 << 20

var errNoStorage = errors.New("catalogue: storage not configured")

// PhotoKey is the private-bucket key of a product photo draft.
func PhotoKey(productID, imageID ids.ID, contentType string) string {
	return "product-images/" + productID.String() + "/" + imageID.String() + PhotoExt(contentType)
}

// PhotoExt is the file extension for an image content type.
func PhotoExt(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

func (s *Service) storageReady(ctx context.Context) error {
	if s.storage == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "catalogue: product photos need storage")
		return problem.Wrap(genproblems.ServiceUnavailable, errNoStorage)
	}
	return nil
}

func (s *Service) createPhoto(ctx context.Context, bid, sid, productID string, b *catalogueapi.ProductPhotoCreate) (catalogueapi.ProductPhotoUpload, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.ProductPhotoUpload{}, err
	}
	if err := s.storageReady(ctx); err != nil {
		return catalogueapi.ProductPhotoUpload{}, err
	}
	sc, err := scope(bid, sid)
	if err != nil {
		return catalogueapi.ProductPhotoUpload{}, err
	}
	pid, err := pathID(productID)
	if err != nil {
		return catalogueapi.ProductPhotoUpload{}, err
	}
	if b == nil {
		return catalogueapi.ProductPhotoUpload{}, problem.New(genproblems.BadRequest, "")
	}
	p, err := principal(ctx)
	if err != nil {
		return catalogueapi.ProductPhotoUpload{}, err
	}
	if !b.ContentType.Valid() {
		return catalogueapi.ProductPhotoUpload{}, fieldErr("body.contentType", "not a supported image type")
	}
	if b.SizeBytes < 1 || b.SizeBytes > photoMaxBytes {
		return catalogueapi.ProductPhotoUpload{}, fieldErr("body.sizeBytes", "must be between 1 byte and 5 MB")
	}
	q := s.q(ctx)
	prod, err := q.GetProductForPhoto(ctx, ids.UUID(pid))
	if db.IsNoRows(err) {
		return catalogueapi.ProductPhotoUpload{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return catalogueapi.ProductPhotoUpload{}, fmt.Errorf("catalogue: photo product: %w", err)
	}
	// Only the store that created the product adds its photo; to anyone else
	// the product is not theirs to change.
	if creator, _ := ids.FromUUID(prod.CreatedByStoreID); !prod.CreatedByStoreID.Valid || creator != sc.StoreID {
		return catalogueapi.ProductPhotoUpload{}, problem.New(genproblems.NotFound, "")
	}
	if prod.Status == "merged" || prod.Status == "retired" {
		return catalogueapi.ProductPhotoUpload{}, problem.New(genproblems.InvalidTransition, "the product was merged or retired")
	}
	now := s.clock.Now()
	id := s.ids.New(now)
	ct := string(b.ContentType)
	key := PhotoKey(pid, id, ct)
	row, err := q.InsertProductPhoto(ctx, cataloguedb.InsertProductPhotoParams{
		ID: ids.UUID(id), ProductID: ids.UUID(pid), ObjectKey: key, ContentType: ct,
		SizeBytes: ptr(int32(b.SizeBytes)), StoreID: ids.UUID(sc.StoreID), UploadedBy: ids.UUID(p.UserID), Now: now, //nolint:gosec // at most 5 MB
	})
	if err != nil {
		return catalogueapi.ProductPhotoUpload{}, fmt.Errorf("catalogue: insert photo: %w", err)
	}
	return catalogueapi.ProductPhotoUpload{
		Photo: s.photoOut(row, now),
		Upload: catalogueapi.ProductPhotoUploadTarget{
			Url:    s.storage.PutURL(key, ct, b.SizeBytes, now),
			Method: catalogueapi.PUT,
			Headers: catalogueapi.ProductPhotoUploadHeaders{
				ContentType: b.ContentType, ContentLength: strconv.FormatInt(b.SizeBytes, 10),
			},
			ExpiresAt: now.Add(storage.URLTTL).UTC(),
		},
	}, nil
}

func (s *Service) confirmPhoto(ctx context.Context, bid, sid, productID, photoID string) (catalogueapi.ProductPhoto, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.ProductPhoto{}, err
	}
	if err := s.storageReady(ctx); err != nil {
		return catalogueapi.ProductPhoto{}, err
	}
	sc, err := scope(bid, sid)
	if err != nil {
		return catalogueapi.ProductPhoto{}, err
	}
	pid, err := pathID(productID)
	if err != nil {
		return catalogueapi.ProductPhoto{}, err
	}
	id, err := pathID(photoID)
	if err != nil {
		return catalogueapi.ProductPhoto{}, err
	}
	now := s.clock.Now()
	var out photo
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := cataloguedb.New(s.db.Q(ctx))
		row, err := q.LockProductPhoto(ctx, cataloguedb.LockProductPhotoParams{ID: ids.UUID(id), ProductID: ids.UUID(pid), StoreID: ids.UUID(sc.StoreID)})
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return fmt.Errorf("catalogue: lock photo: %w", err)
		}
		out = photo(row)
		if row.Status != "uploading" {
			return nil // confirmed before: answer it as it is
		}
		obj, err := s.storage.Head(ctx, row.ObjectKey, now)
		if errors.Is(err, storage.ErrNoObject) {
			return fieldErr("upload", "the file was not uploaded")
		}
		if err != nil {
			return fmt.Errorf("catalogue: head photo: %w", err)
		}
		if obj.ContentType != row.ContentType || obj.Size < 1 || obj.Size > photoMaxBytes ||
			(row.SizeBytes != nil && obj.Size != int64(*row.SizeBytes)) {
			return fieldErr("upload", "the uploaded file does not match the declared type and size")
		}
		done, err := q.MarkProductPhotoPending(ctx, cataloguedb.MarkProductPhotoPendingParams{
			ID: row.ID, SizeBytes: ptr(int32(obj.Size)), Now: now, //nolint:gosec // at most 5 MB
		})
		if err != nil {
			return fmt.Errorf("catalogue: confirm photo: %w", err)
		}
		out = photo(done)
		return nil
	})
	if err != nil {
		return catalogueapi.ProductPhoto{}, err
	}
	return s.photoOut(out, now), nil
}

// photo is a product_images row as the shop sees it.
type photo = cataloguedb.InsertProductPhotoRow

func (s *Service) photoOut(r photo, now time.Time) catalogueapi.ProductPhoto {
	out := catalogueapi.ProductPhoto{
		Id: ulidOf(r.ID), ProductId: ulidOf(r.ProductID), Status: catalogueapi.ProductPhotoStatus(r.Status),
		ContentType: catalogueapi.ProductPhotoContentType(r.ContentType), RejectReason: r.RejectReason,
		CreatedAt: r.CreatedAt.UTC(),
	}
	if r.Status != "uploading" && s.storage != nil {
		out.PreviewUrl = ptr(s.storage.GetURL(r.ObjectKey, now))
	}
	return out
}

// itemPhoto is the store's own photo on a store item: shown while it waits
// for review or was turned down, never once it is (or was) the image.
func (s *Service) itemPhoto(r cataloguedb.GetStoreItemRow, now time.Time) *catalogueapi.StoreItemPhoto {
	if !r.PhotoID.Valid || (r.PhotoStatus != "pending" && r.PhotoStatus != "rejected") || s.storage == nil {
		return nil
	}
	return &catalogueapi.StoreItemPhoto{
		Id: ulidOf(r.PhotoID), Status: catalogueapi.StoreItemPhotoStatus(r.PhotoStatus),
		PreviewUrl: s.storage.GetURL(r.PhotoKey, now), RejectReason: r.PhotoRejectReason,
	}
}

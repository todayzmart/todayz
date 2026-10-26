package admin

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	api "github.com/todayz/backend/gen/api/admin-catalogue"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/storage"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Product images (D-252). A shop's photo and an imported stock image wait as
// `pending` in the private bucket; approving one copies it to the public
// images bucket and makes it the product's image (the one before becomes
// `replaced`). Rejecting a waiting image turns it down; rejecting the
// approved one takes it down and the product goes back to the placeholder.
// An admin's own upload is approved when it is confirmed.

const (
	imageMaxBytes = 5 << 20 // the contract's limit (AdminProductImageCreate)
	bulkMax       = 50      //settings:ok contract bound (AdminProductImageBulkApprove.imageIds ≤ 50)

	imgUploading = "uploading"
	imgPending   = "pending"
	imgApproved  = "approved"
)

var errNoStorage = errors.New("admin: storage not configured")

func (s *Service) storageReady(ctx context.Context) error {
	if !s.storage.HasImages() {
		telemetry.Logger(ctx).ErrorContext(ctx, "admin: product images need the private and images buckets")
		return problem.Wrap(genproblems.ServiceUnavailable, errNoStorage)
	}
	return nil
}

func imageExt(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

// publicImageKey is where an approved image lives in the images bucket.
func publicImageKey(productID, imageID ids.ID, contentType string) string {
	return "products/" + productID.String() + "/" + imageID.String() + imageExt(contentType)
}

func (s *Service) imageOut(r admindb.ListProductImagesRow) api.AdminProductImage {
	out := api.AdminProductImage{
		Id: idString(r.ID), ProductId: idString(r.ProductID), ProductName: r.ProductName, BrandName: r.BrandName,
		CategoryName: r.CategoryName, ProductStatus: api.AdminProductStatus(r.ProductStatus),
		ProductImageKey: r.ProductImageKey, Source: api.AdminProductImageSource(r.Source),
		Status: api.AdminProductImageStatus(r.Status), ContentType: api.AdminProductImageContentType(r.ContentType),
		ImageKey: r.PublicKey, SourceRef: r.SourceRef, SourceUrl: r.SourceUrl, PageUrl: r.PageUrl,
		Licence: r.Licence, Attribution: r.Attribution, Match: r.Match, StoreId: optID(r.StoreID),
		StoreName: r.StoreName, RejectReason: r.RejectReason, CreatedAt: r.CreatedAt.UTC(),
	}
	if r.SizeBytes != nil {
		n := int(*r.SizeBytes)
		out.SizeBytes = &n
	}
	if r.DecidedAt != nil {
		t := r.DecidedAt.UTC()
		out.DecidedAt = &t
	}
	if r.Status != imgUploading && s.storage != nil {
		u := s.storage.GetURL(r.ObjectKey, s.now())
		out.PreviewUrl = &u
	}
	return out
}

func (s *Service) listImages(ctx context.Context, p api.ListAdminProductImagesParams) (api.AdminProductImageList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminProductImageList{}, err
	}
	lim, at, after, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return api.AdminProductImageList{}, err
	}
	args := admindb.ListProductImagesParams{Status: imgPending, AfterAt: at, AfterID: after, Lim: int32(lim + 1)} // #nosec G115 -- lim is clamped to [1, maxLimit]
	if p.Status != nil {
		args.Status = string(*p.Status)
	}
	if p.Source != nil {
		v := string(*p.Source)
		args.Source = &v
	}
	if p.ProductId != nil {
		pid, err := ids.Parse(*p.ProductId)
		if err != nil {
			return api.AdminProductImageList{}, fieldErr("query.productId", "not a product id")
		}
		args.ProductID = ids.UUID(pid)
	}
	rows, err := s.q(ctx).ListProductImages(ctx, args)
	if err != nil {
		return api.AdminProductImageList{}, err
	}
	out := api.AdminProductImageList{Items: []api.AdminProductImage{}, ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			last := rows[lim-1]
			lid, _ := ids.FromUUID(last.ID)
			c := encodeCursor(last.CreatedAt, lid)
			out.NextCursor = &c
			break
		}
		out.Items = append(out.Items, s.imageOut(r))
	}
	return out, nil
}

func (s *Service) image(ctx context.Context, id ids.ID) (api.AdminProductImage, error) {
	r, err := s.q(ctx).GetProductImage(ctx, ids.UUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminProductImage{}, notFound()
	}
	if err != nil {
		return api.AdminProductImage{}, err
	}
	return s.imageOut(admindb.ListProductImagesRow(r)), nil
}

// publish copies a locked image to the images bucket and makes it its
// product's image, inside the caller's transaction. It returns the audit
// before/after.
func (s *Service) publish(ctx context.Context, q *admindb.Queries, actor ids.ID, img admindb.LockProductImageRow, size *int32) (map[string]any, map[string]any, error) {
	prod, err := q.LockProductForImage(ctx, img.ProductID)
	if err != nil {
		return nil, nil, err
	}
	if prod.Status == prodMerged || prod.Status == prodRetired {
		return nil, nil, invalidTransition("the product was merged or retired")
	}
	imageID, _ := ids.FromUUID(img.ID)
	productID, _ := ids.FromUUID(img.ProductID)
	key := publicImageKey(productID, imageID, img.ContentType)
	err = s.storage.Publish(ctx, img.ObjectKey, key, img.ContentType, imageMaxBytes, s.now())
	if errors.Is(err, storage.ErrNoObject) {
		return nil, nil, invalidTransition("the stored file is missing")
	}
	if errors.Is(err, storage.ErrTooLarge) {
		return nil, nil, invalidTransition("the stored file is larger than 5 MB")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("admin: publish image: %w", err)
	}
	now := s.now()
	if err := q.ReplaceApprovedImage(ctx, admindb.ReplaceApprovedImageParams{ProductID: img.ProductID, Now: now}); err != nil {
		return nil, nil, err
	}
	if err := q.ApproveProductImage(ctx, admindb.ApproveProductImageParams{
		ID: img.ID, PublicKey: &key, SizeBytes: size, By: ids.UUID(actor), Now: now,
	}); err != nil {
		return nil, nil, err
	}
	if err := q.SetProductImage(ctx, admindb.SetProductImageParams{ID: img.ProductID, ImageID: img.ID, ImageKey: &key, Now: now}); err != nil {
		return nil, nil, err
	}
	before := map[string]any{"status": img.Status, "productImageKey": prod.ImageKey}
	after := map[string]any{"status": imgApproved, "productImageKey": key, "source": img.Source, "productId": productID.String()}
	return before, after, nil
}

// imageChange locks one image and runs change in a transaction with its
// audit entry on the image's product.
func (s *Service) imageChange(ctx context.Context, actor, id ids.ID, action string, reason *string,
	change func(ctx context.Context, q *admindb.Queries, img admindb.LockProductImageRow) (before, after map[string]any, err error),
) error {
	return s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		img, err := q.LockProductImage(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		before, after, err := change(ctx, q, img)
		if err != nil {
			return err
		}
		if reason != nil {
			after["reason"] = *reason
		}
		// The decision shows in the product's history (A-08), naming the image.
		after["imageId"] = id.String()
		pid, _ := ids.FromUUID(img.ProductID)
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: action,
			SubjectType: "product", SubjectID: pid.String(), Before: before, After: after,
		})
		return err
	})
}

func (s *Service) imageReady(ctx context.Context) error {
	if err := s.writeReady(ctx); err != nil {
		return err
	}
	return s.storageReady(ctx)
}

func (s *Service) approveImage(ctx context.Context, actor, id ids.ID, reason *string) (api.AdminProductImage, error) {
	if err := s.imageReady(ctx); err != nil {
		return api.AdminProductImage{}, err
	}
	err := s.imageChange(ctx, actor, id, "catalogue.image.approve", reason,
		func(ctx context.Context, q *admindb.Queries, img admindb.LockProductImageRow) (map[string]any, map[string]any, error) {
			if img.Status != imgPending {
				return nil, nil, invalidTransition("the image is not waiting for review")
			}
			return s.publish(ctx, q, actor, img, nil)
		})
	if err != nil {
		return api.AdminProductImage{}, err
	}
	return s.image(ctx, id)
}

func (s *Service) approveImageBody(ctx context.Context, actor, id ids.ID, body api.AdminProductImageApprove) (api.AdminProductImage, error) {
	reason, err := optReason(body.Reason)
	if err != nil {
		return api.AdminProductImage{}, err
	}
	return s.approveImage(ctx, actor, id, reason)
}

// approveImages approves each image on its own; one that cannot be approved
// is listed with its problem code and the rest go ahead.
func (s *Service) approveImages(ctx context.Context, actor ids.ID, body api.AdminProductImageBulkApprove) (api.AdminProductImageBulkResult, error) {
	reason, err := optReason(body.Reason)
	if err != nil {
		return api.AdminProductImageBulkResult{}, err
	}
	if len(body.ImageIds) == 0 || len(body.ImageIds) > bulkMax {
		return api.AdminProductImageBulkResult{}, fieldErr("body.imageIds", "1 to 50 image ids")
	}
	if err := s.imageReady(ctx); err != nil {
		return api.AdminProductImageBulkResult{}, err
	}
	out := api.AdminProductImageBulkResult{Approved: []api.AdminProductImage{}, Failed: []api.AdminProductImageBulkFailure{}}
	seen := map[string]bool{}
	for _, raw := range body.ImageIds {
		if seen[raw] {
			continue
		}
		seen[raw] = true
		id, err := ids.Parse(raw)
		if err != nil {
			out.Failed = append(out.Failed, api.AdminProductImageBulkFailure{ImageId: raw, Code: genproblems.NotFound, Detail: "no such image"})
			continue
		}
		img, err := s.approveImage(ctx, actor, id, reason)
		if err != nil {
			var p *problem.Error
			if !errors.As(err, &p) || (p.Code != genproblems.NotFound && p.Code != genproblems.InvalidTransition) {
				return api.AdminProductImageBulkResult{}, err
			}
			detail := p.Detail
			if detail == "" {
				detail = "no such image"
			}
			out.Failed = append(out.Failed, api.AdminProductImageBulkFailure{ImageId: raw, Code: p.Code, Detail: detail})
			continue
		}
		out.Approved = append(out.Approved, img)
	}
	return out, nil
}

func (s *Service) rejectImage(ctx context.Context, actor, id ids.ID, body api.AdminProductImageReject) (api.AdminProductImage, error) {
	reason, err := cleanReason(body.Reason)
	if err != nil {
		return api.AdminProductImage{}, err
	}
	if err := s.writeReady(ctx); err != nil {
		return api.AdminProductImage{}, err
	}
	var takenDown *string
	action := "catalogue.image.reject"
	err = s.imageChange(ctx, actor, id, action, &reason,
		func(ctx context.Context, q *admindb.Queries, img admindb.LockProductImageRow) (map[string]any, map[string]any, error) {
			switch img.Status {
			case imgPending:
			case imgApproved:
				// A takedown: the product goes back to the placeholder.
				prod, err := q.LockProductForImage(ctx, img.ProductID)
				if err != nil {
					return nil, nil, err
				}
				if prod.ImageID == img.ID {
					if err := q.SetProductImage(ctx, admindb.SetProductImageParams{ID: img.ProductID, Now: s.now()}); err != nil {
						return nil, nil, err
					}
				}
				if img.Source != "type" {
					// A kind's photo (D-253) is one file shared by many products.
					takenDown = img.PublicKey
				}
			default:
				return nil, nil, invalidTransition("only a waiting or approved image can be rejected")
			}
			if err := q.RejectProductImage(ctx, admindb.RejectProductImageParams{
				ID: img.ID, Reason: &reason, By: ids.UUID(actor), Now: s.now(),
			}); err != nil {
				return nil, nil, err
			}
			return map[string]any{"status": img.Status, "imageKey": img.PublicKey}, map[string]any{"status": "rejected"}, nil
		})
	if err != nil {
		return api.AdminProductImage{}, err
	}
	if takenDown != nil && s.storage.HasImages() {
		// The row no longer points at the public copy; a failed delete leaves
		// an unreferenced file, never a visible image.
		if err := s.storage.RemoveImage(ctx, *takenDown, s.now()); err != nil {
			telemetry.Logger(ctx).WarnContext(ctx, "admin: taken-down image left in the images bucket", "err", err)
		}
	}
	return s.image(ctx, id)
}

func (s *Service) createImage(ctx context.Context, actor, productID ids.ID, body api.AdminProductImageCreate) (api.AdminProductImageUpload, error) {
	if !body.ContentType.Valid() {
		return api.AdminProductImageUpload{}, fieldErr("body.contentType", "not a supported image type")
	}
	if body.SizeBytes < 1 || body.SizeBytes > imageMaxBytes {
		return api.AdminProductImageUpload{}, fieldErr("body.sizeBytes", "must be between 1 byte and 5 MB")
	}
	if err := s.imageReady(ctx); err != nil {
		return api.AdminProductImageUpload{}, err
	}
	q := s.q(ctx)
	st, err := q.ProductStatus(ctx, ids.UUID(productID))
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminProductImageUpload{}, notFound()
	}
	if err != nil {
		return api.AdminProductImageUpload{}, err
	}
	if st == prodMerged || st == prodRetired {
		return api.AdminProductImageUpload{}, invalidTransition("the product was merged or retired")
	}
	now := s.now()
	id := ids.New(now)
	ct := string(body.ContentType)
	key := "product-images/" + productID.String() + "/" + id.String() + imageExt(ct)
	size := int32(body.SizeBytes) // #nosec G115 -- at most 5 MB
	if err := q.InsertAdminProductImage(ctx, admindb.InsertAdminProductImageParams{
		ID: ids.UUID(id), ProductID: ids.UUID(productID), ObjectKey: key, ContentType: ct,
		SizeBytes: &size, UploadedBy: ids.UUID(actor), Now: now,
	}); err != nil {
		return api.AdminProductImageUpload{}, err
	}
	img, err := s.image(ctx, id)
	if err != nil {
		return api.AdminProductImageUpload{}, err
	}
	return api.AdminProductImageUpload{
		Image: img,
		Upload: api.AdminProductImageUploadTarget{
			Url: s.storage.PutURL(key, ct, body.SizeBytes, now), Method: api.PUT,
			Headers:   api.AdminProductImageUploadHeaders{ContentType: body.ContentType, ContentLength: strconv.FormatInt(body.SizeBytes, 10)},
			ExpiresAt: now.Add(storage.URLTTL).UTC(),
		},
	}, nil
}

// confirmImage checks an admin's uploaded file and publishes it at once.
func (s *Service) confirmImage(ctx context.Context, actor, id ids.ID, body api.AdminProductImageApprove) (api.AdminProductImage, error) {
	reason, err := optReason(body.Reason)
	if err != nil {
		return api.AdminProductImage{}, err
	}
	if err := s.imageReady(ctx); err != nil {
		return api.AdminProductImage{}, err
	}
	err = s.imageChange(ctx, actor, id, "catalogue.image.upload", reason,
		func(ctx context.Context, q *admindb.Queries, img admindb.LockProductImageRow) (map[string]any, map[string]any, error) {
			if img.Source != "admin" {
				return nil, nil, notFound()
			}
			if img.Status != imgUploading {
				return nil, nil, invalidTransition("the upload was confirmed already")
			}
			obj, err := s.storage.Head(ctx, img.ObjectKey, s.now())
			if errors.Is(err, storage.ErrNoObject) {
				return nil, nil, fieldErr("upload", "the file was not uploaded")
			}
			if err != nil {
				return nil, nil, fmt.Errorf("admin: head image: %w", err)
			}
			if obj.ContentType != img.ContentType || obj.Size < 1 || obj.Size > imageMaxBytes ||
				(img.SizeBytes != nil && obj.Size != int64(*img.SizeBytes)) {
				return nil, nil, fieldErr("upload", "the uploaded file does not match the declared type and size")
			}
			size := int32(obj.Size) // #nosec G115 -- at most 5 MB
			return s.publish(ctx, q, actor, img, &size)
		})
	if err != nil {
		return api.AdminProductImage{}, err
	}
	return s.image(ctx, id)
}

// closeImages turns down a product's photos still uploading or waiting when
// the product leaves review rejected or merged (D-252).
func (s *Service) closeImages(ctx context.Context, q *admindb.Queries, actor ids.ID, productID pgtype.UUID, reason string) (int64, error) {
	return q.RejectOpenProductImages(ctx, admindb.RejectOpenProductImagesParams{
		ProductID: productID, Reason: &reason, By: ids.UUID(actor), Now: s.now(),
	})
}

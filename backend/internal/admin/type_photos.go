package admin

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	api "github.com/todayz/backend/gen/api/admin-catalogue"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/storage"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Item-type photos (D-253). `todayz images types` stores up to three free-licence
// candidates per kind of item ("toor dal" in Dals & pulses) as `pending` in the
// private bucket. Approving one copies it once to the images bucket, turns the
// kind's other candidates down and gives every product of that kind without an
// image of its own a `type` product image sharing that copy. Rejecting the
// approved one takes it down from all of them.

const tpRejected = "rejected"

// A kind's cursor is its (category, type key), the list's order.
func encodeKindCursor(categoryID pgtype.UUID, typeKey string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(idString(categoryID) + "." + typeKey))
}

func decodeKindCursor(c string) (pgtype.UUID, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return pgtype.UUID{}, "", err
	}
	sid, key, ok := strings.Cut(string(b), ".")
	if !ok || key == "" {
		return pgtype.UUID{}, "", errors.New("admin: kind cursor has no key")
	}
	id, err := ids.Parse(sid)
	if err != nil {
		return pgtype.UUID{}, "", err
	}
	return ids.UUID(id), key, nil
}

func (s *Service) typePhotoOut(r admindb.ListKindTypePhotosRow) api.AdminTypePhoto {
	out := api.AdminTypePhoto{
		Id: idString(r.ID), CategoryId: idString(r.CategoryID), TypeKey: r.TypeKey,
		Status: api.AdminTypePhotoStatus(r.Status), Rank: int(r.Rank),
		ContentType: api.AdminProductImageContentType(r.ContentType), SizeBytes: int(r.SizeBytes),
		ImageKey: r.PublicKey, SourceRef: r.SourceRef, SourceUrl: r.SourceUrl, PageUrl: r.PageUrl,
		Licence: r.Licence, Attribution: r.Attribution, RejectReason: r.RejectReason,
		DecidedAt: utcPtr(r.DecidedAt), CreatedAt: r.CreatedAt.UTC(),
	}
	if r.Status != tpRejected && s.storage != nil {
		u := s.storage.GetURL(r.ObjectKey, s.now())
		out.PreviewUrl = &u
	}
	return out
}

func (s *Service) listTypeKinds(ctx context.Context, p api.ListAdminTypePhotosParams) (api.AdminTypeKindList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminTypeKindList{}, err
	}
	lim := defaultLimit
	if p.Limit != nil {
		lim = min(max(*p.Limit, 1), maxLimit)
	}
	args := admindb.ListTypeKindsParams{Status: imgPending, Lim: int32(lim + 1)} // #nosec G115 -- lim is clamped to [1, maxLimit]
	if p.Status != nil {
		args.Status = string(*p.Status)
	}
	if p.Cursor != nil && *p.Cursor != "" {
		cat, key, err := decodeKindCursor(*p.Cursor)
		if err != nil {
			return api.AdminTypeKindList{}, fieldErr("query.cursor", "not a cursor from this list")
		}
		args.AfterKey, args.AfterCategory = &key, cat
	}
	q := s.q(ctx)
	rows, err := q.ListTypeKinds(ctx, args)
	if err != nil {
		return api.AdminTypeKindList{}, err
	}
	out := api.AdminTypeKindList{Items: []api.AdminTypeKind{}, ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			c := encodeKindCursor(rows[lim-1].CategoryID, rows[lim-1].TypeKey)
			out.NextCursor = &c
			break
		}
		photos, err := q.ListKindTypePhotos(ctx, admindb.ListKindTypePhotosParams{CategoryID: r.CategoryID, TypeKey: r.TypeKey})
		if err != nil {
			return api.AdminTypeKindList{}, err
		}
		kind := api.AdminTypeKind{
			CategoryId: idString(r.CategoryID), CategoryName: r.CategoryName, TypeKey: r.TypeKey,
			ProductCount: int(r.ProductCount), Showing: int(r.Showing), Samples: r.Samples,
			Photos: make([]api.AdminTypePhoto, 0, len(photos)),
		}
		for _, ph := range photos {
			kind.Photos = append(kind.Photos, s.typePhotoOut(ph))
		}
		out.Items = append(out.Items, kind)
	}
	return out, nil
}

func (s *Service) typePhoto(ctx context.Context, id ids.ID) (api.AdminTypePhoto, error) {
	r, err := s.q(ctx).GetTypePhoto(ctx, ids.UUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminTypePhoto{}, notFound()
	}
	if err != nil {
		return api.AdminTypePhoto{}, err
	}
	return s.typePhotoOut(admindb.ListKindTypePhotosRow(r)), nil
}

// typePhotoChange locks one type photo and runs change in a transaction with
// its audit entry.
func (s *Service) typePhotoChange(ctx context.Context, actor, id ids.ID, action string, reason *string,
	change func(ctx context.Context, q *admindb.Queries, tp admindb.LockTypePhotoRow) (before, after map[string]any, err error),
) error {
	return s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		tp, err := q.LockTypePhoto(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		before, after, err := change(ctx, q, tp)
		if err != nil {
			return err
		}
		if reason != nil {
			after["reason"] = *reason
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: action,
			SubjectType: "type_photo", SubjectID: id.String(), Before: before, After: after,
		})
		return err
	})
}

// fillKind gives the kind's approved photo to every product of the kind that
// has no image and never had this photo taken down. It returns how many.
func (s *Service) fillKind(ctx context.Context, q *admindb.Queries, actor ids.ID, tp admindb.LockTypePhotoRow, key string) (int, error) {
	prods, err := q.TypeKindProducts(ctx, admindb.TypeKindProductsParams{CategoryID: tp.CategoryID, TypeKey: tp.TypeKey, TypePhotoID: tp.ID})
	if err != nil || len(prods) == 0 {
		return 0, err
	}
	now := s.now()
	imgIDs := make([]pgtype.UUID, len(prods))
	for i := range prods {
		imgIDs[i] = ids.UUID(ids.New(now))
	}
	if err := q.InsertTypeImages(ctx, admindb.InsertTypeImagesParams{
		By: ids.UUID(actor), Now: now, Ids: imgIDs, ProductIds: prods, TypePhotoID: tp.ID,
	}); err != nil {
		return 0, err
	}
	if err := q.SetTypeProductImages(ctx, admindb.SetTypeProductImagesParams{ImageKey: key, Now: now, Ids: imgIDs, ProductIds: prods}); err != nil {
		return 0, err
	}
	return len(prods), nil
}

func (s *Service) approveTypePhoto(ctx context.Context, actor, id ids.ID, body api.AdminProductImageApprove) (api.AdminTypePhotoDecision, error) {
	reason, err := optReason(body.Reason)
	if err != nil {
		return api.AdminTypePhotoDecision{}, err
	}
	if err := s.imageReady(ctx); err != nil {
		return api.AdminTypePhotoDecision{}, err
	}
	var n int
	err = s.typePhotoChange(ctx, actor, id, "catalogue.type_photo.approve", reason,
		func(ctx context.Context, q *admindb.Queries, tp admindb.LockTypePhotoRow) (map[string]any, map[string]any, error) {
			if tp.Status != imgPending {
				return nil, nil, invalidTransition("the photo is not waiting for review")
			}
			has, err := q.TypeKindHasApproved(ctx, admindb.TypeKindHasApprovedParams{CategoryID: tp.CategoryID, TypeKey: tp.TypeKey})
			if err != nil {
				return nil, nil, err
			}
			if has {
				return nil, nil, invalidTransition("this kind already has a photo; take it down first")
			}
			key := "types/" + id.String() + imageExt(tp.ContentType)
			err = s.storage.Publish(ctx, tp.ObjectKey, key, tp.ContentType, imageMaxBytes, s.now())
			if errors.Is(err, storage.ErrNoObject) {
				return nil, nil, invalidTransition("the stored file is missing")
			}
			if errors.Is(err, storage.ErrTooLarge) {
				return nil, nil, invalidTransition("the stored file is larger than 5 MB")
			}
			if err != nil {
				return nil, nil, fmt.Errorf("admin: publish type photo: %w", err)
			}
			now := s.now()
			if err := q.ApproveTypePhoto(ctx, admindb.ApproveTypePhotoParams{PublicKey: &key, By: ids.UUID(actor), Now: now, ID: tp.ID}); err != nil {
				return nil, nil, err
			}
			other := "another photo was picked for this kind"
			if _, err := q.RejectOtherTypePhotos(ctx, admindb.RejectOtherTypePhotosParams{
				Reason: &other, By: ids.UUID(actor), Now: now, CategoryID: tp.CategoryID, TypeKey: tp.TypeKey, ID: tp.ID,
			}); err != nil {
				return nil, nil, err
			}
			n, err = s.fillKind(ctx, q, actor, tp, key)
			if err != nil {
				return nil, nil, err
			}
			return map[string]any{"status": tp.Status, "typeKey": tp.TypeKey},
				map[string]any{"status": imgApproved, "imageKey": key, "products": n}, nil
		})
	if err != nil {
		return api.AdminTypePhotoDecision{}, err
	}
	photo, err := s.typePhoto(ctx, id)
	return api.AdminTypePhotoDecision{Photo: photo, Products: n}, err
}

// rejectTypePhoto turns a candidate down, or takes the approved photo down
// from every product showing it (they go back to the placeholder; a product
// that lost it this way never gets it again).
func (s *Service) rejectTypePhoto(ctx context.Context, actor, id ids.ID, body api.AdminProductImageReject) (api.AdminTypePhotoDecision, error) {
	reason, err := cleanReason(body.Reason)
	if err != nil {
		return api.AdminTypePhotoDecision{}, err
	}
	if err := s.writeReady(ctx); err != nil {
		return api.AdminTypePhotoDecision{}, err
	}
	var (
		n         int64
		takenDown *string
	)
	err = s.typePhotoChange(ctx, actor, id, "catalogue.type_photo.reject", &reason,
		func(ctx context.Context, q *admindb.Queries, tp admindb.LockTypePhotoRow) (map[string]any, map[string]any, error) {
			now := s.now()
			switch tp.Status {
			case imgPending:
			case imgApproved:
				var err error
				n, err = q.TakeDownTypeImages(ctx, admindb.TakeDownTypeImagesParams{
					Reason: &reason, By: ids.UUID(actor), Now: now, TypePhotoID: tp.ID,
				})
				if err != nil {
					return nil, nil, err
				}
				takenDown = tp.PublicKey
			default:
				return nil, nil, invalidTransition("only a waiting or approved photo can be rejected")
			}
			if err := q.RejectTypePhoto(ctx, admindb.RejectTypePhotoParams{Reason: &reason, By: ids.UUID(actor), Now: now, ID: tp.ID}); err != nil {
				return nil, nil, err
			}
			return map[string]any{"status": tp.Status, "imageKey": tp.PublicKey},
				map[string]any{"status": tpRejected, "products": n}, nil
		})
	if err != nil {
		return api.AdminTypePhotoDecision{}, err
	}
	if takenDown != nil && s.storage.HasImages() {
		if err := s.storage.RemoveImage(ctx, *takenDown, s.now()); err != nil {
			telemetry.Logger(ctx).WarnContext(ctx, "admin: taken-down type photo left in the images bucket", "err", err)
		}
	}
	photo, err := s.typePhoto(ctx, id)
	return api.AdminTypePhotoDecision{Photo: photo, Products: int(n)}, err
}

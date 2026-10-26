package business

// Store photos (D-246): the customer cards and the shop page show the shop's
// newest accepted storefront photo. Documents live in the private bucket, so
// the store_photos_publish job copies that photo into the public images
// bucket and records its key on the store; a store whose storefront photos
// were all rejected later has its photo cleared. The copy runs outside any
// transaction, and the store row is moved on only if no other run moved it
// first.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// photoBatch is how many stores one run publishes; the rest wait for the
// next run, so a photo that keeps failing never holds a run.
const photoBatch = 100

// PhotoResult is what one publish run did.
type PhotoResult struct {
	Published int64
	Cleared   int64
	Failed    int64
}

// photoExt is the file extension of a storefront photo's content type.
func photoExt(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	}
	return ""
}

// storePhotoKey is where a store's photo is published in the images bucket.
func storePhotoKey(store, doc pgtype.UUID, contentType string) string {
	return "stores/" + idString(store) + "/" + idString(doc) + photoExt(contentType)
}

// PublishStorePhotos brings every store's published photo in line with its
// newest accepted storefront photo, up to photoBatch stores per run.
func (s *Service) PublishStorePhotos(ctx context.Context) (PhotoResult, error) {
	var res PhotoResult
	if s.db == nil {
		return res, errUnavailable
	}
	if s.presign == nil {
		return res, errNoStorage
	}
	log := telemetry.Logger(ctx)
	rows, err := s.q(ctx).StorePhotosToPublish(ctx, photoBatch)
	if err != nil {
		return res, err
	}
	// A run cut short by its timeout leaves the rest to the next run.
	for i := 0; i < len(rows) && ctx.Err() == nil; i++ {
		r := rows[i]
		now := s.clock.Now()
		var key *string
		if r.DocID.Valid {
			k := storePhotoKey(r.StoreID, r.DocID, r.ContentType)
			if cerr := s.presign.copyToImages(ctx, r.ObjectKey, k, r.ContentType, now); cerr != nil {
				if ctx.Err() == nil { // else the loop stops; the next run takes it
					log.WarnContext(ctx, "business: could not publish a store photo", "store", idString(r.StoreID), "document", idString(r.DocID), "error", cerr)
					res.Failed++
				}
				continue
			}
			key = &k
		}
		n, err := s.q(ctx).SetStorePhoto(ctx, businessdb.SetStorePhotoParams{
			PhotoKey: key, DocID: r.DocID, StoreID: r.StoreID, PrevDocID: r.PhotoDocumentID,
		})
		if err != nil {
			return res, err
		}
		if n == 0 {
			continue // another run moved this store on
		}
		if key != nil {
			res.Published++
		} else {
			res.Cleared++
		}
		// The photo it replaces is no longer shown anywhere; a delete that
		// fails only leaves an unlinked file behind.
		if r.PhotoKey != nil && (key == nil || *r.PhotoKey != *key) {
			if err := s.presign.removeImage(ctx, *r.PhotoKey, now); err != nil {
				log.WarnContext(ctx, "business: could not delete a replaced store photo", "store", idString(r.StoreID), "error", err)
			}
		}
	}
	return res, nil
}

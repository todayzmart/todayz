package identity

import (
	"context"
	"errors"
	"time"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/identitydb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/imageclean"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/storage"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Profile photo (D-259, 01 §2). The customer registers an upload and PUTs the
// file straight into the private bucket; confirming it re-encodes it on the
// server (imageclean: EXIF and every other byte that is not a pixel is
// dropped) and stores the result as the photo. Only the customer sees it, on
// a presigned GET in /v1/me; there is no public copy and no review.
//
// Object keys: the raw file is profile/<user>/<upload>.upload, the cleaned
// photo profile/<user>/<upload>.jpg. Account deletion removes every key
// UserPhotoObjects lists, so an object that a best-effort remove missed is
// still deleted with the account.

const (
	// photoMaxBytes is the largest file a customer may upload (the contract's
	// ProfilePhotoCreate.sizeBytes maximum).
	photoMaxBytes = 5 << 20
	// photoSide is the stored photo's longest side in pixels.
	photoSide = 512
	// photoMaxPixels refuses a file that would decode to more than this many
	// pixels (a decompression bomb), before it is decoded.
	photoMaxPixels = 40_000_000
	// photoStaleAfter is when an upload nobody confirmed is expired at the
	// customer's next registration: well past its URL's 15 minutes.
	photoStaleAfter = time.Hour
)

var photoTypes = map[string]bool{imageclean.JPEG: true, imageclean.PNG: true, imageclean.WebP: true}

// PhotoUpload is a registered upload and where to PUT the file.
type PhotoUpload struct {
	ID          ids.ID
	URL         string
	ContentType string
	Size        int64
	ExpiresAt   time.Time
}

func photoLockKey(user ids.ID) string { return "profile_photo:" + user.String() }

func photoRawKey(user, upload ids.ID) string {
	return "profile/" + user.String() + "/" + upload.String() + ".upload"
}

func photoKey(user, upload ids.ID) string {
	return "profile/" + user.String() + "/" + upload.String() + ".jpg"
}

func (s *Service) readyPhoto(ctx context.Context) error {
	if err := s.ready(); err != nil {
		return unavailable(ctx, err)
	}
	if s.storage == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "identity: profile photos need storage (STORAGE_*) and platform settings")
		return problem.New(genproblems.ServiceUnavailable, "")
	}
	return nil
}

// CreatePhotoUpload records an upload and presigns its PUT. At most
// profile_photo_uploads_per_hour per person in a rolling hour (429).
func (s *Service) CreatePhotoUpload(ctx context.Context, p authn.Principal, contentType string, size int64) (PhotoUpload, error) {
	if err := s.readyPhoto(ctx); err != nil {
		return PhotoUpload{}, err
	}
	if !photoTypes[contentType] {
		return PhotoUpload{}, fieldErr("body.contentType", "JPEG, PNG or WebP")
	}
	if size < 1 || size > photoMaxBytes {
		return PhotoUpload{}, fieldErr("body.sizeBytes", "at most 5 MB")
	}
	perHour, err := s.settings.Int(ctx, settings.ProfilePhotoUploadsPerHour)
	if err != nil {
		return PhotoUpload{}, problem.Wrap(genproblems.ServiceUnavailable, err)
	}
	now := s.clock.Now()
	id := ids.New(now)
	key := photoRawKey(p.UserID, id)
	var stale []string
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		if err := q.LockKey(ctx, photoLockKey(p.UserID)); err != nil {
			return err
		}
		n, err := q.CountRecentPhotoUploads(ctx, identitydb.CountRecentPhotoUploadsParams{UserID: ids.UUID(p.UserID), Since: now.Add(-time.Hour)})
		if err != nil {
			return err
		}
		if int64(n) >= perHour {
			return problem.New(genproblems.RateLimited, "")
		}
		old, err := q.StalePhotoUploads(ctx, identitydb.StalePhotoUploadsParams{UserID: ids.UUID(p.UserID), Before: now.Add(-photoStaleAfter)})
		if err != nil {
			return err
		}
		for _, o := range old {
			if err := q.ExpirePhotoUpload(ctx, o.ID); err != nil {
				return err
			}
			stale = append(stale, o.ObjectKey)
		}
		return q.InsertPhotoUpload(ctx, identitydb.InsertPhotoUploadParams{
			ID: ids.UUID(id), UserID: ids.UUID(p.UserID), ObjectKey: key, ContentType: contentType,
			SizeBytes: int32(size), Now: now, //nolint:gosec // size ≤ 5 MB, checked above
		})
	})
	if err != nil {
		return PhotoUpload{}, asProblem(err)
	}
	s.removeBestEffort(ctx, stale...)
	return PhotoUpload{
		ID: id, URL: s.storage.PutURL(key, contentType, size, now), ContentType: contentType, Size: size,
		ExpiresAt: now.Add(storage.URLTTL),
	}, nil
}

// ConfirmPhoto makes an uploaded file the profile photo: it is read back,
// checked against what was registered, re-encoded and stored, and the old
// photo and the raw file are removed. Confirming an upload already used
// returns the user as they are.
func (s *Service) ConfirmPhoto(ctx context.Context, p authn.Principal, uploadID ids.ID) (Me, error) {
	if err := s.readyPhoto(ctx); err != nil {
		return Me{}, err
	}
	q := identitydb.New(s.db.Q(ctx))
	up, err := q.LockPhotoUpload(ctx, identitydb.LockPhotoUploadParams{ID: ids.UUID(uploadID), UserID: ids.UUID(p.UserID)})
	switch {
	case db.IsNoRows(err):
		return Me{}, notFound()
	case err != nil:
		return Me{}, asProblem(err)
	}
	switch up.Status {
	case "done":
		return s.GetMe(ctx, p)
	case "expired":
		return Me{}, notFound()
	}

	// The file, outside any transaction: storage calls never hold a row lock.
	now := s.clock.Now()
	raw, err := s.storage.Get(ctx, up.ObjectKey, int64(up.SizeBytes), now)
	switch {
	case errors.Is(err, storage.ErrNoObject):
		return Me{}, fieldErr("upload", "the file was not uploaded")
	case errors.Is(err, storage.ErrTooLarge):
		return Me{}, fieldErr("upload", "the file is larger than registered")
	case err != nil:
		return Me{}, problem.Wrap(genproblems.ServiceUnavailable, err)
	}
	if int64(len(raw)) != int64(up.SizeBytes) {
		return Me{}, fieldErr("upload", "the file is not the size registered")
	}
	if imageclean.Sniff(raw) != up.ContentType {
		return Me{}, fieldErr("upload", "the file is not the type registered")
	}
	clean, err := imageclean.Clean(raw, photoSide, photoMaxPixels)
	switch {
	case errors.Is(err, imageclean.ErrTooLarge):
		return Me{}, fieldErr("upload", "the image has too many pixels")
	case errors.Is(err, imageclean.ErrNotImage):
		return Me{}, fieldErr("upload", "the file is not a readable JPEG, PNG or WebP image")
	case err != nil:
		return Me{}, asProblem(err)
	}
	key := photoKey(p.UserID, uploadID)
	if err := s.storage.Put(ctx, key, imageclean.JPEG, clean.JPEG, now); err != nil {
		return Me{}, problem.Wrap(genproblems.ServiceUnavailable, err)
	}

	var out Me
	var replaced *string
	orphan := false
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		if err := q.LockKey(ctx, photoLockKey(p.UserID)); err != nil {
			return err
		}
		up, err := q.LockPhotoUpload(ctx, identitydb.LockPhotoUploadParams{ID: ids.UUID(uploadID), UserID: ids.UUID(p.UserID)})
		if err != nil {
			return err
		}
		switch up.Status {
		case "uploading":
			u, err := q.LockUserPhoto(ctx, ids.UUID(p.UserID))
			if db.IsNoRows(err) {
				return problem.New(genproblems.Unauthorized, "")
			}
			if err != nil {
				return err
			}
			if refusal := statusRefusal(u.Status); refusal != nil {
				return refusal
			}
			if err := q.SetUserPhoto(ctx, identitydb.SetUserPhotoParams{PhotoKey: &key, ID: ids.UUID(p.UserID)}); err != nil {
				return err
			}
			if err := q.MarkPhotoUploadDone(ctx, identitydb.MarkPhotoUploadDoneParams{Now: now, ID: ids.UUID(uploadID)}); err != nil {
				return err
			}
			if u.PhotoKey != nil && *u.PhotoKey != key {
				replaced = u.PhotoKey
			}
		case "expired":
			orphan = true // expired meanwhile: the cleaned copy is nobody's
		}
		out, err = s.readMe(ctx, q, p)
		return err
	})
	if err != nil {
		return Me{}, asProblem(err)
	}
	gone := []string{up.ObjectKey}
	if replaced != nil {
		gone = append(gone, *replaced)
	}
	if orphan {
		gone = append(gone, key)
	}
	s.removeBestEffort(ctx, gone...)
	return out, nil
}

// RemovePhoto deletes the profile photo. The object is removed before the
// change commits, so a storage failure (503) leaves the photo as it was.
func (s *Service) RemovePhoto(ctx context.Context, p authn.Principal) (Me, error) {
	if err := s.readyPhoto(ctx); err != nil {
		return Me{}, err
	}
	var out Me
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		if err := q.LockKey(ctx, photoLockKey(p.UserID)); err != nil {
			return err
		}
		u, err := q.LockUserPhoto(ctx, ids.UUID(p.UserID))
		if db.IsNoRows(err) {
			return problem.New(genproblems.Unauthorized, "")
		}
		if err != nil {
			return err
		}
		if refusal := statusRefusal(u.Status); refusal != nil {
			return refusal
		}
		if u.PhotoKey != nil {
			if err := q.SetUserPhoto(ctx, identitydb.SetUserPhotoParams{PhotoKey: nil, ID: ids.UUID(p.UserID)}); err != nil {
				return err
			}
			if err := s.storage.Remove(ctx, *u.PhotoKey, s.clock.Now()); err != nil {
				return problem.Wrap(genproblems.ServiceUnavailable, err)
			}
		}
		out, err = s.readMe(ctx, q, p)
		return err
	})
	if err != nil {
		return Me{}, asProblem(err)
	}
	return out, nil
}

// removePhotos deletes every photo object of the user and their upload rows,
// inside the account-deletion transaction: a storage failure is a 503 that
// rolls the deletion back, so no photo outlives the account.
func (s *Service) removePhotos(ctx context.Context, q *identitydb.Queries, user ids.ID) error {
	if err := q.LockKey(ctx, photoLockKey(user)); err != nil {
		return err
	}
	keys, err := q.UserPhotoObjects(ctx, ids.UUID(user))
	if err != nil {
		return err
	}
	if len(keys) > 0 && s.storage == nil {
		return problem.New(genproblems.ServiceUnavailable, "")
	}
	now := s.clock.Now()
	for _, k := range keys {
		if err := s.storage.Remove(ctx, k, now); err != nil {
			return problem.Wrap(genproblems.ServiceUnavailable, err)
		}
	}
	return q.DeletePhotoUploads(ctx, ids.UUID(user))
}

// removeBestEffort deletes objects nothing points at any more; a failure is
// logged (the key only) and left for account deletion to retry.
func (s *Service) removeBestEffort(ctx context.Context, keys ...string) {
	now := s.clock.Now()
	for _, k := range keys {
		if err := s.storage.Remove(ctx, k, now); err != nil {
			telemetry.Logger(ctx).WarnContext(ctx, "identity: could not remove a profile photo object", "key", k, "error", err)
		}
	}
}

// photoURL is the presigned GET of the photo for the customer app.
func (s *Service) photoURL(key *string) *string {
	if key == nil || s.storage == nil {
		return nil
	}
	u := s.storage.GetURL(*key, s.clock.Now())
	return &u
}

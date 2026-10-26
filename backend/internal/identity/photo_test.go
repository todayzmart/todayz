package identity

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/imageclean"
	"github.com/todayz/backend/internal/platform/storage"
	"github.com/todayz/backend/internal/platform/storage/storagetest"
)

func newPhotoFixture(t *testing.T) (*fixture, *storagetest.Fake) {
	t.Helper()
	fake := storagetest.New(t)
	f := newFixtureWith(t, config.EnvTest, func(_ *config.Config, d *Deps) {
		d.Storage = storage.New(fake.Config(), fake.Client())
	})
	return f, fake
}

// photoJPEG is a small JPEG carrying a COM segment with marker, standing in
// for metadata that must not reach storage.
func photoJPEG(t *testing.T, marker string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 4), G: 120, B: uint8(y * 5), A: 255}) //nolint:gosec // < 256
		}
	}
	var b bytes.Buffer
	require.NoError(t, jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}))
	j := b.Bytes()
	com := []byte{0xFF, 0xFE, 0, byte(len(marker) + 2)}
	return append(append(append([]byte{}, j[:2]...), append(com, marker...)...), j[2:]...)
}

func (f *fixture) uploadPhoto(fake *storagetest.Fake, p authn.Principal, contentType string, body []byte) PhotoUpload {
	f.t.Helper()
	u, err := f.svc.CreatePhotoUpload(f.ctx("10.0.0.1"), p, contentType, int64(len(body)))
	require.NoError(f.t, err)
	fake.PutURL(u.URL, contentType, body)
	return u
}

func TestProfilePhoto(t *testing.T) {
	f, fake := newPhotoFixture(t)
	p := f.customer("+919876543210")
	ctx := f.ctx("10.0.0.1")
	uid := ids.UUID(p.UserID)

	me, err := f.svc.GetMe(ctx, p)
	require.NoError(t, err)
	require.Nil(t, me.PhotoURL)
	v0 := me.Version

	// The registration checks type and size.
	_, err = f.svc.CreatePhotoUpload(ctx, p, "image/gif", 10)
	requireCode(t, err, genproblems.ValidationFailed, "a GIF")
	_, err = f.svc.CreatePhotoUpload(ctx, p, imageclean.JPEG, photoMaxBytes+1)
	requireCode(t, err, genproblems.ValidationFailed, "over 5 MB")

	// Upload, confirm: the stored photo is a re-encoded JPEG without the
	// metadata, the raw file is gone, and /v1/me carries a signed link.
	const secret = "GPS 12.9719N 77.6412E"
	raw := photoJPEG(t, secret)
	u := f.uploadPhoto(fake, p, imageclean.JPEG, raw)
	require.Equal(t, storage.URLTTL, u.ExpiresAt.Sub(f.clk.Now()))
	rawKey := photoRawKey(p.UserID, u.ID)
	_, ok := fake.Private(rawKey)
	require.True(t, ok)
	me, err = f.svc.ConfirmPhoto(ctx, p, u.ID)
	require.NoError(t, err)
	require.NotNil(t, me.PhotoURL)
	key := photoKey(p.UserID, u.ID)
	require.Contains(t, *me.PhotoURL, "/"+storagetest.PrivateBucket+"/"+key+"?")
	require.Equal(t, v0+1, me.Version, "the ETag changes")
	stored, ok := fake.Private(key)
	require.True(t, ok)
	require.Equal(t, imageclean.JPEG, imageclean.Sniff(stored))
	require.NotContains(t, string(stored), secret)
	_, ok = fake.Private(rawKey)
	require.False(t, ok, "the raw upload is removed")
	require.Equal(t, 1, f.count(`SELECT count(*) FROM users WHERE id = $1 AND photo_key = $2`, uid, key))

	// Confirming again changes nothing.
	again, err := f.svc.ConfirmPhoto(ctx, p, u.ID)
	require.NoError(t, err)
	require.Equal(t, me.Version, again.Version)

	// Another customer's upload is 404 to this one; so is an unknown id.
	other := f.customer("+919876543211")
	_, err = f.svc.ConfirmPhoto(ctx, other, u.ID)
	requireCode(t, err, genproblems.NotFound)
	_, err = f.svc.ConfirmPhoto(ctx, p, ids.New(t0))
	requireCode(t, err, genproblems.NotFound)

	// The file must be there, of the registered size and type.
	missing, err := f.svc.CreatePhotoUpload(ctx, p, imageclean.JPEG, 100)
	require.NoError(t, err)
	_, err = f.svc.ConfirmPhoto(ctx, p, missing.ID)
	requireCode(t, err, genproblems.ValidationFailed, "nothing uploaded")
	lied := f.uploadPhoto(fake, p, imageclean.PNG, raw)
	_, err = f.svc.ConfirmPhoto(ctx, p, lied.ID)
	requireCode(t, err, genproblems.ValidationFailed, "a JPEG registered as PNG")
	bigger, err := f.svc.CreatePhotoUpload(ctx, p, imageclean.JPEG, 10)
	require.NoError(t, err)
	fake.PutURL(bigger.URL, imageclean.JPEG, raw)
	_, err = f.svc.ConfirmPhoto(ctx, p, bigger.ID)
	requireCode(t, err, genproblems.ValidationFailed, "a larger file than registered")
	junk := []byte("\x89PNG\r\n\x1a\nnot really a png")
	broken := f.uploadPhoto(fake, p, imageclean.PNG, junk)
	_, err = f.svc.ConfirmPhoto(ctx, p, broken.ID)
	requireCode(t, err, genproblems.ValidationFailed, "an unreadable image")
	cur, err := f.svc.GetMe(ctx, p)
	require.NoError(t, err)
	require.Equal(t, me.Version, cur.Version, "a refused confirm keeps the photo")

	// A new photo (a PNG this time) replaces the old one, whose file goes.
	var pb bytes.Buffer
	require.NoError(t, png.Encode(&pb, image.NewNRGBA(image.Rect(0, 0, 8, 8))))
	u2 := f.uploadPhoto(fake, p, imageclean.PNG, pb.Bytes())
	me, err = f.svc.ConfirmPhoto(ctx, p, u2.ID)
	require.NoError(t, err)
	require.Contains(t, *me.PhotoURL, photoKey(p.UserID, u2.ID))
	_, ok = fake.Private(key)
	require.False(t, ok, "the replaced photo is removed")

	// Remove: the file and the link go; removing again changes nothing.
	me, err = f.svc.RemovePhoto(ctx, p)
	require.NoError(t, err)
	require.Nil(t, me.PhotoURL)
	_, ok = fake.Private(photoKey(p.UserID, u2.ID))
	require.False(t, ok)
	again, err = f.svc.RemovePhoto(ctx, p)
	require.NoError(t, err)
	require.Equal(t, me.Version, again.Version)
}

func TestProfilePhotoRateLimitAndStaleUploads(t *testing.T) {
	f, fake := newPhotoFixture(t)
	p := f.customer("+919876543210")
	ctx := f.ctx("10.0.0.1")

	// profile_photo_uploads_per_hour (10) in a rolling hour, then 429.
	var first PhotoUpload
	for i := range 10 {
		u := f.uploadPhoto(fake, p, imageclean.JPEG, []byte("x"))
		if i == 0 {
			first = u
		}
	}
	_, err := f.svc.CreatePhotoUpload(ctx, p, imageclean.JPEG, 1)
	requireCode(t, err, genproblems.RateLimited)

	// An hour on, the window has moved; the unconfirmed uploads are expired
	// and their files removed.
	f.clk.Advance(time.Hour + time.Second)
	_, err = f.svc.CreatePhotoUpload(ctx, p, imageclean.JPEG, 1)
	require.NoError(t, err)
	_, ok := fake.Private(photoRawKey(p.UserID, first.ID))
	require.False(t, ok)
	require.Equal(t, 10, f.count(`SELECT count(*) FROM user_photo_uploads WHERE user_id = $1 AND status = 'expired'`, ids.UUID(p.UserID)))
	_, err = f.svc.ConfirmPhoto(ctx, p, first.ID)
	requireCode(t, err, genproblems.NotFound, "an expired upload")
}

func TestProfilePhotoGoesWithTheAccount(t *testing.T) {
	f, fake := newPhotoFixture(t)
	const phone = "+919876543210"
	p := f.customer(phone)
	ctx := f.ctx("10.0.0.1")
	u := f.uploadPhoto(fake, p, imageclean.JPEG, photoJPEG(t, "x"))
	_, err := f.svc.ConfirmPhoto(ctx, p, u.ID)
	require.NoError(t, err)
	pending := f.uploadPhoto(fake, p, imageclean.JPEG, []byte("y"))

	reqID, code := stepUpCode(f, phone)
	require.NoError(t, f.svc.DeleteAccount(ctx, p, DeleteInput{RequestID: reqID, Code: code}))
	for _, k := range []string{photoKey(p.UserID, u.ID), photoRawKey(p.UserID, pending.ID)} {
		_, ok := fake.Private(k)
		require.False(t, ok, "%s outlived the account", k)
	}
	require.Equal(t, 0, f.count(`SELECT count(*) FROM user_photo_uploads WHERE user_id = $1`, ids.UUID(p.UserID)))
	require.Equal(t, 0, f.count(`SELECT count(*) FROM users WHERE id = $1 AND photo_key IS NOT NULL`, ids.UUID(p.UserID)))
}

func TestProfilePhotoNeedsStorage(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	p := f.customer("+919876543210")
	_, err := f.svc.CreatePhotoUpload(f.ctx("10.0.0.1"), p, imageclean.JPEG, 10)
	requireCode(t, err, genproblems.ServiceUnavailable)
	_, err = f.svc.RemovePhoto(f.ctx("10.0.0.1"), p)
	requireCode(t, err, genproblems.ServiceUnavailable)
}

package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/platform/db"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// KindBusinessFieldPhotosSweep rejects field photos whose upload was never
// confirmed in time and deletes their objects (W3-06; maintenance class: a
// late run only keeps a failed upload around longer, and the cap already
// ignores it).
const KindBusinessFieldPhotosSweep = "business_field_photos_sweep"

// KindStorePhotosPublish copies each store's newest accepted storefront photo
// into the public images bucket for the customer cards (D-246; maintenance
// class: a late run only shows the initials a little longer).
const KindStorePhotosPublish = "store_photos_publish"

// registerbusiness declares the business domain's job kinds.
// The work package that builds internal/business fills it in: one
// platformjobs.Define per kind (W1-11, see identity.go). Keep it to this file.
func registerbusiness(r *Registry, d Deps) {
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindBusinessFieldPhotosSweep, Owner: "W3-06", Class: platformjobs.ClassMaintenance,
		Description: "Rejects field photos never confirmed in time and deletes their files",
		Schedule:    "41 * * * *", UniqueBy: platformjobs.PerTick, Timeout: time.Minute, MaxAttempts: 1,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			if d.Config == nil {
				return platformjobs.Result{}, errors.New("business: field photos sweep needs the config")
			}
			svc, err := business.New(business.Deps{Config: d.Config, DB: db.New(d.Pool), Clock: d.Clock, Log: d.Log})
			if err != nil {
				return platformjobs.Result{}, err
			}
			res, err := svc.SweepFieldPhotos(ctx)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"rejected": res.Rejected, "failed": res.Failed}}, nil
		},
	})
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindStorePhotosPublish, Owner: "W4-98", Class: platformjobs.ClassMaintenance,
		Description: "Publishes each store's newest accepted storefront photo to the images bucket",
		Schedule:    "*/5 * * * *", UniqueBy: platformjobs.PerTick, Timeout: time.Minute, MaxAttempts: 1,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			if d.Config == nil {
				return platformjobs.Result{}, errors.New("business: store photos publish needs the config")
			}
			svc, err := business.New(business.Deps{Config: d.Config, DB: db.New(d.Pool), Clock: d.Clock, Log: d.Log})
			if err != nil {
				return platformjobs.Result{}, err
			}
			res, err := svc.PublishStorePhotos(ctx)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"published": res.Published, "cleared": res.Cleared, "failed": res.Failed}}, nil
		},
	})
}

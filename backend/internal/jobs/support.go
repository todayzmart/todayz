package jobs

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/support"
)

// KindSupportUploadsSweep deletes support uploads no message claimed within
// support_upload_ttl_hours, objects and rows (01 §13, D-226; maintenance
// class: a late run only keeps a refused upload around longer).
const KindSupportUploadsSweep = "support_uploads_sweep"

// registersupport declares the support domain's job kinds.
func registersupport(r *Registry, d Deps) {
	var storage config.Storage
	var noEgress bool
	if d.Config != nil {
		storage, noEgress = d.Config.Storage, d.Config.NoEgress
	}
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindSupportUploadsSweep, Owner: "W3-08", Class: platformjobs.ClassMaintenance,
		Description: "Deletes support uploads no message claimed in time",
		Schedule:    "17 * * * *", UniqueBy: platformjobs.PerTick, Timeout: time.Minute, MaxAttempts: 1,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			svc, err := support.New(support.Deps{
				DB: db.New(d.Pool), Clock: d.Clock, Settings: d.Settings, Storage: storage, NoEgress: noEgress,
			})
			if err != nil {
				return platformjobs.Result{}, err
			}
			res, err := svc.SweepUploads(ctx)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"deleted": res.Deleted, "failed": res.Failed}}, nil
		},
	})
}

package main

import (
	"encoding/base64"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/asks"
	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/search"
	"github.com/todayz/backend/internal/share"
)

// newAsks builds the asks service: the routes, the reply price orders reads
// and the unanswered demand insights reads (W3-16, D-101).
func newAsks(d *deps) *asks.Service {
	ad := asks.Deps{
		DB: d.DB, Clock: d.Clock, Settings: d.Settings, Events: eventCatalogue("asks"),
		Normalize: search.Normalize, OpenAt: business.OpenAt,
	}
	if d.DB != nil {
		ad.Notices = newNotifyService(d)
	}
	if d.Authz != nil {
		ad.Authz = d.Authz
	}
	svc, err := asks.New(ad)
	if err != nil {
		panic("asks: " + err.Error())
	}
	return svc
}

// newShare builds the share cards, site strip and A-23 Demand service. The
// card origins come from config; a stale card image is redrawn by the
// share_card_render job when the API has a queue.
func newShare(d *deps) *share.Service {
	sd := share.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, Events: eventCatalogue("share")}
	c := d.Config
	if c == nil {
		c = &config.Config{Env: config.EnvLocal}
	} else {
		if k := c.Keys.PIIHashKey; k.IsSet() {
			h, err := pii.NewHasher(k.Bytes())
			if err != nil {
				panic("share: PII_HASH_KEY must be 32 bytes")
			}
			sd.Hash = h
			sd.IPHashKey = k.Bytes()
		}
		if k := c.Keys.SharePreviewKey; k.IsSet() {
			sd.PreviewKey = base64.StdEncoding.EncodeToString(k.Bytes())
		}
		if st := share.NewS3Store(c.Storage, httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: c.NoEgress}), d.Clock); st != nil {
			sd.Images = st
		}
	}
	if d.Queue != nil {
		reg := platformjobs.NewRegistry()
		jd := jobs.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log}
		if d.DB != nil {
			jd.Pool = d.DB.Pool
		}
		jobs.Register(reg, jd)
		sd.Jobs = platformjobs.NewEnqueuer(reg, d.Queue, d.Clock)
	}
	sd.AppURL, sd.ImageURL = c.CustomerAppOrigin(), c.ImagesOrigin()
	svc, err := share.New(sd)
	if err != nil {
		panic("share: " + err.Error())
	}
	return svc
}

// mountAsks mounts W3-16's routes: the customer's asks under /v1/me/asks,
// the store inbox under /v1/biz/businesses/{bid}/stores/{sid}/asks, share
// cards and the site strip under /v1/public, and A-23 under /v1/admin. Account
// deletion deletes the customer's asks and the blocks they caused (01 §16).
func mountAsks(r chi.Router, d *deps) {
	svc := newAsks(d)
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	asks.Register(r, svc, az)
	share.Register(r, newShare(d), az)
	if d.Identity != nil {
		d.Identity.AddDeletionHook(identity.OnDeleteFunc(svc.OnAccountDelete))
	}
}

package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/reviews"
)

// mountReviews mounts W2-06's reviews: the customer (/v1/me/reviews*,
// /v1/me/review-reports), shop (/v1/biz/.../reviews*) and public
// (/v1/discover/stores/{slug}/reviews) routes, plus the admin moderation
// routes (/v1/admin/reviews*, /v1/admin/review-reports*) pulled forward from
// W3-05. There is no routes_admin-reviews.go (one routes file per domain).
func mountReviews(r chi.Router, d *deps) {
	rd := reviews.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings}
	if d.DB != nil {
		var hashKey []byte
		if d.Config != nil {
			hashKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		rd.Audit = audit.New(d.DB, d.Clock, hashKey)
	}
	svc, err := reviews.New(rd)
	if err != nil {
		panic("reviews: " + err.Error())
	}
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	reviews.Register(r, svc, az)
}

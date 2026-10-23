package main

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/notify"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/mw"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/realtime"
)

// mountNotify mounts W2-05's routes: the three SSE streams (the realtime
// tag, hand-mounted because a stream is not a strict JSON operation) and the
// notifications domain (inbox, unread count, mark read, preferences).
func mountNotify(r chi.Router, d *deps) {
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	mountStreams(r, d, az)
	notify.Register(r, newNotifyService(d), az)
}

// storeStreamRecheck is how often an open store stream re-runs its
// permission check (realtime.DefaultRecheck; the HTTP test shortens it).
var storeStreamRecheck = realtime.DefaultRecheck

// mountStreams mounts GET /v1/me/stream, /v1/admin/stream and the store
// stream. Each runs slot 15 (mw.Authz) before the handler; the store stream
// also re-runs its check every minute while open, so a member removed from
// the store or the business (D-107) is cut off and refused on reconnect.
func mountStreams(r chi.Router, d *deps, az authz.Authorizer) {
	stream := func(ch realtime.ChannelFunc, app string, recheck func(*http.Request) error) http.Handler {
		every := time.Duration(0)
		if recheck != nil {
			every = storeStreamRecheck
		}
		if d.Realtime == nil || d.DB == nil {
			// Router tests and tools without the hub: the routes exist
			// (the contract tests walk them) and answer 503.
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				problem.Write(w, r, problem.New(genproblems.ServiceUnavailable, "").WithHeader("Retry-After", "5"))
			})
		}
		return &realtime.Stream{
			Hub: d.Realtime, DB: d.DB.Pool, Clock: d.Clock, Channel: ch, App: app,
			Metrics: d.Metrics, Log: d.Log, Recheck: recheck, RecheckEvery: every,
		}
	}
	storeRecheck := func(r *http.Request) error {
		_, err := authz.Authorize(az, r, policy.StoreStream)
		return err
	}
	r.With(mw.Authz(az, policy.MeStream)).Method(http.MethodGet, "/v1/me/stream",
		stream(realtime.MeChannel, "customer", nil))
	r.With(mw.Authz(az, policy.AdminStream)).Method(http.MethodGet, "/v1/admin/stream",
		stream(realtime.AdminChannelFunc, "admin", nil))
	r.With(mw.Authz(az, policy.StoreStream)).Method(http.MethodGet, "/v1/biz/businesses/{bid}/stores/{sid}/stream",
		stream(realtime.StoreChannelFromPath, "business", storeRecheck))
}

// newNotifyService builds the notify service the API uses: the inbox
// handlers here, and notice.Sender for a domain that writes a notification
// in its own transaction (W2-02's routes file hands it to orders as a
// notice.Sender). Outside messages are queued through the API's insert-only
// River client; without one (router tests) they are skipped and counted.
func newNotifyService(d *deps) *notify.Service {
	nd := notify.Deps{DB: d.DB, Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log}
	if d.DB != nil {
		nd.Staff = business.NewStaffResolver(d.DB)
	}
	if d.Queue != nil {
		reg := platformjobs.NewRegistry()
		jd := jobs.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log}
		if d.DB != nil {
			jd.Pool = d.DB.Pool
		}
		jobs.Register(reg, jd)
		nd.Jobs = platformjobs.NewEnqueuer(reg, d.Queue, d.Clock)
	}
	svc, err := notify.New(nd)
	if err != nil {
		// Only a missing clock or a malformed PII key gets here; boot and
		// buildSecurity refuse both before the routes mount.
		panic("notify: " + err.Error())
	}
	return svc
}

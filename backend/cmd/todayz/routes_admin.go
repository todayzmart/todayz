package main

import (
	"context"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/admin"
	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authz"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/jobs/adminapi"
	"github.com/todayz/backend/internal/platform/pii"
)

// mountAdmin mounts the admin domain's routes. Only the work package that builds
// internal/admin edits this file (D-50): build the domain's service from d and
// call its Register, e.g. admin.Register(r, admin.Deps{DB: d.DB, Clock: d.Clock}).
//
// W1-11 mounts the admin-jobs operations here (/v1/admin/jobs*, /v1/admin/job-runs*,
// A-21): they are admin routes and there is no routes_admin-jobs.go (one routes
// file per domain, TestOneMountPerDomain).
func mountAdmin(r chi.Router, d *deps) {
	mountAdminJobs(r, d)
	mountAdminConsole(r, d)
}

// mountAdminConsole mounts W3-05's admin-customers, admin-businesses,
// admin-stores, admin-leads, admin-catalogue, admin-banners, admin-settings
// and admin-audit routes. The strike reset recounts through a minimal orders
// service (its strike rules read only the database and settings); a banner
// takedown goes through the banners service (it owns the removal).
func mountAdminConsole(r chi.Router, d *deps) {
	ad := admin.Deps{DB: d.DB, Clock: d.Clock, Hours: storeHours}
	if d.Authz != nil {
		ad.Authz = d.Authz
	}
	if d.Config != nil {
		if k := d.Config.Keys.PIIEncKey; k.IsSet() {
			seal, err := pii.NewSealer(k.Bytes())
			if err != nil {
				panic("admin: PII_ENC_KEY must be 32 bytes")
			}
			ad.Seal = seal
		}
		if k := d.Config.Keys.PIIHashKey; k.IsSet() {
			h, err := pii.NewHasher(k.Bytes())
			if err != nil {
				panic("admin: PII_HASH_KEY must be 32 bytes")
			}
			ad.Hash = h
		}
		pr := d.Config.Providers
		// A-17 shows set or missing only (D-19); the values never leave config.
		ad.Providers = []admin.ProviderKey{
			{Name: "WHATSAPP_API_KEY", Set: pr.WhatsAppAPIKey.IsSet()},
			{Name: "VAPID_PRIVATE_KEY", Set: pr.VAPIDPrivateKey.IsSet()},
			{Name: "TURNSTILE_SECRET_KEY", Set: pr.TurnstileSecretKey.IsSet()},
			{Name: "SARVAM_API_KEY", Set: pr.SarvamAPIKey.IsSet()},
			{Name: "ANTHROPIC_API_KEY", Set: pr.AnthropicAPIKey.IsSet()},
			{Name: "GEOCODER_API_KEY", Set: pr.GeocoderAPIKey.IsSet()},
		}
	}
	if d.DB != nil {
		var hashKey []byte
		if d.Config != nil {
			hashKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		ad.Audit = audit.New(d.DB, d.Clock, hashKey)
		ord, err := orders.New(orders.Deps{Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log, Settings: d.Settings})
		if err != nil {
			panic("admin: " + err.Error())
		}
		ad.Strikes = ord
		ad.Banners = newBanners(d)
		if ad.Seal != nil && ad.Hash != nil {
			acc, err := identity.NewAccounts(d.DB, ad.Seal, ad.Hash)
			if err != nil {
				panic("admin: " + err.Error())
			}
			ad.Accounts = phoneAccounts{acc}
		}
	}
	svc, err := admin.New(ad)
	if err != nil {
		panic("admin: " + err.Error())
	}
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	admin.Register(r, svc, az)
}

// mountAdminJobs builds the same job registry as the worker (so A-21 lists
// what the worker runs) and mounts the admin-jobs API.
func mountAdminJobs(r chi.Router, d *deps) {
	reg := platformjobs.NewRegistry()
	jd := jobs.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log}
	ad := adminapi.Deps{Registry: reg, Clock: d.Clock, Log: d.Log,
		// W1-01's admin gate: the slot-11 session, its live roles and step-up.
		Gate: adminJobsGate{authz.NewAdminGate(d.Clock)}}
	if d.DB != nil {
		jd.Pool = d.DB.Pool
		ad.DB = d.DB
		var hashKey []byte
		if d.Config != nil {
			hashKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		ad.Audit = audit.New(d.DB, d.Clock, hashKey)
	}
	jobs.Register(reg, jd)
	if d.Queue != nil {
		ad.Enqueuer = platformjobs.NewEnqueuer(reg, d.Queue, d.Clock)
	}
	adminapi.Register(r, ad)
}

// adminJobsGate is W1-01's authz.AdminGate behind W1-11's adminapi.Gate seam.
// The role names are the same D-64 strings in both packages; the admin's id
// is its user id (admin_users.user_id).
type adminJobsGate struct{ g *authz.AdminGate }

// Require implements adminapi.Gate.
func (a adminJobsGate) Require(ctx context.Context, anyOf []adminapi.Role, stepUp bool) (adminapi.Admin, error) {
	want := make([]authz.Role, len(anyOf))
	for i, r := range anyOf {
		want[i] = authz.Role(r)
	}
	p, err := a.g.Require(ctx, want, stepUp)
	if err != nil {
		return adminapi.Admin{}, err
	}
	held := make([]adminapi.Role, 0, len(p.Roles))
	for _, r := range p.Roles {
		if authz.IsAdminRole(r) {
			held = append(held, adminapi.Role(r))
		}
	}
	return adminapi.Admin{ID: p.UserID, Roles: held}, nil
}

// storeHours checks an A-05 hours edit by internal/business's rules and
// returns the stored form.
func storeHours(raw []byte, prefix string) ([]byte, []admin.FieldError, error) {
	h, err := business.ParseHours(raw)
	if err != nil {
		return nil, []admin.FieldError{{Path: prefix, Reason: "not valid hours"}}, nil //nolint:nilerr // bad hours are the caller's field error, not a failure
	}
	if errs := h.Validate(prefix); len(errs) > 0 {
		out := make([]admin.FieldError, len(errs))
		for i, e := range errs {
			out[i] = admin.FieldError{Path: e.Path, Reason: e.Reason}
		}
		return nil, out, nil
	}
	stored, err := h.Marshal()
	return stored, nil, err
}

// phoneAccounts gives A-18 identity's find-or-create by phone, so an invited
// admin's account is made exactly as a first sign-in makes one.
type phoneAccounts struct{ a *identity.Accounts }

func (p phoneAccounts) FindOrCreateByPhone(ctx context.Context, phone string, now time.Time) (admin.PhoneAccount, error) {
	acc, err := p.a.FindOrCreateByPhone(ctx, phone, now)
	return admin.PhoneAccount{UserID: acc.UserID, Status: acc.Status, Created: acc.Created}, err
}

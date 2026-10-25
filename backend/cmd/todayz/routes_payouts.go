package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/payouts"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// mountPayouts mounts W3-01's admin money: A-11 Payouts and B-10 statements
// (admin-payouts), and A-09 Credits (admin-credits: the ledger browser and
// two-person adjustments), which sit in internal/credits next to the ledger.
// There is no routes_admin-credits.go (one routes file per domain).
func mountPayouts(r chi.Router, d *deps) {
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	var staff authz.StaffResolver
	jd := jobs.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log}
	if d.DB != nil {
		jd.Pool = d.DB.Pool
		staff = business.NewStaffResolver(d.DB)
	}
	var (
		open payouts.OpenPayee
		hash *pii.Hasher
	)
	if d.Config != nil {
		if k := d.Config.Keys.PIIEncKey; k.IsSet() {
			seal, err := pii.NewSealer(k.Bytes())
			if err != nil {
				panic("payouts: PII_ENC_KEY must be 32 bytes")
			}
			open = func(f payouts.PayeeField, bid ids.ID, sealed []byte) (string, error) {
				b, err := seal.Open(business.SealField(string(f), bid), sealed)
				return string(b), err
			}
		}
		if k := d.Config.Keys.PIIHashKey; k.IsSet() {
			h, err := pii.NewHasher(k.Bytes())
			if err != nil {
				panic("payouts: PII_HASH_KEY must be 32 bytes")
			}
			hash = h
		}
	}
	svc, err := jobs.PayoutsService(jd, open)
	if err != nil {
		panic("payouts: " + err.Error())
	}
	payouts.Register(r, svc, az, staff)

	ad := credits.AdminDeps{Ledger: creditsService(d), Hash: hash}
	if d.DB != nil {
		var hashKey []byte
		if d.Config != nil {
			hashKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		ad.Audit = audit.New(d.DB, d.Clock, hashKey)
	}
	adm, err := credits.NewAdmin(ad)
	if err != nil {
		panic("credits: " + err.Error())
	}
	credits.RegisterAdmin(r, adm, az)
}

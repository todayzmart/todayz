package main

import (
	"context"
	"time"

	"github.com/go-chi/chi/v5"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// mountBusiness mounts /v1/biz/* and /v1/public/leads (W1-04). The lead form
// checks Turnstile with the identity service's verifier; the engine runs the
// second check of a staff or asks change.
func mountBusiness(r chi.Router, d *deps) {
	bd := business.Deps{Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log}
	if d.Identity != nil {
		bd.Human = d.Identity.HumanVerifier()
	}
	var az authz.Authorizer
	if d.Authz != nil {
		az, bd.Authz = d.Authz, d.Authz
	}
	svc, err := business.New(bd)
	if err != nil {
		// Only a malformed PII key gets here, and buildSecurity
		// (identity.New) refuses the same key before the routes mount.
		panic("business: " + err.Error())
	}
	business.Register(r, svc, az)
	business.RegisterField(r, business.NewField(svc, fieldDeps(d)), az)
	if d.Identity != nil {
		// D-106: an owner of a business (a draft counts) cannot delete the
		// account; a deletion that goes ahead withdraws its pending invites.
		d.Identity.AddDeletionCheck(identity.CanDeleteFunc(func(ctx context.Context, uid ids.ID) error {
			owns, err := business.OwnsActiveBusiness(ctx, d.DB, uid)
			if err != nil {
				return err
			}
			if owns {
				return problem.New(genproblems.Conflict, "This account owns a business on TodayZ. Close the business first, then delete the account.")
			}
			return nil
		}))
		d.Identity.AddDeletionHook(svc)
	}
}

// fieldDeps wires the field agent routes (W3-06): the catalogue for items,
// identity's accounts and SMS sender for the owner's code, under the sign-in
// code's rules. A missing piece leaves the routes answering 503.
func fieldDeps(d *deps) business.FieldDeps {
	fd := business.FieldDeps{Settings: d.Settings, OTP: business.FieldOTP{
		Digits: identity.OTPDigits, TTL: identity.OTPTTL, MaxAttempts: identity.OTPMaxAttempts,
		ResendGap: identity.OTPResendGap, Window: identity.OTPWindow, PerPhone: identity.OTPPerPhone,
	}}
	if d.Identity != nil {
		fd.SMS, fd.AllowTestCode = d.Identity.OwnerCodeSender(), d.Identity.AllowsTestCode()
	}
	if d.DB == nil || d.Config == nil {
		return fd
	}
	cat, err := catalogue.New(catalogue.Deps{Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log, Settings: d.Settings})
	if err != nil {
		panic("business: field catalogue: " + err.Error())
	}
	fd.Catalogue = fieldCatalogue{cat}
	k, hk := d.Config.Keys.PIIEncKey, d.Config.Keys.PIIHashKey
	if k.IsSet() && hk.IsSet() {
		seal, err := pii.NewSealer(k.Bytes())
		if err != nil {
			panic("business: PII_ENC_KEY must be 32 bytes")
		}
		hash, err := pii.NewHasher(hk.Bytes())
		if err != nil {
			panic("business: PII_HASH_KEY must be 32 bytes")
		}
		acc, err := identity.NewAccounts(d.DB, seal, hash)
		if err != nil {
			panic("business: " + err.Error())
		}
		fd.Accounts = fieldAccounts{acc}
	}
	return fd
}

// fieldAccounts gives the owner's code identity's find-or-create by phone, so
// the owner's account is made exactly as a first sign-in makes one.
type fieldAccounts struct{ a *identity.Accounts }

func (f fieldAccounts) FindOrCreateByPhone(ctx context.Context, phone string, now time.Time) (business.FieldAccount, error) {
	acc, err := f.a.FindOrCreateByPhone(ctx, phone, now)
	return business.FieldAccount{UserID: acc.UserID, Status: acc.Status}, err
}

// fieldCatalogue gives the field routes the catalogue's store items.
type fieldCatalogue struct{ c *catalogue.Service }

func (f fieldCatalogue) SearchProducts(ctx context.Context, storeID ids.ID, q, barcode *string) ([]business.CatalogueProduct, error) {
	rows, err := f.c.FieldSearchProducts(ctx, storeID, q, barcode)
	out := make([]business.CatalogueProduct, len(rows))
	for i, r := range rows {
		out[i] = business.CatalogueProduct(r)
	}
	return out, err
}

func (f fieldCatalogue) ListItems(ctx context.Context, storeID, before ids.ID, limit int32) ([]business.CatalogueItem, bool, error) {
	rows, more, err := f.c.FieldListItems(ctx, storeID, before, limit)
	out := make([]business.CatalogueItem, len(rows))
	for i, r := range rows {
		out[i] = business.CatalogueItem(r)
	}
	return out, more, err
}

func (f fieldCatalogue) Item(ctx context.Context, storeID, itemID ids.ID) (business.CatalogueItem, error) {
	it, err := f.c.FieldItem(ctx, storeID, itemID)
	return business.CatalogueItem(it), err
}

func (f fieldCatalogue) CreateItem(ctx context.Context, in business.CatalogueItemCreate) (ids.ID, error) {
	out := catalogue.FieldItemCreate{
		StoreID: in.StoreID, ProductID: in.ProductID, PricePaise: in.PricePaise, MrpPaise: in.MrpPaise,
		OpeningStock: in.OpeningStock, IsListed: in.IsListed, AgentID: in.AgentID,
	}
	if np := in.NewProduct; np != nil {
		p := catalogue.FieldNewProduct(*np)
		out.NewProduct = &p
	}
	return f.c.FieldCreateItem(ctx, out)
}

func (f fieldCatalogue) UpdateItem(ctx context.Context, in business.CatalogueItemUpdate) error {
	return f.c.FieldUpdateItem(ctx, catalogue.FieldItemUpdate(in))
}

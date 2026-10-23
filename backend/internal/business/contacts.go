package business

import (
	"context"
	"errors"
	"fmt"

	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/pii"
)

// Contacts finds a store's escalation contact for the "new order not seen"
// SMS (01 §15): the store's contact phone, else the owner's login phone.
type Contacts struct {
	db   *db.DB
	seal *pii.Sealer
}

var _ notice.StoreContacts = (*Contacts)(nil)

// NewContacts builds the lookup. PII_ENC_KEY must be set: both phones are
// sealed under it.
func NewContacts(d *db.DB, cfg *config.Config) (*Contacts, error) {
	if d == nil || cfg == nil || !cfg.Keys.PIIEncKey.IsSet() {
		return nil, errors.New("business: contacts need the database and PII_ENC_KEY")
	}
	seal, err := pii.NewSealer(cfg.Keys.PIIEncKey.Bytes())
	if err != nil {
		return nil, errors.New("business: PII_ENC_KEY must be 32 bytes")
	}
	return &Contacts{db: d, seal: seal}, nil
}

// Contact implements notice.StoreContacts. The store's own phone is sealed
// under SealField("phone", storeID); the owner's login phone under
// identity's field "phone". A phone that will not open is treated as absent
// (the inbox row is still written); nothing here carries a phone value.
func (c *Contacts) Contact(ctx context.Context, storeID ids.ID) (notice.Contact, bool, error) {
	row, err := businessdb.New(c.db.Q(ctx)).StoreEscalationContact(ctx, ids.UUID(storeID))
	if db.IsNoRows(err) {
		return notice.Contact{}, false, nil
	}
	if err != nil {
		return notice.Contact{}, false, fmt.Errorf("business: escalation contact: %w", err)
	}
	out := notice.Contact{StoreName: row.Name}
	if b, ok := ids.FromUUID(row.BusinessID); ok {
		out.BusinessID = b
	}
	if o, ok := ids.FromUUID(row.OwnerUserID); ok {
		out.OwnerUserID = o
	}
	if row.OwnerLang != nil && *row.OwnerLang != "en" {
		out.Lang = *row.OwnerLang
	}
	if len(row.StorePhoneEnc) > 0 {
		if p, err := c.seal.Open(SealField(FieldPhone, storeID), row.StorePhoneEnc); err == nil {
			out.Phone = string(p)
		}
	}
	if out.Phone == "" && len(row.OwnerPhoneEnc) > 0 {
		if p, err := c.seal.Open(identityPhoneField, row.OwnerPhoneEnc); err == nil {
			out.Phone = string(p)
		}
	}
	return out, true, nil
}

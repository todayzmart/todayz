package identity

import (
	"context"
	"strings"
	"time"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/identitydb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Saved addresses (W2-15; 01 §5.5, C-11, C-18). An address is its owner's
// only: another user's is 404, never 403. The text is personal data: it is
// never logged, and errors never carry it. Orders keep their own snapshot
// (orders.address_snapshot), so a change or a delete here never touches an
// order already placed.

// MaxAddresses is how many addresses one customer keeps. 01 has no rule for
// it; the cap only bounds the list (a question for the lead in the W2-15
// report).
const MaxAddresses = 20

// Address is a saved address as the API shows it.
type Address struct {
	ID        ids.ID
	Label     string
	Line1     string
	Line2     *string
	Landmark  *string
	Pincode   string
	Lat, Lng  float64
	IsDefault bool
	UpdatedAt time.Time
	Version   int32
}

// AddressInput is a new address.
type AddressInput struct {
	Label, Line1    string
	Line2, Landmark *string
	Pincode         string
	Lat, Lng        float64
	IsDefault       bool
}

// AddressPatch is a change; a nil field keeps the value. SetLine2 and
// SetLandmark with a nil value clear it. Lat and Lng move the pin together.
type AddressPatch struct {
	Label, Line1, Pincode *string
	SetLine2              bool
	Line2                 *string
	SetLandmark           bool
	Landmark              *string
	Lat, Lng              *float64
	IsDefault             *bool
}

// ListAddresses is the caller's saved addresses, the default first.
func (s *Service) ListAddresses(ctx context.Context, p authn.Principal) ([]Address, error) {
	if err := s.ready(); err != nil {
		return nil, unavailable(ctx, err)
	}
	rows, err := identitydb.New(s.db.Q(ctx)).ListAddresses(ctx, ids.UUID(p.UserID))
	if err != nil {
		return nil, asProblem(err)
	}
	out := make([]Address, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAddress(identitydb.UpdateAddressRow(r)))
	}
	return out, nil
}

// CreateAddress saves a new address. The first one is the default; a new
// default clears the old one.
func (s *Service) CreateAddress(ctx context.Context, p authn.Principal, in AddressInput) (Address, error) {
	if err := s.ready(); err != nil {
		return Address{}, unavailable(ctx, err)
	}
	label, line1, err := addressText(in.Label, in.Line1)
	if err != nil {
		return Address{}, err
	}
	var out Address
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		if err := q.LockKey(ctx, addressLockKey(p.UserID)); err != nil {
			return err
		}
		n, err := q.CountAddresses(ctx, ids.UUID(p.UserID))
		if err != nil {
			return err
		}
		if n >= MaxAddresses {
			return fieldErr("body", "you already keep the most addresses allowed; delete one first")
		}
		now := s.clock.Now()
		id := ids.New(now)
		isDefault := in.IsDefault || n == 0
		if isDefault {
			if err := q.ClearOtherDefaults(ctx, identitydb.ClearOtherDefaultsParams{UserID: ids.UUID(p.UserID), ID: ids.UUID(id), Now: now}); err != nil {
				return err
			}
		}
		row, err := q.InsertAddress(ctx, identitydb.InsertAddressParams{
			ID: ids.UUID(id), UserID: ids.UUID(p.UserID), Label: label, Line1: line1,
			Line2: optionalText(in.Line2), Landmark: optionalText(in.Landmark), Pincode: in.Pincode,
			Lat: in.Lat, Lng: in.Lng, IsDefault: isDefault, Now: now,
		})
		if err != nil {
			return err
		}
		out = toAddress(identitydb.UpdateAddressRow(row))
		return nil
	})
	if err != nil {
		return Address{}, asProblem(err)
	}
	return out, nil
}

// UpdateAddress changes the caller's address if version is still current
// (412 otherwise). The default cannot be unset directly: another address is
// made the default instead.
func (s *Service) UpdateAddress(ctx context.Context, p authn.Principal, id ids.ID, version int32, patch AddressPatch) (Address, error) {
	if err := s.ready(); err != nil {
		return Address{}, unavailable(ctx, err)
	}
	if patch.Label != nil {
		v := strings.TrimSpace(*patch.Label)
		if v == "" {
			return Address{}, fieldErr("body.label", "must not be blank")
		}
		patch.Label = &v
	}
	if patch.Line1 != nil {
		v := strings.TrimSpace(*patch.Line1)
		if v == "" {
			return Address{}, fieldErr("body.line1", "must not be blank")
		}
		patch.Line1 = &v
	}
	if (patch.Lat == nil) != (patch.Lng == nil) {
		return Address{}, fieldErr("body.lat", "send lat and lng together")
	}
	var out Address
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		if err := q.LockKey(ctx, addressLockKey(p.UserID)); err != nil {
			return err
		}
		cur, err := q.LockAddress(ctx, identitydb.LockAddressParams{ID: ids.UUID(id), UserID: ids.UUID(p.UserID)})
		if db.IsNoRows(err) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if cur.Version != version {
			return preconditionFailed()
		}
		if patch.IsDefault != nil && !*patch.IsDefault && cur.IsDefault {
			return fieldErr("body.isDefault", "make another address the default instead")
		}
		now := s.clock.Now()
		if patch.IsDefault != nil && *patch.IsDefault && !cur.IsDefault {
			if err := q.ClearOtherDefaults(ctx, identitydb.ClearOtherDefaultsParams{UserID: ids.UUID(p.UserID), ID: ids.UUID(id), Now: now}); err != nil {
				return err
			}
		}
		params := identitydb.UpdateAddressParams{
			Label: patch.Label, Line1: patch.Line1, Pincode: patch.Pincode, IsDefault: patch.IsDefault,
			SetLine2: patch.SetLine2, Line2: optionalText(patch.Line2),
			SetLandmark: patch.SetLandmark, Landmark: optionalText(patch.Landmark),
			Now: now, ID: ids.UUID(id), UserID: ids.UUID(p.UserID),
		}
		if patch.Lat != nil {
			params.SetLocation, params.Lat, params.Lng = true, *patch.Lat, *patch.Lng
		}
		row, err := q.UpdateAddress(ctx, params)
		if err != nil {
			return err
		}
		out = toAddress(row)
		return nil
	})
	if err != nil {
		return Address{}, asProblem(err)
	}
	return out, nil
}

// DeleteAddress deletes the caller's address. Deleting the default makes
// the most recently changed remaining one the default.
func (s *Service) DeleteAddress(ctx context.Context, p authn.Principal, id ids.ID) error {
	if err := s.ready(); err != nil {
		return unavailable(ctx, err)
	}
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		if err := q.LockKey(ctx, addressLockKey(p.UserID)); err != nil {
			return err
		}
		wasDefault, err := q.DeleteAddress(ctx, identitydb.DeleteAddressParams{ID: ids.UUID(id), UserID: ids.UUID(p.UserID)})
		if db.IsNoRows(err) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if wasDefault {
			return q.PromoteNewestAddress(ctx, identitydb.PromoteNewestAddressParams{UserID: ids.UUID(p.UserID), Now: s.clock.Now()})
		}
		return nil
	})
	if err != nil {
		return asProblem(err)
	}
	return nil
}

// addressLockKey serialises one user's address changes (the cap and the one
// default) across instances until the transaction ends.
func addressLockKey(user ids.ID) string { return "addresses:" + user.String() }

func addressText(label, line1 string) (string, string, error) {
	label, line1 = strings.TrimSpace(label), strings.TrimSpace(line1)
	if label == "" {
		return "", "", fieldErr("body.label", "must not be blank")
	}
	if line1 == "" {
		return "", "", fieldErr("body.line1", "must not be blank")
	}
	return label, line1, nil
}

// optionalText trims an optional line; blank is no value.
func optionalText(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

func toAddress(r identitydb.UpdateAddressRow) Address {
	id, _ := ids.FromUUID(r.ID)
	return Address{
		ID: id, Label: r.Label, Line1: r.Line1, Line2: r.Line2, Landmark: r.Landmark, Pincode: r.Pincode,
		Lat: r.Lat, Lng: r.Lng, IsDefault: r.IsDefault, UpdatedAt: r.UpdatedAt.UTC(), Version: r.Version,
	}
}

func fieldErr(path, reason string) *problem.Error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

func notFound() *problem.Error { return problem.New(genproblems.NotFound, "") }

func preconditionFailed() *problem.Error { return problem.New(genproblems.PreconditionFailed, "") }

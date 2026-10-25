package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	meapi "github.com/todayz/backend/gen/api/me"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/ids"
)

func (f *fixture) customer(phone string) authn.Principal {
	f.t.Helper()
	in := f.signIn(phone, authn.AppCustomer)
	p, err := f.svc.tokens.Verify(in.AccessToken)
	require.NoError(f.t, err)
	return p
}

func home(label string) AddressInput {
	landmark := "Near the park"
	return AddressInput{Label: label, Line1: "12, 4th Cross, Indiranagar", Landmark: &landmark, Pincode: "560038", Lat: 12.9719, Lng: 77.6412}
}

func TestAddressesCRUD(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	p := f.customer("+919876543210")
	f.remember("Indiranagar", "Near the park", "560038")
	ctx := f.ctx("10.0.0.1")

	list, err := f.svc.ListAddresses(ctx, p)
	require.NoError(t, err)
	require.Empty(t, list)

	a, err := f.svc.CreateAddress(ctx, p, home("  Home "))
	require.NoError(t, err)
	require.Equal(t, "Home", a.Label, "trimmed")
	require.True(t, a.IsDefault, "the first address is the default")
	require.InDelta(t, 12.9719, a.Lat, 1e-9)
	require.InDelta(t, 77.6412, a.Lng, 1e-9)
	require.Equal(t, int32(1), a.Version)

	b, err := f.svc.CreateAddress(ctx, p, home("Office"))
	require.NoError(t, err)
	require.False(t, b.IsDefault)

	// A new default clears the old one, and the default lists first.
	c, err := f.svc.CreateAddress(ctx, p, AddressInput{Label: "Mum", Line1: "3 Church St", Pincode: "560001", Lat: 12.97, Lng: 77.6, IsDefault: true})
	require.NoError(t, err)
	require.True(t, c.IsDefault)
	list, err = f.svc.ListAddresses(ctx, p)
	require.NoError(t, err)
	require.Len(t, list, 3)
	require.Equal(t, c.ID, list[0].ID)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM addresses WHERE user_id = $1 AND is_default`, ids.UUID(p.UserID)))

	// Change: If-Match, keep what is absent, clear landmark, move the pin.
	_, err = f.svc.UpdateAddress(ctx, p, b.ID, b.Version+1, AddressPatch{Label: ptr("Work")})
	requireCode(t, err, genproblems.PreconditionFailed, "a stale If-Match")
	lat, lng := 12.98, 77.65
	up, err := f.svc.UpdateAddress(ctx, p, b.ID, b.Version, AddressPatch{Label: ptr("Work"), SetLandmark: true, Lat: &lat, Lng: &lng})
	require.NoError(t, err)
	require.Equal(t, "Work", up.Label)
	require.Equal(t, b.Line1, up.Line1)
	require.Nil(t, up.Landmark)
	require.InDelta(t, lat, up.Lat, 1e-9)
	require.Equal(t, b.Version+1, up.Version)
	_, err = f.svc.UpdateAddress(ctx, p, b.ID, up.Version, AddressPatch{Lat: &lat})
	requireCode(t, err, genproblems.ValidationFailed, "lat without lng")
	_, err = f.svc.UpdateAddress(ctx, p, b.ID, up.Version, AddressPatch{Label: ptr("  ")})
	requireCode(t, err, genproblems.ValidationFailed, "a blank label")

	// The default cannot be unset; making another the default moves it.
	_, err = f.svc.UpdateAddress(ctx, p, c.ID, c.Version, AddressPatch{IsDefault: ptr(false)})
	requireCode(t, err, genproblems.ValidationFailed, "unset the default")
	up, err = f.svc.UpdateAddress(ctx, p, up.ID, up.Version, AddressPatch{IsDefault: ptr(true)})
	require.NoError(t, err)
	require.True(t, up.IsDefault)
	list, err = f.svc.ListAddresses(ctx, p)
	require.NoError(t, err)
	require.Equal(t, up.ID, list[0].ID)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM addresses WHERE user_id = $1 AND is_default`, ids.UUID(p.UserID)))

	// Deleting the default promotes the most recently changed remaining one.
	require.NoError(t, f.svc.DeleteAddress(ctx, p, up.ID))
	list, err = f.svc.ListAddresses(ctx, p)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.True(t, list[0].IsDefault)
	require.Equal(t, c.ID, list[0].ID, "Mum changed last (its default was cleared)")
	requireCode(t, f.svc.DeleteAddress(ctx, p, up.ID), genproblems.NotFound, "already deleted")
	require.NoError(t, f.svc.DeleteAddress(ctx, p, a.ID))
	require.NoError(t, f.svc.DeleteAddress(ctx, p, c.ID))
	list, err = f.svc.ListAddresses(ctx, p)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestAddressesAreTheOwnersOnly(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	asha := f.customer("+919876543210")
	ravi := f.customer("+919876543211")
	ctx := f.ctx("10.0.0.1")
	a, err := f.svc.CreateAddress(ctx, asha, home("Home"))
	require.NoError(t, err)

	list, err := f.svc.ListAddresses(ctx, ravi)
	require.NoError(t, err)
	require.Empty(t, list, "another user's address never appears")
	_, err = f.svc.UpdateAddress(ctx, ravi, a.ID, a.Version, AddressPatch{Label: ptr("Mine")})
	requireCode(t, err, genproblems.NotFound, "update another user's address")
	requireCode(t, f.svc.DeleteAddress(ctx, ravi, a.ID), genproblems.NotFound, "delete another user's address")
	_, err = f.svc.UpdateAddress(ctx, asha, ids.New(t0), 1, AddressPatch{Label: ptr("x")})
	requireCode(t, err, genproblems.NotFound, "an id that does not exist")

	// Ravi's first address is his default; Asha's stays hers.
	r, err := f.svc.CreateAddress(ctx, ravi, home("Home"))
	require.NoError(t, err)
	require.True(t, r.IsDefault)
	list, err = f.svc.ListAddresses(ctx, asha)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.True(t, list[0].IsDefault)
}

func TestAddressCap(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	// The cap is customer_max_saved_addresses (D-222): lowered here to prove
	// it is read, not the default of 20.
	_, err := f.db.Pool.Exec(context.Background(), `UPDATE platform_settings SET value = '3' WHERE key = 'customer_max_saved_addresses'`)
	require.NoError(t, err)
	p := f.customer("+919876543210")
	ctx := f.ctx("10.0.0.1")
	for i := range 3 {
		_, err := f.svc.CreateAddress(ctx, p, home(fmt.Sprintf("Place %d", i)))
		require.NoError(t, err)
	}
	_, err = f.svc.CreateAddress(ctx, p, home("One more"))
	requireCode(t, err, genproblems.ValidationFailed, "over the cap")
	require.Contains(t, problemOf(t, err).Fields, "body")
}

// TestAddressTextRefusesControlCharacters: Postgres refuses a NUL, which was
// a 500. Every text field refuses control characters and invalid UTF-8 with a
// 422 on its own path, on create and on patch (D-222).
func TestAddressTextRefusesControlCharacters(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	p := f.customer("+919876543210")
	ctx := f.ctx("10.0.0.1")
	a, err := f.svc.CreateAddress(ctx, p, home("Home"))
	require.NoError(t, err)

	bad := map[string]string{"NUL": "Home\x00", "newline": "Home\nOffice", "escape": "\x1b[31m", "DEL": "Ho\x7fme", "invalid UTF-8": "Ho\xffme"}
	for name, v := range bad {
		for _, field := range []string{"label", "line1", "line2", "landmark"} {
			in := home("Office")
			patch := AddressPatch{}
			switch field {
			case "label":
				in.Label, patch.Label = v, &v
			case "line1":
				in.Line1, patch.Line1 = v, &v
			case "line2":
				in.Line2, patch.SetLine2, patch.Line2 = &v, true, &v
			case "landmark":
				in.Landmark, patch.SetLandmark, patch.Landmark = &v, true, &v
			}
			_, err := f.svc.CreateAddress(ctx, p, in)
			requireCode(t, err, genproblems.ValidationFailed, name+" in "+field+" on create")
			require.Contains(t, problemOf(t, err).Fields, "body."+field, name+" in "+field)
			_, err = f.svc.UpdateAddress(ctx, p, a.ID, a.Version, patch)
			requireCode(t, err, genproblems.ValidationFailed, name+" in "+field+" on patch")
			require.Contains(t, problemOf(t, err).Fields, "body."+field, name+" in "+field)
		}
	}
	// Nothing was saved, and the address did not change.
	list, err := f.svc.ListAddresses(ctx, p)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, a.Version, list[0].Version)
}

func TestAddressHandlers(t *testing.T) {
	f := newFixture(t, config.EnvTest)
	p := f.customer("+919876543210")
	h := meHandlers{f.svc}
	ctx := authn.WithPrincipal(f.ctx("10.0.0.1"), p)

	created, err := h.CreateAddress(ctx, meapi.CreateAddressRequestObject{Body: &meapi.AddressCreate{
		Label: "Home", Line1: "12, 4th Cross", Landmark: ptr("Near the park"), Pincode: "560038", Lat: 12.9719, Lng: 77.6412,
	}})
	require.NoError(t, err)
	res := created.(meapi.CreateAddress201JSONResponse)
	require.Equal(t, `"v1"`, *res.Headers.ETag)
	require.True(t, res.Body.IsDefault)

	upd := meapi.UpdateAddressRequestObject{AddressId: res.Body.AddressId, Body: &meapi.AddressPatch{}}
	_, err = h.UpdateAddress(ctx, upd)
	requireCode(t, err, genproblems.PreconditionRequired, "no If-Match")

	// `"landmark": null` clears it: the key's presence comes from patchKeys.
	upd.Params.IfMatch = res.Headers.ETag
	keys := map[string]json.RawMessage{"landmark": json.RawMessage("null")}
	out, err := h.UpdateAddress(context.WithValue(ctx, patchKeysKey{}, keys), upd)
	require.NoError(t, err)
	got := out.(meapi.UpdateAddress200JSONResponse)
	require.Nil(t, got.Body.Landmark)
	require.Equal(t, `"v2"`, *got.Headers.ETag)

	_, err = h.DeleteAddress(ctx, meapi.DeleteAddressRequestObject{AddressId: "not-a-ulid"})
	requireCode(t, err, genproblems.NotFound, "a malformed id")
	_, err = h.DeleteAddress(ctx, meapi.DeleteAddressRequestObject{AddressId: res.Body.AddressId})
	require.NoError(t, err)
	listed, err := h.ListMyAddresses(ctx, meapi.ListMyAddressesRequestObject{})
	require.NoError(t, err)
	require.Empty(t, listed.(meapi.ListMyAddresses200JSONResponse).Addresses)
}

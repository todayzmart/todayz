package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// The saved addresses through the router (W2-15): another customer's address
// is 404 on PATCH and DELETE, a PATCH without If-Match is 428, a business
// token is 403, and a NUL in a text field is 422, not 500. Bodies are never
// printed.
func TestAddressesHTTP(t *testing.T) {
	o := newOrderShop(t)
	body := map[string]any{"label": "Home", "line1": "12, 4th Cross", "pincode": "560038", "lat": 12.9719, "lng": 77.6412}
	c := o.a.call(o.cust, http.MethodPost, "/v1/me/addresses", body, idem(o.a))
	require.Equal(t, http.StatusCreated, c.code, "status %d", c.code)
	id, etag := c.body["addressId"].(string), c.hdr.Get("ETag")
	require.Equal(t, `"v1"`, etag)
	path := "/v1/me/addresses/" + id
	patch := map[string]any{"label": "Office"}

	r := o.a.call(o.cust2, http.MethodPatch, path, patch, map[string]string{"If-Match": etag})
	require.Equal(t, http.StatusNotFound, r.code, "another customer's PATCH: status %d", r.code)
	r = o.a.call(o.cust2, http.MethodDelete, path, nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, "another customer's DELETE: status %d", r.code)

	r = o.a.call(o.cust, http.MethodPatch, path, patch, nil)
	require.Equal(t, http.StatusPreconditionRequired, r.code, "no If-Match: status %d", r.code)

	for _, m := range []string{http.MethodGet, http.MethodPost} {
		var b any
		if m == http.MethodPost {
			b = body
		}
		r = o.a.call(o.cashier, m, "/v1/me/addresses", b, idem(o.a))
		require.Equal(t, http.StatusForbidden, r.code, "a business token, %s: status %d", m, r.code)
	}
	r = o.a.call(o.cashier, http.MethodDelete, path, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a business token, DELETE: status %d", r.code)

	r = o.a.call(o.cust, http.MethodPatch, path, map[string]any{"landmark": "Near\x00the park"}, map[string]string{"If-Match": etag})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "a NUL: status %d", r.code)

	// Nothing above changed it.
	l := o.a.call(o.cust, http.MethodGet, "/v1/me/addresses", nil, nil)
	require.Equal(t, http.StatusOK, l.code, "status %d", l.code)
	items := l.body["addresses"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, "Home", items[0].(map[string]any)["label"])
	require.EqualValues(t, 1, items[0].(map[string]any)["version"])
}

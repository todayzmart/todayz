package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCreditAccountHTTP: the business credit account (B-10) is the owner's
// and the managers'; a manager limited to some stores sees only those stores
// (D-125); a cashier and another business's owner are refused.
func TestCreditAccountHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	a := newBizAPI(t)
	owner := a.signIn(ownerPhone, "business")
	bx, s1 := a.newBusiness(owner, "Lakshmi Traders")
	X := "/v1/biz/businesses/" + bx
	r := a.call(owner, http.MethodPost, X+"/stores", map[string]any{"name": "Lakshmi Traders 2", "locality": "BTM Layout"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	s2 := r.body["id"].(string)

	join := func(phone, role string, stores []string) string {
		t.Helper()
		body := map[string]any{"phone": phone, "role": role}
		if stores != nil {
			body["storeIds"] = stores
		}
		r := a.call(owner, http.MethodPost, X+"/staff", body, nil)
		require.Equal(t, http.StatusCreated, r.code, r.raw)
		token := a.signIn(phone, "business")
		r = a.call(token, http.MethodPost, X+"/staff/"+r.body["id"].(string)+"/accept", nil, nil)
		require.Equal(t, http.StatusOK, r.code, r.raw)
		return token
	}
	manager := join(managerPhone, "manager", nil)
	limited := join(manager2Phone, "manager", []string{s2})
	cashier := join(cashierPhone, "cashier", []string{s1})

	ownerY := a.signIn(otherOwnerPhone, "business")
	by, _ := a.newBusiness(ownerY, "Ganesh Stores")

	stores := func(token string) []string {
		t.Helper()
		r := a.call(token, http.MethodGet, X+"/credit-account", nil, nil)
		require.Equal(t, http.StatusOK, r.code, r.raw)
		var out []string
		for _, s := range r.body["stores"].([]any) {
			out = append(out, s.(map[string]any)["storeId"].(string))
		}
		return out
	}
	require.ElementsMatch(t, []string{s1, s2}, stores(owner))
	require.ElementsMatch(t, []string{s1, s2}, stores(manager), "a manager of every store")
	require.Equal(t, []string{s2}, stores(limited), "D-125: only the manager's store")

	for _, c := range []struct {
		name, token, path string
		codes             []int
	}{
		{"nobody", "", X + "/credit-account", []int{http.StatusUnauthorized}},
		{"a cashier", cashier, X + "/credit-account", []int{http.StatusForbidden}},
		{"Y's owner on X", ownerY, X + "/credit-account", []int{http.StatusForbidden, http.StatusNotFound}},
		{"X's owner on Y", owner, "/v1/biz/businesses/" + by + "/credit-account", []int{http.StatusForbidden, http.StatusNotFound}},
		{"X's limited manager on Y", limited, "/v1/biz/businesses/" + by + "/credit-account", []int{http.StatusForbidden, http.StatusNotFound}},
	} {
		r := a.call(c.token, http.MethodGet, c.path, nil, nil)
		require.Contains(t, c.codes, r.code, "%s: %s", c.name, r.raw)
		require.Contains(t, r.raw, `"code"`, "%s: a problem body", c.name)
	}
}

// TestCollectPaysPlacedCashbackHTTP: an order pays the cashback rate its
// store showed when it was placed (D-124), even after the shop changes it.
func TestCollectPaysPlacedCashbackHTTP(t *testing.T) {
	o := newOrderShop(t)
	o.a.exec(`UPDATE stores SET cashback_bp = 500 WHERE id = $1`, uuidOf(t, o.sid))
	r := o.place(o.cust, line{o.butter, 2}, line{o.atta, 1})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM orders WHERE id = $1 AND cashback_bp = 500`, uuidOf(t, id)))

	o.a.exec(`UPDATE stores SET cashback_bp = 1000 WHERE id = $1`, uuidOf(t, o.sid))
	r = o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = o.verify(o.cashier, id, o.code(id))
	require.Equal(t, http.StatusOK, r.code, "verify: status %d", r.code)
	require.Equal(t, "collected", r.body["status"])

	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM orders
	  WHERE id = $1 AND cashback_credits > 0 AND cashback_credits = items_after_offers_paise * 500 / 1000000`, uuidOf(t, id)),
		"5%, the placed rate, not the store's 10% now")
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'cashback' AND ref_id = $1 AND memo = 'bp:500'`, uuidOf(t, id)))
}

package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
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

// grantCredits gives o's customer n credits (a referral reward) and returns
// the customer's id. It places one order to learn the id.
func (o *orderShop) grantCredits(n int64) pgtype.UUID {
	t := o.t
	t.Helper()
	var cust pgtype.UUID
	require.NoError(t, o.a.tdb.OwnerPool.QueryRow(context.Background(), `SELECT customer_id FROM orders WHERE id = $1`, uuidOf(t, o.order())).Scan(&cust))
	custID, ok := ids.FromUUID(cust)
	require.True(t, ok)
	st := settings.NewCache(settings.PGLoader(o.a.tdb.Pool), settings.Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	svc, err := credits.New(credits.Deps{DB: db.New(o.a.tdb.Pool), Clock: o.a.clk, Settings: st})
	require.NoError(t, err)
	_, err = svc.Post(context.Background(), credits.Posting{Kind: credits.TxReferralReward, IdempotencyKey: "referral:grant-" + custID.String(),
		Legs: []credits.Leg{{Account: credits.Account{Kind: credits.PlatformMarketing}, Amount: -n}, {Account: credits.Account{Kind: credits.CustomerWallet, Owner: custID}, Amount: n}}})
	require.NoError(t, err)
	return cust
}

// placeWithCredits places butter×2 + atta using n credits and checks the hold.
func (o *orderShop) placeWithCredits(n int) string {
	t := o.t
	t.Helper()
	lines := []line{{o.butter, 2}, {o.atta, 1}}
	body := o.cart(lines, o.total(lines)-n*100) // a credit is ₹1
	body["creditsToUse"] = n
	r := o.a.call(o.cust, http.MethodPost, "/v1/me/orders", body, idem(o.a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM credit_holds WHERE order_id = $1 AND status = 'held' AND amount = $2`, uuidOf(t, id), n))
	return id
}

func (o *orderShop) walletIs(cust pgtype.UUID, n int64) bool {
	return o.a.count(`SELECT count(*) FROM account_balances b JOIN ledger_accounts a ON a.id = b.account_id
	  WHERE a.kind = 'customer_wallet' AND a.owner_id = $1 AND b.balance = $2`, cust, n) == 1
}

// TestEndReleasesHoldHTTP: end() releases the credit hold whatever the order
// row says (D-127). A real hold with orders.credits_used forced to 0 is still
// released when the customer cancels, when the accept timer expires the
// order and on a no-show.
func TestEndReleasesHoldHTTP(t *testing.T) {
	o := newOrderShop(t)
	cust := o.grantCredits(40)
	place := func() string {
		t.Helper()
		id := o.placeWithCredits(10)
		// ck_orders_money ties the total to credits_used, so raise it with it.
		o.a.exec(`UPDATE orders SET credits_used = 0, total_paise = total_paise + credits_used::bigint * 100 WHERE id = $1`, uuidOf(t, id))
		return id
	}
	released := func(id string) int {
		return o.a.count(`SELECT count(*) FROM credit_holds WHERE order_id = $1 AND status = 'released' AND released = 10`, uuidOf(t, id))
	}

	id := place()
	r := o.me(o.cust, id, "cancel", map[string]any{"reason": "changed_mind"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 1, released(id), "cancelled: released although credits_used says 0")

	id = place()
	o.advance(10 * time.Minute)
	expiredRun(t, o.timer(orders.KindAcceptTimeout, id, nil))
	require.Equal(t, 1, released(id), "not accepted: released although credits_used says 0")

	id = place()
	r = o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	_, hold := o.times(id)
	o.advance(30 * time.Minute)
	expiredRun(t, o.timer(orders.KindNoShow, id, hold))
	require.Equal(t, 1, released(id), "no-show: released although credits_used says 0")
	require.True(t, o.walletIs(cust, 40), "every credit back in the wallet")
}

// TestCollectCapturesHoldHTTP: collection captures the order's credit hold.
func TestCollectCapturesHoldHTTP(t *testing.T) {
	o := newOrderShop(t)
	cust := o.grantCredits(40)
	id := o.placeWithCredits(10)
	r := o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = o.verify(o.cashier, id, o.code(id))
	require.Equal(t, http.StatusOK, r.code, "verify: status %d", r.code)
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM credit_holds WHERE order_id = $1 AND status = 'captured' AND captured = 10`, uuidOf(t, id)))
	require.True(t, o.walletIs(cust, 30))
}

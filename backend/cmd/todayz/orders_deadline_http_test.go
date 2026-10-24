package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Deadlines are strict and have no grace (lead, W2-02 finish): a second
// after accept_by the shop can no longer accept, accept part or reject, and
// a second after partial_confirm_by the customer can no longer confirm or
// decline. Each is 409 invalid_transition and changes nothing; the timer or
// the sweep ends the order.
func TestDeadlinesRefuseOneSecondLate(t *testing.T) {
	o := newOrderShop(t)

	late := func(what string, r resp) {
		t.Helper()
		require.Equal(t, http.StatusConflict, r.code, "%s: %s", what, r.raw)
		require.Equal(t, "invalid_transition", problemCode(r), what)
	}

	// The shop, a second after accept_by.
	for _, action := range []string{"accept", "accept part", "reject"} {
		id := o.orderOf(line{o.butter, 2}, line{o.salt, 1})
		acceptBy, _ := o.times(id)
		o.set(acceptBy.Add(time.Second))
		var r resp
		switch action {
		case "accept":
			r = o.staff(o.cashier, id, "accept", map[string]any{})
		case "accept part":
			r = o.partial(id, 1, 1)
		case "reject":
			r = o.staff(o.cashier, id, "reject", map[string]any{"reason": "out_of_stock"})
		}
		late(action, r)
		require.Equal(t, "placed", o.status(id), action)
		require.Zero(t, o.held(id), action)
	}

	// The control: a second before accept_by the shop still accepts.
	id := o.order()
	acceptBy, _ := o.times(id)
	o.set(acceptBy.Add(-time.Second))
	r := o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// The customer, a second after partial_confirm_by.
	for _, accept := range []bool{true, false} {
		id := o.orderOf(line{o.butter, 2}, line{o.salt, 1})
		r := o.partial(id, 1, 1)
		require.Equal(t, http.StatusOK, r.code, r.raw)
		confirmBy := o.money(id).confirmBy
		require.NotNil(t, confirmBy)
		o.set(confirmBy.Add(time.Second))
		r = o.me(o.cust, id, "confirm-partial", map[string]any{"accept": accept})
		late("confirm-partial", r)
		require.Equal(t, "awaiting_customer", o.status(id))
		require.Zero(t, o.held(id))
	}
}

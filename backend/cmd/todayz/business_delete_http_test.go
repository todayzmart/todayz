package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// deleteMe runs DELETE /v1/me with a fresh step_up code for phone.
func (a *bizAPI) deleteMe(token, phone, app string) resp {
	a.t.Helper()
	w := a.call("", http.MethodPost, "/v1/auth/otp/send", map[string]string{
		"phone": phone, "app": app, "purpose": "step_up", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
	}, nil)
	require.Equal(a.t, http.StatusOK, w.code, w.raw)
	return a.call(token, http.MethodDelete, "/v1/me", map[string]string{
		"otpRequestId": w.body["requestId"].(string), "code": testOTP,
	}, nil)
}

// D-106: the owner of a business (a draft counts) cannot delete the account,
// and a deleted account's pending invites are withdrawn (W1-04 M2).
func TestBusinessAccountDeletion(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	a := newBizAPI(t)
	owner := a.signIn(ownerPhone, "business")
	r := a.call(owner, http.MethodPost, "/v1/biz/businesses", map[string]any{"brandName": "Lakshmi Traders"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	B := "/v1/biz/businesses/" + r.body["id"].(string)

	// The invited person has an account, so the invite is bound to it.
	invitee := a.signIn(cashier2Phone, "customer")
	r = a.call(owner, http.MethodPost, B+"/staff", map[string]any{"phone": cashier2Phone, "role": "cashier"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM staff WHERE status = 'invited'`))

	// Deleting is a customer-app call; the account is the same person.
	r = a.deleteMe(a.signIn(ownerPhone, "customer"), ownerPhone, "customer")
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "conflict", r.body["code"])
	require.Equal(t, 0, a.count(`SELECT count(*) FROM users WHERE status = 'deleted'`), "the refused deletion changed nothing")

	r = a.deleteMe(invitee, cashier2Phone, "customer")
	require.Equal(t, http.StatusNoContent, r.code, r.raw)
	require.Equal(t, 0, a.count(`SELECT count(*) FROM staff WHERE status = 'invited'`), "the invite is withdrawn")
	require.Equal(t, 0, a.count(`SELECT count(*) FROM staff WHERE invite_phone_enc IS NOT NULL AND status <> 'owner'`), "and its phone cleared")
}

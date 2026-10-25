package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
)

// W3-02 A-13 and A-10 through the router and the real policy: support reads
// the risk queue and a flag but clears, confirms, lists and rejects nothing
// (403); an admin clears the same flag.
func TestAdminRiskHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "support")
	tok := desk.token()
	a := o.a
	at := a.clk.Now()
	store := o.S[strings.LastIndex(o.S, "/")+1:]
	flag := ids.New(at).String()
	a.exec(`INSERT INTO risk_flags (id, subject_type, subject_id, rule, score, created_at, updated_at)
	  VALUES ($1, 'store', $2, 'pos.reversal_ratio', 60, $3, $3)`, uuidOf(t, flag), uuidOf(t, store), at)
	ref := ids.New(at).String()
	call := func(method, path string, body any) resp { t.Helper(); return a.call(tok, method, path, body, nil) }
	verdict := map[string]any{"from": "open", "reason": "checked the counter"}

	r := call(http.MethodGet, "/v1/admin/risk/flags", nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = call(http.MethodGet, "/v1/admin/risk/flags/"+flag, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/v1/admin/risk/flags/" + flag + "/clear", verdict},
		{http.MethodPost, "/v1/admin/risk/flags/" + flag + "/confirm", map[string]any{"from": "open", "reason": "abuse"}},
		{http.MethodGet, "/v1/admin/referrals", nil},
		{http.MethodPost, "/v1/admin/referrals/" + ref + "/reject", map[string]any{"reason": "abuse"}},
	} {
		r = call(c.method, c.path, c.body)
		require.Equal(t, http.StatusForbidden, r.code, "%s %s: %s", c.method, c.path, r.raw)
	}
	require.Equal(t, 1, a.count(`SELECT count(*) FROM risk_flags WHERE id = $1 AND status = 'open'`, uuidOf(t, flag)))
	require.Zero(t, a.count(`SELECT count(*) FROM audit_log WHERE action LIKE 'risk.%' OR action = 'referral.reject'`))

	a.exec(`UPDATE admin_users SET roles = ARRAY['admin']`)
	tok = desk.token()
	r = call(http.MethodPost, "/v1/admin/risk/flags/"+flag+"/clear", verdict)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "cleared", r.body["status"])
	r = call(http.MethodGet, "/v1/admin/referrals", nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
}

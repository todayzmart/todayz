package credits

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

type aenv struct {
	*denv
	adm               *Admin
	finance, finance2 authn.Principal
	customer          ids.ID
	account           string
}

func newAdjustEnv(t *testing.T) *aenv {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	e := newDBEnv(t)
	adm, err := NewAdmin(AdminDeps{Ledger: e.svc, Audit: audit.New(db.New(e.tdb.Pool), e.clk, nil)})
	require.NoError(t, err)
	a := &aenv{denv: e, adm: adm}
	a.finance = authn.Principal{UserID: e.user(), App: authn.AppAdmin, Roles: []string{"finance"}}
	a.finance2 = authn.Principal{UserID: e.user(), App: authn.AppAdmin, Roles: []string{"super_admin"}}
	a.customer = e.user()
	e.grant(a.customer, 1000)
	var u pgtype.UUID
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx,
		`SELECT id FROM ledger_accounts WHERE kind = 'customer_wallet' AND owner_id = $1`, ids.UUID(a.customer)).Scan(&u))
	id, ok := ids.FromUUID(u)
	require.True(t, ok)
	a.account = id.String()
	return a
}

func (a *aenv) adjust(p authn.Principal, paise int) (string, string, error) {
	r, err := a.adm.CreateAdjustment(a.ctx, p, AdjustmentInput{AccountID: a.account, AmountPaise: paise, Reason: "goodwill for a late pickup"})
	if err != nil {
		return "", "", err
	}
	id := ""
	if r.Request != nil {
		id = r.Request.Id
	}
	return string(r.Outcome), id, nil
}

func (a *aenv) wallet() int64 { return a.balance(wallet(a.customer)) }

// SC-CR-15: whole credits; |amount| above the threshold (exclusive) waits
// for a second finance or super_admin; at or under it posts at once.
func TestAdjustmentThresholdDB(t *testing.T) {
	a := newAdjustEnv(t)
	threshold, err := a.st.Paise(a.ctx, settings.AdminAdjustDualApprovalAbovePaise)
	require.NoError(t, err)
	require.EqualValues(t, 50000, threshold, "01 §17 default")

	start := a.wallet()
	for _, paise := range []int{20000, -20000, 50000, -50000} {
		out, id, err := a.adjust(a.finance, paise)
		require.NoError(t, err, paise)
		require.Equal(t, "posted", out, paise)
		require.Empty(t, id)
	}
	require.Equal(t, start, a.wallet(), "±₹200 and ±₹500 net to zero")
	require.Zero(t, a.count(`SELECT count(*) FROM admin_adjustment_requests`), "no request row at or under the threshold")
	require.Equal(t, 4, a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'admin_adjustment'`))
	require.Equal(t, 4, a.count(`SELECT count(*) FROM audit_log WHERE action = 'credits.adjust' AND actor_id = $1`, ids.UUID(a.finance.UserID)))

	for _, paise := range []int{50100, -50100, 60000, -60000} {
		out, id, err := a.adjust(a.finance, paise)
		require.NoError(t, err, paise)
		require.Equal(t, "pending", out, paise)
		require.NotEmpty(t, id)
	}
	require.Equal(t, start, a.wallet(), "a pending request moves nothing")
	require.Equal(t, 4, a.count(`SELECT count(*) FROM admin_adjustment_requests WHERE status = 'pending'`))
	require.Equal(t, 4, a.count(`SELECT count(*) FROM audit_log WHERE action = 'credits.adjustment_request'`))

	// A fraction of a credit, or zero, is refused before anything is written.
	for _, paise := range []int{0, 50, 20050, -150, 60001} {
		_, _, err := a.adjust(a.finance, paise)
		require.Equal(t, "validation_failed", problem.CodeOf(err), paise)
	}

	// The threshold is read from platform_settings.
	a.setting(settings.AdminAdjustDualApprovalAbovePaise, "10000")
	out, _, err := a.adjust(a.finance, 20000)
	require.NoError(t, err)
	require.Equal(t, "pending", out)
}

// Two people: the requester cannot approve their own; a second approves once.
func TestAdjustmentTwoPersonApprovalDB(t *testing.T) {
	a := newAdjustEnv(t)
	start := a.wallet()
	_, req, err := a.adjust(a.finance, 60000)
	require.NoError(t, err)
	rid, err := ids.Parse(req)
	require.NoError(t, err)

	_, err = a.adm.ApproveAdjustment(a.ctx, a.finance, rid, nil)
	require.Equal(t, "approver_is_requester", problem.CodeOf(err))
	require.Equal(t, start, a.wallet())

	note := "checked the order"
	r, err := a.adm.ApproveAdjustment(a.ctx, a.finance2, rid, &note)
	require.NoError(t, err)
	require.Equal(t, "approved", string(r.Outcome))
	require.NotNil(t, r.TransactionId)
	require.Equal(t, start+600, a.wallet())
	require.Equal(t, 1, a.count(`SELECT count(*) FROM admin_adjustment_requests WHERE status = 'approved' AND approved_by = $1 AND requested_by = $2`,
		ids.UUID(a.finance2.UserID), ids.UUID(a.finance.UserID)))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'credits.adjustment_approve' AND actor_id = $1`, ids.UUID(a.finance2.UserID)))

	// A second approval (a retry, or a third person) never posts twice.
	_, err = a.adm.ApproveAdjustment(a.ctx, a.finance2, rid, nil)
	require.Equal(t, "invalid_transition", problem.CodeOf(err))
	_, err = a.adm.RejectAdjustment(a.ctx, a.finance2, rid, "too late")
	require.Equal(t, "invalid_transition", problem.CodeOf(err))
	require.Equal(t, start+600, a.wallet())
	require.Equal(t, 1, a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'admin_adjustment'`))

	// The database refuses a self-approval too (D-05).
	_, req2, err := a.adjust(a.finance, -60000)
	require.NoError(t, err)
	_, err = a.tdb.OwnerPool.Exec(a.ctx, `UPDATE admin_adjustment_requests SET approved_by = requested_by WHERE id = $1`, uuidOf(t, req2))
	require.Error(t, err)

	// A second person rejects; the requester's own reject cancels.
	rid2, _ := ids.Parse(req2)
	rj, err := a.adm.RejectAdjustment(a.ctx, a.finance2, rid2, "not justified")
	require.NoError(t, err)
	require.Equal(t, "rejected", string(rj.Outcome))
	_, req3, err := a.adjust(a.finance, 70000)
	require.NoError(t, err)
	rid3, _ := ids.Parse(req3)
	c, err := a.adm.RejectAdjustment(a.ctx, a.finance, rid3, "typo in the amount")
	require.NoError(t, err)
	require.Equal(t, "cancelled", string(c.Outcome))
	_, err = a.adm.ApproveAdjustment(a.ctx, a.finance2, rid3, nil)
	require.Equal(t, "invalid_transition", problem.CodeOf(err))
	require.Equal(t, start+600, a.wallet())

	_, err = a.adm.ApproveAdjustment(a.ctx, a.finance2, a.gen.New(a.clk.Now()), nil)
	require.Equal(t, "not_found", problem.CodeOf(err))
}

// A debit never drives the wallet below zero, at once or at approval.
func TestAdjustmentInsufficientBalanceDB(t *testing.T) {
	a := newAdjustEnv(t)
	_, _, err := a.adjust(a.finance, -(int(a.wallet())+1)*100)
	require.Equal(t, "adjustment_insufficient_balance", problem.CodeOf(err))

	// Approved later, after the balance fell.
	_, req, err := a.adjust(a.finance, -80000) // 800 of 1000
	require.NoError(t, err)
	_, _, err = a.adjust(a.finance, -50000) // at once: 500 → 500 left
	require.NoError(t, err)
	rid, _ := ids.Parse(req)
	_, err = a.adm.ApproveAdjustment(a.ctx, a.finance2, rid, nil)
	require.Equal(t, "adjustment_insufficient_balance", problem.CodeOf(err))
	require.EqualValues(t, 500, a.wallet())
	require.Equal(t, 1, a.count(`SELECT count(*) FROM admin_adjustment_requests WHERE status = 'pending'`))
}

// The at-once posting is keyed by the caller's Idempotency-Key, so a replay
// that gets past the HTTP idempotency layer never posts twice. TODO(lead): the
// replay answers conflict (each call has its own request id as the ref)
// rather than the first result; the HTTP layer replays the stored response
// before it gets here.
func TestAdjustmentIdempotencyKeyDB(t *testing.T) {
	a := newAdjustEnv(t)
	in := AdjustmentInput{AccountID: a.account, AmountPaise: 10000, Reason: "refund for a missing item", IdempotencyKey: "k-1"}
	r1, err := a.adm.CreateAdjustment(a.ctx, a.finance, in)
	require.NoError(t, err)
	require.NotNil(t, r1.TransactionId)
	_, err = a.adm.CreateAdjustment(a.ctx, a.finance, in)
	require.Equal(t, "conflict", problem.CodeOf(err))
	require.EqualValues(t, 1100, a.wallet())
	require.Equal(t, 1, a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'admin_adjustment'`))
	// Another admin's same key is their own.
	_, err = a.adm.CreateAdjustment(a.ctx, a.finance2, in)
	require.NoError(t, err)
	require.EqualValues(t, 1200, a.wallet())
	in.AmountPaise = 20000
	_, err = a.adm.CreateAdjustment(a.ctx, a.finance, in)
	require.Equal(t, "conflict", problem.CodeOf(err))

	// Only a customer's wallet takes an adjustment.
	var u pgtype.UUID
	require.NoError(t, a.tdb.OwnerPool.QueryRow(a.ctx, `SELECT id FROM ledger_accounts WHERE kind = 'platform_marketing'`).Scan(&u))
	pm, _ := ids.FromUUID(u)
	_, err = a.adm.CreateAdjustment(a.ctx, a.finance, AdjustmentInput{AccountID: pm.String(), AmountPaise: 100, Reason: "x"})
	require.Equal(t, "validation_failed", problem.CodeOf(err))
	_, err = a.adm.CreateAdjustment(a.ctx, a.finance, AdjustmentInput{AccountID: a.account, AmountPaise: 100, Reason: "  "})
	require.Equal(t, "validation_failed", problem.CodeOf(err))
}

func uuidOf(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return ids.UUID(id)
}

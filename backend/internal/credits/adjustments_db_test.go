package credits

import (
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
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

// admin is a fresh finance user.
func (a *aenv) admin() authn.Principal {
	return authn.Principal{UserID: a.user(), App: authn.AppAdmin, Roles: []string{"finance"}}
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
	// A different admin each time, so the day's sum (D-237) is this one alone.
	for _, paise := range []int{20000, -20000, 50000, -50000} {
		out, id, err := a.adjust(a.admin(), paise)
		require.NoError(t, err, paise)
		require.Equal(t, "posted", out, paise)
		require.Empty(t, id)
	}
	require.Equal(t, start, a.wallet(), "±₹200 and ±₹500 net to zero")
	require.Zero(t, a.count(`SELECT count(*) FROM admin_adjustment_requests`), "no request row at or under the threshold")
	require.Equal(t, 4, a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'admin_adjustment'`))
	require.Equal(t, 4, a.count(`SELECT count(*) FROM audit_log WHERE action = 'credits.adjust'`))

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
	_, _, err = a.adjust(a.finance2, -50000) // at once: 500 → 500 left
	require.NoError(t, err)
	rid, _ := ids.Parse(req)
	_, err = a.adm.ApproveAdjustment(a.ctx, a.finance2, rid, nil)
	require.Equal(t, "adjustment_insufficient_balance", problem.CodeOf(err))
	require.EqualValues(t, 500, a.wallet())
	require.Equal(t, 1, a.count(`SELECT count(*) FROM admin_adjustment_requests WHERE status = 'pending'`))
}

// The at-once posting is keyed by the caller's Idempotency-Key, so a replay
// that gets past the HTTP idempotency layer never posts twice: an identical
// one answers the first result, a different body under the key 409.
func TestAdjustmentIdempotencyKeyDB(t *testing.T) {
	a := newAdjustEnv(t)
	in := AdjustmentInput{AccountID: a.account, AmountPaise: 10000, Reason: "refund for a missing item", IdempotencyKey: "k-1"}
	r1, err := a.adm.CreateAdjustment(a.ctx, a.finance, in)
	require.NoError(t, err)
	require.NotNil(t, r1.TransactionId)
	again, err := a.adm.CreateAdjustment(a.ctx, a.finance, in)
	require.NoError(t, err)
	require.Equal(t, "posted", string(again.Outcome))
	require.Equal(t, *r1.TransactionId, *again.TransactionId)
	require.EqualValues(t, 1100, a.wallet())
	require.Equal(t, 1, a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'admin_adjustment'`))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'credits.adjust'`))

	// A debit replayed after it emptied the wallet still answers its first result.
	a.setting(settings.AdminAdjustDualApprovalAbovePaise, "200000")
	debit := AdjustmentInput{AccountID: a.account, AmountPaise: -110000, Reason: "duplicate grant", IdempotencyKey: "k-debit"}
	d1, err := a.adm.CreateAdjustment(a.ctx, a.finance, debit)
	require.NoError(t, err)
	require.Zero(t, a.wallet())
	d2, err := a.adm.CreateAdjustment(a.ctx, a.finance, debit)
	require.NoError(t, err)
	require.Equal(t, *d1.TransactionId, *d2.TransactionId)
	a.setting(settings.AdminAdjustDualApprovalAbovePaise, "400000") // the day's sum is ₹2,300 with the restore (D-237)
	_, err = a.adm.CreateAdjustment(a.ctx, a.finance, AdjustmentInput{AccountID: a.account, AmountPaise: 110000, Reason: "restore", IdempotencyKey: "k-restore"})
	require.NoError(t, err)
	a.setting(settings.AdminAdjustDualApprovalAbovePaise, "50000")
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

// A pending request is keyed by its requester's Idempotency-Key: a replay
// returns the first request, so two approvers can never post it twice.
func TestAdjustmentRequestIdempotencyKeyDB(t *testing.T) {
	a := newAdjustEnv(t)
	in := AdjustmentInput{AccountID: a.account, AmountPaise: 60000, Reason: "goodwill for a late pickup", IdempotencyKey: "req-1"}
	r1, err := a.adm.CreateAdjustment(a.ctx, a.finance, in)
	require.NoError(t, err)
	require.Equal(t, "pending", string(r1.Outcome))
	r2, err := a.adm.CreateAdjustment(a.ctx, a.finance, in)
	require.NoError(t, err)
	require.Equal(t, "pending", string(r2.Outcome))
	require.Equal(t, r1.Request.Id, r2.Request.Id)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM admin_adjustment_requests`))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'credits.adjustment_request'`))

	// The same key with another body is a conflict, not a second request.
	in.AmountPaise = 70000
	_, err = a.adm.CreateAdjustment(a.ctx, a.finance, in)
	require.Equal(t, "conflict", problem.CodeOf(err))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM admin_adjustment_requests`))

	// Approved once; the replay's request is the same row, already approved.
	rid, _ := ids.Parse(r1.Request.Id)
	_, err = a.adm.ApproveAdjustment(a.ctx, a.finance2, rid, nil)
	require.NoError(t, err)
	rid2, _ := ids.Parse(r2.Request.Id)
	_, err = a.adm.ApproveAdjustment(a.ctx, a.finance2, rid2, nil)
	require.Equal(t, "invalid_transition", problem.CodeOf(err))
	require.EqualValues(t, 1600, a.wallet())
	require.Equal(t, 1, a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'admin_adjustment'`))
}

// D-237: the threshold applies to the day's sum of one admin's adjustments to
// one customer (absolute values, pending and posted, this one included), so
// splitting a large adjustment into small ones still needs a second person.
func TestAdjustmentDaySumDB(t *testing.T) {
	a := newAdjustEnv(t) // threshold ₹500
	start := a.wallet()
	for _, paise := range []int{20000, -10000, 20000} { // 200 + 100 + 200 = 500: at the threshold
		out, _, err := a.adjust(a.finance, paise)
		require.NoError(t, err, paise)
		require.Equal(t, "posted", out, paise)
	}
	require.Equal(t, start+300, a.wallet())
	// ₹1 more today, either sign, is above it.
	for _, paise := range []int{100, -100} {
		out, id, err := a.adjust(a.finance, paise)
		require.NoError(t, err, paise)
		require.Equal(t, "pending", out, paise)
		require.NotEmpty(t, id)
	}
	require.Equal(t, start+300, a.wallet())
	var total int64
	require.NoError(t, a.tdb.OwnerPool.QueryRow(a.ctx,
		`SELECT max((after->>'dayTotalPaise')::bigint) FROM audit_log WHERE action = 'credits.adjustment_request'`).Scan(&total))
	require.EqualValues(t, 50200, total, "pending requests count too")

	// Another admin, or another customer, has a sum of its own.
	out, _, err := a.adjust(a.finance2, 50000)
	require.NoError(t, err)
	require.Equal(t, "posted", out)
	other := a.user()
	a.grant(other, 100)
	var u pgtype.UUID
	require.NoError(t, a.tdb.OwnerPool.QueryRow(a.ctx,
		`SELECT id FROM ledger_accounts WHERE kind = 'customer_wallet' AND owner_id = $1`, ids.UUID(other)).Scan(&u))
	oid, _ := ids.FromUUID(u)
	r, err := a.adm.CreateAdjustment(a.ctx, a.finance, AdjustmentInput{AccountID: oid.String(), AmountPaise: 50000, Reason: "goodwill"})
	require.NoError(t, err)
	require.Equal(t, "posted", string(r.Outcome))

	// Rejected and cancelled requests moved nothing and stop counting; an
	// approved one still counts, once (its posting is the approver's).
	reqs := a.count(`SELECT count(*) FROM admin_adjustment_requests WHERE requested_by = $1`, ids.UUID(a.finance.UserID))
	require.Equal(t, 2, reqs)
	var pending []pgtype.UUID
	rows, err := a.tdb.OwnerPool.Query(a.ctx, `SELECT id FROM admin_adjustment_requests WHERE requested_by = $1 ORDER BY amount`, ids.UUID(a.finance.UserID))
	require.NoError(t, err)
	for rows.Next() {
		var id pgtype.UUID
		require.NoError(t, rows.Scan(&id))
		pending = append(pending, id)
	}
	require.NoError(t, rows.Err())
	debitReq, _ := ids.FromUUID(pending[0])
	creditReq, _ := ids.FromUUID(pending[1])
	_, err = a.adm.RejectAdjustment(a.ctx, a.finance, debitReq, "split by mistake")
	require.NoError(t, err)
	approver := a.admin()
	_, err = a.adm.ApproveAdjustment(a.ctx, approver, creditReq, nil)
	require.NoError(t, err)
	out, _, err = a.adjust(a.finance, -100) // 500 + 1 approved + 1 = 502
	require.NoError(t, err)
	require.Equal(t, "pending", out)

	// The approval's posting is the requester's adjustment, not the
	// approver's: the approver still has ₹500 of their own today.
	out, _, err = a.adjust(approver, 50000)
	require.NoError(t, err)
	require.Equal(t, "posted", out)

	// The next IST day starts a new sum.
	a.clk.Set(clock.StartOfDayIST(a.clk.Now()).Add(24 * time.Hour))
	out, _, err = a.adjust(a.finance, 50000)
	require.NoError(t, err)
	require.Equal(t, "posted", out)
}

// D-237 under concurrency: several at-once-sized adjustments by one admin to
// one customer at the same moment never post more than the threshold in all.
func TestAdjustmentDaySumConcurrentDB(t *testing.T) {
	a := newAdjustEnv(t) // threshold ₹500
	const n = 6
	outs := make(chan string, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, _, err := a.adjust(a.finance, 20000)
			if err != nil {
				outs <- "error: " + problem.CodeOf(err)
				return
			}
			outs <- out
		}()
	}
	wg.Wait()
	close(outs)
	posted := 0
	for out := range outs {
		require.Contains(t, []string{"posted", "pending"}, out)
		if out == "posted" {
			posted++
		}
	}
	require.Equal(t, 2, posted, "₹200 + ₹200 fit under ₹500; a third would not")
	require.EqualValues(t, 1400, a.wallet())
	require.Equal(t, n-2, a.count(`SELECT count(*) FROM admin_adjustment_requests WHERE status = 'pending'`))
}

// Approval re-checks the customer inside its transaction: a request for an
// account deleted since is refused and posts nothing.
func TestAdjustmentApproveDeletedCustomerDB(t *testing.T) {
	a := newAdjustEnv(t)
	_, req, err := a.adjust(a.finance, 60000)
	require.NoError(t, err)
	a.exec(`UPDATE users SET status = 'deleted', deleted_at = now() WHERE id = $1`, ids.UUID(a.customer))
	rid, _ := ids.Parse(req)
	_, err = a.adm.ApproveAdjustment(a.ctx, a.finance2, rid, nil)
	require.Equal(t, "invalid_transition", problem.CodeOf(err))
	require.EqualValues(t, 1000, a.wallet())
	require.Zero(t, a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'admin_adjustment'`))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM admin_adjustment_requests WHERE status = 'pending'`))
}

func uuidOf(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return ids.UUID(id)
}

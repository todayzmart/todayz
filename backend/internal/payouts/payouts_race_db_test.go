package payouts_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	api "github.com/todayz/backend/gen/api/admin-payouts"
	"github.com/todayz/backend/internal/payouts"
	"github.com/todayz/backend/internal/platform/ids"
)

// pendingJune closes June with n credits of referral at S1 and returns the
// business's pending payout.
func (e *env) pendingJune(n int64) ids.ID {
	e.t.Helper()
	e.at(ist(2026, 6, 10, 12, 0))
	e.storeReferral(e.s1, n)
	e.close(time.June, 2026)
	p := e.openPayout()
	require.Equal(e.t, api.PayoutStatus("pending"), p.Payout.Status)
	require.Equal(e.t, int(n*100), p.Payout.AmountPaise)
	id, err := ids.Parse(p.Payout.Id)
	require.NoError(e.t, err)
	return id
}

// A store's balance can fall after the close (cashback): mark paid reads the
// live balance under lock and refuses rather than take the store negative.
func TestMarkPaidRefusesWhenTheBalanceFellDB(t *testing.T) {
	e := newEnv(t)
	pid := e.pendingJune(100)
	e.at(ist(2026, 7, 2, 12, 0))
	e.order(e.s1, 0, 160000) // 5% cashback of ₹1,600 = 80 credits from S1
	require.Equal(t, int64(20), e.storeCredit(e.s1))

	_, err := e.svc.MarkPaid(e.ctx, e.finance, pid, "AXIS12345678", 10000, "June payout")
	require.Equal(t, "payout_amount_changed", code(err))
	require.Equal(t, int64(20), e.storeCredit(e.s1), "balance unchanged")
	require.Zero(t, e.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'payout'`))
	require.Equal(t, api.PayoutStatus("pending"), e.openPayout().Payout.Status)
}

// Two finance users mark the same payout paid with different UTRs at once:
// one wins, the other is told it is paid; one posting.
func TestMarkPaidConcurrentUTRsDB(t *testing.T) {
	e := newEnv(t)
	pid := e.pendingJune(100)
	e.at(ist(2026, 7, 2, 12, 0))

	errs := race(func(i int) error {
		_, err := e.svc.MarkPaid(e.ctx, e.finance, pid, []string{"AXIS11111111", "HDFC22222222"}[i], 10000, "June payout")
		return err
	})
	require.ElementsMatch(t, []string{"", "payout_already_paid"}, []string{code(errs[0]), code(errs[1])})
	require.Equal(t, 1, e.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'payout'`))
	require.Zero(t, e.storeCredit(e.s1))
}

// The same UTR on two payouts at once: one is paid, the other refused and
// rolled back with its posting.
func TestMarkPaidSameUTRTwoPayoutsDB(t *testing.T) {
	e := newEnv(t)
	biz2 := e.business("Ravi Stores")
	s3 := e.store(biz2, "Ravi Stores")
	e.kyc(biz2, "verified")
	e.at(ist(2026, 6, 10, 12, 0))
	e.storeReferral(e.s1, 100)
	e.storeReferral(s3, 50)
	e.close(time.June, 2026)
	p1, err := ids.Parse(e.openPayout().Payout.Id)
	require.NoError(t, err)
	p2, err := ids.Parse(e.payoutWhere(`business_id = $1 AND status = 'pending'`, ids.UUID(biz2)).Payout.Id)
	require.NoError(t, err)
	e.at(ist(2026, 7, 2, 12, 0))

	errs := race(func(i int) error {
		_, err := e.svc.MarkPaid(e.ctx, e.finance, []ids.ID{p1, p2}[i], "AXIS12345678", []int{10000, 5000}[i], "June payout")
		return err
	})
	require.ElementsMatch(t, []string{"", "utr_in_use"}, []string{code(errs[0]), code(errs[1])})
	require.Equal(t, 1, e.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'payout'`))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM payouts WHERE status = 'paid'`))
	require.Equal(t, int64(150), e.storeCredit(e.s1)+e.storeCredit(s3)+map[bool]int64{true: 100, false: 50}[errs[0] == nil])
}

// July's close races mark paid of June's payout. Either order is safe: paid
// first, the close opens a new payout of what is left; close first, the
// recomputed amount refuses the old one. Never more than the store holds.
func TestCloseRacesMarkPaidDB(t *testing.T) {
	e := newEnv(t)
	pid := e.pendingJune(100)
	e.at(ist(2026, 7, 10, 12, 0))
	e.storeReferral(e.s1, 30)
	e.at(ist(2026, 8, 1, 0, 30))

	errs := race(func(i int) error {
		if i == 0 {
			_, err := e.svc.MarkPaid(e.ctx, e.finance, pid, "AXIS12345678", 10000, "June payout")
			return err
		}
		_, err := e.svc.CloseMonth(e.ctx, payouts.ClosePeriod(e.clk.Now()))
		return err
	})
	require.NoError(t, errs[1])
	open := e.openPayout()
	switch code(errs[0]) {
	case "":
		require.Equal(t, int64(30), e.storeCredit(e.s1))
		require.NotEqual(t, pid.String(), open.Payout.Id)
		require.Equal(t, 3000, open.Payout.AmountPaise, "only what is left")
	case "payout_amount_changed":
		require.Equal(t, int64(130), e.storeCredit(e.s1))
		require.Equal(t, pid.String(), open.Payout.Id)
		require.Equal(t, 13000, open.Payout.AmountPaise)
	default:
		t.Fatalf("mark paid: %v", errs[0])
	}
	require.LessOrEqual(t, e.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'payout'`), 1)
	clean, err := e.cr.Reconcile(e.ctx)
	require.NoError(t, err)
	require.True(t, clean.Clean(), "reconcile: %v", clean)
}

// race runs f(0) and f(1) at the same moment and returns their errors.
func race(f func(i int) error) [2]error {
	var (
		out   [2]error
		wg    sync.WaitGroup
		start = make(chan struct{})
	)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out[i] = f(i)
		}()
	}
	close(start)
	wg.Wait()
	return out
}

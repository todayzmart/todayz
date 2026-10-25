package jobs

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/referrals"
	"github.com/todayz/backend/internal/risk"
)

// A-10's reject races the reward posting on a qualified referral (W3-02
// review). The referral's row lock orders them: a reject that lands first
// leaves no reward transaction and no credits on either wallet (a side the
// ledger posted meanwhile rolls back with its own transaction); once a side
// has posted the reject is 409 and the referral ends rewarded.
func TestRejectRacesPostRewardsDB(t *testing.T) {
	e := newRefEnv(t, true)
	rs, err := risk.New(risk.Deps{DB: e.pool, Clock: e.clk, Settings: e.cache, Audit: audit.New(e.pool, e.clk, nil)})
	require.NoError(t, err)
	admin := ids.New(refNow)
	e.exec(`INSERT INTO users (id, created_at) VALUES ($1, $2)`, ids.UUID(admin), refNow)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	reject := func(ref ids.ID) error {
		_, err := rs.RejectReferral(e.ctx, admin, ref, "staff self-referral")
		return err
	}
	is409 := func(err error) bool {
		var p *problem.Error
		return errors.As(err, &p) && p.Code == genproblems.InvalidTransition
	}
	total := 0
	rewarded := func(ref, referee ids.ID, round string) {
		t.Helper()
		r := e.referral(ref)
		require.Equal(t, "rewarded", r.status, round)
		require.Equal(t, 2, e.rewardTxs(ref), round)
		require.Equal(t, r.refereeCr, e.balance(credits.CustomerWallet, referee), round)
		total += r.referrerCr
	}
	rejected := func(ref, referee ids.ID, round string) {
		t.Helper()
		require.Equal(t, "rejected", e.referral(ref).status, round)
		require.Zero(t, e.rewardTxs(ref), round+": a rejected referral posts nothing")
		require.Zero(t, e.balance(credits.CustomerWallet, referee), round)
	}

	// The reject lands while the first side is in the ledger: that side rolls back.
	referee, ref := e.claim(code)
	e.buy(referee)
	var hookErr error
	e.flaky.before[referrals.SideReferrer] = func() { hookErr = reject(ref) }
	_, err = e.svc.PostRewards(e.ctx, ref)
	require.NoError(t, err)
	require.NoError(t, hookErr)
	rejected(ref, referee, "reject first")

	// The reject lands after the first side committed: 409, and the second side still posts.
	referee, ref = e.claim(code)
	e.buy(referee)
	e.flaky.before[referrals.SideReferee] = func() { hookErr = reject(ref) }
	_, err = e.svc.PostRewards(e.ctx, ref)
	require.NoError(t, err)
	require.True(t, is409(hookErr), "%v", hookErr)
	rewarded(ref, referee, "side first")

	// Free races: whichever wins, one of the two outcomes holds.
	for i := range 10 {
		round := fmt.Sprintf("round %d", i)
		referee, ref := e.claim(code)
		e.buy(referee)
		var wg sync.WaitGroup
		start := make(chan struct{})
		var rejErr, postErr error
		wg.Add(2)
		go func() { defer wg.Done(); <-start; rejErr = reject(ref) }()
		go func() { defer wg.Done(); <-start; _, postErr = e.svc.PostRewards(e.ctx, ref) }()
		close(start)
		wg.Wait()
		require.NoError(t, postErr, round)
		if rejErr == nil {
			rejected(ref, referee, round)
			continue
		}
		require.True(t, is409(rejErr), "%s: %v", round, rejErr)
		rewarded(ref, referee, round)
	}
	require.Equal(t, total, e.balance(credits.CustomerWallet, referrer), "the referrer holds only the rewards the reject lost to")
	require.Equal(t, e.count(`SELECT count(*) FROM referrals WHERE status = 'rejected'`),
		e.count(`SELECT count(*) FROM audit_log WHERE action = 'referral.reject'`), "a 409 writes no audit row")
}

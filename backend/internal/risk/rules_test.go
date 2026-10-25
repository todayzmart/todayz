package risk

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// Rule fixtures (SC-RK-01…15, 01 §2, §6–8, D-225). These are the pure rules;
// db_test.go runs the same rules over the scan queries.

var (
	tNow   = time.Date(2026, 10, 25, 6, 30, 0, 0, time.UTC) // 12:00 IST
	storeA = ids.New(tNow.Add(-time.Hour))
	storeB = ids.New(tNow.Add(-2 * time.Hour))
	storeC = ids.New(tNow.Add(-3 * time.Hour))
	userA  = ids.New(tNow.Add(-4 * time.Hour))
	userB  = ids.New(tNow.Add(-5 * time.Hour))
)

func TestCodeLockoutsMoreThanTheLimit(t *testing.T) {
	day := clock.StartOfDayIST(tNow)
	fs := CodeLockouts([]StoreCount{{storeA, 10}, {storeB, 11}}, 10, day)
	require.Len(t, fs, 1, "exactly the limit does not flag; one more does")
	f := fs[0]
	assert.Equal(t, storeB, f.SubjectID)
	assert.Equal(t, "store", f.SubjectType)
	assert.Equal(t, RuleCodeLockouts, f.Rule)
	assert.Equal(t, "2026-10-25", f.Day)
	assert.Equal(t, map[string]any{"lockouts": 11, "limit": 10, "day": "2026-10-25"}, f.Details)

	// ABUSE-M.05: with the setting at 2 the third lockout flags.
	require.Len(t, CodeLockouts([]StoreCount{{storeA, 3}}, 2, day), 1)
	require.Empty(t, CodeLockouts([]StoreCount{{storeA, 2}}, 2, day))
}

func TestCodeLockoutsDayIsIST(t *testing.T) {
	// 20:00 UTC on the 24th is 01:30 IST on the 25th.
	day := clock.StartOfDayIST(time.Date(2026, 10, 24, 20, 0, 0, 0, time.UTC))
	fs := CodeLockouts([]StoreCount{{storeA, 5}}, 2, day)
	require.Len(t, fs, 1)
	assert.Equal(t, "2026-10-25", fs[0].Day)
}

func claim(user, store ids.ID, at time.Time) Claim {
	return Claim{ID: ids.New(at), UserID: user, StoreID: store, ClaimedAt: at}
}

func TestBillClaimSpreadWithinTheWindow(t *testing.T) {
	w := 10 * time.Minute
	cs := []Claim{
		claim(userA, storeA, tNow),
		claim(userA, storeB, tNow.Add(4*time.Minute)),
		claim(userA, storeC, tNow.Add(9*time.Minute)),
	}
	fs := BillClaimSpread(cs, 3, w)
	require.Len(t, fs, 1)
	f := fs[0]
	assert.Equal(t, "user", f.SubjectType)
	assert.Equal(t, userA, f.SubjectID)
	assert.Equal(t, 3, f.Details["stores"])
	assert.Equal(t, 10, f.Details["windowMinutes"])
	assert.Len(t, f.Details["claimIds"], 3)
}

func TestBillClaimSpreadFalsePositives(t *testing.T) {
	w := 10 * time.Minute
	// The same three stores spread over 25 minutes: never three in a window.
	slow := []Claim{
		claim(userA, storeA, tNow),
		claim(userA, storeB, tNow.Add(11*time.Minute)),
		claim(userA, storeC, tNow.Add(25*time.Minute)),
	}
	assert.Empty(t, BillClaimSpread(slow, 3, w))
	// Many claims at one store (a regular buying at the counter) is not spread.
	var one []Claim
	for i := range 6 {
		one = append(one, claim(userA, storeA, tNow.Add(time.Duration(i)*time.Minute)))
	}
	assert.Empty(t, BillClaimSpread(one, 3, w))
	// Two customers at three stores each in the window: neither alone reaches 3.
	two := []Claim{
		claim(userA, storeA, tNow), claim(userA, storeB, tNow.Add(time.Minute)),
		claim(userB, storeB, tNow), claim(userB, storeC, tNow.Add(time.Minute)),
	}
	assert.Empty(t, BillClaimSpread(two, 3, w))
}

func TestReversalRatio(t *testing.T) {
	w := 7 * 24 * time.Hour
	rs := []Reversals{
		{storeA, 10, 2}, // exactly 20 %: flags
		{storeB, 10, 1}, // 10 %
		{storeC, 9, 9},  // under the minimum sales: never
	}
	fs := ReversalRatio(rs, 20, 10, w, tNow)
	require.Len(t, fs, 1)
	assert.Equal(t, storeA, fs[0].SubjectID)
	assert.Equal(t, 20, fs[0].Details["ratioPercent"])
	assert.Equal(t, 7, fs[0].Details["windowDays"])
	assert.Equal(t, tNow.Add(-w), fs[0].Since)
}

func TestSelfLikeReferrals(t *testing.T) {
	biz := ids.New(tNow)
	ref := func(kind string, own, staff, dev, phone bool) SelfLike {
		return SelfLike{ReferralID: ids.New(tNow), Kind: kind, OwnShop: own, OwnBusinessID: biz,
			RefereeStaff: staff, SharedDevice: dev, StaffPhone: phone, QualifyingRefType: "order", QualifyingRefID: ids.New(tNow)}
	}
	cases := []struct {
		name string
		r    SelfLike
		hit  bool
	}{
		{"bought at the referrer's own shop, nothing else", ref("retailer_customer", true, false, false, false), false},
		{"a shared device alone, bought elsewhere", ref("customer_customer", false, false, true, false), false},
		{"own shop and the referee works there", ref("retailer_customer", true, true, false, false), true},
		{"own shop and a shared device", ref("customer_customer", true, false, true, false), true},
		{"own shop and the referee was invited as staff", ref("retailer_customer", true, false, false, true), true},
		{"any_shop: the referrer works at the new shop", ref("any_shop", false, true, false, false), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := SelfLikeReferrals([]SelfLike{c.r})
			if !c.hit {
				assert.Empty(t, fs)
				return
			}
			require.Len(t, fs, 1)
			assert.Equal(t, "referral", fs[0].SubjectType)
			assert.Equal(t, RuleSelfLike, fs[0].Rule)
			assert.NotEmpty(t, fs[0].Details["signals"])
			assert.Equal(t, biz.String(), fs[0].Details["businessId"])
		})
	}
}

func TestOTPRulesHoldNoNumberOTPOrCode(t *testing.T) {
	ip := make([]byte, 32)
	ip[0] = 7
	fs := OTPIPFanout([]IPFanout{{IPHash: ip, Phones: 12, Requests: 15}, {IPHash: ip, Phones: 3}}, 10, tNow)
	require.Len(t, fs, 1)
	assert.Equal(t, SubjectIP, fs[0].SubjectType)
	id, ok := HashID(ip)
	require.True(t, ok)
	assert.Equal(t, id, fs[0].SubjectID)
	for k := range fs[0].Details {
		assert.NotContains(t, k, "hash")
	}

	ph := make([]byte, 32)
	ph[1] = 9
	fs = OTPFailedPhone([]FailedPhone{{PhoneHash: ph, Failed: 3}, {PhoneHash: ph, Failed: 3, UserID: userA}, {PhoneHash: ph, Failed: 2}}, 3, tNow)
	require.Len(t, fs, 2)
	assert.Equal(t, SubjectPhone, fs[0].SubjectType, "no account: the flag is on the phone's hash")
	assert.Equal(t, "user", fs[1].SubjectType)
	assert.Equal(t, userA, fs[1].SubjectID)

	_, ok = HashID([]byte{1, 2})
	assert.False(t, ok, "a short hash is not an id")
}

func TestFacts(t *testing.T) {
	d := mustJSON(map[string]any{
		"lockouts": 11, "day": "2026-10-25", "claimIds": []string{"x"}, "bad key": 1,
		"signals": []string{"own_shop"}, "ok": true,
	})
	fs := Facts(d)
	assert.Equal(t, []Fact{
		{"day", "2026-10-25"}, {"lockouts", "11"}, {"ok", "true"}, {"signals", `["own_shop"]`},
	}, fs, "sorted, id lists and keys the contract cannot carry left out")
	assert.Nil(t, Facts([]byte("not json")))
}

// A failed rule is logged by its SQLSTATE or error type, never its text.
func TestFailCauseHasNoErrorText(t *testing.T) {
	pg := &pgconn.PgError{Code: "42501", Message: "permission denied for table order_code_lockouts", Detail: "Key (id)=(01J...)"}
	assert.Equal(t, "sqlstate 42501", failCause(fmt.Errorf("find: %w", pg)))
	assert.Equal(t, "deadline_exceeded", failCause(fmt.Errorf("find: %w", context.DeadlineExceeded)))
	assert.Equal(t, "canceled", failCause(context.Canceled))
	assert.Equal(t, "*errors.errorString", failCause(errors.New("rule text with a value")))
}

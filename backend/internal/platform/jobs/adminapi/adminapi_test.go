package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	adminjobsapi "github.com/todayz/backend/gen/api/admin-jobs"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

func TestScheduleText(t *testing.T) {
	for expr, want := range map[string]string{
		"* * * * *":     "Every minute",
		"*/5 * * * *":   "Every 5 minutes",
		"7 * * * *":     "Every hour at :07",
		"15 */6 * * *":  "Every 6 hours at :15",
		"30 2 * * *":    "Every day at 02:30 IST",
		"0 9 * * 1":     "Every Monday at 09:00 IST",
		"0 9 * * 7":     "Every Sunday at 09:00 IST",
		"45 3 1 * *":    "Monthly on day 1 at 03:45 IST",
		"0 9 * 1 *":     "0 9 * 1 * (IST)",
		"0 9-17 * * *":  "0 9-17 * * * (IST)",
		"0 9 * * 1-5":   "0 9 * * 1-5 (IST)",
		"bad":           "bad (IST)",
		"1,31 * * * *":  "1,31 * * * * (IST)",
		"0 0 1 * 1":     "0 0 1 * 1 (IST)",
		"*/15 9 * * *":  "*/15 9 * * * (IST)",
		"0 */2 * * 1-5": "0 */2 * * 1-5 (IST)",
	} {
		require.Equal(t, want, scheduleText(expr), expr)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 22, 4, 30, 0, 123456000, time.UTC)
	id := ids.New(at)
	gotAt, gotID, err := decodeCursor(encodeCursor(at, id.String()))
	require.NoError(t, err)
	require.True(t, at.Equal(gotAt))
	require.Equal(t, id, gotID)
	for _, bad := range []string{"", "not-a-cursor", "MTIz", encodeCursor(at, "nope")} {
		_, _, err := decodeCursor(bad)
		require.Error(t, err, bad)
	}
}

func TestHealthOrder(t *testing.T) {
	t0 := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	failed, ok := "failed", "succeeded"
	for name, c := range map[string]struct {
		st   kindState
		want adminjobsapi.JobHealth
	}{
		"nothing yet":           {kindState{}, adminjobsapi.JobHealthOk},
		"last ok":               {kindState{lastOK: &t1, lastFinished: &ok}, adminjobsapi.JobHealthOk},
		"last failed":           {kindState{lastOK: &t0, lastFinished: &failed}, adminjobsapi.JobHealthFailing},
		"missed after ok":       {kindState{lastOK: &t0, lastMissed: &t1, lastFinished: &ok}, adminjobsapi.JobHealthMissed},
		"ok after missed":       {kindState{lastOK: &t1, lastMissed: &t0, lastFinished: &ok}, adminjobsapi.JobHealthOk},
		"missed beats failing":  {kindState{lastMissed: &t1, lastFinished: &failed}, adminjobsapi.JobHealthMissed},
		"paused beats the rest": {kindState{lastMissed: &t1, lastFinished: &failed, control: adminjobsapi.JobControl{Paused: true}}, adminjobsapi.JobHealthPaused},
	} {
		require.Equal(t, c.want, health(c.st), name)
	}
}

func TestRolesAndDenyGate(t *testing.T) {
	super := Admin{Roles: []Role{RoleSuperAdmin}}
	require.True(t, super.Has(RoleFinance))
	require.False(t, financeOnly(super))
	require.True(t, financeOnly(Admin{Roles: []Role{RoleFinance}}))
	require.False(t, financeOnly(Admin{Roles: []Role{RoleFinance, RoleAnalyst}}))
	require.False(t, Admin{Roles: []Role{RoleAnalyst}}.HasAny(actRoles))

	// Until W1-01 lands the mounted API fails closed.
	_, err := DenyGate{}.Require(context.Background(), readRoles, false)
	var pe *problem.Error
	require.ErrorAs(t, err, &pe)
	require.Equal(t, genproblems.Unauthorized, pe.Code)
}

func TestReasonOf(t *testing.T) {
	_, err := reasonOf(nil)
	require.Error(t, err)
	_, err = reasonOf(new(" a  "))
	require.Error(t, err)
	r, err := reasonOf(new("  late ledger "))
	require.NoError(t, err)
	require.Equal(t, "late ledger", r)
}

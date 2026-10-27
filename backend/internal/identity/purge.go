package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/todayz/backend/gen/sqlc/identitydb"
	"github.com/todayz/backend/internal/platform/db"
)

// Retention of the sign-in tables (the identity_purge maintenance job).
const (
	// OTPRetention: otp_requests are purged after 24 h (03 §1). The send
	// limits look back one hour, so a purge never frees a limit early.
	OTPRetention = 24 * time.Hour
	// SessionRetention keeps a session row this long after it was revoked or
	// expired, whichever came first, so the sign-in trail stays readable for
	// support. It must be at least the longest refresh lifetime: a rotated
	// row then outlives its own token (which expired at most that long after
	// it was issued, before it was rotated), so reuse detection covers each
	// token's whole life. A replay after that is a plain 401.
	// TODO(lead): the plan names no retention for sessions; 30 days assumed.
	SessionRetention = 30 * 24 * time.Hour
	// AdminSigninFailureRetention keeps a wrong admin sign-in code for the
	// longest window admin_signin_failure_window_hours may be set to (its
	// max, 168 h), so a purge never lowers a count that still matters
	// (D-264). Locks are deleted once they have run out.
	AdminSigninFailureRetention = 168 * time.Hour //settings:ok the max of admin_signin_failure_window_hours
)

// PurgeResult counts the rows one purge deleted.
type PurgeResult struct {
	OTPRequests         int64
	Sessions            int64
	AdminSigninFailures int64
	AdminSigninLocks    int64
}

// Purge deletes OTP requests, sessions and the admin sign-in failures and
// locks (D-264) past their retention. It is
// idempotent: a retried job deletes nothing it should not.
func Purge(ctx context.Context, d *db.DB, now time.Time) (PurgeResult, error) {
	q := identitydb.New(d.Q(ctx))
	var out PurgeResult
	n, err := q.PurgeOtpRequests(ctx, now.Add(-OTPRetention))
	if err != nil {
		return out, fmt.Errorf("identity: purge otp_requests: %w", err)
	}
	out.OTPRequests = n
	n, err = q.PurgeSessions(ctx, now.Add(-SessionRetention))
	if err != nil {
		return out, fmt.Errorf("identity: purge sessions: %w", err)
	}
	out.Sessions = n
	n, err = q.PurgeAdminSigninFailures(ctx, now.Add(-AdminSigninFailureRetention))
	if err != nil {
		return out, fmt.Errorf("identity: purge admin_signin_failures: %w", err)
	}
	out.AdminSigninFailures = n
	n, err = q.PurgeAdminSigninLocks(ctx, now)
	if err != nil {
		return out, fmt.Errorf("identity: purge admin_signin_locks: %w", err)
	}
	out.AdminSigninLocks = n
	return out, nil
}

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/testkit"
)

// TestAdminUnlockSignInCLI: the D-264 break-glass unlock. The number comes
// from a file or stdin, never the command line; the lock and the failure
// count go; one audit row with the system actor and the reason; and neither
// stdout nor stderr ever carries the number.
func TestAdminUnlockSignInCLI(t *testing.T) {
	const (
		adminPhone    = "+919000000041"
		customerPhone = "+919000000042"
		digits        = "9000000041"
	)
	ctx := context.Background()
	db := testkit.NewDB(t)
	e := localEnv(db)
	key, err := base64.StdEncoding.DecodeString(e["PII_HASH_KEY"])
	require.NoError(t, err)
	h, err := pii.NewHasher(key)
	require.NoError(t, err)
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := db.OwnerPool.Exec(ctx, sql, args...)
		require.NoError(t, err)
	}
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		require.NoError(t, db.OwnerPool.QueryRow(ctx, sql, args...).Scan(&n))
		return n
	}
	person := func(phone string, admin bool) ids.ID {
		uid := ids.New(time.Now())
		exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(uid))
		exec(`INSERT INTO phone_identities (id, user_id, phone_enc, phone_hash, verified_at) VALUES ($1, $2, '\x00', $3, now())`,
			ids.UUID(ids.New(time.Now())), ids.UUID(uid), h.Phone(phone))
		if admin {
			exec(`INSERT INTO admin_users (user_id, roles) VALUES ($1, ARRAY['super_admin'])`, ids.UUID(uid))
		}
		return uid
	}
	lock := func(phone string) {
		exec(`INSERT INTO admin_signin_locks (phone_hash, locked_at, locked_until) VALUES ($1, now(), now() + interval '1 day')
			ON CONFLICT (phone_hash) DO UPDATE SET locked_at = now(), locked_until = now() + interval '1 day'`, h.Phone(phone))
		exec(`INSERT INTO admin_signin_failures (id, phone_hash, failed_at) VALUES ($1, $2, now())`, ids.UUID(ids.New(time.Now())), h.Phone(phone))
	}
	file := func(content string) string {
		p := filepath.Join(t.TempDir(), "phone")
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
		return p
	}
	noNumber := func(out, stderr string) {
		t.Helper()
		require.NotContains(t, out+stderr, digits)
		require.NotContains(t, out+stderr, "9000000042")
	}
	audits := func() int {
		return count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.unlock_sign_in'`)
	}

	uid := person(adminPhone, true)
	person(customerPhone, false)
	lock(adminPhone)

	// Usage: exit 2, nothing changed.
	for name, args := range map[string][]string{
		"no subcommand":    {"admin"},
		"unknown":          {"admin", "lock"},
		"no reason":        {"admin", "unlock-sign-in", "--phone-file", file(adminPhone)},
		"no file":          {"admin", "unlock-sign-in", "--reason", "x"},
		"file and env":     {"admin", "unlock-sign-in", "--phone-file", file(adminPhone), "--phone-env", "BREAK_GLASS_PHONE", "--reason", "x"},
		"env unset":        {"admin", "unlock-sign-in", "--phone-env", "BREAK_GLASS_UNSET", "--reason", "x"},
		"stray argument":   {"admin", "unlock-sign-in", "--phone-file", file(adminPhone), "--reason", "x", adminPhone},
		"not E.164":        {"admin", "unlock-sign-in", "--phone-file", file(digits), "--reason", "x"},
		"oversized file":   {"admin", "unlock-sign-in", "--phone-file", file(adminPhone + strings.Repeat(" ", 64)), "--reason", "x"},
		"number in reason": {"admin", "unlock-sign-in", "--phone-file", file(adminPhone), "--reason", "for " + digits},
	} {
		code, out, stderr := runCmd(args, e)
		require.Equal(t, 2, code, name)
		noNumber(out, stderr)
	}
	// A missing file names the path only; a customer's number is refused.
	code, out, stderr := runCmd([]string{"admin", "unlock-sign-in", "--phone-file", filepath.Join(t.TempDir(), "absent"), "--reason", "x"}, e)
	require.Equal(t, 1, code)
	noNumber(out, stderr)
	code, out, stderr = runCmd([]string{"admin", "unlock-sign-in", "--phone-file", file(customerPhone), "--reason", "x"}, e)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "no admin account")
	noNumber(out, stderr)
	require.Equal(t, 1, count(`SELECT count(*) FROM admin_signin_locks`))
	require.Zero(t, audits())

	// The unlock, from a file with a trailing newline.
	code, out, stderr = runCmd([]string{"admin", "unlock-sign-in", "--phone-file", file(adminPhone + "\n"), "--reason", "every super admin locked out"}, e)
	require.Equal(t, 0, code, stderr)
	noNumber(out, stderr)
	require.Contains(t, out+stderr, uid.String())
	require.Contains(t, out+stderr, `"lock_lifted":true`)
	require.Zero(t, count(`SELECT count(*) FROM admin_signin_locks`))
	require.Zero(t, count(`SELECT count(*) FROM admin_signin_failures`))
	require.Equal(t, 1, count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.unlock_sign_in' AND actor_kind = 'system'
		AND subject_id = $1 AND after ->> 'via' = 'break_glass' AND after ->> 'reason' = 'every super admin locked out'`, uid.String()))

	// From stdin, with no lock left: the count is cleared and it is audited.
	var outb, errb bytes.Buffer
	exec(`INSERT INTO admin_signin_failures (id, phone_hash, failed_at) VALUES ($1, $2, now())`, ids.UUID(ids.New(time.Now())), h.Phone(adminPhone))
	err = runAdmin(ctx, []string{"unlock-sign-in", "--phone-file", "-", "--reason", "stale count"}, env(e), strings.NewReader(adminPhone), &outb)
	require.NoError(t, err)
	noNumber(outb.String(), errb.String())
	require.Contains(t, outb.String(), `"lock_lifted":false`)
	require.Zero(t, count(`SELECT count(*) FROM admin_signin_failures`))
	require.Equal(t, 2, audits())

	// From the environment (the prod ops task: no shell, no stdin).
	lock(adminPhone)
	withPhone := map[string]string{"BREAK_GLASS_PHONE": adminPhone}
	for k, v := range e {
		withPhone[k] = v
	}
	code, out, stderr = runCmd([]string{"admin", "unlock-sign-in", "--phone-env", "BREAK_GLASS_PHONE", "--reason", "ops task"}, withPhone)
	require.Equal(t, 0, code, stderr)
	noNumber(out, stderr)
	require.Contains(t, out, `"lock_lifted":true`)
	require.Zero(t, count(`SELECT count(*) FROM admin_signin_locks`))
	require.Equal(t, 3, audits())
	require.Zero(t, count(`SELECT count(*) FROM audit_log WHERE before::text LIKE '%' || $1 || '%' OR after::text LIKE '%' || $1 || '%'`, digits))
}

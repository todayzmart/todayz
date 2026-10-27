package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/dbenv"
	"github.com/todayz/backend/internal/platform/testclock"
)

// maxPhoneFile bounds what the phone file may hold: one number and a newline,
// with room for spaces. Anything longer is refused unread.
const maxPhoneFile = 64 //settings:ok an input guard, not a product number

// runAdmin is `todayz admin unlock-sign-in --phone-file FILE|--phone-env NAME --reason TEXT`
// (D-264 review), the break-glass unlock for when every super admin is locked
// out of sign-in and nobody is left to unlock on A-18. It lifts the admin
// sign-in lock of that admin's number and clears its failure count, as
// todayz_app (DATABASE_URL), and writes an audit row with the system actor
// and the reason.
//
// The number is read from a file, from stdin with `--phone-file -`, or from
// the environment variable that `--phone-env` names (the prod ops task has no
// shell and no stdin, so the deploy RUNBOOK passes it as a container override
// environment value). It never sits on the command line, in the shell history
// or in the process list, and it is never printed or logged; the output names
// the admin's user id.
func runAdmin(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, out io.Writer) error {
	if len(args) == 0 || args[0] != "unlock-sign-in" {
		return usageError{msg: "admin takes unlock-sign-in --phone-file FILE|--phone-env NAME --reason TEXT"}
	}
	fs := flag.NewFlagSet("admin unlock-sign-in", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	phoneFile := fs.String("phone-file", "", "file holding the admin's number in E.164, or - for stdin")
	phoneEnv := fs.String("phone-env", "", "name of the environment variable holding the admin's number in E.164")
	reason := fs.String("reason", "", "why, for the audit log (1 to 500 characters, no phone number)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
		return usageError{msg: "admin unlock-sign-in takes --phone-file FILE|--phone-env NAME --reason TEXT"}
	}
	if (*phoneFile == "") == (*phoneEnv == "") || *reason == "" {
		return usageError{msg: "admin unlock-sign-in needs one of --phone-file FILE or --phone-env NAME, and --reason TEXT"}
	}
	var phone string
	if *phoneEnv != "" {
		phone = strings.TrimSpace(getenv(*phoneEnv))
		if phone == "" {
			return usageError{msg: "the variable named by --phone-env is not set"}
		}
	} else {
		var err error
		if phone, err = readPhoneFile(*phoneFile, stdin); err != nil {
			return err
		}
	}

	cfg, log, err := cliBoot("admin", getenv, out)
	if err != nil {
		return err
	}
	if !cfg.DatabaseURL.IsSet() {
		return errors.New("DATABASE_URL is not set")
	}
	if !cfg.Keys.PIIHashKey.IsSet() {
		return errors.New("PII_HASH_KEY is not set (the number is found by its keyed hash)")
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL.Reveal(), db.Options{AppName: "todayz-admin", MaxConns: 2})
	if err != nil {
		return err
	}
	defer pool.Close()

	// In local and test the API runs on the database's test clock; the
	// unlock reads the same clock, so a lock it lifts is the one sign-in sees.
	var clk clock.Clock = clock.Real{}
	if cfg.Env.AllowsTestRoutes() && dbenv.RequireDev(ctx, pool, cfg.Env, "test clock") == nil {
		off := clock.NewOffset(nil)
		if err := testclock.New(off, testclock.PGStore{Pool: pool}, log).Load(ctx); err != nil {
			return err
		}
		clk = off
	}
	svc, err := identity.New(identity.Deps{
		Config: cfg, DB: db.New(pool), Clock: clk, Log: log,
		SMS: cliNoSMS{}, Human: cliNoHuman{}, // the unlock sends nothing
	})
	if err != nil {
		return err
	}
	res, err := svc.BreakGlassUnlockAdminSignIn(ctx, phone, *reason)
	switch {
	case errors.Is(err, identity.ErrBreakGlassPhone), errors.Is(err, identity.ErrBreakGlassReason):
		return usageError{msg: err.Error()}
	case err != nil:
		return err
	}
	attrs := []any{slog.String("user_id", res.UserID.String()), slog.Bool("lock_lifted", res.LockedUntil != nil)}
	if res.LockedUntil != nil {
		attrs = append(attrs, slog.Time("would_have_ended", *res.LockedUntil))
	}
	log.InfoContext(ctx, "admin sign-in unlocked (break glass); failure count cleared; audited as admin_user.unlock_sign_in", attrs...)
	return nil
}

// readPhoneFile reads the number from path, or from stdin when path is "-".
// Errors never quote the content.
func readPhoneFile(path string, stdin io.Reader) (string, error) {
	var r io.Reader
	if path == "-" {
		r = stdin
	} else {
		f, err := os.Open(path) // #nosec G304 -- the operator names the file on purpose
		if err != nil {
			return "", fmt.Errorf("open the phone file: %w", err) // names the path, never the content
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	buf, err := io.ReadAll(io.LimitReader(r, maxPhoneFile+1))
	if err != nil {
		return "", errors.New("read the phone file failed")
	}
	if len(buf) > maxPhoneFile {
		return "", usageError{msg: "the phone file must hold only the number"}
	}
	return string(bytes.TrimSpace(buf)), nil
}

type cliNoSMS struct{}

func (cliNoSMS) SendOTP(context.Context, string, string) error {
	return errors.New("todayz admin sends no SMS")
}

type cliNoHuman struct{}

func (cliNoHuman) Verify(context.Context, string, string) (bool, error) { return false, nil }

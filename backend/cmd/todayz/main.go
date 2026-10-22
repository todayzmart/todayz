// Command todayz is the one backend binary (02 §2.1):
//
//	todayz api                      HTTP API on API_ADDR
//	todayz worker                   River worker; health on WORKER_ADDR
//	todayz migrate up|down|status   schema migrations, as todayz_owner (DATABASE_MIGRATE_URL)
//	todayz seed dev|demo|test       seed data, as todayz_app (DATABASE_URL); refused in staging and prod
//	todayz version                  build info as JSON
//
// Configuration is environment only (internal/platform/config). Logs are
// JSON on stdout through the redaction handler; values of secrets never
// appear in them.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/todayz/backend/internal/platform/config"
)

const usage = `usage: todayz <command>

commands:
  api                       serve the HTTP API
  worker                    run the River worker
  migrate up|down|status    run schema migrations (DATABASE_MIGRATE_URL)
  seed dev|demo|test        load seed data (DATABASE_URL); local and test only
  version                   print build info
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], config.FromOS(), os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run dispatches a command and returns the exit code: 0 ok, 1 failed, 2 usage.
func run(ctx context.Context, args []string, getenv config.Getenv, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "version", "--version", "-v":
		return runVersion(stdout)
	case "api":
		err = runAPI(ctx, getenv, stdout)
	case "worker":
		err = runWorker(ctx, getenv, stdout)
	case "migrate":
		if len(rest) != 1 {
			_, _ = fmt.Fprint(stderr, usage)
			return 2
		}
		err = runMigrate(ctx, rest[0], getenv, stdout)
	case "seed":
		if len(rest) != 1 {
			_, _ = fmt.Fprint(stderr, usage)
			return 2
		}
		err = runSeed(ctx, rest[0], getenv, stdout)
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "todayz: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
	if err != nil {
		var ue usageError
		if asUsage(err, &ue) {
			_, _ = fmt.Fprintf(stderr, "todayz %s: %s\n\n%s", cmd, ue.msg, usage)
			return 2
		}
		_, _ = fmt.Fprintf(stderr, "todayz %s: %v\n", cmd, err)
		return 1
	}
	return 0
}

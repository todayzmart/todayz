package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const signalChildEnv = "TODAYZ_SIGNAL_CHILD"

// TestSecondSignalExits (D-98): the first SIGTERM only cancels the context
// (the drain starts and the process keeps running); a second SIGTERM takes
// the default action and ends the process at once, instead of waiting for a
// drain that may take minutes. The test runs itself as a child process, so
// the real signals never reach the test binary.
func TestSecondSignalExits(t *testing.T) {
	if os.Getenv(signalChildEnv) == "1" {
		signalChild()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSecondSignalExits$", "-test.count=1")
	cmd.Env = append(os.Environ(), signalChildEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	lines := make(chan string, 16) // the child prints three lines at most
	waited := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
		waited <- cmd.Wait() // only after every read from the pipe
	}()
	waitLine := func(want string) {
		t.Helper()
		deadline := time.After(20 * time.Second)
		for {
			select {
			case l, ok := <-lines:
				require.True(t, ok, "the child exited before printing %q", want)
				if l == want {
					return
				}
			case <-deadline:
				t.Fatalf("the child never printed %q", want)
			}
		}
	}
	waitLine("child: ready")
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	waitLine("child: draining") // the first signal did not end the process

	// The second signal: resent every 500 ms in case it lands before the
	// handler is released; without notifyOnce's release every one of them is
	// swallowed and the child sleeps on through its 30 s "drain".
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(5 * time.Second)
	for {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-waited:
			var ee *exec.ExitError
			require.True(t, errors.As(err, &ee), "the child exited normally: %v", err)
			ws, ok := ee.Sys().(syscall.WaitStatus)
			require.True(t, ok)
			require.True(t, ws.Signaled(), "the child was not ended by a signal")
			require.Equal(t, syscall.SIGTERM, ws.Signal())
			return
		case <-tick.C:
		case <-deadline:
			t.Fatal("a second SIGTERM did not end the process")
		}
	}
}

// signalChild is the child side: it waits for the first signal, then sleeps
// through a "drain" far longer than the parent waits.
func signalChild() {
	ctx, stop := notifyOnce()
	defer stop()
	_, _ = fmt.Fprintln(os.Stdout, "child: ready")
	<-ctx.Done()
	_, _ = fmt.Fprintln(os.Stdout, "child: draining")
	time.Sleep(30 * time.Second)
	_, _ = fmt.Fprintln(os.Stdout, "child: drain ended") // the test then passes: exit 0
}

//go:build unix

package panel_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AlexK-Notable/herdr-panel/panel"
)

const helperEnv = "HERDR_PANEL_TEARDOWN_HELPER"

// TestTeardownHelper is not a test: TestCloseCadence starts the test binary
// again with helperEnv set, and this runs a real panel in that process, so
// signals reach it and not the test runner.
func TestTeardownHelper(t *testing.T) { //nolint:paralleltest // a subprocess body
	spec := os.Getenv(helperEnv)
	if spec == "" {
		t.Skip("run by TestCloseCadence")
	}
	err := panel.RunFile(context.Background(), spec, panel.Options{Width: 80, Height: 24, Context: map[string]string{}, CrashDir: filepath.Dir(spec)})
	if sig, ok := errors.AsType[*panel.SignalError](err); ok {
		os.Exit(128 + int(sig.Signal.(syscall.Signal))) //nolint:forcetypeassert // unix signals
	}
	fmt.Fprintln(os.Stderr, "helper:", err)
	os.Exit(1)
}

// herdr closes a popup with SIGHUP, then SIGTERM, then SIGKILL, 250 ms
// apart. The panel must be gone before the second signal, and must take a
// running command with it even when that command ignores SIGHUP: only the
// process-group kill reaches it. The command here is a grandchild of the
// panel (sh starts it), so killing the panel's direct child is not enough.
func TestCloseCadence(t *testing.T) {
	t.Parallel()
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			pidFile := filepath.Join(dir, "child.pid")
			specFile := filepath.Join(dir, "panel.toml")
			src := fmt.Sprintf(`spec = 1
title = "Teardown"
[[section]]
kind = "text"
source = { run = ["sh", "-c", "trap '' HUP; sleep 30 & echo $! > %s; wait"], timeout_s = 60 }
`, pidFile)
			if err := os.WriteFile(specFile, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			stdinR, stdinW, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stdinW.Close() }()
			devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = devnull.Close() }()
			env := append(os.Environ(), helperEnv+"="+specFile)
			proc, err := os.StartProcess(os.Args[0], []string{os.Args[0], "-test.run=^TestTeardownHelper$"}, &os.ProcAttr{
				Env: env, Files: []*os.File{stdinR, devnull, devnull},
			})
			if err != nil {
				t.Fatal(err)
			}
			_ = stdinR.Close()
			defer func() { _ = proc.Kill() }()

			child := waitForPid(t, pidFile)
			start := time.Now()
			if err := proc.Signal(sig); err != nil {
				t.Fatal(err)
			}
			done := make(chan *os.ProcessState, 1)
			go func() {
				st, _ := proc.Wait()
				done <- st
			}()
			var st *os.ProcessState
			select {
			case st = <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("the panel ignored %s for 2 s", sig)
			}
			took := time.Since(start)
			if want := 128 + int(sig); st.ExitCode() != want {
				t.Errorf("exit status %d, want %d", st.ExitCode(), want)
			}
			if took > 250*time.Millisecond {
				t.Errorf("the panel took %s to end after %s; herdr sends the next signal at 250 ms", took, sig)
			}
			t.Logf("%s: ended in %s", sig, took)
			if alive(child, time.Second) {
				t.Errorf("the grandchild (pid %d) that ignores SIGHUP outlived the panel", child)
				_ = syscall.Kill(child, syscall.SIGKILL)
			}
		})
	}
}

func waitForPid(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.HasSuffix(string(data), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the panel's command never started")
	return 0
}

// alive reports whether pid still exists after waiting up to d for it to go.
func alive(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return false
		}
		if time.Now().After(deadline) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
}

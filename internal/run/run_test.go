package run

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func sh(script string) []string { return []string{"/bin/sh", "-c", script} }

func TestSuccessAndMessage(t *testing.T) {
	t.Parallel()
	r := Exec{}.Run(context.Background(), Request{Argv: []string{"echo", "hello world"}})
	if !r.OK || r.ExitCode != 0 || r.Message != "hello world" {
		t.Errorf("%+v", r)
	}
}

// Arguments are passed as given: no shell ever sees them.
func TestArgumentsAreNotInterpreted(t *testing.T) {
	t.Parallel()
	hostile := `$(touch /tmp/should-not-exist-herdr-panel); "q" 'x' *`
	r := Exec{}.Run(context.Background(), Request{Argv: []string{"printf", "%s", hostile}})
	if string(r.Stdout) != hostile {
		t.Errorf("stdout %q", r.Stdout)
	}
	if _, err := os.Stat("/tmp/should-not-exist-herdr-panel"); err == nil {
		t.Error("a shell ran the argument")
	}
}

func TestFailureShowsStderr(t *testing.T) {
	t.Parallel()
	r := Exec{}.Run(context.Background(), Request{Argv: sh("echo progress; echo 'boom: no such thing' >&2; exit 3")})
	if r.OK || r.ExitCode != 3 || r.Message != "boom: no such thing" || r.Err == nil {
		t.Errorf("%+v", r)
	}
}

func TestExitZeroWithOkFalseIsFailure(t *testing.T) {
	t.Parallel()
	r := Exec{}.Run(context.Background(), Request{Argv: sh(`echo '{"ok": false, "message": "quota exceeded"}'`)})
	if r.OK || r.ExitCode != 0 || r.Message != "quota exceeded" || r.Err == nil {
		t.Errorf("%+v", r)
	}
	r = Exec{}.Run(context.Background(), Request{Argv: sh(`echo '{"ok": true, "message": "renamed"}'`)})
	if !r.OK || r.Message != "renamed" {
		t.Errorf("%+v", r)
	}
}

func TestTimeoutKillsTheWholeGroup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	start := time.Now()
	r := Exec{}.Run(context.Background(), Request{
		Argv:    sh("sleep 30 & echo $! > " + pidFile + "; wait"),
		Timeout: 300 * time.Millisecond,
	})
	if !r.TimedOut || r.OK || time.Since(start) > 5*time.Second {
		t.Fatalf("%+v after %v", r, time.Since(start))
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return // the grandchild is gone
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("grandchild %d outlived the timeout", pid)
}

func TestFloodIsCappedAndDoesNotBlock(t *testing.T) {
	t.Parallel()
	r := Exec{}.Run(context.Background(), Request{
		Argv:    sh("head -c 5000000 /dev/zero; echo done >&2"),
		Timeout: 10 * time.Second,
	})
	if !r.OK || !r.Truncated || len(r.Stdout) != MaxOutput {
		t.Errorf("ok=%v truncated=%v len=%d err=%v", r.OK, r.Truncated, len(r.Stdout), r.Err)
	}
}

// A command that exits while a background child keeps stdout open must not
// hang the panel: WaitDelay ends the wait.
func TestGrandchildHoldingStdoutDoesNotHang(t *testing.T) {
	t.Parallel()
	start := time.Now()
	r := Exec{}.Run(context.Background(), Request{Argv: sh("sleep 20 & echo started"), Timeout: 15 * time.Second})
	if time.Since(start) > 6*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	if !r.OK || r.Message != "started" {
		t.Errorf("%+v", r)
	}
}

func TestCancelKillsCommand(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	start := time.Now()
	r := Exec{}.Run(ctx, Request{Argv: []string{"sleep", "30"}, Timeout: 20 * time.Second})
	if r.OK || r.TimedOut || time.Since(start) > 5*time.Second || !strings.Contains(r.Message, "cancelled") {
		t.Errorf("%+v after %v", r, time.Since(start))
	}
}

func TestEnvAndDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := Exec{}.Run(context.Background(), Request{Argv: sh(`printf '%s|%s' "$PANEL_X" "$PWD"`), Env: []string{"PANEL_X=1 2"}, Dir: dir})
	if got := string(r.Stdout); got != "1 2|"+dir {
		t.Errorf("got %q", got)
	}
	if os.Getenv("HOME") != "" {
		r = Exec{}.Run(context.Background(), Request{Argv: sh(`printf %s "$HOME"`)})
		if string(r.Stdout) != os.Getenv("HOME") {
			t.Error("the environment was not inherited")
		}
	}
}

func TestMissingProgram(t *testing.T) {
	t.Parallel()
	r := Exec{}.Run(context.Background(), Request{Argv: []string{"/nonexistent/prog"}})
	if r.OK || r.Err == nil || r.Message == "" {
		t.Errorf("%+v", r)
	}
	if r := (Exec{}).Run(context.Background(), Request{}); r.Err == nil {
		t.Error("empty argv ran")
	}
}

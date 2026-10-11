//go:build unix

package run

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

//nolint:paralleltest // replaces afterExitPID
func TestAfterExitWaitsForTheProcessThenRuns(t *testing.T) {
	parent := exec.Command("sleep", "1")
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = parent.Wait(); close(exited) }()
	defer func(f func() int) { afterExitPID = f }(afterExitPID)
	afterExitPID = func() int { return parent.Process.Pid }

	out := filepath.Join(t.TempDir(), "out")
	odd := `a b; echo $(id) "q" 'r'`
	res := Exec{}.Run(context.Background(), Request{
		Argv:      []string{"/bin/sh", "-c", `printf '%s' "$1" > "$2"`, "sh", odd, out},
		AfterExit: true,
	})
	if !res.OK {
		t.Fatalf("start: %+v", res)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(out); err == nil {
		t.Fatal("the command ran while the process it waits for was alive")
	}
	<-exited
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(out)
		if err == nil && len(data) > 0 {
			if string(data) != odd {
				t.Fatalf("the command got %q, want %q unchanged", data, odd)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the command never ran after the process exited")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

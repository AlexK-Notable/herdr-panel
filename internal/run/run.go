// Package run executes a spec's commands: never through a shell, always with
// a timeout and an output cap, in a process group that is killed whole when
// the command times out or the panel quits. It is the only package allowed
// to import os/exec.
package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// MaxOutput caps what is kept of each of stdout and stderr. A command may
// write more; the rest is read and dropped, so it never blocks on a full pipe.
const MaxOutput = 1 << 20

// waitDelay bounds how long Wait lingers after the command exits while a
// grandchild still holds its output open.
const waitDelay = 2 * time.Second

// Request is one command to run.
type Request struct {
	Argv    []string
	Env     []string // added to the inherited environment, KEY=value
	Dir     string
	Timeout time.Duration
}

// Result is what happened.
type Result struct {
	Stdout    []byte
	Stderr    []byte
	Truncated bool // output went past MaxOutput
	ExitCode  int  // -1 when the command did not exit on its own
	TimedOut  bool
	Duration  time.Duration
	Err       error // could not start, timed out, killed, or failed

	// OK and Message follow the result contract: a command printing
	// {"ok": false, "message": "..."} failed even when it exited 0.
	OK      bool
	Message string
}

// Runner runs commands. The panel takes one so observe and tests can run
// without executing anything.
type Runner interface {
	Run(ctx context.Context, req Request) Result
}

// Exec runs commands for real.
type Exec struct{}

// Run implements Runner.
func (Exec) Run(ctx context.Context, req Request) Result {
	start := time.Now()
	if len(req.Argv) == 0 || req.Argv[0] == "" {
		return Result{ExitCode: -1, Err: errors.New("no program to run")}
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, req.Argv[0], req.Argv[1:]...)
	cmd.Env = append(os.Environ(), req.Env...)
	cmd.Dir = req.Dir
	cmd.Stdin = nil // /dev/null: a command must never wait on the panel's keyboard
	var stdout, stderr capped
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = waitDelay
	setGroup(cmd)

	err := cmd.Run()
	res := Result{
		Stdout:    stdout.buf.Bytes(),
		Stderr:    stderr.buf.Bytes(),
		Truncated: stdout.dropped || stderr.dropped,
		ExitCode:  -1,
		Duration:  time.Since(start),
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	switch {
	case errors.Is(cctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
		res.TimedOut = true
		res.Err = fmt.Errorf("timed out after %s", timeout)
	case ctx.Err() != nil:
		res.Err = fmt.Errorf("cancelled: %w", ctx.Err())
	case err != nil && errors.Is(err, exec.ErrWaitDelay):
		// The command exited; something it started kept the output open.
		// Its exit status stands.
	case err != nil:
		res.Err = err
	}
	Interpret(&res)
	return res
}

// Interpret applies the result contract and picks the message to show.
func Interpret(r *Result) {
	r.OK = r.Err == nil && r.ExitCode == 0
	out := bytes.TrimSpace(r.Stdout)
	if bytes.HasPrefix(out, []byte("{")) {
		var c struct {
			OK      *bool  `json:"ok"`
			Message string `json:"message"`
		}
		if json.Unmarshal(out, &c) == nil && c.OK != nil {
			r.Message = c.Message
			if !*c.OK {
				r.OK = false
				if r.Err == nil {
					r.Err = errors.New(orDefault(c.Message, "the command reported failure"))
				}
			}
			return
		}
	}
	switch {
	case r.OK:
		r.Message = firstLine(string(out))
	case r.Err != nil && r.TimedOut:
		r.Message = r.Err.Error()
	default:
		msg := lastLine(string(bytes.TrimSpace(r.Stderr)))
		if msg == "" && r.Err != nil {
			msg = r.Err.Error()
		}
		if msg == "" {
			msg = fmt.Sprintf("exit status %d", r.ExitCode)
		}
		r.Message = msg
		if r.Err == nil {
			r.Err = errors.New(msg)
		}
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return strings.TrimSpace(s)
}

func lastLine(s string) string {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

// capped keeps the first MaxOutput bytes and reports every write as complete,
// so a chatty command is never blocked, and never killed by a short write.
type capped struct {
	buf     bytes.Buffer
	dropped bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := MaxOutput - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
			c.dropped = true
		} else {
			c.buf.Write(p)
		}
	} else if len(p) > 0 {
		c.dropped = true
	}
	return len(p), nil
}

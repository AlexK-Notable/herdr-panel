// Package panel runs a herdr-panel spec as a terminal panel.
//
//	s, err := spec.Load("panel.toml")
//	...
//	err = panel.Run(ctx, s, panel.Options{})
//
// RunFile does both, and shows a spec's problems inside the panel instead of
// exiting: in a herdr popup, a process that exits takes its message with it.
package panel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/AlexK-Notable/herdr-panel/herdr"
	"github.com/AlexK-Notable/herdr-panel/internal/atomicfile"
	"github.com/AlexK-Notable/herdr-panel/internal/ui"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

// Options configure Run.
type Options struct {
	Input  io.Reader // nil for os.Stdin
	Output io.Writer // nil for os.Stdout
	// Via is passed to every command as PANEL_VIA: how the panel was opened.
	Via string
	// Context supplies {ctx.*} values. nil reads them from herdr's
	// environment (see herdr.FromEnv).
	Context map[string]string
	// CrashDir is where a crash log goes; "" uses herdr's state directory for
	// the plugin, or herdr-panel's own.
	CrashDir string
}

// SignalError reports that a signal ended the panel. herdr closes a popup
// with SIGHUP; the CLI turns this into exit status 128 + the signal number.
type SignalError struct{ Signal os.Signal }

func (e *SignalError) Error() string { return "ended by " + e.Signal.String() }

// ErrCrashed means the panel caught a panic; it showed the details and wrote
// a crash log before ending.
var ErrCrashed = errors.New("herdr-panel crashed")

// ErrSpec wraps a spec that could not be loaded; the panel showed why.
var ErrSpec = errors.New("the spec has problems")

// RunFile loads the spec at path and runs it. When the spec does not load,
// the panel shows its errors until a key is pressed, then returns an error
// wrapping ErrSpec.
func RunFile(ctx context.Context, path string, opts Options) error {
	s, err := spec.Load(path)
	if err != nil {
		if rerr := run(ctx, ui.Options{LoadErr: err}, opts); rerr != nil {
			return rerr
		}
		return fmt.Errorf("%w: %w", ErrSpec, err)
	}
	return Run(ctx, s, opts)
}

// Run runs a validated spec until the user closes it, a signal arrives, or
// ctx ends.
func Run(ctx context.Context, s *spec.Spec, opts Options) error {
	hctx := herdr.FromEnv(os.Getenv)
	values := opts.Context
	if values == nil {
		values = hctx.Values()
	}
	if opts.CrashDir == "" {
		opts.CrashDir = hctx.StateDir
	}
	sweepStores(s, values)
	return run(ctx, ui.Options{Spec: s, Context: values, Via: opts.Via, CrashDir: opts.CrashDir}, opts)
}

// sweepStores removes temporary files a killed panel left beside its
// stores. herdr closes a popup with SIGHUP, then SIGKILL 500 ms later, so a
// save can be cut between writing and renaming.
func sweepStores(s *spec.Spec, values map[string]string) {
	vals := spec.ValuesFunc(func(ns, name string) (string, bool) {
		switch ns {
		case spec.NSCtx:
			v, ok := values[name]
			return v, ok
		case spec.NSEnv:
			return os.LookupEnv(name)
		}
		return "", false
	})
	for _, st := range s.Stores {
		if p, err := spec.ExpandPath(st.Path, vals, s.Dir); err == nil {
			_, _ = atomicfile.Sweep(filepath.Dir(p), time.Minute)
		}
	}
}

func run(ctx context.Context, uopts ui.Options, opts Options) error {
	in, out := opts.Input, opts.Output
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	uopts.Parent = ctx
	if uopts.CrashDir == "" {
		uopts.CrashDir = opts.CrashDir
	}
	m := ui.New(uopts)

	// Bubble Tea handles SIGINT and SIGTERM, not SIGHUP, which is how herdr
	// closes a popup. The panel takes all three, so each cancels running
	// commands the same way, within herdr's 250 ms grace.
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigs)

	// Bubble Tea's own panic catch prints the stack to stderr, which a popup
	// takes away with it; the panel catches its own panics, and this sends
	// the backstop's output to a file that outlives the popup.
	crashLog := filepath.Join(crashDir(uopts.CrashDir), "stderr-"+time.Now().Format("20060102-150405")+".log")
	restore := redirectStderr(crashLog)
	defer restore()

	p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out), tea.WithContext(ctx), tea.WithoutSignalHandler())
	var got atomic.Value // os.Signal
	go func() {
		for s := range sigs {
			got.Store(s)
			p.Send(ui.QuitRequest{})
		}
	}()
	_, err := p.Run()
	restore()
	switch {
	case errors.Is(err, tea.ErrProgramPanic):
		hold(in, out, "herdr-panel crashed; details are in "+crashLog)
		return ErrCrashed
	case errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil:
		return ctx.Err()
	case err != nil && !errors.Is(err, tea.ErrInterrupted):
		return err
	case got.Load() != nil:
		sig, _ := got.Load().(os.Signal)
		return &SignalError{Signal: sig}
	case m.ExitCode != 0:
		return ErrCrashed
	}
	return nil
}

func crashDir(d string) string {
	if d == "" {
		return filepath.Join(os.TempDir(), "herdr-panel")
	}
	return d
}

// redirectStderr points os.Stderr at a file for the program's lifetime, and
// removes the file again if nothing was written to it.
func redirectStderr(path string) func() {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return func() {}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // G304: our own log path
	if err != nil {
		return func() {}
	}
	orig := os.Stderr
	os.Stderr = f
	done := false
	return func() {
		if done {
			return
		}
		done = true
		os.Stderr = orig
		info, statErr := f.Stat()
		_ = f.Close()
		if statErr == nil && info.Size() == 0 {
			_ = os.Remove(path)
		}
	}
}

// hold keeps a message on screen until a key is pressed (or 30 s pass): in a
// herdr popup, exiting would close the popup and the message with it.
func hold(in io.Reader, out io.Writer, msg string) {
	fmt.Fprintf(out, "\x1b[?1049h\x1b[2J\x1b[H %s\r\n\r\n press any key to close\r\n", msg)
	defer fmt.Fprint(out, "\x1b[?1049l")
	if f, ok := in.(*os.File); ok && term.IsTerminal(f.Fd()) {
		if st, err := term.MakeRaw(f.Fd()); err == nil {
			defer func() { _ = term.Restore(f.Fd(), st) }()
		}
	}
	done := make(chan struct{})
	go func() {
		_, _ = in.Read(make([]byte, 16))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
}

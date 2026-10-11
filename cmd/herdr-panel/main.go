// Command herdr-panel draws a terminal panel from a spec file: settings
// fields, lists with row actions and buttons, driven by mouse and keyboard.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/AlexK-Notable/herdr-panel/herdr"
	"github.com/AlexK-Notable/herdr-panel/internal/observe"
	"github.com/AlexK-Notable/herdr-panel/internal/store"
	"github.com/AlexK-Notable/herdr-panel/internal/ui"
	"github.com/AlexK-Notable/herdr-panel/panel"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

// Exit codes are part of the CLI's contract (docs/PLAN.md, contract item 3).
const (
	exitOK      = 0
	exitRuntime = 1
	exitUsage   = 2
	exitSpec    = 3
)

const usage = `usage: herdr-panel <spec.toml>
       herdr-panel validate <spec.toml>...
       herdr-panel observe <spec.toml> [--size WxH] [--script file] [--sandbox] [--stub file] [--json]
       herdr-panel schema
       herdr-panel version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintln(stdout, "herdr-panel", version())
		return exitOK
	case "schema":
		_, _ = stdout.Write(spec.JSONSchema())
		return exitOK
	case "validate":
		return validate(args[1:], stdout, stderr)
	case "observe":
		return observeCmd(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	}
	if strings.HasPrefix(args[0], "-") || len(args) > 1 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	return runPanel(args[0], stderr)
}

// runPanel runs a spec in this terminal.
func runPanel(path string, stderr io.Writer) int {
	err := panel.RunFile(context.Background(), path, panel.Options{Via: os.Getenv("PANEL_VIA")})
	var sig *panel.SignalError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &sig):
		if n, ok := sig.Signal.(syscall.Signal); ok {
			return 128 + int(n)
		}
		return exitRuntime
	case errors.Is(err, panel.ErrSpec):
		fmt.Fprintln(stderr, err)
		return exitSpec
	default:
		fmt.Fprintln(stderr, "herdr-panel:", err)
		return exitRuntime
	}
}

// version reports the module version a consumer built, or "devel".
func version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "devel"
}

// validate checks each spec and reports every problem with file:line:col.
func validate(paths []string, stdout, stderr io.Writer) int {
	if len(paths) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	code := exitOK
	for _, p := range paths {
		if _, err := spec.Load(p); err != nil {
			fmt.Fprintln(stderr, err)
			if spec.IsSpecError(err) {
				code = max(code, exitSpec)
			} else {
				code = max(code, exitRuntime)
			}
			continue
		}
		fmt.Fprintf(stdout, "%s: ok\n", p)
	}
	if code == exitRuntime {
		return exitRuntime // an unreadable file outranks a bad one
	}
	return code
}

// observeCmd renders a panel headlessly and prints it: the frame, every
// clickable element and the state, after an optional script of actions.
func observeCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("observe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	size := fs.String("size", "80x24", "terminal size, WIDTHxHEIGHT")
	script := fs.String("script", "", "a file of actions to run (click, key, type, paste, resize, snapshot); - for stdin")
	sandbox := fs.Bool("sandbox", false, "run no commands, and write stores to temporary copies")
	stubFile := fs.String("stub", "", "JSON answers for commands in the sandbox (implies --sandbox)")
	asJSON := fs.Bool("json", false, "print JSON (version 1) instead of the frame")
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = append(args[1:], args[0]) // the spec may come first
	}
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	w, h, err := observe.ParseSize(*size)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	s, err := spec.Load(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		if spec.IsSpecError(err) {
			return exitSpec
		}
		return exitRuntime
	}
	opts := ui.Options{Spec: s, Context: herdr.FromEnv(os.Getenv).Values(), Via: "observe"}
	var sb *observe.Sandbox
	if *sandbox || *stubFile != "" {
		sb = &observe.Sandbox{}
		if *stubFile != "" {
			if sb.Stubs, err = observe.LoadStubs(*stubFile); err != nil {
				fmt.Fprintln(stderr, err)
				return exitUsage
			}
		}
		opts.Runner = sb
		tmp, err := os.MkdirTemp("", "herdr-panel-observe-")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return exitRuntime
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		if opts.Stores, err = sandboxStores(s, opts.Context, tmp); err != nil {
			fmt.Fprintln(stderr, err)
			return exitRuntime
		}
	}
	d := observe.New(opts, w, h)
	snaps := []observe.Snapshot{}
	code := exitOK
	if *script != "" {
		var r io.Reader = os.Stdin
		if *script != "-" {
			f, err := os.Open(*script)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return exitUsage
			}
			defer func() { _ = f.Close() }()
			r = f
		}
		got, err := d.RunScript(r)
		snaps = append(snaps, got...)
		if err != nil {
			fmt.Fprintln(stderr, err)
			code = exitRuntime
		}
	}
	snaps = append(snaps, d.Snapshot("final"))
	for _, e := range d.Errors() {
		fmt.Fprintln(stderr, "observe:", e)
	}
	if *asJSON {
		out := struct {
			Observe   int                `json:"observe"`
			Snapshots []observe.Snapshot `json:"snapshots"`
			Commands  []observe.Call     `json:"commands,omitempty"`
		}{Observe: observe.Version, Snapshots: snaps}
		if sb != nil {
			out.Commands = sb.Calls()
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return exitRuntime
		}
		return code
	}
	for _, sn := range snaps {
		if len(snaps) > 1 {
			fmt.Fprintf(stdout, "== %s\n", sn.Label)
		}
		fmt.Fprintln(stdout, sn.Frame)
		fmt.Fprintln(stdout, "--")
		for _, e := range sn.Elements {
			mark := " "
			if e.Focused {
				mark = "*"
			}
			fmt.Fprintf(stdout, "%s %-28s %-13s %3d,%-3d %q\n", mark, e.ID, e.Kind, e.X, e.Y, e.Label)
		}
	}
	if sb != nil {
		for _, c := range sb.Calls() {
			fmt.Fprintf(stdout, "ran: %q\n", c.Argv)
		}
	}
	return code
}

// sandboxStores copies each store's file into tmp, so a sandboxed observe
// can save without touching the real one.
func sandboxStores(s *spec.Spec, ctxValues map[string]string, tmp string) (map[string]store.Store, error) {
	vals := spec.ValuesFunc(func(ns, name string) (string, bool) {
		switch ns {
		case spec.NSCtx:
			v, ok := ctxValues[name]
			return v, ok
		case spec.NSEnv:
			return os.LookupEnv(name)
		}
		return "", false
	})
	out := map[string]store.Store{}
	for _, st := range s.Stores {
		dst := filepath.Join(tmp, st.Name+".env")
		if p, err := spec.ExpandPath(st.Path, vals, s.Dir); err == nil {
			if data, err := os.ReadFile(p); err == nil { //nolint:gosec // G304: the spec's own store file
				if err := os.WriteFile(dst, data, 0o600); err != nil { //nolint:gosec // G703: dst is in our own temp dir
					return nil, err
				}
			}
		}
		out[st.Name] = &store.EnvFile{Path: dst, Create: true}
	}
	return out, nil
}

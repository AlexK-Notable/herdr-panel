// Command herdr-panel draws a terminal panel from a spec file: settings
// fields, lists with row actions and buttons, driven by mouse and keyboard.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"

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
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	}
	fmt.Fprint(stderr, usage)
	return exitUsage
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

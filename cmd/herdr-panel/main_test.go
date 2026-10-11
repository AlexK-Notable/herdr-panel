package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExitCodes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		args []string
		code int
		out  string
	}{
		{nil, exitUsage, ""},
		{[]string{"version"}, exitOK, "herdr-panel "},
		{[]string{"--help"}, exitOK, "usage:"},
		{[]string{"--bogus"}, exitUsage, ""},
		{[]string{"a.toml", "b.toml"}, exitUsage, ""},
	}
	for _, c := range cases {
		var stdout, stderr bytes.Buffer
		if got := run(c.args, &stdout, &stderr); got != c.code {
			t.Errorf("run(%q) = %d, want %d", c.args, got, c.code)
		}
		if !strings.Contains(stdout.String(), c.out) {
			t.Errorf("run(%q) stdout = %q, want it to contain %q", c.args, stdout.String(), c.out)
		}
	}
}

func TestValidateExitCodes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := filepath.Join(dir, "good.toml")
	bad := filepath.Join(dir, "bad.toml")
	missing := filepath.Join(dir, "missing.toml")
	for p, src := range map[string]string{
		good: "spec = 1\ntitle = \"T\"\n[[section]]\nkind = \"text\"\ntext = \"x\"\n",
		bad:  "spec = 1\ntitle = 3\n",
	} {
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		paths []string
		code  int
	}{
		{[]string{good}, exitOK},
		{[]string{good, bad}, exitSpec},
		{[]string{missing}, exitRuntime},
		// An unreadable file outranks a bad one, in either order.
		{[]string{bad, missing}, exitRuntime},
		{[]string{missing, bad}, exitRuntime},
	} {
		var stdout, stderr bytes.Buffer
		if got := run(append([]string{"validate"}, c.paths...), &stdout, &stderr); got != c.code {
			t.Errorf("validate %q = %d, want %d\n%s", c.paths, got, c.code, stderr.String())
		}
	}
}

// A view section is valid, but only a Go program can supply the view: the
// binary refuses to run it with exit status 1, not 3.
func TestViewSpecNeedsAProgram(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "view.toml")
	src := "spec = 1\ntitle = \"T\"\n[[section]]\nkind = \"view\"\nview = \"graph\"\n"
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args []string
		code int
	}{
		{[]string{"validate", p}, exitOK},
		{[]string{"observe", p}, exitRuntime},
		// Running it shows the refusal on screen first; panel's
		// TestMissingViewIsShownThenRefused covers that path.
	} {
		var stdout, stderr bytes.Buffer
		got := run(c.args, &stdout, &stderr)
		if got != c.code {
			t.Errorf("%q = %d, want %d\n%s", c.args, got, c.code, stderr.String())
		}
		if c.code == exitRuntime && !strings.Contains(stderr.String(), "view graph") {
			t.Errorf("%q stderr = %q", c.args, stderr.String())
		}
	}
}

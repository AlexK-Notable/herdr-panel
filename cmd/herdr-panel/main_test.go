package main

import (
	"bytes"
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
		{[]string{"bogus"}, exitUsage, ""},
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

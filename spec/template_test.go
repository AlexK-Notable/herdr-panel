package spec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func values(m map[string]string) Values {
	return ValuesFunc(func(ns, name string) (string, bool) {
		v, ok := m[ns+"."+name]
		return v, ok
	})
}

func TestTemplateExpand(t *testing.T) {
	t.Parallel()
	v := values(map[string]string{"row.id": "w1", "env.HOME": "/h", "env.EMPTY": "", "ctx.cwd": "/c"})
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"{row.id}", "w1"},
		{"--id={row.id}!", "--id=w1!"},
		{"{env.HOME}/x", "/h/x"},
		{"{env.NOPE:-dflt}", "dflt"},
		{"{env.EMPTY:-dflt}", "dflt"},
		{"{env.HOME:-dflt}", "/h"},
		{"{{literal}}", "{literal}"},
		{"{ctx.cwd}{row.id}", "/cw1"},
	}
	for _, c := range cases {
		tpl, err := ParseTemplate(c.in)
		if err != nil {
			t.Errorf("parse %q: %v", c.in, err)
			continue
		}
		got, err := tpl.Expand(v)
		if err != nil || got != c.want {
			t.Errorf("expand %q = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestTemplateSyntaxErrors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"{", "}", "{row}", "{.x}", "{row.}", "{env.1X}", "a{row.id"} {
		if _, err := ParseTemplate(in); err == nil {
			t.Errorf("ParseTemplate(%q) accepted", in)
		}
	}
}

func TestTemplateMissingValue(t *testing.T) {
	t.Parallel()
	tpl := MustTemplate("{row.id}")
	if _, err := tpl.Expand(values(nil)); !errors.Is(err, ErrMissing) {
		t.Errorf("err = %v, want ErrMissing", err)
	}
}

// A value with spaces, quotes and shell syntax must stay exactly one
// argument: this is the property that keeps spec commands injection-free.
func TestExpandArgvKeepsOneArgumentPerElement(t *testing.T) {
	t.Parallel()
	hostile := `a b "c" $(rm -rf ~); echo 'x' | y`
	argv := []Template{MustTemplate("prog"), MustTemplate("--name={row.title}"), MustTemplate("{row.title}")}
	out, err := ExpandArgv(argv, values(map[string]string{"row.title": hostile}), "/spec")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 || out[1] != "--name="+hostile || out[2] != hostile {
		t.Errorf("argv = %q", out)
	}
}

func TestExpandArgvResolvesRelativePrograms(t *testing.T) {
	t.Parallel()
	cases := map[string]string{"./bin/x": "/spec/bin/x", "bin/x": "/spec/bin/x", "herdr": "herdr", "/usr/bin/true": "/usr/bin/true"}
	for in, want := range cases {
		out, err := ExpandArgv([]Template{MustTemplate(in)}, nil, "/spec")
		if err != nil || out[0] != want {
			t.Errorf("%q -> %q, %v; want %q", in, out, err, want)
		}
	}
}

func TestExpandPath(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	cases := map[string]string{
		"~/x.env":                     filepath.Join(home, "x.env"),
		"rel/x.env":                   "/spec/rel/x.env",
		"/abs/x.env":                  "/abs/x.env",
		"{env.NOPE:-~/.config}/a.env": filepath.Join(home, ".config/a.env"),
	}
	for in, want := range cases {
		got, err := ExpandPath(MustTemplate(in), values(nil), "/spec")
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v; want %q", in, got, err, want)
		}
	}
}

package spec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Template is a string with placeholders: {row.<column>}, {field.<id>},
// {ctx.<name>}, {env.<NAME>} and {env.<NAME>:-default}. {{ and }} stand for
// literal braces.
//
// A template always expands to exactly one string. In an argument vector each
// element expands on its own, so a value with spaces or quotes stays one
// argument and can never become two, or become shell syntax.
type Template struct {
	Raw   string
	parts []part
	pos   Pos // where it was written, for errors
}

type part struct {
	lit      string
	ns, name string
	def      string
	hasDef   bool
}

// Namespaces a placeholder may use.
const (
	NSRow   = "row"
	NSField = "field"
	NSCtx   = "ctx"
	NSEnv   = "env"
)

// CtxNames are the names {ctx.*} accepts.
var CtxNames = []string{
	"workspace_id", "tab_id", "pane_id", "cwd",
	"plugin_id", "config_dir", "state_dir", "spec_dir",
}

// ParseTemplate parses s. It checks syntax only; which namespaces and names
// are allowed depends on where the template is used (see Validate).
func ParseTemplate(s string) (Template, error) {
	t := Template{Raw: s}
	var lit strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '{' && i+1 < len(s) && s[i+1] == '{':
			lit.WriteByte('{')
			i++
		case c == '}' && i+1 < len(s) && s[i+1] == '}':
			lit.WriteByte('}')
			i++
		case c == '}':
			return Template{}, fmt.Errorf("unmatched } at offset %d (write }} for a literal brace)", i)
		case c == '{':
			body, end, ok := placeholderBody(s, i)
			if !ok {
				return Template{}, fmt.Errorf("unclosed { at offset %d (write {{ for a literal brace)", i)
			}
			p, err := parsePlaceholder(body)
			if err != nil {
				return Template{}, err
			}
			if lit.Len() > 0 {
				t.parts = append(t.parts, part{lit: lit.String()})
				lit.Reset()
			}
			t.parts = append(t.parts, p)
			i = end
		default:
			lit.WriteByte(c)
		}
	}
	if lit.Len() > 0 {
		t.parts = append(t.parts, part{lit: lit.String()})
	}
	return t, nil
}

// placeholderBody reads the placeholder that opens at s[open]. Inside an
// {env.X:-default}, {{ and }} are literal braces too. It returns the body
// with those unescaped, and the index of the closing }.
func placeholderBody(s string, open int) (string, int, bool) {
	var b strings.Builder
	inDefault := false
	for j := open + 1; j < len(s); j++ {
		switch {
		case inDefault && j+1 < len(s) && s[j] == '}' && s[j+1] == '}':
			b.WriteByte('}')
			j++
		case inDefault && j+1 < len(s) && s[j] == '{' && s[j+1] == '{':
			b.WriteByte('{')
			j++
		case s[j] == '}':
			return b.String(), j, true
		default:
			b.WriteByte(s[j])
			if !inDefault && strings.HasSuffix(b.String(), ":-") {
				inDefault = true
			}
		}
	}
	return "", 0, false
}

// parsed returns t with its parts, parsing Raw when t was written as a Go
// literal, Template{Raw: "..."}.
func (t Template) parsed() (Template, error) {
	if t.parts != nil || t.Raw == "" {
		return t, nil
	}
	p, err := ParseTemplate(t.Raw)
	if err != nil {
		return t, err
	}
	p.pos = t.pos
	return p, nil
}

// MustTemplate parses s and panics on a syntax error; for specs built in Go.
func MustTemplate(s string) Template {
	t, err := ParseTemplate(s)
	if err != nil {
		panic(err)
	}
	return t
}

func parsePlaceholder(body string) (part, error) {
	ns, rest, ok := strings.Cut(body, ".")
	if !ok || ns == "" || rest == "" {
		return part{}, fmt.Errorf("placeholder {%s} must be {namespace.name}", body)
	}
	p := part{ns: ns, name: rest}
	if ns == NSEnv {
		if name, def, found := strings.Cut(rest, ":-"); found {
			p.name, p.def, p.hasDef = name, def, true
		}
	}
	if !validName(p.name, ns == NSEnv) {
		return part{}, fmt.Errorf("placeholder {%s} has an invalid name %q", body, p.name)
	}
	return p, nil
}

func validName(s string, env bool) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		case r == '-' && !env && i > 0:
		default:
			return false
		}
	}
	return true
}

// Placeholders returns the namespace and name of each placeholder.
func (t Template) Placeholders() [][2]string {
	t, _ = t.parsed()
	var out [][2]string
	for _, p := range t.parts {
		if p.ns != "" {
			out = append(out, [2]string{p.ns, p.name})
		}
	}
	return out
}

// IsZero reports whether the template is empty.
func (t Template) IsZero() bool { return t.Raw == "" && len(t.parts) == 0 }

// String returns the template as written.
func (t Template) String() string { return t.Raw }

// Values supplies placeholder values. Lookup returns false when the value is
// unknown; Expand then fails, except for {env.X:-default}.
type Values interface {
	Lookup(ns, name string) (string, bool)
}

// ValuesFunc adapts a function to Values.
type ValuesFunc func(ns, name string) (string, bool)

// Lookup calls f.
func (f ValuesFunc) Lookup(ns, name string) (string, bool) { return f(ns, name) }

// ErrMissing is wrapped by Expand when a placeholder has no value.
var ErrMissing = errors.New("no value")

// Expand fills in every placeholder.
func (t Template) Expand(v Values) (string, error) {
	t, err := t.parsed()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range t.parts {
		if p.ns == "" {
			b.WriteString(p.lit)
			continue
		}
		val, ok := "", false
		if v != nil {
			val, ok = v.Lookup(p.ns, p.name)
		}
		if p.ns == NSEnv && (!ok || val == "") && p.hasDef {
			val, ok = p.def, true
		}
		if !ok {
			return "", fmt.Errorf("{%s.%s}: %w", p.ns, p.name, ErrMissing)
		}
		b.WriteString(val)
	}
	return b.String(), nil
}

// ExpandArgv expands each element of a command on its own, and resolves a
// relative program ("./x", "bin/x") against dir. A bare name ("herdr") is
// left for PATH lookup.
func ExpandArgv(argv []Template, v Values, dir string) ([]string, error) {
	out := make([]string, len(argv))
	for i, t := range argv {
		s, err := t.Expand(v)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", i, err)
		}
		out[i] = s
	}
	if len(out) > 0 && dir != "" && strings.ContainsRune(out[0], '/') && !filepath.IsAbs(out[0]) {
		out[0] = filepath.Join(dir, out[0])
	}
	return out, nil
}

// ExpandPath expands a path template, then a leading ~, then makes it
// absolute against dir.
func ExpandPath(t Template, v Values, dir string) (string, error) {
	s, err := t.Expand(v)
	if err != nil {
		return "", err
	}
	if s == "~" || strings.HasPrefix(s, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expanding ~: %w", err)
		}
		s = filepath.Join(home, s[1:])
	}
	if !filepath.IsAbs(s) && dir != "" {
		s = filepath.Join(dir, s)
	}
	return filepath.Clean(s), nil
}

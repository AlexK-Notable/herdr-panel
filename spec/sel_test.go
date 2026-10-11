package spec

import (
	"errors"
	"strings"
	"testing"
)

const selHead = "spec = 1\ntitle = \"T\"\n"

const selList = `[[section]]
id = "things"
title = "Things"
kind = "list"
source = ["rows"]
columns = ["name"]
`

func TestSelValidSpec(t *testing.T) {
	t.Parallel()
	src := selHead + selList + `[[section]]
title = "Parts of {sel.things.name}"
kind = "list"
source = ["parts", "{sel.things.id}"]
columns = ["part"]
row_actions = [ { label = "Use", key = "u", run = ["use", "{sel.things.id}", "{row.part}"], confirm = "Use {row.part} of {sel.things.name}?", after = "close_then_run" } ]
[[section]]
kind = "text"
source = ["about", "{sel.things.id}"]
[[section]]
kind = "buttons"
buttons = [ { label = "Open", key = "o", run = ["open", "{sel.things.id}"] } ]
`
	s, err := Parse([]byte(src), "x.toml", "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if s.List("things") == nil || s.Sections[1].ID != "" {
		t.Fatalf("list ids: %q %q", s.Sections[0].ID, s.Sections[1].ID)
	}
	if got := s.Sections[1].Source.SelLists(); len(got) != 1 || got[0] != "things" {
		t.Errorf("SelLists = %q", got)
	}
	if a := s.Sections[1].RowActions[0]; a.After != AfterCloseThenRun {
		t.Errorf("after = %q", a.After)
	}
}

func TestSelErrors(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"unknown list", selList + "[[section]]\nkind = \"text\"\nsource = [\"x\", \"{sel.nope.id}\"]\n", `no list section has id "nope"`},
		{"no column", selList + "[[section]]\nkind = \"text\"\nsource = [\"x\", \"{sel.things}\"]\n", "must be {sel.<list id>.<column>}"},
		{"program from a selection", selList + "[[section]]\nkind = \"text\"\nsource = [\"{sel.things.cmd}\"]\n", "the program cannot come from {sel.things.cmd}"},
		{"follows itself", strings.Replace(selList, `source = ["rows"]`, `source = ["rows", "{sel.things.id}"]`, 1), `list "things" follows its own selection`},
		{"two lists in a circle", "[[section]]\nid = \"a\"\nkind = \"list\"\nsource = [\"x\", \"{sel.b.id}\"]\ncolumns = [\"n\"]\n[[section]]\nid = \"b\"\nkind = \"list\"\nsource = [\"y\", \"{sel.a.id}\"]\ncolumns = [\"n\"]\n", "a -> b -> a"},
		{"duplicate id", selList + selList, `list id "things" is used twice`},
		{"bad id", strings.Replace(selList, `id = "things"`, `id = "a b"`, 1), `list id "a b" must be`},
		{"id on a text section", "[[section]]\nid = \"x\"\nkind = \"text\"\ntext = \"hi\"\n", `unknown key "id"`},
		{"lone brace in a title", "[[section]]\ntitle = \"Set {up\"\nkind = \"text\"\ntext = \"hi\"\n", "unclosed {"},
		{"row in a title", "[[section]]\ntitle = \"{row.id}\"\nkind = \"text\"\ntext = \"hi\"\n", "cannot use {row.id}"},
		{"field in a title", "[[section]]\ntitle = \"{field.x}\"\nkind = \"text\"\ntext = \"hi\"\n", "cannot use {field.x}"},
		{"unknown list in a title", "[[section]]\ntitle = \"{sel.nope.name}\"\nkind = \"text\"\ntext = \"hi\"\n", `no list section has id "nope"`},
		{"bad after", selList + "row_actions = [ { label = \"Go\", run = [\"x\"], after = \"later\" } ]\n", "close_then_run"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(selHead+c.src), "x.toml", "/tmp")
			if err == nil {
				t.Fatalf("no error; want %q", c.want)
			}
			var es Errors
			if !errors.As(err, &es) {
				t.Fatalf("err = %v", err)
			}
			for _, e := range es {
				if strings.Contains(e.Msg, c.want) {
					return
				}
			}
			t.Errorf("errors:\n%v\nwant one containing %q", err, c.want)
		})
	}
}

func TestSelCycleIsReportedAtTheList(t *testing.T) {
	t.Parallel()
	src := selHead + strings.Replace(selList, `source = ["rows"]`, `source = ["rows", "{sel.things.id}"]`, 1)
	_, err := Parse([]byte(src), "x.toml", "/tmp")
	var es Errors
	if !errors.As(err, &es) || len(es) != 1 || es[0].Pos.Line != 3 {
		t.Fatalf("want one error at line 3 (the list's [[section]]), got %v", err)
	}
}

func TestExpandTitle(t *testing.T) {
	t.Parallel()
	v := ValuesFunc(func(ns, name string) (string, bool) {
		if ns == NSSel && name == "things.name" {
			return "Alpha", true
		}
		return "", false
	})
	for _, c := range []struct{ title, want string }{
		{"Parts of {sel.things.name}", "Parts of Alpha"},
		{"Parts of {sel.other.name}", "Parts of …"},
		{"Plain {{braces}}", "Plain {braces}"},
		{"Broken {", "Broken {"},
	} {
		s := &Section{Title: c.title}
		if got := s.ExpandTitle(v); got != c.want {
			t.Errorf("ExpandTitle(%q) = %q, want %q", c.title, got, c.want)
		}
	}
	if got := (&Section{Title: "{sel.things.name}"}).ExpandTitle(nil); got != "…" {
		t.Errorf("nil values: %q", got)
	}
}

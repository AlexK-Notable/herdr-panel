package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/AlexK-Notable/herdr-panel/internal/observe"
)

const selSpec = `spec = 1
title = "Garage"
[[section]]
id = "things"
title = "Things"
kind = "list"
source = ["rows"]
columns = [ { key = "name", title = "Name" } ]
[[section]]
title = "Parts of {sel.things.name}"
kind = "list"
source = ["parts", "{sel.things.id}"]
columns = [ { key = "part", title = "Part" } ]
empty = "Pick a thing."
row_actions = [ { label = "Use", key = "u", run = ["use", "{sel.things.id}", "{row.part}"] } ]
[[section]]
kind = "text"
source = ["about", "{sel.things.name}"]
`

var selStubs = []observe.Stub{
	{Match: []string{"rows"}, Stdout: `[{"id":"a","name":"Alpha"},{"id":"b","name":"Beta"},{"id":"c","name":"Gamma"}]`},
	{Match: []string{"parts", "a"}, Stdout: `[{"part":"wheel"}]`},
	{Match: []string{"parts", "b"}, Stdout: `[{"part":"wing"}]`},
	{Match: []string{"parts", "c"}, Stdout: `[{"part":"gear"}]`},
	{Match: []string{"about"}, Stdout: "about it"},
}

func callsOf(f *fixture, prog string) [][]string {
	var out [][]string
	for _, c := range f.sb.Calls() {
		if c.Argv[0] == prog {
			out = append(out, c.Argv)
		}
	}
	return out
}

func TestFollowerFollowsTheSelectedRow(t *testing.T) {
	t.Parallel()
	f := open(t, selSpec, "", selStubs...)
	// The first selection is followed at once: no clock needed.
	f.expectFrame("Parts of Alpha", "wheel", "about it")
	f.click("row:0:1")
	// A selection moved by hand rests first.
	if fr := f.frame(); !strings.Contains(fr, "wheel") || strings.Contains(fr, "wing") {
		t.Fatalf("followed before resting:\n%s", fr)
	}
	f.d.Advance(150 * time.Millisecond)
	f.expectFrame("Parts of Beta", "wing")
	if got := callsOf(f, "about"); strings.Join(got[len(got)-1], " ") != "about Beta" {
		t.Errorf("text follower ran %q", got)
	}
}

func TestPassingOverRowsRunsNothingForThem(t *testing.T) {
	t.Parallel()
	f := open(t, selSpec, "", selStubs...)
	f.expectFrame("wheel")
	before := len(callsOf(f, "parts"))
	f.click("list:0")
	f.key("down", "down") // Alpha -> Beta -> Gamma, without resting on Beta
	f.d.Advance(150 * time.Millisecond)
	f.expectFrame("Parts of Gamma", "gear")
	got := callsOf(f, "parts")[before:]
	if len(got) != 1 || got[0][1] != "c" {
		t.Errorf("parts ran %q after the move, want only c", got)
	}
}

func TestNoSelectionRunsNothing(t *testing.T) {
	t.Parallel()
	f := open(t, selSpec, "", observe.Stub{Match: []string{"rows"}, Stdout: `[]`})
	f.expectFrame("Parts of …", "Pick a thing.")
	if fr := f.frame(); strings.Contains(fr, "✗") {
		t.Errorf("no selection shown as an error:\n%s", fr)
	}
	if n := len(callsOf(f, "parts")) + len(callsOf(f, "about")); n != 0 {
		t.Errorf("followers ran %d times with nothing selected", n)
	}
}

func TestRowActionReadsTheSelection(t *testing.T) {
	t.Parallel()
	f := open(t, selSpec, "", selStubs...)
	f.expectFrame("wheel")
	f.click("row:1:0")
	f.key("u")
	if c := lastCall(f, "use"); strings.Join(c.Argv, " ") != "use a wheel" {
		t.Errorf("argv = %q", c.Argv)
	}
}

func TestLostSelectionClearsFollowers(t *testing.T) {
	t.Parallel()
	f := open(t, selSpec, "", selStubs...)
	f.expectFrame("wheel")
	f.click("list:0")
	f.key("/")
	f.d.Type("zzz") // the filter hides every row
	f.expectFrame("Parts of …", "Pick a thing.")
	if strings.Contains(f.frame(), "wheel") {
		t.Errorf("a follower kept the rows of a selection that is gone:\n%s", f.frame())
	}
}

const closeThenRunSpec = `spec = 1
title = "Launcher"
[stores.c]
kind = "envfile"
path = "c.env"
[[section]]
fields = [ { key = "NAME", type = "text", label = "Name" } ]
[[section]]
kind = "buttons"
buttons = [ { label = "Open other", key = "o", run = ["open-other", "{field.NAME}"], after = "close_then_run" } ]
`

func TestCloseThenRunClosesThenRuns(t *testing.T) {
	t.Parallel()
	f := open(t, closeThenRunSpec, "NAME=x\n")
	f.expectFrame("Open other")
	f.key("o")
	c := lastCall(f, "open-other")
	if !c.AfterExit || strings.Join(c.Argv, " ") != "open-other x" {
		t.Errorf("call = %+v, want open-other x after exit", c)
	}
	if !f.d.Quit {
		t.Error("the panel did not close")
	}
}

func TestCloseThenRunRefusesWithUnsavedEdits(t *testing.T) {
	t.Parallel()
	f := open(t, closeThenRunSpec, "NAME=x\n")
	f.click("field:NAME") // opens it for editing
	f.d.Type("y")
	f.key("enter")
	f.expectFrame("xy • changed")
	f.key("o")
	if f.d.Quit || len(callsOf(f, "open-other")) != 0 {
		t.Fatal("closed and ran with an unsaved edit")
	}
	f.expectFrame("save or revert your 1 unsaved edit(s) first")
}

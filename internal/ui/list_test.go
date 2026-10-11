package ui_test

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/internal/observe"
)

const listSpec = `spec = 1
title = "Things"
[stores.c]
kind = "envfile"
path = "c.env"
[[section]]
kind = "buttons"
buttons = [ { label = "Make", key = "m", run = ["make-thing"] }, { label = "Done", key = "x", run = ["true"], after = "close" } ]
[[section]]
title = "List"
kind = "list"
source = ["rows"]
columns = [ { key = "name", title = "Name" }, { key = "state", width = 8 } ]
row_actions = [
  { label = "Open", key = "o", run = ["open-thing", "{row.id}", "--name={row.name}"] },
  { label = "Delete", key = "d", run = ["delete-thing", "{row.id}"], confirm = "Delete {row.name}?" },
]
`

func rowsStub(json string) observe.Stub {
	return observe.Stub{Match: []string{"rows"}, Stdout: json}
}

func clickAt(x, y int) tea.Msg { return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft} }

// lastCall is the newest call of a program; a successful action is followed
// by a refresh of the list, so the very last call is usually the source.
func lastCall(f *fixture, prog string) observe.Call {
	f.t.Helper()
	calls := f.sb.Calls()
	for _, call := range slices.Backward(calls) {
		if call.Argv[0] == prog {
			return call
		}
	}
	f.t.Fatalf("%s never ran; calls: %+v", prog, calls)
	return observe.Call{}
}

func TestListRowsAndRowAction(t *testing.T) {
	t.Parallel()
	f := open(t, listSpec, "", rowsStub(`[{"id":"a","name":"Alpha","state":true},{"id":"b","name":"Beta with spaces"}]`))
	f.expectFrame("Alpha", "yes", "Beta with spaces")
	f.click("row:1:1")
	f.key("o")
	c := lastCall(f, "open-thing")
	if strings.Join(c.Argv, "|") != "open-thing|b|--name=Beta with spaces" {
		t.Errorf("argv = %q", c.Argv)
	}
	env := strings.Join(c.Env, "\n")
	if !strings.Contains(env, "PANEL_ROW_NAME=Beta with spaces") || !strings.Contains(env, "PANEL_ROW_ID=b") {
		t.Errorf("env = %q", c.Env)
	}
	f.expectFrame("✓ Open")
}

func TestRowsObjectFormAndFailure(t *testing.T) {
	t.Parallel()
	f := open(t, listSpec, "", rowsStub(`{"rows": [{"id":"a","name":"Alpha"}]}`))
	f.expectFrame("Alpha")
	g := open(t, listSpec, "", rowsStub(`{"ok": false, "message": "backend down"}`))
	g.expectFrame("✗ backend down")
	h := open(t, listSpec, "", rowsStub(`not json`))
	h.expectFrame("✗ the source did not print JSON")
}

func TestConfirmBeforeDestructiveAction(t *testing.T) {
	t.Parallel()
	f := open(t, listSpec, "", rowsStub(`[{"id":"a","name":"Alpha"}]`))
	f.click("row:1:0")
	f.key("d")
	f.expectFrame("Delete Alpha?")
	before := len(f.sb.Calls())
	f.key("n")
	if len(f.sb.Calls()) != before {
		t.Fatal("ran despite No")
	}
	f.key("d", "y")
	if c := lastCall(f, "delete-thing"); strings.Join(c.Argv, " ") != "delete-thing a" {
		t.Errorf("argv = %q", c.Argv)
	}
}

func TestOkFalseActionIsAFailure(t *testing.T) {
	t.Parallel()
	f := open(t, listSpec, "", observe.Stub{Match: []string{"make-thing"}, Stdout: `{"ok": false, "message": "quota exceeded"}`})
	f.key("m")
	f.expectFrame("✗ Make: quota exceeded")
}

func TestAfterCloseQuitsOnlyOnSuccess(t *testing.T) {
	t.Parallel()
	f := open(t, listSpec, "")
	f.key("x")
	if !f.d.Quit {
		t.Error("after = close did not close")
	}
	g := open(t, listSpec, "", observe.Stub{Match: []string{"true"}, Exit: 1})
	g.key("x")
	if g.d.Quit {
		t.Error("closed after a failure")
	}
}

func TestFilter(t *testing.T) {
	t.Parallel()
	f := open(t, listSpec, "", rowsStub(`[{"id":"a","name":"Alpha"},{"id":"b","name":"Beta"},{"id":"c","name":"Gamma"}]`))
	f.click("list:1")
	f.key("/")
	f.d.Type("ta")
	fr := f.frame()
	if !strings.Contains(fr, "Beta") || strings.Contains(fr, "Alpha") || strings.Contains(fr, "Gamma") {
		t.Fatalf("filter 'ta':\n%s", fr)
	}
	f.key("enter", "esc")
	f.expectFrame("Alpha", "Beta", "Gamma")
}

func TestWheelMovesListSelection(t *testing.T) {
	t.Parallel()
	f := open(t, listSpec, "", rowsStub(`[{"id":"a","name":"Alpha"},{"id":"b","name":"Beta"}]`))
	f.click("row:1:0")
	for _, e := range f.d.Model().Elements() {
		if e.ID == "row:1:0" {
			f.d.Send(tea.MouseWheelMsg{X: e.X + 1, Y: e.Y, Button: tea.MouseWheelDown})
		}
	}
	f.key("o")
	if c := lastCall(f, "open-thing"); c.Argv[1] != "b" {
		t.Errorf("argv = %q", c.Argv)
	}
}

func TestShortcutWithoutSelectionExplains(t *testing.T) {
	t.Parallel()
	f := open(t, listSpec, "", rowsStub(`[]`))
	f.key("o")
	f.expectFrame("Open: no row selected", "nothing here")
}

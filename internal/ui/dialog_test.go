package ui_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// onScreen fails when the frame or any element reaches past w×h.
func onScreen(t *testing.T, f *fixture, w, h int) {
	t.Helper()
	if n := strings.Count(f.frame(), "\n") + 1; n > h {
		t.Fatalf("frame has %d lines on a %d-line screen:\n%s", n, h, f.frame())
	}
	for _, e := range f.d.Model().Elements() {
		if e.Y >= h || e.X >= w {
			t.Errorf("element %s at %d,%d is off a %dx%d screen", e.ID, e.X, e.Y, w, h)
		}
	}
}

func hasElement(f *fixture, id string) bool {
	for _, e := range f.d.Model().Elements() {
		if e.ID == id {
			return true
		}
	}
	return false
}

func TestHelpFitsAndScrolls(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "")
	f.d.Resize(57, 12)
	f.key("?")
	onScreen(t, f, 57, 12)
	f.expectFrame("more below", "[Close]")
	if strings.Contains(f.frame(), "wheel scrolls") {
		t.Fatal("the last help line fits; the test needs a shorter screen")
	}
	for range 12 {
		f.key("down")
	}
	onScreen(t, f, 57, 12)
	f.expectFrame("wheel scrolls", "more above", "[Close]")
	f.key("x") // any other key closes help
	if strings.Contains(f.frame(), "[Close]") {
		t.Fatal("help did not close")
	}
}

func TestLongPickerKeepsTheCursorOnScreen(t *testing.T) {
	t.Parallel()
	choices := make([]string, 20)
	for i := range choices {
		choices[i] = fmt.Sprintf("%q", fmt.Sprintf("opt%02d", i))
	}
	src := `spec = 1
title = "P"
[stores.c]
kind = "envfile"
path = "c.env"
[[section]]
fields = [ { key = "C", type = "choice", label = "Pick", choices = [` + strings.Join(choices, ", ") + `], default = "opt00" } ]
`
	f := open(t, src, "")
	f.d.Resize(60, 12)
	f.click("field:C")
	onScreen(t, f, 60, 12)
	f.expectFrame("opt00", "more below")
	for i := 1; i <= 15; i++ {
		f.key("down")
		onScreen(t, f, 60, 12)
		if !hasElement(f, fmt.Sprintf("option:%d", i)) {
			t.Fatalf("cursor option %d is not on screen:\n%s", i, f.frame())
		}
	}
	f.expectFrame("more above", "more below")
	f.key("enter")
	f.expectFrame("‹ opt15 ›")
}

func TestConflictIgnoresYAndN(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "MODE=slow\n")
	f.click("field:ON")
	if err := os.WriteFile(f.file, []byte("MODE=fast\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.key("ctrl+s")
	f.expectFrame("Changed on disk")
	f.key("y", "n")
	f.expectFrame("Changed on disk")
	if f.d.Model().Dirty() != 1 {
		t.Fatalf("y or n answered the conflict: dirty %d", f.d.Model().Dirty())
	}
}

func TestHiddenDialogTakesOnlyEsc(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "")
	f.click("field:MODE")
	f.d.Resize(20, 4)
	f.key("down", "enter")
	f.d.Resize(80, 30)
	if f.d.Model().Dirty() != 0 || !hasElement(f, "option:0") {
		t.Fatalf("a key answered a dialog that was not on screen: dirty %d\n%s", f.d.Model().Dirty(), f.frame())
	}
	f.d.Resize(20, 4)
	f.key("esc")
	f.d.Resize(80, 30)
	if hasElement(f, "option:0") {
		t.Fatal("esc did not close the hidden dialog")
	}
}

func longSpec(n int) string {
	var fields []string
	for i := range n {
		fields = append(fields, fmt.Sprintf(`{ key = "K%02d", type = "bool", label = "Field %02d" }`, i, i))
	}
	return `spec = 1
title = "Long"
[stores.c]
kind = "envfile"
path = "c.env"
[[section]]
fields = [ ` + strings.Join(fields, ",\n") + ` ]
`
}

func TestWheelAndPgDnScrollTheBody(t *testing.T) {
	t.Parallel()
	f := open(t, longSpec(30), "")
	f.d.Resize(60, 12)
	f.expectFrame("Field 00")
	for range 5 {
		f.d.Send(wheelAt(10, 5, false))
	}
	if strings.Contains(f.frame(), "Field 00") {
		t.Fatalf("the wheel did not scroll the body:\n%s", f.frame())
	}
	f.key("pgdown")
	f.expectFrame("Field 1")
	if strings.Contains(f.frame(), "Field 04") {
		t.Fatalf("pgdown did not scroll:\n%s", f.frame())
	}
	f.key("tab") // moving the focus brings it back on screen
	if !hasElement(f, f.d.Model().Focused()) {
		t.Fatalf("focus %s is off screen after tab:\n%s", f.d.Model().Focused(), f.frame())
	}
}

func TestWheelBehindADialogMovesOnlyTheDialog(t *testing.T) {
	t.Parallel()
	f := open(t, longSpec(30), "")
	f.d.Resize(60, 12)
	f.key("?")
	for range 3 {
		f.d.Send(wheelAt(1, 1, false))
	}
	f.expectFrame("more above") // the help text scrolled
	f.key("esc")
	f.expectFrame("Field 00") // the body did not
}

func wheelAt(x, y int, up bool) tea.Msg {
	b := tea.MouseWheelDown
	if up {
		b = tea.MouseWheelUp
	}
	return tea.MouseWheelMsg{X: x, Y: y, Button: b}
}

// A choice with no default can be left out of the file; "not set" is how
// it gets back there.
func TestChoiceWithoutDefaultCanBeUnset(t *testing.T) {
	t.Parallel()
	src := `spec = 1
title = "P"
[stores.c]
kind = "envfile"
path = "c.env"
[[section]]
fields = [ { key = "B", type = "choice", label = "Base", choices = ["base", "head"] } ]
`
	f := open(t, src, "B=head\n")
	f.expectFrame("‹ head ›")
	f.click("field:B")
	f.expectFrame("not set", "base", "head")
	f.click("option:0") // not set
	f.expectFrame("‹ not set ›")
	f.key("ctrl+s")
	if got := f.fileText(); got != "" {
		t.Fatalf("not set left %q in the file", got)
	}
	f.key("right") // not set → base
	f.expectFrame("‹ base ›")
	f.key("right", "right") // → head → not set
	f.expectFrame("‹ not set ›")
}

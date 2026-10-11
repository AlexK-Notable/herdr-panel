package ui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/internal/observe"
	"github.com/AlexK-Notable/herdr-panel/internal/ui"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

const viewSpec = `spec = 1
title = "V"
[[section]]
kind = "view"
view = "v"
[[section]]
kind = "buttons"
buttons = [ { label = "Go", run = ["go-thing"] } ]
`

// boom is a view that panics where it is told to.
type boom struct{ where string }

func (b boom) Render(int, bool) ui.ViewOutput {
	if b.where == "render" {
		panic("render exploded")
	}
	return ui.ViewOutput{
		Lines:   []string{"\x1b[1mbold\x1b[0m [a] [b]", "wide 世界 [c]"},
		Regions: []ui.ViewRegion{{ID: "a", Line: 0, X: 5, W: 3}, {ID: "b", Line: 0, X: 9, W: 3}, {ID: "c", Line: 1, X: 10, W: 3}, {ID: "off", Line: 0, X: 40, W: 2}},
	}
}

func (b boom) Update(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(ui.ViewClick); !ok {
		return nil
	}
	switch b.where {
	case "update":
		panic("update exploded")
	case "cmd":
		return func() tea.Msg { panic("command exploded") }
	}
	return nil
}

func viewDriver(t *testing.T, v ui.View) *observe.Driver {
	t.Helper()
	s, err := spec.Parse([]byte(viewSpec), "v.toml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return observe.New(ui.Options{Spec: s, Runner: &observe.Sandbox{}, CrashDir: t.TempDir(), Views: map[string]ui.View{"v": v}}, 60, 16)
}

func TestViewPanicsShowTheCrashScreen(t *testing.T) {
	t.Parallel()
	for _, where := range []string{"render", "update", "cmd"} {
		d := viewDriver(t, boom{where})
		if where != "render" {
			if err := d.Click("view:v:a"); err != nil {
				t.Fatal(err)
			}
		}
		if !d.Model().Crashed() || !strings.Contains(d.Model().Frame(), "exploded") {
			t.Errorf("%s: no crash screen:\n%s", where, d.Model().Frame())
		}
	}
}

func TestViewRegionsHitThemselves(t *testing.T) {
	t.Parallel()
	d := viewDriver(t, boom{})
	got := map[string]bool{}
	for _, e := range d.Model().Elements() {
		got[e.ID] = true
		if hit := d.Model().Hit(e.X+e.W/2, e.Y); hit != e.ID {
			t.Errorf("centre of %s hits %q", e.ID, hit)
		}
	}
	// "off" lies past the end of its line and is dropped; styles and wide
	// characters do not shift the cells.
	if !got["view:v:a"] || !got["view:v:b"] || !got["view:v:c"] || got["view:v:off"] {
		t.Errorf("elements = %v", d.IDs())
	}
	for _, e := range d.Model().Elements() {
		if e.ID == "view:v:c" && e.X != 11 { // one cell of indent, then 10
			t.Errorf("region c at x %d, want 11", e.X)
		}
	}
}

func TestMissingViewIsShownNotCrashed(t *testing.T) {
	t.Parallel()
	s, err := spec.Parse([]byte(viewSpec), "v.toml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := observe.New(ui.Options{Spec: s, Runner: &observe.Sandbox{}}, 60, 16)
	if !strings.Contains(d.Model().Frame(), "no view v was supplied") {
		t.Errorf("frame:\n%s", d.Model().Frame())
	}
}

// echo is a view whose command returns whatever it was told to.
type echo struct {
	send tea.Msg
	got  []tea.Msg
}

func (e *echo) Render(int, bool) ui.ViewOutput {
	return ui.ViewOutput{Lines: []string{"[go]"}, Regions: []ui.ViewRegion{{ID: "go", Line: 0, X: 0, W: 4}}}
}

func (e *echo) Update(msg tea.Msg) tea.Cmd {
	e.got = append(e.got, msg)
	if _, ok := msg.(ui.ViewClick); ok {
		send := e.send
		return func() tea.Msg { return send }
	}
	return nil
}

// A Bubble Tea event a view's command returns goes back to that view, never
// to the panel: an esc would close it, a size would shrink it.
func TestViewEventsComeBackToTheView(t *testing.T) {
	t.Parallel()
	for _, msg := range []tea.Msg{
		tea.KeyPressMsg{Code: tea.KeyEscape},
		tea.WindowSizeMsg{Width: 4, Height: 2},
		tea.SuspendMsg{},
	} {
		e := &echo{send: msg}
		d := viewDriver(t, e)
		if err := d.Click("view:v:go"); err != nil {
			t.Fatal(err)
		}
		if d.Quit || strings.Contains(d.Model().Frame(), "make me bigger") {
			t.Errorf("%T reached the panel: quit %v, frame %q", msg, d.Quit, d.Model().Frame())
		}
		if last := e.got[len(e.got)-1]; last != msg {
			t.Errorf("%T: the view got %T last", msg, last)
		}
	}
}

// regions is a view that draws whatever regions it is given.
type regions []ui.ViewRegion

func (r regions) Render(int, bool) ui.ViewOutput {
	return ui.ViewOutput{Lines: []string{"[one] [two]"}, Regions: r}
}

func (regions) Update(tea.Msg) tea.Cmd { return nil }

func TestBadRegionIDsShowTheCrashScreen(t *testing.T) {
	t.Parallel()
	for _, rs := range []regions{
		{{ID: "a b", Line: 0, X: 0, W: 5}},
		{{ID: "", Line: 0, X: 0, W: 5}},
		{{ID: "x", Line: 0, X: 0, W: 5}, {ID: "x", Line: 0, X: 6, W: 5}},
	} {
		d := viewDriver(t, rs)
		if !d.Model().Crashed() {
			t.Errorf("regions %+v drew without complaint:\n%s", rs, d.Model().Frame())
		}
	}
	if d := viewDriver(t, regions{{ID: "one_1", Line: 0, X: 0, W: 5}, {ID: "Two-2", Line: 0, X: 6, W: 5}}); d.Model().Crashed() {
		t.Errorf("valid ids refused:\n%s", d.Model().Frame())
	}
}
